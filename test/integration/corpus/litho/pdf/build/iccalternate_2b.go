package build

import (
	"encoding/binary"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("iccalternate_2b", iccalternate2b) }

// iccalternate2b builds iccalternate_2b.pdf: ICCBased spaces whose profiles no colour engine
// can use, each with a CIE-based /Alternate, at PDF/A-2b.
//
// ISO 32000-1, 8.6.5.5, Table 66 has a reader that cannot use an ICCBased
// space's profile use the stream's /Alternate, with the colour values as they
// are, and the device space of /N components only when /Alternate is absent.
// No corpus file reached that: the only unusable profiles carrying an
// /Alternate name the device space of /N, which reads the same either way.
//
// Each profile is a version 2.1 header with an empty tag table, as
// iccbased_4.pdf's are: veraPDF parses it, and ISO 19005-2, 6.2.4.2 t1 accepts
// its version and device class, but it holds no transform, so litho, poppler
// (lcms2) and PDF.js (qcms) all fall back to its /Alternate.
//
// | Draws                                              | Space                          |
// | -------------------------------------------------- | ------------------------------ |
// | a square, `50 20 -30 sc`                           | RGB header, /Alternate Lab     |
// | a square, `0.5 sc`                                 | GRAY header, /Alternate CalGray |
// | a 2x1 image of two Lab samples                     | the RGB-header space           |
// | a 2x1 image of an Indexed space over it            | Indexed, base the RGB-header space |
//
// The RGB-header stream states the Lab ranges in its /Range (Table 66), so the
// components the content gives are in range for both spaces.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func iccalternate2b() ([]byte, error) {
	pdf := pdfgen.New()

	const iccHeaderSize = 128
	const iccVersion21 = 0x02100000
	d50 := []float64{0.9642, 1.0, 0.8249}

	// headerOnlyProfile is a version 2.1 monitor profile of the colour space
	// with no tags.
	headerOnlyProfile := func(colourSpace string) []byte {
		tagTable := binary.BigEndian.AppendUint32(nil, 0)
		header := make([]byte, iccHeaderSize, iccHeaderSize+len(tagTable))
		binary.BigEndian.PutUint32(header[0:4], uint32(iccHeaderSize+len(tagTable))) //nolint:gosec // a 132-byte profile.
		binary.BigEndian.PutUint32(header[8:12], iccVersion21)
		copy(header[12:16], "mntr")
		copy(header[16:20], colourSpace)
		copy(header[20:24], "XYZ ")
		copy(header[36:40], "acsp")
		copy(header[68:80], iccS15(d50...))
		return append(header, tagTable...)
	}

	labRange := []any{-128, 127, -128, 127}
	lab := a(n("Lab"), d("WhitePoint", d50, "Range", a(labRange...)))
	calGray := a(n("CalGray"), d("WhitePoint", a(0.9505, 1, 1.089), "Gamma", 1))

	rgbProfile := pdf.Stream(headerOnlyProfile("RGB "))
	rgbProfile.Set("N", 3)
	rgbProfile.Set("Alternate", lab)
	rgbProfile.Set("Range", a(append([]any{0, 100}, labRange...)...))
	labLike := a(n("ICCBased"), pdf.MakeIndirect(rgbProfile))

	grayProfile := pdf.Stream(headerOnlyProfile("GRAY"))
	grayProfile.Set("N", 1)
	grayProfile.Set("Alternate", calGray)
	grayLike := a(n("ICCBased"), pdf.MakeIndirect(grayProfile))

	image := func(colourSpace *pdfgen.Array, samples []byte) *pdfgen.Stream {
		img := pdf.Stream(samples)
		img.Set("Type", n("XObject"))
		img.Set("Subtype", n("Image"))
		img.Set("Width", 2)
		img.Set("Height", 1)
		img.Set("ColorSpace", colourSpace)
		img.Set("BitsPerComponent", 8)
		return pdf.MakeIndirect(img).(*pdfgen.Stream)
	}

	// L* 70, a* 60, b* -40 and L* 40, a* -50, b* 50, over the /Range above.
	labSamples := []byte{178, 188, 88, 102, 78, 178}
	direct := image(labLike, labSamples)
	indexed := image(a(n("Indexed"), labLike, 1, pdfgen.String(labSamples)), []byte{1, 0})

	page := d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 100, 100),
		"Resources", d(
			"ColorSpace", d("C0", labLike, "C1", grayLike),
			"XObject", d("Im0", direct, "Im1", indexed),
		),
		"Contents", pdf.Stream([]byte(
			"/C0 cs 50 20 -30 sc 10 55 35 35 re f\n"+
				"/C1 cs 0.5 sc 55 55 35 35 re f\n"+
				"q 35 0 0 35 10 10 cm /Im0 Do Q\n"+
				"q 35 0 0 35 55 10 cm /Im1 Do Q\n",
		)),
	)
	pdf.AddPage(pdf.Indirect(page))

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))
	return save(pdf, "1.7")
}
