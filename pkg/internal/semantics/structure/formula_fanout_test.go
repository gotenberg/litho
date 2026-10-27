package structure

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// formulaTreeDoc builds a tagged document whose structure tree root lists
// its kids in object 4, written as objs[3].
func formulaTreeDoc(t *testing.T, kids string, more ...string) *document.Document {
	t.Helper()
	objs := append([]string{
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 5 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
		kids,
		"<< /Type /StructTreeRoot /K 4 0 R >>",
	}, more...)
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs))
	for i, o := range objs {
		offs[i] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for _, off := range offs {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(buf.Bytes()), int64(buf.Len()), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	return doc
}

// TestExtractFormulas_StructureCycles pins that the formula search walks each
// structure object once. An element naming itself in /K recursed until the
// goroutine stack was exhausted, a fatal error no recover catches, and direct
// elements naming their own /K array again fanned out 2^depth.
func TestExtractFormulas_StructureCycles(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		kids string
		more []string
	}{
		{"element naming itself", "[6 0 R]", []string{"<< /Type /StructElem /S /Formula /K [0 6 0 R] >>"}},
		{"array naming itself", "[4 0 R 4 0 R 6 0 R]", []string{"<< /Type /StructElem /S /Formula /K 0 >>"}},
		{"shared array", "[<< /S /Div /K 4 0 R >> << /S /Div /K 4 0 R >> 6 0 R]", []string{"<< /Type /StructElem /S /Formula /K 0 >>"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			got, err := ExtractFormulas(ctx, formulaTreeDoc(t, tt.kids, tt.more...))
			if ctx.Err() != nil {
				t.Fatal("the walk did not end before the deadline")
			}
			if err != nil {
				t.Fatalf("ExtractFormulas: %v", err)
			}
			if len(got) != 1 || len(got[0].Content) != 1 || got[0].Content[0].MCID != 0 {
				t.Errorf("got %+v, want the one Formula with marked content 0", got)
			}
		})
	}
}
