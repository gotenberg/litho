package xlsx

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
)

// span is a compact form of a header span for the tests: its text or
// field and the font attributes the codes set.
type span struct {
	text         string
	field        sheet.Field
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
	theme := &ooxml.Theme{Colors: map[string]css.Color{"accent1": rgb(0x4472C4)}}
	b := span{family: "Calibri", size: 11}
	with := func(s span, f func(*span)) span { f(&s); return s }
	for _, tc := range []struct {
		name string
		in   string
		want [3][]span
	}{
		{"centre by default", "Hello", [3][]span{1: {with(b, func(s *span) { s.text = "Hello" })}}},
		{"parts and fields", "&LPage &P of &N&C&A&R&D &T&F&Z&F",
			[3][]span{
				{with(b, func(s *span) { s.text = "Page " }), with(b, func(s *span) { s.field = sheet.FieldPage }), with(b, func(s *span) { s.text = " of " }), with(b, func(s *span) { s.field = sheet.FieldPages })},
				{with(b, func(s *span) { s.field = sheet.FieldSheet })},
				{with(b, func(s *span) { s.field = sheet.FieldDate }), with(b, func(s *span) { s.text = " " }), with(b, func(s *span) { s.field = sheet.FieldTime }), with(b, func(s *span) { s.field = sheet.FieldFile }), with(b, func(s *span) { s.field = sheet.FieldPath })},
			}},
		{"font codes", `&"Arial,Bold Italic"&14A&"-,Regular"&BB&I&U&SC&E&XD&KFF0000E&K04-050F&&`,
			[3][]span{1: {
				with(b, func(s *span) { s.text, s.family, s.size, s.bold, s.italic = "A", "Arial", 14, true, true }),
				with(b, func(s *span) { s.text, s.size, s.bold = "B", 14, true }),
				with(b, func(s *span) { s.text, s.size, s.bold, s.italic, s.under, s.strike = "C", 14, true, true, sheet.Single, true }),
				with(b, func(s *span) {
					s.text, s.size, s.bold, s.italic, s.under, s.strike, s.script = "D", 14, true, true, sheet.Double, true, sheet.Superscript
				}),
				with(b, func(s *span) {
					s.text, s.size, s.bold, s.italic, s.under, s.strike, s.script, s.color = "E", 14, true, true, sheet.Double, true, sheet.Superscript, css.Color{R: 1, A: 1}
				}),
				with(b, func(s *span) {
					s.text, s.size, s.bold, s.italic, s.under, s.strike, s.script, s.color = "F&", 14, true, true, sheet.Double, true, sheet.Superscript, tint(rgb(0x4472C4), -0.5)
				}),
			}}},
		{"a part resets the font", "&B&LA&RB", [3][]span{{with(b, func(s *span) { s.text = "A" })}, nil, {with(b, func(s *span) { s.text = "B" })}}},
		{"a line break stays", "a\nb&12345c&", [3][]span{1: {with(b, func(s *span) { s.text = "a\nbc" })}}},
		{"dropped codes", "&Gx&O&Hy&\"Ar", [3][]span{1: {with(b, func(s *span) { s.text = "xy" })}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := parseHeaderFooter(tc.in, base, theme)
			for part := range 3 {
				if len(got[part]) != len(tc.want[part]) {
					t.Fatalf("part %d = %d spans %+v, want %d", part, len(got[part]), got[part], len(tc.want[part]))
				}
				for i, s := range got[part] {
					w := tc.want[part][i]
					f := s.Font
					if f == nil || s.Text != w.text || s.Field != w.field || f.Families[0] != w.family || f.Size != w.size || f.Bold != w.bold ||
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
