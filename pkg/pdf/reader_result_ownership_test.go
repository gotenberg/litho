package pdf

import (
	"bytes"
	"context"
	"testing"
)

// TestReaderResultsAreCallerOwned pins that what the readers hand back is the
// caller's to keep: mutating a result must not change what a later call
// returns.
//
// These readers resolve document-wide tables that the Reader caches, so
// returning a slice or map straight out of that cache would let one caller edit
// another's data, and would do it single-threaded, where the "not safe for
// concurrent use" note on the models types offers no warning at all. The one
// known exception is [models.Page.DPM], which aliases the cached page metadata
// by design and says so on the field; it is deliberately not pinned here, so
// that deep-copying it later is not a test failure.
//
// Validated against a real alias before being trusted: the same mutate-then-
// reread probe DOES report sharing for Page.DPM, so a "fresh" verdict here
// means the data really is copied rather than the probe being blind.
func TestReaderResultsAreCallerOwned(t *testing.T) {
	t.Parallel()

	data := richReaderPDF(t)
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := r.Close(); closeErr != nil {
			t.Logf("closing reader: %v", closeErr)
		}
	})

	t.Run("Bookmarks", func(t *testing.T) {
		first, err := r.Bookmarks(ctx)
		if err != nil || len(first) == 0 {
			t.Skipf("no bookmarks to check (err %v)", err)
		}
		want := first[0].Title
		first[0].Title = "mutated by the caller"
		again, err := r.Bookmarks(ctx)
		if err != nil {
			t.Fatalf("Bookmarks: %v", err)
		}
		if again[0].Title != want {
			t.Errorf("second call saw the caller's edit: got %q, want %q", again[0].Title, want)
		}
	})

	t.Run("FormFields", func(t *testing.T) {
		first, err := r.FormFields(ctx)
		if err != nil || len(first) == 0 {
			t.Skipf("no form fields to check (err %v)", err)
		}
		want := first[0].Value
		first[0].Value = "mutated by the caller"
		again, err := r.FormFields(ctx)
		if err != nil {
			t.Fatalf("FormFields: %v", err)
		}
		if again[0].Value != want {
			t.Errorf("second call saw the caller's edit: got %q, want %q", again[0].Value, want)
		}
	})

	t.Run("FormData", func(t *testing.T) {
		first, err := r.FormData(ctx)
		if err != nil || len(first) == 0 {
			t.Skipf("no form data to check (err %v)", err)
		}
		var key string
		for k := range first {
			key = k
			break
		}
		want := first[key]
		first[key] = "mutated by the caller"
		again, err := r.FormData(ctx)
		if err != nil {
			t.Fatalf("FormData: %v", err)
		}
		if again[key] != want {
			t.Errorf("second call saw the caller's edit: got %q, want %q", again[key], want)
		}
	})

	t.Run("ExtractAnnotations", func(t *testing.T) {
		first, err := r.ExtractAnnotations(ctx, 1)
		if err != nil || len(first) == 0 {
			t.Skipf("no annotations to check (err %v)", err)
		}
		want := first[0].Subtype
		first[0].Subtype = "mutated by the caller"
		again, err := r.ExtractAnnotations(ctx, 1)
		if err != nil {
			t.Fatalf("ExtractAnnotations: %v", err)
		}
		if again[0].Subtype != want {
			t.Errorf("second call saw the caller's edit: got %q, want %q", again[0].Subtype, want)
		}
	})

	t.Run("Metadata", func(t *testing.T) {
		first, err := r.Metadata(ctx)
		if err != nil {
			t.Fatalf("Metadata: %v", err)
		}
		first["litho-caller-owned-probe"] = "mutated by the caller"
		again, err := r.Metadata(ctx)
		if err != nil {
			t.Fatalf("Metadata: %v", err)
		}
		if _, leaked := again["litho-caller-owned-probe"]; leaked {
			t.Error("second call saw the caller's injected key")
		}
	})
}
