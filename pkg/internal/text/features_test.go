package text

import (
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

// TestShape_FeaturesSubstitute checks that [Style.Features] reach the
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
		st.Features = features
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
		got, ok := Style{LanguageSystem: in}.languageSystem()
		if got != want || ok != (want != "") {
			t.Errorf("languageSystem(%q) = %q, %v, want %q", in, got, ok, want)
		}
	}
}
