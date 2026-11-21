package xlsx

import (
	"math"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// span is a compact form of a header span for the tests: its text or
// field and the font attributes the codes set.
type span struct {
	text         string
	field        sheet.Field
	offset       int
	family       string
	size         float64
	bold, italic bool
	under        sheet.Underline
	strike       bool
	script       sheet.Script
	color        css.Color
}

// TestParseHeaderFooter checks the parts, the fields and the formatting
// codes of a header or footer text.
func TestParseHeaderFooter(t *testing.T) {
	t.Parallel()
	base := sheet.Font{Families: []string{"Calibri"}, Size: 11}
	theme := &ooxml.Theme{Colors: map[string]css.Color{"accent1": rgb(0x4472C4), "folHlink": rgb(0x954F72)}, Major: ooxml.FontScheme{Latin: "Cambria"}, Minor: ooxml.FontScheme{Latin: "Corbel"}}
	b := span{family: "Calibri", size: 11}
	with := func(s span, f func(*span)) span { f(&s); return s }
	for _, tc := range []struct {
		name string
		in   string
		want [3][]span
	}{
		{"centre by default", "Hello", [3][]span{1: {with(b, func(s *span) { s.text = "Hello" })}}},
		{
			"parts and fields", "&LPage &P of &N&C&A&R&D &T&F&Z&F",
			[3][]span{
				{with(b, func(s *span) { s.text = "Page " }), with(b, func(s *span) { s.field = sheet.FieldPage }), with(b, func(s *span) { s.text = " of " }), with(b, func(s *span) { s.field = sheet.FieldPages })},
				{with(b, func(s *span) { s.field = sheet.FieldSheet })},
				{with(b, func(s *span) { s.field = sheet.FieldDate }), with(b, func(s *span) { s.text = " " }), with(b, func(s *span) { s.field = sheet.FieldTime }), with(b, func(s *span) { s.field = sheet.FieldFile }), with(b, func(s *span) { s.field = sheet.FieldPath })},
			},
		},
		{
			"font codes", `&"Arial,Bold Italic"&14A&"Calibri,Regular"&BB&I&U&SC&E&XD&KFF0000E&K04-050F&&`,
			[3][]span{1: {
				with(b, func(s *span) { s.text, s.family, s.size, s.bold, s.italic = "A", "Arial", 14, true, true }),
				with(b, func(s *span) { s.text, s.size, s.bold = "B", 14, true }),
				with(b, func(s *span) {
					s.text, s.size, s.bold, s.italic, s.under, s.strike = "C", 14, true, true, sheet.Single, true
				}),
				with(b, func(s *span) {
					s.text, s.size, s.bold, s.italic, s.under, s.strike, s.script = "D", 14, true, true, sheet.Double, true, sheet.Superscript
				}),
				with(b, func(s *span) {
					s.text, s.size, s.bold, s.italic, s.under, s.strike, s.script, s.color = "E", 14, true, true, sheet.Double, true, sheet.Superscript, css.Color{R: 1, A: 1}
				}),
				with(b, func(s *span) {
					s.text, s.size, s.bold, s.italic, s.under, s.strike, s.script, s.color = "F&", 14, true, true, sheet.Double, true, sheet.Superscript, tint(rgb(0x4472C4), -0.5)
				}),
			}},
		},
		{"a part resets the font", "&B&LA&RB", [3][]span{{with(b, func(s *span) { s.text = "A" })}, nil, {with(b, func(s *span) { s.text = "B" })}}},
		{
			"a repeated part starts over", "&L&BA&CB&LC&L&RD&R",
			[3][]span{
				{with(b, func(s *span) { s.text, s.bold = "A", true }), with(b, func(s *span) { s.field = sheet.FieldRestart }), with(b, func(s *span) { s.text = "C" }), with(b, func(s *span) { s.field = sheet.FieldRestart })},
				{with(b, func(s *span) { s.text = "B" })},
				{with(b, func(s *span) { s.text = "D" }), with(b, func(s *span) { s.field = sheet.FieldRestart })},
			},
		},
		{"a line break stays", "a\nb&c&", [3][]span{1: {with(b, func(s *span) { s.text = "a\nb" })}}},
		{
			"sizes held between 1 and 409 points", "&12345a&0b&0123c&8 9",
			[3][]span{1: {
				with(b, func(s *span) { s.text, s.size = "a", 409 }), with(b, func(s *span) { s.text, s.size = "b", 1 }),
				with(b, func(s *span) { s.text, s.size = "c", 123 }), with(b, func(s *span) { s.text, s.size = " 9", 8 }),
			}},
		},
		{"dropped codes", "&Gx&O&Hy&\"Ar", [3][]span{1: {with(b, func(s *span) { s.text = "xy" })}}},
		{
			"theme fonts", `&"+,Bold"A&"Arial"B&"-"C`,
			[3][]span{1: {
				with(b, func(s *span) { s.text, s.family, s.bold = "A", "Cambria", true }),
				with(b, func(s *span) { s.text, s.family, s.bold = "B", "Arial", true }),
				with(b, func(s *span) { s.text, s.family, s.bold = "C", "Corbel", true }),
			}},
		},
		{
			"style words", `&"Arial,bold"A&"Arial, Bold  ITALIC "B&"Arial,Fett"C&"Arial,Italic Bold"D&"Arial,italic"E`,
			[3][]span{1: {
				with(b, func(s *span) { s.text, s.family, s.bold = "A", "Arial", true }),
				with(b, func(s *span) { s.text, s.family, s.bold, s.italic = "B", "Arial", true, true }),
				with(b, func(s *span) { s.text, s.family = "C", "Arial" }),
				with(b, func(s *span) { s.text, s.family = "D", "Arial" }),
				with(b, func(s *span) { s.text, s.family, s.italic = "E", "Arial", true }),
			}},
		},
		{
			"theme colours past their bounds", "&K12+000a&K04+150b&K04-101c&K04-100d",
			[3][]span{1: {
				with(b, func(s *span) { s.text, s.color = "a", rgb(0x954F72) }),
				with(b, func(s *span) { s.text, s.color = "b", rgb(0x4472C4) }),
				with(b, func(s *span) { s.text, s.color = "c", rgb(0x4472C4) }),
				with(b, func(s *span) { s.text, s.color = "d", tint(rgb(0x4472C4), -1) }),
			}},
		},
		{
			"superscript then subscript", "x&Xa&Yb&Yc&Xd",
			[3][]span{1: {
				with(b, func(s *span) { s.text = "x" }), with(b, func(s *span) { s.text, s.script = "a", sheet.Superscript }),
				with(b, func(s *span) { s.text, s.script = "b", sheet.Subscript }), with(b, func(s *span) { s.text = "c" }),
				with(b, func(s *span) { s.text, s.script = "d", sheet.Superscript }),
			}},
		},
		{
			"lower case and unknown codes", "&l&p&C&bb&b&Q|&O|&H|&J&R&&&&",
			[3][]span{
				{with(b, func(s *span) { s.field = sheet.FieldPage })},
				{with(b, func(s *span) { s.text, s.bold = "b", true }), with(b, func(s *span) { s.text = "|||" })},
				{with(b, func(s *span) { s.text = "&&" })},
			},
		},
		{"an unclosed font name", `&Lab&"Arial cd&Cef`, [3][]span{{with(b, func(s *span) { s.text = "ab" })}}},
		{"an unknown code drops its whole character", "&éa&€b&K€0000c", [3][]span{1: {with(b, func(s *span) { s.text = "ab€0000c" })}}},
		{
			"page offsets", "&P+1 of &N&P+1&N&P+12abc&p+3&N-2",
			[3][]span{1: {
				with(b, func(s *span) { s.field, s.offset = sheet.FieldPage, 1 }), with(b, func(s *span) { s.text = " of " }), with(b, func(s *span) { s.field = sheet.FieldPages }),
				with(b, func(s *span) { s.field, s.offset = sheet.FieldPage, 1 }), with(b, func(s *span) { s.field = sheet.FieldPages }),
				with(b, func(s *span) { s.field, s.offset = sheet.FieldPage, 12 }), with(b, func(s *span) { s.text = "abc" }),
				with(b, func(s *span) { s.field, s.offset = sheet.FieldPage, 3 }), with(b, func(s *span) { s.field, s.offset = sheet.FieldPages, -2 }),
			}},
		},
		{
			"an offset before a size", "&P+1&14x",
			[3][]span{1: {with(b, func(s *span) { s.field, s.offset = sheet.FieldPage, 1 }), with(b, func(s *span) { s.text, s.size = "x", 14 })}},
		},
		{
			"no digit after the sign", "&P +1&P+ 1&N-",
			[3][]span{1: {
				with(b, func(s *span) { s.field = sheet.FieldPage }), with(b, func(s *span) { s.text = " +1" }),
				with(b, func(s *span) { s.field = sheet.FieldPage }), with(b, func(s *span) { s.text = " 1" }),
				with(b, func(s *span) { s.field = sheet.FieldPages }),
			}},
		},
		{
			"offsets wrapped in 32 bits", "&P+99999999999999999999999&N-1000000000&P+2147483648x",
			[3][]span{1: {
				with(b, func(s *span) { s.field, s.offset = sheet.FieldPage, -159383553 }), with(b, func(s *span) { s.field, s.offset = sheet.FieldPages, -1000000000 }),
				with(b, func(s *span) { s.field, s.offset = sheet.FieldPage, math.MinInt32 }), with(b, func(s *span) { s.text = "x" }),
			}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, picture := parseHeaderFooter(tc.in, base, theme)
			if picture != strings.Contains(tc.in, "&G") {
				t.Errorf("picture %v", picture)
			}
			for part := range 3 {
				if len(got[part]) != len(tc.want[part]) {
					t.Fatalf("part %d = %d spans %+v, want %d", part, len(got[part]), got[part], len(tc.want[part]))
				}
				for i, s := range got[part] {
					w := tc.want[part][i]
					f := s.Font
					if f == nil || s.Text != w.text || s.Field != w.field || s.Offset != w.offset || f.Families[0] != w.family || f.Size != w.size || f.Bold != w.bold ||
						f.Italic != w.italic || f.Underline != w.under || f.Strike != w.strike || f.Script != w.script || !near(f.Color, w.color) {
						t.Errorf("part %d span %d = %+v font %+v, want %+v", part, i, s, f, w)
					}
				}
			}
		})
	}
}

// TestRead_HeaderFooter checks the odd, even and first page texts with
// their flags, the even and first pages printing nothing when their texts
// are empty.
func TestRead_HeaderFooter(t *testing.T) {
	t.Parallel()
	doc, _ := readBytes(t, oneSheet(t, `<sheetData/><headerFooter differentFirst="1" differentOddEven="1" scaleWithDoc="1">`+
		`<oddHeader>&amp;CO</oddHeader><oddFooter>&amp;RPage &amp;P</oddFooter><evenHeader>E</evenHeader><firstFooter>F</firstFooter></headerFooter>`))
	hf := doc.Sheets[0].HeaderFooter
	if !hf.DifferentFirst || !hf.DifferentOddEven || hf.ScaleWithDoc {
		t.Errorf("flags %+v", hf)
	}
	if len(hf.Odd.Header[1]) != 1 || hf.Odd.Header[1][0].Text != "O" || len(hf.Odd.Footer[2]) != 2 {
		t.Errorf("odd %+v", hf.Odd)
	}
	if len(hf.Even.Header[1]) != 1 || len(hf.Even.Footer[2]) != 0 {
		t.Errorf("even %+v, want its own header and an empty footer", hf.Even)
	}
	if len(hf.First.Header[1]) != 0 || len(hf.First.Footer[1]) != 1 {
		t.Errorf("first %+v, want an empty header and its own footer", hf.First)
	}
	if f := hf.Odd.Header[1][0].Font; f == nil || f.Families[0] != "Test Sans" {
		t.Errorf("header font %+v, want the Normal font", f)
	}
}

// TestRead_HeaderFooterPictures checks that the header pictures of a
// printed sheet, which are not drawn, are reported once, and that a
// doubled ampersand before G is no picture.
func TestRead_HeaderFooterPictures(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, header string
		want         int
	}{
		{"a picture", `&amp;L&amp;G&amp;Ctext`, 1},
		{"an ampersand and a G", `&amp;&amp;G`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, warnings := readBytes(t, oneSheet(t, `<sheetData/><headerFooter differentFirst="1"><oddHeader>`+tc.header+`</oddHeader><firstFooter>`+tc.header+`</firstFooter></headerFooter>`))
			got := 0
			for _, w := range warnings {
				if w.Code == warn.ContentUnsupported && w.Effect == warn.Omitted && strings.Contains(w.Message, "headers and footers") {
					got++
				}
			}
			if got != tc.want {
				t.Errorf("%d warnings %+v, want %d", got, warnings, tc.want)
			}
		})
	}
}

// TestRead_HeaderKeepsTheMargins checks that a header and a footer, tall
// or not, leave the page margins as they are: no dynamic height.
func TestRead_HeaderKeepsTheMargins(t *testing.T) {
	t.Parallel()
	doc, _ := readBytes(t, oneSheet(t, `<sheetData/><pageMargins left="0.7" right="0.7" top="1" bottom="1" header="0.3" footer="0.3"/>`+
		`<headerFooter><oddHeader>&amp;C&amp;123Big</oddHeader><oddFooter>&amp;CPage &amp;P</oddFooter></headerFooter>`))
	hf := doc.Sheets[0].HeaderFooter
	if hf.GrowHeader || hf.GrowFooter || hf.HeaderSpacing != 0 || hf.FooterSpacing != 0 {
		t.Errorf("header grows %v %v by %v %v, want the margins kept", hf.GrowHeader, hf.GrowFooter, hf.HeaderSpacing, hf.FooterSpacing)
	}
}
