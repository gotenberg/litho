package remediation

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestMatchContentTag_ClaimsBothStandards pins the ISO 14289-1 §7.1 test 3
// claim alongside the ISO 14289-2 §8.2.2 one. The clauses state the same
// requirement, but only the §8.2.2 wording ("shall be an artifact") reaches the
// description test, so the PDF/UA-1 twin went unclaimed and [matchAutoTag],
// which composes this matcher, never fired for a document whose only signal was
// untagged content.
//
// The rejected clauses are the point of the test number: [clausePrefix] is a
// raw strings.HasPrefix, so a claim spelled clausePrefix(v, "7.1") also
// swallows 7.10, 7.11, 7.18.x and 7.21.x, each owned by a different fixer.
//
// # Corpus evidence
//
//   - All corpora: the unclaimed PDF/UA-1 twin left 1488 corpus fixtures
//     unremediated.
func TestMatchContentTag_ClaimsBothStandards(t *testing.T) {
	t.Parallel()
	if !matchContentTag(compliance.Violation{Rule: compliance.Rule_14289_1_7_1_t03}) {
		t.Error("must match ISO 14289-1 §7.1 t3 (PDF/UA-1 content marking)")
	}
	if !matchContentTag(compliance.Violation{Rule: compliance.Rule_14289_2_8_2_2_t01}) {
		t.Error("must match ISO 14289-2 §8.2.2 t1 (PDF/UA-2 content marking)")
	}
	for _, r := range []compliance.Rule{
		{Clause: "7.1", TestNumber: 1},
		{Clause: "7.1", TestNumber: 10},
		{Clause: "7.10", TestNumber: 1},
		{Clause: "7.11", TestNumber: 1},
		{Clause: "7.18.1", TestNumber: 2},
		{Clause: "7.21.7", TestNumber: 1},
	} {
		if matchContentTag(compliance.Violation{Rule: r}) {
			t.Errorf("clause %s test %d belongs to another fixer and must not match",
				r.Clause, r.TestNumber)
		}
	}
}

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

	// Document element /K: one P wrapper per page, each owning that page's
	// MCR with MCID 0 (the ParentTree entry array for a page is indexed by
	// MCID; the old code used the page index, which broke the lookup on every
	// page but the first). The content hangs off the P, never off the
	// Document: ISO/TS 32005 Table 5 forbids a Document from holding a direct
	// content item, so attaching the MCR straight to it swapped one violation
	// for another.
	docElem := pendingDict(t, alloc, 6)
	k, ok := docElem["K"].(ast.PdfArray)
	if !ok || len(k) != 2 {
		t.Fatalf("Document /K: got %v, want 2 wrappers", docElem["K"])
	}
	for i, item := range k {
		ref, ok := item.(ast.PdfIndirectRef)
		if !ok {
			t.Fatalf("Document /K[%d]: got %T, want an indirect wrapper", i, item)
		}
		wrapper := pendingDict(t, alloc, ref.ObjectNum)
		if wrapper["S"] != ast.PdfName("P") {
			t.Errorf("wrapper %d /S: got %v, want P", i, wrapper["S"])
		}
		mcr, ok := wrapper["K"].(ast.PdfDict)
		if !ok || mcr["Type"] != ast.PdfName("MCR") || mcr["MCID"] != ast.PdfInt(0) {
			t.Errorf("wrapper %d /K: got %v, want MCR with MCID 0", i, wrapper["K"])
		}
	}

	// ParentTree: keys matching /StructParents, each pointing at the wrapper
	// that owns the page's content rather than at the Document.
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
	for i, slot := range []int{1, 3} {
		arr, ok := nums[slot].(ast.PdfArray)
		if !ok || len(arr) != 1 {
			t.Fatalf("ParentTree entry %d: got %v, want a one-element array", i, nums[slot])
		}
		ref, ok := arr[0].(ast.PdfIndirectRef)
		if !ok {
			t.Fatalf("ParentTree entry %d: got %T, want a reference", i, arr[0])
		}
		owner := pendingDict(t, alloc, ref.ObjectNum)
		if owner["S"] != ast.PdfName("P") {
			t.Errorf("ParentTree entry %d points at /S %v, want the P wrapper", i, owner["S"])
		}
	}
}

// TestFixContentTag_SharedContents pins that pages naming one /Contents object
// name one wrapped stream after the repair, each page owning its MCID 0
// through a /StructParents of its own. A wrapped copy for each page gave a
// marked form the shared content draws a second reference, which ISO 14289-1,
// 7.20 t2 forbids and veraPDF, parsing a content stream once for the
// document, did not report before the repair (litho formshare_ua1.pdf).
func TestFixContentTag_SharedContents(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, tc := range []struct {
		name   string
		page2  string
		shared bool
	}{
		{name: "one stream two pages name", page2: "7 0 R", shared: true},
		{name: "streams of their own", page2: "8 0 R"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, alloc := newTestDoc(t, buildStructPDF(
				"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 5 0 R >>",
				"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 7 0 R >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents "+tc.page2+" >>",
				"<< /Type /StructTreeRoot /K 6 0 R >>",
				"<< /Type /StructElem /S /Document /P 5 0 R >>",
				"<< /Length 3 >>\nstream\nq Q\nendstream",
				"<< /Length 3 >>\nstream\nq Q\nendstream",
			))
			if _, err := fixContentTag(ctx, alloc, doc, Options{}, nil, "PDF/UA-1"); err != nil {
				t.Fatalf("fixContentTag: %v", err)
			}
			p1, p2 := pendingDict(t, alloc, 3), pendingDict(t, alloc, 4)
			if p1["StructParents"] == p2["StructParents"] {
				t.Errorf("pages share /StructParents %v, want a key each", p1["StructParents"])
			}
			for _, p := range []ast.PdfDict{p1, p2} {
				ref, ok := p["Contents"].(ast.PdfIndirectRef)
				if !ok {
					t.Fatalf("/Contents: got %T, want a wrapped stream", p["Contents"])
				}
				if data := pendingStreamBytes(t, alloc, ref.ObjectNum); !bytes.HasPrefix(data, []byte("/Document <</MCID 0>> BDC")) {
					t.Errorf("/Contents %v reads %q, want the wrapper", ref, data)
				}
			}
			if same := p1["Contents"] == p2["Contents"]; same != tc.shared {
				t.Errorf("pages name /Contents %v and %v, shared = %v, want %v", p1["Contents"], p2["Contents"], same, tc.shared)
			}
		})
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
