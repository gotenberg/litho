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

// valuedWidgetNoAPPDF builds a page whose only content is a text field carrying
// a value and no appearance stream at all.
//
// # Corpus evidence
//
//   - pdfjs annotation-tx.pdf: has this shape.
func valuedWidgetNoAPPDF() []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] /NeedAppearances true >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (Text2) /V (tx annotation) " +
			"/Rect [21.7 710.7 171.7 732.7] /F 4 >>",
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

// TestFlattenFormDrawsAWidgetItCannotBake pins that flattening a widget with no
// stored appearance keeps what it showed on the page.
//
// A viewer draws a variable-text field from /V and /DA when it stores no
// appearance (ISO 32000-1, 12.7.3.3). litho used to drop such an annotation,
// which could remove the only text a page had. It then kept the widget instead,
// which preserved the value but left the form behind so the flatten never
// finished. litho now builds the appearance from the value and bakes that in,
// which satisfies the same guarantee: the text this widget showed is still on
// the page afterwards.
//
// # Corpus evidence
//
//   - pdfjs annotation-tx.pdf: dropping the annotation removed the only text
//     the page had.
func TestFlattenFormDrawsAWidgetItCannotBake(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := valuedWidgetNoAPPDF()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))
	if err := FlattenForm(ctx, alloc, doc); err != nil {
		t.Fatalf("FlattenForm: %v", err)
	}

	// Either outcome preserves the value: the widget still carries it, or the
	// page now draws it. What must never happen is both going away. A page left
	// untouched is the first of those, since its /Annots still names the widget.
	pageDict, ok := alloc.Pending()[3].(ast.PdfDict)
	if !ok {
		catalog, cerr := document.ResolveCatalog(ctx, doc)
		if cerr != nil {
			t.Fatalf("ResolveCatalog: %v", cerr)
		}
		pages, perr := document.ResolvePages(ctx, doc, catalog)
		if perr != nil || len(pages) == 0 {
			t.Fatalf("ResolvePages: %v", perr)
		}
		pageDict = pages[0].Dict
	}

	annots, err := document.ResolveArray(ctx, doc, pageDict["Annots"])
	if err != nil {
		t.Fatalf("resolving /Annots: %v", err)
	}
	if len(annots) == 1 {
		return // Kept, as older builds did.
	}

	res, _ := pageDict["Resources"].(ast.PdfDict)
	xobjs, _ := res["XObject"].(ast.PdfDict)
	if len(xobjs) == 0 {
		t.Fatal("the widget was removed and nothing was drawn: the page lost its only text")
	}

	drawn := ""
	for _, obj := range xobjs {
		ref, isRef := obj.(ast.PdfIndirectRef)
		if !isRef {
			continue
		}
		stream, isStream := alloc.Pending()[ref.ObjectNum].(ast.PdfStream)
		if !isStream {
			continue
		}
		raw, err := stream.Bytes(ctx)
		if err != nil {
			t.Fatalf("reading the baked appearance: %v", err)
		}
		drawn += string(raw)
	}
	if !strings.Contains(drawn, "(tx annotation) Tj") {
		t.Errorf("the flattened page does not draw the field value: %q", drawn)
	}
}
