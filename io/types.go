package io

import (
	"context"
	"errors"
	"io"
)

const DefaultBufferSize = 32 * 1024

var (
	ErrClosed              = errors.New("closed reader")
	ErrSeekerDisabled      = errors.New("disabled seeker")
	ErrSeekerOutOfRange    = errors.New("out of range")
	ErrSeekerInvalidWhence = errors.New("invalid whence")
)

type BufferReadSeekCloserFactory interface {
	// Close must be called in order to release the underlying buffer
	NewReader(r io.Reader) BufferReadSeekCloser
	BufferSize() int
}

type BufferReadSeekCloser interface {
	io.Reader
	io.Seeker
	io.Closer
	// DisableSeeker will disable the seeker function and release the underlying buffers
	DisableSeeker()
}

type Buffer struct {
	pool   Pool
	buffer []byte
}

func NewBuffer(pool Pool, buf []byte) *Buffer {
	buf = buf[:cap(buf)]
	return &Buffer{
		pool:   pool,
		buffer: buf,
	}
}

func (b *Buffer) cleanUp() {
	b.buffer = b.buffer[:cap(b.buffer)]
	b.pool.Put(b)
}

type Pool interface {
	BufferSize() int
	Put(buf *Buffer)
	Get(ctx context.Context) (*Buffer, error)
}
