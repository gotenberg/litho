package build

import (
	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
	"github.com/gotenberg/litho/test/integration/fixture/pdfgen/zdeflate"
)

func init() { register("cmapcidtext_2b", cmapcidtext2b) }

// cmapcidtext2b builds cmapcidtext_2b.pdf: text read through the CID an encoding CMap gives.
//
// A Type 0 font's encoding CMap takes a character code to a CID (ISO 32000-1,
// 9.7.6.2), and the tables a reader falls back on when the font has no
// /ToUnicode CMap are keyed by CID: the character collection's UCS2 CMap
// (9.10.2), and the /CIDToGIDMap stream with the embedded program's cmap behind
// it. Both fonts here encode one-byte codes their CMap sends to CIDs other than
// the codes, so a reader that took the code as the CID reads other characters.
// No corpus file shows text this way without a /ToUnicode CMap that answers
// first.
//
// | Font | Encoding CMap        | Shows | CIDs  | Route                                   | Text |
// | ---- | -------------------- | ----- | ----- | --------------------------------------- | ---- |
// | G1   | <61> to 2, <62> to 1 | 61 62 | 2, 1  | /CIDToGIDMap 1 to A, 2 to B, then cmap  | BA   |
// | G2   | <61> to 35, <62> to 34 | 61 62 | 35, 34 | Adobe-Japan1 UCS2 (CID 34 is U+0041) | BA   |
//
// G1 embeds the three-glyph program of _truetype, so both its CIDs select a
// glyph. G2 is a reference-only Adobe-Japan1 font: it fails the embedding
// rules, and is there because ISO 32000-1, 9.10.2 reads its CIDs through the
// collection whether or not a program is embedded.
//
// Every count was confirmed with live veraPDF 1.30.2. See ../FIXTURES.md for the
// authoritative expected firing set, and _encodingcmap for the font program.
func cmapcidtext2b() ([]byte, error) {
	const oneByte = "<00> <FF>"

	f := newECFixture()

	gidMap := f.pdf.Stream(zdeflate.Compress([]byte{0, 0, 0, 1, 0, 2}, 9))
	gidMap.Set("Filter", n("FlateDecode"))
	f.font("G1", "LithoCIDText", f.cmap("LithoG1", "2 begincidchar\n<61> 2\n<62> 1\nendcidchar\n", ecCMapOpts{Codespace: oneByte}),
		"6162", nil, f.pdf.MakeIndirect(gidMap).(*pdfgen.Stream))

	japan1 := ecSystemInfo("Japan1", 6)
	descendant := f.pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("CIDFontType2"),
		"BaseFont", n("/LithoCIDTextJapan"),
		"CIDSystemInfo", japan1,
		"DW", ttAdvanceWidth,
		"CIDToGIDMap", n("Identity"),
		"FontDescriptor", f.pdf.Indirect(d(
			"Type", n("FontDescriptor"), "FontName", n("/LithoCIDTextJapan"), "Flags", 32, "ItalicAngle", 0, "Ascent", 800,
			"Descent", -200, "CapHeight", 700, "StemV", 80, "FontBBox", a(0, -200, 550, 700))),
	))
	encoding := f.cmap("LithoG2", "2 begincidchar\n<61> 35\n<62> 34\nendcidchar\n", ecCMapOpts{Info: japan1, Codespace: oneByte})
	f.fonts = append(f.fonts, ecFont{"G2", f.pdf.Indirect(d(
		"Type", n("Font"), "Subtype", n("Type0"), "BaseFont", n("/LithoCIDTextJapan"), "Encoding", encoding,
		"DescendantFonts", a(descendant))), "6162"})

	return f.save()
}
