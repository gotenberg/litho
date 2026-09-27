package remediation

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// The page tree [reachTreePDF] builds, by object number.
const (
	reachRoot        = 2
	reachRootFont    = 3
	reachMisNode     = 4
	reachMisFont     = 5
	reachMisPage     = 6
	reachMisContent  = 7
	reachNullNode    = 8
	reachNullFont    = 9
	reachNullPage    = 10
	reachNullContent = 11
	// reachDirectNode lists its one page directly in /Kids.
	reachDirectNode    = 12
	reachDirectFont    = 13
	reachDirectContent = 14
	reachDangleNode    = 15
	reachDangleFont    = 16
	reachDanglePage    = 17
	reachDangleContent = 18
	reachRootPage      = 19
	// reachDangleTarget is the object the /Resources of reachDanglePage
	// name, which the file does not define.
	reachDangleTarget = 9999
	// reachExtra is the first object number of reachTree.extra.
	reachExtra = 21
)

// reachNode is what a test plants in one /Pages node of [reachTreePDF]:
// entries of its /Resources besides its own font, and the content of the page
// it lists.
type reachNode struct {
	// fonts are entries of the node's /Font besides its own font, and res
	// entries of its /Resources besides /Font.
	fonts, res string
	content    string
}

// reachTree holds what a test plants in the page tree [reachTreePDF] builds.
type reachTree struct {
	root, mis, null, direct, dangle reachNode
	// directPage holds entries of the page written directly in the /Kids of
	// reachDirectNode besides /Type, /Parent and /Contents.
	directPage string
	// rootPage lists reachRootPage, which inherits the root's /Resources, in
	// the root's /Kids, so a page draws with them.
	rootPage bool
	extra    []string
}

// reachTreePDF builds a page tree whose pages each resolve their /Resources
// through the tree in one of the ways ISO 32000-1, 7.7.3.4 and veraPDF read
// it, each under a /Pages node of its own lending it a font:
//
//   - reachMisPage states no /Resources and its /Parent names the root, while
//     reachMisNode lists it (litho resinherit_2b.pdf and parentreach_2b.pdf).
//     It inherits F5 from that node, not FR from the root.
//   - reachNullPage states /Resources null, which is absent (ISO 32000-1,
//     7.3.7), so it inherits F3 from reachNullNode.
//   - The page written directly in the /Kids of reachDirectNode, against ISO
//     32000-1, Table 29, inherits F4 from it unless it states /Resources.
//   - reachDanglePage states a reference to no object, a value of its own,
//     so it inherits nothing from its node's F6 and resolves no name.
//   - reachRootPage, listed when rootPage is set, inherits FR from the root.
//
// Each page draws text with its node's font unless the test plants another
// content.
func reachTreePDF(s reachTree) []byte {
	font := func(base string) string {
		return "<< /Type /Font /Subtype /Type1 /BaseFont /" + base + " /Encoding /WinAnsiEncoding >>"
	}
	node := func(fontName string, fontObj int, n reachNode, kids string) string {
		return fmt.Sprintf("<< /Type /Pages /Parent 2 0 R /Kids [%s] /Count 1 /Resources << /Font << /%s %d 0 R %s >> %s >> >>",
			kids, fontName, fontObj, n.fonts, n.res)
	}
	body := func(n reachNode, fontName string) string {
		if n.content != "" {
			return contentObj(n.content)
		}
		return contentObj("BT /" + fontName + " 12 Tf (A) Tj ET")
	}
	kids, count := "4 0 R 8 0 R 12 0 R 15 0 R", 4
	if s.rootPage {
		kids, count = kids+" 19 0 R", count+1
	}
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d /MediaBox [0 0 612 792] /Resources << /Font << /FR 3 0 R %s >> %s >> >>",
			kids, count, s.root.fonts, s.root.res),
		font("Courier"),
		node("F5", reachMisFont, s.mis, "6 0 R"),
		font("Helvetica-Oblique"),
		"<< /Type /Page /Parent 2 0 R /Contents 7 0 R >>",
		body(s.mis, "F5"),
		node("F3", reachNullFont, s.null, "10 0 R"),
		font("Helvetica-Bold"),
		"<< /Type /Page /Parent 8 0 R /Resources null /Contents 11 0 R >>",
		body(s.null, "F3"),
		node("F4", reachDirectFont, s.direct, "<< /Type /Page /Parent 12 0 R "+s.directPage+" /Contents 14 0 R >>"),
		font("Times-Roman"),
		body(s.direct, "F4"),
		node("F6", reachDangleFont, s.dangle, "17 0 R"),
		font("Times-Bold"),
		"<< /Type /Page /Parent 15 0 R /Resources 9999 0 R /Contents 18 0 R >>",
		body(s.dangle, "F6"),
		"<< /Type /Page /Parent 2 0 R /Contents 20 0 R >>",
		body(s.root, "FR"),
	}
	return buildStructPDF(append(objs, s.extra...)...)
}

// reachExtraObj returns the object number of the i-th entry of
// reachTree.extra.
func reachExtraObj(i int) int { return reachExtra + i }

// reachExtraRef renders a reference to the i-th entry of reachTree.extra.
func reachExtraRef(i int) string { return strconv.Itoa(reachExtraObj(i)) + " 0 R" }

// liveDirectPage returns the page written directly in the /Kids of
// reachDirectNode as the output will carry it.
func liveDirectPage(t *testing.T, alloc *writer.ObjAllocator, doc *document.Document) ast.PdfDict {
	t.Helper()

	ctx := context.Background()
	kids := liveArray(ctx, alloc, doc, resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: reachDirectNode})["Kids"])
	if len(kids) != 1 {
		t.Fatalf("the node lists %d kids, want 1: %v", len(kids), kids)
	}
	page, ok := kids[0].(ast.PdfDict)
	if !ok {
		t.Fatalf("the node's kid is %T, want the page written directly in /Kids", kids[0])
	}
	return page
}

// liveGStateCA returns the /CA the graphics state /GS0 in res carries in the
// output.
func liveGStateCA(t *testing.T, alloc *writer.ObjAllocator, doc *document.Document, res ast.PdfDict) float64 {
	t.Helper()

	ctx := context.Background()
	gs := liveDictValue(ctx, alloc, doc, liveDictValue(ctx, alloc, doc, res["ExtGState"])["GS0"])
	if gs == nil {
		t.Fatalf("/GS0 did not resolve in %v", res)
	}
	return document.Float(gs["CA"])
}

// liveShownStrings returns the strings the Tj operators of the content stream
// at objNum show in the output, in stream order.
func liveShownStrings(t *testing.T, alloc *writer.ObjAllocator, doc *document.Document, objNum int) []string {
	t.Helper()

	ctx := context.Background()
	data, err := liveContentBytes(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: objNum})
	if err != nil {
		t.Fatalf("reading the content stream %d: %v", objNum, err)
	}
	ops, err := content.ParseStreamData(ctx, data, content.ForRepair())
	if err != nil {
		t.Fatalf("parsing the content stream %d: %v", objNum, err)
	}
	var shown []string
	for _, op := range ops {
		if op.Operator != "Tj" || len(op.Operands) == 0 {
			continue
		}
		if str, ok := op.Operands[0].(ast.PdfString); ok {
			shown = append(shown, string(str))
		}
	}
	return shown
}

// assertPending fails unless the allocator queued a write at every object of
// objs, each a defect planted where a page draws with it.
func assertPending(t *testing.T, alloc *writer.ObjAllocator, what string, objs ...int) {
	t.Helper()

	for _, objNum := range objs {
		if _, ok := alloc.Pending()[objNum]; !ok {
			t.Errorf("%s at object %d, which a page draws with, was not repaired", what, objNum)
		}
	}
}

// assertNotPending fails when the allocator queued a write at any object of
// objs, which no repair may reach.
func assertNotPending(t *testing.T, alloc *writer.ObjAllocator, what string, objs ...int) {
	t.Helper()

	for _, objNum := range objs {
		if got, ok := alloc.Pending()[objNum]; ok {
			t.Errorf("%s at object %d was rewritten: %v", what, objNum, got)
		}
	}
}

// TestFixTransparency_RepairsTheResourcesThePageDrawsWith pins that the
// PDF/A-1 transparency repair writes an inline graphics state repair into the
// /Resources the page resolves its names against, the ones it inherits
// through the page tree included (ISO 32000-1, 7.7.3.4).
//
// The repair read the /Resources the parser merges from every ancestor, and
// wrote an inline repair through the page's own /Resources entry. A page
// stating none had nowhere to write it, so every inherited /CA 0.5 stayed and
// ISO 19005-1, 6.4 with it. A page stating /Resources null was given a copy
// of the merged dictionary, shadowing its node's for that page alone. A page
// stating a reference to no object had the merged dictionary written at that
// object number, defining /Resources for a page that resolves none. A page
// written directly in /Kids had no object number to write at.
func TestFixTransparency_RepairsTheResourcesThePageDrawsWith(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	const alpha = "/ExtGState << /GS0 << /Type /ExtGState /CA 0.5 >> >>"
	withGS := func(fontName string) reachNode {
		return reachNode{res: alpha, content: "q /GS0 gs Q BT /" + fontName + " 12 Tf (A) Tj ET"}
	}

	t.Run("inherited resources", func(t *testing.T) {
		t.Parallel()

		doc, alloc := newTestDoc(t, reachTreePDF(reachTree{
			root: withGS("FR"), mis: withGS("F5"), null: withGS("F3"), direct: withGS("F4"), dangle: withGS("F6"),
			rootPage: true,
		}))
		if _, err := fixTransparency(ctx, alloc, doc, Options{}, nil, ""); err != nil {
			t.Fatalf("fixTransparency: %v", err)
		}
		for _, tc := range []struct {
			name string
			node int
		}{
			{name: "a page under the root", node: reachRoot},
			{name: "a /Parent naming another node", node: reachMisNode},
			{name: "a null /Resources", node: reachNullNode},
			{name: "a page written directly in /Kids", node: reachDirectNode},
		} {
			if ca := liveGStateCA(t, alloc, doc, liveResourcesOf(t, alloc, doc, tc.node)); ca != 1 {
				t.Errorf("%s: the /GS0 its node lends it keeps /CA %v, want 1", tc.name, ca)
			}
		}
		if ca := liveGStateCA(t, alloc, doc, liveResourcesOf(t, alloc, doc, reachDangleNode)); ca != 0.5 {
			t.Errorf("the /GS0 of the node listing the page that resolves no name was rewritten: /CA %v", ca)
		}
		assertNotPending(t, alloc, "the page stating /Resources null, which would shadow its node's", reachNullPage)
		assertNotPending(t, alloc, "the object the dangling /Resources name, which the page does not define", reachDangleTarget)
	})

	t.Run("resources of a page written directly in /Kids", func(t *testing.T) {
		t.Parallel()

		doc, alloc := newTestDoc(t, reachTreePDF(reachTree{
			direct:     reachNode{content: "q /GS0 gs Q"},
			directPage: "/Resources << " + alpha + " >> /Group << /S /Transparency >>",
		}))
		if _, err := fixTransparency(ctx, alloc, doc, Options{}, nil, ""); err != nil {
			t.Fatalf("fixTransparency: %v", err)
		}
		page := liveDirectPage(t, alloc, doc)
		if ca := liveGStateCA(t, alloc, doc, liveDictValue(ctx, alloc, doc, page["Resources"])); ca != 1 {
			t.Errorf("the page's own /GS0 keeps /CA %v, want 1", ca)
		}
		if _, has := page["Group"]; has {
			t.Errorf("the page keeps its transparency group: %v", page)
		}
	})
}

// TestWalkContentStreams_WalksTheResourcesThePageDrawsWith pins that the
// content stream walk reaches the forms the /Resources a page resolves its
// names against name, and no other.
//
// A page stating no /Resources walked the ones the parser merges from every
// ancestor, so a page whose /Parent names the root, or whose /Resources are
// null, reached the root's forms, and a page stating a reference to no object
// reached its node's, none of which it can draw.
func TestWalkContentStreams_WalksTheResourcesThePageDrawsWith(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	form := formStreamNoResources("MARK")
	xobj := func(name string, i int) reachNode {
		return reachNode{res: "/XObject << /" + name + " " + reachExtraRef(i) + " >>", content: "/" + name + " Do"}
	}
	tree := reachTree{
		root: xobj("FmR", 0), mis: xobj("FmA", 1), null: xobj("FmB", 2), direct: xobj("FmC", 3), dangle: xobj("FmD", 4),
		extra: []string{form, form, form, form, form},
	}
	for _, rootPage := range []bool{false, true} {
		t.Run(fmt.Sprintf("root page listed %v", rootPage), func(t *testing.T) {
			t.Parallel()

			tree := tree
			tree.rootPage = rootPage
			doc, alloc := newTestDoc(t, reachTreePDF(tree))
			if _, err := walkContentStreams(ctx, alloc, doc, "test", func(_ context.Context, data []byte) ([]byte, func()) {
				if !bytes.Contains(data, []byte("MARK")) {
					return nil, nil
				}
				return bytes.ReplaceAll(data, []byte("MARK"), []byte("DONE")), func() {}
			}); err != nil {
				t.Fatalf("walkContentStreams: %v", err)
			}
			assertPending(t, alloc, "the form", reachExtraObj(1), reachExtraObj(2), reachExtraObj(3))
			assertNotPending(t, alloc, "the form of the node listing the page that resolves no name", reachExtraObj(4))
			if rootPage {
				assertPending(t, alloc, "the root's form", reachExtraObj(0))
			} else {
				assertNotPending(t, alloc, "the root's form, which no page draws with", reachExtraObj(0))
			}
		})
	}
}

// TestFixFontIssues_AttachesToUnicodeFromThePageDrawingTheFont pins that a
// generated /ToUnicode is built from the codes of the page that draws the
// font, through the /Resources it resolves its names against.
//
// The fonts repair read each page's fonts from the /Resources the parser
// merges from every ancestor, and marks every font it reaches processed. The
// page whose /Parent names the root came first and reached the root's font,
// which it does not draw, found no code to map and marked it done, so the page
// under the root that draws it was never given one.
func TestFixFontIssues_AttachesToUnicodeFromThePageDrawingTheFont(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, reachTreePDF(reachTree{rootPage: true}))
	if _, err := fixFontIssues(ctx, alloc, doc, Options{}, nil, ""); err != nil {
		t.Fatalf("fixFontIssues: %v", err)
	}
	for _, tc := range []struct {
		name  string
		font  int
		wants bool
	}{
		{name: "the root's font, drawn by the page under the root", font: reachRootFont, wants: true},
		{name: "the font of the node listing the misparented page", font: reachMisFont, wants: true},
		{name: "the font of the node listing the null page", font: reachNullFont, wants: true},
		{name: "the font of the node listing the direct page", font: reachDirectFont, wants: true},
		{name: "the font of the node listing the page that resolves no name", font: reachDangleFont},
	} {
		font := liveDictValue(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: tc.font})
		if _, has := font["ToUnicode"]; has != tc.wants {
			t.Errorf("%s: /ToUnicode present = %v, want %v: %v", tc.name, has, tc.wants, font)
		}
	}
}

// TestFixFontKeys_RepairsTheFontsThePageDrawsWith pins that font-keys
// repairs the fonts of the /Resources a page resolves its names against, and
// no other.
func TestFixFontKeys_RepairsTheFontsThePageDrawsWith(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	const untyped = "<< /Subtype /Type1 /BaseFont /Symbol >>"
	fonts := func(name string, i int) reachNode { return reachNode{fonts: "/" + name + " " + reachExtraRef(i)} }
	doc, alloc := newTestDoc(t, reachTreePDF(reachTree{
		root: fonts("KR", 0), mis: fonts("KA", 1), null: fonts("KB", 2), direct: fonts("KC", 3), dangle: fonts("KD", 4),
		extra: []string{untyped, untyped, untyped, untyped, untyped},
	}))
	if _, err := fixFontKeys(ctx, alloc, doc, Options{}, nil, ""); err != nil {
		t.Fatalf("fixFontKeys: %v", err)
	}
	assertPending(t, alloc, "the font missing /Type", reachExtraObj(1), reachExtraObj(2), reachExtraObj(3))
	assertNotPending(t, alloc, "the font missing /Type, which no page resolves,", reachExtraObj(0), reachExtraObj(4))
}

// TestFixImageIssues_RepairsTheImagesThePageDrawsWith pins that the image
// repair reaches the images of the /Resources a page resolves its names
// against, and no other.
func TestFixImageIssues_RepairsTheImagesThePageDrawsWith(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	const image = "<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray" +
		" /BitsPerComponent 8 /Interpolate true /Length 1 >>\nstream\n\x00\nendstream"
	xobj := func(name string, i int) reachNode {
		return reachNode{res: "/XObject << /" + name + " " + reachExtraRef(i) + " >>", content: "/" + name + " Do"}
	}
	doc, alloc := newTestDoc(t, reachTreePDF(reachTree{
		root: xobj("ImR", 0), mis: xobj("ImA", 1), null: xobj("ImB", 2), direct: xobj("ImC", 3), dangle: xobj("ImD", 4),
		extra: []string{image, image, image, image, image},
	}))
	if _, err := fixImageIssues(ctx, alloc, doc, Options{}, nil, ""); err != nil {
		t.Fatalf("fixImageIssues: %v", err)
	}
	assertPending(t, alloc, "the image asking for interpolation", reachExtraObj(1), reachExtraObj(2), reachExtraObj(3))
	assertNotPending(t, alloc, "the image asking for interpolation, which no page resolves,", reachExtraObj(0), reachExtraObj(4))
}

// TestFixToUnicodeRangeIssues_RepairsTheFontsThePageDrawsWith pins that the
// /ToUnicode range repair reaches the fonts of the /Resources a page resolves
// its names against, and no other.
func TestFixToUnicodeRangeIssues_RepairsTheFontsThePageDrawsWith(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	const cmapData = "/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n" +
		"/CMapName /Test def\n/CMapType 2 def\n" +
		"1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n" +
		"1 beginbfrange\n<01ED> <0206> <0061>\nendbfrange\n" +
		"endcmap\nCMapName currentdict /CMap defineresource pop\nend\nend\n"
	cmapStream := fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(cmapData), cmapData)
	fonts := func(name string, i int) reachNode { return reachNode{fonts: "/" + name + " " + reachExtraRef(i)} }
	extra := make([]string, 0, 10)
	for i := range 5 {
		extra = append(extra, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /ToUnicode "+reachExtraRef(5+i)+" >>")
	}
	for range 5 {
		extra = append(extra, cmapStream)
	}
	doc, alloc := newTestDoc(t, reachTreePDF(reachTree{
		root: fonts("UR", 0), mis: fonts("UA", 1), null: fonts("UB", 2), direct: fonts("UC", 3), dangle: fonts("UD", 4),
		extra: extra,
	}))
	if _, err := fixToUnicodeRangeIssues(ctx, alloc, doc, Options{}, nil, ""); err != nil {
		t.Fatalf("fixToUnicodeRangeIssues: %v", err)
	}
	assertPending(t, alloc, "the /ToUnicode crossing a first-byte boundary", reachExtraObj(6), reachExtraObj(7), reachExtraObj(8))
	assertNotPending(t, alloc, "the /ToUnicode crossing a first-byte boundary, which no page resolves,", reachExtraObj(5), reachExtraObj(9))
}

// TestFixTextControl_StripsOnlyUnderAFontThePageNames pins that the
// control-byte strip judges a string by the font the page's /Resources name
// under the selected name, and leaves one shown with a name they do not
// define.
//
// The strip read each page's fonts from the /Resources the parser merges from
// every ancestor, and took a name missing from them for a simple font. So the
// page whose /Parent names the root stripped a string it shows with the
// root's font, which it cannot name, and the page stating a reference to no
// object one it shows with its node's. A name the page cannot resolve selects
// no font, no .notdef rule judges its strings, and it may name a composite
// font, whose strings a byte-wise strip corrupts.
func TestFixTextControl_StripsOnlyUnderAFontThePageNames(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, reachTreePDF(reachTree{
		mis:    reachNode{content: `BT /F5 12 Tf (\001B) Tj /FR 12 Tf (\001A) Tj ET`},
		null:   reachNode{content: `BT /F3 12 Tf (\001C) Tj ET`},
		direct: reachNode{content: `BT /F4 12 Tf (\001D) Tj ET`},
		dangle: reachNode{content: `BT /F6 12 Tf (\001E) Tj ET`},
	}))
	if _, err := fixTextControl(ctx, alloc, doc, Options{}, nil, ""); err != nil {
		t.Fatalf("fixTextControl: %v", err)
	}
	for _, tc := range []struct {
		name    string
		content int
		want    []string
	}{
		{name: "a /Parent naming another node", content: reachMisContent, want: []string{"B", "\x01A"}},
		{name: "a null /Resources", content: reachNullContent, want: []string{"C"}},
		{name: "a page written directly in /Kids", content: reachDirectContent, want: []string{"D"}},
		{name: "a /Resources naming no object", content: reachDangleContent, want: []string{"\x01E"}},
	} {
		if got := liveShownStrings(t, alloc, doc, tc.content); strings.Join(got, "|") != strings.Join(tc.want, "|") {
			t.Errorf("%s: the page shows %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestFixPageResources_WritesAPageWrittenDirectlyInKids pins that a page
// written directly in its parent's /Kids, with no object number, is given the
// /Resources it inherits, written back into that array.
//
// ISO 19005-2, 6.2.2 t2 asks every page for /Resources of its own, and the
// repair skipped a page with no object number to write at, so it kept the
// violation.
func TestFixPageResources_WritesAPageWrittenDirectlyInKids(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, reachTreePDF(reachTree{}))
	if _, err := fixPageResources(ctx, alloc, doc, Options{}, nil, ""); err != nil {
		t.Fatalf("fixPageResources: %v", err)
	}
	page := liveDirectPage(t, alloc, doc)
	res := liveDictValue(ctx, alloc, doc, page["Resources"])
	if res == nil {
		t.Fatalf("the page states no /Resources dictionary: %v", page)
	}
	if fonts := resourceNames(alloc, doc, res, "Font"); !fonts["F4"] || fonts["FR"] {
		t.Errorf("the page was given /Font %v, want its node's F4 alone", fonts)
	}
	if _, ok := page["Contents"]; !ok {
		t.Errorf("the page lost its /Contents: %v", page)
	}
}

// TestResourcesHolderRepairs_WriteAPageWrittenDirectlyInKids pins that the
// fixers writing an inline repair into the page or ancestor holding a page's
// /Resources write it into a page written directly in its parent's /Kids,
// and that the next fixer reads the page as the last one left it.
//
// ISO 32000-1, Table 29 makes every element of /Kids an indirect reference,
// and each such write was refused for want of an object number, dropping the
// repair.
func TestResourcesHolderRepairs_WriteAPageWrittenDirectlyInKids(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	for _, tc := range []struct {
		name  string
		tree  reachTree
		fix   func(*writer.ObjAllocator, *document.Document) error
		check func(*testing.T, *writer.ObjAllocator, *document.Document, ast.PdfDict)
	}{
		{
			name: "graphics",
			tree: reachTree{
				direct:     reachNode{content: "q /GS0 gs Q"},
				directPage: "/Resources << /ExtGState << /GS0 " + directGState + " >> >>",
			},
			fix: func(alloc *writer.ObjAllocator, doc *document.Document) error {
				_, err := fixGraphics(ctx, alloc, doc, Options{}, nil, "")
				return err
			},
			check: func(t *testing.T, alloc *writer.ObjAllocator, doc *document.Document, res ast.PdfDict) {
				assertGStateRepaired(t, liveDictValue(ctx, alloc, doc, liveDictValue(ctx, alloc, doc, res["ExtGState"])["GS0"]))
			},
		},
		{
			name: "marked-content-lang",
			tree: reachTree{
				direct:     reachNode{content: "/Span /P1 BDC EMC"},
				directPage: "/Resources << /Properties << /P1 << /Lang (4w) >> >> >>",
			},
			fix: func(alloc *writer.ObjAllocator, doc *document.Document) error {
				repairPropertyListLangs(ctx, alloc, doc, false)
				return nil
			},
			check: func(t *testing.T, alloc *writer.ObjAllocator, doc *document.Document, res ast.PdfDict) {
				props := liveDictValue(ctx, alloc, doc, res["Properties"])
				if p1 := liveDictValue(ctx, alloc, doc, props["P1"]); p1 == nil || p1["Lang"] != nil {
					t.Errorf("the property list keeps its unusable /Lang: %v", props)
				}
			},
		},
		{
			name: "devicen",
			tree: reachTree{
				direct:     reachNode{content: "/CSD cs 1 1 scn 0 0 10 10 re f"},
				directPage: "/Resources << /ColorSpace << /CSD [/DeviceN [/SpotA /SpotB] /DeviceCMYK " + reachExtraRef(0) + "] >> >>",
				extra:      []string{treeTintFunction},
			},
			fix: func(alloc *writer.ObjAllocator, doc *document.Document) error {
				_, err := fixDeviceNColorants(ctx, alloc, doc, Options{}, nil, "")
				return err
			},
			check: func(t *testing.T, alloc *writer.ObjAllocator, doc *document.Document, res ast.PdfDict) {
				arr := liveArray(ctx, alloc, doc, liveDictValue(ctx, alloc, doc, res["ColorSpace"])["CSD"])
				if len(arr) < 5 {
					t.Fatalf("the DeviceN has no attributes: %v", arr)
				}
				colorants := liveDictValue(ctx, alloc, doc, liveDictValue(ctx, alloc, doc, arr[4])["Colorants"])
				if colorants["SpotA"] == nil || colorants["SpotB"] == nil {
					t.Errorf("the DeviceN lacks the /Colorants devicen completes: %v", arr)
				}
			},
		},
		{
			name: "page-resources, then graphics",
			tree: reachTree{
				direct: reachNode{res: "/ExtGState << /GS0 " + directGState + " >>", content: "q /GS0 gs Q"},
			},
			fix: func(alloc *writer.ObjAllocator, doc *document.Document) error {
				if _, err := fixPageResources(ctx, alloc, doc, Options{}, nil, ""); err != nil {
					return err
				}
				_, err := fixGraphics(ctx, alloc, doc, Options{}, nil, "")
				return err
			},
			check: func(t *testing.T, alloc *writer.ObjAllocator, doc *document.Document, res ast.PdfDict) {
				assertGStateRepaired(t, liveDictValue(ctx, alloc, doc, liveDictValue(ctx, alloc, doc, res["ExtGState"])["GS0"]))
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			doc, alloc := newTestDoc(t, reachTreePDF(tc.tree))
			if err := tc.fix(alloc, doc); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			page := liveDirectPage(t, alloc, doc)
			res := liveDictValue(ctx, alloc, doc, page["Resources"])
			if res == nil {
				t.Fatalf("the page states no /Resources dictionary: %v", page)
			}
			tc.check(t, alloc, doc, res)
		})
	}
}
