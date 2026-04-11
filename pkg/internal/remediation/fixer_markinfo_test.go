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
