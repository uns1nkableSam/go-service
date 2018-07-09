package service

import (
	"context"
	"fmt"

	// "github.com/shabbyrobe/golib/synctools"
	"github.com/shabbyrobe/golib/synctools"
)

// Runner Starts, Halts and manages Services.
type Runner interface {
	// Start one or more services in this runner and wait until they are Ready.
	//
	// An optional context can be provided via ctx; this allows cancellation to
	// be declared outside the Runner. You may provide a nil Context.
	//
	Start(ctx context.Context, services ...*Service) error

	// Halt one or more services that have been started in this runner.
	//
	// An optional context can be provided via ctx; this allows cancellation to
	// be declared outside the Runner. You may provide a nil Context.
	//
	// If the context is cancelled before the service halts, you may have leaked
	// a goroutine; there is no way for a service lost in this manner to be
	// recovered using go-service, you will need to build in your own recovery
	// mechanisms if you want to handle this condition. In practice, a
	// cancelled 'halt' is probably a good time to panic(), but your specific
	// application may be able to tolerate some goroutine leaks until you can
	// fix the issue.
	Halt(ctx context.Context, services ...*Service) error

	// Shutdown halts all services started in this runner and prevents new ones
	// from being started.
	//
	// If any service fails to halt, err will contain an error for each service
	// that failed, accessible by calling service.Errors(err). n will contain
	// the number of services successfully halted.
	//
	// An optional context can be provided via ctx; this allows cancellation to
	// be declared outside the Runner. You may provide a nil Context, but this is
	// not recommended as your application may block indefinitely.
	//
	// It is safe to call Shutdown multiple times.
	Shutdown(ctx context.Context) (err error)

	// Enable resumes a Shutdown runner.
	Enable() error

	// Suspend prevents new services from being started in this Runner, but
	// does not shut down existing services.
	Suspend() error

	// FIXME:
	// RunnerState() RunnerState

	State(svc *Service) State

	// Services returns the list of services running at the time of the call.
	// time of the call. If StateQuery is provided, only the matching services
	// are returned.
	//
	// Pass limit to restrict the number of returned results. If limit is <= 0,
	// all matching services are returned.
	//
	// You can instruct States to allocate into an existing slice by passing
	// it in. You should replace it with the return value in case it needs
	// to grow.
	Services(state State, limit int, into []ServiceInfo) []ServiceInfo
}

type RunnerOption func(rn *runner)

func RunnerOnEnd(cb OnEnd) RunnerOption     { return func(rn *runner) { rn.onEnd = cb } }
func RunnerOnError(cb OnError) RunnerOption { return func(rn *runner) { rn.onError = cb } }
func RunnerOnState(cb OnState) RunnerOption { return func(rn *runner) { rn.onState = cb } }

type runner struct {
	// runner listeners MUST NOT be changed after runner is created, they are
	// accessed without a lock.
	onEnd   OnEnd
	onError OnError
	onState OnState

	nextID   uint64
	services map[*Service]*runnerService
	state    RunnerState
	// mu     sync.RWMutex
	mu synctools.LoggingRWMutex
}

var _ Runner = &runner{}

func NewRunner(opts ...RunnerOption) Runner {
	rn := &runner{
		services: make(map[*Service]*runnerService),
	}
	for _, o := range opts {
		o(rn)
	}
	return rn
}

func (rn *runner) Enable() error {
	rn.mu.Lock()
	rn.state = RunnerEnabled
	rn.mu.Unlock()
	return nil
}

func (rn *runner) Suspend() error {
	rn.mu.Lock()
	if rn.state != RunnerEnabled {
		rn.mu.Unlock()
		// FIXME: error that allows you to check if it's suspended or shut down:
		return fmt.Errorf("runner is not enabled")
	}
	rn.state = RunnerSuspended
	rn.mu.Unlock()
	return nil
}

func (rn *runner) Shutdown(ctx context.Context) (rerr error) {
	var signal Signal

	if err := func() error {
		rn.mu.Lock()
		defer rn.mu.Unlock()

		if rn.state != RunnerEnabled && rn.state != RunnerSuspended {
			// FIXME: error that allows you to check if it's suspended or shut down:
			return fmt.Errorf("runner is not enabled")
		}

		signal = NewMultiSignal(len(rn.services))

		rn.state = RunnerShutdown

		for _, rs := range rn.services {
			if err := rs.halting(signal); err != nil {
				panic(err)
			}
		}
		return nil

	}(); err != nil {
		return err
	}

	var ctxDone <-chan struct{}
	if ctx != nil {
		ctxDone = ctx.Done()
	}

	select {
	case err := <-signal.Waiter():
		return err

	case <-ctxDone:
		return ctx.Err()
	}
}

func (rn *runner) Start(ctx context.Context, services ...*Service) error {
	svcLen := len(services)
	if svcLen == 0 {
		return nil
	}

	rn.mu.Lock()
	if rn.state != RunnerEnabled {
		rn.mu.Unlock()

		// FIXME: error that allows you to check if it's suspended or shut down:
		return fmt.Errorf("runner is not enabled")
	}

	ready := NewSignal(svcLen)

	var errs []error

	for _, svc := range services {
		if svc == nil || svc.Runnable == nil {
			ready.Done(nil)
			continue
		}

		rs := rn.services[svc]
		if rs != nil {
			ready.Done(fmt.Errorf("service already running"))
			continue
		}

		rn.nextID++
		rs = newRunnerService(rn.nextID, rn, svc, ready)
		rn.services[svc] = rs

		if err := rs.starting(ctx); err != nil {
			ready.Done(err)
			continue
		}

		go func(rs *runnerService, svc *Service) {
			// rn.lock is not assumed to be acquired in here.
			rerr := svc.Runnable.Run(rs)
			if err := rn.ended(rs, rerr); err != nil {
				panic(err)
			}
		}(rs, svc)
	}
	rn.mu.Unlock()

	var ctxDone <-chan struct{}
	if ctx != nil {
		ctxDone = ctx.Done()
	}

	select {
	case err := <-ready.Waiter():
		errs = append(errs, Errors(err)...)
		if len(errs) > 0 {
			return &serviceErrors{errors: errs}
		}
		return nil

	case <-ctxDone:
		return ctx.Err()
	}
}

func (rn *runner) Halt(ctx context.Context, services ...*Service) (rerr error) {
	svcLen := len(services)
	if svcLen == 0 {
		return nil
	}

	done := NewSignal(svcLen)

	var errs []error

	rn.mu.Lock()
	for _, svc := range services {
		rs := rn.services[svc]
		if rs == nil {
			done.Done(nil)
			continue
		}

		// halting will always call done.Done()
		if err := rs.halting(done); err != nil {
			errs = append(errs, err)
			continue
		}
	}
	rn.mu.Unlock()

	var ctxDone <-chan struct{}
	if ctx != nil {
		ctxDone = ctx.Done()
	}

	select {
	case err := <-done.Waiter():
		errs = append(errs, Errors(err)...)
		if len(errs) > 0 {
			return &serviceErrors{errors: errs}
		}
		return nil

	case <-ctxDone:
		return ctx.Err()
	}
}

func (rn *runner) Services(query State, limit int, into []ServiceInfo) []ServiceInfo {
	if query == Halted {
		// The runner does not retain halted services, so this should
		// always return nothing:
		return nil
	}

	rn.mu.Lock()
	defer rn.mu.Unlock()

	slen := len(rn.services)
	if slen == 0 {
		return into
	}

	if limit <= 0 {
		limit = len(rn.services)
	}

	if len(into) == 0 {
		into = make([]ServiceInfo, 0, limit)
	}

	n := 0
	for service, rs := range rn.services {
		state := rs.State()
		if state.Match(query) {
			into = append(into, ServiceInfo{
				State:   state,
				Service: service,
			})
			n++

			if n >= limit {
				break
			}
		}
	}

	return into
}

func (rn *runner) State(svc *Service) (state State) {
	rn.mu.Lock()
	rs := rn.services[svc]
	rn.mu.Unlock()

	if rs != nil {
		state = rs.State()
	} else {
		state = Halted
	}
	return state
}

func (rn *runner) ended(rsvc *runnerService, err error) error {
	rn.mu.Lock()

	delete(rn.services, rsvc.service)

	rsvc.mu.Lock()
	rsvc.setState(Ended)
	rsvc.done = nil

	// This is a strange looking bit of code; we have to separate
	// "ready errors" from "halt errors".
	//
	// - If a service ends, regardless of whether it was a "ready error" or a
	//   "halt error", we want it reported to the listener as the "reason why
	//   the service ended".
	//
	// - If the service fails before it is "ready", the error should be sent
	//   to the "ready" signal *only*, not the "halt" signal.
	//
	// - This is relevant if "halt" is called after a service fails before it's
	//   ready with a context timeout.
	//
	herr := err
	if rsvc.stage == StageReady {
		rsvc.setReady(err)
		herr = nil
	}

	// This MUST happen before the waiters are notified. If not, then the
	// service won't be deleted from Runner.services before Halt() returns,
	// which can cause Start() to return a "service already running" error
	// even if the calls to Start and Halt are sequential.
	rn.raiseOnEnded(rsvc.stage, rsvc.service, err)

	rsvc.readyCalled = false
	for _, w := range rsvc.waiters {
		w.Done(herr)
	}
	rsvc.waiters = nil

	rsvc.mu.Unlock()
	rn.mu.Unlock()

	return nil
}

func (rn *runner) raiseOnEnded(stage Stage, service *Service, err error) {
	if rn.onEnd != nil {
		go rn.onEnd(stage, service, err)
	}
	if service.OnEnd != nil {
		go service.OnEnd(stage, service, err)
	}
}

func (rn *runner) raiseOnError(stage Stage, service *Service, err error) {
	if rn.onError != nil {
		go rn.onError(stage, service, err)
	}
}

func (rn *runner) raiseOnState(service *Service, from, to State) {
	if rn.onState != nil {
		go rn.onState(service, from, to)
	}
	if service.OnState != nil {
		go service.OnState(service, from, to)
	}
}

type ServiceInfo struct {
	State   State
	Service *Service
}
