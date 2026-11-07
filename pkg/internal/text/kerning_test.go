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
		if !(natural-kerned > 10) || math.IsNaN(kerned) {
			t.Errorf("%s: kerned %v, unkerned %v: want the unkerned wider", name, kerned, natural)
		}
	}
}
