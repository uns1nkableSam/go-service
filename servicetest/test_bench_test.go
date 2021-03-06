package servicetest

import (
	"sync"
	"testing"
	"time"

	service "github.com/shabbyrobe/go-service"
)

func BenchmarkRunnerStart1(b *testing.B) {
	benchmarkRunnerStartN(b, 1)
}

func BenchmarkRunnerStartWait1(b *testing.B) {
	benchmarkRunnerStartWaitN(b, 1)
}

func BenchmarkGoroutineStart1(b *testing.B) {
	benchmarkGoroutineStartN(b, 1)
}

func BenchmarkRunnerStart10(b *testing.B) {
	benchmarkRunnerStartN(b, 10)
}

func BenchmarkRunnerStartWait10(b *testing.B) {
	benchmarkRunnerStartWaitN(b, 10)
}

func BenchmarkGoroutineStart10(b *testing.B) {
	benchmarkGoroutineStartN(b, 10)
}

func benchmarkRunnerStartN(b *testing.B, n int) {
	r := service.NewRunner()

	svcs := make([]*service.Service, n)
	for i := 0; i < n; i++ {
		svcs[i] = &service.Service{
			Runnable: (&BlockingService{}).Init(),
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StartTimer()
		if err := r.Start(nil, svcs...); err != nil {
			panic(err)
		}
		b.StopTimer()

		if err := r.Halt(nil, svcs...); err != nil {
			panic(err)
		}
	}
}

func benchmarkRunnerStartWaitN(b *testing.B, n int) {
	r := service.NewRunner()

	svcs := make([]*service.Service, n)
	for i := 0; i < n; i++ {
		svcs[i] = &service.Service{
			Runnable: (&BlockingService{}).Init(),
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StartTimer()
		if err := service.StartTimeout(1*time.Second, r, svcs...); err != nil {
			panic(err)
		}
		b.StopTimer()
		for j := 0; j < n; j++ {
			if err := service.HaltTimeout(1*time.Second, r, svcs...); err != nil {
				panic(err)
			}
		}
	}
}

func benchmarkGoroutineStartN(b *testing.B, n int) {
	b.StopTimer()
	b.ResetTimer()

	var wg sync.WaitGroup
	for i := 0; i < b.N; i++ {
		stop := make(chan struct{})
		wg.Add(n)
		b.StartTimer()
		for i := 0; i < n; i++ {
			go func() {
				<-stop
				wg.Done()
			}()
		}
		b.StopTimer()
		close(stop)
		wg.Wait()
	}
}
