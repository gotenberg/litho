package remediation

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// langFormPDF builds a page drawing form 5, whose content marks a span with an
// unusable inline /Lang and names the property lists /P0, object 6, and /P1,
// which the page's /Resources write directly. formDict holds the form's
// dictionary entries besides /Length, and body follows the marked content.
func langFormPDF(formDict, body string) []byte {
	content := "/Span <</Lang (5v)>> BDC EMC /Span /P0 BDC EMC /Span /P1 BDC EMC" + body
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /Lang (en) >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R"+
			" /Resources << /XObject << /Fm0 5 0 R >> /Properties << /P0 6 0 R /P1 << /Lang (4w) >> >> >> >>",
		contentObj("/Fm0 Do /Span /P1 BDC EMC"),
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] %s /Length %d >>\nstream\n%s\nendstream",
			formDict, len(content), content),
		"<< /Lang (2y) >>",
	)
}

// formPropertyList returns the property list /P1 as the output will carry it
// in the form's own /Resources.
func formPropertyList(t *testing.T, alloc *writer.ObjAllocator, doc *document.Document) ast.PdfDict {
	t.Helper()

	ctx := context.Background()
	res := liveOwnerResources(t, alloc, doc, 5)
	p1 := liveDictValue(ctx, alloc, doc, liveDictValue(ctx, alloc, doc, res["Properties"])["P1"])
	if p1 == nil {
		t.Fatalf("/P1 is gone from the form's /Resources: %v", res)
	}
	return p1
}

// TestFixMarkedContentLang_KeepsNestedResourcesFormWrite pins that the /Lang
// repairs of a form keep the /Resources nested-resources (registry 53) gave
// the form ahead of marked-content-lang (63).
//
// Both write the form stream: nested-resources its dictionary, the /Lang
// repair its body and the property lists the copied /Resources write
// directly. A write built on the document's stream would drop the /Resources
// the form now needs to resolve /P0 and /P1 (ISO 19005-2, 6.2.2 t2), or bring
// the repaired body's /Lang back.
func TestFixMarkedContentLang_KeepsNestedResourcesFormWrite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, langFormPDF("", ""))
	if _, err := fixNestedResources(ctx, alloc, doc, Options{}, nil, ""); err != nil {
		t.Fatalf("fixNestedResources: %v", err)
	}
	if dict, _ := liveStreamBody(t, alloc, doc, 5); dict["Resources"] == nil {
		t.Fatal("control: nested-resources gave the form no /Resources, so there is no earlier edit to lose")
	}
	if _, err := fixMarkedContentLang(ctx, alloc, doc, Options{}, nil, "PDF/A-2a"); err != nil {
		t.Fatalf("fixMarkedContentLang: %v", err)
	}

	dict, body := liveStreamBody(t, alloc, doc, 5)
	if strings.Contains(body, "/Lang") {
		t.Errorf("the form keeps its inline /Lang: %q", body)
	}
	if dict["Resources"] == nil {
		t.Fatalf("the form lost the /Resources nested-resources gave it: %v", dict)
	}
	if p1 := formPropertyList(t, alloc, doc); p1["Lang"] != nil {
		t.Errorf("the form's copy of /P1 keeps /Lang: %v", p1)
	}
	if list := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: 6}); list["Lang"] != nil {
		t.Errorf("/P0 keeps /Lang: %v", list)
	}
}

// TestFixMarkedContentLang_KeepsTransparencysFormWrite pins that the /Lang
// repairs of a form keep the transparency group transparency (registry 31)
// removed from the form's dictionary.
func TestFixMarkedContentLang_KeepsTransparencysFormWrite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, langFormPDF("/Group << /S /Transparency >> /Resources << /Properties << /P1 << /Lang (3x) >> >> >>", ""))
	if _, err := fixTransparency(ctx, alloc, doc, Options{}, nil, "PDF/A-1a"); err != nil {
		t.Fatalf("fixTransparency: %v", err)
	}
	if dict, _ := liveStreamBody(t, alloc, doc, 5); dict["Group"] != nil {
		t.Fatal("control: transparency kept the form's group, so there is no earlier edit to lose")
	}
	if _, err := fixMarkedContentLang(ctx, alloc, doc, Options{}, nil, "PDF/A-1a"); err != nil {
		t.Fatalf("fixMarkedContentLang: %v", err)
	}

	dict, body := liveStreamBody(t, alloc, doc, 5)
	if strings.Contains(body, "/Lang") {
		t.Errorf("the form keeps its inline /Lang: %q", body)
	}
	if dict["Group"] != nil {
		t.Errorf("the form's transparency group is back: %v", dict)
	}
	if p1 := formPropertyList(t, alloc, doc); p1["Lang"] != nil {
		t.Errorf("the form's /P1 keeps /Lang: %v", p1)
	}
}

// TestFixMarkedContentLang_KeepsOutputIntentsResourcesWrite pins that writing
// a direct property list back through the page's /Resources keeps the
// /Default colour space output-intent (registry 6) injected there.
func TestFixMarkedContentLang_KeepsOutputIntentsResourcesWrite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, langFormPDF("/Resources << >>", ""))
	if n := injectTestDefaultRGB(t, alloc, doc); n == 0 {
		t.Fatal("control: output-intent patched no page, so there is no earlier edit to lose")
	}
	if _, err := fixMarkedContentLang(ctx, alloc, doc, Options{}, nil, "PDF/A-2a"); err != nil {
		t.Fatalf("fixMarkedContentLang: %v", err)
	}

	res := liveOwnerResources(t, alloc, doc, 3)
	if p1 := liveDictValue(ctx, alloc, doc, liveDictValue(ctx, alloc, doc, res["Properties"])["P1"]); p1 == nil || p1["Lang"] != nil {
		t.Errorf("the page's direct property list /P1 = %v, want it without /Lang", p1)
	}
	if cs := liveDictValue(ctx, alloc, doc, res["ColorSpace"]); cs["DefaultRGB"] == nil {
		t.Errorf("/DefaultRGB is gone: the property list repair rebuilt /Resources from the "+
			"document and discarded output-intent's write; /Resources = %v", res)
	}
}

// TestFixUnknownOps_KeepsMarkedContentLangFormWrite pins that unknown-ops
// (registry 65), which rewrites the same form body after marked-content-lang
// (63), keeps the /Lang repair.
func TestFixUnknownOps_KeepsMarkedContentLangFormWrite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, langFormPDF("/Resources << >>", " foo"))
	if _, err := fixMarkedContentLang(ctx, alloc, doc, Options{}, nil, "PDF/A-2a"); err != nil {
		t.Fatalf("fixMarkedContentLang: %v", err)
	}
	if _, body := liveStreamBody(t, alloc, doc, 5); strings.Contains(body, "/Lang") {
		t.Fatalf("control: marked-content-lang kept the form's inline /Lang: %q", body)
	}
	if _, err := fixUnknownOps(ctx, alloc, doc, Options{}, nil, "PDF/A-2a"); err != nil {
		t.Fatalf("fixUnknownOps: %v", err)
	}

	_, body := liveStreamBody(t, alloc, doc, 5)
	if strings.Contains(body, "foo") {
		t.Errorf("control: unknown-ops kept the undefined operator: %q", body)
	}
	if strings.Contains(body, "/Lang") {
		t.Errorf("the form's inline /Lang is back: %q", body)
	}
}
