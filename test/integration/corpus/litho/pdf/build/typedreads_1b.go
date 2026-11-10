package build

import (
	"errors"
	"fmt"
	"strings"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("typedreads_1b", typedreads1b) }

// typedreads1b builds typedreads_1b.pdf: the values veraPDF reads again through typed objects.
//
// veraPDF judges a value once as a COS object, reached through the
// cross-reference table, and again wherever a typed object of its model wraps it
// in a new COS object: an outline item's /Title as a CosTextString, a structure
// element's /S as a CosUnicodeName and its /Lang, /Alt and /ActualText, the
// catalog's /Lang, each page box as a CosBBox, a stream's /Filter names, the
// file specifications of the /EmbeddedFiles name tree. The wrapper has no
// identity of its own, so every rule of its type judges it again: the hex string
// tests of ISO 19005-1, 6.1.6, and the limits of 6.1.12. No file of any corpus
// holds a hex string defect in one of these places, or a box, a filter name or a
// structure type past a limit, so no count reached the re-reads; two verapdf
// files hold an outline /Title past the string limit, which is where the reading
// showed.
//
// Every defective string is distinct, so each row counts on its own:
//
// | Where                                                              | Value     | 6.1.6 at PDF/A-1b |
// | ------------------------------------------------------------------ | --------- | ----------------- |
// | the catalog's `/Lang`                                              | `<6e6c7>` | t1 x2             |
// | the outline dictionary's `/Title`, no item                         | `<414>`   | t1 x1             |
// | the first item's `/Title`                                          | `<41424>` | t1 x2             |
// | its child's `/Title`                                               | `<434>`   | t1 x2             |
// | the second item's `/Title`, an indirect string; `/Next` loops back | `<4G>`    | t2 x2             |
// | an item only the outline's `/Last` names                           | `<444>`   | t1 x1             |
// | a structure element's `/Lang`, `/Alt`, `/ActualText`               | `<656e7>` | t1 x2 each        |
// | its `/E`, which no typed object reads                              | `<434>`   | t1 x1             |
// | a `Span` listed twice in its parent's `/K`, its `/Alt`             | `<434>`   | t1 x2             |
// | that `Span`'s `/Lang`, an indirect string                          | `<647>`   | t1 x2             |
// | a marked-content reference's `/Alt`                                | `<424>`   | t1 x1             |
// | the `/Lang` of an `Annot` element a square's `/StructParent` names | `<657>`   | t1 x3             |
// | the `/Lang` of a `Form` element a text field's widget names        | `<667>`   | t1 x4             |
// | the `/Lang` of an element a second square names                    | `<6a7>`   | t1 x3             |
// | the `/Lang` of an element a kid widget with no `/T` names          | `<6b7>`   | t1 x2             |
// | an embedded file specification's `/Desc`                           | `<424>`   | t1 x2             |
//
// Each `/Lang` ends on the digit 7, which an odd hex string reads as `70`, so it
// decodes to a well-formed language tag (`nlp`, `enp`, `dp`): the hex tests are
// all it fails, and the language rules of PDF/A-1a, PDF/A-2a and PDF/UA, whose
// counts veraPDF states per read, stay silent.
//
// The limits ride on names and reals: a structure type and a `/RoleMap` key of
// 130 bytes (6.1.12 t4, twice each), a stream `/Filter` name of 130 bytes (t4,
// twice), five page boxes of one page holding a real past 32767 (t2, twice each),
// a media box two pages inherit from an intermediate node (t2, once for the node
// and once for each page), and a media box whose last element is an indirect
// real (t2, once): 6.1.12 t2 counts 14, t4 counts 6.
//
// The counts rest on veraPDF's reach, measured one construct per file before they
// were packed. OutlinesHelper walks the items from the outline's /First along
// /First and /Next, each once, and never reads the outline dictionary's own
// /Title or an item only /Last names. TaggedPDFHelper walks the structure tree
// from the root's /K, reading an indirect element once. An annotation reads the
// /Lang of the element its /StructParent maps to, and a form field too, down
// every /Kids entry holding a /T; the widget that is also the field reads it
// twice. A media box is judged for every page it applies to.
//
// qpdf writes every string it knows as a literal, so each defective hex string is
// planted as a placeholder literal of the same width and patched as bytes after
// the save, which moves no offset. The page tree is built by hand, since
// pdf.AddPage pushes an inherited /MediaBox onto each page.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func typedreads1b() ([]byte, error) {
	pdf := pdfgen.New()
	type patch struct{ placeholder, literal []byte }
	var patches []patch
	var tooWide error

	// hexstr is a placeholder string patched into the hex literal given, bytes and all.
	hexstr := func(literal string) pdfgen.String {
		placeholder := fmt.Sprintf("(Q%02d%s)", len(patches), strings.Repeat("x", max(0, len(literal)-5)))
		if len(literal) > len(placeholder) && tooWide == nil {
			tooWide = errors.New("typedreads_1b: hex literal wider than its placeholder")
		}
		padded := literal + strings.Repeat(" ", max(0, len(placeholder)-len(literal)))
		patches = append(patches, patch{[]byte(placeholder), []byte(padded)})
		return text(placeholder[1 : len(placeholder)-1])
	}

	long := strings.Repeat("X", 130)

	// Pages: A carries five boxes and the annotations, B and C inherit a media box
	// from an intermediate node, D names an indirect real in its media box.
	root := pdf.Pages
	root.Set("MediaBox", a(0, 0, 612, 792))

	page := func(parent *pdfgen.Dict, extra ...any) *pdfgen.Dict {
		dd := d("Type", n("Page"), "Parent", parent, "Resources", d(),
			"Contents", pdf.MakeIndirect(pdf.Stream([]byte("0 0 m\n"))))
		for i := 0; i < len(extra); i += 2 {
			dd.Set(extra[i].(string), extra[i+1])
		}
		return pdf.Indirect(dd)
	}

	attach := func(parent, kid *pdfgen.Dict, pages int) {
		parent.Get("Kids").(*pdfgen.Array).Append(kid)
		node := parent
		for {
			node.Set("Count", int(node.Get("Count").(pdfgen.Int))+pages)
			if !node.Has("Parent") {
				break
			}
			node = node.Get("Parent").(*pdfgen.Dict)
		}
	}

	pageA := page(
		root,
		"MediaBox", a(0, 0, 612, 40000.5),
		"CropBox", a(0, 0, 612, 40001.5),
		"TrimBox", a(0, 0, 612, 40002.5),
		"BleedBox", a(0, 0, 612, 40003.5),
		"ArtBox", a(0, 0, 612, 40004.5),
	)
	attach(root, pageA, 1)
	node := pdf.Indirect(d("Type", n("Pages"), "Kids", a(), "Count", 0, "Parent", root,
		"MediaBox", a(0, 0, 612, 40005.5)))
	attach(root, node, 0)
	attach(node, page(node), 1)
	attach(node, page(node), 1)
	// pikepdf, which first wrote the file, could not keep a number indirect, so
	// the indirect real is planted as an indirect placeholder name and patched like the strings.
	const realPlaceholder = "/QIndirectReal"
	patches = append(patches, patch{[]byte(realPlaceholder), []byte(fmt.Sprintf("%-*s", len(realPlaceholder), "40006.5"))})
	attach(root, page(root, "MediaBox", a(0, 0, 612, pdf.MakeIndirect(n(realPlaceholder)))), 1)

	// Structure tree.
	structRoot := pdf.Indirect(d("Type", n("StructTreeRoot")))

	element := func(s string, parent *pdfgen.Dict, extra ...any) *pdfgen.Dict {
		dd := d("Type", n("StructElem"), "S", n("/"+s), "P", parent)
		for i := 0; i < len(extra); i += 2 {
			dd.Set(extra[i].(string), extra[i+1])
		}
		return pdf.Indirect(dd)
	}

	eEntries := element("P", structRoot, "Lang", hexstr("<656e7>"), "Alt", hexstr("<414>"),
		"ActualText", hexstr("<424>"), "E", hexstr("<434>"))
	eLong := element(long, structRoot)
	eDoc := element("Document", structRoot)
	span := element("Span", eDoc, "Alt", hexstr("<434>"), "Lang", pdf.MakeIndirect(hexstr("<647>")))
	// A /K looping back to the element would do too, but veraPDF's PDF/A-3 file
	// specification walk follows it until its stack overflows.
	span.Set("K", 0)
	span.Set("Pg", pageA)
	eDoc.Set("K", a(span, span, d("Type", n("MCR"), "Alt", hexstr("<424>"))))
	eAnnot := element("Annot", structRoot, "Lang", hexstr("<657>"))
	eForm := element("Form", structRoot, "Lang", hexstr("<667>"))
	eAnnot2 := element("Annot", structRoot, "Lang", hexstr("<6a7>"))
	eKid := element("Form", structRoot, "Lang", hexstr("<6b7>"))
	structRoot.Set("K", a(eEntries, eLong, eDoc, eAnnot, eForm, eAnnot2))
	structRoot.Set("RoleMap", d(long, n("P")))
	structRoot.Set("ParentTree", d("Nums", a(0, eAnnot, 1, eForm, 2, eAnnot2, 3, eKid)))
	structRoot.Set("ParentTreeNextKey", 4)
	pdf.Root.Set("StructTreeRoot", structRoot)

	// Annotations and form fields reading their elements' /Lang.
	square := pdf.Indirect(d("Type", n("Annot"), "Subtype", n("Square"), "Rect", a(0, 0, 10, 10), "F", 4,
		"StructParent", 0))
	widget := pdf.Indirect(d("Type", n("Annot"), "Subtype", n("Widget"), "FT", n("Tx"), "T", text("f1"),
		"Rect", a(20, 0, 30, 10), "F", 4, "StructParent", 1))
	square2 := pdf.Indirect(d("Type", n("Annot"), "Subtype", n("Square"), "Rect", a(40, 0, 50, 10), "F", 4,
		"StructParent", 2))
	parentField := pdf.Indirect(d("FT", n("Tx"), "T", text("parent"), "Kids", a()))
	kid := pdf.Indirect(d("Type", n("Annot"), "Subtype", n("Widget"), "Parent", parentField,
		"Rect", a(60, 0, 70, 10), "F", 4, "StructParent", 3))
	parentField.Get("Kids").(*pdfgen.Array).Append(kid)
	pageA.Set("Annots", a(square, widget, square2, kid))
	pdf.Root.Set("AcroForm", d("Fields", a(widget, parentField)))

	// Outline: the dictionary, a first item with a child, a second item looping
	// back to the first, and an item only /Last names.
	outlines := pdf.Indirect(d("Type", n("Outlines"), "Title", hexstr("<414>")))
	first := pdf.Indirect(d("Title", hexstr("<41424>"), "Parent", outlines))
	child := pdf.Indirect(d("Title", hexstr("<434>"), "Parent", first))
	second := pdf.Indirect(d("Title", pdf.MakeIndirect(hexstr("<4G>")), "Parent", outlines, "Prev", first))
	lastOnly := pdf.Indirect(d("Title", hexstr("<444>"), "Parent", outlines))
	first.Set("Next", second)
	first.Set("First", child)
	first.Set("Last", child)
	second.Set("Next", first)
	outlines.Set("First", first)
	outlines.Set("Last", lastOnly)
	pdf.Root.Set("Outlines", outlines)

	pdf.Root.Set("Lang", hexstr("<6e6c7>"))

	// An embedded file whose specification is reached through the name tree.
	payload := []byte("litho corpus attachment")
	ef := pdf.Stream(payload)
	ef.Set("Type", n("EmbeddedFile"))
	spec := pdf.Indirect(d("Type", n("Filespec"), "F", text("b.txt"), "UF", text("b.txt"),
		"Desc", hexstr("<424>"), "EF", d("F", pdf.MakeIndirect(ef))))
	pdf.Root.Set("Names", d("EmbeddedFiles", d("Names", a(text("b"), spec))))

	// A stream whose /Filter names a filter past the name limit, reached from the
	// catalog so the object sweep reads it.
	filtered := pdf.Stream([]byte("abc"))
	filtered.Set("Filter", n("/"+strings.Repeat("F", 130)))
	pdf.Root.Set("Extra", pdf.MakeIndirect(filtered))

	if tooWide != nil {
		return nil, tooWide
	}

	// Deliberately no /Metadata and no output intent.
	raw, err := save(pdf, "1.4")
	if err != nil {
		return nil, err
	}

	for _, p := range patches {
		raw, err = patchOnce(raw, p.placeholder, p.literal, "typedreads_1b")
		if err != nil {
			return nil, err
		}
	}
	return raw, nil
}
