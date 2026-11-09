package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("pdfxnoprofile_2b", pdfxnoprofile2b) }

// pdfxnoprofile2b builds pdfxnoprofile_2b.pdf: a PDF/A output intent naming no profile beside
// a PDF/X one, at PDF/A-2b.
//
// The catalog lists a GTS_PDFA1 intent carrying an /OutputConditionIdentifier
// and no /DestOutputProfile, then a GTS_PDFX intent over a version 2.1 CMYK
// printer profile. The page paints DeviceRGB. A GTS_PDFA1 intent covers device
// colour through its destination profile (ISO 19005-2, 6.2.4.3), so this one
// covers none, and the PDF/X intent never does: every PDF/A level fails the
// device colour rule.
//
// The probe is the repair of the PDF/A intent beside the PDF/X one. The PDF/X
// profile is a CMYK one, which does not cover DeviceRGB, so the repair needs an
// RGB profile. The profile-less GTS_PDFA1 intent is replaced, the PDF/X intent
// is kept (ISO 32000-1, 14.11.5), and ISO 19005-2, 6.2.3 t2 asks both intents to
// name the new profile object.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func pdfxnoprofile2b() ([]byte, error) {
	pdf := pdfgen.New()

	profile := pdf.Stream(iccCMYKPrinterProfile())
	profile.Set("N", 4)

	pdf.Root.Set("OutputIntents", a(
		pdf.Indirect(d(
			"Type", n("OutputIntent"),
			"S", n("GTS_PDFA1"),
			"OutputConditionIdentifier", text("sRGB IEC61966-2.1"),
			"RegistryName", text("http://www.color.org"),
		)),
		pdf.Indirect(d(
			"Type", n("OutputIntent"),
			"S", n("GTS_PDFX"),
			"OutputConditionIdentifier", text("litho CMYK printer"),
			"DestOutputProfile", pdf.MakeIndirect(profile),
		)),
	))

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 200, 200),
		"Resources", d(),
		"Contents", pdf.MakeIndirect(pdf.Stream([]byte("0.8 0.1 0.1 rg 20 20 60 60 re f\n"))),
	))
	pdf.AddPage(page)

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))
	return save(pdf, "1.7", noFixMetadataVersion)
}
