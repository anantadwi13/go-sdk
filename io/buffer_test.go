package io

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConstructor(t *testing.T) {
	tests := []struct {
		name        string
		bf          BufferReadSeekCloserFactory
		wantBufSize int
	}{
		{
			name:        "minimal",
			bf:          NewBufferReadSeekCloserFactory(),
			wantBufSize: DefaultBufferSize,
		},
		{
			name:        "with sync pool",
			bf:          NewBufferReadSeekCloserFactory(OptionWithSyncPool(4)),
			wantBufSize: 4,
		},
		{
			name:        "with sync pool without buffer size",
			bf:          NewBufferReadSeekCloserFactory(OptionWithSyncPool(0)),
			wantBufSize: DefaultBufferSize,
		},
		{
			name:        "with custom pool",
			bf:          NewBufferReadSeekCloserFactory(OptionWithPool(&noPool{bufSize: 10})),
			wantBufSize: 10,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.NotNil(t, test.bf)
			assert.EqualValues(t, test.wantBufSize, test.bf.BufferSize())
			brsc := test.bf.NewReader(&testReader{data: []byte("1234567890qwertyuiop")})
			defer func() {
				err := brsc.Close()
				assert.NoError(t, err)
			}()

			n, err := io.Copy(Discard, brsc)
			assert.NoError(t, err)
			assert.EqualValues(t, 20, n)
		})
	}
}

func TestReaderVariant(t *testing.T) {
	tests := []struct {
		name         string
		reader       io.Reader
		readerLength int
	}{
		{
			name:         "reader",
			reader:       &testReader{data: []byte("1234567890qwertyuiop")},
			readerLength: 20,
		},
		{
			name:         "read closer",
			reader:       NopCloser(&testReader{data: []byte("1234567890qwertyuiop")}),
			readerLength: 20,
		},
		{
			name:         "strings reader",
			reader:       strings.NewReader("1234567890qwertyuiop"),
			readerLength: 20,
		},
		{
			name:         "strings read closer",
			reader:       &testReadSeekCloser{strings.NewReader("1234567890qwertyuiop")},
			readerLength: 20,
		},
		{
			name:         "bytes reader",
			reader:       bytes.NewReader([]byte("1234567890qwertyuiop")),
			readerLength: 20,
		},
		{
			name:         "bytes reader len 7",
			reader:       bytes.NewReader([]byte("1234567")),
			readerLength: 7,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			bf := NewBufferReadSeekCloserFactory()
			assert.NotNil(t, bf)
			assert.EqualValues(t, DefaultBufferSize, bf.BufferSize())
			brsc := bf.NewReader(test.reader)

			readLen := int64(test.readerLength / 4)
			n, err := io.CopyN(Discard, brsc, readLen)
			assert.NoError(t, err)
			assert.EqualValues(t, readLen, n)

			seek, err := brsc.Seek(int64(test.readerLength/2), io.SeekStart)
			assert.NoError(t, err)
			assert.EqualValues(t, test.readerLength/2, seek)

			readLen = int64(test.readerLength / 4)
			n, err = io.CopyN(Discard, brsc, readLen)
			assert.NoError(t, err)
			assert.EqualValues(t, readLen, n)

			seek, err = brsc.Seek(-int64(test.readerLength/4), io.SeekEnd)
			assert.NoError(t, err)
			assert.EqualValues(t, test.readerLength-(test.readerLength/4), seek)

			n, err = io.Copy(Discard, brsc)
			assert.NoError(t, err)
			assert.EqualValues(t, test.readerLength/4, n)

			brsc.DisableSeeker()

			seek, err = brsc.Seek(0, io.SeekStart)
			assert.ErrorIs(t, err, ErrSeekerDisabled)
			assert.EqualValues(t, test.readerLength, seek)

			brsc.DisableSeeker()

			err = brsc.Close()
			assert.NoError(t, err)

			n, err = io.Copy(Discard, brsc)
			assert.ErrorIs(t, err, ErrClosed)
			assert.EqualValues(t, 0, n)

			seek, err = brsc.Seek(0, io.SeekStart)
			assert.ErrorIs(t, err, ErrClosed)
			assert.EqualValues(t, test.readerLength, seek)

			err = brsc.Close()
			assert.ErrorIs(t, err, ErrClosed)

			brsc.DisableSeeker()
		})
	}
}

func TestFlowNormalRead(t *testing.T) {
	tp := &testPool{p: newPool(5)}
	bf := NewBufferReadSeekCloserFactory(OptionWithPool(tp))
	assert.EqualValues(t, 5, bf.BufferSize())

	brsc := bf.NewReader(&testReader{data: []byte("1234567890qwertyuiop")})
	defer func() {
		err := brsc.Close()
		assert.NoError(t, err)
		assert.EqualValues(t, 0, tp.Diff())
		err = brsc.Close()
		assert.ErrorIs(t, err, ErrClosed)
	}()
	readBuf := make([]byte, 10)

	readLength := 3
	n, err := brsc.Read(readBuf[:readLength])
	assert.NoError(t, err)
	assert.EqualValues(t, 3, n)
	assert.Equal(t, []byte("123"), readBuf[:n])
	assert.Equal(t, []byte{'1', '2', '3', 0, 0, 0, 0, 0, 0, 0}, readBuf)
	assert.EqualValues(t, 1, tp.Diff())

	readLength = 2
	n, err = brsc.Read(readBuf[:readLength])
	assert.NoError(t, err)
	assert.EqualValues(t, 2, n)
	assert.Equal(t, []byte("45"), readBuf[:n])
	assert.Equal(t, []byte{'4', '5', '3', 0, 0, 0, 0, 0, 0, 0}, readBuf)
	assert.EqualValues(t, 1, tp.Diff())

	readLength = 1
	n, err = brsc.Read(readBuf[:readLength])
	assert.NoError(t, err)
	assert.EqualValues(t, 1, n)
	assert.Equal(t, []byte("6"), readBuf[:n])
	assert.Equal(t, []byte{'6', '5', '3', 0, 0, 0, 0, 0, 0, 0}, readBuf)
	assert.EqualValues(t, 2, tp.Diff())

	readLength = 9
	n, err = brsc.Read(readBuf[:readLength])
	assert.NoError(t, err)
	assert.EqualValues(t, 9, n)
	assert.Equal(t, []byte("7890qwert"), readBuf[:n])
	assert.Equal(t, []byte{'7', '8', '9', '0', 'q', 'w', 'e', 'r', 't', 0}, readBuf)
	assert.EqualValues(t, 3, tp.Diff())

	readLength = 10
	n, err = brsc.Read(readBuf[:readLength])
	assert.NoError(t, err)
	assert.EqualValues(t, 5, n)
	assert.Equal(t, []byte("yuiop"), readBuf[:n])
	assert.Equal(t, []byte{'y', 'u', 'i', 'o', 'p', 'w', 'e', 'r', 't', 0}, readBuf)
	assert.EqualValues(t, 5, tp.Diff())

	readLength = 10
	n, err = brsc.Read(readBuf[:readLength])
	assert.ErrorIs(t, err, io.EOF)
	assert.EqualValues(t, 0, n)
	assert.Equal(t, []byte(""), readBuf[:n])
	assert.Equal(t, []byte{'y', 'u', 'i', 'o', 'p', 'w', 'e', 'r', 't', 0}, readBuf)
	assert.EqualValues(t, 5, tp.Diff())
}

func TestFlowReadSeek(t *testing.T) {
	tp := &testPool{p: newPool(5)}
	bf := NewBufferReadSeekCloserFactory(OptionWithPool(tp))
	assert.EqualValues(t, 5, bf.BufferSize())

	brsc := bf.NewReader(&testReader{data: []byte("1234567890qwertyuiop")})
	defer func() {
		err := brsc.Close()
		assert.NoError(t, err)
		assert.EqualValues(t, 0, tp.Diff())
	}()
	readBuf := make([]byte, 10)

	readLength := 3
	n, err := brsc.Read(readBuf[:readLength])
	assert.NoError(t, err)
	assert.EqualValues(t, 3, n)
	assert.Equal(t, []byte("123"), readBuf[:n])
	assert.Equal(t, []byte{'1', '2', '3', 0, 0, 0, 0, 0, 0, 0}, readBuf)
	assert.EqualValues(t, 1, tp.Diff())

	seek, err := brsc.Seek(-1, io.SeekStart)
	assert.ErrorIs(t, err, ErrSeekerOutOfRange)
	assert.EqualValues(t, 3, seek) // current position still in 3

	seek, err = brsc.Seek(0, 4)
	assert.ErrorIs(t, err, ErrSeekerInvalidWhence)
	assert.EqualValues(t, 3, seek) // current position still in 3
	assert.EqualValues(t, 1, tp.Diff())

	readLength = 1
	n, err = brsc.Read(readBuf[:readLength])
	assert.NoError(t, err)
	assert.EqualValues(t, 1, n)
	assert.Equal(t, []byte("4"), readBuf[:n])
	assert.Equal(t, []byte{'4', '2', '3', 0, 0, 0, 0, 0, 0, 0}, readBuf)
	assert.EqualValues(t, 1, tp.Diff())

	seek, err = brsc.Seek(0, io.SeekStart)
	assert.NoError(t, err)
	assert.EqualValues(t, 0, seek) // current position is in the begining of file (0)

	readLength = 3
	n, err = brsc.Read(readBuf[:readLength])
	assert.NoError(t, err)
	assert.EqualValues(t, 3, n)
	assert.Equal(t, []byte("123"), readBuf[:n])
	assert.Equal(t, []byte{'1', '2', '3', 0, 0, 0, 0, 0, 0, 0}, readBuf)
	assert.EqualValues(t, 1, tp.Diff())

	seek, err = brsc.Seek(-5, io.SeekCurrent)
	assert.ErrorIs(t, err, ErrSeekerOutOfRange)
	assert.EqualValues(t, 3, seek) // current position still in 3

	readLength = 1
	n, err = brsc.Read(readBuf[:readLength])
	assert.NoError(t, err)
	assert.EqualValues(t, 1, n)
	assert.Equal(t, []byte("4"), readBuf[:n])
	assert.Equal(t, []byte{'4', '2', '3', 0, 0, 0, 0, 0, 0, 0}, readBuf)
	assert.EqualValues(t, 1, tp.Diff())

	seek, err = brsc.Seek(-3, io.SeekCurrent)
	assert.NoError(t, err)
	assert.EqualValues(t, 1, seek)

	readLength = 4
	n, err = brsc.Read(readBuf[:readLength])
	assert.NoError(t, err)
	assert.EqualValues(t, 4, n)
	assert.Equal(t, []byte("2345"), readBuf[:n])
	assert.Equal(t, []byte{'2', '3', '4', '5', 0, 0, 0, 0, 0, 0}, readBuf)
	assert.EqualValues(t, 1, tp.Diff())

	seek, err = brsc.Seek(2, io.SeekCurrent)
	assert.NoError(t, err)
	assert.EqualValues(t, 7, seek)
	assert.EqualValues(t, 2, tp.Diff())

	readLength = 1
	n, err = brsc.Read(readBuf[:readLength])
	assert.NoError(t, err)
	assert.EqualValues(t, 1, n)
	assert.Equal(t, []byte("8"), readBuf[:n])
	assert.Equal(t, []byte{'8', '3', '4', '5', 0, 0, 0, 0, 0, 0}, readBuf)
	assert.EqualValues(t, 2, tp.Diff())

	seek, err = brsc.Seek(2, io.SeekEnd)
	assert.ErrorIs(t, err, ErrSeekerOutOfRange)
	assert.EqualValues(t, 8, seek)
	assert.EqualValues(t, 2, tp.Diff())

	seek, err = brsc.Seek(-3, io.SeekEnd)
	assert.NoError(t, err)
	assert.EqualValues(t, 17, seek)
	assert.EqualValues(t, 5, tp.Diff())

	readLength = 1
	n, err = brsc.Read(readBuf[:readLength])
	assert.NoError(t, err)
	assert.EqualValues(t, 1, n)
	assert.Equal(t, []byte("i"), readBuf[:n])
	assert.Equal(t, []byte{'i', '3', '4', '5', 0, 0, 0, 0, 0, 0}, readBuf)
	assert.EqualValues(t, 5, tp.Diff())

	seek, err = brsc.Seek(-21, io.SeekEnd)
	assert.ErrorIs(t, err, ErrSeekerOutOfRange)
	assert.EqualValues(t, 18, seek)
	assert.EqualValues(t, 5, tp.Diff())

	seek, err = brsc.Seek(-20, io.SeekEnd)
	assert.NoError(t, err)
	assert.EqualValues(t, 0, seek)
	assert.EqualValues(t, 5, tp.Diff())
}

func TestFlowReadSeekOutOfRange(t *testing.T) {
	tp := &testPool{p: newPool(5)}
	bf := NewBufferReadSeekCloserFactory(OptionWithPool(tp))
	assert.EqualValues(t, 5, bf.BufferSize())

	brsc := bf.NewReader(&testReader{data: []byte("1234567890qwertyuiop")})
	defer func() {
		err := brsc.Close()
		assert.NoError(t, err)
		assert.EqualValues(t, 0, tp.Diff())
	}()
	readBuf := make([]byte, 10)

	readLength := 3
	n, err := brsc.Read(readBuf[:readLength])
	assert.NoError(t, err)
	assert.EqualValues(t, 3, n)
	assert.Equal(t, []byte("123"), readBuf[:n])
	assert.Equal(t, []byte{'1', '2', '3', 0, 0, 0, 0, 0, 0, 0}, readBuf)
	assert.EqualValues(t, 1, tp.Diff())

	seek, err := brsc.Seek(21, io.SeekStart)
	assert.ErrorIs(t, err, ErrSeekerOutOfRange)
	assert.EqualValues(t, 3, seek) // current position still in 3
	assert.EqualValues(t, 5, tp.Diff())

	readLength = 3
	n, err = brsc.Read(readBuf[:readLength])
	assert.NoError(t, err)
	assert.EqualValues(t, 3, n)
	assert.Equal(t, []byte("456"), readBuf[:n])
	assert.Equal(t, []byte{'4', '5', '6', 0, 0, 0, 0, 0, 0, 0}, readBuf)
	assert.EqualValues(t, 5, tp.Diff())
}

func TestFlowReadSeekDisableSeeker(t *testing.T) {
	tp := &testPool{p: newPool(5)}
	bf := NewBufferReadSeekCloserFactory(OptionWithPool(tp))
	assert.EqualValues(t, 5, bf.BufferSize())

	brsc := bf.NewReader(&testReader{data: []byte("1234567890qwertyuiop")})
	defer func() {
		err := brsc.Close()
		assert.NoError(t, err)
		assert.EqualValues(t, 0, tp.Diff())
	}()
	readBuf := make([]byte, 10)

	readLength := 3
	n, err := brsc.Read(readBuf[:readLength])
	assert.NoError(t, err)
	assert.EqualValues(t, 3, n)
	assert.Equal(t, []byte("123"), readBuf[:n])
	assert.Equal(t, []byte{'1', '2', '3', 0, 0, 0, 0, 0, 0, 0}, readBuf)
	assert.EqualValues(t, 1, tp.Diff())

	seek, err := brsc.Seek(7, io.SeekStart)
	assert.NoError(t, err)
	assert.EqualValues(t, 7, seek)
	assert.EqualValues(t, 2, tp.Diff())

	readLength = 1
	n, err = brsc.Read(readBuf[:readLength])
	assert.NoError(t, err)
	assert.EqualValues(t, 1, n)
	assert.Equal(t, []byte("8"), readBuf[:n])
	assert.Equal(t, []byte{'8', '2', '3', 0, 0, 0, 0, 0, 0, 0}, readBuf)
	assert.EqualValues(t, 2, tp.Diff())

	brsc.DisableSeeker()
	assert.EqualValues(t, 1, tp.Diff())

	seek, err = brsc.Seek(0, io.SeekStart)
	assert.ErrorIs(t, err, ErrSeekerDisabled)
	assert.EqualValues(t, 8, seek)
	assert.EqualValues(t, 1, tp.Diff())

	seek, err = brsc.Seek(1, io.SeekCurrent)
	assert.ErrorIs(t, err, ErrSeekerDisabled)
	assert.EqualValues(t, 8, seek)
	assert.EqualValues(t, 1, tp.Diff())

	readLength = 5
	n, err = brsc.Read(readBuf[:readLength])
	assert.NoError(t, err)
	assert.EqualValues(t, 5, n)
	assert.Equal(t, []byte("90qwe"), readBuf[:n])
	assert.Equal(t, []byte{'9', '0', 'q', 'w', 'e', 0, 0, 0, 0, 0}, readBuf)
	assert.EqualValues(t, 0, tp.Diff())
}

func TestFlowWithBufferNotFullyFilled(t *testing.T) {
	tp := &testPool{p: newPool(5)}
	bf := NewBufferReadSeekCloserFactory(OptionWithPool(tp))
	assert.EqualValues(t, 5, bf.BufferSize())

	func() {
		brsc := bf.NewReader(&testReader{data: []byte("1234567890qwertyui")}) // 18 size
		defer func() {
			err := brsc.Close()
			assert.NoError(t, err)
			assert.EqualValues(t, 0, tp.Diff())
		}()
		readBuf := make([]byte, 10)

		readLength := 3
		n, err := brsc.Read(readBuf[:readLength])
		assert.NoError(t, err)
		assert.EqualValues(t, 3, n)
		assert.Equal(t, []byte("123"), readBuf[:n])
		assert.Equal(t, []byte{'1', '2', '3', 0, 0, 0, 0, 0, 0, 0}, readBuf)
		assert.EqualValues(t, 1, tp.Diff())

		seek, err := brsc.Seek(0, io.SeekEnd)
		assert.NoError(t, err)
		assert.EqualValues(t, 18, seek)
		assert.EqualValues(t, 4, tp.Diff())
	}()

	func() {
		brsc := bf.NewReader(&testReader{data: []byte("1234567890qwertyui")}) // 18 size
		defer func() {
			err := brsc.Close()
			assert.NoError(t, err)
			assert.EqualValues(t, 0, tp.Diff())
		}()

		seek, err := brsc.Seek(20, io.SeekStart)
		assert.ErrorIs(t, err, ErrSeekerOutOfRange)
		assert.EqualValues(t, 0, seek)
		assert.EqualValues(t, 4, tp.Diff())
	}()

	func() {
		brsc := bf.NewReader(&testReader{data: []byte("1234567890qwertyui")}) // 18 size
		defer func() {
			err := brsc.Close()
			assert.NoError(t, err)
			assert.EqualValues(t, 0, tp.Diff())
		}()
		readBuf := make([]byte, 10)

		io.CopyN(Discard, brsc, 14)

		readLength := 10
		n, err := brsc.Read(readBuf[:readLength])
		assert.NoError(t, err)
		assert.EqualValues(t, 4, n)
		assert.Equal(t, []byte("tyui"), readBuf[:n])
		assert.Equal(t, []byte{'t', 'y', 'u', 'i', 0, 0, 0, 0, 0, 0}, readBuf)
		assert.EqualValues(t, 4, tp.Diff())
	}()
}

// todo concurrent test

func BenchmarkBufferWithPool(b *testing.B) {
	data := make([]byte, 32*1024*1024)
	readLength := int64(len(data) / 2)

	bf := NewBufferReadSeekCloserFactory()
	output := Discard

	for i := 0; i < b.N; i++ {
		benchmarkScenario(data, bf, output, readLength)
	}
}

func BenchmarkBufferWithoutPool(b *testing.B) {
	data := make([]byte, 32*1024*1024)
	readLength := int64(len(data) / 2)

	bf := NewBufferReadSeekCloserFactory(OptionWithPool(&noPool{bufSize: DefaultBufferSize}))
	output := Discard

	for i := 0; i < b.N; i++ {
		benchmarkScenario(data, bf, output, readLength)
	}
}

func benchmarkScenario(data []byte, bf BufferReadSeekCloserFactory, output io.Writer, readLength int64) {
	r := bf.NewReader(&testReader{data: data})

	_, err := io.CopyN(output, r, readLength)
	if err != nil {
		panic(err)
	}

	_, err = r.Seek(0, io.SeekStart)
	if err != nil {
		panic(err)
	}

	_, err = io.CopyN(output, r, readLength/2)
	if err != nil {
		panic(err)
	}

	r.DisableSeeker()

	_, err = r.Seek(0, io.SeekStart)
	if err == nil {
		panic("should return error")
	}

	_, err = io.Copy(output, r)
	if err != nil {
		panic(err)
	}
}
