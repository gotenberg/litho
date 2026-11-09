package build

import (
	"fmt"
	"strings"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("parentreach_2b", parentreach2b) }

// parentreach2b builds parentreach_2b.pdf: defects in the /Resources a page inherits from the
// node listing it, while its /Parent names the root, at PDF/A-2b.
//
// ISO 32000-1, 7.7.3.4 hands a page stating no /Resources the dictionary of its
// nearest ancestor in the page tree, and veraPDF reads the tree through /Kids, so
// a page whose /Parent names another node inherits from the node listing it. The
// repairs of the resources a page inherits climbed /Parent, so on such a page
// they repaired the root's dictionary and left the one the page draws with as it
// was. litho resinherit_2b.pdf has the construct, but its node lends a font,
// which no repair touches.
//
// Four pages sit under four intermediate nodes, each /Parent naming the root, and
// each node lends the page a defect one repair owns:
//
// | Page | Its node lends                                        | Repair              |
// | ---- | ----------------------------------------------------- | ------------------- |
// | `Gs` | `GS0`, a graphics state with a transfer function      | graphics            |
// | `Dn` | `CSD`, a DeviceN of nine colorants and no /Colorants  | devicen, aggressive |
// | `Fm` | `Fm0`, a form stating no /Resources, and `GS1`        | nested-resources    |
// | `Lg` | `P1`, a property list whose /Lang is no language tag  | marked-content-lang |
//
// The root lends `GSR`, a conforming graphics state no page draws with. A CMYK
// OutputIntent covers the DeviceCMYK alternate. The tree is built by hand:
// pikepdf's page list flattens it and pushes inherited attributes onto each page,
// which would delete the construct the fixture exists for.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func parentreach2b() ([]byte, error) {
	pdf := pdfgen.New()

	destination := pdf.Stream(iccMinimalProfile("prtr", "CMYK", iccVersion21, "OUTI"))
	destination.Set("N", 4)
	pdf.Root.Set("OutputIntents", a(
		pdf.Indirect(d(
			"Type", n("OutputIntent"),
			"S", n("GTS_PDFA1"),
			"OutputConditionIdentifier", text("litho destination"),
			"DestOutputProfile", pdf.MakeIndirect(destination),
		)),
	))

	node := func(parent, resources *pdfgen.Dict) *pdfgen.Dict {
		return pdf.Indirect(d("Type", n("Pages"), "Kids", a(), "Count", 0, "Parent", parent, "Resources", resources))
	}

	// page is a page stating no /Resources, whose /Parent is parent.
	page := func(parent *pdfgen.Dict, ops string) *pdfgen.Dict {
		return pdf.Indirect(d("Type", n("Page"), "Parent", parent, "Contents", pdf.MakeIndirect(pdf.Stream([]byte(ops)))))
	}

	// attach appends kid to parent's /Kids and adds its pages to every /Count
	// above.
	attach := func(parent, kid *pdfgen.Dict, pages int) {
		parent.Get("Kids").(*pdfgen.Array).Append(kid)
		nd := parent
		for {
			nd.Set("Count", int(nd.Get("Count").(pdfgen.Int))+pages)
			if !nd.Has("Parent") {
				break
			}
			nd = nd.Get("Parent").(*pdfgen.Dict)
		}
	}

	root := pdf.Pages

	// misparented is a node lending resources, listing a page whose /Parent
	// names the root.
	misparented := func(resources *pdfgen.Dict, ops string) {
		nd := node(root, resources)
		attach(root, nd, 0)
		attach(nd, page(root, ops), 1)
	}

	root.Set("MediaBox", a(0, 0, 200, 200))
	root.Set("Resources", d("ExtGState", d("GSR", d("Type", n("ExtGState"), "LW", 1))))

	// Gs: ISO 19005-2, 6.2.5 forbids /TR in a graphics state the page sets.
	misparented(
		d("ExtGState", d("GS0", d("Type", n("ExtGState"), "TR", n("Identity")))),
		"q /GS0 gs Q\n",
	)

	// Dn: ISO 19005-2, 6.2.4.4 t1 requires /Colorants of a DeviceN naming a spot,
	// and ISO 19005-1, 6.1.12 caps it at eight colorants.
	tint := pdf.Indirect(
		d("FunctionType", 2, "Domain", a(0, 1), "C0", a(0, 0, 0, 0), "C1", a(0, 0.5, 1, 0), "N", 1),
	)
	spots := a()
	for i := 1; i < 10; i++ {
		spots.Append(n(fmt.Sprintf("/Spot%d", i)))
	}
	misparented(
		d("ColorSpace", d("CSD", a(n("DeviceN"), spots, n("DeviceCMYK"), tint))),
		fmt.Sprintf("/CSD cs %s scn 0 0 5 5 re f\n", strings.TrimSuffix(strings.Repeat("1 ", 9), " ")),
	)

	// Fm: the form states no /Resources, so it names GS1 only through the page's
	// (ISO 19005-2, 6.2.2 t2).
	form := pdf.Stream([]byte("/GS1 gs"))
	form.Set("Type", n("XObject"))
	form.Set("Subtype", n("Form"))
	form.Set("BBox", a(0, 0, 20, 20))
	misparented(
		d(
			"XObject", d("Fm0", pdf.MakeIndirect(form)),
			"ExtGState", d("GS1", d("Type", n("ExtGState"), "LW", 2)),
		),
		"/Fm0 Do\n",
	)

	// Lg: a marked-content /Lang has to be a language tag (ISO 32000-1, 14.9.2).
	misparented(
		d("Properties", d("P1", d("Lang", text("4w")))),
		"/Span /P1 BDC EMC\n",
	)

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))
	return save(pdf, "1.7")
}
