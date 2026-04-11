package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

func TestFixTrailerID_AddsMissingID(t *testing.T) {
	doc, alloc := newTestDoc(t, minimalCatalogPDF(""))
	if _, ok := doc.Index.Trailer["ID"]; ok {
		t.Fatal("fixture already has trailer /ID")
	}

	descs, err := fixTrailerID(context.Background(), alloc, doc, Options{}, nil, "PDF/A-1b")
	if err != nil {
		t.Fatalf("fixTrailerID: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 description, got %v", descs)
	}

	idObj, ok := doc.Index.Trailer["ID"]
	if !ok {
		t.Fatal("trailer /ID not set after fix")
	}
	idArr, ok := idObj.(ast.PdfArray)
	if !ok {
		t.Fatalf("/ID: got %T, want PdfArray", idObj)
	}
	if len(idArr) != 2 {
		t.Fatalf("/ID array length: got %d, want 2", len(idArr))
	}
	first, ok1 := idArr[0].(ast.PdfString)
	second, ok2 := idArr[1].(ast.PdfString)
	if !ok1 || !ok2 {
		t.Fatalf("/ID entries: got %T, %T; want PdfString each", idArr[0], idArr[1])
	}
	if len(first) != 16 || len(second) != 16 {
		t.Errorf("/ID entries must be 16 bytes each, got %d/%d", len(first), len(second))
	}
	if string(first) != string(second) {
		t.Errorf("/ID entries should be identical, got %x vs %x", first, second)
	}
}

func TestFixTrailerID_SkipsIfPresent(t *testing.T) {
	doc, alloc := newTestDoc(t, minimalCatalogPDF(""))
	doc.Index.Trailer["ID"] = ast.PdfArray{
		ast.PdfString("preexisting-id-bytes"),
		ast.PdfString("preexisting-id-bytes"),
	}

	descs, err := fixTrailerID(context.Background(), alloc, doc, Options{}, nil, "PDF/A-1b")
	if err != nil {
		t.Fatalf("fixTrailerID: %v", err)
	}
	if len(descs) != 0 {
		t.Errorf("expected no descriptions when /ID is present, got %v", descs)
	}
}
