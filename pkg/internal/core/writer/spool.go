package writer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// Spool keeps the raw bytes of streams in a temp file instead of memory. A
// writer that makes more stream data than it wants to hold until the file
// is serialized moves each stream into the spool and queues the stream
// [Spool.Stream] returns in its place: the serializer reads the bytes back
// from the file, so the output is the same, byte for byte, as with the
// stream held in memory.
//
// The file is created on the first [Spool.Stream] or [Spool.Put] call, in
// the spool's directory, and [Spool.Close] closes and removes it. A Spool
// is safe for concurrent use; the streams and blobs it returns may be read
// from any goroutine, until Close.
type Spool struct {
	dir  string
	mu   sync.Mutex
	f    *os.File
	size int64
}

// NewSpool returns a spool whose file goes in dir, the operating system's
// temporary directory when dir is empty. It creates nothing yet.
func NewSpool(dir string) *Spool {
	return &Spool{dir: dir}
}

// Stream writes data to the spool's file and returns a stream with dict
// whose bytes read data back from the file. data is not kept: the caller
// may drop it. The returned stream reads the file on every call to its
// Bytes, and keeps nothing, so the bytes live only while the serializer
// writes them. It fails when the file cannot be created or written.
func (s *Spool) Stream(dict ast.PdfDict, data []byte) (ast.PdfStream, error) {
	b, err := s.Put(data)
	if err != nil {
		return ast.PdfStream{}, err
	}
	return ast.NewLazyStream(dict, b.Bytes), nil
}

// Put writes data to the spool's file and returns the blob that reads it
// back. data is not kept: the caller may drop it. It fails when the file
// cannot be created or written.
func (s *Spool) Put(data []byte) (Blob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		f, err := os.CreateTemp(s.dir, "litho-spool-*")
		if err != nil {
			return Blob{}, fmt.Errorf("creating the spool file: %w", err)
		}
		s.f = f
	}
	f, off := s.f, s.size
	if _, err := f.WriteAt(data, off); err != nil {
		return Blob{}, fmt.Errorf("writing the spool file: %w", err)
	}
	s.size += int64(len(data))
	return Blob{f: f, off: off, n: int64(len(data))}, nil
}

// Blob is bytes a [Spool] holds in its file. It keeps no byte in memory:
// every read goes to the file, and fails once the spool is closed. The
// zero Blob is empty.
type Blob struct {
	f      *os.File
	off, n int64
}

// Len returns the bytes of the blob.
func (b Blob) Len() int64 { return b.n }

// Reader returns a reader over the blob's bytes, read from the file as the
// caller reads.
func (b Blob) Reader() io.Reader {
	if b.f == nil {
		return eofReader{}
	}
	return io.NewSectionReader(b.f, b.off, b.n)
}

// Bytes reads the blob's bytes into a new slice.
func (b Blob) Bytes(ctx context.Context) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	buf := make([]byte, b.n) // fits int: n is the length of a slice Put was given
	if b.n == 0 {
		return buf, nil
	}
	if _, err := b.f.ReadAt(buf, b.off); err != nil {
		return nil, fmt.Errorf("reading the spool file: %w", err)
	}
	return buf, nil
}

// eofReader is the reader of the zero Blob.
type eofReader struct{}

func (eofReader) Read([]byte) (int, error) { return 0, io.EOF }

// Size returns the bytes the spool's file holds.
func (s *Spool) Size() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.size
}

// Close closes and removes the spool's file, if it made one. The streams
// and blobs it returned fail to read after it. Calling Close again does nothing.
func (s *Spool) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		return nil
	}
	name := s.f.Name()
	closeErr := s.f.Close()
	s.f = nil
	return errors.Join(closeErr, os.Remove(name))
}
