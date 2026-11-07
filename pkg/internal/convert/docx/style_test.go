package docx

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
)

func TestRunProperties(t *testing.T) {
	t.Parallel()
	theme := `<a:theme xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><a:themeElements><a:clrScheme name="x">` +
		`<a:dk1><a:srgbClr val="000000"/></a:dk1><a:accent1><a:srgbClr val="4F81BD"/></a:accent1></a:clrScheme>` +
		`<a:fontScheme name="x"><a:majorFont><a:latin typeface="Major Face"/><a:ea typeface=""/><a:cs typeface=""/></a:majorFont>` +
		`<a:minorFont><a:latin typeface="Minor Face"/><a:ea typeface="EA Face"/><a:cs typeface=""/></a:minorFont></a:fontScheme></a:themeElements></a:theme>`
	styles := stylesPart(`<w:style w:type="character" w:styleId="Strong"><w:rPr><w:b/></w:rPr></w:style>` +
		`<w:style w:type="character" w:styleId="Emph"><w:basedOn w:val="Strong"/><w:rPr><w:i/></w:rPr></w:style>` +
		`<w:style w:type="paragraph" w:styleId="Big"><w:rPr><w:sz w:val="40"/><w:color w:val="00FF00"/></w:rPr></w:style>`)
	run := func(rpr, text string) string {
		return `<w:r><w:rPr>` + rpr + `</w:rPr><w:t xml:space="preserve">` + text + `</w:t></w:r>`
	}
	body := `<w:p>` +
		run(`<w:rStyle w:val="Emph"/>`, "chain") +
		run(`<w:b/><w:i/><w:sz w:val="28"/><w:color w:val="FF0000"/>`, "direct") +
		run(`<w:rFonts w:asciiTheme="minorHAnsi" w:eastAsiaTheme="minorEastAsia"/>`, "theme") +
		run(`<w:color w:val="auto" w:themeColor="accent1"/>`, "themecolor") +
		run(`<w:u w:val="double" w:color="0000FF"/><w:dstrike/>`, "lines") +
		run(`<w:caps/><w:highlight w:val="yellow"/>`, "caps") +
		run(`<w:smallCaps/><w:spacing w:val="40"/><w:w w:val="150"/>`, "small") +
		run(`<w:vertAlign w:val="superscript"/>`, "sup") +
		run(`<w:position w:val="6"/>`, "raised") +
		run(`<w:shd w:val="clear" w:color="auto" w:fill="000000"/>`, "dark") +
		run(`<w:shadow/>`, "shadow") +
		run(`<w:lang w:val="fr-FR"/>`, "langue") +
		run(`<w:vanish/>`, "HIDDEN") +
		`</w:p><w:p><w:pPr><w:pStyle w:val="Big"/></w:pPr>` + run(``, "styled") + run(`<w:b w:val="0"/><w:sz w:val="20"/>`, "unbold") + `</w:p>`
	b := build(t, body, map[string]string{"word/styles.xml": styles, "word/theme/theme1.xml": theme})
	if got := textOf(b.root); contains(got, "HIDDEN") {
		t.Errorf("hidden text built: %q", got)
	}
	for _, tc := range []struct {
		text  string
		check func(*css.Style) bool
	}{
		{"chain", func(s *css.Style) bool {
			return s.FontWeight == 700 && s.FontStyle == css.FontItalic && s.FontSize == 10
		}},
		{"direct", func(s *css.Style) bool {
			return s.FontWeight == 700 && s.FontStyle == css.FontItalic && s.FontSize == 14 && s.Color == css.Color{R: 1, A: 1}
		}},
		{"theme", func(s *css.Style) bool {
			// The high ANSI face the defaults set stays; the theme sets the
			// ASCII and East Asian ones.
			return len(s.FontFamilies) == 4 && s.FontFamilies[0] == "Minor Face" && s.FontFamilies[1] == "Test Sans" && s.FontFamilies[2] == "EA Face"
		}},
		{"themecolor", func(s *css.Style) bool { return s.Color.B > 0.7 && s.Color.R < 0.4 }},
		{"lines", func(s *css.Style) bool {
			return s.TextDecoration == css.Underline|css.LineThrough && s.TextDecorationStyle == css.DecorationDouble && s.TextDecorationColor == css.Color{B: 1, A: 1}
		}},
		{"caps", func(s *css.Style) bool {
			return s.TextTransform == css.Uppercase && s.BackgroundColor == css.Color{R: 1, G: 1, A: 1}
		}},
		{"small", func(s *css.Style) bool {
			return s.FontVariant == css.SmallCaps && s.LetterSpacing == css.Pts(2) && s.TextScale == 1.5
		}},
		{"sup", func(s *css.Style) bool {
			return s.VerticalAlign.Kind == css.AlignLength && s.VerticalAlign.Length.Value > 0 && s.FontSize < 10
		}},
		{"raised", func(s *css.Style) bool {
			return s.VerticalAlign == css.VerticalAlign{Kind: css.AlignLength, Length: css.Pts(3)}
		}},
		{"dark", func(s *css.Style) bool { return s.Color == css.Color{R: 1, G: 1, B: 1, A: 1} }},
		{"shadow", func(s *css.Style) bool { return len(s.TextShadow) == 1 }},
		{"styled", func(s *css.Style) bool { return s.FontSize == 20 && s.Color == css.Color{G: 1, A: 1} }},
		{"unbold", func(s *css.Style) bool { return s.FontWeight == 400 && s.FontSize == 10 }},
	} {
		if st := runWith(t, b.root, tc.text).Style; !tc.check(st) {
			t.Errorf("run %q: style %+v", tc.text, *st)
		}
	}
	if l := runWith(t, b.root, "langue").Lang; l != "fr-FR" {
		t.Errorf("lang = %q", l)
	}
}

// contains reports whether s holds sub.
func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestParagraphProperties(t *testing.T) {
	t.Parallel()
	settings := `<w:settings ` + nsDecl + `><w:defaultTabStop w:val="360"/></w:settings>`
	styles := stylesPart(`<w:style w:type="paragraph" w:styleId="Indented"><w:pPr><w:ind w:left="1440"/><w:spacing w:before="240" w:after="120"/></w:pPr></w:style>`)
	p := func(ppr, text string) string {
		return `<w:p><w:pPr>` + ppr + `</w:pPr><w:r><w:t>` + text + `</w:t></w:r></w:p>`
	}
	body := p(`<w:jc w:val="center"/>`, "center") +
		p(`<w:jc w:val="both"/><w:ind w:left="720" w:right="360" w:firstLine="360"/>`, "justify") +
		p(`<w:ind w:left="1440" w:hanging="720"/><w:tabs><w:tab w:val="right" w:leader="dot" w:pos="5000"/><w:tab w:val="bar" w:pos="3000"/><w:tab w:val="left" w:pos="100"/></w:tabs>`, "tabs") +
		p(`<w:spacing w:line="360" w:lineRule="exact"/>`, "exact") +
		p(`<w:spacing w:line="480" w:lineRule="atLeast"/>`, "atleast") +
		p(`<w:keepNext/><w:keepLines/><w:widowControl w:val="0"/>`, "keep") +
		p(`<w:bidi/>`, "rtl") +
		p(`<w:pBdr><w:top w:val="single" w:sz="8" w:space="2" w:color="FF0000"/><w:left w:val="double" w:sz="4" w:space="4"/></w:pBdr><w:shd w:val="clear" w:color="auto" w:fill="EEEEEE"/>`, "border") +
		p(`<w:pStyle w:val="Indented"/><w:ind w:left="360"/>`, "styled")
	b := build(t, body, map[string]string{"word/styles.xml": styles, "word/settings.xml": settings})
	para := func(text string) *css.Style {
		for _, n := range role(b.root, "P") {
			if textOf(n) == text {
				return n.Style
			}
		}
		t.Fatalf("no paragraph %q", text)
		return nil
	}
	for _, tc := range []struct {
		text  string
		check func(*css.Style) bool
	}{
		{"center", func(s *css.Style) bool {
			return s.TextAlign == css.AlignCenter && s.TabSize == css.Pts(18) && s.WhiteSpace == css.PreWrap
		}},
		{"justify", func(s *css.Style) bool {
			return s.TextAlign == css.AlignJustify && s.Margin[css.Left] == css.Pts(36) && s.Margin[css.Right] == css.Pts(18) && s.TextIndent == css.Pts(18)
		}},
		{"tabs", func(s *css.Style) bool {
			// Stops measured from the 72-point indent: the left stop before
			// it is dropped, the hanging indent adds one at 0.
			want := []css.TabStop{{Pos: 0}, {Pos: 78}, {Pos: 178, Align: css.TabRight, Leader: css.TabLeaderDots}}
			if len(s.TabStops) != len(want) {
				return false
			}
			for i := range want {
				if s.TabStops[i] != want[i] {
					return false
				}
			}
			return s.TextIndent == css.Pts(-36)
		}},
		{"exact", func(s *css.Style) bool { return s.LineHeight == css.Pts(18) }},
		{"atleast", func(s *css.Style) bool { return s.MinLineHeight == 24 && s.LineHeight.Kind == css.Normal }},
		{"keep", func(s *css.Style) bool {
			return s.BreakAfter == css.BreakAvoidPage && s.BreakInside == css.InsideAvoidPage && s.Orphans == 1 && s.Widows == 1
		}},
		{"rtl", func(s *css.Style) bool { return s.Direction == css.RTL }},
		{"border", func(s *css.Style) bool {
			return s.Border[css.Top] == css.Border{Width: 1, Style: css.Solid, Color: css.Color{R: 1, A: 1}} && s.Padding[css.Top] == css.Pts(2) &&
				s.Border[css.Left].Style == css.Double && s.BackgroundColor.R > 0.9
		}},
		{"styled", func(s *css.Style) bool { return s.Margin[css.Left] == css.Pts(18) && s.Margin[css.Top] == css.Pts(12) }},
	} {
		if st := para(tc.text); !tc.check(st) {
			t.Errorf("paragraph %q: style %+v", tc.text, *st)
		}
	}
}

func TestParagraphSpacing(t *testing.T) {
	t.Parallel()
	styles := stylesPart(`<w:style w:type="paragraph" w:styleId="List"><w:pPr><w:spacing w:before="120" w:after="240"/><w:contextualSpacing/></w:pPr></w:style>`)
	p := func(style, text string) string {
		return `<w:p><w:pPr><w:pStyle w:val="` + style + `"/></w:pPr><w:r><w:t>` + text + `</w:t></w:r></w:p>`
	}
	b := build(t, p("List", "a")+p("List", "b")+p("Normal", "c"), map[string]string{"word/styles.xml": styles})
	ps := role(b.root, "P")
	want := [][2]float64{{6, 0}, {0, 12}, {0, 0}}
	for i, n := range ps {
		if n.Style.Margin[css.Top].Value != want[i][0] || n.Style.Margin[css.Bottom].Value != want[i][1] {
			t.Errorf("paragraph %d margins %v / %v, want %v", i, n.Style.Margin[css.Top], n.Style.Margin[css.Bottom], want[i])
		}
	}
}

func TestEmptyParagraphKeepsALine(t *testing.T) {
	t.Parallel()
	b := build(t, `<w:p/><w:p><w:pPr><w:rPr><w:sz w:val="48"/></w:rPr></w:pPr></w:p>`, nil)
	ps := role(b.root, "P")
	if len(ps) != 2 {
		t.Fatalf("%d paragraphs", len(ps))
	}
	strut := find(ps[1], func(n *layout.Node) bool { return n.Kind == layout.Text })
	if len(strut) != 1 || strut[0].Style.FontSize != 24 {
		t.Errorf("empty paragraph strut %+v", strut)
	}
}

func TestParagraphSpacing_AtBreaks(t *testing.T) {
	t.Parallel()
	styles := stylesPart(`<w:style w:type="paragraph" w:styleId="Head"><w:pPr><w:spacing w:before="480" w:after="0"/></w:pPr></w:style>` +
		`<w:style w:type="paragraph" w:styleId="Body"><w:pPr><w:spacing w:after="200"/></w:pPr></w:style>`)
	p := func(style, text string) string {
		return `<w:p><w:pPr><w:pStyle w:val="` + style + `"/></w:pPr><w:r><w:t>` + text + `</w:t></w:r></w:p>`
	}
	body := p("Body", "a") + `<w:p><w:r><w:br w:type="page"/></w:r></w:p>` + p("Head", "after break") +
		p("Body", "b") + `<w:p><w:pPr><w:sectPr/></w:pPr></w:p>` + p("Head", "new section") + `<w:sectPr/>`
	b := build(t, body, map[string]string{"word/styles.xml": styles})
	ps := role(b.root, "P")
	var got []string
	for _, n := range ps {
		got = append(got, textOf(n))
	}
	if len(ps) != 4 {
		t.Fatalf("paragraphs %q: the page break paragraph leaves none", got)
	}
	// The hard break drops the space before; the section's first page
	// keeps what the previous paragraph's space after did not give.
	if ps[1].Style.BreakBefore != css.Page || ps[1].Style.Margin[css.Top] != css.Pts(0) {
		t.Errorf("after break: break %v margin %v", ps[1].Style.BreakBefore, ps[1].Style.Margin[css.Top])
	}
	if m := ps[3].Style.Margin[css.Top]; m != css.Pts(14) {
		t.Errorf("new section margin %v, want 14", m)
	}
}
