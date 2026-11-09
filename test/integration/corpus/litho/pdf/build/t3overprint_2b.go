package build

import (
	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("t3overprint_2b", t3overprint2b) }

// t3overprint2b builds t3overprint_2b.pdf: Type 3 glyphs shown under overprint, at PDF/A-2b.
//
// A Type 3 glyph description runs under the graphics state of the text that
// shows it (ISO 32000-1, 9.6.5), overprint parameters included (8.6.7).
// Converting colour judges what overprinting a painting prints: a colour
// converted into DeviceCMYK under overprint mode 1 stops painting the inks of
// its zero components, which an /OPM 0 restores, and a DeviceCMYK colour
// converted out of it paints them. litho once judged the colour the text is
// shown in, never the glyph's own. No corpus file shows a Type 3 glyph under
// overprint, so no cell reached the judgement.
//
// The page shows three glyphs of the font T3 under GS0, /OP true and /OPM 1:
//
// | Code | Opens with | Paints                                       |
// | ---- | ---------- | -------------------------------------------- |
// | `A`  | d0         | 0.5 g: grey, black alone once in DeviceCMYK  |
// | `B`  | d0         | 0 1 1 0 k: leaves cyan and black under /OPM 1 |
// | `C`  | d1         | the text's colour, 0 0 0 1 k                 |
//
// Converted to RGB, B and C paint every ink where they left two or three, and
// both are reported; converted to litho's CMYK printer profile, both move their
// zero components, and are reported too. Before, litho judged the show once,
// in the text's black, and reported one painting.
//
// The CMYK output intent covers DeviceGray and DeviceCMYK, and overprint mode 1
// over DeviceCMYK is allowed at PDF/A-2b (ISO 19005-2, 6.2.4.2 restricts it
// over ICCBased CMYK alone).
//
// See ../FIXTURES.md for the authoritative expected firing set.
func t3overprint2b() ([]byte, error) {
	pdf := pdfgen.New()

	destination := pdf.Stream(iccCMYKPrinterProfile())
	destination.Set("N", 4)
	pdf.Root.Set("OutputIntents", a(
		pdf.Indirect(d(
			"Type", n("OutputIntent"),
			"S", n("GTS_PDFA1"),
			"OutputConditionIdentifier", text("litho CMYK printer"),
			"DestOutputProfile", pdf.MakeIndirect(destination),
		)),
	))

	font := type3Font(
		pdf,
		[]type3Proc{
			{"A", 1000, "0.5 g 0 0 1000 1000 re f"},
			{"B", 1000, "0 1 1 0 k 0 0 1000 1000 re f"},
		},
		[]any{1000, 1000, 1000},
		65,
	)
	font.Get("CharProcs").(*pdfgen.Dict).Set("C", pdf.Stream([]byte("1000 0 0 0 1000 1000 d1\n0 0 1000 1000 re f\n")))
	font.Get("Encoding").(*pdfgen.Dict).Get("Differences").(*pdfgen.Array).Append(n("C"))
	font.Set("LastChar", 67)

	resources := d(
		"ExtGState", d("GS0", pdf.Indirect(d("Type", n("ExtGState"), "OP", true, "op", true, "OPM", 1))),
		"Font", d("T3", pdf.Indirect(font)),
	)
	page := d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 400, 400),
		"Resources", resources,
		"Contents", pdf.MakeIndirect(pdf.Stream([]byte("/GS0 gs 0 0 0 1 k BT /T3 48 Tf 50 300 Td (ABC) Tj ET\n"))),
	)
	pdf.AddPage(pdf.Indirect(page))

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))
	return save(pdf, "1.7")
}
