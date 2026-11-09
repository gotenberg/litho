package build

import (
	"encoding/binary"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("formshare_ua1", formshareUa1) }

// formshareUa1 builds formshare_ua1.pdf: marked forms a content stream two pages share draws.
//
// ISO 14289-1, 7.20 has a form XObject whose content holds MCIDs referenced once:
// a form's /StructParents names the one parent tree entry its marked-content
// sequences resolve through (ISO 32000-1, Table 95 and 14.7.4.4). veraPDF
// 1.30.2 states test 2 on the form object a Do builds, and parses a content
// stream once for the document: a Do in a /Contents two pages share is one
// reference, whether the second page reads it under the same resources or
// under resources of its own. litho read the content of every page again and stated test 2 for a
// marked form the shared content draws once. No corpus file shares a content
// stream drawing a marked form at PDF/UA-1, so this fixture plants both
// readings, each pair of pages drawing a marked form of its own:
//
// | Page | Path                                                       | Answers at PDF/UA-1 |
// | ---- | ---------------------------------------------------------- | ------------------- |
// | 1, 2 | shared content, `/F Do`, page 2 under resources of its own | nothing             |
// | 3, 4 | shared content, `/F Do`, one resource dictionary for both  | nothing             |
//
// Every page is a must-not-fire control. Page 2's resources name the same form
// as page 1's and add an ExtGState, so the two pages read the stream under
// different resources.
//
// A CMYK OutputIntent covers the DeviceGray every stream paints in.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func formshareUa1() ([]byte, error) {
	pdf := pdfgen.New()

	const (
		iccHeaderSize = 128
		iccVersion21  = 0x02100000
	)

	// iccProfile is a structurally minimal ICC profile, as patternglyph_2b.go
	// builds it.
	iccProfile := func(deviceClass, colourSpace, tag string) []byte {
		tagTable := binary.BigEndian.AppendUint32(nil, 0)
		header := make([]byte, iccHeaderSize)
		binary.BigEndian.PutUint32(header[0:4], uint32(iccHeaderSize+len(tagTable))) //nolint:gosec // fixture profiles are small.
		copy(header[4:8], tag)
		binary.BigEndian.PutUint32(header[8:12], iccVersion21)
		copy(header[12:16], deviceClass)
		copy(header[16:20], colourSpace)
		copy(header[20:24], "XYZ ")
		copy(header[36:40], "acsp")
		binary.BigEndian.PutUint32(header[68:72], 0x0000F6D6)
		binary.BigEndian.PutUint32(header[72:76], 0x00010000)
		binary.BigEndian.PutUint32(header[76:80], 0x0000D32D)
		return append(header, tagTable...)
	}

	destination := pdf.Stream(iccProfile("prtr", "CMYK", "OUTI"))
	destination.Set("N", 4)
	pdf.Root.Set("OutputIntents", a(
		pdf.Indirect(d(
			"Type", n("OutputIntent"),
			"S", n("GTS_PDFA1"),
			"OutputConditionIdentifier", text("litho destination"),
			"DestOutputProfile", pdf.MakeIndirect(destination),
		)),
	))

	// form is a form XObject with empty resources and the extra keys given.
	form := func(content string, keys ...any) *pdfgen.Stream {
		xobj := pdf.Stream([]byte(content))
		xobj.Set("Type", n("XObject"))
		xobj.Set("Subtype", n("Form"))
		xobj.Set("BBox", a(0, 0, 20, 20))
		xobj.Set("Resources", d())
		for i := 0; i < len(keys); i += 2 {
			xobj.Set(keys[i].(string), keys[i+1])
		}
		pdf.MakeIndirect(xobj)
		return xobj
	}

	addPage := func(contents pdfgen.Object, resources *pdfgen.Dict) {
		page := d("Type", n("Page"), "MediaBox", a(0, 0, 200, 200), "Resources", resources, "Contents", contents)
		pdf.AddPage(pdf.Indirect(page))
	}

	// sharedPair is two pages whose /Contents is one stream drawing xobject
	// under name.
	sharedPair := func(name string, xobject *pdfgen.Stream, sameResources bool) {
		contents := pdf.MakeIndirect(pdf.Stream([]byte("/" + name + " Do\n")))
		first := pdf.Indirect(d("XObject", d("/"+name, xobject)))
		addPage(contents, first)
		if sameResources {
			addPage(contents, first)
		} else {
			own := d("XObject", d("/"+name, xobject), "ExtGState", d("G0", d("Type", n("ExtGState"))))
			addPage(contents, own)
		}
	}

	const marked = "/P <</MCID 0>> BDC 0 g 0 0 10 10 re f EMC\n"

	sharedPair("F", form(marked, "StructParents", 0), false)
	sharedPair("F", form(marked, "StructParents", 0), true)

	attachXMP(pdf, xmpPacket(xmpOpts{UAPart: 1}))
	return save(pdf, "1.7")
}
