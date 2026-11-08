package slides

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
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
