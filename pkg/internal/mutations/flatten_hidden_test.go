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

// hiddenWidgetsPDF builds a page carrying six widgets that all store an
// appearance: hidden (/F 2), hidden and printed (/F 6), no-view (/F 32),
// no-view and printed (/F 36), no-view, printed and toggled (/F 292), and an
// ordinary printed one (/F 4). Each appearance is a distinct form XObject so
// the content can be searched for which of them were baked in.
func hiddenWidgetsPDF() []byte {
	ap := func(marker string) string {
		body := fmt.Sprintf("%s 0 0 rg 0 0 10 10 re f", marker)
		return fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] /Length %d >>\nstream\n%s\nendstream",
			len(body), body)
	}
	widget := func(name string, flags, x, apNum int) string {
		return fmt.Sprintf("<< /Type /Annot /Subtype /Widget /FT /Tx /T (%s) /V (%s) /Rect [%d 10 %d 20] /F %d"+
			" /AP << /N %d 0 R >> >>", name, name, x, x+10, flags, apNum)
	}
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R 5 0 R 6 0 R 7 0 R 8 0 R 9 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R 5 0 R 6 0 R 7 0 R 8 0 R 9 0 R] >>",
		widget("hidden", 2, 10, 10),
		widget("hiddenprint", 6, 30, 11),
		widget("noview", 32, 50, 12),
		widget("noviewprint", 36, 70, 13),
		widget("toggleprint", 292, 90, 14),
		widget("shown", 4, 110, 15),
		ap("0.1"),
		ap("0.2"),
		ap("0.3"),
		ap("0.4"),
		ap("0.5"),
		ap("0.6"),
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

// TestFlattenFormDropsUndisplayedWidgets pins that flattening neither paints
// nor keeps a widget the viewer never displays.
//
// Per ISO 32000-1, Table 165 the Hidden flag (bit 2) means the annotation is
// neither displayed nor printed, and NoView (bit 6) means it is not displayed
// but still printed when the Print flag (bit 3) is set. Baking either
// appearance into the page makes it visible for good. Keeping the widget left a
// form field in a document the flatten promised had none. Content that shows
// only when printed needs an optional content group whose /AS applies its
// print usage (ISO 32000-1, 8.11.4.4), which ISO 19005-2, 6.9, ISO 14289-1,
// 7.10 and ISO 14289-2, 8.7 forbid, so a printed no-view widget goes too.
//
// # Corpus evidence
//
//   - pdfjs annotation_hidden_noview.pdf: flattening drew both its hidden and
//     its no-view text field, where poppler renders neither in the original.
//   - pdfjs annotation_hidden_noview.pdf, bug1737260.pdf, bug1847733.pdf,
//     widget_hidden_print.pdf and verapdf PDF_UA-1__7.18.1-t03-pass-c.pdf:
//     kept a form field after flattening, for a hidden or no-view widget.
func TestFlattenFormDropsUndisplayedWidgets(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := hiddenWidgetsPDF()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))
	if err := FlattenForm(ctx, alloc, doc); err != nil {
		t.Fatalf("FlattenForm: %v", err)
	}

	pageDict, ok := alloc.Pending()[3].(ast.PdfDict)
	if !ok {
		t.Fatal("the page was not rewritten, so nothing was flattened at all")
	}

	if annotsObj, has := pageDict["Annots"]; has {
		annots, err := document.ResolveArray(ctx, doc, annotsObj)
		if err != nil {
			t.Fatalf("resolving /Annots: %v", err)
		}
		for _, a := range annots {
			d, err := document.ResolveDict(ctx, doc, a)
			if err != nil || d == nil {
				continue
			}
			name, _ := document.ResolveString(ctx, doc, d["T"])
			t.Errorf("the %q widget survived the flattening", document.DecodePdfString(name))
		}
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
	if !strings.Contains(string(body), "/FlatAP") {
		t.Errorf("the displayed widget was not baked in: %q", body)
	}
}
