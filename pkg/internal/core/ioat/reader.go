package ioat

import "io"

// Reader is a stateful, buffered reader built on an [io.ReaderAt]. It
// is NOT safe for concurrent use. Each goroutine must create its own
// Reader from the same [io.ReaderAt], which is position-independent and
// concurrent-safe.
type Reader struct {
	r      io.ReaderAt
	size   int64
	offset int64
	buf    []byte
	bufPos int
	bufLen int
}

// defaultBufSize balances syscall overhead and per-Reader memory at 32 KB.
const defaultBufSize = 32768

// NewReader creates a new concurrent-safe buffered reader.
func NewReader(r io.ReaderAt, size int64, offset int64) *Reader {
	return &Reader{
		r:      r,
		size:   size,
		offset: offset,
		buf:    make([]byte, defaultBufSize),
	}
}

// fill reads more data into the buffer from the underlying ReaderAt.
func (b *Reader) fill() error {
	if b.offset >= b.size {
		return io.EOF
	}

	n, err := b.r.ReadAt(b.buf, b.offset)
	b.bufPos = 0
	b.bufLen = n
	b.offset += int64(n)

	if n > 0 {
		return nil
	}
	if err != nil {
		return err
	}
	return io.EOF
}

// ReadByte reads and returns a single byte.
// If no byte is available, returns an error.
func (b *Reader) ReadByte() (byte, error) {
	if b.bufPos >= b.bufLen {
		err := b.fill()
		if err != nil {
			return 0, err
		}
	}
	c := b.buf[b.bufPos]
	b.bufPos++
	return c, nil
}

// UnreadByte unreads the last byte. Only one byte can be safely unread.
func (b *Reader) UnreadByte() error {
	if b.bufPos <= 0 {
		return ErrUnreadByte
	}
	b.bufPos--
	return nil
}

// Read implements io.Reader.
func (b *Reader) Read(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}

	if b.bufPos >= b.bufLen {
		err := b.fill()
		if err != nil {
			return 0, err
		}
	}

	n = copy(p, b.buf[b.bufPos:b.bufLen])
	b.bufPos += n
	return n, nil
}

// Position returns the current absolute read position in the underlying
// ReaderAt, accounting for buffered but unconsumed bytes.
func (b *Reader) Position() int64 {
	return b.offset - int64(b.bufLen-b.bufPos)
}

// ReaderAt returns the underlying io.ReaderAt.
func (b *Reader) ReaderAt() io.ReaderAt {
	return b.r
}

// Seek moves the current offset. Note: this drops the current buffer.
func (b *Reader) Seek(offset int64, whence int) (int64, error) {
	var abs int64
	switch whence {
	case io.SeekStart:
		abs = offset
	case io.SeekCurrent:
		abs = b.offset - int64(b.bufLen-b.bufPos) + offset
	case io.SeekEnd:
		abs = b.size + offset
	default:
		return 0, ErrSeekInvalidWhence
	}

	if abs < 0 {
		return 0, ErrSeekNegativePosition
	}

	// Invalidate buffer
	b.offset = abs
	b.bufPos = 0
	b.bufLen = 0

	return abs, nil
}
