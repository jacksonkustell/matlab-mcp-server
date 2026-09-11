// Copyright 2026 The MathWorks, Inc.

package matlabcommandqueue

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/matlab/matlab-mcp-server/internal/entities"
	"github.com/matlab/matlab-mcp-server/internal/messages"
	"github.com/matlab/matlab-mcp-server/internal/usecases/evalmatlabcode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testWaitTimeout = 2 * time.Second

func TestQueue_RejectsBlankCode(t *testing.T) {
	queue, _ := newTestQueue(t, false, defaultMonitorInterval)

	result, err := queue.Enqueue(" \t\n", "")

	require.Error(t, err)
	assert.Empty(t, result)
	assert.Empty(t, queue.Poll())
}

func TestQueue_EnqueueReturnsWhileEvaluationIsBlocked(t *testing.T) {
	queue, evaluator := newTestQueue(t, true, defaultMonitorInterval)

	first, err := queue.Enqueue("first", "")
	require.NoError(t, err)
	require.Equal(t, StatusQueued, first.Status)
	waitForEvaluation(t, evaluator, "first")

	resultC := make(chan EnqueueResult, 1)
	errC := make(chan error, 1)
	go func() {
		result, enqueueErr := queue.Enqueue("second", "")
		resultC <- result
		errC <- enqueueErr
	}()

	select {
	case result := <-resultC:
		assert.Equal(t, "command-2", result.CommandID)
		assert.Equal(t, StatusQueued, result.Status)
		assert.Equal(t, queuedCommandMessage, result.Message)
	case <-time.After(testWaitTimeout):
		t.Fatal("enqueue blocked while MATLAB evaluation was in progress")
	}
	require.NoError(t, <-errC)

	commands := queue.Poll()
	require.Len(t, commands, 2)
	assert.Equal(t, StatusInProgress, commands[0].Status)
	assert.Equal(t, StatusQueued, commands[1].Status)
}

func TestQueue_TransitionsToCompletedAndConsumesTerminalCommand(t *testing.T) {
	queue, evaluator := newTestQueue(t, true, defaultMonitorInterval)
	evaluator.responses["disp('done')"] = entities.EvalResponse{ConsoleOutput: "done\n"}

	result, err := queue.Enqueue("disp('done')", `C:\project`)
	require.NoError(t, err)
	waitForEvaluation(t, evaluator, "disp('done')")

	active := queue.Poll()
	require.Len(t, active, 1)
	assert.Equal(t, result.CommandID, active[0].CommandID)
	assert.Equal(t, StatusInProgress, active[0].Status)
	assert.Equal(t, inProgressStatusMessage, active[0].StatusMessage)
	assert.Empty(t, active[0].Output)

	evaluator.release <- struct{}{}
	completed := waitForTerminalSnapshot(t, queue, result.CommandID)
	assert.Equal(t, StatusCompleted, completed.Status)
	assert.Equal(t, "done\n", completed.Output)
	assert.Equal(t, completedStatusMessage, completed.StatusMessage)

	terminal := queue.Poll()
	require.Len(t, terminal, 1)
	assert.Equal(t, completed, terminal[0])
	assert.Empty(t, queue.Poll())
}

func TestQueue_RecordsFailureAndConsumesItOnce(t *testing.T) {
	queue, evaluator := newTestQueue(t, false, defaultMonitorInterval)
	expectedError := errors.New("MATLAB command failed")
	evaluator.errors["badCommand"] = expectedError

	result, err := queue.Enqueue("badCommand", "")
	require.NoError(t, err)

	failed := waitForTerminalSnapshot(t, queue, result.CommandID)
	assert.Equal(t, StatusFailed, failed.Status)
	assert.Empty(t, failed.Output)
	assert.Equal(t, expectedError.Error(), failed.StatusMessage)

	terminal := queue.Poll()
	require.Len(t, terminal, 1)
	assert.Equal(t, StatusFailed, terminal[0].Status)
	assert.Equal(t, expectedError.Error(), terminal[0].StatusMessage)
	assert.Empty(t, queue.Poll())
}

func TestQueue_ExecutesCommandsInFIFOOrderWithOneEvaluationAtATime(t *testing.T) {
	queue, evaluator := newTestQueue(t, true, defaultMonitorInterval)

	first, err := queue.Enqueue("first", "")
	require.NoError(t, err)
	second, err := queue.Enqueue("second", "")
	require.NoError(t, err)
	third, err := queue.Enqueue("third", "")
	require.NoError(t, err)

	waitForEvaluation(t, evaluator, "first")
	assertNoEvaluation(t, evaluator)

	evaluator.release <- struct{}{}
	waitForEvaluation(t, evaluator, "second")
	assertNoEvaluation(t, evaluator)

	evaluator.release <- struct{}{}
	waitForEvaluation(t, evaluator, "third")
	evaluator.release <- struct{}{}

	waitForTerminalSnapshot(t, queue, first.CommandID)
	waitForTerminalSnapshot(t, queue, second.CommandID)
	waitForTerminalSnapshot(t, queue, third.CommandID)

	assert.Equal(t, []string{"first", "second", "third"}, evaluator.Codes())
	assert.Equal(t, 1, evaluator.MaximumConcurrentCalls())

	terminal := queue.Poll()
	require.Len(t, terminal, 3)
	assert.Equal(t, []string{first.CommandID, second.CommandID, third.CommandID}, []string{
		terminal[0].CommandID,
		terminal[1].CommandID,
		terminal[2].CommandID,
	})
}

func TestQueue_MonitorStartsForInProgressCommandAndStopsBeforeCompletion(t *testing.T) {
	const monitorInterval = 5 * time.Millisecond

	queue, evaluator := newTestQueue(t, true, monitorInterval)
	result, err := queue.Enqueue("longRunningCommand", "")
	require.NoError(t, err)
	waitForEvaluation(t, evaluator, "longRunningCommand")

	eventually(t, func() bool {
		command, exists := snapshotCommand(queue, result.CommandID)
		return exists && command.Status == StatusInProgress && command.StatusMessage != inProgressStatusMessage
	})

	monitored, exists := snapshotCommand(queue, result.CommandID)
	require.True(t, exists)
	assert.Regexp(t, `^monitoring - [1-9][0-9]*$`, monitored.StatusMessage)

	evaluator.release <- struct{}{}
	completed := waitForTerminalSnapshot(t, queue, result.CommandID)
	assert.Equal(t, completedStatusMessage, completed.StatusMessage)

	time.Sleep(3 * monitorInterval)
	afterMonitorStopped, exists := snapshotCommand(queue, result.CommandID)
	require.True(t, exists)
	assert.Equal(t, completed, afterMonitorStopped)
}

func TestQueue_PollAndWorkerUpdatesAreSafeWhenConcurrent(t *testing.T) {
	queue, evaluator := newTestQueue(t, false, defaultMonitorInterval)

	for i := 0; i < 25; i++ {
		_, err := queue.Enqueue("command", "")
		require.NoError(t, err)
	}

	var pollingGroup sync.WaitGroup
	for i := 0; i < 4; i++ {
		pollingGroup.Add(1)
		go func() {
			defer pollingGroup.Done()
			for j := 0; j < 50; j++ {
				queue.Poll()
			}
		}()
	}
	pollingGroup.Wait()

	eventually(t, func() bool {
		return evaluator.CallCount() == 25
	})
}

func TestQueue_ShutdownFunctionStopsBlockedWorker(t *testing.T) {
	evaluator := newTestEvaluator(true)
	lifecycleSignaler := &testLifecycleSignaler{}
	queue := newQueue(
		testLoggerFactory{},
		&testGlobalMATLAB{client: testMATLABSessionClient{}},
		evaluator,
		lifecycleSignaler,
		defaultMonitorInterval,
	)
	t.Cleanup(func() {
		require.NoError(t, queue.Shutdown())
	})

	_, err := queue.Enqueue("longRunningCommand", "")
	require.NoError(t, err)
	waitForEvaluation(t, evaluator, "longRunningCommand")

	require.NotNil(t, lifecycleSignaler.shutdownFunction)
	require.NoError(t, lifecycleSignaler.shutdownFunction())
}

func newTestQueue(t *testing.T, evaluatorBlocks bool, monitorInterval time.Duration) (*Queue, *testEvaluator) {
	t.Helper()

	evaluator := newTestEvaluator(evaluatorBlocks)
	lifecycleSignaler := &testLifecycleSignaler{}
	queue := newQueue(
		testLoggerFactory{},
		&testGlobalMATLAB{client: testMATLABSessionClient{}},
		evaluator,
		lifecycleSignaler,
		monitorInterval,
	)
	t.Cleanup(func() {
		require.NoError(t, queue.Shutdown())
	})

	return queue, evaluator
}

func waitForEvaluation(t *testing.T, evaluator *testEvaluator, expectedCode string) {
	t.Helper()

	select {
	case request := <-evaluator.started:
		assert.Equal(t, expectedCode, request.Code)
		assert.False(t, request.CaptureOutput)
	case <-time.After(testWaitTimeout):
		t.Fatalf("timed out waiting for evaluation of %q", expectedCode)
	}
}

func assertNoEvaluation(t *testing.T, evaluator *testEvaluator) {
	t.Helper()

	select {
	case request := <-evaluator.started:
		t.Fatalf("unexpected evaluation of %q", request.Code)
	case <-time.After(25 * time.Millisecond):
	}
}

func waitForTerminalSnapshot(t *testing.T, queue *Queue, commandID string) Command {
	t.Helper()

	var command Command
	eventually(t, func() bool {
		var exists bool
		command, exists = snapshotCommand(queue, commandID)
		return exists && isTerminal(command.Status)
	})
	return command
}

func snapshotCommand(queue *Queue, commandID string) (Command, bool) {
	queue.mutex.Lock()
	defer queue.mutex.Unlock()

	record, exists := queue.commands[commandID]
	if !exists {
		return Command{}, false
	}

	return record.command, true
}

func eventually(t *testing.T, condition func() bool) {
	t.Helper()

	deadline := time.NewTimer(testWaitTimeout)
	defer deadline.Stop()

	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()

	for {
		if condition() {
			return
		}

		select {
		case <-deadline.C:
			t.Fatal("condition did not become true before timeout")
		case <-ticker.C:
		}
	}
}

type testLoggerFactory struct{}

func (testLoggerFactory) GetGlobalLogger() (entities.Logger, messages.Error) {
	return testLogger{}, nil
}

type testLogger struct{}

func (testLogger) Debug(string) {}
func (testLogger) Info(string)  {}
func (testLogger) Warn(string)  {}
func (testLogger) Error(string) {}
func (testLogger) With(string, any) entities.Logger {
	return testLogger{}
}
func (testLogger) WithError(error) entities.Logger {
	return testLogger{}
}

type testLifecycleSignaler struct {
	shutdownFunction func() error
}

func (s *testLifecycleSignaler) AddShutdownFunction(shutdownFunction func() error) {
	s.shutdownFunction = shutdownFunction
}

type testGlobalMATLAB struct {
	client entities.MATLABSessionClient
}

func (m *testGlobalMATLAB) Client(context.Context, entities.Logger) (entities.MATLABSessionClient, error) {
	return m.client, nil
}

type testMATLABSessionClient struct{}

func (testMATLABSessionClient) Eval(context.Context, entities.Logger, entities.EvalRequest) (entities.EvalResponse, error) {
	return entities.EvalResponse{}, nil
}

func (testMATLABSessionClient) EvalWithCapture(context.Context, entities.Logger, entities.EvalRequest) (entities.EvalResponse, error) {
	return entities.EvalResponse{}, nil
}

func (testMATLABSessionClient) FEval(context.Context, entities.Logger, entities.FEvalRequest) (entities.FEvalResponse, error) {
	return entities.FEvalResponse{}, nil
}

func (testMATLABSessionClient) Ping(context.Context, entities.Logger) entities.PingResponse {
	return entities.PingResponse{}
}

type testEvaluator struct {
	mutex sync.Mutex

	started chan evalmatlabcode.Args
	release chan struct{}
	block   bool

	responses map[string]entities.EvalResponse
	errors    map[string]error
	codes     []string

	currentCalls         int
	maximumConcurrent    int
	completedEvaluations int
}

func newTestEvaluator(block bool) *testEvaluator {
	return &testEvaluator{
		started:   make(chan evalmatlabcode.Args, 100),
		release:   make(chan struct{}, 100),
		block:     block,
		responses: make(map[string]entities.EvalResponse),
		errors:    make(map[string]error),
	}
}

func (e *testEvaluator) Execute(ctx context.Context, _ entities.Logger, _ entities.MATLABSessionClient, request evalmatlabcode.Args) (entities.EvalResponse, error) {
	e.mutex.Lock()
	e.codes = append(e.codes, request.Code)
	e.currentCalls++
	if e.currentCalls > e.maximumConcurrent {
		e.maximumConcurrent = e.currentCalls
	}
	e.mutex.Unlock()

	e.started <- request

	if e.block {
		select {
		case <-e.release:
		case <-ctx.Done():
			e.finishEvaluation()
			return entities.EvalResponse{}, ctx.Err()
		}
	}

	e.mutex.Lock()
	response := e.responses[request.Code]
	err := e.errors[request.Code]
	e.mutex.Unlock()

	e.finishEvaluation()
	return response, err
}

func (e *testEvaluator) finishEvaluation() {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	e.currentCalls--
	e.completedEvaluations++
}

func (e *testEvaluator) Codes() []string {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	return append([]string(nil), e.codes...)
}

func (e *testEvaluator) MaximumConcurrentCalls() int {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	return e.maximumConcurrent
}

func (e *testEvaluator) CallCount() int {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	return e.completedEvaluations
}
