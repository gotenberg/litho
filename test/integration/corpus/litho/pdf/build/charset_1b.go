package build

import (
	"bytes"
	"fmt"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("charset_1b", charset1b) }

// charset1b builds charset_1b.pdf: what a subset font's /CharSet and /CIDSet must list.
//
// A coverage scan showed twelve cells no fixture reached, all of them the same
// two clauses read at four different strictnesses:
//
// | Rule                          | Reads                                           |
// | ----------------------------- | ----------------------------------------------- |
// | ISO 19005-1, 6.3.5 t2/t3      | a Type 1 or CID subset must carry the entry     |
// | ISO 19005-2/3, 6.2.11.4.2 t1  | a `/CharSet` present must list every glyph      |
// | ISO 19005-2/3, 6.2.11.4.2 t2  | a `/CIDSet` present must identify every CID     |
// | ISO 14289-1, 7.21.4.2 t1/t2   | the PDF/UA-1 spelling of the same two           |
//
// Part 1 requires the entry to exist; parts 2 and 3 permit it to be absent and
// judge it only once present. One font cannot carry both defects, so the file
// carries eight, four per family:
//
// | Font       | `/BaseFont`       | Entry            | Answers                    |
// | ---------- | ----------------- | ---------------- | -------------------------- |
// | `T1None`   | `ABCDEF+LithoCFF` | no `/CharSet`    | 6.3.5 t2 at part 1 only    |
// | `T1Short`  | `BCDEFG+LithoCFF` | `/CharSet (/A)`  | the completeness tests     |
// | `T1Full`   | `CDEFGH+LithoCFF` | `/CharSet (/A/B)`| nothing                    |
// | `T1Whole`  | `LithoCFF`        | no `/CharSet`    | nothing: not a subset      |
// | `CIDNone`  | `DEFGHI+LithoCID` | no `/CIDSet`     | 6.3.5 t3 at part 1 only    |
// | `CIDShort` | `EFGHIJ+LithoCID` | `/CIDSet` less 2 | the completeness tests     |
// | `CIDFull`  | `FGHIJK+LithoCID` | full `/CIDSet`   | nothing                    |
// | `CIDWhole` | `LithoCID`        | no `/CIDSet`     | nothing: not a subset      |
//
// Every font is painted, and each shows both glyphs the program defines. That is
// the masking probe twice over: the two families answer independently at every
// profile, so a checker that reports the first offending font and stops reports
// one rule where two are due. The two `Whole` fonts are the subset-prefix
// exemption planted as a control, and the two `Full` fonts are the conforming
// case of the very entry the others break.
//
// The subset prefixes differ per font on purpose. veraPDF keys a font object on
// its name, so two fonts sharing `ABCDEF+LithoCFF` would collapse into one and
// the control would never be judged separately from the defect.
//
// `T1Short` renders `B`, which is what makes it answer at part 1 as well: ISO
// 19005-1, 6.3.5 t2 is one-directional and scoped to the glyphs the content
// paints, while ISO 19005-2, 6.2.11.4.2 t1 compares against the whole program.
// Both readings condemn a `/CharSet` of `(/A)` on a font that paints `B`.
//
// The programs come from `cff.go` and `truetype.go`, which emit the bytes
// directly: a validator has to parse a real program to know which glyphs it
// defines, and a placeholder stream leaves every completeness test silent.
//
// Neither `/Metadata` nor an output intent is present, so the file also answers
// the metadata and colour clauses. They belong to different parts of the
// specification entirely and give the font verdicts something to hide behind.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func charset1b() ([]byte, error) {
	pdf := pdfgen.New()

	cffProgram := cffBuild()
	ttProgram := ttBuild([]ttSubtable{{3, 1, map[int]string{0x41: "A", 0x42: "B"}}}, nil)

	const nonsymbolic = 32
	const symbolic = 4

	// A /CIDSet is a bitmap indexed by CID, most significant bit first, so CIDs 1
	// and 2 are bits 6 and 5 of the first byte. CID 0 is .notdef and exempt.
	cidsetFull := []byte{0b01100000}
	cidsetShort := []byte{0b01000000}

	// cffDescriptor is a Type 1 descriptor carrying the bare CFF, with
	// /CharSet unless charset is empty.
	cffDescriptor := func(name, charset string) *pdfgen.Dict {
		program := pdf.Stream(cffProgram)
		program.Set("Subtype", n("/Type1C"))
		dd := d(
			"Type", n("FontDescriptor"),
			"FontName", n("/"+name),
			"Flags", nonsymbolic,
			"ItalicAngle", 0,
			"Ascent", 800,
			"Descent", -200,
			"CapHeight", 700,
			"StemV", 80,
			"FontBBox", a(0, -200, 550, 700),
			"FontFile3", pdf.MakeIndirect(program),
		)
		if charset != "" {
			dd.Set("CharSet", text(charset))
		}
		return pdf.Indirect(dd)
	}

	// type1Font is one simple font over the CFF program, conforming apart
	// from /CharSet.
	type1Font := func(name, charset string) *pdfgen.Dict {
		return pdf.Indirect(d(
			"Type", n("Font"),
			"Subtype", n("Type1"),
			"BaseFont", n("/"+name),
			"FirstChar", 65,
			"LastChar", 66,
			"Widths", a(600, 600),
			"Encoding", n("WinAnsiEncoding"),
			"FontDescriptor", cffDescriptor(name, charset)))
	}

	// ttDescriptor is a CIDFont descriptor carrying the TrueType program,
	// /CIDSet when asked.
	ttDescriptor := func(name string, cidset []byte) *pdfgen.Dict {
		program := pdf.Stream(ttProgram)
		program.Set("Length1", len(ttProgram))
		dd := d(
			"Type", n("FontDescriptor"),
			"FontName", n("/"+name),
			"Flags", symbolic,
			"ItalicAngle", 0,
			"Ascent", 800,
			"Descent", -200,
			"CapHeight", 700,
			"StemV", 80,
			"FontBBox", a(0, -200, 550, 700),
			"FontFile2", pdf.MakeIndirect(program),
		)
		if cidset != nil {
			dd.Set("CIDSet", pdf.MakeIndirect(pdf.Stream(cidset)))
		}
		return pdf.Indirect(dd)
	}

	// cidFont is a Type 0 font over a CIDFontType2 descendant, Identity-H
	// encoded.
	cidFont := func(name string, cidset []byte) *pdfgen.Dict {
		descendant := pdf.Indirect(d(
			"Type", n("Font"),
			"Subtype", n("CIDFontType2"),
			"BaseFont", n("/"+name),
			"CIDSystemInfo", d(
				"Registry", text("Adobe"), "Ordering", text("Identity"), "Supplement", 0),
			"FontDescriptor", ttDescriptor(name, cidset),
			"DW", 600,
			"CIDToGIDMap", n("Identity")))
		return pdf.Indirect(d(
			"Type", n("Font"),
			"Subtype", n("Type0"),
			"BaseFont", n("/"+name),
			"Encoding", n("/Identity-H"),
			"DescendantFonts", a(descendant)))
	}

	fonts := d(
		"T1None", type1Font("ABCDEF+LithoCFF", ""),
		"T1Short", type1Font("BCDEFG+LithoCFF", "/A"),
		"T1Full", type1Font("CDEFGH+LithoCFF", "/A/B"),
		"T1Whole", type1Font("LithoCFF", ""),
		"CIDNone", cidFont("DEFGHI+LithoCID", nil),
		"CIDShort", cidFont("EFGHIJ+LithoCID", cidsetShort),
		"CIDFull", cidFont("FGHIJK+LithoCID", cidsetFull),
		"CIDWhole", cidFont("LithoCID", nil),
	)

	// Every font shows both glyphs its program defines. A font declared in
	// /Resources but never selected by a Tf is reached by neither engine.
	lines := make([][]byte, 0, 8)
	y := 720
	for _, key := range []string{"T1None", "T1Short", "T1Full", "T1Whole"} {
		lines = append(lines, []byte(fmt.Sprintf("BT /%s 12 Tf 50 %d Td (AB) Tj ET", key, y)))
		y -= 30
	}
	for _, key := range []string{"CIDNone", "CIDShort", "CIDFull", "CIDWhole"} {
		lines = append(lines, []byte(fmt.Sprintf("BT /%s 12 Tf 50 %d Td <00010002> Tj ET", key, y)))
		y -= 30
	}
	content := append(bytes.Join(lines, []byte("\n")), '\n')

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d("Font", fonts),
		"Contents", pdf.Stream(content)))
	pdf.AddPage(page)

	// Deliberately no /Metadata and no output intent.
	return save(pdf, "1.7")
}
