package build

import (
	"encoding/binary"
	"fmt"
	"unicode/utf16"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("imgxmp_4", imgxmp4) }

// imgxmp4 builds imgxmp_4.pdf: image XObject dictionaries and XMP packets for PDF/A-4.
//
// Two ISO 19005-4 clusters were unreached by the rest of the corpus. 6.2.7.1
// governs the image XObject dictionary, and `complex_4.pdf` touches only its
// `/Interpolate` test. 6.7.2.1 governs the XMP packet, and nothing reached
// anything past the "catalog has a metadata stream" test.
//
// Both clusters need sibling objects rather than one loaded object.
//
// The four XMP tests all scope to veraPDF's `XMPPackage`, so a single malformed
// packet would cascade: a packet that is not valid UTF-8 does not parse either,
// which masks the header-attribute tests and takes the identification schema down
// with it. A PDF may carry a `/Metadata` stream on the catalog and on every page,
// and veraPDF models each one as its own `XMPPackage`, so each defect gets its own
// packet on its own page. That isolation is the point of the fixture. The catalog
// packet stays conforming, because the document has to keep declaring PDF/A-4.
//
// Page-level packets are also the only packets the save leaves alone, as
// pikepdf, which first wrote the committed file, did: it reserialises the catalog
// packet through its own XMP writer, which repairs a `bytes=` attribute, an `encoding=` attribute, broken
// markup and a UTF-16 encoding alike. Page-level metadata streams pass through
// untouched.
//
// The images follow the same rule: four sibling XObjects, one defect each, so a
// check that reports the first offending image and stops reports one rule where
// four are due. Every image uses a CalRGB colour space and every image mask is
// painted through an explicit CalGray fill colour, so ISO 19005-4, 6.2.4.3 stays
// out of the way of the tests this fixture is about. The file declares no font for
// the same reason.
//
// `ImMask` is one image mask reached two ways: drawn directly and named as the
// `/Mask` of `ImBase`. veraPDF only reads `/BitsPerComponent` on an image mask it
// reaches through another image's `/Mask`, and litho only reads it on an image
// mask drawn from the page resources, so one object serving both roles carries the
// single defect both models can see.
//
// Two tests of the two clauses stay out. `6.2.7.1/3` is `/Interpolate`, which
// `complex_4.pdf` already covers and which every image here carries as false, so
// it is a control rather than a gap. `6.7.2.1/1` needs the catalog to have no
// metadata stream, which would take the PDF/A-4 declaration with it and silence
// everything else the fixture is for.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func imgxmp4() ([]byte, error) {
	pdf := pdfgen.New()

	// The CIE D65 white point, in the CIE-based colour spaces of ISO 32000-2, 8.6.5.
	// Both keep the images and the image masks clear of the device colour space
	// tests, which have their own fixtures.
	whitePoint := a(0.9505, 1.0, 1.089)
	calRGB := a(n("CalRGB"), d("WhitePoint", whitePoint))
	calGray := a(n("CalGray"), d("WhitePoint", whitePoint))

	// 2x2 CalRGB pixels. The bytes are shared by every image below: none of the
	// defects is in the sample data.
	pixels := zlibCompress([]byte{255, 0, 0, 0, 255, 0, 0, 0, 255, 255, 255, 0})

	// 2x2 stencil, one bit per sample, rows padded to a byte.
	stencil := []byte("\x80\x00")

	// image builds a 2x2 CalRGB image XObject that conforms unless extra
	// breaks it.
	image := func(extra ...any) *pdfgen.Stream {
		img := pdf.Stream(pixels)
		img.Set("Type", n("XObject"))
		img.Set("Subtype", n("Image"))
		img.Set("Width", 2)
		img.Set("Height", 2)
		img.Set("ColorSpace", calRGB)
		img.Set("BitsPerComponent", 8)
		img.Set("Filter", n("FlateDecode"))
		// ISO 19005-4, 6.2.7.1 t3 allows the key when its value is false. Setting it
		// explicitly on every image keeps that test a control rather than a gap.
		img.Set("Interpolate", false)
		for i := 0; i < len(extra); i += 2 {
			img.Set(extra[i].(string), extra[i+1])
		}
		pdf.MakeIndirect(img)
		return img
	}

	// imageMask builds a 2x2 stencil mask declaring the given
	// /BitsPerComponent.
	imageMask := func(bits int) *pdfgen.Stream {
		img := pdf.Stream(stencil)
		img.Set("Type", n("XObject"))
		img.Set("Subtype", n("Image"))
		img.Set("Width", 2)
		img.Set("Height", 2)
		img.Set("ImageMask", true)
		img.Set("BitsPerComponent", bits)
		img.Set("Decode", a(0, 1))
		img.Set("Interpolate", false)
		pdf.MakeIndirect(img)
		return img
	}

	// t1: an /Alternates array. Its one alternate conforms in every respect, so the
	// report belongs to the image that carries the key, not to what it points at.
	imAlt := image(
		"Alternates", a(d("Image", image(), "DefaultForPrinting", false)),
	)

	// t2: an /OPI dictionary, the open prepress interface proxy for an image held
	// outside the file. ISO 19005-4, 6.2.7.1 forbids the key outright.
	opi := d()
	opi.Set("/2.0", d(
		"Type", n("OPI"),
		"Version", 2.0,
		"F", text("scan.tif"),
		"Size", a(2, 2),
		"CropRect", a(0, 2, 2, 0),
	))
	imOPI := image("OPI", opi)

	// t4: /BitsPerComponent 3, which is in no row of ISO 32000-2, Table 87.
	imBPC := image("BitsPerComponent", 3)

	// t5: an image mask declaring 2 bits per sample where only 1 is defined. It is
	// drawn from the page resources and named as the /Mask of ImBase, because the
	// two validators reach an image mask by different routes. 2 is a legal value for
	// a regular image, so the defect stays inside t5 rather than also failing t4.
	imMask := imageMask(2)
	imBase := image("Mask", imMask)

	// Controls. A conforming image and a conforming image mask stand beside the
	// four defects, so a check that condemns every image once it has seen one bad
	// one reports them too.
	imOK := image()
	imMaskOK := imageMask(1)

	resources := d(
		"ColorSpace", d("CsGray", calGray),
		"XObject", d(
			"ImAlt", imAlt,
			"ImOPI", imOPI,
			"ImBPC", imBPC,
			"ImMask", imMask,
			"ImBase", imBase,
			"ImOK", imOK,
			"ImMaskOK", imMaskOK,
		),
	)

	// An image mask paints the current fill colour through its samples, so the fill
	// colour has to be calibrated too or the mask drags 6.2.4.3 in with it.
	imagePageContent := []byte(`/CsGray cs 0 sc
q 40 0 0 40 40 500 cm /ImAlt Do Q
q 40 0 0 40 100 500 cm /ImOPI Do Q
q 40 0 0 40 160 500 cm /ImBPC Do Q
q 40 0 0 40 220 500 cm /ImMask Do Q
q 40 0 0 40 280 500 cm /ImBase Do Q
q 40 0 0 40 340 500 cm /ImOK Do Q
q 40 0 0 40 400 500 cm /ImMaskOK Do Q
`)

	// Packet header variants. veraPDF reads the attributes straight out of the
	// <?xpacket ...?> processing instruction, so the defect is in the header alone
	// and the packet body stays well-formed and parseable.
	const packetID = `id="W5M0MpCehiHzreSzNTczkc9d"`
	const headerOK = "<?xpacket begin=\"\xef\xbb\xbf\" " + packetID + "?>"
	const headerBytes = "<?xpacket begin=\"\xef\xbb\xbf\" " + packetID + " bytes=\"1024\"?>"
	const headerEncoding = "<?xpacket begin=\"\xef\xbb\xbf\" " + packetID + " encoding=\"UTF-8\"?>"
	const trailer = `<?xpacket end="w"?>`

	const body = `<x:xmpmeta xmlns:x="adobe:ns:meta/">
 <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description rdf:about="" xmlns:dc="http://purl.org/dc/elements/1.1/">
   <dc:title><rdf:Alt><rdf:li xml:lang="x-default">%s</rdf:li></rdf:Alt></dc:title>
  </rdf:Description>
 </rdf:RDF>
</x:xmpmeta>`

	// A start tag whose end tag differs in case. XML is case sensitive, so the
	// packet is not well-formed and has no valid serialization.
	const bodyMalformed = `<x:xmpmeta xmlns:x="adobe:ns:meta/">
 <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description rdf:about="" xmlns:dc="http://purl.org/dc/elements/1.1/">
   <dc:title>page metadata</dc:Title>
  </rdf:Description>
 </rdf:RDF>
</x:xmpmeta>`

	// pagePacket serialises one page-level XMP packet.
	//
	// No page packet carries a pdfaid property: the identification schema belongs
	// to the catalog packet, which is the only one veraPDF treats as the main XMP
	// package.
	pagePacket := func(header, bodyText, encoding string) []byte {
		if bodyText == "" {
			bodyText = fmt.Sprintf(body, "page metadata")
		}
		txt := fmt.Sprintf("%s\n%s\n%s\n", header, bodyText, trailer)
		if encoding == "utf-16" {
			units := utf16.Encode([]rune("\xef\xbb\xbf" + txt))
			out := make([]byte, 0, 2*len(units))
			for _, u := range units {
				out = binary.BigEndian.AppendUint16(out, u)
			}
			return out
		}
		return []byte(txt)
	}

	// metadataStream wraps an XMP packet in a metadata stream dictionary.
	metadataStream := func(packet []byte) *pdfgen.Stream {
		stream := pdf.Stream(packet)
		stream.Set("Type", n("Metadata"))
		stream.Set("Subtype", n("XML"))
		pdf.MakeIndirect(stream)
		return stream
	}

	// One page per defect. Page 1 carries the images and a conforming packet, so its
	// silence proves the four below report per packet rather than once per document.
	type pageSpec struct {
		content   []byte
		packet    []byte
		resources *pdfgen.Dict
	}
	pages := []pageSpec{
		{imagePageContent, pagePacket(headerOK, "", "utf-8"), resources},
		{[]byte(""), pagePacket(headerBytes, "", "utf-8"), d()},
		{[]byte(""), pagePacket(headerEncoding, "", "utf-8"), d()},
		{[]byte(""), pagePacket(headerOK, bodyMalformed, "utf-8"), d()},
		{[]byte(""), pagePacket(headerOK, "", "utf-16"), d()},
	}

	for _, p := range pages {
		page := d(
			"Type", n("Page"),
			"MediaBox", a(0, 0, 612, 792),
			"Resources", p.resources,
			"Contents", pdf.Stream(p.content),
			"Metadata", metadataStream(p.packet),
		)
		pdf.AddPage(pdf.Indirect(page))
	}

	// ISO 19005-4, 6.7.3 requires pdfaid:part 4 and pdfaid:rev 2020. Both are
	// correct here: the catalog packet is a control, not a defect.
	attachXMP(pdf, xmpPacket(xmpOpts{Part: 4, Rev: 2020}))

	return save(pdf, "2.0")
}
