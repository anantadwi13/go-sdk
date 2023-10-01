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
	p := &pool{
		bufSize: bufferSize,
	}
	p.p = &sync.Pool{New: func() interface{} {
		return NewBuffer(p, make([]byte, bufferSize))
	}}
	return p
}

func (p *pool) BufferSize() int {
	return p.bufSize
}

func (p *pool) Put(buf *Buffer) {
	p.p.Put(buf)
}

func (p *pool) Get(ctx context.Context) (*Buffer, error) {
	return p.p.Get().(*Buffer), nil
}
