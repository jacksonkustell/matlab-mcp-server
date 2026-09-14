// Copyright 2026 The MathWorks, Inc.

// Package tcp receives local TCP monitoring messages.
package tcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/matlab/matlab-mcp-server/internal/entities"
	"github.com/matlab/matlab-mcp-server/internal/messages"
	"github.com/matlab/matlab-mcp-server/internal/usecases/matlabcommandqueue"
)

const (
	loopbackHost              = "127.0.0.1"
	defaultAddress            = loopbackHost + ":0"
	maxMonitoringMessageBytes = 1024 * 1024
	registrationTimeout       = time.Second

	registerProgressEndpointFunction   = "matlab_mcp.registerMCPProgressEndpoint"
	unregisterProgressEndpointFunction = "matlab_mcp.unregisterMCPProgressEndpoint"
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

	registrationID string

	mutex                sync.Mutex
	listener             net.Listener
	connections          map[net.Conn]struct{}
	subscription         *subscription
	stopped              bool
	registered           bool
	registeredSessionID  entities.SessionID
	registeredClient     entities.MATLABSessionClient
	registrationComplete chan struct{}

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
		loggerFactory:  loggerFactory,
		address:        address,
		listen:         listen,
		registrationID: uuid.NewString(),
		connections:    make(map[net.Conn]struct{}),
	}
}

// EnsureRegistered starts the local listener and registers it with the supplied
// MATLAB session if that session has not already been registered.
func (r *Receiver) EnsureRegistered(
	ctx context.Context,
	logger entities.Logger,
	client entities.MATLABSessionClient,
	sessionID entities.SessionID,
) error {
	for {
		r.mutex.Lock()
		if r.stopped {
			r.mutex.Unlock()
			return errReceiverStopped
		}

		listener, err := r.startListenerLocked()
		if err != nil {
			r.mutex.Unlock()

			monitoringErr := fmt.Errorf("listen for monitoring messages on %s: %w", r.address, err)
			r.logWarning("Monitoring is unavailable.", monitoringErr)
			return monitoringErr
		}

		if r.registered && r.registeredSessionID == sessionID {
			r.mutex.Unlock()
			return nil
		}

		if r.registrationComplete != nil {
			registrationComplete := r.registrationComplete
			r.mutex.Unlock()

			select {
			case <-registrationComplete:
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		registrationComplete := make(chan struct{})
		r.registrationComplete = registrationComplete
		port, err := listenerPort(listener)
		if err != nil {
			r.registrationComplete = nil
			close(registrationComplete)
			r.mutex.Unlock()
			return err
		}
		r.mutex.Unlock()

		_, err = client.FEval(ctx, logger, entities.FEvalRequest{
			Function: registerProgressEndpointFunction,
			Arguments: []string{
				r.registrationID,
				loopbackHost,
				strconv.Itoa(port),
			},
			NumOutputs: 0,
		})
		if err != nil {
			err = fmt.Errorf("register monitoring endpoint: %w", err)
		}

		r.mutex.Lock()
		if r.registrationComplete == registrationComplete {
			r.registrationComplete = nil
			close(registrationComplete)
		}
		if err == nil && r.listener == listener {
			r.registered = true
			r.registeredSessionID = sessionID
			r.registeredClient = client
		}
		r.mutex.Unlock()

		if err != nil {
			r.logWarning("Monitoring is unavailable.", err)
		}
		return err
	}
}

// Subscribe begins event delivery for the active command. The local listener
// must already have been prepared by EnsureRegistered.
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
		registrationComplete := r.registrationComplete
		r.mutex.Unlock()

		if registrationComplete != nil {
			timer := time.NewTimer(registrationTimeout)
			select {
			case <-registrationComplete:
			case <-timer.C:
			}
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}

		r.mutex.Lock()

		listener := r.listener
		r.listener = nil
		registeredClient := r.registeredClient

		connections := make([]net.Conn, 0, len(r.connections))
		for connection := range r.connections {
			connections = append(connections, connection)
		}

		activeSubscription := r.subscription
		r.subscription = nil
		r.mutex.Unlock()

		r.unregisterEndpoint(registeredClient)

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
		r.registered = false
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

func (r *Receiver) startListenerLocked() (net.Listener, error) {
	if r.listener != nil {
		return r.listener, nil
	}

	listener, err := r.listen("tcp", r.address)
	if err != nil {
		return nil, err
	}

	r.listener = listener
	r.registered = false
	r.workers.Add(1)
	go r.acceptConnections(listener)

	return listener, nil
}

func (r *Receiver) unregisterEndpoint(client entities.MATLABSessionClient) {
	if client == nil || r.loggerFactory == nil {
		return
	}

	logger, loggerErr := r.loggerFactory.GetGlobalLogger()
	if loggerErr != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), registrationTimeout)
	defer cancel()

	_, err := client.FEval(ctx, logger, entities.FEvalRequest{
		Function:   unregisterProgressEndpointFunction,
		Arguments:  []string{r.registrationID},
		NumOutputs: 0,
	})
	if err != nil {
		logger.WithError(err).Warn("Failed to unregister monitoring endpoint.")
	}
}

func listenerPort(listener net.Listener) (int, error) {
	address, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("monitoring listener has unexpected address type %T", listener.Addr())
	}
	if address.Port <= 0 {
		return 0, fmt.Errorf("monitoring listener has invalid port %d", address.Port)
	}

	return address.Port, nil
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
