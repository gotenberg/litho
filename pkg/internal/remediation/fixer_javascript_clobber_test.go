package remediation

import (
	"context"
	"maps"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// javaScriptIndirectNamesPDF builds a one-page document whose catalog /Names
// dictionary is INDIRECT at object 4 and holds a /JavaScript name tree.
//
// The indirection is the point. An inline /Names lives inside the catalog,
// which fixJavaScript already reads through the allocator, so only an indirect
// one can carry an edit that an earlier fixer queued at its own object number.
func javaScriptIndirectNamesPDF() []byte {
	return assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /Names 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /JavaScript 5 0 R /Dests << /Names [] >> >>",
		"<< /Names [(init) << /S /JavaScript /JS (app.alert\\(1\\)) >>] >>",
	})
}

// TestFixJavaScript_KeepsAnEarlierFixersPendingNamesWrite pins that removing
// the /JavaScript name tree starts from the /Names dictionary as the earlier
// fixers left it.
//
// fixJavaScript read /Names with document.ResolveDict, copied it without its
// /JavaScript entry and pointed the catalog at the copy. An earlier fixer's
// edit queued at the /Names object number was therefore orphaned: the output
// catalog referenced a copy of the ON-DISK dictionary. catalog-cleanup is such
// a peer and precedes javascript in the registry: fixAlternatePresentations
// writes the indirect /Names back at its own object number, so the clobber
// resurrected the /AlternatePresentations it had removed (ISO 19005-2, 6.10).
//
// The marker is a key the fixture does not declare, so its presence can only
// come from the pending copy.
func TestFixJavaScript_KeepsAnEarlierFixersPendingNamesWrite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, javaScriptIndirectNamesPDF())

	const namesObj = 4

	onDisk, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: namesObj})
	if err != nil || onDisk == nil {
		t.Fatalf("fixture /Names at object %d did not resolve: %v", namesObj, err)
	}
	if _, has := onDisk["JavaScript"]; !has {
		t.Fatalf("precondition: /Names must carry /JavaScript or the removal never runs; got %v", onDisk)
	}
	if _, has := onDisk["Marker"]; has {
		t.Fatalf("precondition: the fixture must NOT declare /Marker, else it proves nothing")
	}

	peer := make(ast.PdfDict, len(onDisk)+1)
	maps.Copy(peer, onDisk)
	peer["Marker"] = ast.PdfName("peer")
	if !alloc.AllocAt(namesObj, peer) {
		t.Fatalf("AllocAt(%d) refused the earlier fixer's pending write", namesObj)
	}

	fixes, err := fixJavaScript(ctx, alloc, doc, Options{}, nil, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixJavaScript: %v", err)
	}
	if len(fixes) == 0 {
		t.Fatal("control: fixJavaScript reported no fix, so the removal never ran and this test proves nothing")
	}

	catalog := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: 1})
	names := liveDictValue(ctx, alloc, doc, catalog["Names"])
	if names == nil {
		t.Fatalf("the catalog /Names %v no longer resolves to a dictionary", catalog["Names"])
	}
	if _, has := names["JavaScript"]; has {
		t.Errorf("the /Names the catalog points at still carries /JavaScript: %v", names)
	}
	if _, kept := names["Marker"]; !kept {
		t.Errorf("/Marker is gone: fixJavaScript rebuilt /Names from the on-disk copy and "+
			"discarded the earlier fixer's write; /Names = %v", names)
	}
	if _, has := names["Dests"]; !has {
		t.Errorf("unrelated /Names entries must survive the rewrite; /Names = %v", names)
	}
}
