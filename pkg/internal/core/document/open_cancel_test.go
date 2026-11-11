package document

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

// TestNewDocument_ACancelledOpenIsNotADamagedFile pins that an open the
// context ends reports the context's error, not a damaged cross-reference
// table: the file was never judged. A caller that answers a damaged file as
// the client's mistake would otherwise blame the client for its own timeout.
func TestNewDocument_ACancelledOpenIsNotADamagedFile(t *testing.T) {
	t.Parallel()

	data := unlistedObjectsPDF(0, 0)
	if _, err := NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil); err != nil {
		t.Fatalf("the fixture does not open: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("NewDocument with a cancelled context: %v, want context.Canceled", err)
	}
	if errors.Is(err, ErrDamagedXRef) {
		t.Errorf("a cancelled open reports a damaged cross-reference table: %v", err)
	}
}
