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

// streamObj returns the body of a stream object holding data, with extra
// entries in its dictionary.
func streamObj(extra, data string) string {
	return fmt.Sprintf("<< /Length %d %s >>\nstream\n%s\nendstream", len(data), extra, data)
}

// emptyFigurePDF builds a one-page tagged PDF whose page content marks MCID 0
// alone: catalog (1), pages (2), page (3), structure tree root (4), Document
// (5), the page's content (6), the parent tree (7), a P holding MCID 0 (8) and
// the Figure under test (9), listed after the P. extra objects follow from 10.
func emptyFigurePDF(figure string, extra ...string) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R /MarkInfo << /Marked true >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 6 0 R /StructParents 0 >>",
		"<< /Type /StructTreeRoot /K 5 0 R /ParentTree 7 0 R >>",
		"<< /Type /StructElem /S /Document /P 4 0 R /K [8 0 R 9 0 R] >>",
		streamObj("", "/P << /MCID 0 >> BDC 0 0 m 10 10 l S EMC"),
		"<< /Nums [0 [8 0 R 9 0 R]] >>",
		"<< /Type /StructElem /S /P /P 5 0 R /Pg 3 0 R /K 0 >>",
		figure,
	}
	return buildStructPDF(append(objs, extra...)...)
}

// liveKids returns the /K of the object at objNum as alloc stages it.
func liveKids(t *testing.T, doc *document.Document, alloc *writer.ObjAllocator, objNum int) ast.PdfArray {
	t.Helper()
	ctx := context.Background()
	d := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: objNum})
	if d == nil {
		t.Fatalf("object %d is no dictionary", objNum)
	}
	switch k := d["K"].(type) {
	case nil:
		return nil
	case ast.PdfArray:
		return k
	default:
		return ast.PdfArray{k}
	}
}

// listsRef reports whether list holds a reference to objNum.
func listsRef(list ast.PdfArray, objNum int) bool {
	return slices.ContainsFunc(list, func(o ast.PdfObject) bool {
		ref, isRef := o.(ast.PdfIndirectRef)
		return isRef && ref.ObjectNum == objNum
	})
}

// aggressive is the mode the removal runs in: content may change and the
// caller supplies no alternate text.
var aggressive = Options{PreserveContent: false}

func TestFixEmptyFigure_RemovesFigureEnclosingNothing(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, figure string }{
		{"no kid", "<< /Type /StructElem /S /Figure /P 5 0 R /Pg 3 0 R >>"},
		{"empty /K", "<< /Type /StructElem /S /Figure /P 5 0 R /Pg 3 0 R /K [] >>"},
		{"MCID no content marks", "<< /Type /StructElem /S /Figure /P 5 0 R /Pg 3 0 R /K 1 >>"},
		{"MCR no content marks", "<< /Type /StructElem /S /Figure /P 5 0 R /K << /Type /MCR /Pg 3 0 R /MCID 7 >> >>"},
		{"OBJR to an undefined object", "<< /Type /StructElem /S /Figure /P 5 0 R /Pg 3 0 R /K << /Type /OBJR /Obj 99 0 R >> >>"},
		{"OBJR without /Obj", "<< /Type /StructElem /S /Figure /P 5 0 R /Pg 3 0 R /K << /Type /OBJR >> >>"},
		{"null kid", "<< /Type /StructElem /S /Figure /P 5 0 R /Pg 3 0 R /K [null 98 0 R] >>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, alloc := newTestDoc(t, emptyFigurePDF(tc.figure))
			fixes, err := fixEmptyFigure(context.Background(), alloc, doc, aggressive, nil, "PDF/UA-2")
			if err != nil {
				t.Fatalf("fixEmptyFigure: %v", err)
			}
			if len(fixes) != 1 || !strings.Contains(fixes[0], "removed empty Figure 9 0 R") {
				t.Fatalf("fixes %v, want the removal of Figure 9 reported", fixes)
			}
			kids := liveKids(t, doc, alloc, 5)
			if listsRef(kids, 9) || !listsRef(kids, 8) {
				t.Errorf("Document /K = %v, want the P and not the Figure", kids)
			}
			slots, _ := pendingDict(t, alloc, 7)["Nums"].(ast.PdfArray)
			if want := (ast.PdfArray{ast.PdfInt(0), ast.PdfArray{ast.PdfIndirectRef{ObjectNum: 8}, ast.PdfNull{}}}); fmt.Sprint(slots) != fmt.Sprint(want) {
				t.Errorf("/ParentTree /Nums = %v, want %v: the Figure's slot nulled in place", slots, want)
			}
		})
	}
}

func TestFixEmptyFigure_KeepsFigureEnclosingContent(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		figure string
		extra  []string
	}{
		{name: "MCID content marks", figure: "<< /Type /StructElem /S /Figure /P 5 0 R /Pg 3 0 R /K 0 >>"},
		{name: "MCID with no /Pg", figure: "<< /Type /StructElem /S /Figure /P 5 0 R /K 1 >>"},
		{name: "MCID on no page", figure: "<< /Type /StructElem /S /Figure /P 5 0 R /Pg 5 0 R /K 1 >>"},
		{name: "MCR into a form XObject", figure: "<< /Type /StructElem /S /Figure /P 5 0 R /K << /Type /MCR /Pg 3 0 R /Stm 10 0 R /MCID 1 >> >>", extra: []string{
			streamObj("/Type /XObject /Subtype /Form /BBox [0 0 1 1]", "/Figure << /MCID 1 >> BDC EMC"),
		}},
		{name: "OBJR to an object", figure: "<< /Type /StructElem /S /Figure /P 5 0 R /Pg 3 0 R /K << /Type /OBJR /Obj 10 0 R >> >>", extra: []string{
			"<< /Type /Annot /Subtype /Link /Rect [0 0 1 1] >>",
		}},
		{name: "a kid element", figure: "<< /Type /StructElem /S /Figure /P 5 0 R /K 10 0 R >>", extra: []string{
			"<< /Type /StructElem /S /Caption /P 9 0 R >>",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, alloc := newTestDoc(t, emptyFigurePDF(tc.figure, tc.extra...))
			fixes, err := fixEmptyFigure(context.Background(), alloc, doc, aggressive, nil, "PDF/UA-2")
			if err != nil {
				t.Fatalf("fixEmptyFigure: %v", err)
			}
			if len(fixes) != 0 || len(alloc.Pending()) != 0 {
				t.Errorf("a Figure that may enclose content stays: fixes=%v pending=%d", fixes, len(alloc.Pending()))
			}
		})
	}
}

// TestFixEmptyFigure_KeepsFigureAFormMarks pins that a marked-content
// sequence in a form XObject the page's resources name counts as the page's:
// a form with no /StructParents marks its sequences for the page drawing it
// (ISO 32000-1, 14.7.4.3).
func TestFixEmptyFigure_KeepsFigureAFormMarks(t *testing.T) {
	t.Parallel()
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 6 0 R /StructParents 0 /Resources << /XObject << /Fm0 7 0 R >> >> >>",
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /Figure /P 4 0 R /Pg 3 0 R /K 3 >>",
		streamObj("", "/Fm0 Do"),
		streamObj("/Type /XObject /Subtype /Form /BBox [0 0 1 1]", "/Figure << /MCID 3 >> BDC 0 0 m 1 1 l S EMC"),
	)
	doc, alloc := newTestDoc(t, pdf)
	fixes, err := fixEmptyFigure(context.Background(), alloc, doc, aggressive, nil, "PDF/UA-2")
	if err != nil {
		t.Fatalf("fixEmptyFigure: %v", err)
	}
	if len(fixes) != 0 {
		t.Errorf("a Figure whose MCID a form of its page marks stays: %v", fixes)
	}
}

// TestFixEmptyFigure_KeepsFigureAnAppearanceMarks pins that a marked-content
// sequence in the appearance of an annotation on the page counts as the
// page's: an appearance is a form XObject (ISO 32000-1, 12.5.5).
func TestFixEmptyFigure_KeepsFigureAnAppearanceMarks(t *testing.T) {
	t.Parallel()
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /StructParents 0 /Annots [6 0 R] >>",
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /Figure /P 4 0 R /Pg 3 0 R /K 2 >>",
		"<< /Type /Annot /Subtype /Stamp /Rect [0 0 1 1] /AP << /N << /On 7 0 R >> >> >>",
		streamObj("/BBox [0 0 1 1]", "/Figure << /MCID 2 >> BDC 0 0 m 1 1 l S EMC"),
	)
	doc, alloc := newTestDoc(t, pdf)
	fixes, err := fixEmptyFigure(context.Background(), alloc, doc, aggressive, nil, "PDF/UA-2")
	if err != nil {
		t.Fatalf("fixEmptyFigure: %v", err)
	}
	if len(fixes) != 0 {
		t.Errorf("a Figure whose MCID an appearance on its page marks stays: %v", fixes)
	}
}

// TestFixEmptyFigure_KeepsFigureTheParentTreePairsWithContent pins that a
// /ParentTree slot naming the Figure for an identifier the page marks keeps
// it, even when its /K lists another: nulling the slot would untag that
// content.
func TestFixEmptyFigure_KeepsFigureTheParentTreePairsWithContent(t *testing.T) {
	t.Parallel()
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 6 0 R /StructParents 0 >>",
		"<< /Type /StructTreeRoot /K 5 0 R /ParentTree 7 0 R >>",
		"<< /Type /StructElem /S /Figure /P 4 0 R /Pg 3 0 R /K 1 >>",
		streamObj("", "/Figure << /MCID 0 >> BDC EMC"),
		"<< /Nums [0 [5 0 R]] >>",
	)
	doc, alloc := newTestDoc(t, pdf)
	fixes, err := fixEmptyFigure(context.Background(), alloc, doc, aggressive, nil, "PDF/UA-2")
	if err != nil {
		t.Fatalf("fixEmptyFigure: %v", err)
	}
	if len(fixes) != 0 {
		t.Errorf("a Figure the parent tree pairs with marked content stays: %v", fixes)
	}
}

func TestFixEmptyFigure_ModeGates(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		opts Options
	}{
		{"default mode", Options{PreserveContent: true}},
		{"caller alternate text, aggressive", Options{FigureAlt: "A chart"}},
		{"caller alternate text, default", Options{PreserveContent: true, FigureAlt: "A chart"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, alloc := newTestDoc(t, emptyFigurePDF("<< /Type /StructElem /S /Figure /P 5 0 R >>"))
			fixes, err := fixEmptyFigure(context.Background(), alloc, doc, tc.opts, nil, "PDF/UA-2")
			if err != nil {
				t.Fatalf("fixEmptyFigure: %v", err)
			}
			if len(fixes) != 0 || len(alloc.Pending()) != 0 {
				t.Errorf("the Figure stays: fixes=%v pending=%d", fixes, len(alloc.Pending()))
			}
		})
	}
}

// TestFixEmptyFigure_JudgesAlternateTextByProfile pins that only a Figure
// failing the profile's rule goes: an empty /Alt meets ISO 14289-2,
// 8.2.5.28.2 and fails ISO 14289-1, 7.3.
func TestFixEmptyFigure_JudgesAlternateTextByProfile(t *testing.T) {
	t.Parallel()
	figure := "<< /Type /StructElem /S /Figure /P 5 0 R /Alt () >>"
	for _, tc := range []struct {
		profile string
		removed bool
	}{
		{"PDF/UA-2", false},
		{"PDF/UA-1", true},
	} {
		t.Run(tc.profile, func(t *testing.T) {
			t.Parallel()
			doc, alloc := newTestDoc(t, emptyFigurePDF(figure))
			fixes, err := fixEmptyFigure(context.Background(), alloc, doc, aggressive, nil, tc.profile)
			if err != nil {
				t.Fatalf("fixEmptyFigure: %v", err)
			}
			if got := len(fixes) == 1; got != tc.removed {
				t.Errorf("removed = %v (fixes %v), want %v", got, fixes, tc.removed)
			}
		})
	}
}

// TestFixEmptyFigure_CascadesThroughFiguresOnly pins that an element the
// removal empties goes only when it is a Figure lacking alternate text too.
func TestFixEmptyFigure_CascadesThroughFiguresOnly(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		outer      string
		outerGoes  bool
		outerLabel string
	}{
		{"Figure lacking alternate text", "<< /Type /StructElem /S /Figure /P 5 0 R /K 10 0 R >>", true, ""},
		{"Figure with alternate text", "<< /Type /StructElem /S /Figure /P 5 0 R /Alt (A chart) /K 10 0 R >>", false, "Figure"},
		{"Div", "<< /Type /StructElem /S /Div /P 5 0 R /K 10 0 R >>", false, "Div"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, alloc := newTestDoc(t, emptyFigurePDF(tc.outer, "<< /Type /StructElem /S /Figure /P 9 0 R /Pg 3 0 R /K 4 >>"))
			fixes, err := fixEmptyFigure(context.Background(), alloc, doc, aggressive, nil, "PDF/UA-2")
			if err != nil {
				t.Fatalf("fixEmptyFigure: %v", err)
			}
			if !slices.ContainsFunc(fixes, func(f string) bool { return strings.Contains(f, "Figure 10 0 R") }) {
				t.Fatalf("fixes %v, want the inner Figure removed", fixes)
			}
			if got := listsRef(liveKids(t, doc, alloc, 5), 9); got == tc.outerGoes {
				t.Errorf("Document still lists the outer element = %v, want %v", got, !tc.outerGoes)
			}
			if !tc.outerGoes {
				if kids := liveKids(t, doc, alloc, 9); len(kids) != 0 {
					t.Errorf("the %s keeps /K %v, want none", tc.outerLabel, kids)
				}
			}
		})
	}
}

// TestFixEmptyFigure_KeepsFigureAnInlineElementHolds pins that a Figure an
// element written inline lists stays: the inline element has no object
// number to write its /K back at.
func TestFixEmptyFigure_KeepsFigureAnInlineElementHolds(t *testing.T) {
	t.Parallel()
	doc, alloc := newTestDoc(t, emptyFigurePDF("<< /Type /StructElem /S /Sect /P 5 0 R /K [<< /Type /StructElem /S /Div /K 10 0 R >>] >>",
		"<< /Type /StructElem /S /Figure >>"))
	fixes, err := fixEmptyFigure(context.Background(), alloc, doc, aggressive, nil, "PDF/UA-2")
	if err != nil {
		t.Fatalf("fixEmptyFigure: %v", err)
	}
	if len(fixes) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("fixes=%v pending=%d, want the Figure kept", fixes, len(alloc.Pending()))
	}
}

// TestFixEmptyFigure_RemovesFiguresWrittenDirectly pins that a Figure written
// directly in a /K, with no object number of its own, goes like any other
// when it encloses nothing: out of the element or the /K array written as an
// object of its own that holds it. A direct Figure enclosing content stays.
func TestFixEmptyFigure_RemovesFiguresWrittenDirectly(t *testing.T) {
	t.Parallel()
	empty := "<< /Type /StructElem /S /Figure /Pg 3 0 R /K 1 >>"
	marked := "<< /Type /StructElem /S /Figure /Pg 3 0 R /K 0 >>"
	for _, tc := range []struct {
		name  string
		elem  string
		extra []string
		owner int
		want  string
	}{
		{name: "in an element's /K array", elem: "<< /Type /StructElem /S /Sect /P 5 0 R /K [" + empty + " " + marked + "] >>", owner: 9, want: "[" + marked + "]"},
		{name: "as an element's /K", elem: "<< /Type /StructElem /S /Sect /P 5 0 R /K " + empty + " >>", owner: 9},
		{name: "nested in a Figure that goes too", elem: "<< /Type /StructElem /S /Sect /P 5 0 R /K [<< /Type /StructElem /S /Figure /K [" + empty + " null] >> " + marked + "] >>", owner: 9, want: "[" + marked + "]"},
		{name: "under an inline element", elem: "<< /Type /StructElem /S /Sect /P 5 0 R /K [<< /Type /StructElem /S /Div /K [" + empty + " " + marked + "] >>] >>", owner: 9, want: "[<< /Type /StructElem /S /Div /K [" + marked + "] >>]"},
		{name: "in a /K array two elements share", elem: "<< /Type /StructElem /S /Sect /P 5 0 R /K 10 0 R >>", extra: []string{
			"[" + empty + " " + marked + "]",
			"<< /Type /StructElem /S /Sect /P 5 0 R /K 10 0 R >>",
		}, owner: 10, want: "[" + marked + "]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, alloc := newTestDoc(t, emptyFigurePDF(tc.elem, tc.extra...))
			fixes, err := fixEmptyFigure(context.Background(), alloc, doc, aggressive, nil, "PDF/UA-2")
			if err != nil {
				t.Fatalf("fixEmptyFigure: %v", err)
			}
			if len(fixes) != 1 || !strings.Contains(fixes[0], "removed 1 empty Figure(s) written directly") {
				t.Fatalf("fixes %v, want one direct Figure removed", fixes)
			}
			if len(alloc.Pending()) != 1 {
				t.Errorf("pending %d objects, want the owner %d alone", len(alloc.Pending()), tc.owner)
			}
			got := alloc.Pending()[tc.owner]
			if d, isDict := got.(ast.PdfDict); isDict {
				got = d["K"]
			}
			var want ast.PdfObject
			if tc.want != "" {
				// The expected value is read back from a file holding it as
				// an object of its own, after the fixture's.
				wantDoc, _ := newTestDoc(t, emptyFigurePDF(tc.elem, append(slices.Clone(tc.extra), tc.want)...))
				resolved, err := wantDoc.Resolve(context.Background(), ast.PdfIndirectRef{ObjectNum: 10 + len(tc.extra)})
				if err != nil {
					t.Fatalf("reading the expected value: %v", err)
				}
				want = resolved
			}
			if fmt.Sprint(got) != fmt.Sprint(want) {
				t.Errorf("owner %d holds %v, want %v", tc.owner, got, want)
			}
		})
	}
}

// TestFixEmptyFigure_DirectFigureNamingItsOwnArrayStays pins that a Figure
// written directly in a /K array that it names again as its own /K ends the
// walk and stays: it may enclose itself, and nothing shows it encloses
// nothing.
func TestFixEmptyFigure_DirectFigureNamingItsOwnArrayStays(t *testing.T) {
	t.Parallel()
	doc, alloc := newTestDoc(t, emptyFigurePDF("<< /Type /StructElem /S /Sect /P 5 0 R /K 10 0 R >>",
		"[<< /Type /StructElem /S /Figure /K 10 0 R >> << /Type /StructElem /S /Figure /K 10 0 R >>]"))
	fixes, err := fixEmptyFigure(context.Background(), alloc, doc, aggressive, nil, "PDF/UA-2")
	if err != nil {
		t.Fatalf("fixEmptyFigure: %v", err)
	}
	if len(fixes) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("fixes=%v pending=%d, want both Figures kept", fixes, len(alloc.Pending()))
	}
}

// TestFixEmptyFigure_UnreadableParentTreeKeepsEveryFigure pins that a
// /ParentTree the walk cannot read whole leaves every Figure in place: a slot
// naming the Figure could stay behind.
func TestFixEmptyFigure_UnreadableParentTreeKeepsEveryFigure(t *testing.T) {
	t.Parallel()
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K 5 0 R /ParentTree << /Nums [0] >> >>",
		"<< /Type /StructElem /S /Figure /P 4 0 R >>",
	)
	doc, alloc := newTestDoc(t, pdf)
	fixes, err := fixEmptyFigure(context.Background(), alloc, doc, aggressive, nil, "PDF/UA-2")
	if err != nil {
		t.Fatalf("fixEmptyFigure: %v", err)
	}
	if len(fixes) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("fixes=%v pending=%d, want the Figure kept", fixes, len(alloc.Pending()))
	}
}

// referencesPDF builds a two-page tagged PDF whose Figures 9 (page 3) and 12
// (page 13) enclose nothing, each named by every kind of reference the
// removal cleans: the /IDTree, a /Ref, the /ParentTree, an outline item's
// /SE and a link's GoTo /SD. Page 3 holds a P (8), so the Document begins on it
// and a structure destination can name it instead; page 13 holds nothing else,
// so Figure 12 is the only element a structure destination to it can name.
func referencesPDF() []byte {
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R /Outlines 16 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 13 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 6 0 R /StructParents 0 /Annots [10 0 R] >>",
		"<< /Type /StructTreeRoot /K 5 0 R /ParentTree 7 0 R /IDTree << /Names [(fig1) 9 0 R (fig2) 12 0 R (para) 8 0 R] >> >>",
		"<< /Type /StructElem /S /Document /P 4 0 R /K [9 0 R 8 0 R 12 0 R 11 0 R] >>",
		streamObj("", "/P << /MCID 0 >> BDC 0 0 m 10 10 l S EMC"),
		"<< /Nums [0 [8 0 R 9 0 R] 1 [12 0 R] 2 11 0 R 3 9 0 R] >>",
		"<< /Type /StructElem /S /P /P 5 0 R /Pg 3 0 R /K 0 /ID (para) >>",
		"<< /Type /StructElem /S /Figure /P 5 0 R /Pg 3 0 R /K 1 /ID (fig1) >>",
		"<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] /StructParent 2 /A << /S /GoTo /D [3 0 R /Fit] /SD [9 0 R /Fit] /Next << /S /GoTo /D [13 0 R /Fit] /SD [12 0 R /Fit] >> >> >>",
		"<< /Type /StructElem /S /Link /P 5 0 R /Pg 3 0 R /K << /Type /OBJR /Obj 10 0 R >> /Ref [9 0 R 8 0 R 12 0 R] >>",
		"<< /Type /StructElem /S /Figure /P 5 0 R /Pg 13 0 R /K 0 /ID (fig2) >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 14 0 R /StructParents 1 >>",
		streamObj("", "0 0 m 10 10 l S"),
		"<< /Title (Figure) /Parent 16 0 R /SE 9 0 R /A << /S /GoTo /D [3 0 R /Fit] /SD [9 0 R /Fit] >> >>",
		"<< /Type /Outlines /First 15 0 R /Last 15 0 R /Count 1 >>",
	)
}

func TestFixEmptyFigure_CleansEveryReference(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc, alloc := newTestDoc(t, referencesPDF())
	fixes, err := fixEmptyFigure(ctx, alloc, doc, aggressive, nil, "PDF/UA-2")
	if err != nil {
		t.Fatalf("fixEmptyFigure: %v", err)
	}
	for _, want := range []string{"removed empty Figure 9 0 R", "2 structure destination(s) retargeted, 1 outline /SE"} {
		if !slices.ContainsFunc(fixes, func(f string) bool { return strings.Contains(f, want) }) {
			t.Errorf("fixes %v, want %q", fixes, want)
		}
	}
	if slices.ContainsFunc(fixes, func(f string) bool { return strings.Contains(f, "Figure 12 0 R") }) {
		t.Errorf("fixes %v: Figure 12 is the only element the /SD to page 13 can name, and stays", fixes)
	}

	if kids := liveKids(t, doc, alloc, 5); listsRef(kids, 9) || !listsRef(kids, 12) || len(kids) != 3 {
		t.Errorf("Document /K = %v, want the P, Figure 12 and the Link", kids)
	}
	if ref := pendingDict(t, alloc, 11)["Ref"]; fmt.Sprint(ref) != fmt.Sprint(ast.PdfArray{ast.PdfIndirectRef{ObjectNum: 8}, ast.PdfIndirectRef{ObjectNum: 12}}) {
		t.Errorf("Link /Ref = %v, want the P and Figure 12", ref)
	}

	root := pendingDict(t, alloc, 4)
	ids, _ := root["IDTree"].(ast.PdfDict)
	if names := fmt.Sprint(ids["Names"]); strings.Contains(names, "fig1") || !strings.Contains(names, "fig2") || !strings.Contains(names, "para") {
		t.Errorf("/IDTree /Names = %s, want the P's and Figure 12's identifiers", names)
	}
	wantNums := ast.PdfArray{
		ast.PdfInt(0), ast.PdfArray{ast.PdfIndirectRef{ObjectNum: 8}, ast.PdfNull{}},
		ast.PdfInt(1), ast.PdfArray{ast.PdfIndirectRef{ObjectNum: 12}},
		ast.PdfInt(2), ast.PdfIndirectRef{ObjectNum: 11},
	}
	if nums := pendingDict(t, alloc, 7)["Nums"]; fmt.Sprint(nums) != fmt.Sprint(wantNums) {
		t.Errorf("/ParentTree /Nums = %v, want %v", nums, wantNums)
	}

	// The link's action opens page 3, where the Document begins, and its /Next
	// opens page 13, where Figure 12 alone begins: removing it would drop the
	// /SD and leave ISO 14289-2, 8.8 unmet.
	act, _ := pendingDict(t, alloc, 10)["A"].(ast.PdfDict)
	if sd := fmt.Sprint(act["SD"]); sd != fmt.Sprint(ast.PdfArray{ast.PdfIndirectRef{ObjectNum: 5}, ast.PdfName("Fit")}) {
		t.Errorf("link /SD = %s, want the Document, the outermost element beginning on page 3", sd)
	}
	next, _ := act["Next"].(ast.PdfDict)
	if sd := fmt.Sprint(next["SD"]); sd != fmt.Sprint(ast.PdfArray{ast.PdfIndirectRef{ObjectNum: 12}, ast.PdfName("Fit")}) || next["D"] == nil {
		t.Errorf("/Next = %v, want its /SD naming Figure 12 and its /D kept", next)
	}
	item := pendingDict(t, alloc, 15)
	if _, has := item["SE"]; has {
		t.Errorf("outline item %v still names the removed Figure in /SE", item)
	}
	if sd := fmt.Sprint(item["A"].(ast.PdfDict)["SD"]); sd != fmt.Sprint(ast.PdfArray{ast.PdfIndirectRef{ObjectNum: 5}, ast.PdfName("Fit")}) {
		t.Errorf("outline /SD = %s, want the Document", sd)
	}
}

// TestFixEmptyFigure_KeepsFigureADestinationNeeds pins that a destination
// naming a Figure with no page destination beside it keeps the Figure: an
// explicit destination whose target is the Figure, and an /SD beside no /D.
func TestFixEmptyFigure_KeepsFigureADestinationNeeds(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, annot string }{
		{"destination array", "<< /Type /Annot /Subtype /Link /Rect [0 0 1 1] /Dest [9 0 R /XYZ null null null] >>"},
		{"/SD beside no /D", "<< /Type /Annot /Subtype /Link /Rect [0 0 1 1] /A << /S /GoTo /SD [9 0 R /Fit] >> >>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, alloc := newTestDoc(t, emptyFigurePDF("<< /Type /StructElem /S /Figure /P 5 0 R >>", tc.annot))
			fixes, err := fixEmptyFigure(context.Background(), alloc, doc, aggressive, nil, "PDF/UA-2")
			if err != nil {
				t.Fatalf("fixEmptyFigure: %v", err)
			}
			if len(fixes) != 0 || len(alloc.Pending()) != 0 {
				t.Errorf("fixes=%v pending=%d, want the Figure kept", fixes, len(alloc.Pending()))
			}
		})
	}
}

// TestRegistryRemovesEmptyFiguresBeforeNamingThem pins the order the removal
// needs: after the fixers that build the tree, and before struct-dest, which
// would name a Figure that goes, and figure-alt, which would give it a
// placeholder.
func TestRegistryRemovesEmptyFiguresBeforeNamingThem(t *testing.T) {
	t.Parallel()
	pos := make(map[string]int, len(registry))
	for i, f := range registry {
		pos[f.name] = i
	}
	for _, before := range []string{"parent-tree", "single-doc-root"} {
		if pos[before] > pos["empty-figure"] {
			t.Errorf("%s runs after empty-figure", before)
		}
	}
	for _, after := range []string{"struct-dest", "figure-alt"} {
		if pos[after] < pos["empty-figure"] {
			t.Errorf("%s runs before empty-figure", after)
		}
	}
}

// TestRemediatePDFUA_RemovesEmptyFigures pins the whole run: an aggressive
// PDF/UA-2 remediation takes the empty Figure out and gives no placeholder,
// its output no longer fails ISO 14289-2, 8.2.5.28.2, and a caller's
// alternate text keeps the Figure instead.
func TestRemediatePDFUA_RemovesEmptyFigures(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, tc := range []struct {
		name    string
		opts    Options
		removed bool
	}{
		{"aggressive", aggressive, true},
		{"aggressive with caller alternate text", Options{FigureAlt: "A chart"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, alloc := newTestDoc(t, emptyFigurePDF("<< /Type /StructElem /S /Figure /P 5 0 R /Pg 3 0 R /K 1 >>"))
			rep, err := RemediatePDFUA(ctx, doc, alloc, models.PDFUA2, tc.opts)
			if err != nil {
				t.Fatalf("RemediatePDFUA: %v", err)
			}
			removed := slices.ContainsFunc(rep.Fixed, func(f string) bool { return strings.Contains(f, "removed empty Figure 9 0 R") })
			placeholder := slices.ContainsFunc(rep.Fixed, func(f string) bool { return strings.Contains(f, "/Alt on") })
			if removed != tc.removed || placeholder == tc.removed {
				t.Fatalf("fixes %v: removed=%v alt=%v, want removed=%v", rep.Fixed, removed, placeholder, tc.removed)
			}

			var out bytes.Buffer
			if err := mutations.FullRewrite(ctx, &out, mutations.FullRewriteConfig{Doc: doc, Overrides: alloc.Pending(), RequiredVersion: rep.RequiredVersion}); err != nil {
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
				if matchFigureAlt(v) {
					t.Errorf("the output fails %v", v)
				}
			}
		})
	}
}
