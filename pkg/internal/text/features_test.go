package text

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-text/typesetting/language"
)

// fixtureNotoSans is Noto Sans among the pinned fixture fonts, whose GSUB
// has smcp and c2sc for Latin and no pcap. A test that needs it skips
// when the fixture fonts are absent.
const fixtureNotoSans = "../../../test/integration/testdata/fonts/noto-core-20201225/NotoSans-Regular.ttf"

func TestHasSubstitution(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(fixtureNotoSans)
	if err != nil {
		t.Skipf("fixture fonts absent: %v", err)
	}
	f := newTestFonts(t, fontFile{name: filepath.Base(fixtureNotoSans), data: data}, repoFont(t, "ab.ttf", dejavuAB))
	noto := style()
	noto.Families = []string{"Noto Sans"}
	for _, c := range []struct {
		r    rune
		tag  string
		want bool
	}{
		{'a', "smcp", true}, {'a', "c2sc", true}, {'a', "pcap", false}, {'1', "smcp", true},
		{'α', "smcp", true}, {'a', "sm", false},
	} {
		if got := f.HasSubstitution(noto, c.r, c.tag); got != c.want {
			t.Errorf("Noto Sans %q %s = %v, want %v", c.r, c.tag, got, c.want)
		}
	}
	dejavu := style()
	dejavu.Families = []string{"DejaVu Sans"}
	if f.HasSubstitution(dejavu, 'A', "smcp") {
		t.Errorf("a face without GSUB has smcp")
	}
}

// TestShape_FeaturesSubstitute checks that [FontSettings.Features] reach the
// shaper: smcp draws Noto Sans's small capitals, narrower than its
// lowercase, and a later setting of the tag overrides an earlier one.
func TestShape_FeaturesSubstitute(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(fixtureNotoSans)
	if err != nil {
		t.Skipf("fixture fonts absent: %v", err)
	}
	f := newTestFonts(t, fontFile{name: filepath.Base(fixtureNotoSans), data: data})
	width := func(features ...Feature) float64 {
		st := style()
		st.Families = []string{"Noto Sans"}
		st.Size = 100
		st.Settings = &FontSettings{Features: features}
		p := shape(t, f, []ParagraphRun{{Text: "mmm", Style: st}}, ParagraphOptions{})
		return p.Line(0, 3, false).Width
	}
	plain := width()
	small := width(Feature{Tag: "smcp", Value: 1})
	if small >= plain {
		t.Errorf("smcp: width %v, want under the lowercase %v", small, plain)
	}
	if got := width(Feature{Tag: "smcp", Value: 1}, Feature{Tag: "smcp", Value: 0}); got != plain {
		t.Errorf("smcp turned off again: width %v, want %v", got, plain)
	}
	if got := width(Feature{Tag: "bad\x01", Value: 1}); got != plain {
		t.Errorf("an invalid tag: width %v, want %v", got, plain)
	}
}

func TestStyle_LanguageSystem(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]language.Language{
		"":       "",
		"TRK":    "x-hbot-54524b20",
		"TRK ":   "x-hbot-54524b20",
		"ROMANI": "x-hbot-524f4d41",
		"\x01AB": "",
	} {
		got, ok := Style{Settings: &FontSettings{LanguageSystem: in}}.languageSystem()
		if got != want || ok != (want != "") {
			t.Errorf("languageSystem(%q) = %q, %v, want %q", in, got, ok, want)
		}
	}
}

// TestShape_ExtractText checks [ParagraphRun.Extract]: the glyphs of a
// run extract as its Extract, those of the other runs as their text, and
// an Extract of another length is ignored.
func TestShape_ExtractText(t *testing.T) {
	t.Parallel()
	f := newTestFonts(t, repoFont(t, "ab.ttf", dejavuAB))
	st := style()
	st.Families = []string{"DejaVu Sans"}
	p := shape(t, f, []ParagraphRun{
		{Text: "A", Style: st}, {Text: "AB", Extract: "ab", Style: st},
		{Text: "BA", Extract: "b", Style: st}, {Text: "B", Style: st},
	}, ParagraphOptions{})
	if got := string(p.extractText(0, 6)); got != "AabBAB" {
		t.Errorf("extracted %q, want %q", got, "AabBAB")
	}
	plain := shape(t, f, []ParagraphRun{{Text: "AB", Style: st}}, ParagraphOptions{})
	if plain.extract != nil {
		t.Errorf("a paragraph without Extract holds %q", string(plain.extract))
	}
}

// TestShape_FaceSizeAdjustAndOverrides checks the size-adjust and metric
// override descriptors of a document face (CSS Fonts 5, 4.12; CSS Fonts 4,
// 4.11): the face draws at its scaled size, and its ascent, descent and
// line gap are the given fractions of that size.
func TestShape_FaceSizeAdjustAndOverrides(t *testing.T) {
	t.Parallel()
	base := newTestFonts(t, fontFile{name: "sans_regular.ttf", data: testSans("Regular", 400, false, 100)})
	plain := base.WithFaces(NewFaceSet([]*Face{docFace(t, testSans("Regular", 400, false, 100), "Test Sans", FaceDescriptor{})}))
	adjusted := base.WithFaces(NewFaceSet([]*Face{docFace(t, testSans("Regular", 400, false, 100), "Test Sans", FaceDescriptor{
		SizeAdjust: 0.5, Ascent: MetricOverride{Value: 0.8, Set: true}, LineGap: MetricOverride{Value: 0, Set: true},
	})}))
	st := style()
	st.Size = 20
	w := func(f *Fonts) float64 {
		return shape(t, f, []ParagraphRun{{Text: "AB", Style: st}}, ParagraphOptions{}).Line(0, 2, false).Width
	}
	if a, b := w(plain), w(adjusted); math.Abs(b-a/2) > 1e-6 {
		t.Errorf("adjusted width %v, want half of %v", b, a)
	}
	m := adjusted.Match(st).Metrics(20)
	pm := plain.Match(st).Metrics(20)
	if math.Abs(m.Ascent-8) > 1e-9 || m.LineGap != 0 || math.Abs(m.Descent-pm.Descent/2) > 1e-9 {
		t.Errorf("metrics %+v, plain %+v", m, pm)
	}
}

// TestShape_FontSizeAdjust checks font-size-adjust (CSS Fonts 5, 2.6): a
// face whose ex-height is 0.8 em draws at half the size for a value of
// 0.4, its metrics with it, and a value of 0 adjusts nothing.
func TestShape_FontSizeAdjust(t *testing.T) {
	t.Parallel()
	f := newTestFonts(t, repoFont(t, "Ahem.ttf", ahemPath))
	st := style()
	st.Families = []string{"Ahem"}
	st.Size = 20
	w := func(s Style) float64 {
		return shape(t, f, []ParagraphRun{{Text: "XX", Style: s}}, ParagraphOptions{}).Line(0, 2, false).Width
	}
	plain := w(st)
	if got := f.Aspect(st, AdjustExHeight); math.Abs(got-0.8) > 1e-9 {
		t.Fatalf("Ahem's ex aspect %v, want 0.8", got)
	}
	adjusted := st
	adjusted.Settings = &FontSettings{SizeAdjust: SizeAdjust{Value: 0.4}}
	if got := w(adjusted); math.Abs(got-plain/2) > 1e-6 {
		t.Errorf("adjusted width %v, want %v", got, plain/2)
	}
	if m, pm := f.Match(st).MetricsFor(adjusted), f.Match(st).MetricsFor(st); math.Abs(m.Ascent-pm.Ascent/2) > 1e-9 {
		t.Errorf("adjusted ascent %v, plain %v", m.Ascent, pm.Ascent)
	}
	adjusted.Settings = &FontSettings{SizeAdjust: SizeAdjust{Metric: AdjustCapHeight}}
	if got := w(adjusted); got != plain {
		t.Errorf("a value of 0: width %v, want %v", got, plain)
	}
}
