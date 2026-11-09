package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("pagegroup_2b", pagegroup2b) }

// pagegroup2b builds pagegroup_2b.pdf: page transparency groups and a page XMP packet.
//
// ISO 19005-2, 6.2.10 t2 asks every page that contains transparency, in a
// document without a PDF/A OutputIntent, for a /Group whose /CS gives the
// default blending colour space. The file declares no OutputIntent, and four
// pages paint through a graphics state with constant alpha 0.5, in an ICCBased
// space so no page uses a device colour space of its own:
//
// | Page | /Group                                | Rule at PDF/A-2b |
// | ---- | ------------------------------------- | ---------------- |
// | 1    | none                                  | `6.2.10/2`       |
// | 2    | `/S /Transparency /CS [/ICCBased ..]` | silent           |
// | 3    | `/S /Transparency`, no `/CS`          | `6.2.10/2`       |
// | 4    | `/S /Transparency /CS /DeviceRGB`     | `6.2.4.3/2`      |
// | 5    | `/S /Transparency`, no `/CS`, opaque  | silent           |
// | 6    | `/S /Transparency /CS /CS0`           | `6.2.10/2`       |
// | 7    | the same, painting an isolated group  | `6.2.10/2`       |
//
// Page 2 is the control: its ICCBased blending space answers the rule, and a
// repair of pages 1 and 3 has to leave it in place. Page 4 blends in DeviceRGB,
// which ISO 19005-2, 6.2.4.3 t2 allows only under an RGB OutputIntent.
//
// Pages 5 and 6 are the two halves of how veraPDF reads the rule. It decides a
// page contains transparency from what the page draws, never from its own
// /Group, so page 5, which paints the same square without the graphics state,
// needs no blending space. It reads the group's /CS with no resources, so the
// /CS0 that pages 6 and 7 name, the ICCBased entry of their own /ColorSpace,
// is no blending space at all. Page 7 draws its transparency as a form with an
// isolated transparency group rather than through constant alpha.
//
// At PDF/A-1b every one of those groups is a transparency group ISO 19005-1,
// 6.4 t3 forbids, and page 4's DeviceRGB blending space fails 6.2.3.3 t1 as
// well: a repair that removes the groups must not synthesize them again.
//
// Page 1 also carries its own XMP packet under /Metadata. ISO 19005-2,
// 6.6.2.3.1 constrains the properties of every metadata stream in the file, not
// only the catalog's: photoshop:History is no property of the XMP 2005
// Photoshop schema, which fails t1 and t2, and dc:format is written as a
// language alternative where the schema defines MIMEType text, which fails t2.
// xmp:CreatorTool is a predefined text property and stays silent. Part 1 judges
// the catalog packet alone, so the page packet is silent at PDF/A-1b.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func pagegroup2b() ([]byte, error) {
	pdf := pdfgen.New()

	// iccProfile is a structurally minimal ICC profile veraPDF will parse.
	//
	// veraPDF reads profile bytes eagerly and throws on a placeholder, so the
	// header carries a real size, an `acsp` signature and a PCS illuminant.
	iccProfile := func(colourSpace string, components int, tag string) *pdfgen.Stream {
		body := iccMinimalProfile("mntr", colourSpace, iccVersion21, tag)
		stream := pdf.Stream(body)
		stream.Set("N", components)
		pdf.MakeIndirect(stream)
		return stream
	}

	iccbased := pdf.MakeIndirect(a(n("ICCBased"), iccProfile("RGB ", 3, "ICCB")))

	pagePacket := []byte("<?xpacket begin=\"\xef\xbb\xbf\" id=\"W5M0MpCehiHzreSzNTczkc9d\"?>\n" +
		"<x:xmpmeta xmlns:x=\"adobe:ns:meta/\">\n" +
		" <rdf:RDF xmlns:rdf=\"http://www.w3.org/1999/02/22-rdf-syntax-ns#\">\n" +
		"  <rdf:Description rdf:about=\"\"\n" +
		"    xmlns:photoshop=\"http://ns.adobe.com/photoshop/1.0/\"\n" +
		"    xmlns:xmp=\"http://ns.adobe.com/xap/1.0/\"\n" +
		"    xmlns:dc=\"http://purl.org/dc/elements/1.1/\">\n" +
		"   <photoshop:History>Scanned, then cropped</photoshop:History>\n" +
		"   <dc:format><rdf:Alt><rdf:li xml:lang=\"x-default\">image/tiff</rdf:li></rdf:Alt></dc:format>\n" +
		"   <xmp:CreatorTool>litho corpus scanner</xmp:CreatorTool>\n" +
		"  </rdf:Description>\n" +
		" </rdf:RDF>\n" +
		"</x:xmpmeta>\n" +
		"<?xpacket end=\"w\"?>\n")

	// Every page paints the same square through constant alpha, in the ICCBased
	// space, so each page contains transparency and none uses a device space.
	const content = "/GS0 gs /CS0 cs 0.2 0.4 0.6 scn 60 60 200 200 re f\n"
	const opaque = "/CS0 cs 0.2 0.4 0.6 scn 60 60 200 200 re f\n"

	addPage := func(group *pdfgen.Dict, metadata *pdfgen.Stream, content string, xobjects *pdfgen.Dict) {
		resources := d(
			"ExtGState", d("GS0", d("Type", n("ExtGState"), "ca", 0.5)),
			"ColorSpace", d("CS0", iccbased),
		)
		page := d(
			"Type", n("Page"),
			"MediaBox", a(0, 0, 612, 792),
			"Resources", resources,
			"Contents", pdf.Stream([]byte(content)),
		)
		if xobjects != nil {
			resources.Set("XObject", xobjects)
		}
		if group != nil {
			page.Set("Group", group)
		}
		if metadata != nil {
			page.Set("Metadata", metadata)
		}
		pdf.AddPage(pdf.Indirect(page))
	}

	pageMeta := pdf.Stream(pagePacket)
	pageMeta.Set("Type", n("Metadata"))
	pageMeta.Set("Subtype", n("XML"))

	addPage(nil, pdf.MakeIndirect(pageMeta).(*pdfgen.Stream), content, nil)
	addPage(d("Type", n("Group"), "S", n("Transparency"), "CS", iccbased), nil, content, nil)
	addPage(d("Type", n("Group"), "S", n("Transparency")), nil, content, nil)
	addPage(d("Type", n("Group"), "S", n("Transparency"), "CS", n("DeviceRGB")), nil, content, nil)
	addPage(d("Type", n("Group"), "S", n("Transparency")), nil, opaque, nil)
	addPage(d("Type", n("Group"), "S", n("Transparency"), "CS", n("/CS0")), nil, content, nil)

	isolated := pdf.Stream([]byte(opaque))
	isolated.Set("Type", n("XObject"))
	isolated.Set("Subtype", n("Form"))
	isolated.Set("BBox", a(0, 0, 612, 792))
	isolated.Set("Resources", d("ColorSpace", d("CS0", iccbased)))
	isolated.Set("Group", d("Type", n("Group"), "S", n("Transparency"), "CS", iccbased, "I", true))
	addPage(
		d("Type", n("Group"), "S", n("Transparency"), "CS", n("/CS0")),
		nil,
		"/Fm0 Do\n",
		d("Fm0", pdf.MakeIndirect(isolated)),
	)

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))
	return save(pdf, "1.7")
}
