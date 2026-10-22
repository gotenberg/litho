package document

import (
	"errors"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/panicerr"
)

// TestErrInternalPanic_MatchesACodecWorkerPanic checks a panic a codec's
// worker recovers, which wraps the panicerr sentinel since the codec cannot
// import this package, matches ErrInternalPanic, and so pdf.ErrInternalPanic.
func TestErrInternalPanic_MatchesACodecWorkerPanic(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("JPXDecode: %w", &panicerr.Error{Where: "worker", Value: "boom"})
	if !errors.Is(err, ErrInternalPanic) {
		t.Fatalf("errors.Is(%v, ErrInternalPanic) = false", err)
	}
}
