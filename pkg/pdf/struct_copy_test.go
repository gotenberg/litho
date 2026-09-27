package pdf

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/models"
)

// taggedThreePagePDF builds a tagged document of three pages, each drawing one
// word as marked content: Alpha, Bravo and Charlie. Its Document element holds
// a page-less Sect grouping the paragraphs of pages 1 and 2, a Link element on
// page 2 enclosing a link annotation, and the paragraph of page 3. The
// paragraphs are of type Para, which the /RoleMap maps to P, carry the
// identifiers p1 to p3 the /IDTree lists, and the one on page 2 names the Bold
// class the /ClassMap defines. lang is the catalog's /Lang.
//
// edits are pairs of strings: each replaces the first occurrence of its first
// string, in an object or a content stream, with its second, before the file
// is laid out.
func taggedThreePagePDF(lang string, edits ...string) []byte {
	word := func(w string) string {
		return fmt.Sprintf("/P <</MCID 0>> BDC BT /F1 12 Tf 50 700 Td (%s) Tj ET EMC\n", w)
	}
	// Content streams are held apart, keyed by their object number, and
	// wrapped once the edits are in, so each /Length stays exact.
	contents := map[int]string{
		6:  word("Alpha"),
		7:  word("Bravo") + "/Link <</MCID 1>> BDC BT /F1 12 Tf 50 600 Td (link) Tj ET EMC\n",
		20: word("Charlie"),
	}
	objs := []string{
		// 1: catalog.
		fmt.Sprintf("<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 9 0 R /MarkInfo << /Marked true >> "+
			"/Lang (%s) /ViewerPreferences << /DisplayDocTitle true >> >>", lang),
		// 2: pages.
		"<< /Type /Pages /Kids [3 0 R 4 0 R 5 0 R] /Count 3 >>",
		// 3-5: pages.
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 6 0 R " +
			"/Resources << /Font << /F1 8 0 R >> >> /StructParents 0 /Tabs /S >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 7 0 R " +
			"/Resources << /Font << /F1 8 0 R >> >> /StructParents 1 /Annots [19 0 R] /Tabs /S >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 20 0 R " +
			"/Resources << /Font << /F1 8 0 R >> >> /StructParents 2 /Tabs /S >>",
		// 6-7: content of pages 1 and 2.
		"",
		"",
		// 8: font.
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
		// 9: structure tree root.
		"<< /Type /StructTreeRoot /K [10 0 R] /ParentTree 17 0 R /ParentTreeNextKey 4 /IDTree 18 0 R " +
			"/RoleMap << /Para /P >> /ClassMap << /Bold << /O /Layout /TextAlign /Center >> >> >>",
		// 10: Document.
		"<< /Type /StructElem /S /Document /P 9 0 R /K [11 0 R 14 0 R 15 0 R] >>",
		// 11: a Sect naming no page.
		"<< /Type /StructElem /S /Sect /P 10 0 R /K [12 0 R 13 0 R] >>",
		// 12-13: the paragraphs of pages 1 and 2.
		"<< /Type /StructElem /S /Para /P 11 0 R /Pg 3 0 R /ID (p1) /K 0 >>",
		"<< /Type /StructElem /S /Para /P 11 0 R /Pg 4 0 R /ID (p2) /C /Bold /K 0 >>",
		// 14: the Link of page 2.
		"<< /Type /StructElem /S /Link /P 10 0 R /Pg 4 0 R /K [1 << /Type /OBJR /Obj 19 0 R >>] /Alt (link) >>",
		// 15: the paragraph of page 3.
		"<< /Type /StructElem /S /Para /P 10 0 R /Pg 5 0 R /ID (p3) /K 0 >>",
		// 16: unused.
		"<< >>",
		// 17: parent tree.
		"<< /Nums [0 [12 0 R] 1 [13 0 R 14 0 R] 2 [15 0 R] 3 14 0 R] >>",
		// 18: identifiers.
		"<< /Names [(p1) 12 0 R (p2) 13 0 R (p3) 15 0 R] >>",
		// 19: the link annotation of page 2.
		"<< /Type /Annot /Subtype /Link /Rect [50 590 150 620] /Border [0 0 0] /P 4 0 R " +
			"/Contents (link) /StructParent 3 /F 4 /A << /S /URI /URI (https://example.com/) >> >>",
		// 20: content of page 3.
		"",
	}
	for i := 0; i+1 < len(edits); i += 2 {
		old, replacement := edits[i], edits[i+1]
		applied := false
		for j := range objs {
			if strings.Contains(objs[j], old) {
				objs[j], applied = strings.Replace(objs[j], old, replacement, 1), true
				break
			}
		}
		for _, num := range []int{6, 7, 20} {
			if !applied && strings.Contains(contents[num], old) {
				contents[num], applied = strings.Replace(contents[num], old, replacement, 1), true
			}
		}
		if !applied {
			panic("taggedThreePagePDF: no object holds " + old)
		}
	}
	for num, content := range contents {
		objs[num-1] = fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content)
	}
	return buildPDF(objs)
}

// untaggedPDF builds an untagged one-page document whose page states a stray
// /StructParents key, which no structure tree indexes.
func untaggedPDF() []byte {
	const content = "BT /F1 12 Tf 50 700 Td (Delta) Tj ET\n"
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R " +
			"/Resources << /Font << /F1 5 0 R >> >> /StructParents 0 >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
	})
}

// TestSplit_CarriesTheStructureOfTheKeptPages pins that a split keeps the
// elements describing the pages it keeps, their ancestors, and the root
// entries naming them, and no element of a page it leaves out.
//
// A structure element names the page its content sits on (ISO 32000-1, 14.7.2,
// Table 323), so the output's tree has to hold the elements of its pages and
// only those: one describing another page would carry that page along.
func TestSplit_CarriesTheStructureOfTheKeptPages(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	in := taggedThreePagePDF("en-US")
	src := openDeclaration(t, in, false)
	defer src.Close()

	var out bytes.Buffer
	if err := Split(ctx, src, []int{2}, &out); err != nil {
		t.Fatalf("Split: %v", err)
	}
	tree := readOutputTree(t, out.Bytes())
	if tree.root == nil {
		t.Fatal("the split output has no structure tree")
	}
	if got, want := tree.outline(), "Document[Sect[Para#p2] Link]"; got != want {
		t.Errorf("tree = %q, want %q", got, want)
	}
	if got, want := tree.ids(t), []string{"p2"}; !slices.Equal(got, want) {
		t.Errorf("/IDTree = %v, want %v", got, want)
	}
	if got, want := tree.parentTreeKeys(t), []int{1, 3}; !slices.Equal(got, want) {
		t.Errorf("/ParentTree keys = %v, want %v", got, want)
	}
	tree.assertPagesOwnTree(t)
	for _, word := range []string{"Alpha", "Charlie"} {
		if bytes.Contains(out.Bytes(), []byte(word)) {
			t.Errorf("the output holds %q, drawn on a page the split left out", word)
		}
	}
	for _, profile := range []models.PDFUAProfile{models.PDFUA1, models.PDFUA2} {
		if extra := ruleSubset(uaRuleIDs(ctx, t, out.Bytes(), profile), uaRuleIDs(ctx, t, in, profile)); len(extra) > 0 {
			t.Errorf("%s: the split fails %v, which the source passes", profile, extra)
		}
	}
}

// TestSplit_DropsTheReferenceToALinkItLeftOut pins that a split which takes a
// link to a page it leaves out off the page it keeps also takes its object
// reference out of the Link element (ISO 32000-1, 14.7.4.3).
//
// The split drops the link from /Annots, since its destination is gone (see
// mutations.splitDestOverrides). Its object reference still named it, and
// copying the reference copied the link, and through its destination the page
// the split left out.
func TestSplit_DropsTheReferenceToALinkItLeftOut(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	in := taggedThreePagePDF("en-US", "/A << /S /URI /URI (https://example.com/) >>", "/Dest [3 0 R /Fit]")
	src := openDeclaration(t, in, false)
	defer src.Close()

	var out bytes.Buffer
	if err := Split(ctx, src, []int{2}, &out); err != nil {
		t.Fatalf("Split: %v", err)
	}
	if bytes.Contains(out.Bytes(), []byte("Alpha")) {
		t.Error("the output holds the content of page 1, which the split left out")
	}
	tree := readOutputTree(t, out.Bytes())
	if got, want := tree.outline(), "Document[Sect[Para#p2] Link]"; got != want {
		t.Errorf("tree = %q, want %q", got, want)
	}
	for _, elem := range tree.elements() {
		for _, kid := range tree.kids(elem["K"]) {
			if d := tree.dict(kid); d != nil && d["Type"] == ast.PdfName("OBJR") {
				t.Errorf("element %v keeps an object reference to %v", elem["S"], d["Obj"])
			}
		}
	}
}

// TestSplit_DropsAnElementItsPagesLeftEmpty pins that a split drops a grouping
// element whose content all sat on pages it left out, such as a table row
// naming no page whose cells sit on another page. The row describes nothing
// the output holds, and left behind it would make the table irregular. An
// empty cell of that row carries no content and keeps nothing alive: it goes
// with its row.
func TestSplit_DropsAnElementItsPagesLeftEmpty(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	// A Table in place of the Sect, holding the paragraphs of pages 1 and 2
	// under two rows written directly into its /K, the first with an empty
	// cell.
	in := taggedThreePagePDF("en-US", "/S /Sect /P 10 0 R /K [12 0 R 13 0 R]",
		"/S /Table /P 10 0 R /K [<< /Type /StructElem /S /TR /P 11 0 R /K [12 0 R << /Type /StructElem /S /TD >>] >> "+
			"<< /Type /StructElem /S /TR /P 11 0 R /K [13 0 R] >>]")
	src := openDeclaration(t, in, false)
	defer src.Close()

	var out bytes.Buffer
	if err := Split(ctx, src, []int{2}, &out); err != nil {
		t.Fatalf("Split: %v", err)
	}
	tree := readOutputTree(t, out.Bytes())
	if got, want := tree.outline(), "Document[Table[TR[Para#p2]] Link]"; got != want {
		t.Errorf("tree = %q, want %q", got, want)
	}
}

// TestSplit_ADocumentElementStaysWhenNothingElseDoes pins that a split of a
// page holding no tagged content keeps the root's Document element, which ISO
// 14289-2, 8.2.5.2 requires as the root's only child and which describes the
// document rather than a page.
func TestSplit_ADocumentElementStaysWhenNothingElseDoes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	// Page 3's content is an artifact, and no element describes it.
	in := taggedThreePagePDF("en-US",
		"/K [11 0 R 14 0 R 15 0 R]", "/K [11 0 R 14 0 R]",
		"/P <</MCID 0>> BDC BT /F1 12 Tf 50 700 Td (Charlie)", "/Artifact BMC BT /F1 12 Tf 50 700 Td (Charlie)",
		"/StructParents 2 /Tabs /S", "/Tabs /S",
		" 2 [15 0 R]", "")
	src := openDeclaration(t, in, false)
	defer src.Close()

	var out bytes.Buffer
	if err := Split(ctx, src, []int{3}, &out); err != nil {
		t.Fatalf("Split: %v", err)
	}
	tree := readOutputTree(t, out.Bytes())
	if got, want := tree.outline(), "Document"; got != want {
		t.Errorf("tree = %q, want %q", got, want)
	}
	if extra := ruleSubset(uaRuleIDs(ctx, t, out.Bytes(), models.PDFUA2), uaRuleIDs(ctx, t, in, models.PDFUA2)); len(extra) > 0 {
		t.Errorf("the split fails %v, which the source passes", extra)
	}
}

// TestSplit_CarriesATreeWhoseParentTreeNamesNoObject pins that a /ParentTree
// naming an object the file does not define reads as the absent entry it is
// (ISO 32000-1, 7.3.10), so the split carries the elements with a tree as
// empty as the source's rather than dropping the structure.
func TestSplit_CarriesATreeWhoseParentTreeNamesNoObject(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	src := openDeclaration(t, taggedThreePagePDF("en-US", "/ParentTree 17 0 R", "/ParentTree 99 0 R"), false)
	defer src.Close()

	var out bytes.Buffer
	if err := Split(ctx, src, []int{2}, &out); err != nil {
		t.Fatalf("Split: %v", err)
	}
	tree := readOutputTree(t, out.Bytes())
	if got, want := tree.outline(), "Document[Sect[Para#p2] Link]"; got != want {
		t.Errorf("tree = %q, want %q", got, want)
	}
	if keys := tree.parentTreeKeys(t); len(keys) != 0 {
		t.Errorf("/ParentTree keys = %v, want none, as the source's", keys)
	}
}

// TestSplit_TagsTheDrawnSignature pins that a split of a tagged page drawing a
// signed signature carries the page's structure and tags the drawing under the
// Form element that enclosed the widget, as a full rewrite does (see
// TestWriter_FullRewriteTagsTheDrawnSignature), adding no PDF/UA violation.
func TestSplit_TagsTheDrawnSignature(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	in := taggedSignedPDF()
	src := openDeclaration(t, in, false)
	defer src.Close()

	var out bytes.Buffer
	if err := Split(ctx, src, []int{1}, &out); err != nil {
		t.Fatalf("Split: %v", err)
	}
	if content := pageContent(ctx, t, out.Bytes(), 1); !strings.Contains(content, "/Form <</MCID 1>> BDC") {
		t.Errorf("the drawn signature is not tagged as MCID 1 of a Form element: %q", content)
	}
	for _, profile := range []models.PDFUAProfile{models.PDFUA1, models.PDFUA2} {
		if extra := ruleSubset(uaRuleIDs(ctx, t, out.Bytes(), profile), uaRuleIDs(ctx, t, in, profile)); len(extra) > 0 {
			t.Errorf("%s: the split fails %v, which the source passes", profile, extra)
		}
	}
}

// TestWriter_MovedPageKeepsItsStructure pins that a page moved within its
// document, inserted as a copy and then deleted from where it was, keeps its
// elements, and the document the rule set it had.
func TestWriter_MovedPageKeepsItsStructure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	in := taggedThreePagePDF("en-US")
	r := openDeclaration(t, in, false)
	defer r.Close()
	copySource := openDeclaration(t, in, false)
	defer copySource.Close()

	w := NewWriter(r)
	defer w.Close()
	if err := w.InsertPages(3, copySource, 1); err != nil {
		t.Fatalf("InsertPages: %v", err)
	}
	if err := w.DeletePages(1); err != nil {
		t.Fatalf("DeletePages: %v", err)
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	tree := readOutputTree(t, out.Bytes())
	if got, want := tree.outline(), "Document[Sect[Para#p2] Link Para#p3 Document[Sect[Para#p1_2]]]"; got != want {
		t.Errorf("tree = %q, want %q", got, want)
	}
	tree.assertPagesOwnTree(t)
	for _, profile := range []models.PDFUAProfile{models.PDFUA1, models.PDFUA2} {
		before, after := uaRuleIDs(ctx, t, in, profile), uaRuleIDs(ctx, t, out.Bytes(), profile)
		if !slices.Equal(before, after) {
			t.Errorf("%s: the output fails %v, the source %v", profile, after, before)
		}
	}
}

// TestSplitAndMerge_CarryTheNamespaces pins that the PDF 2.0 namespaces of the
// copied elements travel with them (ISO 32000-2, 14.7.4.2): the root's
// /Namespaces lists the namespace each copied element names, one per source.
// The associated files of the root (Table 355) travel too.
func TestSplitAndMerge_CarryTheNamespaces(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	in := taggedThreePagePDF("en-US",
		"/Type /StructTreeRoot ", "/Type /StructTreeRoot /Namespaces [16 0 R] "+
			"/AF [<< /Type /Filespec /F (notes.txt) /UF (notes.txt) /AFRelationship /Supplement >>] ",
		"/S /Document /P 9 0 R", "/S /Document /NS 16 0 R /P 9 0 R",
		"<< >>", "<< /Type /Namespace /NS (http://iso.org/pdf2/ssn) >>")
	src := openDeclaration(t, in, false)
	defer src.Close()

	check := func(t *testing.T, data []byte, want int) {
		t.Helper()
		tree := readOutputTree(t, data)
		if files := tree.kids(tree.root["AF"]); len(files) != want {
			t.Errorf("the root's /AF holds %d files, want %d", len(files), want)
		}
		namespaces := tree.kids(tree.root["Namespaces"])
		if len(namespaces) != want {
			t.Fatalf("/Namespaces holds %d entries, want %d", len(namespaces), want)
		}
		for _, elem := range tree.elements() {
			ns, named := elem["NS"].(ast.PdfIndirectRef)
			if !named {
				continue
			}
			if !slices.Contains(namespaces, ast.PdfObject(ns)) {
				t.Errorf("element %v names namespace %v, which /Namespaces does not list", elem["S"], ns)
			}
			if uri, _ := tree.dict(ns)["NS"].(ast.PdfString); uri != "http://iso.org/pdf2/ssn" {
				t.Errorf("element %v names a namespace of URI %q", elem["S"], uri)
			}
		}
	}

	var split bytes.Buffer
	if err := Split(ctx, src, []int{2}, &split); err != nil {
		t.Fatalf("Split: %v", err)
	}
	check(t, split.Bytes(), 1)

	var merged bytes.Buffer
	if err := Merge(ctx, []*Reader{src, src}, &merged); err != nil {
		t.Fatalf("Merge: %v", err)
	}
	check(t, merged.Bytes(), 2)
}

// TestMerge_JoinsTheSourcesTrees pins that a merge holds one structure tree
// over every source: the second source's Document element goes into the
// first's, so the root keeps the single Document ISO 14289-2, 8.2.5.2 asks
// for, its keys are renumbered past the first's, and its identifiers renamed
// where the first uses them.
func TestMerge_JoinsTheSourcesTrees(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	in := taggedThreePagePDF("en-US")
	src := openDeclaration(t, in, false)
	defer src.Close()

	var out bytes.Buffer
	if err := Merge(ctx, []*Reader{src, src}, &out); err != nil {
		t.Fatalf("Merge: %v", err)
	}
	tree := readOutputTree(t, out.Bytes())
	if tree.root == nil {
		t.Fatal("the merge output has no structure tree")
	}
	const one = "Sect[Para#p1 Para#p2] Link Para#p3"
	const two = "Sect[Para#p1_2 Para#p2_2] Link Para#p3_2"
	if got, want := tree.outline(), "Document["+one+" Document["+two+"]]"; got != want {
		t.Errorf("tree = %q, want %q", got, want)
	}
	if got, want := tree.structParents(), []int{0, 1, 2, 4, 5, 6}; !slices.Equal(got, want) {
		t.Errorf("pages' /StructParents = %v, want %v", got, want)
	}
	if got, want := tree.parentTreeKeys(t), []int{0, 1, 2, 3, 4, 5, 6, 7}; !slices.Equal(got, want) {
		t.Errorf("/ParentTree keys = %v, want %v", got, want)
	}
	if got, want := tree.ids(t), []string{"p1", "p1_2", "p2", "p2_2", "p3", "p3_2"}; !slices.Equal(got, want) {
		t.Errorf("/IDTree = %v, want %v", got, want)
	}
	if next, _ := tree.root["ParentTreeNextKey"].(ast.PdfInt); next != 8 {
		t.Errorf("/ParentTreeNextKey = %d, want 8", next)
	}
	tree.assertPagesOwnTree(t)
	for _, profile := range []models.PDFUAProfile{models.PDFUA1, models.PDFUA2} {
		before, after := uaRuleIDs(ctx, t, in, profile), uaRuleIDs(ctx, t, out.Bytes(), profile)
		if !slices.Equal(before, after) {
			t.Errorf("%s: the merge fails %v, the source %v", profile, after, before)
		}
	}
}

// TestMerge_RenamesAConflictingRoleAndKeepsEachLanguage pins that a merge of
// sources mapping one structure type to different standard types keeps both
// meanings, and that a source whose catalog names another language keeps it.
//
// One /RoleMap serves every element of the default namespace (ISO 32000-1,
// 14.7.3), so the second source's Para becomes Para_2, mapped to H1, while the
// first's stays mapped to P. The second source also maps Document to itself,
// which defines nothing: renaming it would have renamed the value with the key
// into Document_2 mapped to Document_2, a loop ISO 14289-2, 8.2.4 test 2
// reports. An element takes the language of its parent and then the catalog's
// (ISO 32000-1, 14.9.2.1), and the output's catalog is the first source's, so
// the second source's Document states its own.
func TestMerge_RenamesAConflictingRoleAndKeepsEachLanguage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	first := openDeclaration(t, taggedThreePagePDF("en-US"), false)
	defer first.Close()
	second := openDeclaration(t, taggedThreePagePDF("fr-FR", "/RoleMap << /Para /P >>", "/RoleMap << /Para /H1 /Document /Document >>"), false)
	defer second.Close()

	var out bytes.Buffer
	if err := Merge(ctx, []*Reader{first, second}, &out); err != nil {
		t.Fatalf("Merge: %v", err)
	}
	tree := readOutputTree(t, out.Bytes())
	roleMap := tree.dict(tree.root["RoleMap"])
	if got := roleMap["Para"]; got != ast.PdfName("P") {
		t.Errorf("/RoleMap /Para = %v, want /P", got)
	}
	if got := roleMap["Para_2"]; got != ast.PdfName("H1") {
		t.Errorf("/RoleMap /Para_2 = %v, want /H1", got)
	}
	if got, renamed := roleMap["Document_2"]; renamed {
		t.Errorf("/RoleMap renames the identity mapping of /Document, into /Document_2 -> %v", got)
	}
	const one = "Sect[Para#p1 Para#p2] Link Para#p3"
	const two = "Sect[Para_2#p1_2 Para_2#p2_2] Link Para_2#p3_2"
	if got, want := tree.outline(), "Document["+one+" Document["+two+"]]"; got != want {
		t.Errorf("tree = %q, want %q", got, want)
	}
	documents := 0
	for _, elem := range tree.elements() {
		if elem["S"] != ast.PdfName("Document") {
			continue
		}
		documents++
		lang, has := elem["Lang"]
		switch documents {
		case 1:
			if has {
				t.Errorf("the first source's Document states /Lang %v, which the catalog already names", lang)
			}
		case 2:
			if lang != ast.PdfString("fr-FR") {
				t.Errorf("the second source's Document states /Lang %v, want (fr-FR)", lang)
			}
		}
	}
}

// TestMerge_KeepsAnUnmappedTypeUnmapped pins that a type one source leaves
// unmapped keeps that meaning when an earlier source maps it: the first
// source's /RoleMap maps Group to Art, and the second's Group, which it maps
// to nothing (ISO 32000-1, 14.7.3), would otherwise read as an Art the second
// source never wrote. It becomes Group_2, which the /RoleMap leaves unmapped
// as the second source did.
func TestMerge_KeepsAnUnmappedTypeUnmapped(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	first := openDeclaration(t, taggedThreePagePDF("en-US", "/RoleMap << /Para /P >>", "/RoleMap << /Para /P /Group /Art >>"), false)
	defer first.Close()
	second := openDeclaration(t, taggedThreePagePDF("en-US", "/S /Sect /P 10 0 R", "/S /Group /P 10 0 R"), false)
	defer second.Close()

	var out bytes.Buffer
	if err := Merge(ctx, []*Reader{first, second}, &out); err != nil {
		t.Fatalf("Merge: %v", err)
	}
	tree := readOutputTree(t, out.Bytes())
	const one = "Sect[Para#p1 Para#p2] Link Para#p3"
	const two = "Group_2[Para#p1_2 Para#p2_2] Link Para#p3_2"
	if got, want := tree.outline(), "Document["+one+" Document["+two+"]]"; got != want {
		t.Errorf("tree = %q, want %q", got, want)
	}
	roleMap := tree.dict(tree.root["RoleMap"])
	if got := roleMap["Group"]; got != ast.PdfName("Art") {
		t.Errorf("/RoleMap /Group = %v, want /Art, the first source's", got)
	}
	if got, mapped := roleMap["Group_2"]; mapped {
		t.Errorf("/RoleMap maps /Group_2 to %v, which the second source left unmapped", got)
	}
}

// TestMerge_RenamesClassesAndTheIdentifiersCellsName pins that a merge keeps
// a class two sources define differently apart, and renames a clashing
// identifier wherever a table cell names it.
//
// The second source's Bold class centers where the first's aligns to the end,
// so its paragraph names Bold_2, which the /ClassMap defines as the second
// source did (ISO 32000-1, 14.7.5.2). Its paragraph p2 names p1 in /Headers
// (Table 349), and p1 is the first source's too, so both become p1_2.
func TestMerge_RenamesClassesAndTheIdentifiersCellsName(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	headers := []string{"/ID (p2) /C /Bold", "/ID (p2) /C /Bold /A << /O /Table /Headers [(p1)] >>"}
	first := openDeclaration(t, taggedThreePagePDF("en-US", append(headers, "/TextAlign /Center", "/TextAlign /End")...), false)
	defer first.Close()
	second := openDeclaration(t, taggedThreePagePDF("en-US", headers...), false)
	defer second.Close()

	var out bytes.Buffer
	if err := Merge(ctx, []*Reader{first, second}, &out); err != nil {
		t.Fatalf("Merge: %v", err)
	}
	tree := readOutputTree(t, out.Bytes())
	classMap := tree.dict(tree.root["ClassMap"])
	if align := tree.dict(classMap["Bold"])["TextAlign"]; align != ast.PdfName("End") {
		t.Errorf("/ClassMap /Bold aligns %v, want /End, the first source's", align)
	}
	if align := tree.dict(classMap["Bold_2"])["TextAlign"]; align != ast.PdfName("Center") {
		t.Errorf("/ClassMap /Bold_2 aligns %v, want /Center, the second source's", align)
	}
	var second2 ast.PdfDict
	for _, elem := range tree.elements() {
		if elem["ID"] == ast.PdfString("p2_2") {
			second2 = elem
		}
	}
	if second2 == nil {
		t.Fatal("the second source's p2 is not in the tree as p2_2")
	}
	if got := second2["C"]; got != ast.PdfName("Bold_2") {
		t.Errorf("p2_2 names class %v, want /Bold_2", got)
	}
	headersOf := tree.kids(tree.dict(second2["A"])["Headers"])
	if len(headersOf) != 1 || headersOf[0] != ast.PdfString("p1_2") {
		t.Errorf("p2_2 names the headers %v, want [(p1_2)]", headersOf)
	}
}

// TestMerge_LeavesAnUntaggedSourceOutsideTheTree pins that a merge of a tagged
// and an untagged document carries the tagged one's tree, and that the
// untagged one's content keys no entry of it.
//
// The untagged page states a stray /StructParents 0. The output's /ParentTree
// files the tagged page 1 under key 0, so leaving the stray key would hand the
// untagged content to that page's elements (ISO 32000-1, 14.7.4.4).
func TestMerge_LeavesAnUntaggedSourceOutsideTheTree(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	untagged := openDeclaration(t, untaggedPDF(), false)
	defer untagged.Close()
	tagged := openDeclaration(t, taggedThreePagePDF("en-US"), false)
	defer tagged.Close()

	var out bytes.Buffer
	if err := Merge(ctx, []*Reader{untagged, tagged}, &out); err != nil {
		t.Fatalf("Merge: %v", err)
	}
	tree := readOutputTree(t, out.Bytes())
	if tree.root == nil {
		t.Fatal("the merge output has no structure tree")
	}
	if got, want := tree.outline(), "Document[Sect[Para#p1 Para#p2] Link Para#p3]"; got != want {
		t.Errorf("tree = %q, want %q", got, want)
	}
	if got, want := tree.structParents(), []int{-1, 0, 1, 2}; !slices.Equal(got, want) {
		t.Errorf("pages' /StructParents = %v, want %v", got, want)
	}
	// The output's catalog is the untagged source's, which names no language,
	// so the tagged source's Document states its own.
	for _, elem := range tree.elements() {
		if elem["S"] == ast.PdfName("Document") && elem["Lang"] != ast.PdfString("en-US") {
			t.Errorf("the tagged source's Document states /Lang %v, want (en-US)", elem["Lang"])
		}
	}
}

// TestInsertPages_JoinsTheTargetTreeWherePagesGo pins that inserted pages
// carry their elements into the target's tree, their source's Document
// element into the target's, as a merge joins a source (ISO 14289-2, 8.2.5.2
// keeps the root's single Document), and before the elements of the pages that
// follow the insertion point: the structure tree gives the logical reading
// order (ISO 32000-1, 14.8.2.3). Their keys follow the target's, which
// reserves 0 to 3.
func TestInsertPages_JoinsTheTargetTreeWherePagesGo(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		pos  int
		want string
	}{
		{"front", 0, "Document[Document[Para#p3_2] Sect[Para#p1 Para#p2] Link Para#p3]"},
		{"middle", 2, "Document[Sect[Para#p1 Para#p2] Link Document[Para#p3_2] Para#p3]"},
		{"end", 3, "Document[Sect[Para#p1 Para#p2] Link Para#p3 Document[Para#p3_2]]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in := taggedThreePagePDF("en-US")
			target := openDeclaration(t, in, false)
			defer target.Close()
			source := openDeclaration(t, in, false)
			defer source.Close()

			w := NewWriter(target)
			defer w.Close()
			if err := w.InsertPages(tc.pos, source, 3); err != nil {
				t.Fatalf("InsertPages: %v", err)
			}
			var out bytes.Buffer
			if err := w.WriteTo(ctx, &out); err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
			tree := readOutputTree(t, out.Bytes())
			if got := tree.outline(); got != tc.want {
				t.Errorf("tree = %q, want %q", got, tc.want)
			}
			keys := tree.structParents()
			if got := keys[tc.pos]; got != 6 {
				t.Errorf("the inserted page's /StructParents = %d, want its own 2 past the target's 4", got)
			}
			if got, want := tree.parentTreeKeys(t), []int{0, 1, 2, 3, 6}; !slices.Equal(got, want) {
				t.Errorf("/ParentTree keys = %v, want %v", got, want)
			}
			if next, _ := tree.root["ParentTreeNextKey"].(ast.PdfInt); next != 8 {
				t.Errorf("/ParentTreeNextKey = %d, want 8", next)
			}
			tree.assertPagesOwnTree(t)
			for _, profile := range []models.PDFUAProfile{models.PDFUA1, models.PDFUA2} {
				before, after := uaRuleIDs(ctx, t, in, profile), uaRuleIDs(ctx, t, out.Bytes(), profile)
				if !slices.Equal(before, after) {
					t.Errorf("%s: the output fails %v, the source %v", profile, after, before)
				}
			}
		})
	}
}

// TestInsertPages_MixedTagging pins what an insertion does when one side is
// untagged: a tagged source's elements form the tree of an untagged target,
// and an untagged source's content joins a tagged target outside its tree,
// keying none of its /ParentTree entries (ISO 32000-1, 14.7.4.4).
func TestInsertPages_MixedTagging(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("untagged target", func(t *testing.T) {
		t.Parallel()
		target := openDeclaration(t, untaggedPDF(), false)
		defer target.Close()
		source := openDeclaration(t, taggedThreePagePDF("en-US"), false)
		defer source.Close()
		w := NewWriter(target)
		defer w.Close()
		if err := w.InsertPages(1, source, 2); err != nil {
			t.Fatalf("InsertPages: %v", err)
		}
		var out bytes.Buffer
		if err := w.WriteTo(ctx, &out); err != nil {
			t.Fatalf("WriteTo: %v", err)
		}
		tree := readOutputTree(t, out.Bytes())
		if tree.root == nil {
			t.Fatal("the output has no structure tree")
		}
		if got, want := tree.outline(), "Document[Sect[Para#p2] Link]"; got != want {
			t.Errorf("tree = %q, want %q", got, want)
		}
		if got, want := tree.parentTreeKeys(t), []int{1, 3}; !slices.Equal(got, want) {
			t.Errorf("/ParentTree keys = %v, want %v", got, want)
		}
		tree.assertPagesOwnTree(t)
	})

	t.Run("untagged source", func(t *testing.T) {
		t.Parallel()
		target := openDeclaration(t, taggedThreePagePDF("en-US"), false)
		defer target.Close()
		source := openDeclaration(t, untaggedPDF(), false)
		defer source.Close()
		w := NewWriter(target)
		defer w.Close()
		if err := w.InsertPages(0, source); err != nil {
			t.Fatalf("InsertPages: %v", err)
		}
		var out bytes.Buffer
		if err := w.WriteTo(ctx, &out); err != nil {
			t.Fatalf("WriteTo: %v", err)
		}
		tree := readOutputTree(t, out.Bytes())
		if got, want := tree.structParents(), []int{-1, 0, 1, 2}; !slices.Equal(got, want) {
			t.Errorf("pages' /StructParents = %v, want %v", got, want)
		}
		if got, want := tree.outline(), "Document[Sect[Para#p1 Para#p2] Link Para#p3]"; got != want {
			t.Errorf("tree = %q, want %q", got, want)
		}
	})
}
