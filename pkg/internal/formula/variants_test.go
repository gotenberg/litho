package formula

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// fixtureLibreOffice holds the Liberation faces and OpenSymbol of the
// reference image, provisioned with the corpus fonts: faces without a
// MATH table, as LibreOffice Math draws formulas in.
var fixtureLibreOffice = []string{
	"../../../test/integration/testdata/fonts/gotenberg/truetype/liberation",
	"../../../test/integration/testdata/fonts/gotenberg/truetype/libreoffice",
}

var (
	liberationOnce  sync.Once
	liberationFonts *text.Fonts
	liberationErr   error
)

// liberation returns a Setter over the Liberation faces and OpenSymbol,
// skipping the test when the fixture fonts are absent.
func liberation(t *testing.T) *Setter {
	t.Helper()
	liberationOnce.Do(func() {
		if _, liberationErr = os.Stat(fixtureLibreOffice[1] + "/opens___.ttf"); liberationErr != nil {
			return
		}
		liberationFonts, liberationErr = text.NewFonts(context.Background(), nil, fixtureLibreOffice)
	})
	if liberationErr != nil {
		t.Skipf("fixture fonts absent: %v", liberationErr)
	}
	return NewSetter(liberationFonts, nil)
}

// texts returns the glyph runs of b drawn at the origin.
func texts(b *Box) []*typeset.Text {
	var out []*typeset.Text
	for _, f := range b.Draw(0, 0) {
		if t, ok := f.(*typeset.Text); ok {
			out = append(out, t)
		}
	}
	return out
}

// TestFontVariants_DrawItalicInTheItalicFace locks Style.FontVariants: a
// single-letter identifier is drawn as its letter in the italic face of
// its family, a mathvariant italic identifier of two letters is italic
// too, and bold on an mstyle reaches the identifier inside it, where
// MathML Core draws the Mathematical Alphanumeric Symbol and leaves the
// others upright.
func TestFontVariants_DrawItalicInTheItalicFace(t *testing.T) {
	s := liberation(t)
	root := &Element{Name: "math", Children: []*Element{
		{Name: "mi", Text: "x"},
		attrs(&Element{Name: "mi", Text: "ac"}, "mathvariant", "italic"),
		attrs(&Element{Name: "mstyle", Children: []*Element{{Name: "mi", Text: "y"}}}, "mathvariant", "bold"),
	}}
	st := mathStyle("Liberation Serif", 12)
	st.FontVariants = true
	runs := texts(lay(t, s, root, st))
	if len(runs) != 3 {
		t.Fatalf("got %d runs, want 3", len(runs))
	}
	for i, want := range []string{"Italic", "Italic", "BoldItalic"} {
		if key := runs[i].Face.Key; !strings.Contains(strings.ReplaceAll(key, "-", ""), want) {
			t.Errorf("run %d drawn in %q, want a %s face", i, key, want)
		}
		if len(runs[i].Glyphs) == 0 || runs[i].Glyphs[0].GID == 0 {
			t.Errorf("run %d draws no glyph", i)
		}
	}
	core := texts(lay(t, s, root, mathStyle("Liberation Serif", 12)))
	if len(core) == 0 || strings.Contains(core[0].Face.Key, "LiberationSerif-Italic") {
		t.Errorf("MathML Core drew x in %v, want the Mathematical Alphanumeric Symbol's face", core)
	}
}

// TestScriptScale_ScalesScriptsOfAFontWithoutMath locks
// Style.ScriptScale: a superscript in a face without a MATH table is
// drawn at the scale given, and at MathML Core's 0.71 without one.
func TestScriptScale_ScalesScriptsOfAFontWithoutMath(t *testing.T) {
	s := liberation(t)
	root := &Element{Name: "math", Children: []*Element{{Name: "msup", Children: []*Element{{Name: "mi", Text: "a"}, {Name: "mn", Text: "2"}}}}}
	for _, tc := range []struct {
		scale, want float64
	}{{0.6, 7.2}, {0, float64(0.71 * 12)}} {
		st := mathStyle("Liberation Serif", 12)
		st.ScriptScale = tc.scale
		runs := texts(lay(t, s, root, st))
		if len(runs) != 2 || !near(runs[1].Size, tc.want) {
			t.Errorf("scale %g: script run at %v, want %g pt", tc.scale, sizes(runs), tc.want)
		}
	}
}

// sizes returns the font sizes of runs.
func sizes(runs []*typeset.Text) []float64 {
	out := make([]float64, len(runs))
	for i, r := range runs {
		out[i] = r.Size
	}
	return out
}

// TestOperatorFamilies_DrawOperatorsInTheirFont locks
// Style.OperatorFamilies: an operator is drawn in the operator families,
// an identifier in the formula's.
func TestOperatorFamilies_DrawOperatorsInTheirFont(t *testing.T) {
	s := liberation(t)
	root := &Element{Name: "math", Children: []*Element{{Name: "mi", Text: "a"}, {Name: "mo", Text: "+"}, {Name: "mi", Text: "b"}}}
	st := mathStyle("Liberation Serif", 12)
	st.OperatorFamilies = []string{"OpenSymbol"}
	runs := texts(lay(t, s, root, st))
	if len(runs) != 3 {
		t.Fatalf("got %d runs, want 3", len(runs))
	}
	for i, want := range []string{"LiberationSerif", "opens___", "LiberationSerif"} {
		if !strings.Contains(runs[i].Face.Key, want) {
			t.Errorf("run %d drawn in %q, want %s", i, runs[i].Face.Key, want)
		}
	}
}
