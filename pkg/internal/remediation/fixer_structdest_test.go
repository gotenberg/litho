package remediation

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/mutations"
	"github.com/gotenberg/litho/pkg/models"
)

// structDestPDF builds a tagged three-page PDF whose destinations take every
// form the structure destination fixer meets.
//
// Pages 1, 2 and 3 are objects 3, 10 and 11. The Document element (5) holds
// paragraph 12, which begins on page 1 and continues on page 2, and paragraph
// 13, which begins on page 2. Page 2's /ParentTree entry lists paragraph 12
// first, and page 3 holds no structure content and no /StructParents.
//
// The /OpenAction is a destination array for page 1. The outline holds Alpha
// (7, a /Dest array for page 1, with a colour), Bravo (9, a /Dest array for
// page 3), Charlie (14, a direct GoTo action for page 2), Delta (15, whose
// GoTo action 17 names page 1) and Echo (16, a URI action beside a /Dest
// array). Page 2 carries the Link annotation 8, a /Dest array for page 2.
func structDestPDF() []byte {
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R /Outlines 6 0 R /OpenAction [3 0 R /XYZ null null 0] >>",
		"<< /Type /Pages /Kids [3 0 R 10 0 R 11 0 R] /Count 3 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /StructParents 0 >>",
		"<< /Type /StructTreeRoot /K 5 0 R /ParentTree << /Nums [0 [12 0 R] 1 [12 0 R 13 0 R]] >> >>",
		"<< /Type /StructElem /S /Document /P 4 0 R /K [12 0 R 13 0 R] >>",
		"<< /Type /Outlines /First 7 0 R /Last 16 0 R /Count 5 >>",
		"<< /Title (Alpha) /Parent 6 0 R /Next 9 0 R /C [1 0 0] /Dest [3 0 R /Fit] >>",
		"<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] /Border [0 0 0] /Dest [10 0 R /XYZ 0 792 0] >>",
		"<< /Title (Bravo) /Parent 6 0 R /Prev 7 0 R /Next 14 0 R /Dest [11 0 R /Fit] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /StructParents 1 /Annots [8 0 R] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructElem /S /P /P 5 0 R /Pg 3 0 R /K [0 << /Type /MCR /Pg 10 0 R /MCID 0 >>] >>",
		"<< /Type /StructElem /S /P /P 5 0 R /Pg 10 0 R /K 1 >>",
		"<< /Title (Charlie) /Parent 6 0 R /Prev 9 0 R /Next 15 0 R /A << /S /GoTo /D [10 0 R /Fit] >> >>",
		"<< /Title (Delta) /Parent 6 0 R /Prev 14 0 R /Next 16 0 R /A 17 0 R >>",
		"<< /Title (Echo) /Parent 6 0 R /Prev 15 0 R /A << /S /URI /URI (https://example.com) >> /Dest [3 0 R /Fit] >>",
		"<< /S /GoTo /D [3 0 R /Fit] >>",
	)
}

// wantStructGoTo checks that action is a GoTo action whose /D opens page with
// view and whose /SD opens elem with the same view.
func wantStructGoTo(t *testing.T, label string, action ast.PdfObject, page, elem int, view ...ast.PdfObject) {
	t.Helper()
	act, ok := action.(ast.PdfDict)
	if !ok {
		t.Fatalf("%s: action %v (%T), want a GoTo action dictionary", label, action, action)
	}
	if act["S"] != ast.PdfName("GoTo") {
		t.Errorf("%s: /S %v, want /GoTo (ISO 32000-1, Table 193 requires it)", label, act["S"])
	}
	want := func(key ast.PdfName, target int) {
		arr, isArr := act[key].(ast.PdfArray)
		if !isArr || len(arr) == 0 {
			t.Errorf("%s: /%s %v, want a destination array", label, string(key), act[key])
			return
		}
		if ref, isRef := arr[0].(ast.PdfIndirectRef); !isRef || ref.ObjectNum != target {
			t.Errorf("%s: /%s names %v, want object %d", label, string(key), arr[0], target)
		}
		if !slices.Equal(arr[1:], ast.PdfArray(view)) {
			t.Errorf("%s: /%s view %v, want %v", label, string(key), arr[1:], view)
		}
	}
	want("D", page)
	want("SD", elem)
}

// TestFixStructDestWritesGoToActions pins the form a structure destination
// takes: a GoTo action carrying the page destination under /D and the
// structure destination under /SD (ISO 32000-2, 12.6.4.2, Table 202), never a
// dictionary in /Dest, which ISO 32000-1, Table 153 and Table 173 do not
// allow and PDF.js reads as an action with no /S and follows nowhere. The
// target is the element whose content begins on the destination page.
func TestFixStructDestWritesGoToActions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc, alloc := newTestDoc(t, structDestPDF())

	fixes, err := fixStructDest(ctx, alloc, doc, Options{}, nil, "PDF/UA-2")
	if err != nil {
		t.Fatalf("fixStructDest: %v", err)
	}
	if len(fixes) != 1 {
		t.Fatalf("fixes = %v, want one description", fixes)
	}

	catalog := pendingDict(t, alloc, 1)
	wantStructGoTo(t, "OpenAction", catalog["OpenAction"], 3, 5, ast.PdfName("XYZ"), ast.PdfNull{}, ast.PdfNull{}, ast.PdfInt(0))

	alpha := pendingDict(t, alloc, 7)
	if _, has := alpha["Dest"]; has {
		t.Errorf("Alpha keeps /Dest %v beside its action, which ISO 32000-1, Table 153 forbids", alpha["Dest"])
	}
	wantStructGoTo(t, "Alpha", alpha["A"], 3, 5, ast.PdfName("Fit"))
	if _, has := alpha["C"]; !has || alpha["Next"] == nil || alpha["Title"] == nil {
		t.Errorf("Alpha lost an entry of its own: %v", alpha)
	}

	// Paragraph 12 holds page 2's first marked content but begins on page 1,
	// so a reader following it would open page 1: paragraph 13 begins on page 2.
	link := pendingDict(t, alloc, 8)
	if _, has := link["Dest"]; has {
		t.Errorf("the link keeps /Dest %v beside its action, which ISO 32000-1, Table 173 forbids", link["Dest"])
	}
	wantStructGoTo(t, "link", link["A"], 10, 13, ast.PdfName("XYZ"), ast.PdfInt(0), ast.PdfInt(792), ast.PdfInt(0))
	if _, has := link["Border"]; !has {
		t.Errorf("the link lost its /Border: %v", link)
	}

	wantStructGoTo(t, "Charlie", pendingDict(t, alloc, 14)["A"], 10, 13, ast.PdfName("Fit"))
	wantStructGoTo(t, "Delta's action", pendingDict(t, alloc, 17), 3, 5, ast.PdfName("Fit"))
	if _, rewritten := alloc.Pending()[15]; rewritten {
		t.Error("Delta was rewritten, want its indirect action rewritten in place")
	}

	// No element begins on page 3, so Bravo keeps its page destination rather
	// than naming an element a reader would open on another page.
	if _, rewritten := alloc.Pending()[9]; rewritten {
		t.Errorf("Bravo was rewritten: %v", alloc.Pending()[9])
	}
	// Echo's URI action is what a reader follows, and the fixer does not
	// replace it.
	if _, rewritten := alloc.Pending()[16]; rewritten {
		t.Errorf("Echo was rewritten: %v", alloc.Pending()[16])
	}

	again, err := fixStructDest(ctx, alloc, doc, Options{}, nil, "PDF/UA-2")
	if err != nil || again != nil {
		t.Errorf("second pass: fixes %v, error %v, want nothing left to rewrite", again, err)
	}
}

// TestFixStructDestSatisfiesStructureDestinations pins what the rewrite
// answers once written: ISO 14289-2, 8.8 fires only on Bravo, whose page no
// element begins on, and on Echo, whose /Dest sits beside a URI action.
func TestFixStructDestSatisfiesStructureDestinations(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc, alloc := newTestDoc(t, structDestPDF())
	if _, err := fixStructDest(ctx, alloc, doc, Options{}, nil, "PDF/UA-2"); err != nil {
		t.Fatalf("fixStructDest: %v", err)
	}
	if t1, t2 := structDest88(t, doc, alloc); t1 != 2 || t2 != 0 {
		t.Errorf("8.8 fires %d times on test 1 and %d on test 2, want 2 (Bravo, Echo) and 0", t1, t2)
	}
}

// TestRemediatePDFUA2NamesAnElementOfAWrappedTree pins the structure
// destination fixer behind the single-doc-root fixer, which runs first and
// wraps a root holding two paragraphs in a Document (ISO 14289-2, 8.2.5.2):
// the outline item opening page 2 names the paragraph beginning there, read
// through the Document.
func TestRemediatePDFUA2NamesAnElementOfAWrappedTree(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc, alloc := newTestDoc(t, buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R /Outlines 7 0 R /MarkInfo << /Marked true >> /Lang (en) >>",
		"<< /Type /Pages /Kids [3 0 R 9 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /StructParents 0 >>",
		"<< /Type /StructTreeRoot /K [5 0 R 6 0 R] /ParentTree << /Nums [0 [5 0 R] 1 [6 0 R]] >> >>",
		"<< /Type /StructElem /S /P /P 4 0 R /Pg 3 0 R /K 0 >>",
		"<< /Type /StructElem /S /P /P 4 0 R /Pg 9 0 R /K 0 >>",
		"<< /Type /Outlines /First 8 0 R /Last 8 0 R /Count 1 >>",
		"<< /Title (Two) /Parent 7 0 R /Dest [9 0 R /Fit] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /StructParents 1 >>",
	))
	rep, err := RemediatePDFUA(ctx, doc, alloc, models.PDFUA2, Options{})
	if err != nil {
		t.Fatalf("RemediatePDFUA: %v", err)
	}
	if !slices.ContainsFunc(rep.Fixed, func(f string) bool { return strings.Contains(f, "Document element") }) {
		t.Fatalf("fixes %v, want the single-doc-root fixer to wrap the paragraphs", rep.Fixed)
	}
	wantStructGoTo(t, "Two", pendingDict(t, alloc, 8)["A"], 9, 6, ast.PdfName("Fit"))
}

// structDest88 writes the fixer's edits and counts ISO 14289-2, 8.8 tests 1
// and 2 on the output.
func structDest88(t *testing.T, doc *document.Document, alloc *writer.ObjAllocator) (t1, t2 int) {
	t.Helper()
	ctx := context.Background()
	var out bytes.Buffer
	if err := mutations.FullRewrite(ctx, &out, mutations.FullRewriteConfig{Doc: doc, Overrides: alloc.Pending()}); err != nil {
		t.Fatalf("FullRewrite: %v", err)
	}
	reread, err := document.NewDocument(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()), nil)
	if err != nil {
		t.Fatalf("reopening the output: %v", err)
	}
	errs, err := compliance.ValidatePDFUA(ctx, reread, models.PDFUA2)
	if err != nil {
		t.Fatalf("ValidatePDFUA: %v", err)
	}
	for _, v := range toViolations(errs) {
		switch v.Rule {
		case compliance.Rule_14289_2_8_8_t01:
			t1++
		case compliance.Rule_14289_2_8_8_t02:
			t2++
		}
	}
	return t1, t2
}

// namedStructDestPDF builds a tagged three-page PDF laid out as
// [structDestPDF], whose destinations name their target (ISO 32000-1,
// 12.3.2.3), sit in a dictionary under /Dest, or hang off additional actions.
//
// The /Names /Dests tree maps (p2) to page 2 and (sd) to a destination
// dictionary already carrying a structure destination, and the catalog /Dests
// maps /legacy to page 1. The outline holds Alpha (14, /Dest (p2)), Bravo (15,
// a GoTo action to (p2)), Charlie (16, /Dest (sd)) and Delta (17, /Dest
// (nope), which names nothing). Page 1 carries the Link 8 (/Dest /legacy) and
// the Link 9, whose /Dest holds a GoTo action for page 2. Page 2 opens with a
// GoTo action for page 2 under its /AA and carries the Widget 20, whose direct
// /AA holds a direct GoTo action for page 1 under /E and the indirect GoTo
// action 22 under /X, and the Link 21, whose /AA 23 holds a direct GoTo action
// to (p2).
func namedStructDestPDF() []byte {
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R /Outlines 6 0 R /Names << /Dests 7 0 R >> /Dests << /legacy [3 0 R /Fit] >> >>",
		"<< /Type /Pages /Kids [3 0 R 10 0 R 11 0 R] /Count 3 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /StructParents 0 /Annots [8 0 R 9 0 R] >>",
		"<< /Type /StructTreeRoot /K 5 0 R /ParentTree << /Nums [0 [12 0 R] 1 [12 0 R 13 0 R]] >> >>",
		"<< /Type /StructElem /S /Document /P 4 0 R /K [12 0 R 13 0 R] >>",
		"<< /Type /Outlines /First 14 0 R /Last 17 0 R /Count 4 >>",
		"<< /Names [(p2) [10 0 R /XYZ 0 792 0] (sd) 19 0 R] >>",
		"<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] /Dest /legacy >>",
		"<< /Type /Annot /Subtype /Link /Rect [0 20 10 30] /Dest << /Type /Action /S /GoTo /D [10 0 R /Fit] >> >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /StructParents 1 /Annots [20 0 R 21 0 R] /AA << /O << /S /GoTo /D [10 0 R /Fit] >> >> >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructElem /S /P /P 5 0 R /Pg 3 0 R /K [0 << /Type /MCR /Pg 10 0 R /MCID 0 >>] >>",
		"<< /Type /StructElem /S /P /P 5 0 R /Pg 10 0 R /K 1 >>",
		"<< /Title (Alpha) /Parent 6 0 R /Next 15 0 R /Dest (p2) >>",
		"<< /Title (Bravo) /Parent 6 0 R /Prev 14 0 R /Next 16 0 R /A << /S /GoTo /D (p2) >> >>",
		"<< /Title (Charlie) /Parent 6 0 R /Prev 15 0 R /Next 17 0 R /Dest (sd) >>",
		"<< /Title (Delta) /Parent 6 0 R /Prev 16 0 R /Dest (nope) >>",
		"null",
		"<< /D [10 0 R /Fit] /SD [13 0 R /Fit] >>",
		"<< /Type /Annot /Subtype /Widget /Rect [0 40 10 50] /AA << /E << /S /GoTo /D [3 0 R /Fit] >> /X 22 0 R >> >>",
		"<< /Type /Annot /Subtype /Link /Rect [0 60 10 70] /AA 23 0 R >>",
		"<< /S /GoTo /D [10 0 R /Fit] >>",
		"<< /D << /S /GoTo /D (p2) >> >>",
	)
}

// TestFixStructDestNamedAndNestedDestinations pins the destinations
// [TestFixStructDestWritesGoToActions] does not reach. A named destination
// opens the page its name maps to (ISO 32000-1, 12.3.2.3), and the GoTo action
// replacing it keeps the name under /D, so the name tree stays the one table
// of targets. A GoTo action under an annotation's or a page's /AA gains /SD
// wherever it is written. A dictionary under /Dest, which ISO 32000-1, Tables
// 153 and 173 do not allow, becomes the /A action it stands for.
func TestFixStructDestNamedAndNestedDestinations(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc, alloc := newTestDoc(t, namedStructDestPDF())

	if _, err := fixStructDest(ctx, alloc, doc, Options{}, nil, "PDF/UA-2"); err != nil {
		t.Fatalf("fixStructDest: %v", err)
	}

	xyz := []ast.PdfObject{ast.PdfName("XYZ"), ast.PdfInt(0), ast.PdfInt(792), ast.PdfInt(0)}
	wantNamedGoTo := func(label string, action ast.PdfObject, name ast.PdfObject, elem int, view ...ast.PdfObject) {
		t.Helper()
		act, ok := action.(ast.PdfDict)
		if !ok {
			t.Fatalf("%s: action %v (%T), want a GoTo action dictionary", label, action, action)
		}
		if act["S"] != ast.PdfName("GoTo") || act["D"] != name {
			t.Errorf("%s: /S %v /D %v, want /GoTo and the name %v", label, act["S"], act["D"], name)
		}
		sd, isArr := act["SD"].(ast.PdfArray)
		if !isArr || len(sd) == 0 || sd[0] != (ast.PdfIndirectRef{ObjectNum: elem}) || !slices.Equal(sd[1:], ast.PdfArray(view)) {
			t.Errorf("%s: /SD %v, want element %d with view %v", label, act["SD"], elem, view)
		}
	}

	alpha := pendingDict(t, alloc, 14)
	if _, has := alpha["Dest"]; has {
		t.Errorf("Alpha keeps /Dest %v beside its action", alpha["Dest"])
	}
	wantNamedGoTo("Alpha", alpha["A"], ast.PdfString("p2"), 13, xyz...)
	wantNamedGoTo("Bravo", pendingDict(t, alloc, 15)["A"], ast.PdfString("p2"), 13, xyz...)
	wantNamedGoTo("link 8", pendingDict(t, alloc, 8)["A"], ast.PdfName("legacy"), 5, ast.PdfName("Fit"))

	link9 := pendingDict(t, alloc, 9)
	if _, has := link9["Dest"]; has {
		t.Errorf("link 9 keeps its dictionary under /Dest: %v", link9["Dest"])
	}
	wantStructGoTo(t, "link 9", link9["A"], 10, 13, ast.PdfName("Fit"))

	pageAA, _ := pendingDict(t, alloc, 10)["AA"].(ast.PdfDict)
	wantStructGoTo(t, "page 2 /AA /O", pageAA["O"], 10, 13, ast.PdfName("Fit"))

	widgetAA, _ := pendingDict(t, alloc, 20)["AA"].(ast.PdfDict)
	wantStructGoTo(t, "widget /AA /E", widgetAA["E"], 3, 5, ast.PdfName("Fit"))
	if widgetAA["X"] != (ast.PdfIndirectRef{ObjectNum: 22}) {
		t.Errorf("widget /AA /X %v, want its action 22 kept by reference", widgetAA["X"])
	}
	wantStructGoTo(t, "action 22", pendingDict(t, alloc, 22), 10, 13, ast.PdfName("Fit"))
	wantNamedGoTo("link 21 /AA /D", pendingDict(t, alloc, 23)["D"], ast.PdfString("p2"), 13, xyz...)
	if _, rewritten := alloc.Pending()[21]; rewritten {
		t.Error("link 21 was rewritten, want its indirect /AA rewritten in place")
	}

	// Charlie's name already maps to a structure destination, and Delta's to
	// nothing a reader could open.
	for _, objNum := range []int{7, 16, 17, 19} {
		if _, rewritten := alloc.Pending()[objNum]; rewritten {
			t.Errorf("object %d was rewritten: %v", objNum, alloc.Pending()[objNum])
		}
	}

	if t1, t2 := structDest88(t, doc, alloc); t1 != 1 || t2 != 0 {
		t.Errorf("8.8 fires %d times on test 1 and %d on test 2, want 1 (Delta) and 0", t1, t2)
	}
}

// TestFixStructDestNamesAnElementOfTheOnlyPage pins the destinations of a
// document of one page whose tree holds no content: the structure destination
// names the tree's first element, which a reader can open on no page but the
// destination's. A page object outside the page tree is no page of the
// document, and a document of two pages keeps its page destinations alone.
func TestFixStructDestNamesAnElementOfTheOnlyPage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	build := func(kids string, count int) []byte {
		return buildStructPDF(
			"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R /Outlines 6 0 R >>",
			fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", kids, count),
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
			"<< /Type /StructTreeRoot /K 5 0 R >>",
			"<< /Type /StructElem /S /Document /P 4 0 R /K [] >>",
			"<< /Type /Outlines /First 7 0 R /Last 8 0 R /Count 2 >>",
			"<< /Title (One) /Parent 6 0 R /Next 8 0 R /A << /S /GoTo /D [3 0 R /FitH 721] >> >>",
			"<< /Title (Orphan) /Parent 6 0 R /Prev 7 0 R /Dest [9 0 R /Fit] >>",
			"<< /Type /Page /Parent 10 0 R /MediaBox [0 0 612 792] >>",
			"<< /Type /Pages /Kids [9 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		)
	}

	doc, alloc := newTestDoc(t, build("3 0 R", 1))
	if _, err := fixStructDest(ctx, alloc, doc, Options{}, nil, "PDF/UA-2"); err != nil {
		t.Fatalf("fixStructDest: %v", err)
	}
	wantStructGoTo(t, "One", pendingDict(t, alloc, 7)["A"], 3, 5, ast.PdfName("FitH"), ast.PdfInt(721))
	if _, rewritten := alloc.Pending()[8]; rewritten {
		t.Errorf("Orphan was rewritten: %v", alloc.Pending()[8])
	}
	if t1, t2 := structDest88(t, doc, alloc); t1 != 1 || t2 != 0 {
		t.Errorf("8.8 fires %d times on test 1 and %d on test 2, want 1 (Orphan) and 0", t1, t2)
	}

	doc, alloc = newTestDoc(t, build("3 0 R 11 0 R", 2))
	fixes, err := fixStructDest(ctx, alloc, doc, Options{}, nil, "PDF/UA-2")
	if err != nil || fixes != nil {
		t.Errorf("two pages: fixes %v, error %v, want nothing rewritten", fixes, err)
	}
}

// TestRemediatePDFUA2NamesTheElementOfAnUntaggedPage pins the structure
// destination fixer behind the struct-tree fixer: an untagged document of
// one page gains a structure tree holding an empty Document, which its
// bookmark then names.
func TestRemediatePDFUA2NamesTheElementOfAnUntaggedPage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc, alloc := newTestDoc(t, buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /Outlines 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /Outlines /First 5 0 R /Last 5 0 R /Count 1 >>",
		"<< /Title (General) /Parent 4 0 R /A << /S /GoTo /D [3 0 R /FitH 721] >> >>",
	))
	rep, err := RemediatePDFUA(ctx, doc, alloc, models.PDFUA2, Options{})
	if err != nil {
		t.Fatalf("RemediatePDFUA: %v", err)
	}
	for _, v := range toViolations(rep.Residual) {
		if matchStructDest(v) {
			t.Errorf("residual %v", v)
		}
	}
	act, _ := pendingDict(t, alloc, 5)["A"].(ast.PdfDict)
	sd, _ := act["SD"].(ast.PdfArray)
	if len(sd) == 0 {
		t.Fatalf("General's action %v names no structure destination", act)
	}
	elem, _ := alloc.Pending()[sd[0].(ast.PdfIndirectRef).ObjectNum].(ast.PdfDict)
	if elem["S"] != ast.PdfName("Document") {
		t.Errorf("/SD names %v, want the Document element the struct-tree fixer wrote", elem)
	}
}

// TestFixStructDest_NoStructTree pins the guard: without a structure
// tree nothing is rewritten.
func TestFixStructDest_NoStructTree(t *testing.T) {
	ctx := context.Background()
	data := cyclePDFRemediation(t)
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(3)
	fixes, err := fixStructDest(ctx, alloc, doc, Options{}, nil, "PDF/UA-2")
	if err != nil || fixes != nil {
		t.Fatalf("want no-op without StructTreeRoot, got fixes=%v err=%v", fixes, err)
	}
}
