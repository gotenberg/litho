package structure

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// TestExtractFormulas_ResolvesPages pins that each MCID of a formula carries
// the page that holds it. MCIDs number marked content within one page (ISO
// 32000-1, 14.7.4.2), so the same MCID on two pages names two sequences, and
// without the page a caller could not tell them apart.
func TestExtractFormulas_ResolvesPages(t *testing.T) {
	t.Parallel()

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 5 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
		"<< /Type /StructTreeRoot /K [6 0 R 7 0 R] >>",
		// Two formulas share MCID 0: one on page 1, one on page 2.
		"<< /Type /StructElem /S /Formula /Pg 3 0 R /K 0 >>",
		// The second also holds a marked-content reference that names page 1
		// on its own, overriding the element's page (Table 324).
		"<< /Type /StructElem /S /Formula /Pg 4 0 R /K [0 << /Type /MCR /Pg 3 0 R /MCID 4 >>] >>",
	}
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

	got, err := ExtractFormulas(context.Background(), doc)
	if err != nil {
		t.Fatalf("ExtractFormulas: %v", err)
	}
	want := [][]models.MarkedContentRef{
		{{Page: 1, MCID: 0}},
		{{Page: 2, MCID: 0}, {Page: 1, MCID: 4}},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d formulas, want %d", len(got), len(want))
	}
	for i := range want {
		if !slices.Equal(got[i].Content, want[i]) {
			t.Errorf("formula %d: got %v, want %v", i, got[i].Content, want[i])
		}
	}
}
