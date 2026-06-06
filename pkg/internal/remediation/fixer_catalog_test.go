package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

func TestMatchCatalogCleanup_NeedsRendering(t *testing.T) {
	t.Parallel()
	if !matchCatalogCleanup(compliance.Violation{Rule: compliance.Rule_19005_2_6_4_2_t02}) {
		t.Error("matchCatalogCleanup must match §6.4.2 (NeedsRendering)")
	}
}

func TestFixCatalogCleanup_RemovesNeedsRendering(t *testing.T) {
	doc, alloc := newTestDoc(t, minimalCatalogPDF("/NeedsRendering true"))

	descs, err := fixCatalogCleanup(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixCatalogCleanup: %v", err)
	}
	if len(descs) == 0 {
		t.Fatal("expected the forbidden /NeedsRendering key to be removed")
	}
	if _, has := pendingDict(t, alloc, 1)["NeedsRendering"]; has {
		t.Error("/NeedsRendering should have been removed from the catalog")
	}
}

// TestFixCatalogCleanup_NeedsRenderingWithInlinePerms is a regression guard:
// an inline /Perms is rewritten through the catalog by fixPermsDict, so the
// later /NeedsRendering removal must build on that fresh catalog rather than a
// stale copy. Without the re-read, the /Perms cleanup was clobbered.
func TestFixCatalogCleanup_NeedsRenderingWithInlinePerms(t *testing.T) {
	doc, alloc := newTestDoc(t, minimalCatalogPDF("/NeedsRendering true /Perms << /DocMDP 3 0 R /Foo (x) >>"))

	if _, err := fixCatalogCleanup(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixCatalogCleanup: %v", err)
	}

	catalog := pendingDict(t, alloc, 1)
	if _, has := catalog["NeedsRendering"]; has {
		t.Error("/NeedsRendering should have been removed")
	}
	perms, ok := catalog["Perms"].(ast.PdfDict)
	if !ok {
		t.Fatalf("/Perms: got %T, want an inline ast.PdfDict", catalog["Perms"])
	}
	if _, has := perms["Foo"]; has {
		t.Error("/Perms cleanup was lost: forbidden /Foo still present alongside the /NeedsRendering fix")
	}
	if _, has := perms["DocMDP"]; !has {
		t.Error("/Perms /DocMDP should be preserved")
	}
}
