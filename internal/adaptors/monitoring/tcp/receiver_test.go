// Copyright 2026 The MathWorks, Inc.

package tcp

import (
	"bytes"
	"errors"
	"fmt"
	"net"
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
}

func TestReceiver_RegistersLifecycleCleanup(t *testing.T) {
	lifecycleSignaler := &testLifecycleSignaler{}
	logger := newRecordingLogger()
	receiver := New(testLoggerFactory{logger: logger}, lifecycleSignaler)
	receiver.address = "127.0.0.1:0"
	t.Cleanup(func() {
		require.NoError(t, receiver.Shutdown())
	})

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

	subscription, err := receiver.Subscribe(nil, nil)

	require.Nil(t, subscription)
	require.ErrorIs(t, err, expectedError)
	assert.Equal(t, 1, logger.WarningCount())
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
