package compliance

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// type1Glyph is one charstring of a [type1NamedGlyphProgram].
type type1Glyph struct {
	name  string
	width int
}

// type1WidthFont is a Type 1 font program with the /Length1 and /Length2 of
// its clear-text and encrypted sections; the rest is the /Length3 trailer.
type type1WidthFont struct {
	data         string
	clear, eexec int
}

// type1Trailer closes a Type 1 program: 512 zeros, then cleartomark.
var type1Trailer = "\n" + strings.Repeat(strings.Repeat("0", 64)+"\n", 8) + "cleartomark\n"

// type1NamedGlyphProgram builds a Type 1 font program with the given built-in
// encoding whose charstrings each set their advance with hsbw (Adobe Type 1 Font
// Format, 6.4), so the advance widths decode. The encrypted section is binary
// and carries the usual Private boilerplate, the form veraPDF reads. Widths up
// to 1131 fit the encoding used.
func type1NamedGlyphProgram(vector string, glyphs ...type1Glyph) type1WidthFont {
	clear := "%!PS-AdobeFont-1.0: ABCDEF+Test 001.000\n11 dict begin\n/FontInfo 2 dict dup begin\n" +
		"/FullName (Test) readonly def\nend readonly def\n/FontName /ABCDEF+Test def\n" + vector +
		"\n/PaintType 0 def\n/FontType 1 def\n/FontMatrix [0.001 0 0 0.001 0 0] readonly def\n" +
		"/FontBBox {0 0 1000 1000} readonly def\ncurrentdict end\ncurrentfile eexec\n"

	var plain strings.Builder
	// The first four plaintext bytes are eexec salt the decoder discards.
	plain.WriteString("saltdup /Private 8 dict dup begin\n/RD{string currentfile exch readstring pop}executeonly def\n" +
		"/ND{noaccess def}executeonly def\n/NP{noaccess put}executeonly def\n/MinFeature{16 16}def\n/password 5839 def\n")
	fmt.Fprintf(&plain, "/CharStrings %d dict dup begin\n", len(glyphs))
	for _, g := range glyphs {
		// lenIV salt, then "0 width hsbw endchar", charstring-encrypted.
		cs := []byte{0, 0, 0, 0, 139}
		if g.width <= 107 {
			cs = append(cs, byte(g.width+139)) //nolint:gosec // fixture widths are small
		} else {
			v := g.width - 108
			cs = append(cs, byte(v/256+247), byte(v%256)) //nolint:gosec // fixture widths stay below 1132
		}
		cs = append(cs, 13, 14)
		r := uint16(4330)
		for i, p := range cs {
			cs[i] = p ^ byte(r>>8)
			r = (uint16(cs[i])+r)*52845 + 22719
		}
		fmt.Fprintf(&plain, "/%s %d RD ", g.name, len(cs))
		plain.Write(cs)
		plain.WriteString(" ND\n")
	}
	plain.WriteString("end\nend\nreadonly put\nnoaccess put\ndup/FontName get exch definefont pop\nmark currentfile closefile\n")

	// Per ISO 32000-1, 9.9.1 the private section is eexec-encrypted with the
	// key and multipliers Adobe's Type 1 specification fixes.
	enc := []byte(plain.String())
	r := uint16(55665)
	for i, p := range enc {
		enc[i] = p ^ byte(r>>8)
		r = (uint16(enc[i])+r)*52845 + 22719
	}
	return type1WidthFont{data: clear + string(enc) + type1Trailer, clear: len(clear), eexec: len(enc)}
}

// type1EncodingCase is one font of [type1EncodingCases]: a Type 1 font showing
// code with the given /Flags, /Encoding and widths, and the number of times
// ISO 19005-2, 6.2.11.8 t1, 6.2.11.4.1 t2 and 6.2.11.5 t1 fail on it.
type type1EncodingCase struct {
	name      string
	flags     int
	encoding  string
	code      byte
	firstChar int
	widths    map[int]int
	notdef    int
	presence  int
	width     int
}

// type1EncodingVector is the built-in encoding of the case program: every code
// .notdef but the two it names.
const type1EncodingVector = "/Encoding 256 array\n0 1 255 {1 index exch /.notdef put} for\n" +
	"dup 32 /space put\ndup 65 /A put\nreadonly def"

// type1EncodingGlyphs are the charstrings of the case program.
var type1EncodingGlyphs = []type1Glyph{{".notdef", 500}, {"space", 250}, {"A", 722}}

var type1EncodingCases = []type1EncodingCase{
	{"a control code from WinAnsiEncoding, symbolic", 4, "/WinAnsiEncoding", 10, 32, nil, 1, 1, 1},
	{"a control code from WinAnsiEncoding", 32, "/WinAnsiEncoding", 10, 32, nil, 1, 1, 1},
	{"the control code inside /Widths at the .notdef advance", 32, "/WinAnsiEncoding", 10, 0, map[int]int{10: 500}, 1, 1, 0},
	{"the control code inside /Widths at another advance", 32, "/WinAnsiEncoding", 10, 0, map[int]int{10: 250}, 1, 1, 1},
	{"a WinAnsiEncoding bullet the program lacks", 32, "/WinAnsiEncoding", 0x81, 32, map[int]int{0x81: 500}, 0, 1, 1},
	{"WinAnsiEncoding DEL, a bullet the program lacks", 32, "/WinAnsiEncoding", 0x7F, 32, nil, 0, 1, 0},
	{"a control code from MacRomanEncoding", 32, "/MacRomanEncoding", 10, 32, nil, 1, 1, 1},
	{"a control code from MacRomanEncoding, symbolic", 4, "/MacRomanEncoding", 10, 32, nil, 1, 1, 1},
	{"a code MacRomanEncoding leaves undefined", 32, "/MacRomanEncoding", 0xAD, 32, map[int]int{0xAD: 500}, 1, 1, 0},
	{"a control code over a WinAnsiEncoding base", 32, "<< /BaseEncoding /WinAnsiEncoding /Differences [65 /A] >>", 10, 32, nil, 1, 1, 1},
	{"a control code from StandardEncoding", 32, "/StandardEncoding", 10, 32, nil, 0, 1, 1},
	{"a control code from StandardEncoding, symbolic", 4, "/StandardEncoding", 10, 32, nil, 0, 1, 1},
	{"a control code with no /Encoding", 32, "", 10, 32, nil, 0, 1, 1},
	{"a control code with no /Encoding, symbolic", 4, "", 10, 32, nil, 0, 1, 1},
	{"a control code under /Differences alone, symbolic", 4, "<< /Differences [65 /A] >>", 10, 32, nil, 0, 1, 1},
	{"a /Differences name the program lacks", 32, "<< /Differences [65 /blank] >>", 65, 32, nil, 0, 1, 1},
	{"a /Differences .notdef", 32, "<< /BaseEncoding /WinAnsiEncoding /Differences [65 /.notdef] >>", 65, 32, nil, 1, 1, 1},
	{"a code the program names", 4, "/WinAnsiEncoding", 65, 32, nil, 0, 0, 0},
}

// type1BareVectorCases run against a program whose built-in vector is a bare
// "256 array": a code it does not list names nothing, so its glyph is weighed
// at the /MissingWidth instead of the .notdef advance.
var type1BareVectorCases = []type1EncodingCase{
	{"a control code with no /Encoding, bare vector, symbolic", 4, "", 10, 32, nil, 0, 1, 0},
	{"a control code from StandardEncoding, bare vector", 32, "/StandardEncoding", 10, 32, nil, 0, 1, 0},
	{"a control code from WinAnsiEncoding, bare vector", 32, "/WinAnsiEncoding", 10, 32, nil, 1, 1, 1},
}

// objects returns the PDF objects of the case: a page showing the code in a
// Type 1 font embedding program.
func (c type1EncodingCase) objects(program type1WidthFont) []string {
	widths := map[int]int{32: 250, 65: 722}
	for code, w := range c.widths {
		widths[code] = w
	}
	lastChar := slices.Max([]int{65, int(c.code)})
	var arr strings.Builder
	for code := c.firstChar; code <= lastChar; code++ {
		fmt.Fprintf(&arr, " %d", widths[code])
	}
	encoding := ""
	if c.encoding != "" {
		encoding = " /Encoding " + c.encoding
	}
	body := fmt.Sprintf("BT /F1 12 Tf 10 50 Td <%02x> Tj ET", c.code)
	return []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(body), body),
		fmt.Sprintf("<< /Type /Font /Subtype /Type1 /BaseFont /ABCDEF+Test /FirstChar %d /LastChar %d /Widths [%s ]%s"+
			" /FontDescriptor 6 0 R >>", c.firstChar, lastChar, arr.String(), encoding),
		fmt.Sprintf("<< /Type /FontDescriptor /FontName /ABCDEF+Test /Flags %d /FontBBox [0 0 1000 1000] /ItalicAngle 0"+
			" /Ascent 800 /Descent -200 /CapHeight 700 /StemV 80 /FontFile 7 0 R >>", c.flags),
		fmt.Sprintf("<< /Length %d /Length1 %d /Length2 %d /Length3 %d >>\nstream\n%s\nendstream",
			len(program.data), program.clear, program.eexec, len(program.data)-program.clear-program.eexec, program.data),
	}
}

// TestType1EncodingNames pins how a Type 1 font's glyphs are named for ISO
// 19005-2, 6.2.11.8 t1 (.notdef), 6.2.11.4.1 t2 (presence) and 6.2.11.5 t1
// (widths). The name is the one the /Encoding gives the code (ISO 32000-1,
// 9.6.6.1), whatever the symbolic flag says: .notdef where a MacRomanEncoding,
// MacExpertEncoding or WinAnsiEncoding base leaves the code undefined, and no
// name at all under StandardEncoding, a /Differences-only dictionary or no
// /Encoding, where the program's built-in glyph is shown. A glyph the program
// does not define is weighed at the /MissingWidth, and so is a code outside
// /Widths. Every case was measured against veraPDF.
func TestType1EncodingNames(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	filled := type1NamedGlyphProgram(type1EncodingVector, type1EncodingGlyphs...)
	bare := type1NamedGlyphProgram(strings.Replace(type1EncodingVector, "0 1 255 {1 index exch /.notdef put} for\n", "", 1),
		type1EncodingGlyphs...)

	type run struct {
		type1EncodingCase
		program type1WidthFont
	}
	var runs []run
	for _, c := range type1EncodingCases {
		runs = append(runs, run{c, filled})
	}
	for _, c := range type1BareVectorCases {
		runs = append(runs, run{c, bare})
	}
	for _, tt := range runs {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t, tt.objects(tt.program)...)
			viols, err := ValidatePDFA(ctx, doc, models.PDFA2b)
			if err != nil {
				t.Fatalf("ValidatePDFA: %v", err)
			}
			for _, want := range []struct {
				rule Rule
				n    int
			}{
				{Rule_19005_2_6_2_11_8_t01, tt.notdef},
				{Rule_19005_2_6_2_11_4_1_t02, tt.presence},
				{Rule_19005_2_6_2_11_5_t01, tt.width},
			} {
				if got := countRuleErrs(t, viols, want.rule); got != want.n {
					t.Errorf("%s t%d fired %d times, want %d", want.rule.Clause, want.rule.TestNumber, got, want.n)
				}
			}
		})
	}
}
