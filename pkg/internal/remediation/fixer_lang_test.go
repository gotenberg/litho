package remediation

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

func TestFixCatalogLang_AddsDefaultEN(t *testing.T) {
	doc, alloc := newTestDoc(t, minimalCatalogPDF(""))

	descs, err := fixCatalogLang(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixCatalogLang: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 description, got %v", descs)
	}

	newCatalog := pendingDict(t, alloc, 1)
	lang, ok := newCatalog["Lang"].(ast.PdfString)
	if !ok {
		t.Fatalf("catalog /Lang: got %T, want PdfString", newCatalog["Lang"])
	}
	if string(lang) != "en" {
		t.Errorf("/Lang: got %q, want %q", lang, "en")
	}
}

func TestFixCatalogLang_UsesOptsLang(t *testing.T) {
	doc, alloc := newTestDoc(t, minimalCatalogPDF(""))

	descs, err := fixCatalogLang(context.Background(), alloc, doc, Options{Lang: "fr-FR"}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixCatalogLang: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 description, got %v", descs)
	}
	lang := pendingDict(t, alloc, 1)["Lang"].(ast.PdfString)
	if string(lang) != "fr-FR" {
		t.Errorf("/Lang: got %q, want %q", lang, "fr-FR")
	}
}

func TestFixCatalogLang_SkipsIfPresent(t *testing.T) {
	doc, alloc := newTestDoc(t, minimalCatalogPDF("/Lang (de)"))

	descs, err := fixCatalogLang(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixCatalogLang: %v", err)
	}
	if len(descs) != 0 {
		t.Errorf("expected no descriptions when /Lang is present, got %v", descs)
	}
	if len(alloc.Pending()) != 0 {
		t.Errorf("expected no pending allocations, got %d", len(alloc.Pending()))
	}
}

func TestFixCatalogLang_ReplacesInvalidCatalogLang(t *testing.T) {
	doc, alloc := newTestDoc(t, minimalCatalogPDF("/Lang (en US)"))

	descs, err := fixCatalogLang(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixCatalogLang: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 description, got %v", descs)
	}
	lang, ok := pendingDict(t, alloc, 1)["Lang"].(ast.PdfString)
	if !ok || string(lang) != "en" {
		t.Errorf("catalog /Lang: got %v, want %q", pendingDict(t, alloc, 1)["Lang"], "en")
	}
}

func TestFixCatalogLang_RemovesInvalidStructElemLang(t *testing.T) {
	doc, alloc := newTestDoc(t, taggedLangPDF("en", "", "en US"))

	descs, err := fixCatalogLang(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixCatalogLang: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 description, got %v", descs)
	}
	if _, ok := pendingDict(t, alloc, 5)["Lang"]; ok {
		t.Error("struct element /Lang should have been removed")
	}
	// Catalog /Lang is valid, so the catalog must be left untouched.
	if _, ok := alloc.Pending()[1]; ok {
		t.Error("catalog should not be rewritten when its /Lang is valid")
	}
}

func TestFixCatalogLang_RepairsStructTreeRootLang(t *testing.T) {
	doc, alloc := newTestDoc(t, taggedLangPDF("en", "en US", ""))

	descs, err := fixCatalogLang(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixCatalogLang: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 description, got %v", descs)
	}
	if _, ok := pendingDict(t, alloc, 4)["Lang"]; ok {
		t.Error("StructTreeRoot /Lang should have been removed")
	}
}

func TestFixCatalogLang_RemovesInvalidInlineStructElemLang(t *testing.T) {
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R /Lang (en) >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /Document /P 4 0 R /K [<< /Type /StructElem /S /P /Lang (en US) >>] >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixCatalogLang(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixCatalogLang: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 description, got %v", descs)
	}
	// The inline child has no object number, so the repair must persist by
	// rewriting the owning indirect element (object 5) with a rebuilt /K.
	kids, ok := pendingDict(t, alloc, 5)["K"].(ast.PdfArray)
	if !ok || len(kids) != 1 {
		t.Fatalf("object 5 /K: got %T (%v), want array of 1", pendingDict(t, alloc, 5)["K"], pendingDict(t, alloc, 5)["K"])
	}
	inline, ok := kids[0].(ast.PdfDict)
	if !ok {
		t.Fatalf("inline kid: got %T, want ast.PdfDict", kids[0])
	}
	if _, ok := inline["Lang"]; ok {
		t.Error("inline struct element /Lang should have been removed")
	}
	if inline["S"] != ast.PdfName("P") {
		t.Errorf("inline struct element /S should be preserved, got %v", inline["S"])
	}
}

func TestFixCatalogLang_KeepsValidStructElemLang(t *testing.T) {
	doc, alloc := newTestDoc(t, taggedLangPDF("en", "", "fr-FR"))

	descs, err := fixCatalogLang(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixCatalogLang: %v", err)
	}
	if len(descs) != 0 {
		t.Errorf("expected no descriptions when every /Lang is valid, got %v", descs)
	}
	if len(alloc.Pending()) != 0 {
		t.Errorf("expected no pending allocations, got %d", len(alloc.Pending()))
	}
}

func TestFixCatalogLang_StructTreeCycleTerminates(t *testing.T) {
	// Objects 5 and 6 reference each other through /K. The visited set must
	// break the cycle, terminate, and still repair object 5's invalid /Lang.
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R /Lang (en) >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /Document /P 4 0 R /K [6 0 R] /Lang (en US) >>",
		"<< /Type /StructElem /S /Sect /P 5 0 R /K [5 0 R] >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixCatalogLang(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixCatalogLang: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 description, got %v", descs)
	}
	if _, ok := pendingDict(t, alloc, 5)["Lang"]; ok {
		t.Error("object 5 /Lang should have been removed")
	}
}

// buildStructPDF assembles a single-page PDF from the given object bodies
// (object 1 is the catalog). It computes correct xref offsets so the document
// parses without relying on xref recovery.
func buildStructPDF(bodies ...string) []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(bodies)+1)
	for i, body := range bodies {
		offsets[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	xrefStart := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \r\n", len(bodies)+1)
	for i := 1; i <= len(bodies); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \r\n", offsets[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(bodies)+1, xrefStart)
	return buf.Bytes()
}

// taggedLangPDF builds a tagged single-page PDF: catalog -> StructTreeRoot
// (object 4) -> one Document structure element (object 5). Each /Lang is
// emitted only when its argument is non-empty.
func taggedLangPDF(catLang, rootLang, elemLang string) []byte {
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R"+langExtra(catLang)+" >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K 5 0 R"+langExtra(rootLang)+" >>",
		"<< /Type /StructElem /S /Document /P 4 0 R"+langExtra(elemLang)+" >>",
	)
}

// langExtra renders a /Lang entry for the given tag, or nothing when empty.
func langExtra(tag string) string {
	if tag == "" {
		return ""
	}
	return " /Lang (" + tag + ")"
}
