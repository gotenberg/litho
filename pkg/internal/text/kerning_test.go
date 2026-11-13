package text

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

// fixtureSerif is Liberation Serif among the pinned fixture fonts, whose
// GPOS kern feature pulls A and V together. A test that needs it skips
// when the fixture fonts are absent.
const fixtureSerif = "../../../test/integration/testdata/fonts/liberation-2.1.5/LiberationSerif-Regular.ttf"

func TestStyle_KerningFeatures(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		noKerning bool
		want      int
	}{
		{"kerns by default", false, 0},
		{"none turns kern off", true, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := Style{NoKerning: c.noKerning}.features()
			if len(got) != c.want {
				t.Fatalf("features %+v, want %d", got, c.want)
			}
			if c.want == 1 && (got[0].Tag.String() != "kern" || got[0].Value != 0) {
				t.Errorf("feature %+v, want kern=0", got[0])
			}
		})
	}
}

func TestShape_NoKerningMeasuresTheNaturalAdvances(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(fixtureSerif)
	if err != nil {
		t.Skipf("fixture fonts absent: %v", err)
	}
	f := newTestFonts(t, fontFile{name: filepath.Base(fixtureSerif), data: data})
	width := func(noKerning bool) (para, wrapped float64) {
		st := style()
		st.Families = []string{"Liberation Serif"}
		st.Size = 100
		st.NoKerning = noKerning
		p := shape(t, f, []ParagraphRun{{Text: "AVA", Style: st}}, ParagraphOptions{})
		lines, _ := layout(t, f, "AVA", st, 1000, Collapse)
		return p.Line(0, 3, false).Width, lines[0].Width
	}
	var widths [2][2]float64
	widths[0][0], widths[0][1] = width(false)
	widths[1][0], widths[1][1] = width(true)
	for j, name := range []string{"Shape", "Layout"} {
		kerned, natural := widths[0][j], widths[1][j]
		// The A-V and V-A pairs of Liberation Serif kern by about 13 points
		// each at 100 points.
		if d := natural - kerned; math.IsNaN(d) || d <= 10 {
			t.Errorf("%s: kerned %v, unkerned %v: want the unkerned wider", name, kerned, natural)
		}
	}
}

// TestShape_KernsAcrossRunsOfOneFont locks shaping across a run boundary
// where only the colour changes (CSS Text 3, 7.3): the A-V pair of two
// runs kerns as it does in one run, and each glyph keeps its run's colour;
// a change of size breaks the shaping.
func TestShape_KernsAcrossRunsOfOneFont(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(fixtureSerif)
	if err != nil {
		t.Skipf("fixture fonts absent: %v", err)
	}
	f := newTestFonts(t, fontFile{name: filepath.Base(fixtureSerif), data: data})
	st := style()
	st.Families = []string{"Liberation Serif"}
	st.Size = 100
	red := st
	red.Color.R = 1
	whole := shape(t, f, []ParagraphRun{{Text: "AV", Style: st}}, ParagraphOptions{}).Line(0, 2, false).Width
	split := shape(t, f, []ParagraphRun{{Text: "A", Style: st}, {Text: "V", Style: red}}, ParagraphOptions{}).Line(0, 2, false)
	if math.Abs(split.Width-whole) > 1e-6 {
		t.Errorf("A|V across a colour change %v wide, AV %v: want the same kerning", split.Width, whole)
	}
	if len(split.Spans) != 2 || split.Spans[0].Run != 0 || split.Spans[1].Run != 1 ||
		split.Spans[1].Text == nil || split.Spans[1].Text.Color.R != 1 || split.Spans[0].Text == nil || split.Spans[0].Text.Color.R != 0 {
		t.Errorf("spans %+v, want one per run in its own colour", split.Spans)
	}
	big := st
	big.Size = 101
	apart := shape(t, f, []ParagraphRun{{Text: "A", Style: st}, {Text: "V", Style: big}}, ParagraphOptions{}).Line(0, 2, false).Width
	if apart-whole < 10 {
		t.Errorf("A|V across a size change %v wide, AV %v: want no kerning across", apart, whole)
	}
}
