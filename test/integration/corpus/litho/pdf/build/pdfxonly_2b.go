package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("pdfxonly_2b", pdfxonly2b) }

// pdfxonly2b builds pdfxonly_2b.pdf: a PDF/X output intent and no PDF/A one, at PDF/A-2b.
//
// The catalog's sole output intent is a GTS_PDFX intent over a version 2.1 CMYK
// printer profile, and the page paints a DeviceCMYK square and a DeviceRGB one.
// Only a GTS_PDFA1 intent covers device colour under PDF/A (ISO 19005-1,
// 6.2.3.3; ISO 19005-2/3/4, 6.2.4.3), so every PDF/A level fails the device
// colour rule while the intent itself is well formed.
//
// The probe is what remediation does with the PDF/X intent. ISO 32000-1, 14.11.5
// admits several intents, so a repair adds a GTS_PDFA1 intent beside it; ISO
// 19005-2, 6.2.3 t2 asks every intent carrying /DestOutputProfile to name one
// indirect object, and the PDF/X profile is one PDF/A admits for CMYK, so the
// PDF/A intent can name it and leave the PDF/X intent as it is. A repair that
// replaces the whole /OutputIntents array drops the PDF/X intent.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func pdfxonly2b() ([]byte, error) {
	pdf := pdfgen.New()

	profile := pdf.Stream(iccCMYKPrinterProfile())
	profile.Set("N", 4)

	pdf.Root.Set("OutputIntents", a(
		pdf.Indirect(d(
			"Type", n("OutputIntent"),
			"S", n("GTS_PDFX"),
			"OutputConditionIdentifier", text("litho CMYK printer"),
			"RegistryName", text("http://www.color.org"),
			"DestOutputProfile", pdf.MakeIndirect(profile),
		)),
	))

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 200, 200),
		"Resources", d(),
		"Contents", pdf.MakeIndirect(
			pdf.Stream([]byte("0 0.6 1 0 k 20 20 60 60 re f\n0.8 0.1 0.1 rg 120 120 60 60 re f\n")),
		),
	))
	pdf.AddPage(page)

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))
	return save(pdf, "1.7", noFixMetadataVersion)
}
