package wrapper

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
)

var (
	ErrAlreadyShutdown = errors.New("already shutdown")
)

type HandleFunc func(ctx context.Context, wrapperData *Data)

type Option func(wrapperData *Data)

type Middleware func(next HandleFunc) HandleFunc

type FuncManager interface {
	// Run will run the fn synchronously
	Run(ctx context.Context, fn HandleFunc, opts ...Option)
	// RunAsync will run the fn inside goroutine. No need to spawn the goroutine
	RunAsync(ctx context.Context, fn HandleFunc, opts ...Option)
	// Wait will wait for the func manager is shutdown
	Wait() <-chan struct{}
	// Shutdown will force shutdown when the ctx is done
	Shutdown(ctx context.Context) error
}

type Data struct {
	dataLock sync.RWMutex
	data     map[interface{}]interface{}
}

func (d *Data) Get(key interface{}) interface{} {
	d.dataLock.RLock()
	defer d.dataLock.RUnlock()
	if d.data == nil {
		return nil
	}
	return d.data[key]
}

func (d *Data) Set(key interface{}, val interface{}) error {
	if key == nil {
		return errors.New("nil key")
	}
	if !reflect.TypeOf(key).Comparable() {
		return errors.New("key is not comparable")
	}
	d.dataLock.Lock()
	defer d.dataLock.Unlock()
	if d.data == nil {
		d.data = make(map[interface{}]interface{})
	}
	d.data[key] = val
	return nil
}

type key string

const (
	keyIdentifier = key("identifier")
)

func WithOptionIdentifier(funcName string) Option {
	return func(data *Data) {
		_ = data.Set(keyIdentifier, funcName)
	}
}

func GetIdentifier(wrapperData *Data) string {
	val, ok := wrapperData.Get(keyIdentifier).(string)
	if !ok {
		return ""
	}
	return val
}

func WithMiddlewareRecoverPanic(onPanic func(recoverVal interface{}, wrapperData *Data)) Middleware {
	return func(next HandleFunc) HandleFunc {
		return func(ctx context.Context, wrapperData *Data) {
			defer func() {
				val := recover()
				if val != nil {
					if onPanic != nil {
						onPanic(val, wrapperData)
					}
				}
			}()
			next(ctx, wrapperData)
		}
	}
}

type funcManager struct {
	wg            sync.WaitGroup
	isShutdown    int32
	shutdown      chan struct{}
	mainCtx       context.Context
	mainCtxCancel context.CancelFunc
	middlewares   []Middleware
}

func NewFuncManager(middlewares ...Middleware) FuncManager {
	ctx, cancel := context.WithCancel(context.Background())

	m := &funcManager{
		shutdown:      make(chan struct{}),
		mainCtx:       ctx,
		mainCtxCancel: cancel,
		middlewares:   middlewares,
	}

	return m
}

func (m *funcManager) Run(ctx context.Context, fn HandleFunc, opts ...Option) {
	if atomic.LoadInt32(&m.isShutdown) == 1 {
		return
	}

	m.wg.Add(1)
	defer m.wg.Done()
	m.run(ctx, fn, opts...)
}

func (m *funcManager) RunAsync(ctx context.Context, fn HandleFunc, opts ...Option) {
	if atomic.LoadInt32(&m.isShutdown) == 1 {
		return
	}

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.run(ctx, fn, opts...)
	}()
}

func (m *funcManager) Wait() <-chan struct{} {
	return m.shutdown
}

func (m *funcManager) Shutdown(ctx context.Context) error {
	if !atomic.CompareAndSwapInt32(&m.isShutdown, 0, 1) {
		return ErrAlreadyShutdown
	}

	defer func() {
		close(m.shutdown)
	}()

	m.mainCtxCancel()

	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
	}

	return nil
}

func (m *funcManager) run(ctx context.Context, fn HandleFunc, opts ...Option) {
	if fn == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	wrapperData := &Data{}

	go func() {
		select {
		case <-ctx.Done():
		case <-m.mainCtx.Done():
			cancel()
		}
	}()

	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt(wrapperData)
	}

	for i := len(m.middlewares) - 1; i >= 0; i-- {
		if m.middlewares[i] == nil {
			continue
		}
		fn = m.middlewares[i](fn)
	}

	fn(ctx, wrapperData)
}
