package text

import (
	"encoding/binary"
	"slices"
	"strings"
	"testing"
)

// withRevision sets the head fontRevision of prog to rev, a 16.16
// fixed-point number.
func withRevision(prog []byte, rev uint32) []byte {
	numTables := int(binary.BigEndian.Uint16(prog[4:]))
	for i := range numTables {
		rec := prog[12+16*i:]
		if string(rec[:4]) == "head" {
			binary.BigEndian.PutUint32(prog[binary.BigEndian.Uint32(rec[8:])+4:], rev)
		}
	}
	return prog
}

// drawnBy lays text out in s and returns the key of the face that draws
// its last run.
func drawnBy(t *testing.T, f *Fonts, text string, s Style) string {
	t.Helper()
	lines, rep := layout(t, f, text, s, 1000, Collapse)
	if len(rep.MissingGlyphs) != 0 || len(lines) != 1 {
		t.Fatalf("%q: report %+v, lines %d", text, rep, len(lines))
	}
	runs := lines[0].Runs
	return runs[len(runs)-1].Face.Key
}

// TestLayout_FallbackFollowsTheJudgingEngine locks the faces a character
// the primary face lacks takes under each [Fallback] rule.
//
// # Corpus evidence
//
//   - probe, html: in the gotenberg/gotenberg:9.4.1 image, Chromium prints
//     Armenian under the default serif font in DejaVu Sans and Han in Noto
//     Sans CJK JP, SC for lang="zh-CN".
//   - probe, odt: LibreOffice in the same image prints Armenian in DejaVu
//     Serif under Liberation Serif and under a missing family whose
//     style:font-family-generic is roman, in DejaVu Sans under Liberation
//     Sans and a missing family without a generic; Han under a missing
//     Asian family in Noto Sans CJK SC.
func TestLayout_FallbackFollowsTheJudgingEngine(t *testing.T) {
	t.Parallel()
	const armenian, han = 'Ա', '日'
	f := newTestFonts(t,
		fontFile{name: "a_liberation_serif.ttf", data: familyFace("Liberation Serif", "Regular", 400, false)},
		fontFile{name: "b_liberation_sans.ttf", data: familyFace("Liberation Sans", "Regular", 400, false)},
		fontFile{name: "c_dejavu_serif.ttf", data: familyFace("DejaVu Serif", "Regular", 400, false, armenian)},
		fontFile{name: "d_dejavu_sans.ttf", data: familyFace("DejaVu Sans", "Regular", 400, false, armenian)},
		fontFile{name: "e_serif_jp.ttf", data: familyFace("Noto Serif CJK JP", "Regular", 400, false, han)},
		fontFile{name: "f_sans_jp.ttf", data: familyFace("Noto Sans CJK JP", "Regular", 400, false, han)},
		fontFile{name: "g_sans_sc.ttf", data: familyFace("Noto Sans CJK SC", "Regular", 400, false, han)},
		fontFile{name: "h_serif_sc.ttf", data: familyFace("Noto Serif CJK SC", "Regular", 400, false, han)},
	)
	for _, tc := range []struct {
		name     string
		fallback Fallback
		named    []string
		lang     string
		r        rune
		want     string
	}{
		{"fontconfig, Armenian", FallbackFontconfig, nil, "", armenian, "c_dejavu_serif"},
		{"fontconfig, Han", FallbackFontconfig, nil, "", han, "e_serif_jp"},
		{"Chromium, Armenian", FallbackChromium, nil, "", armenian, "d_dejavu_sans"},
		{"Chromium, Han", FallbackChromium, nil, "", han, "f_sans_jp"},
		{"Chromium, Han in zh-CN", FallbackChromium, nil, "zh-CN", han, "g_sans_sc"},
		{"LibreOffice, Armenian in Liberation Serif", FallbackLibreOffice, []string{"Liberation Serif"}, "", armenian, "c_dejavu_serif"},
		{"LibreOffice, Armenian in Liberation Sans", FallbackLibreOffice, []string{"Liberation Sans"}, "", armenian, "d_dejavu_sans"},
		{"LibreOffice, Armenian in a missing roman family", FallbackLibreOffice, []string{"Imaginary", "serif"}, "", armenian, "c_dejavu_serif"},
		{"LibreOffice, Armenian in a missing family", FallbackLibreOffice, []string{"Imaginary"}, "", armenian, "d_dejavu_sans"},
		{"LibreOffice, Han in a missing Asian family", FallbackLibreOffice, []string{"Imaginary", "Liberation Serif"}, "", han, "g_sans_sc"},
		{"LibreOffice, Han in Liberation Serif", FallbackLibreOffice, []string{"Liberation Serif"}, "", han, "h_serif_sc"},
		{"LibreOffice, Han in Japanese", FallbackLibreOffice, []string{"Imaginary"}, "ja", han, "f_sans_jp"},
		{"LibreOffice from the rewritten list, Armenian in a missing family", FallbackLibreOffice, []string{"Imaginary"}, "", armenian, "d_dejavu_sans"},
	} {
		named := tc.named
		if named == nil {
			named = []string{"serif"}
		}
		fb := tc.fallback
		if fb == FallbackLibreOffice && !strings.Contains(tc.name, "rewritten") {
			fb = LibreOfficeFallback(named)
		}
		s := Style{Families: BrowserFamilies(named), Fallback: fb, Lang: tc.lang, Size: 10}
		if got := drawnBy(t, f, "A"+string(tc.r), s); !strings.Contains(got, tc.want) {
			t.Errorf("%s: drawn with %s, want %s", tc.name, got, tc.want)
		}
	}
}

// TestLayout_LastResortRanksByRevision locks that a character no family
// list names takes the face of the higher font revision, as fontconfig's
// fontversion ranks it.
//
// # Corpus evidence
//
//   - probe, html: in the gotenberg/gotenberg:9.4.1 image, Chromium prints
//     Gujarati in Noto Serif Gujarati, revision 2.002, over Noto Sans
//     Gujarati, revision 2.001, first in index order; LibreOffice does too.
func TestLayout_LastResortRanksByRevision(t *testing.T) {
	t.Parallel()
	const ka = 'ક'
	f := newTestFonts(t,
		fontFile{name: "a_sans.ttf", data: familyFace("Test Sans", "Regular", 400, false)},
		fontFile{name: "b_noto_sans.ttf", data: withRevision(familyFace("Noto Sans Gujarati", "Regular", 400, false, ka), 0x00020042)},
		fontFile{name: "c_noto_serif.ttf", data: withRevision(familyFace("Noto Serif Gujarati", "Regular", 400, false, ka), 0x00020083)},
	)
	if got := drawnBy(t, f, "A"+string(ka), style()); !strings.Contains(got, "c_noto_serif") {
		t.Errorf("ક drawn with %s, want the newer Noto Serif Gujarati", got)
	}
}

// TestLayout_LastResortKeepsTheFaceOfTheRun locks that a face of last
// resort keeps drawing the characters of its script and style it covers.
//
// # Corpus evidence
//
//   - probe, html: in the gotenberg/gotenberg:9.4.1 image, Chromium prints
//     Tamil that names no font in Noto Sans Tamil alone, although Noto Sans
//     Grantha, of the same revision and first in index order, covers
//     U+0BAA and U+0BB5; LibreOffice does too.
func TestLayout_LastResortKeepsTheFaceOfTheRun(t *testing.T) {
	t.Parallel()
	const ma, pa = 'ம', 'ப'
	f := newTestFonts(t,
		fontFile{name: "a_sans.ttf", data: familyFace("Test Sans", "Regular", 400, false)},
		fontFile{name: "b_grantha.ttf", data: familyFace("Noto Sans Grantha", "Regular", 400, false, pa)},
		fontFile{name: "c_tamil.ttf", data: familyFace("Noto Sans Tamil", "Regular", 400, false, ma, pa)},
	)
	if got := drawnBy(t, f, "A"+string(ma)+string(pa), style()); !strings.Contains(got, "c_tamil") {
		t.Errorf("ப after ம drawn with %s, want Noto Sans Tamil", got)
	}
	if got := drawnBy(t, f, "A"+string(pa), style()); !strings.Contains(got, "b_grantha") {
		t.Errorf("ப alone drawn with %s, want Noto Sans Grantha, first in index order", got)
	}
}

func TestWithoutBrowserDefaultsUndoesTheInserts(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ in, want []string }{
		{[]string{"Imaginary", "Noto Sans"}, []string{"Imaginary", "Noto Sans"}},
		{[]string{"Arial", "monospace"}, []string{"Arial", "monospace"}},
		{[]string{"Imaginary", "sans-serif"}, []string{"Imaginary", "sans-serif"}},
	} {
		if got := withoutBrowserDefaults(BrowserFamilies(tc.in)); !slices.Equal(got, tc.want) {
			t.Errorf("withoutBrowserDefaults(BrowserFamilies(%q)) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestFonts_CoversByFamilyAndAliases(t *testing.T) {
	t.Parallel()
	f := newTestFonts(t,
		fontFile{name: "a_symbols.ttf", data: familyFace("Standard Symbols PS", "Regular", 400, false)},
	)
	if f.Covers("Symbol", false, 'A') {
		t.Error("Symbol covers A by itself, want only through its clone")
	}
	if !f.Covers("Symbol", true, 'A') {
		t.Error("Symbol does not cover A through Standard Symbols PS")
	}
	if f.Covers("Standard Symbols PS", true, 'Z') {
		t.Error("Standard Symbols PS covers Z, which it lacks")
	}
}
