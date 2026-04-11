package remediation

import (
	"context"
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
