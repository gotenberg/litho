package text

import (
	"context"
	"os"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
)

// familyFace builds a face of family covering U+0020, A and B, plus extra,
// with the weight and italic bit given.
func familyFace(family, subfamily string, weight uint16, italic bool, extra ...rune) []byte {
	glyphs := []fontbuild.Glyph{
		{Name: "space", Unicode: ' ', Advance: spaceAdvance, Empty: true},
		{Name: "A", Unicode: 'A', Advance: aAdvance},
		{Name: "B", Unicode: 'B', Advance: bAdvance},
	}
	for _, r := range extra {
		glyphs = append(glyphs, fontbuild.Glyph{Name: "u" + strings.ToUpper(string(r)), Unicode: r, Advance: 1000})
	}
	prog := fontbuild.TrueType{
		PostScriptName: strings.ReplaceAll(family+"-"+subfamily, " ", ""),
		Family:         family,
		Subfamily:      subfamily,
		NotdefAdvance:  500,
		Glyphs:         glyphs,
	}.Build()
	return withAspect(prog, weight, italic)
}

func TestMatch_MetricAliases(t *testing.T) {
	t.Parallel()
	f := newTestFonts(t,
		fontFile{name: "a_dejavu.ttf", data: familyFace("DejaVu Sans", "Regular", 400, false)},
		fontFile{name: "liberation_sans.ttf", data: familyFace("Liberation Sans", "Regular", 400, false)},
		fontFile{name: "liberation_serif.ttf", data: familyFace("Liberation Serif", "Regular", 400, false)},
		fontFile{name: "carlito.ttf", data: familyFace("Carlito", "Regular", 400, false)},
		fontFile{name: "cousine.ttf", data: familyFace("Cousine", "Regular", 400, false)},
	)
	for _, tc := range []struct {
		family, want string
	}{
		{"Arial", "Liberation Sans"},
		{"Helvetica", "Liberation Sans"},
		{"Times New Roman", "Liberation Serif"},
		{"Times", "Liberation Serif"},
		{"Calibri", "Carlito"},
		{"Courier New", "Cousine"},
		{"Courier", "Cousine"},
		{"Liberation Sans", "Liberation Sans"},
		{"Tinos", "Liberation Serif"},
		{"Unknown Family", "DejaVu Sans"},
	} {
		if got := f.Match(Style{Families: []string{tc.family}, Size: 10}).Family(); got != tc.want {
			t.Errorf("Match(%s) = %s, want %s", tc.family, got, tc.want)
		}
	}
}

func TestMatch_GenericOrder(t *testing.T) {
	t.Parallel()
	// Liberation Serif sorts first, but 60-latin.conf lists DejaVu Serif
	// first.
	f := newTestFonts(t,
		fontFile{name: "a_liberation_serif.ttf", data: familyFace("Liberation Serif", "Regular", 400, false)},
		fontFile{name: "b_dejavu_serif.ttf", data: familyFace("DejaVu Serif", "Regular", 400, false)},
		fontFile{name: "c_noto_serif.ttf", data: familyFace("Noto Serif", "Regular", 400, false)},
		fontFile{name: "d_mono.ttf", data: familyFace("Liberation Mono", "Regular", 400, false)},
	)
	for _, tc := range []struct {
		families []string
		want     string
	}{
		{[]string{"serif"}, "DejaVu Serif"},
		{[]string{"Liberation Serif", "serif"}, "Liberation Serif"},
		{[]string{"monospace"}, "Liberation Mono"},
		// Nothing of sans-serif is indexed: the first face.
		{[]string{"sans-serif"}, "Liberation Serif"},
	} {
		if got := f.Match(Style{Families: tc.families, Size: 10}).Family(); got != tc.want {
			t.Errorf("Match(%q) = %s, want %s", tc.families, got, tc.want)
		}
	}
}

func TestBrowserFamilies_ResolveAsChromium(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		in, want []string
	}{
		{nil, []string{"Times New Roman", "serif"}},
		{[]string{"Georgia"}, []string{"Georgia", "Times New Roman", "serif"}},
		{[]string{"Georgia", "Sans-Serif"}, []string{"Georgia", "Arial", "Sans-Serif"}},
		{[]string{"monospace"}, []string{"monospace"}},
		{[]string{"cursive", "fantasy"}, []string{"Comic Sans MS", "cursive", "Impact", "fantasy"}},
		// ui-monospace is no generic Chromium resolves on Linux.
		{[]string{"ui-monospace"}, []string{"ui-monospace", "Times New Roman", "serif"}},
	} {
		in := slices.Clone(tc.in)
		if got := BrowserFamilies(in); !slices.Equal(got, tc.want) {
			t.Errorf("BrowserFamilies(%q) = %q, want %q", tc.in, got, tc.want)
		}
		if !slices.Equal(in, tc.in) {
			t.Errorf("BrowserFamilies(%q) changed its argument", tc.in)
		}
	}
	// The DejaVu faces sort first and lead fontconfig's lists, but Chromium
	// asks for Times New Roman and Arial in place of serif and sans-serif,
	// which their metric aliases turn into the Liberation faces.
	f := newTestFonts(t,
		fontFile{name: "a_dejavu_serif.ttf", data: familyFace("DejaVu Serif", "Regular", 400, false)},
		fontFile{name: "b_dejavu_sans.ttf", data: familyFace("DejaVu Sans", "Regular", 400, false)},
		fontFile{name: "c_dejavu_mono.ttf", data: familyFace("DejaVu Sans Mono", "Regular", 400, false)},
		fontFile{name: "d_liberation_serif.ttf", data: familyFace("Liberation Serif", "Regular", 400, false)},
		fontFile{name: "e_liberation_sans.ttf", data: familyFace("Liberation Sans", "Regular", 400, false)},
		fontFile{name: "f_liberation_mono.ttf", data: familyFace("Liberation Mono", "Regular", 400, false)},
	)
	for _, tc := range []struct {
		families []string
		want     string
	}{
		{[]string{"serif"}, "Liberation Serif"},
		{[]string{"sans-serif"}, "Liberation Sans"},
		{[]string{"Georgia", "serif"}, "Liberation Serif"},
		// An unavailable family alone takes Chromium's standard font.
		{[]string{"Georgia"}, "Liberation Serif"},
		{[]string{"DejaVu Serif", "serif"}, "DejaVu Serif"},
		// Chromium asks for "Monospace", which fontconfig resolves by its
		// own list: DejaVu Sans Mono first.
		{[]string{"monospace"}, "DejaVu Sans Mono"},
		{[]string{"system-ui"}, "DejaVu Sans"},
	} {
		if got := f.Match(Style{Families: BrowserFamilies(tc.families), Size: 10}).Family(); got != tc.want {
			t.Errorf("Match(BrowserFamilies(%q)) = %s, want %s", tc.families, got, tc.want)
		}
	}
	// Without the Liberation faces, fontconfig's lists decide.
	g := newTestFonts(t,
		fontFile{name: "a_noto_serif.ttf", data: familyFace("Noto Serif", "Regular", 400, false)},
		fontFile{name: "b_dejavu_serif.ttf", data: familyFace("DejaVu Serif", "Regular", 400, false)},
	)
	if got := g.Match(Style{Families: BrowserFamilies([]string{"serif"}), Size: 10}).Family(); got != "DejaVu Serif" {
		t.Errorf("serif without Liberation = %s, want DejaVu Serif", got)
	}
}

func TestMatch_AspectScoring(t *testing.T) {
	t.Parallel()
	f := newTestFonts(t,
		fontFile{name: "a_bold.ttf", data: familyFace("Fam", "Bold", 700, false)},
		fontFile{name: "b_light.ttf", data: familyFace("Fam", "Light", 300, false)},
		fontFile{name: "c_regular.ttf", data: familyFace("Fam", "Regular", 400, false)},
		fontFile{name: "d_italic.ttf", data: familyFace("Fam", "Italic", 400, true)},
		fontFile{name: "e_black.ttf", data: familyFace("Fam", "Black", 900, false)},
	)
	for _, tc := range []struct {
		weight int
		italic bool
		want   string
	}{
		{700, false, "a_bold"},
		{500, false, "c_regular"},
		{450, false, "c_regular"},
		{400, false, "c_regular"},
		{600, false, "a_bold"},
		{800, false, "e_black"},
		{350, false, "b_light"},
		{200, false, "b_light"},
		{400, true, "d_italic"},
	} {
		got := f.Match(Style{Families: []string{"Fam"}, Weight: tc.weight, Italic: tc.italic, Size: 10})
		if !strings.Contains(got.Key(), tc.want) {
			t.Errorf("Match(weight %d, italic %v) = %s, want %s", tc.weight, tc.italic, got.Key(), tc.want)
		}
	}
}

func TestMatch_SlantBeforeWeight(t *testing.T) {
	t.Parallel()
	files := []fontFile{
		{name: "a_regular.ttf", data: familyFace("Fam", "Regular", 400, false)},
		{name: "b_bold.ttf", data: familyFace("Fam", "Bold", 700, false)},
		{name: "c_italic.ttf", data: familyFace("Fam", "Italic", 400, true)},
	}
	boldItalic := Style{Families: []string{"Fam"}, Weight: 700, Italic: true, Size: 10}
	// CSS Fonts 4, 5.2: font-style narrows the set before font-weight.
	if got := newTestFonts(t, files...).Match(boldItalic); !strings.Contains(got.Key(), "c_italic") {
		t.Errorf("bold italic without a bold italic face = %s, want the italic face", got.Key())
	}
	files = append(files, fontFile{name: "d_bold_italic.ttf", data: familyFace("Fam", "Bold Italic", 700, true)})
	if got := newTestFonts(t, files...).Match(boldItalic); !strings.Contains(got.Key(), "d_bold_italic") {
		t.Errorf("bold italic = %s, want the bold italic face", got.Key())
	}
}

func TestLayout_CJKFallbackByLang(t *testing.T) {
	t.Parallel()
	const han = '日'
	f := newTestFonts(t,
		fontFile{name: "a_sans.ttf", data: familyFace("Test Sans", "Regular", 400, false)},
		fontFile{name: "b_other_cjk.ttf", data: familyFace("Other CJK", "Regular", 400, false, han)},
		fontFile{name: "c_kr.ttf", data: familyFace("Noto Sans CJK KR", "Regular", 400, false, han)},
		fontFile{name: "d_sc.ttf", data: familyFace("Noto Sans CJK SC", "Regular", 400, false, han)},
		fontFile{name: "e_jp.ttf", data: familyFace("Noto Sans CJK JP", "Regular", 400, false, han)},
	)
	for _, tc := range []struct {
		lang, want string
	}{
		{"", "e_jp"},
		{"ja", "e_jp"},
		{"zh-CN", "d_sc"},
		{"ko-KR", "c_kr"},
		{"fr", "e_jp"},
	} {
		s := style()
		s.Lang = tc.lang
		lines, rep := layout(t, f, "A日", s, 100, Collapse)
		if len(rep.MissingGlyphs) != 0 || len(lines[0].Runs) != 2 {
			t.Fatalf("lang %q: report %+v, runs %d", tc.lang, rep, len(lines[0].Runs))
		}
		if k := lines[0].Runs[1].Face.Key; !strings.Contains(k, tc.want) {
			t.Errorf("lang %q: 日 drawn with %s, want %s", tc.lang, k, tc.want)
		}
	}
	// A generic family in a CJK language matches the Noto CJK face first,
	// as 70-noto-cjk.conf prepends it.
	if got := f.Match(Style{Families: []string{"sans-serif"}, Lang: "zh-Hans", Size: 10}).Family(); got != "Noto Sans CJK SC" {
		t.Errorf("sans-serif in zh-Hans = %s", got)
	}
}

func TestFonts_LoadsFacesLazily(t *testing.T) {
	t.Parallel()
	f := newTestFonts(t, append(sansFamily(), fontFile{name: "digits.ttf", data: testDigits()})...)
	if n := f.loadedFaces(); n != 0 {
		t.Fatalf("NewFonts loaded %d fonts", n)
	}
	for _, fc := range f.faces {
		if fc.font != nil {
			t.Fatalf("face %s holds a parsed font", fc.Key())
		}
	}
	if m := f.Match(style()).Metrics(10); m.Ascent <= 0 || f.loadedFaces() != 0 {
		t.Errorf("Metrics %+v loaded %d fonts", m, f.loadedFaces())
	}
	layout(t, f, "AB", style(), 100, Collapse)
	if n := f.loadedFaces(); n != 1 {
		t.Errorf("a layout drawing with one face loaded %d fonts", n)
	}
	layout(t, f, "AD", style(), 100, Collapse)
	if n := f.loadedFaces(); n != 2 {
		t.Errorf("a fallback face loaded %d fonts in all, want 2", n)
	}
}

func TestFonts_EvictsPastTheCountCap(t *testing.T) {
	t.Parallel()
	f := newTestFonts(t, sansFamily()...)
	f.maxLoaded = 2
	bold, italic := style(), style()
	bold.Weight = 700
	italic.Italic = true
	want, _ := layout(t, f, "AB", style(), 100, Collapse)
	layout(t, f, "AB", bold, 100, Collapse)
	layout(t, f, "AB", italic, 100, Collapse)
	if n := f.loadedFaces(); n != 2 {
		t.Fatalf("cache holds %d fonts, cap 2", n)
	}
	regular := f.Match(style())
	f.mu.Lock()
	evicted := regular.font == nil
	f.mu.Unlock()
	if !evicted {
		t.Error("the least recently used font stayed loaded")
	}
	// A face that left the cache loads again and lays out the same.
	got, _ := layout(t, f, "AB", style(), 100, Collapse)
	if lineText(got[0]) != lineText(want[0]) || got[0].Width != want[0].Width || got[0].Runs[0].Face != want[0].Runs[0].Face {
		t.Error("a reloaded face lays out differently")
	}
	if n := f.loadedFaces(); n != 2 {
		t.Errorf("cache holds %d fonts after a reload, cap 2", n)
	}
}

func TestFonts_EvictsPastTheByteCap(t *testing.T) {
	t.Parallel()
	f := newTestFonts(t, sansFamily()...)
	f.maxLoadedBytes = 1
	bold := style()
	bold.Weight = 700
	layout(t, f, "AB", style(), 100, Collapse)
	layout(t, f, "AB", bold, 100, Collapse)
	// The font just loaded stays, past the byte cap.
	if n := f.loadedFaces(); n != 1 {
		t.Errorf("cache holds %d fonts, want only the last", n)
	}
}

// systemFontsDir is the macOS system font directory the memory test
// indexes.
const systemFontsDir = "/System/Library/Fonts"

// TestNewFonts_SystemFontsFootprint indexes the system fonts of the machine
// and checks that the index stays small and fast: a font set of about 800
// faces, CJK collections included.
func TestNewFonts_SystemFontsFootprint(t *testing.T) {
	if _, err := os.Stat(systemFontsDir); err != nil {
		t.Skipf("no %s", systemFontsDir)
	}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	start := time.Now()
	f, err := NewFonts(context.Background(), nil, []string{systemFontsDir})
	if err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	runtime.GC()
	runtime.ReadMemStats(&after)
	kept := int64(after.HeapAlloc) - int64(before.HeapAlloc) //nolint:gosec // heap sizes fit int64
	t.Logf("%d faces indexed in %v, %.1f MiB kept", f.Len(), elapsed, float64(kept)/(1<<20))
	if f.Len() == 0 {
		t.Fatal("no face indexed")
	}
	if kept > 64<<20 {
		t.Errorf("index keeps %d bytes, want under 64 MiB", kept)
	}
	if !raceEnabled && elapsed > time.Second {
		t.Errorf("indexing took %v, want under 1 s", elapsed)
	}
	if f.loadedFaces() != 0 {
		t.Errorf("indexing loaded %d fonts", f.loadedFaces())
	}
	runtime.KeepAlive(f)
}

// loadedFaces returns the number of parsed fonts the cache of f holds.
func (f *Fonts) loadedFaces() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.loaded
}
