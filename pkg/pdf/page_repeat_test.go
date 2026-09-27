package pdf

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// repeatFormPDF builds a two-page document whose first page draws Alpha and
// carries a widget of the text field "name", whose other widget is on page 2,
// the widget of the check box "agree", merged with its field, a link to page 2,
// and a text annotation with its pop-up. Page 2 draws Bravo. Every widget
// shares one appearance stream.
func repeatFormPDF() []byte {
	stream := func(dict, content string) string {
		return fmt.Sprintf("<< %s /Length %d >>\nstream\n%s\nendstream", dict, len(content), content)
	}
	return buildPDF([]string{
		// 1: catalog.
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [7 0 R 9 0 R] /DA (/F1 0 Tf 0 g) " +
			"/DR << /Font << /F1 13 0 R >> >> >> >>",
		// 2: pages.
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		// 3-4: pages.
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 5 0 R " +
			"/Resources << /Font << /F1 13 0 R >> >> /Annots [8 0 R 9 0 R 11 0 R 12 0 R 14 0 R] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 6 0 R " +
			"/Resources << /Font << /F1 13 0 R >> >> /Annots [10 0 R] >>",
		// 5-6: content.
		stream("", "BT /F1 12 Tf 50 700 Td (Alpha) Tj ET"),
		stream("", "BT /F1 12 Tf 50 700 Td (Bravo) Tj ET"),
		// 7: the text field, with a widget on each page.
		"<< /FT /Tx /T (name) /V (Ann) /Kids [8 0 R 10 0 R] >>",
		// 8: its widget on page 1.
		"<< /Type /Annot /Subtype /Widget /Parent 7 0 R /P 3 0 R /Rect [50 600 250 620] /F 4 /AP << /N 15 0 R >> >>",
		// 9: a check box merged with its widget.
		"<< /Type /Annot /Subtype /Widget /FT /Btn /T (agree) /V /Off /AS /Off /P 3 0 R " +
			"/Rect [50 550 70 570] /F 4 /AP << /N << /Yes 15 0 R /Off 15 0 R >> >> >>",
		// 10: the text field's widget on page 2.
		"<< /Type /Annot /Subtype /Widget /Parent 7 0 R /P 4 0 R /Rect [50 600 250 620] /F 4 /AP << /N 15 0 R >> >>",
		// 11: a link to page 2.
		"<< /Type /Annot /Subtype /Link /Rect [50 500 150 520] /Border [0 0 0] /P 3 0 R /Dest [4 0 R /Fit] >>",
		// 12: a text annotation.
		"<< /Type /Annot /Subtype /Text /Rect [300 700 320 720] /Contents (note) /P 3 0 R /Popup 14 0 R /F 4 >>",
		// 13: font.
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
		// 14: its pop-up.
		"<< /Type /Annot /Subtype /Popup /Rect [320 600 500 700] /Parent 12 0 R /P 3 0 R >>",
		// 15: the widgets' appearance.
		stream("/Type /XObject /Subtype /Form /BBox [0 0 200 20] /Resources << /Font << /F1 13 0 R >> >>",
			"BT /F1 10 Tf 2 5 Td (Ann) Tj ET"),
	})
}

// pageAnnots returns the annotation references each page of o lists.
func (o *outputTree) pageAnnots() [][]ast.PdfIndirectRef {
	out := make([][]ast.PdfIndirectRef, len(o.pages))
	for i, page := range o.pages {
		annots, _ := document.ResolveArray(o.ctx, o.doc, page.Dict["Annots"])
		for _, annot := range annots {
			if ref, isRef := annot.(ast.PdfIndirectRef); isRef {
				out[i] = append(out[i], ref)
			}
		}
	}
	return out
}

// assertAnnotationsHaveOnePage checks that every annotation of o is listed by
// one page, which its /P names (ISO 32000-1, Table 164), and that every widget
// with a parent field is one of its kids, whose every kid a page lists.
func (o *outputTree) assertAnnotationsHaveOnePage(t *testing.T) {
	t.Helper()
	owner := make(map[int]int)
	for i, annots := range o.pageAnnots() {
		for _, ref := range annots {
			if page, listed := owner[ref.ObjectNum]; listed {
				t.Errorf("annotation %d is listed by pages %d and %d", ref.ObjectNum, page+1, i+1)
				continue
			}
			owner[ref.ObjectNum] = i
			annot := o.dict(ref)
			if p, _ := annot["P"].(ast.PdfIndirectRef); p.ObjectNum != o.pages[i].Ref.ObjectNum {
				t.Errorf("annotation %d on page %d names %v as its page", ref.ObjectNum, i+1, annot["P"])
			}
		}
	}
	for num := range owner {
		annot := o.dict(ast.PdfIndirectRef{ObjectNum: num})
		parent, hasParent := annot["Parent"].(ast.PdfIndirectRef)
		if subtype, _ := annot["Subtype"].(ast.PdfName); subtype != "Widget" || !hasParent {
			continue
		}
		kids, _ := document.ResolveArray(o.ctx, o.doc, o.dict(parent)["Kids"])
		if !slices.Contains(kids, ast.PdfObject(ast.PdfIndirectRef{ObjectNum: num})) {
			t.Errorf("widget %d is not a kid of its field %d", num, parent.ObjectNum)
		}
		for _, kid := range kids {
			if ref, _ := kid.(ast.PdfIndirectRef); !hasKey(owner, ref.ObjectNum) {
				t.Errorf("field %d lists widget %v, which no page lists", parent.ObjectNum, kid)
			}
		}
	}
}

func hasKey(m map[int]int, k int) bool {
	_, ok := m[k]
	return ok
}

// formRootNames returns the partial names of the root fields the output's
// /AcroForm lists, sorted, and fails the test when a widget on a page belongs
// to a field the form does not reach (ISO 32000-1, 12.7.2).
func (o *outputTree) formRootNames(t *testing.T) []string {
	t.Helper()
	catalog, err := document.ResolveCatalog(o.ctx, o.doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	roots, _ := document.ResolveArray(o.ctx, o.doc, o.dict(catalog.Dict["AcroForm"])["Fields"])
	var names []string
	for _, root := range roots {
		name, _ := document.ResolveString(o.ctx, o.doc, o.dict(root)["T"])
		names = append(names, document.DecodePdfString(name))
	}
	for i, annots := range o.pageAnnots() {
		for _, ref := range annots {
			if o.dict(ref)["Subtype"] != ast.PdfName("Widget") {
				continue
			}
			root := ref
			for range maxFieldParentDepthForTest {
				parent, hasParent := o.dict(root)["Parent"].(ast.PdfIndirectRef)
				if !hasParent {
					break
				}
				root = parent
			}
			if !slices.Contains(roots, ast.PdfObject(root)) {
				t.Errorf("page %d: widget %d belongs to field %d, which /AcroForm /Fields does not list", i+1, ref.ObjectNum, root.ObjectNum)
			}
		}
	}
	slices.Sort(names)
	return names
}

// annotOfSubtype returns the annotation of the given subtype page lists.
func (o *outputTree) annotOfSubtype(t *testing.T, page int, subtype ast.PdfName) ast.PdfDict {
	t.Helper()
	for _, ref := range o.pageAnnots()[page] {
		if annot := o.dict(ref); annot["Subtype"] == subtype {
			return annot
		}
	}
	t.Fatalf("page %d lists no %s annotation", page+1, subtype)
	return nil
}

// TestSplit_RepeatedPageIsAPageOfItsOwn pins that a page named twice is two
// pages of the output. A page object has one /Parent and one place in the
// page tree (ISO 32000-1, 7.7.3.3), so the second naming used to name the
// first copy again and the output held the page once. Each copy has
// annotations of its own, whose /P names it (Table 164), a widget of its own
// that its field lists, and shares the first copy's content and resources.
func TestSplit_RepeatedPageIsAPageOfItsOwn(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	src := openBytes(t, repeatFormPDF())
	defer src.Close()

	var out bytes.Buffer
	if err := Split(ctx, src, []int{1, 2, 1}, &out); err != nil {
		t.Fatalf("Split: %v", err)
	}
	if got, want := pageTexts(t, openBytes(t, out.Bytes())), []string{"Alpha", "Bravo", "Alpha"}; !slices.Equal(got, want) {
		t.Fatalf("page texts %q, want %q", got, want)
	}
	o := readOutputTree(t, out.Bytes())
	if o.pages[0].Ref == o.pages[2].Ref {
		t.Fatalf("pages 1 and 3 are the same object %v", o.pages[0].Ref)
	}
	o.assertAnnotationsHaveOnePage(t)
	if got := len(o.pageAnnots()[2]); got != 5 {
		t.Errorf("the repeated page lists %d annotations, want 5", got)
	}
	if got, want := o.formRootNames(t), []string{"agree", "agree", "name", "name"}; !slices.Equal(got, want) {
		t.Errorf("the form lists %q, want %q", got, want)
	}

	// The link on each copy lands on the one page 2 of the output.
	for _, page := range []int{0, 2} {
		dest, _ := document.ResolveArray(ctx, o.doc, o.annotOfSubtype(t, page, "Link")["Dest"])
		if len(dest) == 0 || dest[0] != ast.PdfObject(o.pages[1].Ref) {
			t.Errorf("the link on page %d goes to %v, want page 2 %v", page+1, dest, o.pages[1].Ref)
		}
	}
	// Each copy's text annotation opens the pop-up on its own page.
	note := o.annotOfSubtype(t, 2, "Text")
	popup, _ := note["Popup"].(ast.PdfIndirectRef)
	if !slices.Contains(o.pageAnnots()[2], popup) {
		t.Errorf("the repeated page's note opens pop-up %v, which it does not list", popup)
	}

	// What draws the page is shared: its content, its font and the widgets'
	// appearance.
	if first, again := o.pages[0].Dict["Contents"], o.pages[2].Dict["Contents"]; first != again {
		t.Errorf("the copies draw %v and %v, want one content stream", first, again)
	}
	firstFont := o.dict(o.pages[0].Dict["Resources"])["Font"]
	againFont := o.dict(o.pages[2].Dict["Resources"])["Font"]
	if o.dict(firstFont)["F1"] != o.dict(againFont)["F1"] {
		t.Errorf("the copies use fonts %v and %v, want one", o.dict(firstFont)["F1"], o.dict(againFont)["F1"])
	}
	if first, again := o.annotOfSubtype(t, 0, "Widget")["AP"], o.annotOfSubtype(t, 2, "Widget")["AP"]; o.dict(first)["N"] != o.dict(again)["N"] {
		t.Errorf("the copies' widgets draw %v and %v, want one appearance", o.dict(first)["N"], o.dict(again)["N"])
	}
}

// TestInsertPages_RepeatedSourcePageIsAPageOfItsOwn pins that a source page
// inserted twice is two pages of the document, each with its own
// annotations, as a repeated page of a split is.
func TestInsertPages_RepeatedSourcePageIsAPageOfItsOwn(t *testing.T) {
	t.Parallel()
	out := writeLabelled(t, labelledPDF(2, ""), func(w *Writer) error {
		return w.InsertPages(1, openBytes(t, repeatFormPDF()), 1, 1)
	})
	if got, want := pageTexts(t, openBytes(t, out)), []string{"", "Alpha", "Alpha", ""}; !slices.Equal(got, want) {
		t.Fatalf("page texts %q, want %q", got, want)
	}
	o := readOutputTree(t, out)
	if o.pages[1].Ref == o.pages[2].Ref {
		t.Fatalf("pages 2 and 3 are the same object %v", o.pages[1].Ref)
	}
	o.assertAnnotationsHaveOnePage(t)
	if got, want := o.formRootNames(t), []string{"agree", "agree", "name", "name"}; !slices.Equal(got, want) {
		t.Errorf("the form lists %q, want %q", got, want)
	}
	if first, again := o.pages[1].Dict["Contents"], o.pages[2].Dict["Contents"]; first != again {
		t.Errorf("the copies draw %v and %v, want one content stream", first, again)
	}
}

// assertKeysDescribeTheirPages checks that every page of o names a
// /StructParents key of its own, and that each element its /ParentTree entry
// lists names that page.
func (o *outputTree) assertKeysDescribeTheirPages(t *testing.T) {
	t.Helper()
	entries := make(map[int]ast.PdfObject)
	if err := document.WalkNumberTree(o.ctx, o.doc, o.root["ParentTree"], func(int) {}, func(key int, value ast.PdfObject) {
		entries[key] = value
	}); err != nil {
		t.Fatalf("walking the /ParentTree: %v", err)
	}
	seen := make(map[int]int)
	for i, key := range o.structParents() {
		if key < 0 {
			continue
		}
		if page, taken := seen[key]; taken {
			t.Errorf("pages %d and %d both name /StructParents %d", page+1, i+1, key)
		}
		seen[key] = i
		elems, _ := document.ResolveArray(o.ctx, o.doc, entries[key])
		if len(elems) == 0 {
			t.Errorf("page %d names /StructParents %d, which the /ParentTree does not list", i+1, key)
		}
		for _, elem := range elems {
			if pg, _ := o.dict(elem)["Pg"].(ast.PdfIndirectRef); pg != o.pages[i].Ref {
				t.Errorf("page %d's key %d lists an element on %v", i+1, key, o.dict(elem)["Pg"])
			}
		}
	}
}

// TestSplit_RepeatedPageHasElementsOfItsOwn pins that a page named again
// carries elements of its own, as the pages of a document merged with itself
// do: a structure element names the one page its content is on (ISO 32000-1,
// 14.7.2, Table 323), and a /StructParents key one /ParentTree entry
// (14.7.4.4), so the copies cannot share either.
func TestSplit_RepeatedPageHasElementsOfItsOwn(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	src := openDeclaration(t, taggedThreePagePDF("en-US"), false)
	defer src.Close()

	var out bytes.Buffer
	if err := Split(ctx, src, []int{2, 1, 2}, &out); err != nil {
		t.Fatalf("Split: %v", err)
	}
	o := readOutputTree(t, out.Bytes())
	if got, want := o.outline(), "Document[Sect[Para#p1 Para#p2] Link Document[Sect[Para#p2_2] Link]]"; got != want {
		t.Errorf("outline %q, want %q", got, want)
	}
	if got, want := o.ids(t), []string{"p1", "p2", "p2_2"}; !slices.Equal(got, want) {
		t.Errorf("identifiers %q, want %q", got, want)
	}
	o.assertPagesOwnTree(t)
	o.assertKeysDescribeTheirPages(t)
	o.assertAnnotationsHaveOnePage(t)
	if got, want := pageTexts(t, openBytes(t, out.Bytes())), []string{"Bravo\nlink", "Alpha", "Bravo\nlink"}; !slices.Equal(got, want) {
		t.Errorf("page texts %q, want %q", got, want)
	}
}

// TestInsertPages_RepeatedPageHasElementsOfItsOwn pins the structure of a
// source page inserted twice into a tagged document: each copy's elements
// join where the pages go, the second after the first.
func TestInsertPages_RepeatedPageHasElementsOfItsOwn(t *testing.T) {
	t.Parallel()
	out := writeLabelled(t, taggedThreePagePDF("en-US"), func(w *Writer) error {
		return w.InsertPages(1, openBytes(t, taggedThreePagePDF("en-US")), 2, 2)
	})
	o := readOutputTree(t, out)
	want := "Document[Sect[Para#p1 Para#p2] Document[Sect[Para#p2_2] Link] Document[Sect[Para#p2_3] Link] Link Para#p3]"
	if got := o.outline(); got != want {
		t.Errorf("outline %q, want %q", got, want)
	}
	o.assertPagesOwnTree(t)
	o.assertKeysDescribeTheirPages(t)
	o.assertAnnotationsHaveOnePage(t)
}

// keyedFormPDF builds a tagged page that draws its word through a form
// XObject whose /StructParents keys the marked content into the /ParentTree.
func keyedFormPDF() []byte {
	const form = "/P <</MCID 0>> BDC BT /F1 12 Tf 0 0 Td (Echo) Tj ET EMC"
	const page = "q 1 0 0 1 50 700 cm /Fm0 Do Q"
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 6 0 R /MarkInfo << /Marked true >> /Lang (en) >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R " +
			"/Resources << /XObject << /Fm0 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(page), page),
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 200 20] /StructParents 0 "+
			"/Resources << /Font << /F1 9 0 R >> >> /Length %d >>\nstream\n%s\nendstream", len(form), form),
		"<< /Type /StructTreeRoot /K [7 0 R] /ParentTree << /Nums [0 [8 0 R]] >> /ParentTreeNextKey 1 >>",
		"<< /Type /StructElem /S /Document /P 6 0 R /K [8 0 R] >>",
		"<< /Type /StructElem /S /P /P 7 0 R /Pg 3 0 R /K << /Type /MCR /Stm 5 0 R /MCID 0 >> >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
	})
}

// TestSplit_RepeatedPageCopiesAKeyedForm pins that a form XObject whose
// marked content a /StructParents key indexes is not shared between the
// copies of a page: the key names one /ParentTree entry (ISO 32000-1,
// 14.7.4.4), whose element names the page the form is drawn on. Each copy
// draws a form of its own, with its own key, while the font the forms use
// stays shared.
func TestSplit_RepeatedPageCopiesAKeyedForm(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	src := openBytes(t, keyedFormPDF())
	defer src.Close()

	var out bytes.Buffer
	if err := Split(ctx, src, []int{1, 1}, &out); err != nil {
		t.Fatalf("Split: %v", err)
	}
	if got, want := pageTexts(t, openBytes(t, out.Bytes())), []string{"Echo", "Echo"}; !slices.Equal(got, want) {
		t.Fatalf("page texts %q, want %q", got, want)
	}
	o := readOutputTree(t, out.Bytes())
	forms := make([]ast.PdfIndirectRef, 2)
	keys := make([]ast.PdfObject, 2)
	fonts := make([]ast.PdfObject, 2)
	for i := range forms {
		forms[i], _ = o.dict(o.dict(o.pages[i].Dict["Resources"])["XObject"])["Fm0"].(ast.PdfIndirectRef)
		stream, err := document.ResolveStream(ctx, o.doc, forms[i])
		if err != nil {
			t.Fatalf("resolving the form of page %d: %v", i+1, err)
		}
		keys[i] = stream.Dict["StructParents"]
		fonts[i] = o.dict(o.dict(stream.Dict["Resources"])["Font"])["F1"]
	}
	if forms[0] == forms[1] || keys[0] == keys[1] {
		t.Errorf("the copies draw forms %v and %v keyed %v and %v, want two keyed apart", forms[0], forms[1], keys[0], keys[1])
	}
	if fonts[0] != fonts[1] {
		t.Errorf("the forms use fonts %v and %v, want one", fonts[0], fonts[1])
	}
	for i, form := range forms {
		key, _ := keys[i].(ast.PdfInt)
		var elems ast.PdfArray
		if err := document.WalkNumberTree(ctx, o.doc, o.root["ParentTree"], func(int) {}, func(k int, value ast.PdfObject) {
			if k == int(key) {
				elems, _ = document.ResolveArray(ctx, o.doc, value)
			}
		}); err != nil {
			t.Fatalf("walking the /ParentTree: %v", err)
		}
		if len(elems) != 1 {
			t.Fatalf("form %v's key %d lists %v", form, key, elems)
		}
		elem := o.dict(elems[0])
		mcr := o.dict(elem["K"])
		if elem["Pg"] != ast.PdfObject(o.pages[i].Ref) || mcr["Stm"] != ast.PdfObject(form) {
			t.Errorf("page %d's element names page %v and form %v, want %v and %v", i+1, elem["Pg"], mcr["Stm"], o.pages[i].Ref, form)
		}
	}
}
