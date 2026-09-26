package remediation

import (
	"bytes"
	"context"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// FuzzSpliceRepairedOperations asserts the splice of the undefined-operator
// repair never panics on crafted bytes, and that a splice it lets stand
// carries every inline image across byte for byte, from its BI to its EI.
func FuzzSpliceRepairedOperations(f *testing.F) {
	f.Add([]byte("q BI /W 4 /H 1 /BPC 8 /CS /G ID \x00\xff)\r EI EI Q zz -. 2 Td ETBT 0 0 1 1 re%c\nf) BI /W 1 /H 1 /BPC 8 /CS /G ID \x01 EI"))
	f.Add([]byte("q BI /W 4 /H 1 /BPC 8 /CS /G ID \x00")) // Truncated inside the image data.
	f.Fuzz(func(t *testing.T, data []byte) {
		ctx := context.Background()
		out, _, ok := spliceRepairedOperations(ctx, data)
		if !ok {
			return
		}
		if in, got := inlineImageBytes(ctx, data), inlineImageBytes(ctx, out); !slices.EqualFunc(in, got, bytes.Equal) {
			t.Fatalf("the splice of %q changed its inline images:\n  %q\nto\n  %q", data, in, got)
		}
	})
}

// inlineImageBytes returns the bytes of every inline image the reading of a
// repair reads in data, each from its BI to its EI.
func inlineImageBytes(ctx context.Context, data []byte) [][]byte {
	ops, ranges, err := content.ParseStreamDataWithOffsets(ctx, data, content.ForRepair())
	if err != nil {
		return nil
	}
	var images [][]byte
	for i, op := range ops {
		if op.Operator != "BI" {
			continue
		}
		span := data[ranges[i].Start:ranges[i].End]
		if bi, ok := inlineImageStart(span); ok {
			images = append(images, span[bi:])
		}
	}
	return images
}
