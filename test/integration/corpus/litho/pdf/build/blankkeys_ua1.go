package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("blankkeys_ua1", blankkeysUa1) }

// blankkeysUa1 builds blankkeys_ua1.pdf: images whose dictionaries say where and how they
// paint, blanked by a redaction, at PDF/UA-1.
//
// Redaction rewrites the image a region covers with new samples. The rewrite
// wrote a fresh dictionary, so an image lost every entry blanking did not set,
// and with /OC a hidden image's blank showed. Four 16 x 16 RGB ramps on page 1
// carry the entries a blanked image keeps (ISO 32000-1, Table 89), and page 2
// draws one of them again, so that one is blanked in a copy:
//
// | Image | Draws on | Carries                                       | Tagged as                         |
// | ----- | -------- | --------------------------------------------- | --------------------------------- |
// | ImA   | 1        | /Interpolate true, /Intent /Perceptual, /Name | a Figure's marked content, MCID 0 |
// | ImB   | 1        | /OC naming a group the default config hides   | an artifact                       |
// | ImC   | 1        | /StructParent 1                               | a Figure's object reference       |
// | ImE   | 1 and 2  | /StructParent 2                               | a Figure's object reference       |
//
// ImC and ImE are drawn outside any marked content: their Figure elements name
// them by /OBJR and their /StructParent keys map back to those elements (ISO
// 32000-1, 14.7.4.3 and 14.7.4.4). veraPDF reads such a Do as untagged content
// (ISO 14289-1, 7.1 test 3), and so does litho.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func blankkeysUa1() ([]byte, error) {
	pdf := pdfgen.New()

	const size = 16

	// ramp is a 16 x 16 DeviceRGB ramp, each sample row a different colour.
	ramp := func(shift int, entries ...any) *pdfgen.Stream {
		var raw []byte
		for y := range size {
			for x := range size {
				raw = append(raw,
					byte((x*16+shift)%256), //nolint:gosec // reduced modulo 256.
					byte((y*16)%256),
					byte(((255-x*16+shift)%256+256)%256),
				)
			}
		}
		img := pdf.Stream(zlibCompress(raw))
		img.Set("Type", n("XObject"))
		img.Set("Subtype", n("Image"))
		img.Set("Width", size)
		img.Set("Height", size)
		img.Set("ColorSpace", n("DeviceRGB"))
		img.Set("BitsPerComponent", 8)
		img.Set("Filter", n("FlateDecode"))
		for i := 0; i < len(entries); i += 2 {
			img.Set(entries[i].(string), entries[i+1])
		}
		pdf.MakeIndirect(img)
		return img
	}

	hidden := pdf.Indirect(d("Type", n("OCG"), "Name", text("Hidden layer")))
	pdf.Root.Set("OCProperties", d(
		"OCGs", a(hidden),
		"D", d("Name", text("Default"), "OFF", a(hidden)),
	))

	imA := ramp(0, "Interpolate", true, "Intent", n("Perceptual"), "Name", n("ImA"))
	imB := ramp(64, "OC", hidden)
	imC := ramp(128, "StructParent", 1)
	imE := ramp(192, "StructParent", 2)

	page1 := "/Figure <</MCID 0>> BDC q 160 0 0 160 40 150 cm /ImA Do Q EMC\n" +
		"/Artifact BMC q 160 0 0 160 210 150 cm /ImB Do Q EMC\n" +
		"q 80 0 0 80 60 60 cm /ImC Do Q\n" +
		"q 80 0 0 80 260 60 cm /ImE Do Q\n"
	page2 := "q 160 0 0 160 150 150 cm /ImE Do Q\n"

	var pages []*pdfgen.Dict
	for i, pc := range []struct {
		content  string
		xobjects *pdfgen.Dict
	}{
		{page1, d("ImA", imA, "ImB", imB, "ImC", imC, "ImE", imE)},
		{page2, d("ImE", imE)},
	} {
		page := pdf.Indirect(d(
			"Type", n("Page"),
			"MediaBox", a(0, 0, 400, 400),
			"Resources", d("XObject", pc.xobjects),
			"Contents", pdf.MakeIndirect(pdf.Stream([]byte(pc.content))),
			"StructParents", i*3,
		))
		pdf.AddPage(page)
		pages = append(pages, page)
	}

	root := pdf.Indirect(d("Type", n("StructTreeRoot")))
	document := pdf.Indirect(d("Type", n("StructElem"), "S", n("Document"), "P", root))

	figure := func(alt string, kid any) *pdfgen.Dict {
		return pdf.Indirect(d(
			"Type", n("StructElem"), "S", n("Figure"), "P", document, "Pg", pages[0], "Alt", text(alt), "K", kid,
		))
	}

	figA := figure("A ramp, smoothed", 0)
	figC := figure("A ramp drawn once", d("Type", n("OBJR"), "Obj", imC, "Pg", pages[0]))
	figE := figure("A ramp drawn on both pages", d("Type", n("OBJR"), "Obj", imE, "Pg", pages[0]))
	document.Set("K", a(figA, figC, figE))
	root.Set("K", document)
	root.Set("ParentTree", pdf.Indirect(
		d("Nums", a(0, a(figA), 1, figC, 2, figE, 3, a())),
	))
	root.Set("ParentTreeNextKey", 4)

	pdf.Root.Set("StructTreeRoot", root)
	pdf.Root.Set("MarkInfo", d("Marked", true))
	pdf.Root.Set("Lang", text("en-US"))
	pdf.Root.Set("ViewerPreferences", d("DisplayDocTitle", true))
	attachXMP(pdf, xmpPacket(xmpOpts{UAPart: 1}))
	return save(pdf, "1.7")
}
