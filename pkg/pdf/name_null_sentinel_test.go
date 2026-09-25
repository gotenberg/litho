package pdf

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

// TestWriteTo_NameHoldingNullIsMatchable pins that a full rewrite of a
// document whose "#00" escape decodes to a name holding NUL fails with
// [ErrNameHoldsNull], which a caller can match, rather than writing a name
// ISO 32000-1, 7.3.5 excludes. The same document opens, and a rewrite of its
// NUL-free twin succeeds, so the refusal is about the name alone.
func TestWriteTo_NameHoldingNullIsMatchable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	for _, tc := range []struct {
		key     string
		refused bool
	}{
		{"/X#00Y", true},
		{"/XY", false},
	} {
		t.Run(tc.key, func(t *testing.T) {
			t.Parallel()
			data := limitsPDF(
				"<< /Type /Catalog /Pages 2 0 R "+tc.key+" 1 >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
			)
			r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer r.Close()

			var out bytes.Buffer
			err = NewWriter(r).WriteTo(ctx, &out, WithFullRewrite())
			if got := errors.Is(err, ErrNameHoldsNull); got != tc.refused {
				t.Fatalf("errors.Is(err, ErrNameHoldsNull) = %v, want %v (err = %v)", got, tc.refused, err)
			}
			if !tc.refused && err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
		})
	}
}
