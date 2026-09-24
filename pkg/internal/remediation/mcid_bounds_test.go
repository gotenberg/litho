package remediation

import (
	"context"
	"fmt"
	"testing"
)

// The assertions below count the descriptions a fixer returns rather than the
// bytes it allocates. runtime.ReadMemStats reports TotalAlloc for the whole
// PROCESS, so inside a t.Parallel() test the delta records everything the
// package allocated during the window rather than the call under test. An
// earlier version of this file asserted a 16 MB ceiling and duly reported 21.9
// MB and 16.7 MB for two bounded calls, failing on code it had passed six times
// the same hour. The description count is exact and attributable, and it
// discriminates just as sharply: the bound's whole effect is that the fixer
// refuses, so an unbounded run registers a page and a bounded one does not.
//
// The measured allocations are kept in the comments below as the record of what
// the bound prevents.

// corpusMaxMCID is the largest marked-content id real documents carry. The
// controls below use it so a bound that refused real documents would fail
// rather than pass quietly.
//
// # Corpus evidence
//
//   - All corpora, 5171 files: the largest marked-content id is 12522.
//   - govdocs thread_000_000314.pdf: carries that largest id.
const corpusMaxMCID = 12522

// parentTreeMCIDPDF builds a structure tree whose single element registers one
// marked-content id against a page, the shape fixParentTree walks.
func parentTreeMCIDPDF(mcid int) []byte {
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /StructParents 0 >>",
		"<< /Type /StructTreeRoot /K 5 0 R /ParentTree << /Nums [] >> >>",
		fmt.Sprintf("<< /Type /StructElem /S /Document /P 4 0 R /Pg 3 0 R /K [%d] >>", mcid),
	)
}

// listContentMCIDPDF is the shape the other fixListContent tests use, with the
// list item's bare marked-content id supplied.
func listContentMCIDPDF(mcid int) []byte {
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /StructParents 0 >>",
		"<< /Type /StructTreeRoot /K 5 0 R /ParentTree << /Nums [0 [6 0 R]] >> >>",
		"<< /Type /StructElem /S /L /P 4 0 R /K [6 0 R] >>",
		fmt.Sprintf("<< /Type /StructElem /S /LI /P 5 0 R /Pg 3 0 R /K [%d] >>", mcid),
	)
}

// parentTreeMCRPDF carries the id inside a marked-content reference dictionary
// rather than as a bare integer. structElemMCIDs accepts both shapes, and a
// fixture using only the integer form leaves the MCR branch unexercised: a
// guard no test can reach is a guard no test can prove.
func parentTreeMCRPDF(mcid int) []byte {
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /StructParents 0 >>",
		"<< /Type /StructTreeRoot /K 5 0 R /ParentTree << /Nums [] >> >>",
		fmt.Sprintf("<< /Type /StructElem /S /Document /P 4 0 R /Pg 3 0 R /K [<< /Type /MCR /MCID %d >>] >>", mcid),
	)
}

// TestFixParentTree_RefusesAnImplausibleMCID pins that a marked-content id
// taken from the file is bounded before it sizes the ParentTree array.
//
// The ParentTree is indexed by MCID, so fixParentTree sizes one page's array at
// the largest id it finds and writes a null into every slot. Measured before
// the bound: a 578-byte file allocated 183.2 MB, returned no error, and
// reported "registered 1 page(s)" as though it had succeeded. The allocation is
// committed, not merely reserved, because every slot is written.
func TestFixParentTree_RefusesAnImplausibleMCID(t *testing.T) {
	t.Parallel()

	data := parentTreeMCIDPDF(12_000_000)
	doc, alloc := newTestDoc(t, data)

	descs, err := fixParentTree(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-2")
	if err != nil {
		t.Fatalf("fixParentTree: %v", err)
	}
	if len(descs) != 0 {
		t.Errorf("descriptions = %v, want none: an implausible id was registered anyway", descs)
	}
}

// TestFixParentTree_KeepsAnOrdinaryMCID is the control. A bound that refused
// every id would satisfy the test above while disabling the fixer outright.
func TestFixParentTree_KeepsAnOrdinaryMCID(t *testing.T) {
	t.Parallel()

	data := parentTreeMCIDPDF(corpusMaxMCID)
	doc, alloc := newTestDoc(t, data)

	descs, err := fixParentTree(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-2")
	if err != nil {
		t.Fatalf("fixParentTree: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("descriptions = %v, want one: the bound rejects the largest id in the corpora", descs)
	}
}

// TestFixParentTree_RefusesAnImplausibleMCR covers the marked-content
// reference form of the same hazard.
func TestFixParentTree_RefusesAnImplausibleMCR(t *testing.T) {
	t.Parallel()

	data := parentTreeMCRPDF(12_000_000)
	doc, alloc := newTestDoc(t, data)

	descs, err := fixParentTree(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-2")
	if err != nil {
		t.Fatalf("fixParentTree: %v", err)
	}
	if len(descs) != 0 {
		t.Errorf("descriptions = %v, want none: an implausible MCR id was registered", descs)
	}
}

// TestFixParentTree_KeepsAnOrdinaryMCR is the control for the MCR form, and it
// also proves the fixture reaches the MCR branch at all: if it did not, the
// refusal test above would pass for the wrong reason.
func TestFixParentTree_KeepsAnOrdinaryMCR(t *testing.T) {
	t.Parallel()

	data := parentTreeMCRPDF(corpusMaxMCID)
	doc, alloc := newTestDoc(t, data)

	descs, err := fixParentTree(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-2")
	if err != nil {
		t.Fatalf("fixParentTree: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("descriptions = %v, want one: the MCR branch is never reached, so the refusal test proves nothing", descs)
	}
}

// TestFixListContent_RefusesAnImplausibleMCID pins the same bound on the list
// content repair, which grows a page's ParentTree slot array to hold the id it
// moves. Measured before the bound: 192,147,280 bytes from a 667-byte file.
func TestFixListContent_RefusesAnImplausibleMCID(t *testing.T) {
	t.Parallel()

	data := listContentMCIDPDF(12_000_000)
	doc, alloc := newTestDoc(t, data)

	descs, err := fixListContent(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-2")
	if err != nil {
		t.Fatalf("fixListContent: %v", err)
	}
	if len(descs) != 0 {
		t.Errorf("descriptions = %v, want none: the repair ran on an implausible id", descs)
	}
}

// TestFixListContent_KeepsAnOrdinaryMCID is the matching control.
func TestFixListContent_KeepsAnOrdinaryMCID(t *testing.T) {
	t.Parallel()

	data := listContentMCIDPDF(corpusMaxMCID)
	doc, alloc := newTestDoc(t, data)

	descs, err := fixListContent(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-2")
	if err != nil {
		t.Fatalf("fixListContent: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("descriptions = %v, want one: the bound rejects a real list item", descs)
	}
}
