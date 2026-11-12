package typeset_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"os"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// spoolDoc returns a document of a few pages of text and a picture, so it
// has content streams, a font program and an image to spool or keep.
func spoolDoc(t *testing.T) *typeset.Document {
	t.Helper()
	face := buildFace("Spool", 100)
	pic := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for i := range pic.Pix {
		pic.Pix[i] = uint8(i) // test values wrap on purpose
	}
	src := encode(t, pic)
	doc := &typeset.Document{}
	for range 5 {
		doc.Pages = append(doc.Pages, &typeset.Page{Width: 200, Height: 200, Content: []typeset.Fragment{
			run(t, face, 12, typeset.Point{X: 10, Y: 20}, "Spooled text"),
			&typeset.Image{Rect: typeset.Rect{X: 10, Y: 40, Width: 50, Height: 50}, Source: src},
		}})
	}
	return doc
}

// emptyDir fails t when dir holds a file.
func emptyDir(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("%d files left in the temp directory", len(entries))
	}
}

// TestWriteSpoolKeepsTheBytes locks that the output is the same whether
// every stream goes through the spool file or none does, and that the
// file is gone when Write returns.
func TestWriteSpoolKeepsTheBytes(t *testing.T) {
	t.Parallel()
	doc := spoolDoc(t)
	dir := t.TempDir()
	var outs [3][]byte
	for i, held := range []int64{-1, 1, 0} {
		var buf bytes.Buffer
		if err := typeset.Write(context.Background(), doc, &buf, typeset.Options{TempDir: dir, Spool: held}); err != nil {
			t.Fatalf("Write with spool %d: %v", held, err)
		}
		outs[i] = buf.Bytes()
		emptyDir(t, dir)
	}
	if !bytes.Equal(outs[0], outs[1]) || !bytes.Equal(outs[0], outs[2]) {
		t.Error("the spool changes the output")
	}
}

// failingWriter fails every write, or panics when panics is set.
type failingWriter struct{ panics bool }

var errDst = errors.New("dst refused")

func (f failingWriter) Write([]byte) (int, error) {
	if f.panics {
		panic(errDst)
	}
	return 0, errDst
}

// TestWriteSpoolIsRemovedOnFailure locks that the spool file is removed
// when dst fails and when a panic crosses Write.
func TestWriteSpoolIsRemovedOnFailure(t *testing.T) {
	t.Parallel()
	doc := spoolDoc(t)
	dir := t.TempDir()
	if err := typeset.Write(context.Background(), doc, failingWriter{}, typeset.Options{TempDir: dir, Spool: 1}); !errors.Is(err, errDst) {
		t.Errorf("Write = %v, want the dst error", err)
	}
	emptyDir(t, dir)
	func() {
		defer func() {
			if r := recover(); r != errDst { // the panic value is the sentinel itself
				t.Errorf("recovered %v, want the dst panic", r)
			}
		}()
		_ = typeset.Write(context.Background(), doc, failingWriter{panics: true}, typeset.Options{TempDir: dir, Spool: 1}) // the call panics before it returns
	}()
	emptyDir(t, dir)
	if err := typeset.Write(context.Background(), doc, &bytes.Buffer{}, typeset.Options{TempDir: dir + "/missing", Spool: 1}); err == nil {
		t.Error("Write succeeds without a spool directory")
	}
}
