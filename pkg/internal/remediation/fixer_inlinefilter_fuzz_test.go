package remediation

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/filter"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/limits"
)

// FuzzReencodeInlineImages asserts the re-encoding of inline images never
// panics on crafted bytes, leaves a stream it does not rewrite as written,
// keeps every operation, and leaves nothing to rewrite a second time.
func FuzzReencodeInlineImages(f *testing.F) {
	lzw := "\x80\x00\x08\x08\x07\xfb\xfd\x00\x40\x00\x40\x40"
	f.Add([]byte("q BI /W 4 /H 2 /BPC 8 /CS /G /F /LZW ID "+lzw+" EI Q"), 2)
	f.Add([]byte("q BI /W 4 /H 2 /BPC 8 /CS /G /F /LZW ID "+lzw[:5]), 1) // Truncated inside the image data.
	f.Fuzz(func(t *testing.T, data []byte, part int) {
		ctx := context.Background()
		part = 1 + int(uint(part)%4)
		dec := filter.NewDecoder(nil, limits.Default())
		out, n := reencodeInlineImages(ctx, data, part, dec)
		if n == 0 {
			if !bytes.Equal(out, data) {
				t.Fatalf("rewrote nothing in %q, but wrote %q", data, out)
			}
			return
		}
		before, err := content.ParseStreamData(ctx, data, content.ForRepair())
		if err != nil {
			t.Fatalf("rewrote %q, which does not parse: %v", data, err)
		}
		after, err := content.ParseStreamData(ctx, out, content.ForRepair())
		if err != nil || len(after) != len(before) {
			t.Fatalf("the rewrite of %q read %d operations as %d (%v): %q", data, len(before), len(after), err, out)
		}
		if _, again := reencodeInlineImages(ctx, out, part, dec); again != 0 {
			t.Fatalf("the rewrite of %q left %d images to rewrite: %q", data, again, out)
		}
	})
}
