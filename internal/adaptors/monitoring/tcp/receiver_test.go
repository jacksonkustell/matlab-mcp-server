// Copyright 2026 The MathWorks, Inc.

package tcp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/matlab/matlab-mcp-server/internal/entities"
	"github.com/matlab/matlab-mcp-server/internal/messages"
	"github.com/matlab/matlab-mcp-server/internal/usecases/matlabcommandqueue"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const receiverTestTimeout = 2 * time.Second

func TestReceiver_DeliversValidMonitoringEvent(t *testing.T) {
	receiver, _ := newTestReceiver(t)
	events := make(chan matlabcommandqueue.MonitoringEvent, 1)
	ensureReceiverRegistered(t, receiver, 1)

	subscription, err := receiver.Subscribe(func(event matlabcommandqueue.MonitoringEvent) {
		events <- event
	}, nil)
	require.NoError(t, err)
	t.Cleanup(subscription.Unsubscribe)

	connection := dialReceiver(t, receiver)
	defer func() {
		_ = connection.Close()
	}()

	_, err = fmt.Fprintln(connection, `{"timestamp":"2026-09-14T12:34:56.789-04:00","message":"running step 1"}`)
	require.NoError(t, err)

	select {
	case event := <-events:
		assert.Equal(t, "2026-09-14T12:34:56.789-04:00", event.Timestamp)
		assert.Equal(t, "running step 1", event.Message)
	case <-time.After(receiverTestTimeout):
		t.Fatal("timed out waiting for a monitoring event")
	}
}

func TestReceiver_DiscardsMalformedMessagesAndContinuesReading(t *testing.T) {
	receiver, logger := newTestReceiver(t)
	events := make(chan matlabcommandqueue.MonitoringEvent, 1)
	ensureReceiverRegistered(t, receiver, 1)

	subscription, err := receiver.Subscribe(func(event matlabcommandqueue.MonitoringEvent) {
		events <- event
	}, nil)
	require.NoError(t, err)
	t.Cleanup(subscription.Unsubscribe)

	connection := dialReceiver(t, receiver)
	defer func() {
		_ = connection.Close()
	}()

	_, err = fmt.Fprintln(connection, `not JSON`)
	require.NoError(t, err)
	_, err = fmt.Fprintln(connection, `{"timestamp":"not-a-timestamp","message":"invalid"}`)
	require.NoError(t, err)
	_, err = fmt.Fprintln(connection, `{"timestamp":"2026-09-14T12:34:56Z","message":"valid"}`)
	require.NoError(t, err)

	select {
	case event := <-events:
		assert.Equal(t, "valid", event.Message)
	case <-time.After(receiverTestTimeout):
		t.Fatal("timed out waiting for the valid monitoring event")
	}

	assert.GreaterOrEqual(t, logger.WarningCount(), 2)
}

func TestReceiver_DropsMessagesAfterUnsubscribe(t *testing.T) {
	receiver, _ := newTestReceiver(t)
	events := make(chan matlabcommandqueue.MonitoringEvent, 1)
	ensureReceiverRegistered(t, receiver, 1)

	subscription, err := receiver.Subscribe(func(event matlabcommandqueue.MonitoringEvent) {
		events <- event
	}, nil)
	require.NoError(t, err)
	subscription.Unsubscribe()

	connection := dialReceiver(t, receiver)
	defer func() {
		_ = connection.Close()
	}()

	_, err = fmt.Fprintln(connection, `{"timestamp":"2026-09-14T12:34:56Z","message":"late message"}`)
	require.NoError(t, err)

	select {
	case event := <-events:
		t.Fatalf("received a message after unsubscribe: %#v", event)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestReceiver_UnsubscribeWaitsForInFlightEventCallback(t *testing.T) {
	receiver, _ := newTestReceiver(t)
	callbackStarted := make(chan struct{})
	callbackRelease := make(chan struct{})
	var releaseOnce sync.Once
	releaseCallback := func() {
		releaseOnce.Do(func() {
			close(callbackRelease)
		})
	}
	ensureReceiverRegistered(t, receiver, 1)

	subscription, err := receiver.Subscribe(func(matlabcommandqueue.MonitoringEvent) {
		close(callbackStarted)
		<-callbackRelease
	}, nil)
	require.NoError(t, err)
	t.Cleanup(subscription.Unsubscribe)
	t.Cleanup(releaseCallback)

	connection := dialReceiver(t, receiver)
	defer func() {
		_ = connection.Close()
	}()

	_, err = fmt.Fprintln(connection, `{"timestamp":"2026-09-14T12:34:56Z","message":"blocking callback"}`)
	require.NoError(t, err)

	select {
	case <-callbackStarted:
	case <-time.After(receiverTestTimeout):
		t.Fatal("timed out waiting for the monitoring callback to start")
	}

	unsubscribed := make(chan struct{})
	go func() {
		subscription.Unsubscribe()
		close(unsubscribed)
	}()

	select {
	case <-unsubscribed:
		t.Fatal("unsubscribe returned before the active callback finished")
	case <-time.After(50 * time.Millisecond):
	}

	releaseCallback()

	select {
	case <-unsubscribed:
	case <-time.After(receiverTestTimeout):
		t.Fatal("timed out waiting for unsubscribe to join the callback")
	}
}

func TestReceiver_ReportsReadFailuresToTheActiveSubscription(t *testing.T) {
	receiver, logger := newTestReceiver(t)
	monitoringErrors := make(chan error, 1)
	ensureReceiverRegistered(t, receiver, 1)

	subscription, err := receiver.Subscribe(nil, func(monitoringErr error) {
		monitoringErrors <- monitoringErr
	})
	require.NoError(t, err)
	t.Cleanup(subscription.Unsubscribe)

	connection := dialReceiver(t, receiver)
	defer func() {
		_ = connection.Close()
	}()

	_, err = connection.Write(bytes.Repeat([]byte("x"), maxMonitoringMessageBytes+1))
	require.NoError(t, err)

	select {
	case monitoringErr := <-monitoringErrors:
		require.ErrorContains(t, monitoringErr, "read monitoring message")
	case <-time.After(receiverTestTimeout):
		t.Fatal("timed out waiting for a monitoring read failure")
	}

	assert.GreaterOrEqual(t, logger.WarningCount(), 1)
}

func TestReceiver_ShutdownClosesActiveConnections(t *testing.T) {
	receiver, _ := newTestReceiver(t)
	client := ensureReceiverRegistered(t, receiver, 1)

	subscription, err := receiver.Subscribe(nil, nil)
	require.NoError(t, err)
	t.Cleanup(subscription.Unsubscribe)

	connection := dialReceiver(t, receiver)
	defer func() {
		_ = connection.Close()
	}()
	eventually(t, func() bool {
		receiver.mutex.Lock()
		defer receiver.mutex.Unlock()
		return len(receiver.connections) == 1
	})

	require.NoError(t, receiver.Shutdown())

	require.NoError(t, connection.SetReadDeadline(time.Now().Add(receiverTestTimeout)))
	buffer := make([]byte, 1)
	_, err = connection.Read(buffer)
	require.Error(t, err)
	assertUnregistrationRequest(t, receiver, client.FEvalRequests())
}

func TestReceiver_RegistersLifecycleCleanup(t *testing.T) {
	lifecycleSignaler := &testLifecycleSignaler{}
	logger := newRecordingLogger()
	receiver := New(testLoggerFactory{logger: logger}, lifecycleSignaler)
	receiver.address = "127.0.0.1:0"
	t.Cleanup(func() {
		require.NoError(t, receiver.Shutdown())
	})
	client := ensureReceiverRegistered(t, receiver, 1)

	subscription, err := receiver.Subscribe(nil, nil)
	require.NoError(t, err)
	t.Cleanup(subscription.Unsubscribe)
	require.NotNil(t, lifecycleSignaler.shutdownFunction)
	address := receiverAddress(t, receiver)

	require.NoError(t, lifecycleSignaler.shutdownFunction())

	_, err = receiver.Subscribe(nil, nil)
	require.ErrorIs(t, err, errReceiverStopped)

	connection, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
	require.Error(t, err)
	if connection != nil {
		_ = connection.Close()
	}
	assertUnregistrationRequest(t, receiver, client.FEvalRequests())
}

func TestReceiver_ReturnsBindFailuresWithoutCreatingASubscription(t *testing.T) {
	expectedError := errors.New("address already in use")
	logger := newRecordingLogger()
	receiver := newReceiver(
		testLoggerFactory{logger: logger},
		defaultAddress,
		func(string, string) (net.Listener, error) {
			return nil, expectedError
		},
	)
	t.Cleanup(func() {
		require.NoError(t, receiver.Shutdown())
	})

	client := &recordingMATLABSessionClient{}
	err := receiver.EnsureRegistered(t.Context(), newRecordingLogger(), client, 1)

	require.ErrorIs(t, err, expectedError)
	assert.Empty(t, client.FEvalRequests())
	assert.Equal(t, 1, logger.WarningCount())
}

func TestReceiver_UsesIndependentDynamicListeners(t *testing.T) {
	receiver1, _ := newTestReceiver(t)
	receiver2, _ := newTestReceiver(t)

	client1 := ensureReceiverRegistered(t, receiver1, 1)
	client2 := ensureReceiverRegistered(t, receiver2, 1)

	assert.NotEqual(t, receiverAddress(t, receiver1), receiverAddress(t, receiver2))
	assert.NotEqual(t, receiver1.registrationID, receiver2.registrationID)
	assertRegistrationRequest(t, receiver1, client1.FEvalRequests())
	assertRegistrationRequest(t, receiver2, client2.FEvalRequests())
}

func TestReceiver_RegistersOnlyOncePerSessionAndReregistersForANewSession(t *testing.T) {
	receiver, _ := newTestReceiver(t)
	client := &recordingMATLABSessionClient{}
	logger := newRecordingLogger()

	require.NoError(t, receiver.EnsureRegistered(t.Context(), logger, client, 1))
	require.NoError(t, receiver.EnsureRegistered(t.Context(), logger, client, 1))
	require.NoError(t, receiver.EnsureRegistered(t.Context(), logger, client, 2))

	requests := client.FEvalRequests()
	require.Len(t, requests, 2)
	assertRegistrationRequest(t, receiver, requests[:1])
	assertRegistrationRequest(t, receiver, requests[1:])
}

func TestReceiver_RetriesFailedRegistration(t *testing.T) {
	receiver, _ := newTestReceiver(t)
	expectedError := errors.New("MATLAB registration failed")
	client := &recordingMATLABSessionClient{
		fevalErrors: []error{expectedError, nil},
	}
	logger := newRecordingLogger()

	err := receiver.EnsureRegistered(t.Context(), logger, client, 1)
	require.ErrorIs(t, err, expectedError)
	require.NoError(t, receiver.EnsureRegistered(t.Context(), logger, client, 1))

	requests := client.FEvalRequests()
	require.Len(t, requests, 2)
	assertRegistrationRequest(t, receiver, requests[:1])
	assertRegistrationRequest(t, receiver, requests[1:])
}

func TestReceiver_SubscribeOnlyAttachesAndDetachesCallbacks(t *testing.T) {
	receiver, _ := newTestReceiver(t)
	client := ensureReceiverRegistered(t, receiver, 1)

	firstSubscription, err := receiver.Subscribe(nil, nil)
	require.NoError(t, err)
	firstSubscription.Unsubscribe()

	secondSubscription, err := receiver.Subscribe(nil, nil)
	require.NoError(t, err)
	secondSubscription.Unsubscribe()

	assertRegistrationRequest(t, receiver, client.FEvalRequests())
}

func newTestReceiver(t *testing.T) (*Receiver, *recordingLogger) {
	t.Helper()

	logger := newRecordingLogger()
	receiver := newReceiver(testLoggerFactory{logger: logger}, "127.0.0.1:0", net.Listen)
	t.Cleanup(func() {
		require.NoError(t, receiver.Shutdown())
	})

	return receiver, logger
}

func ensureReceiverRegistered(t *testing.T, receiver *Receiver, sessionID entities.SessionID) *recordingMATLABSessionClient {
	t.Helper()

	client := &recordingMATLABSessionClient{}
	require.NoError(t, receiver.EnsureRegistered(t.Context(), newRecordingLogger(), client, sessionID))
	assertRegistrationRequest(t, receiver, client.FEvalRequests())

	return client
}

func assertRegistrationRequest(t *testing.T, receiver *Receiver, requests []entities.FEvalRequest) {
	t.Helper()

	require.NotEmpty(t, requests)
	port, err := listenerPort(receiver.listener)
	require.NoError(t, err)

	request := requests[0]
	assert.Equal(t, registerProgressEndpointFunction, request.Function)
	assert.Equal(t, []string{
		receiver.registrationID,
		loopbackHost,
		strconv.Itoa(port),
	}, request.Arguments)
	assert.Zero(t, request.NumOutputs)
}

func assertUnregistrationRequest(t *testing.T, receiver *Receiver, requests []entities.FEvalRequest) {
	t.Helper()

	require.NotEmpty(t, requests)

	request := requests[len(requests)-1]
	assert.Equal(t, unregisterProgressEndpointFunction, request.Function)
	assert.Equal(t, []string{receiver.registrationID}, request.Arguments)
	assert.Zero(t, request.NumOutputs)
}

func dialReceiver(t *testing.T, receiver *Receiver) net.Conn {
	t.Helper()

	connection, err := net.DialTimeout("tcp", receiverAddress(t, receiver), receiverTestTimeout)
	require.NoError(t, err)
	return connection
}

func receiverAddress(t *testing.T, receiver *Receiver) string {
	t.Helper()

	receiver.mutex.Lock()
	defer receiver.mutex.Unlock()
	require.NotNil(t, receiver.listener)

	return receiver.listener.Addr().String()
}

func eventually(t *testing.T, condition func() bool) {
	t.Helper()

	deadline := time.NewTimer(receiverTestTimeout)
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

type testLifecycleSignaler struct {
	shutdownFunction func() error
}

func (s *testLifecycleSignaler) AddShutdownFunction(shutdownFunction func() error) {
	s.shutdownFunction = shutdownFunction
}

type testLoggerFactory struct {
	logger entities.Logger
}

func (f testLoggerFactory) GetGlobalLogger() (entities.Logger, messages.Error) {
	return f.logger, nil
}

type recordingLogger struct {
	state *recordingLoggerState
}

type recordingLoggerState struct {
	mutex    sync.Mutex
	warnings []string
}

func newRecordingLogger() *recordingLogger {
	return &recordingLogger{
		state: &recordingLoggerState{},
	}
}

func (l *recordingLogger) Debug(string) {}
func (l *recordingLogger) Info(string)  {}
func (l *recordingLogger) Error(string) {}

func (l *recordingLogger) Warn(message string) {
	l.state.mutex.Lock()
	defer l.state.mutex.Unlock()

	l.state.warnings = append(l.state.warnings, message)
}

func (l *recordingLogger) With(string, any) entities.Logger {
	return l
}

func (l *recordingLogger) WithError(error) entities.Logger {
	return l
}

func (l *recordingLogger) WarningCount() int {
	l.state.mutex.Lock()
	defer l.state.mutex.Unlock()

	return len(l.state.warnings)
}

type recordingMATLABSessionClient struct {
	mutex sync.Mutex

	fevalRequests []entities.FEvalRequest
	fevalErrors   []error
}

func (c *recordingMATLABSessionClient) Eval(context.Context, entities.Logger, entities.EvalRequest) (entities.EvalResponse, error) {
	return entities.EvalResponse{}, nil
}

func (c *recordingMATLABSessionClient) EvalWithCapture(context.Context, entities.Logger, entities.EvalRequest) (entities.EvalResponse, error) {
	return entities.EvalResponse{}, nil
}

func (c *recordingMATLABSessionClient) FEval(_ context.Context, _ entities.Logger, request entities.FEvalRequest) (entities.FEvalResponse, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.fevalRequests = append(c.fevalRequests, request)
	if len(c.fevalErrors) == 0 {
		return entities.FEvalResponse{}, nil
	}

	err := c.fevalErrors[0]
	c.fevalErrors = c.fevalErrors[1:]
	return entities.FEvalResponse{}, err
}

func (c *recordingMATLABSessionClient) Ping(context.Context, entities.Logger) entities.PingResponse {
	return entities.PingResponse{}
}

func (c *recordingMATLABSessionClient) FEvalRequests() []entities.FEvalRequest {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	return append([]entities.FEvalRequest(nil), c.fevalRequests...)
}
