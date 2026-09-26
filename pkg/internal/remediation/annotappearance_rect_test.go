package remediation

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// rectAnnotPDF builds a page holding one Square annotation with no appearance
// dictionary, whose /Rect is written as the caller specifies.
func rectAnnotPDF(rect string) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Square /Rect " + rect + " /F 4 >>",
		"0", "0", "100", "100",
	}

	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	start := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, start)
	return b.Bytes()
}

// appearanceFor runs the appearance fixer over the fixture and returns the
// annotation dictionary it queued, or nil when it left the annotation alone.
func appearanceFor(t *testing.T, rect string) ast.PdfDict {
	t.Helper()

	ctx := context.Background()
	data := rectAnnotPDF(rect)
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))
	if _, err := fixAnnotAppearance(ctx, alloc, doc, Options{}, nil, ""); err != nil {
		t.Fatalf("fixAnnotAppearance: %v", err)
	}
	patched, _ := alloc.Pending()[4].(ast.PdfDict)
	return patched
}

// TestAppearanceBuiltForIndirectRect pins that an annotation whose /Rect
// coordinates are indirect references still gets an appearance, sized from its
// real rectangle.
//
// ISO 19005-2, 6.3.3 requires an appearance dictionary, and litho exempts an
// annotation enclosing no area. ISO 32000-1, 7.3.9 allows an indirect reference
// wherever a direct object may appear, and reading those references as numbers
// gives zero for every corner: a real 100 by 100 annotation looked empty, so the
// fixer skipped it and the file stayed non-conformant. Any appearance it did
// build would have carried a 1 by 1 /BBox rather than the annotation's size.
func TestAppearanceBuiltForIndirectRect(t *testing.T) {
	t.Parallel()

	direct := appearanceFor(t, "[0 0 100 100]")
	if direct == nil {
		t.Fatal("the direct annotation got no appearance, so the fixture proves nothing")
	}

	indirect := appearanceFor(t, "[5 0 R 6 0 R 7 0 R 8 0 R]")
	if indirect == nil {
		t.Fatal("the annotation with indirect coordinates got no appearance: " +
			"it was treated as enclosing no area and skipped")
	}
	if _, ok := indirect["AP"]; !ok {
		t.Errorf("the queued annotation has no /AP: %v", indirect)
	}
}

// TestAppearanceSkippedForGenuinelyEmptyRect is the control: an annotation that
// really does enclose no area keeps the exemption and gains no appearance.
func TestAppearanceSkippedForGenuinelyEmptyRect(t *testing.T) {
	t.Parallel()

	if got := appearanceFor(t, "[10 10 10 10]"); got != nil {
		if _, added := got["AP"]; added {
			t.Errorf("an empty annotation was given an appearance: %v", got)
		}
	}
}

// TestNormalizeBBoxUsesTheRectSize pins the size the generated appearance
// carries, so a resolved rectangle is not silently reduced to the 1x1 fallback.
func TestNormalizeBBoxUsesTheRectSize(t *testing.T) {
	t.Parallel()

	box := normalizeBBox(ast.PdfArray{
		ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(100), ast.PdfInt(100),
	})
	if len(box) != 4 {
		t.Fatalf("normalizeBBox returned %v", box)
	}
	if document.Float(box[2]) != 100 || document.Float(box[3]) != 100 {
		t.Errorf("/BBox = %v, want [0 0 100 100]", box)
	}
}
