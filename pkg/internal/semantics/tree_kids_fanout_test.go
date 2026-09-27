package semantics

import (
	"context"
	"testing"
	"time"
)

// TestExtractEmbeddedFiles_SharedKidsArrayTerminates pins that the
// /EmbeddedFiles walk visits a /Kids array written as an object of its own
// once. Its direct nodes each name it again as their own /Kids: the node marks
// never see a direct node, and the walk, which has no depth bound, recursed
// until the goroutine stack overflowed, a fatal error no recover() converts.
func TestExtractEmbeddedFiles_SharedKidsArrayTerminates(t *testing.T) {
	t.Parallel()

	doc := openBytes(t, efPDF(
		efCatalog, efPages, efPage,
		"<< /Kids 5 0 R >>",
		"[<< /Kids 5 0 R >> << /Names [(a.txt) 6 0 R] >>]",
		"<< /Type /Filespec /F (a.txt) /UF (a.txt) /EF << /F 7 0 R >> >>",
		"<< /Length 3 >>\nstream\nabc\nendstream",
	))

	type result struct {
		n   int
		err error
	}
	done := make(chan result, 1)
	go func() {
		files, err := ExtractEmbeddedFiles(context.Background(), doc)
		done <- result{len(files), err}
	}()
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("extracting: %v", r.err)
		}
		if r.n != 1 {
			t.Errorf("files = %d, want 1", r.n)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the walk did not end before the deadline")
	}
}
