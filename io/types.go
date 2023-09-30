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
	io.ReadSeekCloser
	DisableSeeker()
}

type Pool interface {
	BufferSize() int
	Put(buf []byte)
	Get(ctx context.Context) ([]byte, error)
}
