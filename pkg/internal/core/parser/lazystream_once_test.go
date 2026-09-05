package parser

import (
	"context"
	"io"
	"sync"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// countingReaderAt records how many times a stream's bytes were read from the
// source.
type countingReaderAt struct {
	data []byte
	mu   sync.Mutex
	n    int
}

func (c *countingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
	if off >= int64(len(c.data)) {
		return 0, io.EOF
	}
	return copy(p, c.data[off:]), nil
}

func (c *countingReaderAt) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

// TestLazyStreamReadsTheSourceOnce pins the guarantee callers actually depend
// on: a parsed stream reads and decrypts its bytes once, however often it is
// asked for them.
//
// [ast.NewLazyStream] cannot provide this itself, and its doc used to claim it
// did. The memoisation lives here, in the sync.Once this loader wraps around
// the read. Remediation reads the same stream repeatedly (carrying a body
// across a dict rewrite, decoding an ICC profile, re-checking a font program),
// so losing it would turn one read into many.
func TestLazyStreamReadsTheSourceOnce(t *testing.T) {
	t.Parallel()

	src := &countingReaderAt{data: []byte("stream payload bytes")}
	p := &Parser{}
	s := p.newLazyStream(ast.PdfDict{}, src, 0, int64(len(src.data)))

	ctx := context.Background()
	for i := range 5 {
		got, err := s.Bytes(ctx)
		if err != nil {
			t.Fatalf("read %d: %v", i, err)
		}
		if string(got) != string(src.data) {
			t.Fatalf("read %d returned %q", i, got)
		}
	}
	if n := src.count(); n != 1 {
		t.Errorf("the source was read %d times for 5 accesses, want 1", n)
	}
}

// TestLazyStreamCachesTheReadError pins that a failed read is remembered too:
// retrying on every access would hammer a broken source, and the caller gets
// the same answer either way.
func TestLazyStreamCachesTheReadError(t *testing.T) {
	t.Parallel()

	src := &countingReaderAt{data: nil} // every ReadAt returns io.EOF
	p := &Parser{}
	s := p.newLazyStream(ast.PdfDict{}, src, 0, 8)

	ctx := context.Background()
	first, err := s.Bytes(ctx)
	if err == nil {
		t.Fatalf("reading past the source succeeded, returning %d bytes", len(first))
	}
	for range 4 {
		if _, again := s.Bytes(ctx); again == nil {
			t.Fatal("a later read succeeded where the first failed")
		}
	}
	if n := src.count(); n != 1 {
		t.Errorf("the failing source was read %d times, want 1", n)
	}
}

// TestLazyStreamIsSafeForConcurrentReaders pins that the memoisation holds
// under the concurrent access Document documents: readers share a *Document
// after construction, so two goroutines can reach the same stream at once.
func TestLazyStreamIsSafeForConcurrentReaders(t *testing.T) {
	t.Parallel()

	src := &countingReaderAt{data: []byte("shared payload")}
	p := &Parser{}
	s := p.newLazyStream(ast.PdfDict{}, src, 0, int64(len(src.data)))

	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			if _, err := s.Bytes(context.Background()); err != nil {
				t.Errorf("concurrent read: %v", err)
			}
		})
	}
	wg.Wait()

	if n := src.count(); n != 1 {
		t.Errorf("16 concurrent readers caused %d source reads, want 1", n)
	}
}
