package annotation

import (
	"testing"
	"time"
)

// TestExtractDocumentScripts_SharedKidsArrayFanOut pins that the JavaScript
// name-tree walk visits a /Kids array written as an object of its own once:
// its direct nodes each name it again as their own /Kids, and walked anew
// wherever it is reached the array fans out 2^depth.
func TestExtractDocumentScripts_SharedKidsArrayFanOut(t *testing.T) {
	t.Parallel()

	ctx, doc := widgetDoc(t, jsTreeObjs(
		"<< /Kids 6 0 R >>",
		"[<< /Kids 6 0 R >> << /Kids 6 0 R >> << /Names [(a) 7 0 R] >>]",
		"<< /S /JavaScript /JS (app.alert\\(1\\);) >>",
	))

	type result struct {
		n   int
		err error
	}
	done := make(chan result, 1)
	go func() {
		scripts, err := ExtractDocumentScripts(ctx, doc)
		done <- result{len(scripts), err}
	}()
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("extracting scripts: %v", r.err)
		}
		if r.n != 1 {
			t.Errorf("scripts = %d, want 1", r.n)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the walk did not end before the deadline")
	}
}
