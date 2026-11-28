package slides

import (
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// TestText_LinePlacement checks the baselines of PPTX text against the
// numbers Impress prints (p1.pptx and p2.pptx of the measurement decks:
// Arial at 20 and 40 points in a box without insets at 28.35, 28.35).
func TestText_LinePlacement(t *testing.T) {
	a20 := arial(20)
	two := Paragraph{Runs: []Run{{Text: "Hxg two", Style: a20}, {Break: true, Style: a20}, {Text: "Hxg three", Style: a20}}, Style: a20}
	spaced := func(p Paragraph, before, after float64) Paragraph {
		p.SpaceBefore, p.SpaceAfter = before, after
		return p
	}
	lined := func(p Paragraph, prop, pts float64) Paragraph {
		p.LineSpacing, p.LineSpacingPoints = prop, pts
		return p
	}
	pct := func(p Paragraph, before float64) Paragraph {
		p.SpaceBeforePct = before
		return p
	}
	for _, tc := range []struct {
		name  string
		paras []Paragraph
		want  map[string]float64
	}{
		{"single", []Paragraph{para("Hxg one", a20), two, para("Hxg four", a20)}, map[string]float64{"Hxg one": 48.33, "Hxg two": 72.34, "Hxg three": 96.35, "Hxg four": 120.36}},
		{"forty", []Paragraph{para("Hxg one", arial(40)), para("Hxg four", arial(40))}, map[string]float64{"Hxg one": 68.34, "Hxg four": 116.33}},
		{"prop150", []Paragraph{lined(para("A1", a20), 1.5, 0), lined(para("A2", a20), 1.5, 0)}, map[string]float64{"A1": 60.32, "A2": 96.32}},
		{"prop80", []Paragraph{lined(para("A1", a20), 0.8, 0), lined(para("A2", a20), 0.8, 0)}, map[string]float64{"A1": 43.68, "A2": 62.9}},
		{"exact30", []Paragraph{lined(para("A1", a20), 0, 30), lined(para("A2", a20), 0, 30)}, map[string]float64{"A1": 54.31, "A2": 84.30}},
		// A closing line break leaves a line as tall as the end mark's
		// size (sd-data/n759180.pptx: 13 points, end mark 5).
		{"closing-break", []Paragraph{{Runs: []Run{{Text: "B0", Style: a20}, {Break: true, Style: a20}}, Style: arial(5)}, para("B1", a20)}, map[string]float64{"B0": 48.33, "B1": 78.33}},
		{"spacing", []Paragraph{spaced(para("P0", a20), 10, 5), spaced(para("P1", a20), 10, 5), spaced(para("P2", a20), 10, 5)}, map[string]float64{"P0": 48.33, "P1": 87.33, "P2": 126.34}},
		// A percentage spacing is a share of the 24-point single line.
		{"spacing-percent", []Paragraph{pct(para("P0", a20), 0.2), pct(para("P1", a20), 0.2), pct(para("P2", a20), 0.2)}, map[string]float64{"P0": 48.33, "P1": 77.13, "P2": 105.93}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := deck(&Slide{Shapes: []*Shape{box(28.35, 28.35, 340, 340, tc.paras...)}})
			pg := convert(t, doc, office.Options{}).Pages[0]
			for s, y := range tc.want {
				r := find(t, pg, s)
				if !near(r.y, y, 0.2) || !near(r.x, 28.35, 0.2) {
					t.Errorf("%s at %.2f, %.2f, want 28.35, %.2f", s, r.x, r.y, y)
				}
			}
		})
	}
}

// TestText_FontLinePlacement checks the baselines of OpenDocument text,
// lines following the faces' metrics and paragraphs spaced by the larger
// of the two spacings, against the numbers Impress prints (m1.fodp and
// m2.fodp of the measurement decks: Liberation Sans 20 in a frame without
// padding at 28.35, 28.35).
func TestText_FontLinePlacement(t *testing.T) {
	ls := RunStyle{Families: []string{"Liberation Sans"}, Size: 20, Color: css.Color{A: 1}}
	spaced := func(p Paragraph, before, after float64) Paragraph {
		p.SpaceBefore, p.SpaceAfter = before, after
		return p
	}
	lined := func(p Paragraph, prop, pts float64) Paragraph {
		p.LineSpacing, p.LineSpacingPoints = prop, pts
		return p
	}
	for _, tc := range []struct {
		name  string
		paras []Paragraph
		want  map[string]float64
	}{
		{"single", []Paragraph{para("P0", ls), para("P1", ls)}, map[string]float64{"P0": 46.46, "P1": 68.77}},
		{"prop150", []Paragraph{lined(para("P0", ls), 1.5, 0), lined(para("P1", ls), 1.5, 0)}, map[string]float64{"P0": 57.6, "P1": 91.05}},
		{"exact30", []Paragraph{lined(para("P0", ls), 0, 30), lined(para("P1", ls), 0, 30)}, map[string]float64{"P0": 54.15, "P1": 84.14}},
		{"max", []Paragraph{spaced(para("P0", ls), 5, 10), spaced(para("P1", ls), 5, 10), spaced(para("P2", ls), 5, 10)}, map[string]float64{"P0": 46.46, "P1": 78.78, "P2": 111.09}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sh := box(28.35, 28.35, 340, 340, tc.paras...)
			sh.Text.FontLineSpacing, sh.Text.MaxParagraphSpacing = true, true
			pg := convert(t, deck(&Slide{Shapes: []*Shape{sh}}), office.Options{}).Pages[0]
			for s, y := range tc.want {
				if r := find(t, pg, s); !near(r.y, y, 0.2) {
					t.Errorf("%s at %.2f, want %.2f", s, r.y, y)
				}
			}
		})
	}
}

// TestText_DrawnFaceLineHeight checks that an OpenDocument line is as tall
// as the face that draws it: Chinese text in a family the fonts lack is
// drawn in Noto Sans CJK SC, 1.448 em tall, and its lines at 140 %
// proportional spacing stand 1.448 x 20 x 1.4 = 40.54 points apart, as
// Impress spaces sd/tdf153161_FlushToSave.odp; Latin text in Liberation
// Sans keeps that face's 1.117 em.
func TestText_DrawnFaceLineHeight(t *testing.T) {
	cjk := RunStyle{Families: []string{"方正楷体_GBK", "Liberation Sans"}, Size: 20, Lang: "zh-CN", Color: css.Color{A: 1}}
	ls := RunStyle{Families: []string{"Liberation Sans"}, Size: 20, Color: css.Color{A: 1}}
	lined := func(p Paragraph) Paragraph {
		p.LineSpacing = 1.4
		return p
	}
	for _, tc := range []struct {
		name  string
		style RunStyle
		a, b  string
		pitch float64
	}{
		{"cjk", cjk, "早期一", "早期二", float64(1.448 * 20 * 1.4)},
		{"latin", ls, "Hxg0", "Hxg1", float64(1.117 * 20 * 1.4)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sh := box(28.35, 28.35, 340, 340, lined(para(tc.a, tc.style)), lined(para(tc.b, tc.style)))
			sh.Text.FontLineSpacing, sh.Text.MaxParagraphSpacing = true, true
			doc := deck(&Slide{Shapes: []*Shape{sh}})
			doc.Fallback = text.FallbackLibreOffice
			pg := convert(t, doc, office.Options{}).Pages[0]
			a, b := find(t, pg, tc.a), find(t, pg, tc.b)
			if got := b.y - a.y; !near(got, tc.pitch, 0.1) {
				t.Errorf("lines %.2f apart, want %.2f", got, tc.pitch)
			}
		})
	}
}

// TestText_UnwrappedOverflow checks where unwrapped text wider than its
// box sits, as Impress prints sd/tdf113198.pptx (centred, overflowing both
// sides) and sd/tdf104722.pptx (start-aligned, overflowing right): every
// character drawn, the block aligned on the box by its first paragraph.
func TestText_UnwrappedOverflow(t *testing.T) {
	const s = "A line far wider than its box"
	for _, tc := range []struct {
		align css.TextAlign
		want  func(x, w float64) bool
	}{
		{css.AlignLeft, func(x, _ float64) bool { return near(x, 100, 0.2) }},
		{css.AlignCenter, func(x, w float64) bool { return near(x+w/2, 125, 0.5) }},
		{css.AlignRight, func(x, w float64) bool { return near(x+w, 150, 0.5) }},
	} {
		p := para(s, arial(20))
		p.Align = tc.align
		sh := box(100, 100, 50, 30, p)
		sh.Text.Wrap = false
		pg := convert(t, deck(&Slide{Shapes: []*Shape{sh}}), office.Options{}).Pages[0]
		rs := runs(pg)
		if len(rs) != 1 || rs[0].text != s {
			t.Fatalf("align %d: runs %v, want one line", tc.align, rs)
		}
		b := &builder{ctx: context.Background(), fonts: pinnedFonts(t)}
		w := b.width(s, b.textStyle(&RunStyle{Families: []string{"Arial"}, Size: 20}, 1))
		if !tc.want(rs[0].x, w) {
			t.Errorf("align %d: line at %.2f, %.2f wide", tc.align, rs[0].x, w)
		}
	}
}

// TestText_Clip checks that a clipped body leaves out the lines whose
// baseline falls outside its rectangle, as Impress prints
// sd/style-overflow-behavior-clip.fodp, and cuts the rest at it.
func TestText_Clip(t *testing.T) {
	for _, clip := range []bool{false, true} {
		sh := box(100, 100, 300, 30, para("Kept", arial(20)), para("Gone", arial(20)))
		sh.Text.Clip = clip
		pg := convert(t, deck(&Slide{Shapes: []*Shape{sh}}), office.Options{}).Pages[0]
		var got []string
		for _, r := range runs(pg) {
			got = append(got, r.text)
		}
		want := 2
		if clip {
			want = 1
		}
		if len(got) != want || got[0] != "Kept" {
			t.Errorf("clip %v: runs %q", clip, got)
		}
		if g, ok := pg.Content[0].(*typeset.Group); clip && (!ok || g.Clip == nil) {
			t.Errorf("clip %v: no clipping group", clip)
		}
	}
}

// TestText_Scripts checks where superscripts and subscripts sit: an
// OpenDocument text's moved automatically by its face (Liberation Sans 20
// up 6.8 and down 1.4, as Impress prints them), a PresentationML text's by
// PowerPoint's shifts, 30% up and 25% down.
func TestText_Scripts(t *testing.T) {
	for _, tc := range []struct {
		fontLines bool
		up, down  float64
	}{
		{true, 6.8, 1.4},
		{false, 6, 5},
	} {
		ls := RunStyle{Families: []string{"Liberation Sans"}, Size: 20, Color: css.Color{A: 1}}
		sup, sub := ls, ls
		sup.Script, sub.Script = Superscript, Subscript
		sh := box(100, 100, 400, 200, Paragraph{Runs: []Run{{Text: "Base", Style: ls}, {Text: "Up", Style: sup}}, Style: ls},
			Paragraph{Runs: []Run{{Text: "Base2", Style: ls}, {Text: "Down", Style: sub}}, Style: ls})
		sh.Text.FontLineSpacing = tc.fontLines
		pg := convert(t, deck(&Slide{Shapes: []*Shape{sh}}), office.Options{}).Pages[0]
		b1, u, b2, d := find(t, pg, "Base"), find(t, pg, "Up"), find(t, pg, "Base2"), find(t, pg, "Down")
		if !near(b1.y-u.y, tc.up, 0.05) || !near(d.y-b2.y, tc.down, 0.05) || !near(u.size, 11.6, 0.01) {
			t.Errorf("font lines %v: up %.2f, down %.2f, size %.2f", tc.fontLines, b1.y-u.y, d.y-b2.y, u.size)
		}
	}
}

// TestText_DefaultTab checks the default tab stops: an inch apart in
// PresentationML text, half an inch in OpenDocument text, as Impress
// prints a frame without tab stops.
func TestText_DefaultTab(t *testing.T) {
	for _, tc := range []struct {
		fontLines bool
		want      float64
	}{{false, 72}, {true, 36}} {
		sh := box(100, 100, 400, 100, para("0\t1", arial(20)))
		sh.Text.FontLineSpacing = tc.fontLines
		pg := convert(t, deck(&Slide{Shapes: []*Shape{sh}}), office.Options{}).Pages[0]
		var x1 float64
		for _, r := range runs(pg) {
			if i := strings.Index(r.text, "1"); i >= 0 {
				x1 = r.x
			}
		}
		if !near(x1, 100+tc.want, 0.2) {
			t.Errorf("font lines %v: second digit at %.2f, want %.2f; runs %v", tc.fontLines, x1, 100+tc.want, runs(pg))
		}
	}
}

// TestText_PowerPointKeepsEmoji checks that a line of a PowerPoint
// presentation breaks at the space before a run of emoji, not between two
// of them, as PowerPoint sets "A " and ten emoji in a narrow box, where
// under Impress's fallback the emoji fill the first line.
func TestText_PowerPointKeepsEmoji(t *testing.T) {
	first := func(fallback text.Fallback) int {
		d := deck(&Slide{Shapes: []*Shape{box(100, 100, 120, 300, para("A \U0001F600\U0001F602\U0001F970\U0001F60D", arial(20)))}})
		d.Fallback = fallback
		out := convert(t, d, office.Options{})
		a := find(t, out.Pages[0], "A")
		n := 0
		for _, r := range runs(out.Pages[0]) {
			if near(r.y, a.y, 0.5) {
				for _, c := range r.text {
					if c >= 0x1F300 {
						n++
					}
				}
			}
		}
		return n
	}
	if got := first(text.FallbackFontconfig); got != 0 {
		t.Errorf("%d emoji on the line of A under PowerPoint's rules, want 0", got)
	}
	if got := first(text.FallbackLibreOffice); got == 0 {
		t.Errorf("no emoji on the line of A under Impress's fallback, want some")
	}
}

// TestText_OfficeEmoji checks that an emoji of a PowerPoint presentation
// advances as Segoe UI Emoji's do, 2812 units of 2048, the text after it
// moving with it, where under Impress's fallback it keeps Noto Color
// Emoji's 2550.
func TestText_OfficeEmoji(t *testing.T) {
	span := func(fallback text.Fallback) float64 {
		d := deck(&Slide{Shapes: []*Shape{box(100, 100, 400, 200, para("A\U0001F600B", arial(20)))}})
		d.Fallback = fallback
		out := convert(t, d, office.Options{})
		return find(t, out.Pages[0], "B").x - find(t, out.Pages[0], "A").x
	}
	if got, want := span(text.FallbackFontconfig)-span(text.FallbackLibreOffice), 20*(2812.0-2550)/2048; !near(got, want, 0.05) {
		t.Errorf("B moves %.2f points further under PowerPoint's rules, want %.2f", got, want)
	}
}

// TestText_KeepInWord checks that under PowerPoint's rules a line breaks
// after a closing parenthesis or a solidus only at a space: "(b)+cdef" and
// "b/cdefg" start the second line, where under Impress's rules the line
// breaks after ")" and "/".
func TestText_KeepInWord(t *testing.T) {
	starts := func(fallback text.Fallback, s string) []string {
		d := deck(&Slide{Shapes: []*Shape{box(100, 100, 75, 200, para(s, arial(20)))}})
		d.Fallback = fallback
		var out []string
		for _, r := range runs(convert(t, d, office.Options{}).Pages[0]) {
			out = append(out, strings.TrimSpace(r.text))
		}
		return out
	}
	if got := starts(text.FallbackFontconfig, "Aa (b)+cdefg"); len(got) < 2 || !strings.HasPrefix(got[1], "(b)+") {
		t.Errorf("lines %q under PowerPoint's rules, want the second to start with (b)+", got)
	}
	if got := starts(text.FallbackLibreOffice, "Aa (b)+cdefg"); len(got) < 2 || !strings.HasPrefix(got[1], "+") {
		t.Errorf("lines %q under Impress's rules, want the second to start with +", got)
	}
	if got := starts(text.FallbackFontconfig, "Aa bc/defgh"); len(got) < 2 || !strings.HasPrefix(got[1], "bc/") {
		t.Errorf("lines %q under PowerPoint's rules, want the second to start with bc/", got)
	}
	if got := starts(text.FallbackLibreOffice, "Aa bc/defgh"); len(got) < 2 || !strings.HasPrefix(got[1], "defgh") {
		t.Errorf("lines %q under Impress's rules, want the second to start with defgh", got)
	}
	for _, s := range []string{"Aa bc}defgh", "Aa bc…defgh"} {
		if got := starts(text.FallbackFontconfig, s); len(got) < 2 || !strings.HasPrefix(got[1], "bc") {
			t.Errorf("lines %q under PowerPoint's rules, want the second to start with bc", got)
		}
		if got := starts(text.FallbackLibreOffice, s); len(got) < 2 || !strings.HasPrefix(got[1], "defgh") {
			t.Errorf("lines %q under Impress's rules, want the second to start with defgh", got)
		}
	}
}

// TestText_PowerPointCentresBlock checks that under PowerPoint's rules a
// body anchored at the centre that wraps narrows to its widest line in the
// middle of its rectangle, lines aligned left or right in it, and that an
// unwrapped vertical line longer than its rectangle overflows both ends;
// under Impress's rules the wrapped block keeps the rectangle's width and
// the vertical line overflows past its end.
func TestText_PowerPointCentresBlock(t *testing.T) {
	a20 := arial(20)
	wide := widthOf(t, "bbbbbbbbb", a20)
	wrapped := func(fallback text.Fallback, align css.TextAlign) *typeset.Page {
		p := para("Aa bbbbbbbbb cc", a20)
		p.Align = align
		sh := box(100, 100, 120, 200, p)
		sh.Text.HorizontalAnchor = HorizontalCenter
		d := deck(&Slide{Shapes: []*Shape{sh}})
		d.Fallback = fallback
		return convert(t, d, office.Options{}).Pages[0]
	}
	if wide >= 120 {
		t.Fatalf("the widest line is %v points, want it narrower than the box", wide)
	}
	start := 100 + (120-wide)/2
	if got := find(t, wrapped(text.FallbackFontconfig, css.AlignLeft), "b").x; !near(got, start, 0.5) {
		t.Errorf("left-aligned widest line at %v under PowerPoint's rules, want %v", got, start)
	}
	pg := wrapped(text.FallbackFontconfig, css.AlignRight)
	if got := find(t, pg, "b").x; !near(got, start, 0.5) {
		t.Errorf("right-aligned widest line at %v under PowerPoint's rules, want %v", got, start)
	}
	if got, want := find(t, pg, "cc").x+widthOf(t, "cc", a20), start+wide; !near(got, want, 0.5) {
		t.Errorf("right-aligned last line ends at %v under PowerPoint's rules, want %v", got, want)
	}
	if got := find(t, wrapped(text.FallbackLibreOffice, css.AlignLeft), "b").x; !near(got, 100, 0.5) {
		t.Errorf("left-aligned widest line at %v under Impress's rules, want 100", got)
	}
	vertical := func(fallback text.Fallback) float64 {
		sh := box(100, 100, 50, 50, para("Long line here", a20))
		sh.Text.Wrap, sh.Text.Vertical, sh.Text.HorizontalAnchor = false, css.VerticalRL, HorizontalCenter
		d := deck(&Slide{Shapes: []*Shape{sh}})
		d.Fallback = fallback
		return find(t, convert(t, d, office.Options{}).Pages[0], "Long").y
	}
	long := widthOf(t, "Long line here", a20)
	if got, want := vertical(text.FallbackFontconfig), 100+(50-long)/2; !near(got, want, 0.5) {
		t.Errorf("vertical line starts at %v under PowerPoint's rules, want %v", got, want)
	}
	if got := vertical(text.FallbackLibreOffice); !near(got, 100, 0.5) {
		t.Errorf("vertical line starts at %v under Impress's rules, want 100", got)
	}
}

// TestText_NonNegativeAdvance checks that under PowerPoint's rules a
// spacing of -4000 points, past the cap, sets every glyph of "ABC" where
// the first is, where under Impress's rules the spacing past the cap is
// dropped and the glyphs keep their advances.
func TestText_NonNegativeAdvance(t *testing.T) {
	glyphX := func(fallback text.Fallback) []float64 {
		st := arial(20)
		st.Spacing = -4000
		d := deck(&Slide{Shapes: []*Shape{box(100, 100, 300, 100, para("ABC", st))}})
		d.Fallback = fallback
		var xs []float64
		var walk func([]typeset.Fragment)
		walk = func(frags []typeset.Fragment) {
			for _, f := range frags {
				switch v := f.(type) {
				case *typeset.Text:
					for _, g := range v.Glyphs {
						xs = append(xs, v.Origin.X+g.X)
					}
				case *typeset.Group:
					walk(v.Content)
				}
			}
		}
		walk(convert(t, d, office.Options{}).Pages[0].Content)
		return xs
	}
	if got := glyphX(text.FallbackFontconfig); len(got) != 3 || got[1] != got[0] || got[2] != got[0] {
		t.Errorf("glyphs at %v under PowerPoint's rules, want all three at one place", got)
	}
	if got := glyphX(text.FallbackLibreOffice); len(got) != 3 || !(got[1] > got[0] && got[2] > got[1]) {
		t.Errorf("glyphs at %v under Impress's rules, want them apart", got)
	}
}

// TestText_ClusterAlone checks that under PowerPoint's rules a paragraph
// whose margins leave its lines no room prints one character a line, where
// under Impress's rules each word overflows its line.
func TestText_ClusterAlone(t *testing.T) {
	lines := func(fallback text.Fallback) int {
		p := para("ab cd", arial(20))
		p.MarginRight = 500
		d := deck(&Slide{Shapes: []*Shape{box(100, 100, 200, 400, p)}})
		d.Fallback = fallback
		return len(runs(convert(t, d, office.Options{}).Pages[0]))
	}
	if got := lines(text.FallbackFontconfig); got != 4 {
		t.Errorf("%d lines under PowerPoint's rules, want 4", got)
	}
	if got := lines(text.FallbackLibreOffice); got != 2 {
		t.Errorf("%d lines under Impress's rules, want 2", got)
	}
}

// TestText_Highlight checks that a highlighted run paints its text box
// in the highlight's colour behind its glyphs.
func TestText_Highlight(t *testing.T) {
	green := css.Color{G: 1, A: 1}
	st := arial(18)
	st.Highlight = green
	pg := convert(t, deck(&Slide{Shapes: []*Shape{box(0, 0, 300, 100, para("Test", st))}}), office.Options{}).Pages[0]
	var pp []placedPath
	paths(pg.Content, typeset.Identity, &pp)
	for _, p := range pp {
		if p.p.Fill != nil && p.p.Fill.Color == colorOf(green) {
			if b := p.bounds(); b.Height < 18 || b.Height > 22 || b.Width < 30 || b.Width > 40 {
				t.Errorf("highlight %+v, want about 35 by 20", b)
			}
			return
		}
	}
	t.Errorf("no green fill among %d paths", len(pp))
}

// TestText_BodyRotation states that a body's rotation turns its text
// clockwise about the centre of its text rectangle, inside the shape
// (ECMA-376 Part 1, 21.1.2.1.1 bodyPr rot): a quarter turn takes the line's
// start to where the rectangle's centre turns it.
func TestText_BodyRotation(t *testing.T) {
	at := func(rot float64) run {
		sh := box(100, 100, 200, 100, para("Turned", arial(20)))
		sh.Text.Rotation = rot
		return find(t, convert(t, deck(&Slide{Shapes: []*Shape{sh}}), office.Options{}).Pages[0], "Turned")
	}
	flat, turned := at(0), at(90)
	// About (200, 150), y down: (x, y) goes to (200 - (y - 150), 150 + (x - 200)).
	wx, wy := 200-(flat.y-150), 150+(flat.x-200)
	if !near(turned.x, wx, 0.01) || !near(turned.y, wy, 0.01) {
		t.Errorf("turned line at %.2f,%.2f, want %.2f,%.2f (flat at %.2f,%.2f)", turned.x, turned.y, wx, wy, flat.x, flat.y)
	}
}
