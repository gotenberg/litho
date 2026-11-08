package slides

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
)

// TestText_Bullets checks bullets and numbers against the places Impress
// gives them (p2.pptx and p6.pptx of the measurement decks: Arial 20 in a
// box at 28.35, 28.35).
func TestText_Bullets(t *testing.T) {
	a20 := arial(20)
	bul := func(s string, ml, ind float64, b *Bullet) Paragraph {
		p := para(s, a20)
		p.MarginLeft, p.Indent, p.Bullet = ml, ind, b
		return p
	}
	dot := func() *Bullet { return &Bullet{Kind: BulletChar, Char: "•", Families: []string{"Arial"}} }
	num := func(f string, start int) *Bullet { return &Bullet{Kind: BulletNumber, Format: f, StartAt: start} }
	centred := bul("Centred", 36, -36, dot())
	centred.Align = css.AlignCenter
	sub := bul("Sub", 72, -36, num("alphaLcParenR", 0))
	sub.Level = 1
	for _, tc := range []struct {
		name  string
		paras []Paragraph
		want  [][3]any
	}{
		{"hanging", []Paragraph{bul("One", 36, -36, dot()), bul("Two", 72, -18, dot()), bul("Four", 10, -36, dot())},
			[][3]any{{"•", 28.35, 47.2}, {"One", 64.35, 48.33}, {"•", 82.35, 71.2}, {"Two", 100.35, 72.34}, {"•", 28.35, 95.2}, {"Four", 64.35, 96.35}}},
		{"indents", []Paragraph{bul("Three", 36, 0, dot()), bul("Four", 36, 10, dot()), centred},
			[][3]any{{"•", 64.35, 47.2}, {"Three", 71.35, 48.33}, {"Four", 74.35, 72.34}, {"•", 124.7, 95.2}, {"Centred", 160.7, 96.35}}},
		{"numbers", []Paragraph{bul("N1", 36, -36, num("arabicPeriod", 0)), bul("N2", 36, -36, num("arabicPeriod", 0)), sub, bul("N3", 36, -36, num("arabicPeriod", 0)), bul("R", 36, -36, num("romanUcPeriod", 4))},
			[][3]any{{"1.", 28.35, 48.33}, {"2.", 28.35, 72.34}, {"a)", 64.35, 96.35}, {"3.", 28.35, 120.36}, {"IV.", 28.35, 144.37}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pg := convert(t, deck(&Slide{Shapes: []*Shape{box(28.35, 28.35, 300, 300, tc.paras...)}}), office.Options{}).Pages[0]
			rs := runs(pg)
			for _, w := range tc.want {
				ok := false
				for _, r := range rs {
					if r.text == w[0].(string) && near(r.x, w[1].(float64), 0.3) && near(r.y, w[2].(float64), 0.3) {
						ok = true
					}
				}
				if !ok {
					t.Errorf("no %q at %v, %v in %v", w[0], w[1], w[2], rs)
				}
			}
		})
	}
}

func TestFormatNumber(t *testing.T) {
	for _, tc := range []struct {
		format string
		n      int
		want   string
		ok     bool
	}{
		{"arabicPeriod", 3, "3.", true},
		{"arabicParenR", 12, "12)", true},
		{"arabicParenBoth", 1, "(1)", true},
		{"alphaLcPeriod", 28, "bb.", true},
		{"alphaUcParenR", 1, "A)", true},
		{"romanLcPeriod", 9, "ix.", true},
		{"romanUcPlain", 2024, "MMXXIV", true},
		{"hebrew2Minus", 5, "5.", false},
	} {
		if got, ok := formatNumber(tc.format, tc.n); got != tc.want || ok != tc.ok {
			t.Errorf("formatNumber(%q, %d) = %q, %v; want %q, %v", tc.format, tc.n, got, ok, tc.want, tc.ok)
		}
	}
}

// TestText_SymbolBulletText checks that a recoded bullet draws OpenSymbol's
// glyph and reads as its symbol font's private-use code
// (sd/tdf123090.pptx: a Wingdings "n").
func TestText_SymbolBulletText(t *testing.T) {
	p := para("aaa", arial(18))
	p.MarginLeft, p.Indent = 18, -18
	p.Bullet = &Bullet{Kind: BulletChar, Char: "n", Families: []string{"Wingdings"}}
	pg := convert(t, deck(&Slide{Shapes: []*Shape{box(10, 10, 300, 100, p)}}), office.Options{}).Pages[0]
	find(t, pg, "\uf06e")
	find(t, pg, "aaa")
}

// TestText_Autofit checks the scale levels a shrinking body takes and the
// growth of a growing one (p3.pptx, m3.fodp).
func TestText_Autofit(t *testing.T) {
	four := []Paragraph{para("L0", arial(20)), para("L1", arial(20)), para("L2", arial(20)), para("L3", arial(20))}
	shrink := box(28.35, 28.35, 150, 50, four...)
	shrink.Text.Autofit = AutofitShrink
	pg := convert(t, deck(&Slide{Shapes: []*Shape{shrink}}), office.Options{}).Pages[0]
	l0, l1 := find(t, pg, "L0"), find(t, pg, "L1")
	// The level of 62.5% and 80% spacing: 13 points on 12.48-point lines.
	if !near(l0.size, 13, 0.01) || !near(l1.y-l0.y, 12.48, 0.05) || !near(l0.y, 28.35+12.48, 0.1) {
		t.Errorf("shrunk to %.2f at %.2f, pitch %.2f", l0.size, l0.y, l1.y-l0.y)
	}
	// Empty paragraphs closing the body take no room in the fit, as
	// sd/font-scale.pptx prints.
	trailing := append(append([]Paragraph(nil), four...), Paragraph{Style: arial(20)}, Paragraph{Style: arial(20)})
	tail := box(28.35, 28.35, 150, 50, trailing...)
	tail.Text.Autofit = AutofitShrink
	pg = convert(t, deck(&Slide{Shapes: []*Shape{tail}}), office.Options{}).Pages[0]
	if r := find(t, pg, "L0"); !near(r.size, 13, 0.01) {
		t.Errorf("with empty closing paragraphs shrunk to %.2f, want 13", r.size)
	}
	// Empty lines keep their spacing as the fit scales the others, as
	// sd/autofitted-textbox-indent.odp prints: 18 points down to 11.
	ls := RunStyle{Families: []string{"Liberation Sans"}, Size: 18, Color: css.Color{A: 1}}
	gaps := box(28.35, 28.35, 300, 64.92, para("E0", ls), Paragraph{Style: ls}, Paragraph{Style: ls}, Paragraph{Style: ls}, para("E4", ls))
	gaps.Text.Autofit, gaps.Text.FontLineSpacing, gaps.Text.MaxParagraphSpacing = AutofitShrink, true, true
	pg = convert(t, deck(&Slide{Shapes: []*Shape{gaps}}), office.Options{}).Pages[0]
	if r := find(t, pg, "E0"); !near(r.size, 11, 0.01) {
		t.Errorf("with empty lines shrunk to %.2f, want 11", r.size)
	}
	for _, tc := range []struct {
		anchor Anchor
		box    compose.Rect
	}{
		{AnchorTop, compose.Rect{X: 28.35, Y: 28.35, Width: 150, Height: 96}},
		{AnchorBottom, compose.Rect{X: 28.35, Y: 28.35 - 46, Width: 150, Height: 96}},
		{AnchorMiddle, compose.Rect{X: 28.35, Y: 28.35 - 23, Width: 150, Height: 96}},
	} {
		grow := box(28.35, 28.35, 150, 50, four...)
		grow.Text.Autofit, grow.Text.Anchor = AutofitGrow, tc.anchor
		grow.Line = Line{Width: 1, Color: red}
		pg := convert(t, deck(&Slide{Shapes: []*Shape{grow}}), office.Options{}).Pages[0]
		var ps []placedPath
		paths(pg.Content, compose.Identity, &ps)
		if len(ps) != 1 || !rectNear(ps[0].bounds(), tc.box, 0.05) {
			t.Errorf("anchor %d: grown box %+v, want %+v", tc.anchor, ps[0].bounds(), tc.box)
		}
		if r := find(t, pg, "L0"); !near(r.y, tc.box.Y+20, 0.1) {
			t.Errorf("anchor %d: L0 at %.2f", tc.anchor, r.y)
		}
	}
}

// TestNumbers checks how numbered paragraphs count: per level and format,
// a start of 0 continuing, the same start continuing, another start
// restarting.
func TestNumbers(t *testing.T) {
	p := func(level, start int, format string) Paragraph {
		return Paragraph{Level: level, Runs: []Run{{Text: "x"}}, Bullet: &Bullet{Kind: BulletNumber, Format: format, StartAt: start}}
	}
	for _, tc := range []struct {
		name  string
		paras []Paragraph
		want  []int
	}{
		{"continue", []Paragraph{p(0, 0, "arabicPeriod"), p(0, 0, "arabicPeriod"), p(0, 0, "arabicPeriod")}, []int{1, 2, 3}},
		{"same-start", []Paragraph{p(0, 3, "arabicPeriod"), p(0, 3, "arabicPeriod")}, []int{3, 4}},
		{"zero-continues", []Paragraph{p(0, 3, "arabicPeriod"), p(0, 0, "arabicPeriod")}, []int{3, 4}},
		{"restart", []Paragraph{p(0, 1, "arabicPeriod"), p(0, 1, "arabicPeriod"), p(0, 7, "arabicPeriod")}, []int{1, 2, 7}},
		{"format", []Paragraph{p(0, 0, "arabicPeriod"), p(0, 0, "alphaLcParenR")}, []int{1, 1}},
		{"levels", []Paragraph{p(0, 0, "arabicPeriod"), p(1, 0, "arabicPeriod"), p(1, 0, "arabicPeriod"), p(0, 0, "arabicPeriod"), p(1, 0, "arabicPeriod")}, []int{1, 1, 2, 2, 1}},
	} {
		got := numbers(tc.paras)
		for i := range tc.want {
			if got[i] != tc.want[i] {
				t.Errorf("%s: numbers %v, want %v", tc.name, got, tc.want)
				break
			}
		}
	}
}
