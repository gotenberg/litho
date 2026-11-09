package build

import (
	"fmt"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("glyphs_2b", glyphs2b) }

// glyphs2b builds glyphs_2b.pdf: composite fonts and the glyphs a page actually paints.
//
// The coverage scan showed one cluster unreached in every profile at once. The
// same three requirements appear under four spellings, so a single file covering
// them lifts PDF/A-2, PDF/A-4, PDF/UA-1 and PDF/UA-2 together:
//
// | Construct                                        | A-2b spelling  |
// | ------------------------------------------------ | -------------- |
// | `CIDFontType2` embedded with no `/CIDToGIDMap`   | `6.2.11.3.2/1` |
// | a painted code whose glyph the program lacks     | `6.2.11.4.1/2` |
// | a painted code whose glyph is `.notdef`          | `6.2.11.8/1`   |
//
// Two of these are usage-scoped: the clause judges the glyphs a page paints, not
// the ones a font could reach. That is what makes the file a masking probe rather
// than four separate documents. A missing glyph and a `.notdef` glyph ride on
// separate fonts, so a check that abandons a font after its first bad glyph still
// has to report the other; and each defective font sits beside a conforming one
// whose glyphs are all present and named, so a check that condemns a font wholesale
// fires on the control.
//
// The five composite fonts, identical but for one thing each:
//
//	CID0  `CIDFontType2` with `/FontFile2` and no `/CIDToGIDMap`, painting a
//	      glyph the program does have. Answers the CIDToGIDMap rule alone.
//	CID1  the same font declaring `/CIDToGIDMap /Identity`, painting CID 5. The
//	      program holds three glyphs, so CID 5 resolves past the end of the
//	      program and answers the missing-glyph rule.
//	CID2  the same font again, painting CID 0, which is `.notdef` by definition.
//	CID3  the conforming control: mapping declared, painting a present glyph
//	      that is not `.notdef`. A firing against it is over-suppression.
//	CID4  the rendering-mode control: CID 5 again, painted only in mode 3.
//
// CID1 and CID2 both declare `/CIDToGIDMap /Identity`, so neither of them can
// answer the CIDToGIDMap rule and CID0 is left as its only source. Equally CID0
// paints a real glyph, so it cannot answer the other two. Each rule has exactly
// one source, which is what lets the instance counts prove the controls silent.
//
// The rendering mode matters, but only to one of the two glyph tests. Measured
// against veraPDF 1.30.2, the missing-glyph test reads
// `renderingMode == 3 || isGlyphPresent == null || isGlyphPresent == true`, so a
// glyph painted in mode 3 is invisible and exempt. The `.notdef` test reads
// `name != ".notdef"` and carries no such escape at all. CID4 paints the missing
// glyph in mode 3 and nowhere else, so it is the control for the missing-glyph
// escape and only that one: the `.notdef` rule condemns it like any other, since
// CID 5 lies past the end of a three-glyph program and so resolves to `.notdef`.
//
// That asymmetry is why the two rules do not have one source each. The missing-
// glyph rule answers once, for CID1. The `.notdef` rule answers three times: CID2
// paints `.notdef` by its number, and CID1 and CID4 both reach it by resolving a
// CID the program does not hold. litho reports only CID2, an under-count recorded
// against the fixture rather than papered over.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func glyphs2b() ([]byte, error) {
	pdf := pdfgen.New()

	const nonsymbolic = 32

	// The program holds .notdef, A and B, so glyph index 3 and beyond do not exist.
	latin := map[int]string{0x41: "A", 0x42: "B"}
	program := ttBuild([]ttSubtable{{3, 1, latin}}, nil)
	const missingCID = 5
	const notdefCID = 0
	const presentCID = 1

	// fontFile is one embedded TrueType program. Each font gets its own
	// stream object.
	fontFile := func() *pdfgen.Stream {
		stream := pdf.Stream(program)
		stream.Set("Length1", len(program))
		return pdf.MakeIndirect(stream).(*pdfgen.Stream)
	}

	descriptor := func(name string, flags int) *pdfgen.Dict {
		return pdf.Indirect(d(
			"Type", n("FontDescriptor"),
			"FontName", n("/"+name),
			"Flags", flags,
			"ItalicAngle", 0,
			"Ascent", 800,
			"Descent", -200,
			"CapHeight", 700,
			"StemV", 80,
			"FontBBox", a(0, -200, 550, 700),
			"FontFile2", fontFile(),
		))
	}

	// cidFont is a Type0 font over a CIDFontType2 descendant, Identity-H
	// encoded.
	//
	// Passing "" for cidToGID leaves /CIDToGIDMap out, which is the defect
	// ISO 19005-2, 6.2.11.3.2 judges; the clause requires an embedded
	// CIDFontType2 to state the mapping rather than leave it implied.
	cidFont := func(name, cidToGID string) *pdfgen.Dict {
		descendant := d(
			"Type", n("Font"),
			"Subtype", n("CIDFontType2"),
			"BaseFont", n("/"+name),
			"CIDSystemInfo", d(
				"Registry", text("Adobe"), "Ordering", text("Identity"), "Supplement", 0,
			),
			"FontDescriptor", descriptor(name, nonsymbolic),
			"DW", ttAdvanceWidth,
		)
		if cidToGID != "" {
			descendant.Set("CIDToGIDMap", n("/"+cidToGID))
		}
		return pdf.Indirect(d(
			"Type", n("Font"),
			"Subtype", n("Type0"),
			"BaseFont", n("/"+name),
			"Encoding", n("/Identity-H"),
			"DescendantFonts", a(pdf.Indirect(descendant)),
		))
	}

	// CID0 answers the CIDToGIDMap rule and nothing else: it paints CID 1, a glyph
	// the program does hold and which is not .notdef.
	cidNoMap := cidFont("LithoCIDNoMap", "")
	// CID1 and CID2 declare the mapping, so they cannot answer that rule. Each
	// answers exactly one glyph rule.
	cidMissing := cidFont("LithoCIDMissing", "Identity")
	cidNotdef := cidFont("LithoCIDNotdef", "Identity")
	// The control: mapping declared, painting a present, named glyph.
	cidOK := cidFont("LithoCIDConforming", "Identity")
	// The rendering-mode control. It carries the same missing CID as CID1, and its
	// only use is in mode 3, which both glyph tests exempt. It needs its own font
	// because the exemption is judged per font: painting the missing glyph visibly
	// and invisibly through one font leaves the font answerable either way, so the
	// control would prove nothing.
	cidInvisible := cidFont("LithoCIDInvisible", "Identity")

	// simpleTrueType is a simple TrueType font. subtype is spelled by the
	// caller on purpose.
	simpleTrueType := func(name, subtype string) *pdfgen.Dict {
		return pdf.Indirect(d(
			"Type", n("Font"),
			"Subtype", n("/"+subtype),
			"BaseFont", n("/"+name),
			"FirstChar", 0x41,
			"LastChar", 0x42,
			"Widths", a(ttAdvanceWidth, ttAdvanceWidth),
			"Encoding", n("WinAnsiEncoding"),
			"FontDescriptor", descriptor(name, nonsymbolic),
		))
	}

	// A conforming simple TrueType. ISO 19005-2, 6.2.11.2 t2, which lists the font
	// subtypes a file may use, is deliberately not planted: veraPDF builds no font
	// object at all for a subtype it does not recognise, so the rule is unreachable
	// through its model and any file carrying one would diverge permanently. Probed
	// with TrueType2, Type1C, CIDFontType3 and MMType1; none fires it.
	ttOK := simpleTrueType("LithoGoodSubtype", "TrueType")

	// cidText is an Identity-H string selecting one CID, which is two bytes.
	cidText := func(cid int) string {
		return fmt.Sprintf("<%04X>", cid)
	}

	content := "BT /CN 12 Tf 50 740 Td " + cidText(presentCID) + " Tj ET\n" +
		"BT /CM 12 Tf 50 710 Td " + cidText(missingCID) + " Tj ET\n" +
		"BT /CD 12 Tf 50 680 Td " + cidText(notdefCID) + " Tj ET\n" +
		"BT /CO 12 Tf 50 650 Td " + cidText(presentCID) + " Tj ET\n" +
		// The rendering-mode control, on its own font so the exemption is real.
		"BT 3 Tr /CI 12 Tf 50 620 Td " + cidText(missingCID) + " Tj ET\n" +
		"BT /TG 12 Tf 50 560 Td (AB) Tj ET\n"

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d(
			"Font", d(
				"CN", cidNoMap,
				"CM", cidMissing,
				"CD", cidNotdef,
				"CO", cidOK,
				"CI", cidInvisible,
				"TG", ttOK,
			),
		),
		"Contents", pdf.Stream([]byte(content)),
	))
	pdf.AddPage(page)

	// Deliberately no /OutputIntents and no /Metadata, both independent of the font
	// clauses above so a font verdict stays visibly separate from them.
	return save(pdf, "1.7")
}
