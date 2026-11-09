package build

import (
	"strings"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("formplacement_2b", formplacement2b) }

// formplacement2b builds formplacement_2b.pdf: widgets whose appearance a viewer places by
// ISO 32000-1, 12.5.5, Algorithm 8.1 somewhere a translation to /Rect does not,
// at PDF/A-2b.
//
// A viewer transforms an appearance's /BBox by its /Matrix, then scales and
// translates the box it gets onto the annotation's /Rect. Flattening a form, and
// the drawing of signed signatures every rewrite does, placed an appearance by
// translating it to the first corner /Rect names, which is right only for a box
// at the origin sized to /Rect. The corpora reach few such appearances, none
// rotated by /Matrix, so this fixture gives the gate every case:
//
// | Field     | /Rect                | Appearance /BBox  | /Matrix           | Viewer places it    |
// | --------- | -------------------- | ----------------- | ----------------- | ------------------- |
// | control   | [20 200 100 260]     | [0 0 80 60]       | none              | translated          |
// | offset    | [120 200 200 260]    | [200 100 280 160] | none              | box moved to /Rect  |
// | scaled    | [220 200 300 260]    | [0 0 40 30]       | none              | scaled by 2         |
// | rotated   | [20 40 80 120]       | [0 0 80 60]       | 90 degrees        | turned upright      |
// | combined  | [120 40 180 120]     | [10 20 50 50]     | 90 degrees        | turned, scaled by 2 |
// | reversed  | [300 120 220 40]     | [0 0 80 80]       | translates 40,-30 | translation undone  |
// | collapsed | [350 50 350 50]      | [0 0 20 20]       | none              | scaled to nothing   |
//
// Each appearance fills its box in a light colour, a dark square in the box's
// lower-left corner and a red border, so a misplaced, unturned or unscaled
// drawing shows. reversed names its /Rect corners in the other order (ISO
// 32000-1, 7.9.5). collapsed has no area: a viewer draws nothing of it.
//
// The fields are text fields that hold no value, so the appearance is all there
// is to draw. Each widget is tagged under a Form element of its own, so the
// flattened appearances are tagged content (formuntagged_ua1.pdf covers the
// untagged case). The file
// declares an RGB OutputIntent, and every widget prints.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func formplacement2b() ([]byte, error) {
	const printFlag = 4

	pdf := pdfgen.New()

	destination := pdf.Stream(iccRGBDisplayProfile())
	destination.Set("N", 3)
	pdf.Root.Set("OutputIntents", a(
		pdf.Indirect(d(
			"Type", n("OutputIntent"),
			"S", n("GTS_PDFA1"),
			"OutputConditionIdentifier", text("litho RGB display"),
			"DestOutputProfile", pdf.MakeIndirect(destination),
		)),
	))

	num := pyG

	// appearance is a form that fills bbox, marks its lower-left corner and
	// strokes its border.
	appearance := func(bbox []int, fill [3]float64, matrix []int) *pdfgen.Stream {
		x0, y0, x1, y1 := float64(bbox[0]), float64(bbox[1]), float64(bbox[2]), float64(bbox[3])
		w, h := x1-x0, y1-y0
		r, g, b := fill[0], fill[1], fill[2]
		content := strings.Join(
			[]string{
				num(r), num(g), num(b), "rg",
				num(x0), num(y0), num(w), num(h), "re f",
				"0 0 0.5 rg",
				num(x0), num(y0), num(w / 3), num(h / 3), "re f",
				"0.7 0 0 RG 2 w",
				num(x0 + 1), num(y0 + 1), num(w - 2), num(h - 2), "re S\n",
			},
			" ",
		)
		s := pdf.Stream([]byte(content))
		s.Set("Type", n("XObject"))
		s.Set("Subtype", n("Form"))
		s.Set("BBox", bbox)
		if matrix != nil {
			s.Set("Matrix", matrix)
		}
		s.Set("Resources", d())
		pdf.MakeIndirect(s)
		return s
	}

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"Parent", pdf.Root.Get("Pages"),
		"MediaBox", a(0, 0, 400, 300),
		"Resources", d(),
		"Contents", pdf.MakeIndirect(pdf.Stream([]byte(""))),
	))
	pdf.Pages.Set("Kids", a(page))
	pdf.Pages.Set("Count", 1)

	quarter := []int{0, 1, -1, 0, 0, 0}
	type widgetSpec struct {
		name   string
		rect   []int
		bbox   []int
		matrix []int
		fill   [3]float64
	}
	widgets := []widgetSpec{
		{"control", []int{20, 200, 100, 260}, []int{0, 0, 80, 60}, nil, [3]float64{0.85, 0.9, 1}},
		{"offset", []int{120, 200, 200, 260}, []int{200, 100, 280, 160}, nil, [3]float64{0.85, 1, 0.85}},
		{"scaled", []int{220, 200, 300, 260}, []int{0, 0, 40, 30}, nil, [3]float64{1, 0.95, 0.8}},
		{"rotated", []int{20, 40, 80, 120}, []int{0, 0, 80, 60}, quarter, [3]float64{0.95, 0.85, 1}},
		{"combined", []int{120, 40, 180, 120}, []int{10, 20, 50, 50}, quarter, [3]float64{0.85, 1, 1}},
		{"reversed", []int{300, 120, 220, 40}, []int{0, 0, 80, 80}, []int{1, 0, 0, 1, 40, -30}, [3]float64{1, 0.88, 0.88}},
		{"collapsed", []int{350, 50, 350, 50}, []int{0, 0, 20, 20}, nil, [3]float64{0.9, 0.9, 0.9}},
	}

	structRoot := pdf.Indirect(d("Type", n("StructTreeRoot")))
	document := pdf.Indirect(d("Type", n("StructElem"), "S", n("Document"), "P", structRoot))

	var fields, elements, nums []any
	for key, w := range widgets {
		widget := pdf.Indirect(d(
			"Type", n("Annot"),
			"Subtype", n("Widget"),
			"FT", n("Tx"),
			"T", text(w.name),
			"Rect", w.rect,
			"P", page,
			"F", printFlag,
			"StructParent", key,
			"AP", d("N", appearance(w.bbox, w.fill, w.matrix)),
		))
		element := pdf.Indirect(d(
			"Type", n("StructElem"),
			"S", n("Form"),
			"P", document,
			"Pg", page,
			"K", d("Type", n("OBJR"), "Obj", widget),
		))
		fields = append(fields, widget)
		elements = append(elements, element)
		nums = append(nums, key, element)
	}

	page.Set("Annots", a(fields...))
	pdf.Root.Set("AcroForm", d("Fields", a(fields...)))

	// Each widget is tagged under a Form element of its own (ISO 32000-1,
	// 14.8.4.4), so flattening draws each appearance as that element's content
	// rather than as unmarked content, which PDF/UA forbids (ISO 14289-1, 7.1).
	document.Set("K", a(elements...))
	structRoot.Set("K", document)
	structRoot.Set("ParentTree", pdf.Indirect(d("Nums", a(nums...))))
	structRoot.Set("ParentTreeNextKey", len(widgets))
	pdf.Root.Set("StructTreeRoot", structRoot)
	pdf.Root.Set("MarkInfo", d("Marked", true))
	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))
	return save(pdf, "1.7")
}
