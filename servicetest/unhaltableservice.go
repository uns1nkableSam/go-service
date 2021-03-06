package servicetest

import service "github.com/shabbyrobe/go-service"

// UnhaltableService is a defective service implementation for testing purposes.
//
// It deliberately ignores service.Context.Done() and service.Context.ShouldHalt().
//
// To stop it properly and ensure you are not leaking resources, use the Kill()
// function.
//
type UnhaltableService struct {
	Name service.Name
	halt chan error
	init bool
}

var _ service.Runnable = &UnhaltableService{}

func (u *UnhaltableService) Init() *UnhaltableService {
	u.init = true
	if u.Name == "" {
		u.Name.AppendUnique()
	}
	u.halt = make(chan error)
	return u
}

func (u *UnhaltableService) Kill() {
	close(u.halt)
}

func (u *UnhaltableService) Run(ctx service.Context) error {
	if !u.init {
		panic("call Init()!")
	}
	if err := ctx.Ready(); err != nil {
		return err
	}
	return <-u.halt
}
