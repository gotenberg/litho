package build

import (
	"bytes"
	"fmt"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("resinherit_2b", resinherit2b) }

// resinherit2b builds resinherit_2b.pdf: the page attributes a page inherits, at PDF/A-2b.
//
// ISO 32000-1, 7.7.3.4 and Table 30 make /Resources and /MediaBox inheritable: a
// page that does not state one takes it from its nearest ancestor in the page
// tree that does, whole. A census of every corpus found no page inheriting from
// an intermediate /Pages node, only from the root, so a validator that fell back
// to the root's dictionary passed the gate while missing every font and colour
// space an intermediate node lends.
//
// Each page sits under an intermediate node of its own, and every font and colour
// space is a distinct object, so each verdict names the page it comes from:
//
// | Page     | Its /Resources        | Ancestor lends               | Answers                                   |
// | -------- | --------------------- | ---------------------------- | ----------------------------------------- |
// | `Inter`  | none                  | `F1`, `CS0`, two levels up   | `6.2.11.4.1/1`, `6.2.11.6/2`, `6.2.4.3/2`, `6.2.2/2` |
// | `Own`    | `F2` alone            | `F6`, `CS4`                  | `6.2.11.4.1/1` for `F2`                   |
// | `Null`   | `null`                | `F3`, `CS2`                  | `6.2.11.4.1/1`, `6.2.4.3/3`, `6.2.2/2`    |
// | `Dangle` | `9999 0 R`, no object | `F4`, `CS3`                  | nothing from the ancestor                 |
// | `Parent` | none, `/Parent` wrong | `F5`, on the node listing it | `6.2.11.4.1/1`, `6.2.2/2`                 |
// | `Box1`, `Box2` | none            | `/MediaBox [0 0 2 2]`        | `6.1.13/11`, once per page                |
//
// Every object a page must not reach carries a defect no reached object does,
// so reaching it adds a rule class rather than an instance. `FR`, the root's
// font, `F6` and `F4` are TrueType fonts outside the standard 14 with no
// /FirstChar (`6.2.11.2/4`). `CS4` and `CS3` are Separations naming `SpotRGB`,
// the colorant `CS0` names, over another alternate (`6.2.4.4/2`). Every page
// draws with the names its ancestors define: `Inter` with `FR`, which its nearest
// ancestor does not define, since that ancestor's dictionary replaces the root's
// rather than merging with it; `Own` with `F6` and `CS4`, past the dictionary it
// states; `Dangle` with `F4` and `CS3`.
//
// A null entry is absent (ISO 32000-1, 7.3.7), and a reference to a missing
// object is a value, which is how veraPDF reads both. `Parent` names the root as
// its /Parent while the intermediate node lists it in /Kids: veraPDF inherits
// through the node the tree walk came from. `F1` is the one TrueType font with no
// /Encoding, which makes the inheritance from an intermediate node a rule class
// of its own (`6.2.11.6/2`), not only an instance of the embedding rule the other
// pages fire.
//
// qpdf cannot write a null dictionary value, which it drops, nor a reference to a
// missing object, so both are planted as placeholder names of the same length and
// patched as bytes after the save, which moves no offset. The tree is built by
// hand: pdf.AddPage flattens it and pushes inherited attributes onto each
// page, which would delete the constructs the fixture exists for.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func resinherit2b() ([]byte, error) {
	pdf := pdfgen.New()

	tintRGB := d("FunctionType", 2, "Domain", a(0, 1), "C0", a(0, 0, 0), "C1", a(1, 0, 0), "N", 1)
	tintCMYK := d("FunctionType", 2, "Domain", a(0, 1), "C0", a(0, 0, 0, 0), "C1", a(1, 0, 0, 0), "N", 1)

	// standardFont is an unembedded standard font: it fails 6.2.11.4.1 t1 and
	// nothing else.
	standardFont := func(base string) *pdfgen.Dict {
		return pdf.Indirect(d("Type", n("Font"), "Subtype", n("Type1"), "BaseFont", n(base)))
	}

	// truetype is an unembedded non-symbolic TrueType font outside the
	// standard 14.
	truetype := func(name string, encoding, first bool) *pdfgen.Dict {
		dd := d(
			"Type", n("Font"),
			"Subtype", n("TrueType"),
			"BaseFont", n("/"+name),
			"LastChar", 65,
			"Widths", a(722),
			"FontDescriptor", pdf.Indirect(d(
				"Type", n("FontDescriptor"),
				"FontName", n("/"+name),
				"Flags", 32,
				"FontBBox", a(-166, -225, 1000, 931),
				"ItalicAngle", 0,
				"Ascent", 718,
				"Descent", -207,
				"CapHeight", 718,
				"StemV", 88,
			)),
		)
		if encoding {
			dd.Set("Encoding", n("WinAnsiEncoding"))
		}
		if first {
			dd.Set("FirstChar", 65)
		}
		return pdf.Indirect(dd)
	}

	// unreachedFont is a font no page may reach: no /FirstChar (6.2.11.2 t4)
	// if one does.
	unreachedFont := func(name string) *pdfgen.Dict {
		return truetype(name, true, false)
	}

	// separation is a Separation over a device space, which no OutputIntent
	// covers.
	separation := func(colorant string, alternate pdfgen.Name, tint *pdfgen.Dict) obj {
		return pdf.MakeIndirect(a(n("Separation"), n(colorant), alternate, tint))
	}

	// unreachedSpace is a colour space no page may reach: SpotRGB over
	// DeviceCMYK, which contradicts CS0's SpotRGB over DeviceRGB (6.2.4.4 t2)
	// if one does.
	unreachedSpace := func() obj {
		return separation("/SpotRGB", n("DeviceCMYK"), tintCMYK)
	}

	resources := func(fonts, spaces []any) *pdfgen.Dict {
		res := d()
		if len(fonts) > 0 {
			res.Set("Font", d(fonts...))
		}
		if len(spaces) > 0 {
			res.Set("ColorSpace", d(spaces...))
		}
		return res
	}

	textOps := func(name string, y int) string {
		return fmt.Sprintf("BT /%s 12 Tf 50 %d Td (A) Tj ET\n", name, y)
	}

	fillOps := func(name string) string {
		return fmt.Sprintf("/%s cs 1 scn 0 0 10 10 re f\n", name)
	}

	node := func(parent *pdfgen.Dict, extra ...any) *pdfgen.Dict {
		dd := d("Type", n("Pages"), "Kids", a(), "Count", 0, "Parent", parent)
		for i := 0; i < len(extra); i += 2 {
			dd.Set(extra[i].(string), extra[i+1])
		}
		return pdf.Indirect(dd)
	}

	page := func(parent *pdfgen.Dict, ops *string, extra ...any) *pdfgen.Dict {
		dd := d("Type", n("Page"), "Parent", parent)
		if ops != nil {
			dd.Set("Contents", pdf.MakeIndirect(pdf.Stream([]byte(*ops))))
		}
		for i := 0; i < len(extra); i += 2 {
			dd.Set(extra[i].(string), extra[i+1])
		}
		return pdf.Indirect(dd)
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

	ops := func(s string) *string { return &s }

	root := pdf.Pages
	root.Set("MediaBox", a(0, 0, 612, 792))
	root.Set("Resources", resources([]any{"/FR", unreachedFont("LithoRoot")}, nil))

	// Inter: the node two levels up lends F1 and CS0; the node between states none.
	interTop := node(root, "/Resources", resources(
		[]any{"/F1", truetype("LithoInter", false, true)},
		[]any{"/CS0", separation("/SpotRGB", n("DeviceRGB"), tintRGB)},
	))
	interMid := node(interTop)
	attach(root, interTop, 0)
	attach(interTop, interMid, 0)
	attach(interMid, page(interMid, ops(textOps("F1", 700)+textOps("FR", 680)+fillOps("CS0"))), 1)

	// Own: the page states F2 alone, so its ancestor's F6 and CS4 stay out of reach.
	ownNode := node(root, "/Resources", resources([]any{"/F6", unreachedFont("LithoOwn")}, []any{"/CS4", unreachedSpace()}))
	attach(root, ownNode, 0)
	attach(ownNode, page(
		ownNode,
		ops(textOps("F2", 700)+textOps("F6", 680)+fillOps("CS4")),
		"/Resources", resources([]any{"/F2", standardFont("/Courier")}, nil),
	), 1)

	// Null: a null /Resources is absent, so the ancestor's F3 and CS2 are inherited.
	nullNode := node(root, "/Resources", resources(
		[]any{"/F3", standardFont("/Helvetica-Bold")},
		[]any{"/CS2", separation("/SpotNull", n("DeviceCMYK"), tintCMYK)},
	))
	attach(root, nullNode, 0)
	attach(nullNode, page(nullNode, ops(textOps("F3", 700)+fillOps("CS2")), "/Resources", n("/LithoNullResources")), 1)

	// Dangle: a reference to a missing object is the page's own value, so F4 and
	// CS3 are not inherited.
	dangleNode := node(root, "/Resources", resources([]any{"/F4", unreachedFont("LithoDangle")}, []any{"/CS3", unreachedSpace()}))
	attach(root, dangleNode, 0)
	attach(dangleNode, page(dangleNode, ops(textOps("F4", 700)+fillOps("CS3")), "/Resources", n("/LithoDangleRes")), 1)

	// Parent: listed by parent_node, while its /Parent names the root.
	parentNode := node(root, "/Resources", resources([]any{"/F5", standardFont("/Helvetica-Oblique")}, nil))
	attach(root, parentNode, 0)
	attach(parentNode, page(root, ops(textOps("F5", 700))), 1)

	// Box1, Box2: a 2 by 2 media box on their node, below the 3-unit minimum.
	boxNode := node(root, "/MediaBox", a(0, 0, 2, 2))
	attach(root, boxNode, 0)
	attach(boxNode, page(boxNode, nil), 1)
	attach(boxNode, page(boxNode, nil), 1)

	// Deliberately no /Metadata.
	raw, err := save(pdf, "1.7")
	if err != nil {
		return nil, err
	}

	for _, p := range [][2]string{{"/LithoNullResources", "null"}, {"/LithoDangleRes", "9999 0 R"}} {
		placeholder, value := []byte(p[0]), []byte(p[1])
		if bytes.Count(raw, placeholder) != 1 {
			return nil, fmt.Errorf("resinherit_2b: expected exactly one %q", placeholder)
		}
		value = append(value, bytes.Repeat([]byte(" "), len(placeholder)-len(value))...)
		raw = bytes.ReplaceAll(raw, placeholder, value)
	}
	return raw, nil
}
