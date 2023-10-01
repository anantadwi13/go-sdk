package io

import (
	"context"
	"io"
	"sync/atomic"
)

type testReadSeekCloser struct {
	readSeeker io.ReadSeeker
}

func (t *testReadSeekCloser) Read(p []byte) (n int, err error) {
	return t.readSeeker.Read(p)
}

func (t *testReadSeekCloser) Seek(offset int64, whence int) (int64, error) {
	return t.readSeeker.Seek(offset, whence)
}

func (t *testReadSeekCloser) Close() error {
	return nil
}

type testReader struct {
	data []byte
	pos  int64
}

func (r *testReader) Read(p []byte) (n int, err error) {
	if r.pos == int64(len(r.data)) {
		return 0, io.EOF
	}
	n = copy(p, r.data[r.pos:])
	r.pos += int64(n)
	return
}

type noPool struct {
	bufSize int
}

func (p *noPool) BufferSize() int {
	return p.bufSize
}

func (p *noPool) Put(buf *Buffer) {
}

func (p *noPool) Get(ctx context.Context) (*Buffer, error) {
	return NewBuffer(p, make([]byte, p.bufSize)), nil
}

type testPool struct {
	diff int32
	p    Pool
}

func (t *testPool) Diff() int32 {
	return atomic.LoadInt32(&t.diff)
}

func (t *testPool) BufferSize() int {
	return t.p.BufferSize()
}

func (t *testPool) Put(buf *Buffer) {
	atomic.AddInt32(&t.diff, -1)
	t.p.Put(buf)
}

func (t *testPool) Get(ctx context.Context) (*Buffer, error) {
	atomic.AddInt32(&t.diff, 1)
	buf, err := t.p.Get(ctx)
	if err != nil {
		return nil, err
	}
	buf.pool = t
	return buf, nil
}
