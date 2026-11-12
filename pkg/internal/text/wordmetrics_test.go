package text

import (
	"math"
	"testing"
)

// TestWordMetrics gives the line box of a stand-in for a Microsoft face
// the metrics Word lays that face out with, when the style asks for them,
// and leaves every other face, and the named face itself, its own.
func TestWordMetrics(t *testing.T) {
	t.Parallel()
	standIns := newTestFonts(t,
		fontFile{name: "caladea.ttf", data: familyFace("Caladea", "Regular", 400, false)},
		fontFile{name: "carlito.ttf", data: familyFace("Carlito", "Regular", 400, false)},
	)
	real := newTestFonts(t,
		fontFile{name: "cambria.ttf", data: familyFace("Cambria", "Regular", 400, false)},
		fontFile{name: "caladea.ttf", data: familyFace("Caladea", "Regular", 400, false)},
	)
	tests := []struct {
		name   string
		f      *Fonts
		family string
		word   bool
		// ascent and descent are Word's in em, 0 for the face's own.
		ascent, descent float64
	}{
		{"cambria by caladea", standIns, "Cambria", true, 0.950, 0.222},
		{"not asked", standIns, "Cambria", false, 0, 0},
		{"calibri by carlito", standIns, "Calibri", true, 0, 0},
		{"caladea named", standIns, "Caladea", true, 0, 0},
		{"cambria installed", real, "Cambria", true, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			st := style()
			st.Families = []string{tt.family}
			st.Size = 20
			st.WordMetrics = tt.word
			fc := tt.f.Match(st)
			want := fc.Metrics(20)
			if tt.ascent > 0 {
				want.Ascent, want.Descent, want.LineGap = tt.ascent*20, tt.descent*20, 0
			}
			got := fc.MetricsFor(st)
			if math.Abs(got.Ascent-want.Ascent) > 1e-9 || math.Abs(got.Descent-want.Descent) > 1e-9 || got.LineGap != want.LineGap {
				t.Errorf("metrics %+v, want %+v", got, want)
			}
			p := shape(t, tt.f, []ParagraphRun{{Text: "AB", Style: st}}, ParagraphOptions{})
			sp := p.Line(0, 2, false).Spans[0]
			if math.Abs(sp.Ascent-want.Ascent) > 1e-6 || math.Abs(sp.Descent-want.Descent) > 1e-6 {
				t.Errorf("span ascent %v descent %v, want %v and %v", sp.Ascent, sp.Descent, want.Ascent, want.Descent)
			}
		})
	}
}
