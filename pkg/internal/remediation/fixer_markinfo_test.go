package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

func TestFixMarkInfo_AddsMarkedTrue(t *testing.T) {
	doc, alloc := newTestDoc(t, minimalCatalogPDF(""))

	descs, err := fixMarkInfo(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixMarkInfo: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 description, got %v", descs)
	}

	newCatalog := pendingDict(t, alloc, 1)
	miObj, ok := newCatalog["MarkInfo"]
	if !ok {
		t.Fatal("catalog is missing /MarkInfo after fix")
	}
	miDict, ok := miObj.(ast.PdfDict)
	if !ok {
		t.Fatalf("/MarkInfo: got %T, want PdfDict", miObj)
	}
	marked, ok := miDict["Marked"].(ast.PdfBool)
	if !ok || !bool(marked) {
		t.Errorf("/MarkInfo/Marked: got %v, want true", miDict["Marked"])
	}
}

// TestFixMarkInfo_RecoversRootlessTrailer pins the shared catalog recovery:
// a file whose active trailer lost /Root (a damaged incremental update) must
// still remediate. The fixer recovers the catalog from the object table, the
// same way the reader and the full-rewrite writer do, instead of aborting with
// "trailer missing /Root". Reproduces govdocs thread_100_100982 at PDF/UA-1.
func TestFixMarkInfo_RecoversRootlessTrailer(t *testing.T) {
	doc, alloc := newTestDoc(t, rootlessCatalogPDF(""))

	if _, ok := doc.Index.Trailer["Root"]; ok {
		t.Fatal("fixture precondition: trailer must omit /Root")
	}

	descs, err := fixMarkInfo(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixMarkInfo on rootless trailer: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 description, got %v", descs)
	}

	newCatalog := pendingDict(t, alloc, 1)
	miDict, ok := newCatalog["MarkInfo"].(ast.PdfDict)
	if !ok {
		t.Fatalf("recovered catalog missing /MarkInfo dict, got %T", newCatalog["MarkInfo"])
	}
	if marked, ok := miDict["Marked"].(ast.PdfBool); !ok || !bool(marked) {
		t.Errorf("/MarkInfo/Marked: got %v, want true", miDict["Marked"])
	}
}
