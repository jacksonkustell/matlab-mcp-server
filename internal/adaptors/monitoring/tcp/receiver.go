// Copyright 2026 The MathWorks, Inc.

// Package tcp receives local TCP monitoring messages.
package tcp

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/matlab/matlab-mcp-server/internal/entities"
	"github.com/matlab/matlab-mcp-server/internal/messages"
	"github.com/matlab/matlab-mcp-server/internal/usecases/matlabcommandqueue"
)

const (
	defaultAddress            = "127.0.0.1:50506"
	maxMonitoringMessageBytes = 1024 * 1024
)

var (
	errReceiverStopped   = errors.New("monitoring receiver is shut down")
	errAlreadySubscribed = errors.New("monitoring receiver already has an active subscription")
)

type LoggerFactory interface {
	GetGlobalLogger() (entities.Logger, messages.Error)
}

type LifecycleSignaler interface {
	AddShutdownFunction(shutdownFunction func() error)
}

type listenerFactory func(network string, address string) (net.Listener, error)

// Receiver receives newline-delimited JSON monitoring messages over local TCP.
type Receiver struct {
	loggerFactory LoggerFactory
	address       string
	listen        listenerFactory

	mutex        sync.Mutex
	listener     net.Listener
	connections  map[net.Conn]struct{}
	subscription *subscription
	stopped      bool

	workers      sync.WaitGroup
	shutdownOnce sync.Once
	shutdownErr  error
}

// New creates a lazily-started TCP monitoring receiver and registers its shutdown hook.
func New(loggerFactory LoggerFactory, lifecycleSignaler LifecycleSignaler) *Receiver {
	receiver := newReceiver(loggerFactory, defaultAddress, net.Listen)
	lifecycleSignaler.AddShutdownFunction(receiver.Shutdown)

	return receiver
}

func newReceiver(loggerFactory LoggerFactory, address string, listen listenerFactory) *Receiver {
	return &Receiver{
		loggerFactory: loggerFactory,
		address:       address,
		listen:        listen,
		connections:   make(map[net.Conn]struct{}),
	}
}

// Subscribe begins event delivery, starting the local listener on the first subscription.
func (r *Receiver) Subscribe(
	onEvent func(matlabcommandqueue.MonitoringEvent),
	onError func(error),
) (matlabcommandqueue.MonitoringSubscription, error) {
	r.mutex.Lock()
	if r.stopped {
		r.mutex.Unlock()
		return nil, errReceiverStopped
	}

	if r.subscription != nil {
		r.mutex.Unlock()
		return nil, errAlreadySubscribed
	}

	if r.listener == nil {
		listener, err := r.listen("tcp", r.address)
		if err != nil {
			r.mutex.Unlock()

			monitoringErr := fmt.Errorf("listen for monitoring messages on %s: %w", r.address, err)
			r.logWarning("Monitoring is unavailable.", monitoringErr)
			return nil, monitoringErr
		}

		r.listener = listener
		r.workers.Add(1)
		go r.acceptConnections(listener)
	}

	activeSubscription := &subscription{
		receiver: r,
		onEvent:  onEvent,
		onError:  onError,
	}
	r.subscription = activeSubscription
	r.mutex.Unlock()

	return activeSubscription, nil
}

// Shutdown stops accepting and reading connections, then waits for all callbacks to finish.
func (r *Receiver) Shutdown() error {
	r.shutdownOnce.Do(func() {
		r.mutex.Lock()
		r.stopped = true

		listener := r.listener
		r.listener = nil

		connections := make([]net.Conn, 0, len(r.connections))
		for connection := range r.connections {
			connections = append(connections, connection)
		}

		activeSubscription := r.subscription
		r.subscription = nil
		r.mutex.Unlock()

		if listener != nil {
			if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
				r.shutdownErr = err
			}
		}

		for _, connection := range connections {
			_ = connection.Close()
		}

		if activeSubscription != nil {
			activeSubscription.callbacks.Wait()
		}
		r.workers.Wait()
	})

	return r.shutdownErr
}

func (r *Receiver) acceptConnections(listener net.Listener) {
	defer r.workers.Done()

	for {
		connection, err := listener.Accept()
		if err != nil {
			if r.isStopped() || errors.Is(err, net.ErrClosed) {
				return
			}

			r.reportReceiverFailure(fmt.Errorf("accept monitoring connection: %w", err))
			r.clearListener(listener)
			_ = listener.Close()
			return
		}

		r.mutex.Lock()
		if r.stopped {
			r.mutex.Unlock()
			_ = connection.Close()
			return
		}

		r.connections[connection] = struct{}{}
		r.workers.Add(1)
		r.mutex.Unlock()

		go r.readConnection(connection)
	}
}

func (r *Receiver) readConnection(connection net.Conn) {
	defer r.workers.Done()
	defer func() {
		r.mutex.Lock()
		delete(r.connections, connection)
		r.mutex.Unlock()
		_ = connection.Close()
	}()

	scanner := bufio.NewScanner(connection)
	scanner.Buffer(make([]byte, 0, maxMonitoringMessageBytes), maxMonitoringMessageBytes)

	for scanner.Scan() {
		event, err := parseMonitoringEvent(scanner.Bytes())
		if err != nil {
			r.logWarning("Discarded malformed monitoring message.", err)
			continue
		}

		r.dispatchEvent(event)
	}

	if err := scanner.Err(); err != nil && !r.isStopped() {
		r.reportReceiverFailure(fmt.Errorf("read monitoring message: %w", err))
	}
}

func (r *Receiver) dispatchEvent(event matlabcommandqueue.MonitoringEvent) {
	r.withActiveSubscription(func(activeSubscription *subscription) {
		if activeSubscription.onEvent != nil {
			activeSubscription.onEvent(event)
		}
	})
}

func (r *Receiver) reportReceiverFailure(err error) {
	r.logWarning("Monitoring is unavailable.", err)

	r.withActiveSubscription(func(activeSubscription *subscription) {
		if activeSubscription.onError != nil {
			activeSubscription.onError(err)
		}
	})
}

func (r *Receiver) withActiveSubscription(callback func(*subscription)) {
	r.mutex.Lock()
	activeSubscription := r.subscription
	if activeSubscription != nil {
		activeSubscription.callbacks.Add(1)
	}
	r.mutex.Unlock()

	if activeSubscription == nil {
		return
	}

	defer activeSubscription.callbacks.Done()
	callback(activeSubscription)
}

func (r *Receiver) clearListener(listener net.Listener) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	if r.listener == listener {
		r.listener = nil
	}
}

func (r *Receiver) isStopped() bool {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	return r.stopped
}

func (r *Receiver) logWarning(message string, err error) {
	if r.loggerFactory == nil {
		return
	}

	logger, loggerErr := r.loggerFactory.GetGlobalLogger()
	if loggerErr != nil {
		return
	}

	logger.WithError(err).Warn(message)
}

func parseMonitoringEvent(data []byte) (matlabcommandqueue.MonitoringEvent, error) {
	var message monitoringMessage
	if err := json.Unmarshal(data, &message); err != nil {
		return matlabcommandqueue.MonitoringEvent{}, fmt.Errorf("invalid JSON: %w", err)
	}

	if strings.TrimSpace(message.Message) == "" {
		return matlabcommandqueue.MonitoringEvent{}, errors.New("message must not be blank")
	}

	if strings.TrimSpace(message.Timestamp) == "" {
		return matlabcommandqueue.MonitoringEvent{}, errors.New("timestamp must not be blank")
	}

	if _, err := time.Parse(time.RFC3339Nano, message.Timestamp); err != nil {
		return matlabcommandqueue.MonitoringEvent{}, fmt.Errorf("timestamp must use RFC 3339 format: %w", err)
	}

	return matlabcommandqueue.MonitoringEvent{
		Message:   message.Message,
		Timestamp: message.Timestamp,
	}, nil
}

type monitoringMessage struct {
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
}

type subscription struct {
	receiver *Receiver
	onEvent  func(matlabcommandqueue.MonitoringEvent)
	onError  func(error)

	callbacks sync.WaitGroup
	once      sync.Once
}

func (s *subscription) Unsubscribe() {
	s.once.Do(func() {
		s.receiver.mutex.Lock()
		if s.receiver.subscription == s {
			s.receiver.subscription = nil
		}
		s.receiver.mutex.Unlock()

		s.callbacks.Wait()
	})
}
