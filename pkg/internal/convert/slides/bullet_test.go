package slides

import (
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// TestText_Bullets checks bullets and numbers against the places Impress
// gives them (p2.pptx and p6.pptx of the measurement decks: Arial 20 in a
// box at 28.35, 28.35), under Impress's rules.
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
		{
			"hanging",
			[]Paragraph{bul("One", 36, -36, dot()), bul("Two", 72, -18, dot()), bul("Four", 10, -36, dot())},
			[][3]any{{"•", 28.35, 47.2}, {"One", 64.35, 48.33}, {"•", 82.35, 71.2}, {"Two", 100.35, 72.34}, {"•", 28.35, 95.2}, {"Four", 64.35, 96.35}},
		},
		{
			"indents",
			[]Paragraph{bul("Three", 36, 0, dot()), bul("Four", 36, 10, dot()), centred},
			[][3]any{{"•", 64.35, 47.2}, {"Three", 71.35, 48.33}, {"Four", 74.35, 72.34}, {"•", 124.7, 95.2}, {"Centred", 160.7, 96.35}},
		},
		{
			"numbers",
			[]Paragraph{bul("N1", 36, -36, num("arabicPeriod", 0)), bul("N2", 36, -36, num("arabicPeriod", 0)), sub, bul("N3", 36, -36, num("arabicPeriod", 0)), bul("R", 36, -36, num("romanUcPeriod", 4))},
			[][3]any{{"1.", 28.35, 48.33}, {"2.", 28.35, 72.34}, {"a)", 64.35, 96.35}, {"3.", 28.35, 120.36}, {"IV.", 28.35, 144.37}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := deck(&Slide{Shapes: []*Shape{box(28.35, 28.35, 300, 300, tc.paras...)}})
			doc.ImpressBullets = true
			pg := convert(t, doc, office.Options{}).Pages[0]
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

// TestText_PowerPointBullets locks how PowerPoint sets a character
// bullet: on its line's baseline, and, for a symbol font's code drawn in
// a face that stands in for it, at Office's advance, so that the text
// after a bullet wider than its indent starts where PowerPoint starts it
// and reads apart from the bullet where PowerPoint's reads apart.
//
// # Corpus evidence
//
//   - libreoffice/pptx: sd/n83889.pptx sets its Wingdings 0xA7 bullets at
//     18.025 points on their lines' baselines, 8.26 points wide, the text
//     joined to them; sd/presLeftAlign.pptx sets an Arial bullet on its
//     line's baseline, 8.15 points before its text.
//   - openxmlsdk/pptx: v2FxTestFiles/presentation/O12_templates
//     Stafftrain_TP10167128.potx sets its Wingdings 2 0x97 bullets at
//     22.35 points, 12.38 points wide, the text 8.55 points after them,
//     where OpenSymbol's glyph for the code advances a whole em.
func TestText_PowerPointBullets(t *testing.T) {
	p := para("One", arial(20))
	p.MarginLeft, p.Indent = 36, -36
	p.Bullet = &Bullet{Kind: BulletChar, Char: "•", Families: []string{"Arial"}}
	pg := convert(t, deck(&Slide{Shapes: []*Shape{box(28.35, 28.35, 300, 300, p)}}), office.Options{}).Pages[0]
	if b, one := find(t, pg, "•"), find(t, pg, "One"); !near(b.y, one.y, 0.01) || !near(b.x, 28.35, 0.3) || !near(one.x, 64.35, 0.3) {
		t.Errorf("bullet at %.2f, %.2f, text at %.2f, %.2f; want both on the baseline", b.x, b.y, one.x, one.y)
	}

	// A Wingdings 2 0x97 at 28 points: Office advances it 0.554 em,
	// 15.51 points, and the text, the bullet wider than its indent of 10
	// points, starts there.
	p = para("Two", arial(28))
	p.MarginLeft, p.Indent = 10, -10
	p.Bullet = &Bullet{Kind: BulletChar, Char: "\uf097", Families: []string{"Wingdings 2"}}
	pg = convert(t, deck(&Slide{Shapes: []*Shape{box(0, 0, 300, 300, p)}}), office.Options{}).Pages[0]
	if two := find(t, pg, "Two"); !near(two.x, 15.51, 0.05) {
		t.Errorf("text at %.2f, want 15.51 past the bullet's start", two.x)
	}
	var lbl *typeset.Text
	var glyph *typeset.Path
	var walk func([]typeset.Fragment)
	walk = func(fs []typeset.Fragment) {
		for _, f := range fs {
			switch v := f.(type) {
			case *typeset.Text:
				if v.Parent != nil && v.Parent.Role == "Lbl" {
					lbl = v
				}
			case *typeset.Path:
				if v.Parent == nil && v.Fill != nil && lbl == nil {
					glyph = v
				}
			case *typeset.Group:
				walk(v.Content)
			}
		}
	}
	walk(pg.Content)
	if lbl == nil || !lbl.Invisible || glyph == nil {
		t.Fatalf("bullet text %+v, glyph %v; want invisible text over the glyph's outline", lbl, glyph)
	}
	// OpenSymbol's glyph advances 1 em: the text reads 0.554 of it.
	if !near(lbl.HScale, 0.554, 0.002) {
		t.Errorf("bullet text scaled by %.3f, want 0.554", lbl.HScale)
	}
}

// TestText_PowerPointPictureBullet checks a picture bullet under
// PowerPoint's rules: seven tenths of the text size tall in whole
// hundredths of an inch, standing on the line's baseline.
func TestText_PowerPointPictureBullet(t *testing.T) {
	p := para("One", arial(20))
	p.MarginLeft, p.Indent = 36, -36
	p.Bullet = &Bullet{Kind: BulletPicture, Picture: tinyPicture(t)}
	pg := convert(t, deck(&Slide{Shapes: []*Shape{box(28.35, 28.35, 300, 300, p)}}), office.Options{}).Pages[0]
	var img typeset.Rect
	var walk func([]typeset.Fragment, typeset.Matrix)
	walk = func(fs []typeset.Fragment, m typeset.Matrix) {
		for _, f := range fs {
			switch v := f.(type) {
			case *typeset.Image:
				x0, y0 := apply(m, v.Rect.X, v.Rect.Y)
				x1, y1 := apply(m, v.Rect.X+v.Rect.Width, v.Rect.Y+v.Rect.Height)
				img = typeset.Rect{X: min(x0, x1), Y: min(y0, y1), Width: math.Abs(x1 - x0), Height: math.Abs(y1 - y0)}
			case *typeset.Group:
				tr := v.Transform
				if tr == (typeset.Matrix{}) {
					tr = typeset.Identity
				}
				walk(v.Content, then(tr, m))
			}
		}
	}
	walk(pg.Content, typeset.Identity)
	one := find(t, pg, "One")
	if !near(img.Height, 13.68, 0.01) || !near(img.Width, 13.68, 0.01) || !near(img.Y+img.Height, one.y, 0.05) || !near(img.X, 28.35, 0.3) {
		t.Errorf("bullet at %+v, text baseline %.2f; want 13.68 square on the baseline", img, one.y)
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
// glyph and reads as the Unicode character PowerPoint writes for it,
// whether the file writes the code or its private-use character, or as
// that private-use character under Impress's rules or where PowerPoint
// names no character (sd/bulletColor.pptx: a Wingdings "q").
func TestText_SymbolBulletText(t *testing.T) {
	for _, tc := range []struct {
		impress            bool
		char, family, text string
		name               string
	}{
		{false, "q", "Wingdings", "\u2751", "\u2751"},
		{false, "\uf071", "Wingdings", "\u2751", "\u2751"},
		{true, "q", "Wingdings", "\uf071", ""},
		{false, "q", "Wingdings 2", "\uf071", ""},
		{false, "n", "Wingdings", "\u25fc", "\u25fc"},
		{false, "\uf0fc", "Wingdings", "\u2713", "\u2713"},
		{false, "\u00d8", "Wingdings", "\u27a2", "\u27a2"},
		{false, "\uf0a8", "Wingdings", "\uf0a8", ""},
		{false, "\uf0b7", "Symbol", "\u2022", "\u2022"},
		{false, "\uf075", "Wingdings", "\u25c6", "\u25c6"},
	} {
		p := para("aaa", arial(18))
		p.MarginLeft, p.Indent = 18, -18
		p.Bullet = &Bullet{Kind: BulletChar, Char: tc.char, Families: []string{tc.family}}
		doc := deck(&Slide{Shapes: []*Shape{box(10, 10, 300, 100, p)}})
		doc.ImpressBullets = tc.impress
		out := convert(t, doc, office.Options{})
		pg := out.Pages[0]
		find(t, pg, tc.text)
		find(t, pg, "aaa")
		// Its Lbl reads as a character a reader can name, not the
		// private-use code.
		var lbl *typeset.StructElem
		var walk func(*typeset.StructElem)
		walk = func(e *typeset.StructElem) {
			if e.Role == "Lbl" && lbl == nil {
				lbl = e
			}
			for _, k := range e.Kids {
				walk(k)
			}
		}
		walk(out.Structure)
		if lbl == nil || lbl.ActualText == "" || []rune(lbl.ActualText)[0] >= 0xe000 && []rune(lbl.ActualText)[0] <= 0xf8ff {
			t.Errorf("Impress %v: bullet Lbl %+v", tc.impress, lbl)
		} else if tc.name != "" && lbl.ActualText != tc.name {
			t.Errorf("bullet Lbl reads %q, want %q", lbl.ActualText, tc.name)
		}
	}
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
		box    typeset.Rect
	}{
		{AnchorTop, typeset.Rect{X: 28.35, Y: 28.35, Width: 150, Height: 96}},
		{AnchorBottom, typeset.Rect{X: 28.35, Y: 28.35 - 46, Width: 150, Height: 96}},
		{AnchorMiddle, typeset.Rect{X: 28.35, Y: 28.35 - 23, Width: 150, Height: 96}},
	} {
		grow := box(28.35, 28.35, 150, 50, four...)
		grow.Text.Autofit, grow.Text.Anchor = AutofitGrow, tc.anchor
		grow.Line = Line{Width: 1, Color: red}
		pg := convert(t, deck(&Slide{Shapes: []*Shape{grow}}), office.Options{}).Pages[0]
		var ps []placedPath
		paths(pg.Content, typeset.Identity, &ps)
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

// TestText_SavedFit checks that a body with SavedFit takes the scale its
// file saved, overflowing or not, and never computes one.
func TestText_SavedFit(t *testing.T) {
	four := []Paragraph{para("L0", arial(20)), para("L1", arial(20)), para("L2", arial(20)), para("L3", arial(20))}
	for _, tc := range []struct {
		name  string
		scale float64
		size  float64
	}{{"none saved", 0, 20}, {"half", 0.5, 10}} {
		s := box(28.35, 28.35, 150, 50, four...)
		s.Text.Autofit, s.Text.SavedFit, s.Text.FontScale = AutofitShrink, true, tc.scale
		pg := convert(t, deck(&Slide{Shapes: []*Shape{s}}), office.Options{}).Pages[0]
		if r := find(t, pg, "L0"); !near(r.size, tc.size, 0.01) {
			t.Errorf("%s: %.2f points, want %.2f", tc.name, r.size, tc.size)
		}
	}
}

// TestText_BulletAboveItsLine checks that a bullet four times its text's
// size, which reaches above its line's top and so above its text body's,
// is not cut there.
func TestText_BulletAboveItsLine(t *testing.T) {
	p := para("One", arial(18))
	p.MarginLeft, p.Indent = 22.5, -22.5
	p.Bullet = &Bullet{Kind: BulletChar, Char: "•", Families: []string{"Arial"}, Scale: 4}
	pg := convert(t, deck(&Slide{Shapes: []*Shape{box(100, 100, 300, 30, p)}}), office.Options{}).Pages[0]
	var top float64
	found := false
	var walk func([]typeset.Fragment, typeset.Matrix, float64)
	walk = func(fs []typeset.Fragment, m typeset.Matrix, clipTop float64) {
		for _, f := range fs {
			switch v := f.(type) {
			case *typeset.Text:
				if v.Parent != nil && v.Parent.Role == "Lbl" {
					_, y := apply(m, v.Origin.X, v.Origin.Y)
					top, found = clipTop, true
					if y-0.6*72 < clipTop {
						t.Errorf("bullet of baseline %.2f cut at %.2f", y, clipTop)
					}
				}
			case *typeset.Group:
				tr := v.Transform
				if tr == (typeset.Matrix{}) {
					tr = typeset.Identity
				}
				inner := then(tr, m)
				ct := clipTop
				if v.Clip != nil {
					ct = clipMinY(v.Clip, inner, ct)
				}
				walk(v.Content, inner, ct)
			}
		}
	}
	walk(pg.Content, typeset.Identity, math.Inf(-1))
	if !found {
		t.Fatalf("no bullet; clip top %.2f", top)
	}
}

// clipMinY returns the larger of floor and the top of the clip path c
// mapped by m.
func clipMinY(c *typeset.Path, m typeset.Matrix, floor float64) float64 {
	y0 := math.Inf(1)
	for _, op := range c.Ops {
		if op.Kind == typeset.ClosePath {
			continue
		}
		_, y := apply(m, op.P[0].X, op.P[0].Y)
		y0 = min(y0, y)
	}
	return max(floor, y0)
}
