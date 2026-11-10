package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("ttcmap_2b", ttcmap2b) }

// ttcmap2b builds ttcmap_2b.pdf: 'cmap' subtable requirements on embedded TrueType programs.
//
// The coverage scan showed ISO 19005-2, 6.2.11.6 tests 1 and 4 unreached, and
// `ttencoding_2b.pdf` explains why: those two are scoped to `TrueTypeFontProgram`
// rather than to the font dictionary, so they need a real embedded program whose
// 'cmap' table carries particular (platformID, encodingID) subtables. No corpus
// fixture embedded a TrueType program at all. `truetype.go` emits them.
//
// The two tests are mutually exclusive in one font, since t1 judges non-symbolic
// programs and t4 symbolic ones, so the fixture carries four sibling fonts: one
// defective of each kind and one conforming control of each kind. All four are
// drawn, because an undrawn font reaches neither test. Planting both defects at
// once is the masking probe: both are `TrueTypeFontProgram` rules, so a check
// that reports the first offending program and stops reports one where two are
// due, and the two controls catch a check that condemns every embedded program
// once it sees one.
//
// The part split is real and is exercised by the same file at its other profiles.
// Parts 2 and 3 and PDF/UA-1 ask for at least one subtable, and more than one when
// a Microsoft Symbol (3,0) is present. PDF/A-4 and PDF/UA-2 instead ask for a
// Microsoft Unicode (3,1) or a Macintosh Roman (1,0), whatever the count. PDF/A-1
// states no non-symbolic requirement at all. The non-symbolic defect here carries
// only a (3,0), which every part that states the rule rejects, so the cell moves
// with the profile rather than staying silent outside part 2.
//
// The glyph names are carried in a 'post' table in format 2.0 on purpose. Without
// them a non-symbolic font whose 'cmap' offers no usable subtable fails the glyph
// lookup too and drags in 6.2.11.4.1 t2, which would mask the 'cmap' verdict this
// fixture is about. Measured on both tools: format 3.0 adds that clause, format
// 2.0 does not.
//
// Per part the clause is ISO 19005-2 and -3, 6.2.11.6, ISO 19005-4, 6.2.10.6,
// ISO 14289-1, 7.21.6, and ISO 14289-2, 8.4.5.7.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func ttcmap2b() ([]byte, error) {
	pdf := pdfgen.New()

	// Flags bit 3 (value 4) marks a symbolic font, bit 6 (value 32) a non-symbolic
	// one. Per ISO 32000-1, Table 123 the two are exclusive, and both tests this
	// fixture reaches turn on which one is set.
	const (
		symbolic    = 4
		nonsymbolic = 32
	)

	// The codes the page draws. A symbolic (3,0) subtable is conventionally keyed in
	// the 0xF000 range, so the symbolic fonts map both forms and a viewer resolves
	// the glyph either way.
	latin := map[int]string{0x41: "A", 0x42: "B"}
	latinAndSymbolRange := map[int]string{0x41: "A", 0x42: "B", 0xF041: "A", 0xF042: "B"}

	// truetype is one simple TrueType font dictionary with an embedded
	// program.
	truetype := func(name string, flags int, subtables []ttSubtable, encoding string) *pdfgen.Dict {
		program := ttBuild(subtables, nil)
		fontFile := pdf.Stream(program)
		fontFile.Set("Length1", len(program))
		descriptor := d(
			"Type", n("FontDescriptor"),
			"FontName", n("/"+name),
			"Flags", flags,
			"ItalicAngle", 0,
			"Ascent", 800,
			"Descent", -200,
			"CapHeight", 700,
			"StemV", 80,
			"FontBBox", a(0, -200, 550, 700),
			"FontFile2", pdf.MakeIndirect(fontFile),
		)
		font := d(
			"Type", n("Font"),
			"Subtype", n("TrueType"),
			"BaseFont", n("/"+name),
			"FirstChar", 65,
			"LastChar", 66,
			"Widths", a(ttAdvanceWidth, ttAdvanceWidth),
			"FontDescriptor", pdf.Indirect(descriptor),
		)
		if encoding != "" {
			font.Set("Encoding", n("/"+encoding))
		}
		return pdf.Indirect(font)
	}

	// T1: non-symbolic, and its program carries only a Microsoft Symbol (3,0), which
	// supplies no non-symbolic lookup. Fires t1.
	t1 := truetype(
		"LithoNonSymOnly30",
		nonsymbolic,
		[]ttSubtable{{3, 0, latinAndSymbolRange}},
		"WinAnsiEncoding",
	)
	// T2: symbolic, and its program carries two subtables, neither of them (3,0).
	// Fires t4. It declares no /Encoding, so 6.2.11.6 t3 stays silent.
	t2 := truetype("LithoSymTwoNo30", symbolic, []ttSubtable{{1, 0, latin}, {3, 1, latin}}, "")
	// T3 and T4 conform, so a firing against either is over-suppression.
	t3 := truetype("LithoNonSymOK", nonsymbolic, []ttSubtable{{3, 1, latin}, {1, 0, latin}}, "WinAnsiEncoding")
	t4 := truetype("LithoSymOK30", symbolic, []ttSubtable{{3, 0, latinAndSymbolRange}}, "")

	// An ExtGState carrying a transfer function, which ISO 19005-2, 6.2.5 forbids
	// outright. It belongs to no font clause, so it shows a 'cmap' verdict does not
	// leak into unrelated checks, and gives the page a second independent defect.
	transfer := pdf.Stream([]byte("{ 1 exch sub }"))
	transfer.Set("FunctionType", 4)
	transfer.Set("Domain", a(0, 1))
	transfer.Set("Range", a(0, 1))
	gs := pdf.Indirect(d("Type", n("ExtGState"), "TR", pdf.MakeIndirect(transfer)))

	// The two defective fonts are drawn inside a q/Q pair whose Q restores the font
	// selected before it. Per ISO 32000-1, 8.4.2 and Table 104 the text font is part
	// of the graphics state, so a validator that ignores the restore attributes the
	// last two codes to the wrong font and reports the wrong program.
	content := "q /GS1 gs 0 0 0 rg 50 730 40 20 re f Q\n" +
		"BT /T3 24 Tf 50 700 Td (AB) Tj ET\n" +
		"q BT /T1 24 Tf 50 660 Td (AB) Tj ET\n" +
		"BT /T2 24 Tf 50 620 Td (AB) Tj ET Q\n" +
		"BT 50 580 Td (AB) Tj ET\n" +
		"BT /T4 24 Tf 50 540 Td (AB) Tj ET\n"

	page := d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d(
			"Font", d("T1", t1, "T2", t2, "T3", t3, "T4", t4),
			"ExtGState", d("GS1", gs),
		),
		"Contents", pdf.Stream([]byte(content)),
	)
	pdf.AddPage(pdf.Indirect(page))

	// Deliberately no /Metadata and no /OutputIntents.
	return save(pdf, "1.7")
}
