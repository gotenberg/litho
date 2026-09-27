package remediation

import (
	"context"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestWalkFormTooltipFields_SharedKidsArrayFanOut pins that the tooltip fixer
// reads a /Kids array written as an object of its own once: its direct fields
// each name it again as their /Kids, and read anew wherever it is reached the
// walk recursed with no bound. The indirect field beside the repeat is still
// patched.
func TestWalkFormTooltipFields_SharedKidsArrayFanOut(t *testing.T) {
	t.Parallel()
	data := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [5 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
		"[<< /T (a) /Kids 4 0 R >> << /T (d) /Kids 4 0 R >> 6 0 R]",
		"<< /T (p) /TU (p) /Kids 4 0 R >>",
		"<< /T (c) /FT /Tx >>",
	)
	doc, alloc := newTestDoc(t, data)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	count := 0
	walkFormTooltipFields(ctx, alloc, doc, nil, ast.PdfArray{ast.PdfIndirectRef{ObjectNum: 5}}, &count, make(map[int]bool), 0)
	if ctx.Err() != nil {
		t.Fatal("the walk did not end before the deadline")
	}
	if count != 1 {
		t.Errorf("patched %d fields, want 1, field c", count)
	}
	if c, _ := alloc.Pending()[6].(ast.PdfDict); c["TU"] == nil {
		t.Errorf("field c = %v, want a /TU", c)
	}
}
