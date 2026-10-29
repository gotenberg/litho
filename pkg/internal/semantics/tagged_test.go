package semantics

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestIsTagged requires both the structure tree root and /Marked true.
func TestIsTagged(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, catalogExtra string
		want               bool
	}{
		{"tagged", " /StructTreeRoot 4 0 R /MarkInfo << /Marked true >>", true},
		{"marked false", " /StructTreeRoot 4 0 R /MarkInfo << /Marked false >>", false},
		{"no MarkInfo", " /StructTreeRoot 4 0 R", false},
		{"no Marked", " /StructTreeRoot 4 0 R /MarkInfo << >>", false},
		{"no structure tree", " /MarkInfo << /Marked true >>", false},
		{"untagged", "", false},
	} {
		data := buildPDF([]string{
			"<< /Type /Catalog /Pages 2 0 R" + tc.catalogExtra + " >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
			"<< /Type /StructTreeRoot >>",
		})
		doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			t.Fatalf("%s: NewDocument: %v", tc.name, err)
		}
		got, err := IsTagged(context.Background(), doc)
		if err != nil {
			t.Fatalf("%s: IsTagged: %v", tc.name, err)
		}
		if got != tc.want {
			t.Errorf("%s: IsTagged = %t, want %t", tc.name, got, tc.want)
		}
	}
}
