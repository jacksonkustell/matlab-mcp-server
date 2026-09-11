// Copyright 2026 The MathWorks, Inc.

// Package matlabcommandqueue manages asynchronous, serialized MATLAB commands.
package matlabcommandqueue

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/matlab/matlab-mcp-server/internal/entities"
	"github.com/matlab/matlab-mcp-server/internal/messages"
	"github.com/matlab/matlab-mcp-server/internal/usecases/evalmatlabcode"
)

const (
	defaultMonitorInterval = time.Second

	queuedStatusMessage     = "queued"
	inProgressStatusMessage = "in progress"
	completedStatusMessage  = "completed"
	queuedCommandMessage    = "Command queued."
)

var (
	errCodeBlank    = errors.New("code must not be blank")
	errQueueStopped = errors.New("MATLAB command queue is shutting down")
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
	globalMATLAB  entities.GlobalMATLAB
	evaluator     MATLABCodeEvaluator

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

	monitorInterval time.Duration
}

// New creates a process-lifetime command queue and registers a graceful shutdown hook.
func New(
	loggerFactory LoggerFactory,
	globalMATLAB entities.GlobalMATLAB,
	evaluator MATLABCodeEvaluator,
	lifecycleSignaler LifecycleSignaler,
) *Queue {
	return newQueue(
		loggerFactory,
		globalMATLAB,
		evaluator,
		lifecycleSignaler,
		defaultMonitorInterval,
	)
}

func newQueue(
	loggerFactory LoggerFactory,
	globalMATLAB entities.GlobalMATLAB,
	evaluator MATLABCodeEvaluator,
	lifecycleSignaler LifecycleSignaler,
	monitorInterval time.Duration,
) *Queue {
	if monitorInterval <= 0 {
		monitorInterval = defaultMonitorInterval
	}

	lifetimeContext, cancelLifetime := context.WithCancel(context.Background())
	queue := &Queue{
		loggerFactory: loggerFactory,
		globalMATLAB:  globalMATLAB,
		evaluator:     evaluator,

		commands:   make(map[string]*commandRecord),
		wakeWorker: make(chan struct{}, 1),

		lifetimeContext: lifetimeContext,
		cancelLifetime:  cancelLifetime,
		workerDone:      make(chan struct{}),

		monitorInterval: monitorInterval,
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
	stopMonitor := q.startMonitor(command.CommandID)

	logger, loggerErr := q.loggerFactory.GetGlobalLogger()
	if loggerErr != nil {
		stopMonitor()
		q.completeFailure(command.CommandID, loggerErr)
		return
	}

	client, err := q.globalMATLAB.Client(q.lifetimeContext, logger)
	if err != nil {
		stopMonitor()
		q.completeFailure(command.CommandID, err)
		return
	}

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

func (q *Queue) startMonitor(commandID string) func() {
	monitorContext, cancelMonitor := context.WithCancel(q.lifetimeContext)
	monitorDone := make(chan struct{})

	go func() {
		defer close(monitorDone)

		ticker := time.NewTicker(q.monitorInterval)
		defer ticker.Stop()

		iteration := 0
		for {
			select {
			case <-monitorContext.Done():
				return
			case <-ticker.C:
				iteration++
				q.updateMonitoringStatus(commandID, iteration)
			}
		}
	}()

	return func() {
		cancelMonitor()
		<-monitorDone
	}
}

func (q *Queue) updateMonitoringStatus(commandID string, iteration int) {
	q.mutex.Lock()
	defer q.mutex.Unlock()

	record, exists := q.commands[commandID]
	if !exists || record.command.Status != StatusInProgress {
		return
	}

	record.command.StatusMessage = fmt.Sprintf("monitoring - %d", iteration)
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
