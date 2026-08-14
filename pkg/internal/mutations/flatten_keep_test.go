package mutations

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// valuedWidgetNoAPPDF builds a page whose only content is a text field carrying
// a value and no appearance stream at all, as pdfjs annotation-tx.pdf does.
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

// TestFlattenFormKeepsAWidgetItCannotBake pins that a widget with no appearance
// survives the flatten instead of being deleted.
//
// A viewer draws a variable-text field from /V and /DA when it stores no
// appearance (ISO 32000-1, 12.7.3.3). litho has nothing to bake into the page
// for such a widget, and it dropped the annotation anyway: on pdfjs
// annotation-tx.pdf that removed the only text the page had, and `litho
// flatten-form` reported success on a document it had just emptied.
func TestFlattenFormKeepsAWidgetItCannotBake(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := valuedWidgetNoAPPDF()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber())
	if err := FlattenForm(ctx, alloc, doc); err != nil {
		t.Fatalf("FlattenForm: %v", err)
	}

	// The page is only rewritten when something changed; either way the widget
	// must still be reachable from /Annots.
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
	if len(annots) != 1 {
		t.Fatalf("page kept %d annotations, want the 1 widget that could not be baked", len(annots))
	}
}
