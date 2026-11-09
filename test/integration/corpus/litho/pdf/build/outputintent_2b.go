package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("outputintent_2b", outputintent2b) }

// outputintent2b builds outputintent_2b.pdf: OutputIntent and ICC destination profiles.
//
// The coverage scan showed ISO 19005-2, 6.2.3 unreached across all four part-2
// and part-3 profiles. It constrains the output intent that establishes a file's
// destination colour space: what the ICC profile itself may declare, how several
// intents must relate, and what a PDF/X intent may carry.
//
// Every fixture in the corpus so far omits `/OutputIntents` entirely, which is
// what keeps the colour clauses reachable elsewhere. This one is the opposite
// case and the only fixture that declares one, so it also exercises the code
// paths that only run when a destination profile is present.
//
// The three tests are independent and are planted together, which is the masking
// probe: a check that reports the first offending intent and stops reports one
// rule where three are due.
//
// | Intent | Construct                                                    | Rule      |
// | ------ | ------------------------------------------------------------ | --------- |
// | A      | ICC profile whose device class is `scnr`                     | `6.2.3/1` |
// | B      | a second intent whose destination profile is another object  | `6.2.3/2` |
// | B      | `/S /GTS_PDFX` carrying `/DestOutputProfileRef`              | `6.2.3/3` |
//
// Intent B's profile is well formed, a `prtr` device class over `RGB `, so it is
// the control for test 1: the verdict lands on the offending profile alone rather
// than on every profile in the array.
//
// The ICC profiles are built here rather than shipped. A destination profile only
// has to be structurally sound for these tests, which read the header, so each is
// a 128-byte header plus an empty tag table. veraPDF parses profile bytes eagerly
// and throws on a placeholder, so the header carries a real size, an `acsp`
// signature and a PCS illuminant.
//
// Test 1 is reached through the device class rather than through the data colour
// space. Both routes fire it, but a profile declaring `Lab ` makes litho report
// ISO 19005-2, 6.2.4.3 t4 against the page's device colour operators while
// veraPDF does not, and a fixture is not the place to carry that. See
// ../FIXTURES.md.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func outputintent2b() ([]byte, error) {
	pdf := pdfgen.New()

	// ICC header offsets used below, per ISO 15076-1: 0 size, 8 version, 12 device
	// class, 16 data colour space, 20 PCS, 36 'acsp' signature, 68 PCS illuminant.
	// iccProfile is a structurally minimal ICC profile with the chosen header
	// fields.
	iccProfile := func(deviceClass, colourSpace string) []byte {
		return iccMinimalProfile(deviceClass, colourSpace, iccVersion21, "litho"[:4])
	}

	destinationProfile := func(deviceClass, colourSpace string) *pdfgen.Stream {
		stream := pdf.Stream(iccProfile(deviceClass, colourSpace))
		stream.Set("N", 3)
		pdf.MakeIndirect(stream)
		return stream
	}

	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type1"),
		"BaseFont", n("/Helvetica"),
		"Encoding", n("WinAnsiEncoding"),
	))

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d("Font", d("F1", font)),
		"Contents", pdf.Stream([]byte(
			"BT /F1 12 Tf 50 700 Td (output intents) Tj ET\n"+
				"0 0 0 rg 50 650 60 30 re f\n",
		)),
	))
	pdf.AddPage(page)

	// Intent A: a scanner profile, which is neither an output nor a monitor profile.
	intentA := pdf.Indirect(d(
		"Type", n("OutputIntent"),
		"S", n("GTS_PDFA1"),
		"OutputConditionIdentifier", text("litho scanner profile"),
		"DestOutputProfile", destinationProfile("scnr", "RGB "),
	))

	// Intent B: a well-formed profile, but a second and distinct one, and a PDF/X
	// intent carrying the reference key that ISO 15930-7 Annex A defines and this
	// clause forbids.
	intentB := pdf.Indirect(d(
		"Type", n("OutputIntent"),
		"S", n("GTS_PDFX"),
		"OutputConditionIdentifier", text("litho printer profile"),
		"DestOutputProfile", destinationProfile("prtr", "RGB "),
		"DestOutputProfileRef", pdf.Indirect(d("CheckSum", text("litho"))),
	))

	pdf.Root.Set("OutputIntents", a(intentA, intentB))

	// Deliberately no /Metadata.
	return save(pdf, "1.7")
}
