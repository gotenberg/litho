package compose_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"os"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
)

// spoolDoc returns a document of a few pages of text and a picture, so it
// has content streams, a font program and an image to spool or keep.
func spoolDoc(t *testing.T) *compose.Document {
	t.Helper()
	face := buildFace("Spool", 100)
	pic := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for i := range pic.Pix {
		pic.Pix[i] = uint8(i) //nolint:gosec // test values wrap on purpose
	}
	src := encode(t, pic)
	doc := &compose.Document{}
	for range 5 {
		doc.Pages = append(doc.Pages, &compose.Page{Width: 200, Height: 200, Content: []compose.Fragment{
			run(t, face, 12, compose.Point{X: 10, Y: 20}, "Spooled text"),
			&compose.Image{Rect: compose.Rect{X: 10, Y: 40, Width: 50, Height: 50}, Source: src},
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
		if err := compose.Write(context.Background(), doc, &buf, compose.Options{TempDir: dir, Spool: held}); err != nil {
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
	if err := compose.Write(context.Background(), doc, failingWriter{}, compose.Options{TempDir: dir, Spool: 1}); !errors.Is(err, errDst) {
		t.Errorf("Write = %v, want the dst error", err)
	}
	emptyDir(t, dir)
	func() {
		defer func() {
			if r := recover(); r != errDst { //nolint:errorlint // the panic value is the sentinel itself
				t.Errorf("recovered %v, want the dst panic", r)
			}
		}()
		_ = compose.Write(context.Background(), doc, failingWriter{panics: true}, compose.Options{TempDir: dir, Spool: 1}) //nolint:errcheck // the call panics before it returns
	}()
	emptyDir(t, dir)
	if err := compose.Write(context.Background(), doc, &bytes.Buffer{}, compose.Options{TempDir: dir + "/missing", Spool: 1}); err == nil {
		t.Error("Write succeeds without a spool directory")
	}
}
