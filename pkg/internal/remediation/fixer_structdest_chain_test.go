package remediation

import (
	"context"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// chainedGoToPDF builds a tagged three-page PDF whose GoTo actions run after
// other actions through /Next (ISO 32000-1, 12.6.2, Table 193).
//
// Pages 1, 2 and 3 are objects 3, 10 and 11, and the paragraphs 12, 13 and 14
// begin on them in turn. The Document element 5 holding them begins on page 1
// and comes first in logical order, so a destination on page 1 names it (see
// [mutations.StructPageTargets]). The /OpenAction is a direct GoTo action for page 1 whose /Next
// is the GoTo action 15, for page 2. The outline item 7 holds a direct GoTo
// action for page 1 whose /Next is a direct array: a Named action, then 15.
// Page 1 carries the Link 8, whose GoTo action 16 for page 3 runs the URI
// action 17, then the GoTo action 18 for page 2, which runs 16 again, and the
// Link 9, whose direct Named action runs the array 19: a direct GoTo action
// for page 3 and a direct GoTo action already naming paragraph 12. Page 2's
// /AA /O is the Named action 20, whose /Next is a direct GoTo action for
// page 2.
func chainedGoToPDF() []byte {
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R /Outlines 6 0 R /OpenAction << /S /GoTo /D [3 0 R /Fit] /Next 15 0 R >> >>",
		"<< /Type /Pages /Kids [3 0 R 10 0 R 11 0 R] /Count 3 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /StructParents 0 /Annots [8 0 R 9 0 R] >>",
		"<< /Type /StructTreeRoot /K 5 0 R /ParentTree << /Nums [0 [12 0 R] 1 [13 0 R] 2 [14 0 R]] >> >>",
		"<< /Type /StructElem /S /Document /P 4 0 R /K [12 0 R 13 0 R 14 0 R] >>",
		"<< /Type /Outlines /First 7 0 R /Last 7 0 R /Count 1 >>",
		"<< /Title (One) /Parent 6 0 R /A << /S /GoTo /D [3 0 R /Fit] /Next [<< /S /Named /N /NextPage >> 15 0 R] >> >>",
		"<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] /A 16 0 R >>",
		"<< /Type /Annot /Subtype /Link /Rect [0 20 10 30] /A << /S /Named /N /NextPage /Next 19 0 R >> >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /StructParents 1 /AA << /O 20 0 R >> >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /StructParents 2 >>",
		"<< /Type /StructElem /S /P /P 5 0 R /Pg 3 0 R /K 0 >>",
		"<< /Type /StructElem /S /P /P 5 0 R /Pg 10 0 R /K 0 >>",
		"<< /Type /StructElem /S /P /P 5 0 R /Pg 11 0 R /K 0 >>",
		"<< /S /GoTo /D [10 0 R /Fit] >>",
		"<< /S /GoTo /D [11 0 R /Fit] /Next 17 0 R >>",
		"<< /S /URI /URI (https://example.com/) /Next 18 0 R >>",
		"<< /S /GoTo /D [10 0 R /FitH 700] /Next 16 0 R >>",
		"[<< /S /GoTo /D [11 0 R /Fit] >> << /S /GoTo /D [3 0 R /Fit] /SD [12 0 R /Fit] >>]",
		"<< /S /Named /N /FirstPage /Next << /S /GoTo /D [10 0 R /Fit] >> >>",
	)
}

// TestFixStructDestChainedGoToActions pins that every GoTo action an action
// runs through /Next gains a structure destination by the rules its head
// follows (ISO 14289-2, 8.8), however the chain is written: an action or an
// array, direct or an object of its own, looping back on itself or shared by
// two owners. An object of its own is rewritten at its object number, and the
// actions that are no GoTo action keep their entries.
func TestFixStructDestChainedGoToActions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc, alloc := newTestDoc(t, chainedGoToPDF())

	fixes, err := fixStructDest(ctx, alloc, doc, Options{}, nil, "PDF/UA-2")
	if err != nil {
		t.Fatalf("fixStructDest: %v", err)
	}
	if want := []string{"gave 7 destination(s) a structure destination"}; len(fixes) != 1 || fixes[0] != want[0] {
		t.Errorf("fixes = %v, want %v", fixes, want)
	}

	fit := ast.PdfName("Fit")
	ref := func(n int) ast.PdfIndirectRef { return ast.PdfIndirectRef{ObjectNum: n} }

	openAction, _ := pendingDict(t, alloc, 1)["OpenAction"].(ast.PdfDict)
	wantStructGoTo(t, "OpenAction", openAction, 3, 5, fit)
	if openAction["Next"] != ref(15) {
		t.Errorf("OpenAction /Next %v, want the shared action 15 kept by reference", openAction["Next"])
	}
	wantStructGoTo(t, "shared action 15", pendingDict(t, alloc, 15), 10, 13, fit)

	head, _ := pendingDict(t, alloc, 7)["A"].(ast.PdfDict)
	wantStructGoTo(t, "outline item", head, 3, 5, fit)
	next, _ := head["Next"].(ast.PdfArray)
	if len(next) != 2 || next[1] != ref(15) {
		t.Fatalf("outline item /Next %v, want the Named action and action 15", head["Next"])
	}
	if named, _ := next[0].(ast.PdfDict); len(named) != 2 || named["N"] != ast.PdfName("NextPage") {
		t.Errorf("the Named action became %v, want it untouched", next[0])
	}

	wantStructGoTo(t, "loop head 16", pendingDict(t, alloc, 16), 11, 14, fit)
	loopLast := pendingDict(t, alloc, 18)
	wantStructGoTo(t, "loop action 18", loopLast, 10, 13, ast.PdfName("FitH"), ast.PdfInt(700))
	if loopLast["Next"] != ref(16) {
		t.Errorf("action 18 /Next %v, want the loop back to 16 kept", loopLast["Next"])
	}

	arr, isArr := alloc.Pending()[19].(ast.PdfArray)
	if !isArr || len(arr) != 2 {
		t.Fatalf("array 19 = %v, want it rewritten at its own object number", alloc.Pending()[19])
	}
	wantStructGoTo(t, "array 19 [0]", arr[0], 11, 14, fit)
	wantStructGoTo(t, "array 19 [1]", arr[1], 3, 12, fit)

	named := pendingDict(t, alloc, 20)
	if named["S"] != ast.PdfName("Named") || named["N"] != ast.PdfName("FirstPage") {
		t.Errorf("action 20 = %v, want the Named action it was", named)
	}
	wantStructGoTo(t, "action 20 /Next", named["Next"], 10, 13, fit)

	// The URI action, the links and page 2 hold their actions by reference, and
	// keep their entries.
	for _, objNum := range []int{8, 9, 10, 17} {
		if _, rewritten := alloc.Pending()[objNum]; rewritten {
			t.Errorf("object %d was rewritten: %v", objNum, alloc.Pending()[objNum])
		}
	}

	if t1, t2 := structDest88(t, doc, alloc); t1 != 0 || t2 != 0 {
		t.Errorf("8.8 fires %d times on test 1 and %d on test 2, want neither", t1, t2)
	}
	again, err := fixStructDest(ctx, alloc, doc, Options{}, nil, "PDF/UA-2")
	if err != nil || again != nil {
		t.Errorf("second pass: fixes %v, error %v, want nothing left to rewrite", again, err)
	}
}

// TestFixStructDestChainFanOut pins that a /Next array written as an object of
// its own, whose direct actions each run it again, is walked once: walked anew
// wherever it is reached, it would fan out 2^depth.
func TestFixStructDestChainFanOut(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	doc, alloc := newTestDoc(t, buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R /OpenAction << /S /Named /N /NextPage /Next 6 0 R >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /StructParents 0 >>",
		"<< /Type /StructTreeRoot /K 5 0 R /ParentTree << /Nums [0 [5 0 R]] >> >>",
		"<< /Type /StructElem /S /P /P 4 0 R /Pg 3 0 R /K 0 >>",
		"[<< /S /GoTo /D [3 0 R /Fit] /Next 6 0 R >> << /S /GoTo /D [3 0 R /Fit] /Next 6 0 R >>]",
	))
	fixes, err := fixStructDest(ctx, alloc, doc, Options{}, nil, "PDF/UA-2")
	if ctx.Err() != nil {
		t.Fatal("the walk did not end before the deadline")
	}
	if err != nil {
		t.Fatalf("fixStructDest: %v", err)
	}
	if want := "gave 2 destination(s) a structure destination"; len(fixes) != 1 || fixes[0] != want {
		t.Errorf("fixes = %v, want [%s]", fixes, want)
	}
	arr, _ := alloc.Pending()[6].(ast.PdfArray)
	if len(arr) != 2 {
		t.Fatalf("array 6 = %v, want it rewritten at its own object number", alloc.Pending()[6])
	}
	for i, act := range arr {
		wantStructGoTo(t, "array 6 entry", act, 3, 5, ast.PdfName("Fit"))
		if d, _ := act.(ast.PdfDict); d["Next"] != (ast.PdfIndirectRef{ObjectNum: 6}) {
			t.Errorf("entry %d /Next %v, want the array kept by reference", i, d["Next"])
		}
	}
}
