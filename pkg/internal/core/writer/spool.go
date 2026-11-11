package writer

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// Spool keeps the raw bytes of streams in a temp file instead of memory. A
// writer that makes more stream data than it wants to hold until the file
// is serialized moves each stream into the spool and queues the stream
// [Spool.Stream] returns in its place: the serializer reads the bytes back
// from the file, so the output is the same, byte for byte, as with the
// stream held in memory.
//
// The file is created on the first [Spool.Stream] call, in the spool's
// directory, and [Spool.Close] closes and removes it. A Spool is used by one
// goroutine; the streams it returns may be read from any, until Close.
type Spool struct {
	dir  string
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
	if s.f == nil {
		f, err := os.CreateTemp(s.dir, "litho-spool-*")
		if err != nil {
			return ast.PdfStream{}, fmt.Errorf("creating the spool file: %w", err)
		}
		s.f = f
	}
	f, off, n := s.f, s.size, len(data)
	if _, err := f.WriteAt(data, off); err != nil {
		return ast.PdfStream{}, fmt.Errorf("writing the spool file: %w", err)
	}
	s.size += int64(n)
	return ast.NewLazyStream(dict, func(ctx context.Context) ([]byte, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		buf := make([]byte, n)
		if _, err := f.ReadAt(buf, off); err != nil {
			return nil, fmt.Errorf("reading the spool file: %w", err)
		}
		return buf, nil
	}), nil
}

// Size returns the bytes the spool's file holds.
func (s *Spool) Size() int64 {
	return s.size
}

// Close closes and removes the spool's file, if it made one. The streams
// it returned fail to read after it. Calling Close again does nothing.
func (s *Spool) Close() error {
	if s.f == nil {
		return nil
	}
	name := s.f.Name()
	closeErr := s.f.Close()
	s.f = nil
	return errors.Join(closeErr, os.Remove(name))
}
