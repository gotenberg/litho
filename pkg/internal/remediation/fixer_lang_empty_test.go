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

// emptyLangPDF builds a tagged document whose catalog and one structure element
// both carry an empty /Lang, the shape of PDF_UA-1__7.2-t29-fail-n and -fail-o.
func emptyLangPDF() []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /Lang () /StructTreeRoot 4 0 R /MarkInfo << /Marked true >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K [5 0 R] >>",
		"<< /Type /StructElem /S /Document /P 4 0 R /K [6 0 R] >>",
		"<< /Type /StructElem /S /P /P 5 0 R /Lang () >>",
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

// runLangFix applies the /Lang fixer for a profile and returns the allocator.
func runLangFix(t *testing.T, profile string) *writer.ObjAllocator {
	t.Helper()

	ctx := context.Background()
	data := emptyLangPDF()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber())
	if _, err := fixCatalogLang(ctx, alloc, doc, Options{}, nil, profile); err != nil {
		t.Fatalf("fixCatalogLang: %v", err)
	}
	return alloc
}

// TestFixCatalogLangReplacesAnEmptyTagForPDFUA pins that an empty /Lang is
// repaired under PDF/UA.
//
// ISO 32000-1, 14.9.2 lets the empty text string mean "the language is
// unknown", so [compliance.IsValidLangTag] accepts it and the fixer did
// nothing. PDF/UA requires the language to be identified (ISO 14289-1, 7.2 /
// ISO 14289-2, 8.4.4), and litho and veraPDF both reported the empty catalog
// /Lang that remediation then left in place.
func TestFixCatalogLangReplacesAnEmptyTagForPDFUA(t *testing.T) {
	t.Parallel()

	alloc := runLangFix(t, "PDF/UA-1")

	catalog, ok := alloc.Pending()[1].(ast.PdfDict)
	if !ok {
		t.Fatalf("the catalog was not rewritten: %T", alloc.Pending()[1])
	}
	if lang, _ := catalog["Lang"].(ast.PdfString); string(lang) != "en" {
		t.Errorf("catalog /Lang = %q, want %q", lang, "en")
	}

	// The structure element's empty tag is stripped so it inherits the
	// catalog's language instead of declaring its own unknown one.
	elem, ok := alloc.Pending()[6].(ast.PdfDict)
	if !ok {
		t.Fatalf("the structure element was not rewritten: %T", alloc.Pending()[6])
	}
	if _, still := elem["Lang"]; still {
		t.Errorf("the structure element kept an empty /Lang: %v", elem["Lang"])
	}
}

// TestFixCatalogLangKeepsAnEmptyTagForPDFA is the control: outside PDF/UA the
// empty tag is the spec's way of saying the language is unknown, and rewriting
// it would assert a language the document never claimed.
func TestFixCatalogLangKeepsAnEmptyTagForPDFA(t *testing.T) {
	t.Parallel()

	alloc := runLangFix(t, "PDF/A-2a")

	if catalog, rewritten := alloc.Pending()[1].(ast.PdfDict); rewritten {
		if lang, _ := catalog["Lang"].(ast.PdfString); string(lang) != "" {
			t.Errorf("catalog /Lang = %q under PDF/A, want the empty tag left alone", lang)
		}
	}
	if elem, rewritten := alloc.Pending()[6].(ast.PdfDict); rewritten {
		if _, gone := elem["Lang"]; !gone {
			t.Error("a structure element's empty /Lang was stripped under PDF/A")
		}
	}
}

// TestFixCatalogLangStillReplacesAMalformedTag is the control for the case that
// already worked: a tag that is not empty but is not a Language-Tag either.
func TestFixCatalogLangStillReplacesAMalformedTag(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := bytes.Replace(emptyLangPDF(), []byte("/Lang () /StructTreeRoot"), []byte("/Lang (1-pt) /StructTreeRoot"), 1)
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber())
	if _, err := fixCatalogLang(ctx, alloc, doc, Options{}, nil, "PDF/A-2a"); err != nil {
		t.Fatalf("fixCatalogLang: %v", err)
	}
	catalog, ok := alloc.Pending()[1].(ast.PdfDict)
	if !ok {
		t.Fatalf("the catalog was not rewritten: %T", alloc.Pending()[1])
	}
	if lang, _ := catalog["Lang"].(ast.PdfString); string(lang) != "en" {
		t.Errorf("catalog /Lang = %q, want the malformed tag replaced with %q", lang, "en")
	}
}
