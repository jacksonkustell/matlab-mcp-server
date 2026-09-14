// Copyright 2026 The MathWorks, Inc.

// Package matlabcommandqueue manages asynchronous, serialized MATLAB commands.
package matlabcommandqueue

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/matlab/matlab-mcp-server/internal/entities"
	"github.com/matlab/matlab-mcp-server/internal/messages"
	"github.com/matlab/matlab-mcp-server/internal/usecases/evalmatlabcode"
)

const (
	queuedStatusMessage     = "queued"
	inProgressStatusMessage = "in progress"
	completedStatusMessage  = "completed"
	queuedCommandMessage    = "Command queued."
)

var (
	errCodeBlank                         = errors.New("code must not be blank")
	errMonitoringSubscriptionUnavailable = errors.New("monitoring subscription was not created")
	errQueueStopped                      = errors.New("MATLAB command queue is shutting down")
)

type Status string

const (
	StatusQueued     Status = "queued"
	StatusInProgress Status = "in_progress"
	StatusCompleted  Status = "completed"
	StatusFailed     Status = "failed"
)

type LoggerFactory interface {
	GetGlobalLogger() (entities.Logger, messages.Error)
}

type LifecycleSignaler interface {
	AddShutdownFunction(shutdownFunction func() error)
}

type MATLABCodeEvaluator interface {
	Execute(ctx context.Context, sessionLogger entities.Logger, client entities.MATLABSessionClient, request evalmatlabcode.Args) (entities.EvalResponse, error)
}

// MATLABClientProvider supplies the shared MATLAB client and the identity of
// the session that owns it.
type MATLABClientProvider interface {
	ClientWithSessionID(ctx context.Context, logger entities.Logger) (entities.MATLABSessionClient, entities.SessionID, error)
}

// MonitoringEvent is a live monitoring message received while a command runs.
type MonitoringEvent struct {
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
}

// MonitoringSubscription stops delivery and waits for in-flight callbacks to finish.
type MonitoringSubscription interface {
	Unsubscribe()
}

// Monitoring delivers live monitoring events independently of their transport.
type Monitoring interface {
	EnsureRegistered(ctx context.Context, logger entities.Logger, client entities.MATLABSessionClient, sessionID entities.SessionID) error
	Subscribe(onEvent func(MonitoringEvent), onError func(error)) (MonitoringSubscription, error)
}

// Command is the public snapshot of a queued MATLAB command.
type Command struct {
	CommandID     string `json:"command_id"             jsonschema:"The stable identifier assigned to this command."`
	Code          string `json:"code"                   jsonschema:"The MATLAB code submitted for execution."`
	ProjectPath   string `json:"project_path,omitempty" jsonschema:"The optional MATLAB project folder used as the working folder before execution."`
	Status        Status `json:"status"                 jsonschema:"The command status: queued, in_progress, completed, or failed."`
	Output        string `json:"output,omitempty"       jsonschema:"Console output produced by a completed command, when available."`
	StatusMessage string `json:"status_message"         jsonschema:"A human-readable status or failure message for the command."`
}

// EnqueueResult confirms that a command was accepted without waiting for MATLAB.
type EnqueueResult struct {
	CommandID string `json:"command_id" jsonschema:"The stable identifier assigned to the queued command."`
	Status    Status `json:"status"     jsonschema:"The initial command status, always queued."`
	Message   string `json:"message"    jsonschema:"A message confirming that the command was queued."`
}

type commandRecord struct {
	command Command
}

// Queue owns all command records and processes them in first-in, first-out order.
type Queue struct {
	loggerFactory LoggerFactory
	globalMATLAB  MATLABClientProvider
	evaluator     MATLABCodeEvaluator
	monitoring    Monitoring

	mutex      sync.Mutex
	commands   map[string]*commandRecord
	order      []string
	pending    []string
	nextID     uint64
	stopped    bool
	wakeWorker chan struct{}

	lifetimeContext context.Context
	cancelLifetime  context.CancelFunc
	workerDone      chan struct{}
	shutdownOnce    sync.Once
}

// New creates a process-lifetime command queue and registers a graceful shutdown hook.
func New(
	loggerFactory LoggerFactory,
	globalMATLAB MATLABClientProvider,
	evaluator MATLABCodeEvaluator,
	monitoring Monitoring,
	lifecycleSignaler LifecycleSignaler,
) *Queue {
	return newQueue(
		loggerFactory,
		globalMATLAB,
		evaluator,
		monitoring,
		lifecycleSignaler,
	)
}

func newQueue(
	loggerFactory LoggerFactory,
	globalMATLAB MATLABClientProvider,
	evaluator MATLABCodeEvaluator,
	monitoring Monitoring,
	lifecycleSignaler LifecycleSignaler,
) *Queue {
	if monitoring == nil {
		monitoring = noopMonitoring{}
	}

	lifetimeContext, cancelLifetime := context.WithCancel(context.Background())
	queue := &Queue{
		loggerFactory: loggerFactory,
		globalMATLAB:  globalMATLAB,
		evaluator:     evaluator,
		monitoring:    monitoring,

		commands:   make(map[string]*commandRecord),
		wakeWorker: make(chan struct{}, 1),

		lifetimeContext: lifetimeContext,
		cancelLifetime:  cancelLifetime,
		workerDone:      make(chan struct{}),
	}

	go queue.runWorker()
	lifecycleSignaler.AddShutdownFunction(queue.Shutdown)

	return queue
}

// Enqueue accepts a nonblank MATLAB command and returns before MATLAB is contacted.
func (q *Queue) Enqueue(code string, projectPath string) (EnqueueResult, error) {
	if strings.TrimSpace(code) == "" {
		return EnqueueResult{}, errCodeBlank
	}

	q.mutex.Lock()
	if q.stopped {
		q.mutex.Unlock()
		return EnqueueResult{}, errQueueStopped
	}

	q.nextID++
	commandID := fmt.Sprintf("command-%d", q.nextID)
	q.commands[commandID] = &commandRecord{
		command: Command{
			CommandID:     commandID,
			Code:          code,
			ProjectPath:   projectPath,
			Status:        StatusQueued,
			StatusMessage: queuedStatusMessage,
		},
	}
	q.order = append(q.order, commandID)
	q.pending = append(q.pending, commandID)
	q.mutex.Unlock()

	q.wakeWorkerIfNeeded()

	return EnqueueResult{
		CommandID: commandID,
		Status:    StatusQueued,
		Message:   queuedCommandMessage,
	}, nil
}

// Poll returns all commands in creation order and consumes terminal commands atomically.
func (q *Queue) Poll() []Command {
	q.mutex.Lock()
	defer q.mutex.Unlock()

	commands := make([]Command, 0, len(q.order))
	activeCommandIDs := make([]string, 0, len(q.order))

	for _, commandID := range q.order {
		record, exists := q.commands[commandID]
		if !exists {
			continue
		}

		commands = append(commands, record.command)
		if isTerminal(record.command.Status) {
			delete(q.commands, commandID)
			continue
		}

		activeCommandIDs = append(activeCommandIDs, commandID)
	}

	q.order = activeCommandIDs
	return commands
}

// Shutdown stops the worker after canceling the queue lifetime context.
func (q *Queue) Shutdown() error {
	q.shutdownOnce.Do(func() {
		q.mutex.Lock()
		q.stopped = true
		q.mutex.Unlock()

		q.cancelLifetime()
		<-q.workerDone
	})

	return nil
}

func (q *Queue) wakeWorkerIfNeeded() {
	select {
	case q.wakeWorker <- struct{}{}:
	default:
	}
}

func (q *Queue) runWorker() {
	defer close(q.workerDone)

	for {
		command, ok := q.dequeue()
		if !ok {
			select {
			case <-q.lifetimeContext.Done():
				return
			case <-q.wakeWorker:
				continue
			}
		}

		q.execute(command)
	}
}

func (q *Queue) dequeue() (Command, bool) {
	q.mutex.Lock()
	defer q.mutex.Unlock()

	if len(q.pending) == 0 {
		return Command{}, false
	}

	commandID := q.pending[0]
	q.pending = q.pending[1:]

	record, exists := q.commands[commandID]
	if !exists {
		return Command{}, false
	}

	record.command.Status = StatusInProgress
	record.command.StatusMessage = inProgressStatusMessage

	return record.command, true
}

func (q *Queue) execute(command Command) {
	logger, loggerErr := q.loggerFactory.GetGlobalLogger()
	if loggerErr != nil {
		q.completeFailure(command.CommandID, loggerErr)
		return
	}

	client, sessionID, err := q.globalMATLAB.ClientWithSessionID(q.lifetimeContext, logger)
	if err != nil {
		q.completeFailure(command.CommandID, err)
		return
	}

	stopMonitor := q.startMonitor(command.CommandID, logger, client, sessionID)

	response, err := q.evaluator.Execute(
		q.lifetimeContext,
		logger,
		client,
		evalmatlabcode.Args{
			Code:          command.Code,
			ProjectPath:   command.ProjectPath,
			CaptureOutput: false,
		},
	)
	if err != nil {
		stopMonitor()
		q.completeFailure(command.CommandID, err)
		return
	}

	stopMonitor()
	q.completeSuccess(command.CommandID, response.ConsoleOutput)
}

func (q *Queue) startMonitor(
	commandID string,
	logger entities.Logger,
	client entities.MATLABSessionClient,
	sessionID entities.SessionID,
) func() {
	if err := q.monitoring.EnsureRegistered(q.lifetimeContext, logger, client, sessionID); err != nil {
		q.updateMonitoringUnavailable(commandID, err)
	}

	subscription, err := q.monitoring.Subscribe(
		func(event MonitoringEvent) {
			q.updateMonitoringStatus(commandID, event)
		},
		func(monitoringErr error) {
			q.updateMonitoringUnavailable(commandID, monitoringErr)
		},
	)
	if err != nil {
		q.updateMonitoringUnavailable(commandID, err)
		return func() {}
	}
	if subscription == nil {
		q.updateMonitoringUnavailable(commandID, errMonitoringSubscriptionUnavailable)
		return func() {}
	}

	return subscription.Unsubscribe
}

func (q *Queue) updateMonitoringStatus(commandID string, event MonitoringEvent) {
	q.mutex.Lock()
	defer q.mutex.Unlock()

	record, exists := q.commands[commandID]
	if !exists || record.command.Status != StatusInProgress {
		return
	}

	record.command.StatusMessage = fmt.Sprintf("monitoring - %s: %s", event.Timestamp, event.Message)
}

func (q *Queue) updateMonitoringUnavailable(commandID string, err error) {
	q.mutex.Lock()
	defer q.mutex.Unlock()

	record, exists := q.commands[commandID]
	if !exists || record.command.Status != StatusInProgress {
		return
	}

	record.command.StatusMessage = fmt.Sprintf("monitoring unavailable: %v", err)
}

func (q *Queue) completeSuccess(commandID string, output string) {
	q.mutex.Lock()
	defer q.mutex.Unlock()

	record, exists := q.commands[commandID]
	if !exists {
		return
	}

	record.command.Status = StatusCompleted
	record.command.Output = output
	record.command.StatusMessage = completedStatusMessage
}

func (q *Queue) completeFailure(commandID string, err error) {
	q.mutex.Lock()
	defer q.mutex.Unlock()

	record, exists := q.commands[commandID]
	if !exists {
		return
	}

	record.command.Status = StatusFailed
	record.command.StatusMessage = err.Error()
}

func isTerminal(status Status) bool {
	return status == StatusCompleted || status == StatusFailed
}

type noopMonitoring struct{}

func (noopMonitoring) EnsureRegistered(context.Context, entities.Logger, entities.MATLABSessionClient, entities.SessionID) error {
	return nil
}

func (noopMonitoring) Subscribe(func(MonitoringEvent), func(error)) (MonitoringSubscription, error) {
	return noopMonitoringSubscription{}, nil
}

type noopMonitoringSubscription struct{}

func (noopMonitoringSubscription) Unsubscribe() {}
