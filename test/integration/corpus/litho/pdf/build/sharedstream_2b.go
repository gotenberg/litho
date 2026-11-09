package build

import (
	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
	"github.com/gotenberg/litho/test/integration/fixture/pdfgen/zdeflate"
)

func init() { register("sharedstream_2b", sharedstream2b) }

// sharedstream2b builds sharedstream_2b.pdf: one content stream two pages read differently.
//
// ISO 32000-1, 7.7.3.3 lets two pages name the same content stream, and each
// page reads it through its own /Resources (7.8.3). Both pages here name one
// stream as their whole /Contents:
//
//	BT /F1 24 Tf 50 700 Td (A\x01) Tj ET
//
//	page 1  /F1 is a simple TrueType under WinAnsiEncoding: `A` shows A, and
//	        0x01 has no glyph, so it shows `.notdef` (ISO 19005-2, 6.2.11.8 t1)
//	page 2  /F1 is a Type0 font under Identity-H: the two bytes are one code,
//	        0x4101, CID 16641, which its CIDToGIDMap sends to the glyph A
//
// The aggressive text-control repair strips the 0x01 page 1 shows. Rewriting the
// shared stream in place took the byte off page 2 as well, leaving it a lone
// `A` that completes no two-byte code: page 2's glyph was gone. The page the
// repair rewrites now gets a stream of its own, and page 2 keeps its code.
//
// No corpus file has a page that names, through a reference, a stream another
// page names too and that a content repair rewrites, so the gate never saw it.
//
// Both fonts embed the conforming TrueType program of `ttcmap_2b.pdf`. The simple
// font's `/Widths` cover the codes from 1, so the width of the `.notdef` glyph
// agrees with the dictionary, and the Type0 font carries a ToUnicode CMap so its
// code maps to U+0041.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func sharedstream2b() ([]byte, error) {
	pdf := pdfgen.New()

	latin := map[int]string{0x41: "A", 0x42: "B"}
	program := ttBuild([]ttSubtable{{3, 1, latin}, {1, 0, latin}}, nil)

	fontFile := func() pdfgen.Object {
		stream := pdf.Stream(program)
		stream.Set("Length1", len(program))
		return pdf.MakeIndirect(stream)
	}

	descriptor := func(name string) *pdfgen.Dict {
		return pdf.Indirect(d(
			"Type", n("FontDescriptor"),
			"FontName", n(name),
			"Flags", 32, // non-symbolic, ISO 32000-1, Table 123
			"ItalicAngle", 0,
			"Ascent", 800,
			"Descent", -200,
			"CapHeight", 700,
			"StemV", 80,
			"FontBBox", a(0, -200, 550, 700),
			"FontFile2", fontFile(),
		))
	}

	const first, last = 1, 66
	widths := a()
	for range last - first + 1 {
		widths.Append(ttAdvanceWidth)
	}
	simple := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("TrueType"),
		"BaseFont", n("/LithoSharedSans"),
		"FirstChar", first,
		"LastChar", last,
		"Widths", widths,
		"FontDescriptor", descriptor("/LithoSharedSans"),
		"Encoding", n("WinAnsiEncoding"),
	))

	const cid = 0x4101 // the bytes "A\x01" read as one Identity-H code
	const gidA = 1
	gidMap := make([]byte, 2*(cid+1))
	gidMap[2*cid] = byte(gidA >> 8)
	gidMap[2*cid+1] = byte(gidA & 0xFF)
	cidToGID := pdf.Stream(zdeflate.Compress(gidMap, 9))
	cidToGID.Set("Filter", n("FlateDecode"))

	toUnicode := pdf.Stream([]byte(
		"/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n" +
			"/CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def\n" +
			"/CMapName /Adobe-Identity-UCS def\n/CMapType 2 def\n" +
			"1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n" +
			"1 beginbfchar\n<4101> <0041>\nendbfchar\n" +
			"endcmap\nCMapName currentdict /CMap defineresource pop\nend\nend\n",
	))

	composite := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type0"),
		"BaseFont", n("/LithoSharedCID"),
		"Encoding", n("/Identity-H"),
		"ToUnicode", pdf.MakeIndirect(toUnicode),
		"DescendantFonts", a(
			pdf.Indirect(d(
				"Type", n("Font"),
				"Subtype", n("CIDFontType2"),
				"BaseFont", n("/LithoSharedCID"),
				"CIDSystemInfo", d(
					"Registry", text("Adobe"),
					"Ordering", text("Identity"),
					"Supplement", 0,
				),
				"FontDescriptor", descriptor("/LithoSharedCID"),
				"DW", 1000,
				"W", a(cid, a(ttAdvanceWidth)),
				"CIDToGIDMap", pdf.MakeIndirect(cidToGID),
			)),
		),
	))

	shared := pdf.MakeIndirect(pdf.Stream([]byte("BT /F1 24 Tf 50 700 Td (A\x01) Tj ET\n")))

	for _, font := range []*pdfgen.Dict{simple, composite} {
		page := d(
			"Type", n("Page"),
			"MediaBox", a(0, 0, 612, 792),
			"Resources", d("Font", d("F1", font)),
			"Contents", shared,
		)
		pdf.AddPage(pdf.Indirect(page))
	}

	// Deliberately no /Metadata and no /OutputIntents.
	return save(pdf, "1.7")
}
