package servicemgr

import (
	"sync"
	"time"

	service "github.com/shabbyrobe/go-service"
)

var (
	listener *listenerDispatcher
	runner   service.Runner
	lock     sync.RWMutex
)

func init() {
	Reset()
}

func Runner() service.Runner {
	lock.RLock()
	r := runner
	lock.RUnlock()
	return r
}

// Reset is used to replace the global runner with a fresh one. If you do not ensure
// all services are halted before calling Reset(), you will leak reasources.
func Reset() {
	lock.Lock()
	listener = newListenerDispatcher()
	runner = service.NewRunner(listener)
	lock.Unlock()
}

func State(s service.Service) service.State {
	lock.RLock()
	defer lock.RUnlock()

	return runner.State(s)
}

// StartWait starts a service in the global runner.
//
// You may also provide an optional Listener (which may be the service itself),
// which will allow the caller to respond to errors and service ends.
//
// Listeners can be used multiple times when starting different services.
func StartWait(s service.Service, l service.Listener, timeout time.Duration) error {
	lock.RLock()
	defer lock.RUnlock()
	if l != nil {
		listener.Add(s, l)
	}
	return runner.StartWait(s, timeout)
}

// Start starts a service in the global runner.
//
// You may also provide an optional Listener (which may be the service itself),
// which will allow the caller to respond to errors and service ends.
//
// Listeners can be used multiple times when starting different services.
func Start(s service.Service, l service.Listener) error {
	lock.RLock()
	defer lock.RUnlock()
	if l != nil {
		listener.Add(s, l)
	}
	return runner.Start(s)
}

func Halt(s service.Service, timeout time.Duration) error {
	lock.RLock()
	defer lock.RUnlock()

	return runner.Halt(s, timeout)
}

func HaltAll(timeout time.Duration) error {
	lock.RLock()
	defer lock.RUnlock()

	return runner.HaltAll(timeout)
}

func Services(state service.State) []service.Service {
	lock.RLock()
	defer lock.RUnlock()

	return runner.Services(state)
}

func Unregister(s service.Service) error {
	lock.RLock()
	defer lock.RUnlock()

	listener.Remove(s)
	return runner.Unregister(s)
}

func WhenReady(timeout time.Duration) <-chan error {
	lock.RLock()
	defer lock.RUnlock()

	return runner.WhenReady(timeout)
}

func EnsureHalt(s service.Service, timeout time.Duration) error {
	return service.EnsureHalt(Runner(), s, timeout)
}

func MustEnsureHalt(s service.Service, timeout time.Duration) {
	service.MustEnsureHalt(Runner(), s, timeout)
}

func getListener() *listenerDispatcher {
	lock.RLock()
	l := listener
	lock.RUnlock()
	return l
}