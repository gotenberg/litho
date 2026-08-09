package typography

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// unbalancedFormPDF builds a page that draws text, invokes a form XObject
// which opens an /Artifact span and never closes it, then draws more text.
func unbalancedFormPDF() []byte {
	form := "/Artifact BMC\nBT /F1 12 Tf 10 10 Td (INSIDEFORM) Tj ET\n"
	page := "BT /F1 12 Tf 50 700 Td (BEFOREDO) Tj ET\nq /Fm1 Do Q\n" +
		"BT /F1 12 Tf 50 600 Td (AFTERDO) Tj ET\n"

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R " +
			"/Resources << /Font << /F1 5 0 R >> /XObject << /Fm1 6 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(page), page),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 200 50] "+
			"/Resources << /Font << /F1 5 0 R >> >> /Length %d >>\nstream\n%sendstream", len(form), form),
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

// TestExtractElements_FormArtifactDoesNotEscape pins that a form XObject
// leaving an /Artifact span open does not suppress the page text that follows
// its Do.
//
// Marked-content operators shall be balanced within a content stream (ISO
// 32000-1, 14.6), so a form's spans end with the form. The skip flag is derived
// from the marked-content stack, so without closing the form's leak every later
// text run on the page is discarded as decoration.
func TestExtractElements_FormArtifactDoesNotEscape(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := unbalancedFormPDF()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil || len(pages) == 0 {
		t.Fatalf("ResolvePages: %v (%d pages)", err, len(pages))
	}
	page := pages[0]

	ops, err := content.ParsePageContent(ctx, doc, page.Dict)
	if err != nil {
		t.Fatalf("ParsePageContent: %v", err)
	}

	machine := graphics.NewMachine([4]float64{0, 0, 612, 792})
	machine.StripArtifacts = true
	elements, err := ExtractElements(ctx, doc, machine, page, ops)
	if err != nil {
		t.Fatalf("ExtractElements: %v", err)
	}

	var joined strings.Builder
	for _, e := range elements {
		joined.WriteString(e.Text)
	}
	got := joined.String()
	if !strings.Contains(got, "BEFOREDO") {
		t.Errorf("extracted %q, want it to contain %q", got, "BEFOREDO")
	}
	if !strings.Contains(got, "AFTERDO") {
		t.Errorf("the form's unclosed /Artifact swallowed the rest of the page: extracted %q", got)
	}
	if strings.Contains(got, "INSIDEFORM") {
		t.Errorf("text inside the form's /Artifact span is decoration and must be stripped: extracted %q", got)
	}
}
