package page

import (
	"context"
	_ "embed"
	"fmt"
	"maps"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
)

func init() {
	maps.Copy(popplerFixtures, substitutePopplerFixtures)
}

// dejaVuSansAB is DejaVu Sans 2.37 subset to A, B and space with fontTools:
// the face the pinned poppler draws an unembedded Calibri with (fc-match
// Calibri in litho-corpus-builder), so both renderers draw the same
// substitute. The DejaVu changes are in the public domain and the Bitstream
// Vera licence allows redistributing the font; its name table carries both.
//
//go:embed testdata/dejavu_sans_ab.ttf
var dejaVuSansAB []byte

// substitutePopplerFixtures join popplerFixtures, each an unembedded
// Identity-H Calibri drawn with DejaVu Sans. /W states DejaVu's advances, so
// neither renderer rescales a glyph.
//
//   - subst_tounicode: an Adobe-Identity font whose /ToUnicode gives CIDs 9,
//     80 and 109 the characters A, B and space. Poppler reads it through its
//     /ToUnicode as litho does; read as glyph indices under the Identity
//     /CIDToGIDMap, the CIDs would select glyphs the subset does not hold.
//   - subst_collection: an Adobe-Japan1 font with no /ToUnicode, whose CIDs
//     34, 35 and 1 its collection's UCS2 CMap reads as A, B and space
//     (ISO 32000-1, 9.10.2). Poppler reads them through poppler-data's
//     Adobe-Japan1 CID-to-Unicode table and, finding no font that supports
//     Japanese, draws with the one font it is given.
var substitutePopplerFixtures = map[string]fixture{
	"subst_tounicode": {
		page:       textPage,
		content:    "0 g BT /F 30 Tf 5 60 Td <00090050006D0009> Tj 0 -40 Td <0050006D00090050> Tj ET",
		objs:       unembeddedType0("Identity-H", "Identity", "[9 [684] 80 [686] 109 [318]]", substituteToUnicode),
		substitute: dejaVuSansAB,
	},
	"subst_collection": {
		page:       textPage,
		content:    "0 g BT /F 30 Tf 5 60 Td <0022002300010022> Tj 0 -40 Td <0023000100220023> Tj ET",
		objs:       unembeddedType0("Identity-H", "Japan1", "[1 [318] 34 [684 686]]", ""),
		substitute: dejaVuSansAB,
	},
}

// substituteProgram is the program served for the fonts the substitute
// fixtures do not embed: rectangles as wide as their advance, A 600 units
// wide, B 700, and an empty space.
var substituteProgram = fontbuild.TrueType{PostScriptName: "LithoSub", Family: "LithoSub", Subfamily: "Regular", Glyphs: textGlyphs}.Build()

// substituteToUnicode is a /ToUnicode CMap giving CIDs 9, 80 and 109 the
// characters A, B and space.
const substituteToUnicode = "/CIDInit /ProcSet findresource begin 12 dict begin begincmap " +
	"/CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def /CMapName /Adobe-Identity-UCS def /CMapType 2 def " +
	"1 begincodespacerange <0000> <FFFF> endcodespacerange " +
	"3 beginbfchar <0009> <0041> <0050> <0042> <006D> <0020> endbfchar " +
	"endcmap CMapName currentdict /CMap defineresource pop end end"

// unembeddedType0 returns objects 5 (a Type 0 font over encoding, /F in
// textPage), 6 (its CIDFontType2 descendant of the collection ordering, with
// widths w and no embedded program) and 7 (its descriptor), and the
// /ToUnicode stream toUnicode as object 8 when it is not empty.
func unembeddedType0(encoding, ordering, w, toUnicode string) []string {
	font := "<< /Type /Font /Subtype /Type0 /BaseFont /Calibri /Encoding /" + encoding + " /DescendantFonts [6 0 R]"
	if toUnicode != "" {
		font += " /ToUnicode 8 0 R"
	}
	objs := []string{
		font + " >>",
		"<< /Type /Font /Subtype /CIDFontType2 /BaseFont /Calibri /CIDSystemInfo << /Registry (Adobe) /Ordering (" + ordering +
			") /Supplement 2 >> /FontDescriptor 7 0 R /DW 1000 /W " + w + " >>",
		"<< /Type /FontDescriptor /FontName /Calibri /Flags 32 /FontBBox [0 0 1000 500] /ItalicAngle 0 " +
			"/Ascent 500 /Descent 0 /CapHeight 500 /StemV 80 >>",
	}
	if toUnicode != "" {
		objs = append(objs, stream("", toUnicode))
	}
	return objs
}

// substituteFixtures draw a Type 0 font the document does not embed with the
// rectangle substitute. A code selects the substitute's glyph by the character its
// /ToUnicode or its collection's UCS2 CMap gives it, never by its CID: under
// an Identity /CIDToGIDMap CIDs 9, 80 and 109 would select glyphs the
// substitute does not hold (ISO 32000-1, 9.7.4.2, Table 117).
var substituteFixtures = map[string]fixture{
	"subst_tounicode": {
		page:       textPage,
		content:    "0 g BT /F 20 Tf 5 70 Td <00090050006D0009> Tj 0 -40 Td <0050006D00090050> Tj ET",
		objs:       unembeddedType0("Identity-H", "Identity", "[9 [600] 80 [700] 109 [250]]", substituteToUnicode),
		substitute: substituteProgram,
	},
	// Adobe-Japan1 CIDs 34, 35 and 1 are A, B and space in its UCS2 CMap.
	// substitutePopplerFixtures draws the same font against poppler.
	"subst_collection": {
		page:       textPage,
		content:    "0 g BT /F 20 Tf 5 70 Td <002200230001002200230022> Tj ET",
		objs:       unembeddedType0("Identity-H", "Japan1", "[1 [250] 34 [600 700]]", ""),
		substitute: substituteProgram,
	},
}

func TestSubstituteByUnicode(t *testing.T) {
	t.Parallel()
	// CID 9 is A, 600 wide: x 5..17 at 20 points; CID 80 is B, 17..31; CID
	// 109 a space, 31..36; then A again, 36..48. Rows 20..30.
	img, report := draw(t, substituteFixtures["subst_tounicode"])
	expect(t, img, 0, map[[2]int]rgb{
		{6, 25}: black, {16, 25}: black, {18, 25}: black, {30, 25}: black,
		{33, 25}: paper, {37, 25}: black, {47, 25}: black, {49, 25}: paper,
	})
	if n := report.Unsupported[PlaceholderGlyph]; n != 0 {
		t.Errorf("%d placeholders, want every code drawn from the substitute", n)
	}
	// The collection's UCS2 CMap reads Adobe-Japan1 CID 34 as A, 35 as B
	// and 1 as a space.
	img, _ = draw(t, substituteFixtures["subst_collection"])
	expect(t, img, 0, map[[2]int]rgb{{6, 25}: black, {18, 25}: black, {33, 25}: paper, {37, 25}: black})
}

func TestSubstituteVertical(t *testing.T) {
	t.Parallel()
	// Identity-V selects the substitute's glyph by the same reading: CID 34
	// is A. The vertical metrics stay the font's, the /DW2 default here
	// (ISO 32000-1, 9.7.4.3), so the glyphs fall where TestVerticalText's
	// embedded ones do.
	f := fixture{
		page:       textPage,
		content:    "0 g BT /F 20 Tf 50 80 Td <00220022> Tj ET",
		objs:       unembeddedType0("Identity-V", "Japan1", "[34 [600]]", ""),
		substitute: substituteProgram,
	}
	img, report := draw(t, f)
	expect(t, img, 0, map[[2]int]rgb{
		{50, 32}: black, {50, 52}: black, {50, 42}: paper,
		{45, 32}: black, {42, 32}: paper, {57, 32}: paper,
	})
	if n := report.Unsupported[PlaceholderGlyph]; n != 0 {
		t.Errorf("%d placeholders, want none", n)
	}
}

func TestSubstituteWithoutUnicodeDrawsPlaceholder(t *testing.T) {
	t.Parallel()
	// An Adobe-Identity Melior without /ToUnicode, a face of no known glyph
	// order, leaves its CIDs no character: CID 65, U+0041 read as a
	// character, and CID 2, the substitute's A read as a glyph index, each
	// draw a hollow box, x 5..17 and 17..29.
	objs := unembeddedType0("Identity-H", "Identity", "[2 [600] 65 [600]]", "")
	for i := range objs {
		objs[i] = strings.ReplaceAll(objs[i], "/Calibri", "/Melior")
	}
	f := fixture{
		page:       textPage,
		content:    "0 g BT /F 20 Tf 5 70 Td <00410002> Tj ET",
		objs:       objs,
		substitute: substituteProgram,
	}
	img, report := draw(t, f)
	expect(t, img, 0, map[[2]int]rgb{{5, 25}: black, {11, 25}: paper, {23, 25}: paper})
	if n := report.Unsupported[PlaceholderGlyph]; n != 2 {
		t.Errorf("%d placeholders, want 2", n)
	}
}

func TestSolidPlaceholdersFillTheBox(t *testing.T) {
	t.Parallel()
	// The same two hollow boxes, x 5..17 and 17..29: SolidPlaceholders fills
	// their inside, which the hollow frame leaves paper.
	objs := unembeddedType0("Identity-H", "Identity", "[2 [600] 65 [600]]", "")
	for i := range objs {
		objs[i] = strings.ReplaceAll(objs[i], "/Calibri", "/Melior")
	}
	f := fixture{page: textPage, content: "0 g BT /F 20 Tf 5 70 Td <00410002> Tj ET", objs: objs}
	doc, pg := open(t, f.bytes(), limits.Limits{})
	opts := f.options()
	opts.SolidPlaceholders = true
	c, report, err := Render(context.Background(), doc, pg, opts)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	expect(t, c.Image(), 0, map[[2]int]rgb{{5, 25}: black, {11, 25}: black, {23, 25}: black, {40, 25}: paper})
	if n := report.Unsupported[PlaceholderGlyph]; n != 2 {
		t.Errorf("%d placeholders, want 2", n)
	}
}

func TestSubstituteByGlyphOrder(t *testing.T) {
	t.Parallel()
	// The codes of pdfjs issue15594_reduced: an Adobe-Identity Calibri
	// without /ToUnicode, whose CIDs are glyph indices of Calibri. Its glyph
	// order reads CIDs 9, 80, 109, 263, 386 and 442 as ÄÖÜäöü, which the
	// substitute holds. Each glyph is 10 wide at 20 points: x 5..15, 15..25
	// and so on to 65.
	var glyphs []fontbuild.Glyph
	for _, r := range "ÄÖÜäöü" {
		glyphs = append(glyphs, fontbuild.Glyph{Name: fmt.Sprintf("uni%04X", r), Unicode: r, Advance: 500})
	}
	program := fontbuild.TrueType{PostScriptName: "LithoUmlauts", Family: "LithoUmlauts", Subfamily: "Regular", Glyphs: glyphs}.Build()
	f := fixture{
		page:       textPage,
		content:    "0 g BT /F 20 Tf 5 70 Td <00090050006D0107018201BA> Tj ET",
		objs:       unembeddedType0("Identity-H", "Identity", "[9 [500] 80 [500] 109 [500] 263 [500] 386 [500] 442 [500]]", ""),
		substitute: program,
	}
	img, report := draw(t, f)
	want := map[[2]int]rgb{{4, 25}: paper, {66, 25}: paper}
	for i := range 6 {
		want[[2]int{10 + 10*i, 25}] = black
	}
	expect(t, img, 0, want)
	if n := report.Unsupported[PlaceholderGlyph]; n != 0 {
		t.Errorf("%d placeholders, want none", n)
	}
}
