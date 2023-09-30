package io

import (
	"context"
	"io"
	"sync"
)

var (
	Discard   io.Writer
	NopCloser func(r io.Reader) io.ReadCloser
)

type pool struct {
	p       *sync.Pool
	bufSize int
}

func newPool(bufferSize int) Pool {
	if bufferSize <= 0 {
		bufferSize = DefaultBufferSize
	}
	p := &sync.Pool{New: func() interface{} {
		return make([]byte, 0, bufferSize)
	}}
	return &pool{p: p, bufSize: bufferSize}
}

func (p *pool) BufferSize() int {
	return p.bufSize
}

func (p *pool) Put(bytes []byte) {
	p.p.Put(bytes)
}

func (p *pool) Get(ctx context.Context) ([]byte, error) {
	return p.p.Get().([]byte), nil
}
