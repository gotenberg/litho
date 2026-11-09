package build

import (
	"bytes"
	"encoding/hex"
	"fmt"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("inlinelzw_2b", inlinelzw2b) }

// inlinelzw2b builds inlinelzw_2b.pdf: inline images whose filters the parts of ISO 19005
// forbid, in every kind of stream veraPDF parses, for the repair that re-encodes
// them inside their content stream.
//
// ISO 19005-1, 6.1.10 forbids LZWDecode in an inline image's filters, and ISO
// 19005-2/3, 6.1.10 and ISO 19005-4, 6.1.9 forbid LZWDecode, Crypt and every
// value outside ISO 32000-1, Table 6. veraPDF judges each inline image as an
// image of its own, so each forbidden image here fails the rule once:
//
//	page   P1 writes /F /LZW with /DP << /EarlyChange 0 >> and /L, its data
//	       length. P2 writes /F [/AHx /LZW /RL]: the repair decodes through the
//	       LZW and keeps the RunLength after it. P3 writes /F /AHx, which every
//	       part permits, and is the control.
//	Fm     a form XObject the page draws, whose image writes /Filter /LZWDecode,
//	       the full key and name.
//	T3     the procedure of glyph a of a Type 3 font the page shows, whose image
//	       writes /F [/LZW].
//	Pt     the cell of a tiling pattern the page fills with, whose image writes
//	       /F /LZW.
//	Ap     the normal appearance of a Square annotation, whose image writes
//	       /F /Lzw: a name outside Table 6, which parts 2 to 4 forbid and part 1
//	       does not.
//
// Every image draws the same eight grey samples, and its data is real LZW, so a
// repair that moves a pixel shows.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func inlinelzw2b() ([]byte, error) {
	pdf := pdfgen.New()

	samples := []byte{0x00, 0x40, 0x80, 0xFF, 0xFF, 0x80, 0x40, 0x00}

	// runlength RunLength-encodes data as one literal run (ISO 32000-1, 7.4.5).
	runlength := func(data []byte) []byte {
		out := append([]byte{byte(len(data) - 1)}, data...) //nolint:gosec // eight samples.
		return append(out, 0x80)
	}

	var inlineErr error
	inline := func(entries string, data []byte) []byte {
		// A reader finds the end of inline image data by scanning for EI, so no
		// planted payload may hold one.
		if bytes.Contains(data, []byte("EI")) {
			inlineErr = fmt.Errorf("inline payload holds EI: %s", entries)
		}
		out := []byte("q 10 0 0 10 0 0 cm BI /W 4 /H 2 /CS /G /BPC 8 " + entries + " ID ")
		out = append(out, data...)
		return append(out, " EI Q\n"...)
	}
	upperHex := func(b []byte) []byte { return bytes.ToUpper([]byte(hex.EncodeToString(b))) }

	lzw := lzwEncode(samples, 1)
	lzw0 := lzwEncode(samples, 0)
	chained := append(upperHex(lzwEncode(runlength(samples), 1)), '>')

	fm := pdf.Stream(inline("/Filter /LZWDecode", lzw),
		"Type", n("XObject"), "Subtype", n("Form"), "BBox", a(0, 0, 100, 100), "Resources", d())
	pdf.MakeIndirect(fm)

	glyphA := pdf.Stream(append([]byte("1000 0 d0 "), inline("/F [/LZW]", lzw)...))
	pdf.MakeIndirect(glyphA)
	t3 := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type3"),
		"FontBBox", a(0, 0, 1000, 1000),
		"FontMatrix", a(0.001, 0, 0, 0.001, 0, 0),
		"CharProcs", d("a", glyphA),
		"Encoding", d("Type", n("Encoding"), "Differences", a(97, n("a"))),
		"FirstChar", 97,
		"LastChar", 97,
		"Widths", a(1000),
		"Resources", d(),
	))

	pt := pdf.Stream(inline("/F /LZW", lzw),
		"Type", n("Pattern"), "PatternType", 1, "PaintType", 1, "TilingType", 1,
		"BBox", a(0, 0, 10, 10), "XStep", 10, "YStep", 10, "Resources", d())
	pdf.MakeIndirect(pt)

	ap := pdf.Stream(inline("/F /Lzw", lzw),
		"Type", n("XObject"), "Subtype", n("Form"), "BBox", a(0, 0, 40, 40), "Resources", d())
	pdf.MakeIndirect(ap)
	annot := pdf.Indirect(d(
		"Type", n("Annot"),
		"Subtype", n("Square"),
		"Rect", a(10, 10, 50, 50),
		"F", 4,
		"AP", d("N", ap),
	))

	var body []byte
	body = append(body, inline(fmt.Sprintf("/F /LZW /DP << /EarlyChange 0 >> /L %d", len(lzw0)), lzw0)...)
	body = append(body, inline("/F [/AHx /LZW /RL]", chained)...)
	body = append(body, inline("/F /AHx", append(upperHex(samples), '>'))...)
	body = append(body, "q 100 0 0 100 100 400 cm /Fm Do Q\n"+
		"BT /T3 12 Tf 10 700 Td (a) Tj ET\n"+
		"q /Pattern cs /Pt scn 0 0 50 50 re f Q\n"...)
	content := pdf.Stream(body)
	pdf.MakeIndirect(content)
	if inlineErr != nil {
		return nil, inlineErr
	}

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d("Font", d("T3", t3), "XObject", d("Fm", fm), "Pattern", d("Pt", pt)),
		"Contents", content,
		"Annots", a(annot),
	))
	pdf.AddPage(page)

	// Deliberately no /OutputIntents and no /Metadata.
	return save(pdf, "1.7")
}
