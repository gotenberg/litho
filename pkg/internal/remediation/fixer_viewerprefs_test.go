package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

func TestFixViewerPrefs_AddsDisplayDocTitle(t *testing.T) {
	doc, alloc := newTestDoc(t, minimalCatalogPDF(""))

	descs, err := fixViewerPrefs(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixViewerPrefs: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 description, got %v", descs)
	}

	newCatalog := pendingDict(t, alloc, 1)
	vpObj, ok := newCatalog["ViewerPreferences"]
	if !ok {
		t.Fatal("catalog is missing /ViewerPreferences after fix")
	}
	vpDict, ok := vpObj.(ast.PdfDict)
	if !ok {
		t.Fatalf("/ViewerPreferences: got %T, want PdfDict", vpObj)
	}
	ddt, ok := vpDict["DisplayDocTitle"].(ast.PdfBool)
	if !ok || !bool(ddt) {
		t.Errorf("/ViewerPreferences/DisplayDocTitle: got %v, want true", vpDict["DisplayDocTitle"])
	}
}

func TestFixViewerPrefs_PreservesExistingKeys(t *testing.T) {
	doc, alloc := newTestDoc(t, minimalCatalogPDF(`/ViewerPreferences << /HideToolbar true >>`))

	descs, err := fixViewerPrefs(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixViewerPrefs: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 description, got %v", descs)
	}

	newCatalog := pendingDict(t, alloc, 1)
	vp := newCatalog["ViewerPreferences"].(ast.PdfDict)
	if got, ok := vp["HideToolbar"].(ast.PdfBool); !ok || !bool(got) {
		t.Errorf("/HideToolbar lost after merge: got %v", vp["HideToolbar"])
	}
	if got, ok := vp["DisplayDocTitle"].(ast.PdfBool); !ok || !bool(got) {
		t.Errorf("/DisplayDocTitle not set: got %v", vp["DisplayDocTitle"])
	}
}
