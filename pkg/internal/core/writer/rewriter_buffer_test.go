package writer

import (
	"bytes"
	"errors"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// writeCounter counts the writes it receives and keeps their bytes.
type writeCounter struct {
	bytes.Buffer
	writes int
}

func (w *writeCounter) Write(p []byte) (int, error) {
	w.writes++
	return w.Buffer.Write(p)
}

// manyObjects returns an allocator holding n small dictionaries and the
// trailer of a file made of them.
func manyObjects(n int) (*ObjAllocator, ast.PdfDict) {
	alloc := NewObjAllocator(0)
	var root ast.PdfIndirectRef
	for i := range n {
		ref := alloc.Alloc(ast.PdfDict{"Type": ast.PdfName("Annot"), "F": ast.PdfInt(i), "Contents": ast.PdfString("a note of some words")})
		if i == 0 {
			root = ref
		}
	}
	return alloc, ast.PdfDict{"Root": root}
}

// TestWriteRewrite_BuffersTheDestination checks that a file of many objects
// reaches its destination in writes of about [BufferSize], not one a token,
// and that the buffer changes no byte of it.
func TestWriteRewrite_BuffersTheDestination(t *testing.T) {
	t.Parallel()
	const objects = 20_000
	for _, xrefStream := range []bool{false, true} {
		alloc, trailer := manyObjects(objects)
		var dst writeCounter
		if err := WriteRewrite(t.Context(), &dst, RewriteConfig{Version: "1.7", Allocator: alloc, Trailer: trailer, UseXRefStream: xrefStream}); err != nil {
			t.Fatal(err)
		}
		// Every write but the last fills the buffer; a write larger than
		// the buffer goes through whole, so the count never passes this.
		if most := dst.Len()/BufferSize + 2; dst.writes > most {
			t.Errorf("xref stream %v: %d bytes reached the destination in %d writes, want at most %d", xrefStream, dst.Len(), dst.writes, most)
		}
		if dst.writes > objects/100 {
			t.Errorf("xref stream %v: %d writes for %d objects", xrefStream, dst.writes, objects)
		}

		// The same file through a destination that is a buffer already.
		alloc, trailer = manyObjects(objects)
		var plain bytes.Buffer
		if err := WriteRewrite(t.Context(), &plain, RewriteConfig{Version: "1.7", Allocator: alloc, Trailer: trailer, UseXRefStream: xrefStream}); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(dst.Bytes(), plain.Bytes()) {
			t.Errorf("xref stream %v: the counted and the plain destination hold different files: %s", xrefStream, firstDiff(dst.String(), plain.String()))
		}
		if !bytes.HasSuffix(plain.Bytes(), []byte("%%EOF\n")) {
			t.Errorf("xref stream %v: the file does not end with its end-of-file marker: the buffer was not flushed", xrefStream)
		}
	}
}

// failAfter fails every write once it has taken limit bytes.
type failAfter struct {
	limit int
	n     int
}

var errDiskFull = errors.New("disk full")

func (w *failAfter) Write(p []byte) (int, error) {
	if w.n+len(p) > w.limit {
		return 0, errDiskFull
	}
	w.n += len(p)
	return len(p), nil
}

// TestWriteRewrite_ReportsAFailedWrite checks that an error of the
// destination reaches the caller wherever the buffer meets it: on the first
// full buffer, or on the flush that ends a file smaller than the buffer.
func TestWriteRewrite_ReportsAFailedWrite(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct{ objects, limit int }{
		"a file larger than the buffer":  {objects: 20_000, limit: BufferSize},
		"a file smaller than the buffer": {objects: 3, limit: 10},
	} {
		alloc, trailer := manyObjects(tc.objects)
		err := WriteRewrite(t.Context(), &failAfter{limit: tc.limit}, RewriteConfig{Version: "1.7", Allocator: alloc, Trailer: trailer})
		if !errors.Is(err, errDiskFull) {
			t.Errorf("%s: WriteRewrite = %v, want the destination's error", name, err)
		}
	}
}
