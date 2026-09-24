package remediation

import (
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestFixInfoDate_KeepsAnEarlierFixersPendingInfoWrite pins that the /Info date
// repair keeps an earlier fixer's pending write to /Info.
//
// [fixInfoDate] rebuilds the trailer /Info with maps.Copy and writes the copy
// back at the same object number. It used to read /Info with
// document.ResolveDict, consulting no alloc-aware helper, so an earlier
// fixer's edit to /Info was discarded.
//
// Peer and ordering are measured, and this is the tightest adjacency the
// program has found: trailer-id and info-date are CONSECUTIVE registry
// entries, with nothing between them. They are also the ONLY two /Info writers
// in the package.
//
// What made this a correctness defect rather than a lost edit:
// enforceInfoDictLimits (trailer-id) trims /Info to /ModDate alone for
// ISO 19005-4 6.1.3 tests 4-5, then info-date rebuilt the dictionary from the
// on-disk copy one entry later and RESTORED the very keys the trim removed. The
// output then carried /Info entries PDF/A-4 forbids.
//
// The defect needs no invented marker key: /CreationDate reappearing IS the
// violation. The seed is exactly what the trim produces, so this test also
// documents that shape.
//
// liveInfoDict already solved this for xmp-metadata, and its own comment says
// so: it prefers "an in-flight patched version from alloc so earlier fixers
// (e.g. info-date) see their changes". info-date now reads /Info through it
// too.
func TestFixInfoDate_KeepsAnEarlierFixersPendingInfoWrite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, isoDatePDF())

	const infoObj = 4
	infoRef := ast.PdfIndirectRef{ObjectNum: infoObj}

	onDisk, err := document.ResolveDict(ctx, doc, infoRef)
	if err != nil || onDisk == nil {
		t.Fatalf("fixture /Info object %d did not resolve: %v", infoObj, err)
	}
	modDate, ok := onDisk["ModDate"].(ast.PdfString)
	if !ok {
		t.Fatalf("precondition: fixture /Info must carry /ModDate, got %v", onDisk["ModDate"])
	}
	if _, present := onDisk["CreationDate"]; !present {
		t.Fatal("precondition: fixture /Info must carry /CreationDate, or its " +
			"reappearance proves nothing")
	}
	if strings.HasPrefix(document.DecodePdfString(modDate), "D:") {
		t.Fatal("precondition: /ModDate must NOT already be in D: form, or the " +
			"repair never runs and this test proves nothing")
	}

	// Stand in for enforceInfoDictLimits at trailer-id: trim
	// /Info to /ModDate alone, which is exactly what the PDF/A-4 repair emits.
	trimmed := ast.PdfDict{"ModDate": modDate}
	if !alloc.AllocAt(infoObj, trimmed) {
		t.Fatalf("AllocAt(%d) refused the earlier fixer's pending write", infoObj)
	}

	if _, err := fixInfoDate(ctx, alloc, doc, Options{}, nil, "PDF/A-4"); err != nil {
		t.Fatalf("fixInfoDate: %v", err)
	}

	got := pendingDict(t, alloc, infoObj)

	// Self-diagnosing, keyed on the SEEDED object: the seed carried an ISO
	// /ModDate, so seeing D: form proves object 4 itself was rewritten. Without
	// this the test could pass by reading back its own seed untouched.
	gotMod, ok := got["ModDate"].(ast.PdfString)
	if !ok || !strings.HasPrefix(document.DecodePdfString(gotMod), "D:") {
		t.Fatalf("/ModDate is %v, want D: form: the repair never rewrote object %d, "+
			"so this test proves nothing about clobbering; info = %v",
			got["ModDate"], infoObj, got)
	}

	if _, back := got["CreationDate"]; back {
		t.Errorf("/CreationDate is back: fixInfoDate rebuilt /Info from the on-disk "+
			"copy and discarded the earlier fixer's trim, restoring a key "+
			"ISO 19005-4 6.1.3 forbids; info = %v", got)
	}
}
