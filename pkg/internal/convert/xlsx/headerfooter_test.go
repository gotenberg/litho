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
	picture      bool
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
		{"dropped codes", "x&O&Hy&\"Ar", [3][]span{1: {with(b, func(s *span) { s.text = "xy" })}}},
		{"a picture", "&Lx&Gy", [3][]span{0: {with(b, func(s *span) { s.text = "x" }), {picture: true}, with(b, func(s *span) { s.text = "y" })}}},
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
					if w.picture || s.Picture != nil {
						if !w.picture || s.Picture == nil || s.Text != "" {
							t.Errorf("part %d span %d = %+v, want a picture %v", part, i, s, w.picture)
						}
						continue
					}
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
	if !hf.DifferentFirst || !hf.DifferentOddEven || !hf.ScaleWithDoc {
		t.Errorf("flags %+v", hf)
	}
	for xml, want := range map[string]bool{`<headerFooter/>`: true, `<headerFooter scaleWithDoc="0"/>`: false} {
		doc, _ := readBytes(t, oneSheet(t, `<sheetData/>`+xml))
		if got := doc.Sheets[0].HeaderFooter.ScaleWithDoc; got != want {
			t.Errorf("%s: scaleWithDoc %t, want %t", xml, got, want)
		}
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

// TestRead_HeaderFooterPictures checks that a header picture the sheet's
// header drawing lacks is reported once, and that a doubled ampersand
// before G is no picture.
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
				if w.Code == warn.ContentUnsupported && w.Effect == warn.Omitted && strings.Contains(w.Message, "header or footer picture") {
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

// TestHeaderPictureSize locks the size Excel prints a header picture at
// under Calibri 11: its screen pixels scaled by the digit's 47 printer
// pixels over its 7 screen pixels across and the standard row's 121 over
// 20 down, and a picture of 100,000 pixels a side held to a page side
// before it is scaled.
func TestHeaderPictureSize(t *testing.T) {
	r := &reader{digitWidth: 47 * printerPixel, screenDigit: 7, standardPrinted: 121, standardScreen: 20}
	for _, tc := range []struct{ w, h, ww, wh float64 }{
		{120, 45, 128.88, 43.56},
		{90, 22.5, 96.72, 21.84},
		{75000, 75000, 14400, 13939.2},
	} {
		if w, h := r.headerPictureSize(tc.w, tc.h); math.Abs(w-tc.ww) > 1e-6 || math.Abs(h-tc.wh) > 1e-6 {
			t.Errorf("%v by %v points prints %v by %v, want %v by %v", tc.w, tc.h, w, h, tc.ww, tc.wh)
		}
	}
}

// TestRead_HeaderPictureParts checks that the shapes of the header drawing
// go to the parts their ids name, the even page's apart.
func TestRead_HeaderPictureParts(t *testing.T) {
	t.Parallel()
	sh := testSheet{
		name: "S",
		xml: `<sheetData/><headerFooter differentOddEven="1"><oddHeader>&amp;L&amp;G&amp;Cx</oddHeader><oddFooter>&amp;C&amp;G</oddFooter><evenHeader>&amp;R&amp;G</evenHeader></headerFooter>` +
			`<legacyDrawingHF r:id="rIdH"/>`,
		rels: `<Relationship Id="rIdH" Type="` + relVMLDrawing + `" Target="../drawings/vmlDrawing1.vml"/>`,
	}
	shape := func(id string) string {
		return `<v:shape id="` + id + `" style="position:absolute;width:30pt;height:15pt"><v:imagedata o:relid="rIdP"/></v:shape>`
	}
	files := workbookFiles([]testSheet{sh}, minimalStyles, "", "", map[string]string{
		"xl/drawings/vmlDrawing1.vml": `<xml xmlns:v="urn:schemas-microsoft-com:vml" xmlns:o="urn:schemas-microsoft-com:office:office" xmlns:x="urn:schemas-microsoft-com:office:excel">` +
			shape("LH") + shape("CF") + shape("RHEVEN") + `</xml>`,
		"xl/drawings/_rels/vmlDrawing1.vml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rIdP" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="../media/image1.png"/></Relationships>`,
		"xl/media/image1.png": pngBytes(t),
	})
	doc, warnings := readBytes(t, zipFiles(t, files))
	if len(warnings) > 0 {
		t.Errorf("warnings %v", warnings)
	}
	hf := doc.Sheets[0].HeaderFooter
	for _, p := range []*sheet.Span{&hf.Odd.Header[0][0], &hf.Odd.Footer[1][0], &hf.Even.Header[2][0]} {
		if p.Picture == nil || p.Picture.Picture == nil || p.Picture.Width <= 0 || p.Picture.Height <= 0 {
			t.Errorf("span %+v, want a picture", p)
		}
	}
}
