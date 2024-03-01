package wrapper

import (
	"context"
	"errors"
	"log"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestFlow1(t *testing.T) {
	checker := int32(14)
	wg := sync.WaitGroup{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wg.Add(3) // we will run 3 functions
	m := NewFuncManager(
		func(next HandleFunc) HandleFunc {
			return func(ctx context.Context, wrapperData *Data) {
				defer wg.Done()

				switch GetIdentifier(wrapperData) {
				case "check-shutdown", "check-panic", "":
					atomic.AddInt32(&checker, -1)
				}
				atomic.AddInt32(&checker, -1)
				log.Println("previous", GetIdentifier(wrapperData))
				next(ctx, wrapperData)
				atomic.AddInt32(&checker, -1)
				log.Println("after", GetIdentifier(wrapperData))
			}
		},
		WithMiddlewareRecoverPanic(func(recoverVal interface{}, wrapperData *Data) {
			atomic.AddInt32(&checker, -1)
			log.Println("panic", GetIdentifier(wrapperData), recoverVal)
		}),
	)

	runnerCtx, runnerCtxCancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer runnerCtxCancel()

	m.RunAsync(runnerCtx, func(ctx context.Context, wrapperData *Data) {
		<-ctx.Done()
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			atomic.AddInt32(&checker, -1)
		}
		panic("trigger panic" + ctx.Err().Error())
	}, WithOptionIdentifier("check-panic"))

	m.RunAsync(
		context.Background(),
		func(ctx context.Context, wrapperData *Data) {
			select {
			case <-time.After(20 * time.Second):
				panic("will not be triggered")
			case <-ctx.Done():
				atomic.AddInt32(&checker, -1)
				log.Println("will be here")
			}
		},
		WithOptionIdentifier("check-shutdown"),
	)

	m.Run(
		context.Background(),
		func(ctx context.Context, wrapperData *Data) {
			atomic.AddInt32(&checker, -1)
			log.Println("executed")
		},
	)

	go func() {
		wg.Wait()
		cancel()
	}()

	go func() {
		<-time.After(2 * time.Second)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := m.Shutdown(ctx)
		if err != nil {
			log.Println("shutdown error", err)
			return
		}
	}()

	<-m.Wait()
	<-ctx.Done()

	// running function after shutting down
	m.Run(context.Background(), func(ctx context.Context, wrapperData *Data) {
		atomic.AddInt32(&checker, -1) // will not be triggered
	})
	m.RunAsync(context.Background(), func(ctx context.Context, wrapperData *Data) {
		// todo
		atomic.AddInt32(&checker, -1) // will not be triggered
	})
	err := m.Shutdown(context.Background())
	if errors.Is(err, ErrAlreadyShutdown) {
		atomic.AddInt32(&checker, -1)
	}

	if checker != 0 {
		t.Errorf("invalid checker, checker is not 0. checker: %d", checker)
	}
}

func TestFlow2(t *testing.T) {
	checker := int32(2)
	wg := sync.WaitGroup{}
	m := NewFuncManager()

	wg.Add(1)
	m.RunAsync(context.Background(), func(ctx context.Context, wrapperData *Data) {
		defer wg.Done()
		<-time.After(2 * time.Second)
		atomic.AddInt32(&checker, -1)
	})

	ctxShutdown, cancelCtxShutdown := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancelCtxShutdown()
	err := m.Shutdown(ctxShutdown)
	if errors.Is(err, context.DeadlineExceeded) {
		atomic.AddInt32(&checker, -1)
	}

	wg.Wait()

	if checker != 0 {
		t.Errorf("invalid checker, checker is not 0. checker: %d", checker)
	}
}

func TestValidation(t *testing.T) {
	checker := int32(4)
	wg := sync.WaitGroup{}
	m := NewFuncManager(nil)
	m.Run(context.Background(), nil)
	m.RunAsync(context.Background(), nil)
	m.Run(nil, func(ctx context.Context, wrapperData *Data) {
		atomic.AddInt32(&checker, -1)
	})
	wg.Add(1)
	m.RunAsync(nil, func(ctx context.Context, wrapperData *Data) {
		defer wg.Done()
		atomic.AddInt32(&checker, -1)
	})
	m.Run(nil, func(ctx context.Context, wrapperData *Data) {
		atomic.AddInt32(&checker, -1)
	}, nil)
	wg.Add(1)
	m.RunAsync(nil, func(ctx context.Context, wrapperData *Data) {
		defer wg.Done()
		atomic.AddInt32(&checker, -1)
	}, nil)

	wg.Wait()

	if checker != 0 {
		t.Errorf("invalid checker, checker is not 0. checker: %d", checker)
	}
}

func TestData(t *testing.T) {
	checker := int32(6)
	data := &Data{}

	// key is nil
	err := data.Set(nil, 123)
	if err != nil {
		checker--
	}

	// non comparable
	err = data.Set([]string{"a"}, 123)
	if err != nil {
		checker--
	}

	// key as string
	err = data.Set("abc", 123)
	if err == nil {
		checker--
	}

	// key as another string type
	type key string
	err = data.Set(key("abc"), 456)
	if err == nil {
		checker--
	}

	if data.Get("abc") == 123 {
		checker--
	}
	if data.Get(key("abc")) == 456 {
		checker--
	}

	if checker != 0 {
		t.Errorf("invalid checker, checker is not 0. checker: %d", checker)
	}
}
