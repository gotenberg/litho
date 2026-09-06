package mutations

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// hiddenWidgetsPDF builds a page carrying three widgets that all store an
// appearance: one hidden (/F 2), one no-view (/F 32) and one ordinary (/F 4).
// Each appearance is a distinct form XObject so the content can be searched for
// which of them were baked in.
func hiddenWidgetsPDF() []byte {
	ap := func(num int, marker string) string {
		body := fmt.Sprintf("%s 0 0 rg 0 0 10 10 re f", marker)
		return fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] /Length %d >>\nstream\n%s\nendstream",
			len(body), body)
	}
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R 5 0 R 6 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R 5 0 R 6 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (hidden) /Rect [10 10 20 20] /F 2" +
			" /AP << /N 7 0 R >> >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (noview) /Rect [30 10 40 20] /F 32" +
			" /AP << /N 8 0 R >> >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (shown) /Rect [50 10 60 20] /F 4" +
			" /AP << /N 9 0 R >> >>",
		ap(7, "0.1"),
		ap(8, "0.2"),
		ap(9, "0.3"),
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

// TestFlattenFormLeavesUndisplayedWidgetsAlone pins that flattening does not
// paint a widget the viewer never shows.
//
// Per ISO 32000-1, Table 165 the Hidden flag (bit 2) means the annotation is
// neither displayed nor printed, and NoView (bit 6) means it is not displayed
// but still printed. Baking either appearance into the page makes it visible for
// good, which for a hidden field puts a value on the page nobody could see
// before. Flattening pdfjs annotation_hidden_noview.pdf drew both its hidden and
// its no-view text field, where poppler renders neither in the original; after
// the fix the flattened render is pixel-identical to it.
//
// Both are kept as annotations rather than dropped: hidden shows nothing either
// way, but no-view still prints, and dropping it would lose that.
func TestFlattenFormLeavesUndisplayedWidgetsAlone(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := hiddenWidgetsPDF()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber())
	if err := FlattenForm(ctx, alloc, doc); err != nil {
		t.Fatalf("FlattenForm: %v", err)
	}

	pageDict, ok := alloc.Pending()[3].(ast.PdfDict)
	if !ok {
		t.Fatal("the page was not rewritten, so nothing was flattened at all")
	}

	annots, err := document.ResolveArray(ctx, doc, pageDict["Annots"])
	if err != nil {
		t.Fatalf("resolving /Annots: %v", err)
	}
	kept := map[string]bool{}
	for _, a := range annots {
		d, err := document.ResolveDict(ctx, doc, a)
		if err != nil || d == nil {
			continue
		}
		name, _ := document.ResolveString(ctx, doc, d["T"])
		kept[document.DecodePdfString(name)] = true
	}
	for _, want := range []string{"hidden", "noview"} {
		if !kept[want] {
			t.Errorf("the %q widget was removed; it is not displayed, so its appearance was baked in", want)
		}
	}
	if kept["shown"] {
		t.Error("the displayed widget was not flattened")
	}

	// The page content must invoke exactly one appearance: the shown widget's.
	// Flattening writes a fresh content stream, so it lives in the allocator
	// rather than the document's cross-reference table.
	var body []byte
	switch c := pageDict["Contents"].(type) {
	case ast.PdfIndirectRef:
		pending, ok := alloc.Pending()[c.ObjectNum].(ast.PdfStream)
		if !ok {
			t.Fatalf("object %d is not a pending stream", c.ObjectNum)
		}
		body, err = pending.Bytes(ctx)
		if err != nil {
			t.Fatalf("reading the flattened content: %v", err)
		}
	case ast.PdfArray:
		for _, part := range c {
			ref, isRef := part.(ast.PdfIndirectRef)
			if !isRef {
				continue
			}
			if pending, ok := alloc.Pending()[ref.ObjectNum].(ast.PdfStream); ok {
				chunk, berr := pending.Bytes(ctx)
				if berr != nil {
					continue
				}
				body = append(body, chunk...)
				continue
			}
			if stream, serr := document.ResolveStream(ctx, doc, part); serr == nil {
				chunk, derr := doc.DecodeStream(ctx, stream)
				if derr == nil {
					body = append(body, chunk...)
				}
			}
		}
	default:
		t.Fatalf("unexpected /Contents type %T", c)
	}
	if n := strings.Count(string(body), " Do"); n != 1 {
		t.Errorf("content invokes %d appearances, want 1 (only the displayed widget): %q", n, body)
	}
}
