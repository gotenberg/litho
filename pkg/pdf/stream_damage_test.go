package pdf

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

// TestEmbeddedFileThatDoesNotDecodeIsDamaged holds the Open function of an
// embedded file whose data does not decode to [ErrDamagedStream]: it once
// returned the decoder's bare error, which a service answered as its own
// failure.
func TestEmbeddedFileThatDoesNotDecodeIsDamaged(t *testing.T) {
	t.Parallel()

	data := colorFixturePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /Names << /EmbeddedFiles << /Names [(a.xml) 4 0 R] >> >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /Filespec /F (a.xml) /EF << /F 5 0 R >> >>",
		colorStream("/Type /EmbeddedFile /Filter /FlateDecode", "not zlib data at all"),
	})
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })

	files, err := r.EmbeddedFiles(context.Background())
	if err != nil || len(files) != 1 {
		t.Fatalf("EmbeddedFiles = %d files, %v; want one", len(files), err)
	}
	rc, err := files[0].Open(context.Background())
	if err == nil {
		_ = rc.Close()
		t.Fatal("Open succeeded on data that does not decode")
	}
	if !errors.Is(err, ErrDamagedStream) || !IsInvalidInput(err) {
		t.Errorf("Open error = %v, want an invalid input wrapping ErrDamagedStream", err)
	}
}
