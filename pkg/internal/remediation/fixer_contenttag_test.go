package remediation

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// contentTagPDF builds a two-page tagged-skeleton PDF: catalog with
// StructTreeRoot (object 5) holding a Document element (object 6), pages at
// objects 3 and 4 with untagged "q Q" content streams at 7 and 8. The
// stExtra and docExtra strings are appended into the StructTreeRoot and
// Document element bodies.
func contentTagPDF(stExtra, docExtra string) []byte {
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 5 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 7 0 R >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 8 0 R >>",
		"<< /Type /StructTreeRoot /K 6 0 R"+stExtra+" >>",
		"<< /Type /StructElem /S /Document /P 5 0 R"+docExtra+" >>",
		"<< /Length 3 >>\nstream\nq Q\nendstream",
		"<< /Length 3 >>\nstream\nq Q\nendstream",
	)
}

// pendingStreamBytes returns the body of the stream queued at the given
// object number.
func pendingStreamBytes(t *testing.T, alloc interface{ Pending() map[int]ast.PdfObject }, objNum int) []byte {
	t.Helper()
	s, ok := alloc.Pending()[objNum].(ast.PdfStream)
	if !ok {
		t.Fatalf("object %d: got %T, want ast.PdfStream", objNum, alloc.Pending()[objNum])
	}
	data, err := s.Bytes(context.Background())
	if err != nil {
		t.Fatalf("stream %d bytes: %v", objNum, err)
	}
	return data
}

func TestFixContentTag_TagsUntaggedPages(t *testing.T) {
	doc, alloc := newTestDoc(t, contentTagPDF("", ""))

	descs, err := fixContentTag(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixContentTag: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 description, got %v", descs)
	}

	// Each page gets a fresh wrapped stream, MCID 0, and a unique
	// /StructParents key matching its ParentTree entry.
	for i, pageObj := range []int{3, 4} {
		page := pendingDict(t, alloc, pageObj)
		if page["StructParents"] != ast.PdfInt(i) {
			t.Errorf("page %d /StructParents: got %v, want %d", pageObj, page["StructParents"], i)
		}
		contentsRef, ok := page["Contents"].(ast.PdfIndirectRef)
		if !ok {
			t.Fatalf("page %d /Contents: got %T", pageObj, page["Contents"])
		}
		data := pendingStreamBytes(t, alloc, contentsRef.ObjectNum)
		if !bytes.Contains(data, []byte("/Document <</MCID 0>> BDC")) || !bytes.Contains(data, []byte("q Q")) {
			t.Errorf("page %d wrapped content wrong: %q", pageObj, data)
		}
	}

	// Document element /K: two MCRs, both with MCID 0 (the ParentTree
	// entry array for a page is indexed by MCID; the old code used the
	// page index, which broke the lookup on every page but the first).
	docElem := pendingDict(t, alloc, 6)
	k, ok := docElem["K"].(ast.PdfArray)
	if !ok || len(k) != 2 {
		t.Fatalf("Document /K: got %v, want 2 MCRs", docElem["K"])
	}
	for i, item := range k {
		mcr, ok := item.(ast.PdfDict)
		if !ok || mcr["Type"] != ast.PdfName("MCR") || mcr["MCID"] != ast.PdfInt(0) {
			t.Errorf("Document /K[%d]: got %v, want MCR with MCID 0", i, item)
		}
	}

	// ParentTree: [0 [doc] 1 [doc]], keys matching /StructParents.
	st := pendingDict(t, alloc, 5)
	pt, ok := st["ParentTree"].(ast.PdfDict)
	if !ok {
		t.Fatalf("ParentTree: got %T", st["ParentTree"])
	}
	nums, ok := pt["Nums"].(ast.PdfArray)
	if !ok || len(nums) != 4 {
		t.Fatalf("ParentTree /Nums: got %v, want 4 entries", pt["Nums"])
	}
	if nums[0] != ast.PdfInt(0) || nums[2] != ast.PdfInt(1) {
		t.Errorf("ParentTree keys: got %v/%v, want 0/1", nums[0], nums[2])
	}
}

func TestFixContentTag_MergesExistingParentTree(t *testing.T) {
	// An existing ParentTree entry (key 5, e.g. an annotation
	// /StructParent) must survive, and new keys must start above it. The
	// old code replaced the whole tree.
	doc, alloc := newTestDoc(t, contentTagPDF(" /ParentTree << /Nums [5 6 0 R] >>", ""))

	if _, err := fixContentTag(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1"); err != nil {
		t.Fatalf("fixContentTag: %v", err)
	}

	st := pendingDict(t, alloc, 5)
	nums := st["ParentTree"].(ast.PdfDict)["Nums"].(ast.PdfArray)
	if len(nums) != 6 {
		t.Fatalf("ParentTree /Nums: got %v, want existing pair + 2 new", nums)
	}
	if nums[0] != ast.PdfInt(5) {
		t.Errorf("existing ParentTree entry destroyed: %v", nums)
	}
	if nums[2] != ast.PdfInt(6) || nums[4] != ast.PdfInt(7) {
		t.Errorf("new keys must start after the highest existing key: %v", nums)
	}
	if pendingDict(t, alloc, 3)["StructParents"] != ast.PdfInt(6) {
		t.Errorf("page /StructParents: got %v, want 6", pendingDict(t, alloc, 3)["StructParents"])
	}
}

func TestFixContentTag_PreservesSingleRefK(t *testing.T) {
	// Document /K is a single indirect reference; the old code dropped any
	// non-array /K, orphaning the existing child.
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 5 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 8 0 R >>",
		"<< /Type /StructElem /S /P /P 6 0 R >>",
		"<< /Type /StructTreeRoot /K 6 0 R >>",
		"<< /Type /StructElem /S /Document /P 5 0 R /K 4 0 R >>",
		"<< /Type /Page >>",
		"<< /Length 3 >>\nstream\nq Q\nendstream",
	)
	doc, alloc := newTestDoc(t, pdf)

	if _, err := fixContentTag(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1"); err != nil {
		t.Fatalf("fixContentTag: %v", err)
	}

	k, ok := pendingDict(t, alloc, 6)["K"].(ast.PdfArray)
	if !ok || len(k) != 2 {
		t.Fatalf("Document /K: got %v, want [existing child, MCR]", pendingDict(t, alloc, 6)["K"])
	}
	if k[0] != (ast.PdfIndirectRef{ObjectNum: 4}) {
		t.Errorf("existing /K child dropped: %v", k)
	}
}

func TestFixContentTag_SkipsPageWithStructParents(t *testing.T) {
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 5 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 7 0 R /StructParents 0 >>",
		"<< /Type /Page >>",
		"<< /Type /StructTreeRoot /K 6 0 R /ParentTree << /Nums [0 6 0 R] >> >>",
		"<< /Type /StructElem /S /Document /P 5 0 R >>",
		"<< /Length 3 >>\nstream\nq Q\nendstream",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixContentTag(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixContentTag: %v", err)
	}
	if len(descs) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("a page that already owns a ParentTree entry must be skipped: descs=%v pending=%d", descs, len(alloc.Pending()))
	}
}

func TestFixContentTag_BailsOnKidsParentTree(t *testing.T) {
	doc, alloc := newTestDoc(t, contentTagPDF(" /ParentTree << /Kids [6 0 R] >>", ""))

	descs, err := fixContentTag(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixContentTag: %v", err)
	}
	if len(descs) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("a /Kids ParentTree cannot be merged and must not be destroyed: descs=%v pending=%d", descs, len(alloc.Pending()))
	}
}

func TestFixContentTag_RespectsPendingPageDict(t *testing.T) {
	doc, alloc := newTestDoc(t, contentTagPDF("", ""))

	// An earlier fixer queued a page rewrite carrying a marker.
	alloc.AllocAt(3, ast.PdfDict{
		"Type":     ast.PdfName("Page"),
		"Parent":   ast.PdfIndirectRef{ObjectNum: 2},
		"MediaBox": ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(612), ast.PdfInt(792)},
		"Contents": ast.PdfIndirectRef{ObjectNum: 7},
		"Marker":   ast.PdfName("FromPending"),
	})

	if _, err := fixContentTag(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1"); err != nil {
		t.Fatalf("fixContentTag: %v", err)
	}
	page := pendingDict(t, alloc, 3)
	if page["Marker"] != ast.PdfName("FromPending") {
		t.Error("fixContentTag clobbered the pending page rewrite")
	}
	if _, has := page["StructParents"]; !has {
		t.Error("pending page not tagged")
	}
}
