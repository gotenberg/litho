package remediation

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

// markedLangPDF builds a tagged page whose content stream marks a span with an
// inline /Lang, the shape of PDF_UA-1__7.2-t29-fail-g. tag is the language
// value written into the property list. When named is true the property list is
// reached by name through /Resources /Properties instead of inline.
func markedLangPDF(tag string, named bool) []byte {
	body := "BT /F1 12 Tf 10 700 Td (hello) Tj ET"
	marked := fmt.Sprintf("/Span <</Lang (%s)>> BDC\n%s\nEMC\n", tag, body)
	props := ""
	if named {
		marked = fmt.Sprintf("/Span /MC0 BDC\n%s\nEMC\n", body)
		props = " /Properties << /MC0 7 0 R >>"
	}

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /Lang (en) /StructTreeRoot 5 0 R /MarkInfo << /Marked true >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R" +
			" /Resources <<" + props + " >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(marked), marked),
		"<< /Type /StructTreeRoot /K [6 0 R] >>",
		"<< /Type /StructElem /S /Document /P 5 0 R >>",
		fmt.Sprintf("<< /Lang (%s) >>", tag),
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

// runMarkedLangFix applies the /Lang fixer and returns the rewritten content
// stream plus the allocator.
func runMarkedLangFix(t *testing.T, tag string, named bool, profile string) (string, *writer.ObjAllocator) {
	t.Helper()

	ctx := context.Background()
	data := markedLangPDF(tag, named)
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber())
	if _, err := fixCatalogLang(ctx, alloc, doc, Options{}, nil, profile); err != nil {
		t.Fatalf("fixCatalogLang: %v", err)
	}

	stream, ok := alloc.Pending()[4].(ast.PdfStream)
	if !ok {
		return "", alloc
	}
	raw, err := stream.Bytes(ctx)
	if err != nil {
		t.Fatalf("reading the rewritten content stream: %v", err)
	}
	return string(raw), alloc
}

// TestFixLangStripsAnInlineMarkedContentTag pins that an unusable /Lang on a
// BDC property list is removed.
//
// A /Lang may be given inline on BDC (ISO 32000-1, 14.6.2) and the language
// identifier rules of 14.9.2 reach it. The repair only walked the structure
// tree, so PDF_UA-1__7.2-t29-fail-g through -p came out of remediation still
// carrying tags like "-pt", which litho and veraPDF both keep reporting.
func TestFixLangStripsAnInlineMarkedContentTag(t *testing.T) {
	t.Parallel()

	got, _ := runMarkedLangFix(t, "-pt", false, "PDF/UA-1")
	if got == "" {
		t.Fatal("the content stream was not rewritten")
	}
	if strings.Contains(got, "/Lang") {
		t.Errorf("the marked-content /Lang survived:\n%s", got)
	}
	// The span itself, and everything it wraps, must still be there.
	for _, want := range []string{"/Span", "BDC", "(hello) Tj", "EMC"} {
		if !strings.Contains(got, want) {
			t.Errorf("the rewritten stream lost %q:\n%s", want, got)
		}
	}
}

// TestFixLangKeepsAValidMarkedContentTag is the control: a usable tag is left
// alone, and with nothing to repair the content stream is not rewritten at all.
func TestFixLangKeepsAValidMarkedContentTag(t *testing.T) {
	t.Parallel()

	got, _ := runMarkedLangFix(t, "pt-BR", false, "PDF/UA-1")
	if got != "" {
		t.Errorf("a page with a valid /Lang had its content stream rewritten:\n%s", got)
	}
}

// TestFixLangStripsANamedPropertyListTag pins the other form: a property list
// reached by name is an ordinary object, so it is repaired without touching
// the content stream.
func TestFixLangStripsANamedPropertyListTag(t *testing.T) {
	t.Parallel()

	got, alloc := runMarkedLangFix(t, "1-pt", true, "PDF/UA-1")
	if got != "" {
		t.Errorf("a named property list should not require a content rewrite:\n%s", got)
	}
	props, ok := alloc.Pending()[7].(ast.PdfDict)
	if !ok {
		t.Fatalf("the named property list was not rewritten: %T", alloc.Pending()[7])
	}
	if _, still := props["Lang"]; still {
		t.Errorf("the named property list kept /Lang = %v", props["Lang"])
	}
}

// TestFixLangLeavesMarkedContentAloneOutsidePDFUA is the control for the
// profile gate: under PDF/A an empty tag means "unknown", which the spec
// allows, so nothing is rewritten.
func TestFixLangLeavesMarkedContentAloneOutsidePDFUA(t *testing.T) {
	t.Parallel()

	got, _ := runMarkedLangFix(t, "", false, "PDF/A-2a")
	if got != "" {
		t.Errorf("an empty tag was rewritten under PDF/A:\n%s", got)
	}
}
