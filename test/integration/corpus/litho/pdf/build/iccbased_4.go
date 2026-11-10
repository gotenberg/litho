package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("iccbased_4", iccbased4) }

// iccbased4 builds iccbased_4.pdf: ICCBased colour spaces under PDF/A-4.
//
// The coverage scan showed ISO 19005-4, 6.2.4.2 unreached. It judges the ICC
// profile an ICCBased colour space carries, and the two ways such a space may
// conflict with the file's destination profile.
//
// Four ICCBased spaces are drawn on one page, three defective and one
// conforming, which is the masking probe: a check that reports the first
// offending colour space and stops reports one rule where three are due.
//
// | Space | Construct                                              | Rule        |
// | ----- | ------------------------------------------------------ | ----------- |
// | CS0   | profile version 5.0                                    | `6.2.4.2/1` |
// | CS1   | CMYK, stroked with `/OP true` and `/OPM 1`             | `6.2.4.2/2` |
// | CS2   | the OutputIntent's own destination profile             | `6.2.4.2/3` |
// | CS3   | RGB, `mntr`, version 2.1, unrelated to the OutputIntent | silent     |
//
// The three defects are kept apart deliberately, because two of them are easy to
// trigger by accident. Test 3 compares the ICCBased profile against the
// OutputIntent's by object and by MD5, so any CMYK space carrying the same bytes
// fires it: CS1 therefore uses a profile that differs from the destination one,
// or it would report test 3 as well and the overprint verdict would be masked.
// CS3 is RGB rather than CMYK, which keeps it outside test 3 entirely.
//
// The profiles are built here rather than shipped. These tests read the header,
// so each is a 128-byte header plus an empty tag table, carrying a real size, an
// `acsp` signature and a PCS illuminant, because veraPDF parses profile bytes
// eagerly and throws on a placeholder.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func iccbased4() ([]byte, error) {
	pdf := pdfgen.New()

	const iccVersion21 = 0x02100000
	const iccVersion50 = 0x05000000

	// iccProfile is a structurally minimal ICC profile with the chosen header
	// fields.
	//
	// `tag` is the preferred CMM field. It carries no meaning for these tests and
	// exists only to make otherwise identical profiles differ byte for byte,
	// which is what keeps test 3 off the spaces that are not about it.
	iccProfile := iccMinimalProfile

	profileStream := func(data []byte, components int) *pdfgen.Stream {
		stream := pdf.Stream(data)
		stream.Set("N", components)
		return pdf.MakeIndirect(stream).(*pdfgen.Stream)
	}

	iccBased := func(profile *pdfgen.Stream) pdfgen.Object {
		return pdf.MakeIndirect(a(n("ICCBased"), profile))
	}

	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type1"),
		"BaseFont", n("/Helvetica"),
		"Encoding", n("WinAnsiEncoding"),
	))

	// The destination profile. Test 3 measures the other spaces against this one.
	destination := profileStream(iccProfile("prtr", "CMYK", iccVersion21, "OUTI"), 4)
	pdf.Root.Set("OutputIntents", a(
		pdf.Indirect(d(
			"Type", n("OutputIntent"),
			"S", n("GTS_PDFA1"),
			"OutputConditionIdentifier", text("litho destination"),
			"DestOutputProfile", destination,
		)),
	))

	colourSpaces := d(
		// Version 5.0, which the clause caps below (t1).
		"CS0", iccBased(profileStream(iccProfile("prtr", "CMYK", iccVersion50, "V5PR"), 4)),
		// A valid CMYK profile, distinct from the destination, stroked under an
		// overprint mode of 1 (t2).
		"CS1", iccBased(profileStream(iccProfile("mntr", "CMYK", iccVersion21, "ALT1"), 4)),
		// The destination profile itself (t3).
		"CS2", iccBased(destination),
		// Conforming and unrelated: RGB, so test 3 does not reach it.
		"CS3", iccBased(profileStream(iccProfile("mntr", "RGB ", iccVersion21, "RGBP"), 3)),
	)

	overprint := pdf.Indirect(d("Type", n("ExtGState"), "OP", true, "op", true, "OPM", 1))

	content := "BT /F1 12 Tf 50 740 Td (ICCBased colour spaces) Tj ET\n" +
		"q /CS0 cs 0.1 0.2 0.3 0.4 scn 50 700 80 30 re f Q\n" +
		// Only CS1 is stroked under the overprint state, so the mode-1 verdict
		// belongs to it alone.
		"q /GS0 gs /CS1 CS 0.1 0.2 0.3 0.4 SCN 2 w 50 660 80 30 re S Q\n" +
		"q /CS2 cs 0.1 0.2 0.3 0.4 scn 50 620 80 30 re f Q\n" +
		"q /CS3 cs 0.2 0.4 0.6 scn 50 580 80 30 re f Q\n"

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d(
			"Font", d("F1", font),
			"ColorSpace", colourSpaces,
			"ExtGState", d("GS0", overprint),
		),
		"Contents", pdf.Stream([]byte(content)),
	))
	pdf.AddPage(page)

	// Deliberately no /Metadata.
	return save(pdf, "2.0")
}
