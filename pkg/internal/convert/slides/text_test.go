package slides

import (
	"context"
	"github.com/gotenberg/litho/pkg/internal/compose"

	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
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
		if g, ok := pg.Content[0].(*compose.Group); clip && (!ok || g.Clip == nil) {
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
