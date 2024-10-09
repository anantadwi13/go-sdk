package io

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
)

type bufferReadSeekCloserFactory struct {
	pool Pool
}

type OptionBufferReadSeekCloserFactory func(f *bufferReadSeekCloserFactory)

func OptionWithPool(p Pool) OptionBufferReadSeekCloserFactory {
	return func(f *bufferReadSeekCloserFactory) {
		if f == nil {
			return
		}
		f.pool = p
	}
}

func OptionWithSyncPool(bufferSize int) OptionBufferReadSeekCloserFactory {
	return func(f *bufferReadSeekCloserFactory) {
		if f == nil {
			return
		}
		f.pool = newPool(bufferSize)
	}
}

func NewBufferReadSeekCloserFactory(options ...OptionBufferReadSeekCloserFactory) BufferReadSeekCloserFactory {
	b := &bufferReadSeekCloserFactory{}

	for _, option := range options {
		if option == nil {
			continue
		}
		option(b)
	}

	if b.pool == nil {
		b.pool = newPool(DefaultBufferSize)
	}

	return b
}

func (b *bufferReadSeekCloserFactory) NewReader(r io.Reader) BufferReadSeekCloser {
	var rc io.ReadCloser
	switch r := r.(type) {
	case BufferReadSeekCloser:
		rc = r
	case io.ReadSeeker:
		return &bufReadSeeker{readSeeker: r}
	case io.ReadCloser:
		rc = r
	default:
		rc = NopCloser(r)
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &bufReader{
		ctx:       ctx,
		cancelCtx: cancel,
		pool:      b.pool,
		reader:    rc,
	}
}

func (b *bufferReadSeekCloserFactory) BufferSize() int {
	return b.pool.BufferSize()
}

type bufReadSeeker struct {
	mu               sync.Mutex
	isSeekerDisabled int32
	isClosed         int32
	currentPos       int64

	readSeeker io.ReadSeeker
}

func (b *bufReadSeeker) Read(p []byte) (n int, err error) {
	if atomic.LoadInt32(&b.isClosed) == 1 {
		return 0, ErrClosed
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	n, err = b.readSeeker.Read(p)
	b.currentPos += int64(n)
	return
}

func (b *bufReadSeeker) Seek(offset int64, whence int) (int64, error) {
	if atomic.LoadInt32(&b.isClosed) == 1 {
		return b.currentPos, ErrClosed
	}
	if atomic.LoadInt32(&b.isSeekerDisabled) == 1 {
		return b.currentPos, ErrSeekerDisabled
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	curPos, err := b.readSeeker.Seek(offset, whence)
	if err != nil {
		return b.currentPos, err
	}
	b.currentPos = curPos
	return curPos, err
}

func (b *bufReadSeeker) Close() error {
	if !atomic.CompareAndSwapInt32(&b.isClosed, 0, 1) {
		return ErrClosed
	}

	switch rs := b.readSeeker.(type) {
	case io.Closer:
		return rs.Close()
	default:
		return nil
	}
}

func (b *bufReadSeeker) DisableSeeker() {
	if atomic.LoadInt32(&b.isClosed) == 1 {
		return
	}
	if !atomic.CompareAndSwapInt32(&b.isSeekerDisabled, 0, 1) {
		return
	}
}

type bufReader struct {
	mu sync.Mutex

	ctx              context.Context
	cancelCtx        context.CancelFunc
	pool             Pool
	isSeekerDisabled int32
	isClosed         int32
	isEofReached     bool
	reader           io.ReadCloser
	buffer           []*Buffer

	currentPos int64
}

func (b *bufReader) DisableSeeker() {
	if atomic.LoadInt32(&b.isClosed) == 1 {
		return
	}
	if !atomic.CompareAndSwapInt32(&b.isSeekerDisabled, 0, 1) {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	// cleanup unused buffer
	b.cleanUpBuffer(false)
}

func (b *bufReader) Seek(offset int64, whence int) (int64, error) {
	if atomic.LoadInt32(&b.isClosed) == 1 {
		return b.currentPos, ErrClosed
	}
	if atomic.LoadInt32(&b.isSeekerDisabled) == 1 {
		return b.currentPos, ErrSeekerDisabled
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	var abs int64

	switch whence {
	case io.SeekStart:
		abs = offset
	case io.SeekCurrent:
		abs = b.currentPos + offset
	case io.SeekEnd:
		if offset > 0 {
			return b.currentPos, ErrSeekerOutOfRange
		}
		_, err := b.read(-1)
		if err != nil && !errors.Is(err, io.EOF) {
			return b.currentPos, err
		}
		abs = b.getReaderPos() + offset
	default:
		return b.currentPos, ErrSeekerInvalidWhence
	}

	if abs < 0 {
		return b.currentPos, ErrSeekerOutOfRange
	}

	bytesToRead := abs - b.getReaderPos()
	if bytesToRead > 0 {
		n, err := b.read(bytesToRead)
		if err != nil && !errors.Is(err, io.EOF) {
			return b.currentPos, err
		}
		if n < bytesToRead {
			return b.currentPos, ErrSeekerOutOfRange
		}
	}

	b.currentPos = abs
	return abs, nil
}

func (b *bufReader) Read(p []byte) (int, error) {
	if atomic.LoadInt32(&b.isClosed) == 1 {
		return 0, ErrClosed
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	n := 0

	if b.currentPos < b.getReaderPos() {
		// get data from buffer
		tmpN, err := b.readTo(p)
		n += tmpN
		if err != nil {
			return n, err
		}
	}

	if n == len(p) {
		return n, nil
	}

	// if seeker is disabled, read the data directly
	if atomic.LoadInt32(&b.isSeekerDisabled) == 1 {
		// cleanup all unused buffer
		defer b.cleanUpBuffer(true)

		tmpN, err := b.reader.Read(p[n:])
		n += tmpN
		return n, err
	}

	tmpN, err := b.read(int64(len(p[n:])))
	if tmpN > 0 {
		var realN int
		realN, err = b.readTo(p[n:]) // reassign error
		n += realN
	}
	if err != nil {
		return n, err
	}

	return n, nil
}

// copy data from buffer to p
func (b *bufReader) readTo(p []byte) (n int, err error) {
	for {
		switch {
		case b.currentPos >= b.getReaderPos():
			if b.isEofReached && n == 0 {
				err = io.EOF
			}
			return
		case n == len(p):
			return
		case atomic.LoadInt32(&b.isClosed) == 1:
			err = ErrClosed
			return
		}

		buf := b.buffer[b.currentPos/int64(b.pool.BufferSize())]
		currentPos := int(b.currentPos % int64(b.pool.BufferSize()))

		read := copy(p[n:], buf.buffer[currentPos:])
		n += read
		b.currentPos += int64(read)
	}
}

// put data from underlying reader to buffer
func (b *bufReader) read(n int64) (bytesRead int64, err error) {
	for {
		switch {
		case b.isEofReached:
			err = io.EOF
			return
		case errors.Is(err, io.EOF):
			b.isEofReached = true
			if bytesRead > 0 {
				err = nil
			}
			return
		case err != nil:
			// unexpected error
			return
		case n >= 0 && bytesRead >= n:
			return
		}

		var buf *Buffer

		if len(b.buffer) != 0 {
			buf = b.buffer[len(b.buffer)-1]
		}
		if buf == nil || len(buf.buffer) == cap(buf.buffer) {
			buf, err = b.pool.Get(b.ctx)
			if err != nil {
				if errors.Is(err, context.Canceled) {
					err = ErrClosed
				}
				return
			}

			buf.buffer = buf.buffer[:0]
			b.buffer = append(b.buffer, buf)
		}

		var tmpN int
		tmpN, err = b.reader.Read(buf.buffer[len(buf.buffer):cap(buf.buffer)])
		if tmpN > 0 {
			buf.buffer = buf.buffer[:len(buf.buffer)+tmpN]
			bytesRead += int64(tmpN)
		}
	}
}

func (b *bufReader) getReaderPos() int64 {
	l := len(b.buffer)

	if l == 0 {
		return 0
	}

	return int64(l-1)*int64(b.pool.BufferSize()) + int64(len(b.buffer[l-1].buffer))
}

func (b *bufReader) cleanUpBuffer(all bool) {
	currentReaderPos := int(b.currentPos / int64(b.pool.BufferSize()))

	for i := range b.buffer {
		if !all && i >= currentReaderPos {
			return
		}

		if b.buffer[i] == nil {
			continue
		}
		b.buffer[i].cleanUp()
		b.buffer[i] = nil
	}
	b.buffer = nil
}

func (b *bufReader) Close() error {
	if !atomic.CompareAndSwapInt32(&b.isClosed, 0, 1) {
		return ErrClosed
	}

	defer func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.cleanUpBuffer(true)
	}()

	b.cancelCtx()
	return b.reader.Close()
}
