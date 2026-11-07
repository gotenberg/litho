package text

import (
	"context"
	"math"
	"slices"
	"strings"
	"testing"
)

func TestSynthesis_BoldAndItalic(t *testing.T) {
	t.Parallel()
	regular := []fontFile{{name: "sans_regular.ttf", data: testSans("Regular", 400, false, 100)}}
	for _, tc := range []struct {
		name         string
		files        []fontFile
		weight       int
		italic       bool
		size         float64
		stroke, skew float64
		mod          func(*Style)
	}{
		{"oblique angle", regular, 400, true, 10, 0, math.Tan(30 * math.Pi / 180), func(s *Style) { s.Oblique = 30 }},
		{"no synthetic bold", regular, 700, false, 10, 0, 0, func(s *Style) { s.NoSyntheticBold = true }},
		{"no synthetic italic", regular, 700, true, 9, 9.0 / 24, 0, func(s *Style) { s.NoSyntheticItalic = true }},
		{"regular", regular, 400, false, 10, 0, 0, nil},
		// Chromium emboldens past 200 heavier than the face: 700 on 400,
		// not 600.
		{"semibold", regular, 600, false, 10, 0, 0, nil},
		{"bold small", regular, 700, false, 9, 9.0 / 24, 0, nil},
		{"bold large", regular, 700, false, 36, 36.0 / 32, 0, nil},
		{"bold between", regular, 700, false, 22.5, 22.5 * (1.0/24 + 0.5*(1.0/32-1.0/24)), 0, nil},
		{"italic", regular, 400, true, 10, 0, 0.25, nil},
		{"bold italic", regular, 700, true, 10, 10.0 * (1.0/24 + (1.0/27)*(1.0/32-1.0/24)), 0.25, nil},
		{"bold face", sansFamily(), 700, false, 10, 0, 0, nil},
		{"italic face", sansFamily(), 400, true, 10, 0, 0, nil},
		// Bold italic in a family of regular, bold and italic takes the
		// italic face, which is emboldened.
		{"bold italic face", sansFamily(), 700, true, 10, 10.0 * (1.0/24 + (1.0/27)*(1.0/32-1.0/24)), 0, nil},
	} {
		f := newTestFonts(t, tc.files...)
		s := style()
		s.Weight, s.Italic, s.Size = tc.weight, tc.italic, tc.size
		if tc.mod != nil {
			tc.mod(&s)
		}
		lines, _ := layout(t, f, "AB", s, 1000, Collapse)
		p := shape(t, f, []ParagraphRun{{Text: "AB", Style: s}}, ParagraphOptions{})
		sp := p.Line(0, p.Len(), false).Spans
		for _, got := range [][2]float64{{lines[0].Runs[0].Stroke, lines[0].Runs[0].Skew}, {sp[0].Text.Stroke, sp[0].Text.Skew}} {
			if math.Abs(got[0]-tc.stroke) > 1e-9 || math.Abs(got[1]-tc.skew) > 1e-9 {
				t.Errorf("%s: stroke %v skew %v, want %v and %v", tc.name, got[0], got[1], tc.stroke, tc.skew)
			}
		}
	}
}

func TestLayout_MissingEmojiDrawBlank(t *testing.T) {
	t.Parallel()
	f := newTestFonts(t, sansFamily()...)
	lines, rep := layout(t, f, "A\U0001F680❤️中", style(), 1000, Collapse)
	// U+FE0F is default ignorable: it is no missing glyph.
	if want := []rune{'❤', '\U0001F680'}; !slices.Equal(rep.MissingEmoji, want) {
		t.Errorf("MissingEmoji %q, want %q", rep.MissingEmoji, want)
	}
	if want := []rune{'中'}; !slices.Equal(rep.MissingGlyphs, want) {
		t.Errorf("MissingGlyphs %q, want %q", rep.MissingGlyphs, want)
	}
	if got := lineText(lines[0]); got != "A\U0001F680❤️中" {
		t.Errorf("text %q", got)
	}
	gid := map[string]uint16{}
	for _, r := range lines[0].Runs {
		for _, g := range r.Glyphs {
			if g.Text != "" {
				gid[g.Text] = g.GID
			}
		}
	}
	// The emoji draw the space glyph, the CJK character the .notdef box.
	if gid["\U0001F680"] == 0 || gid["❤️"] == 0 || gid["中"] != 0 {
		t.Errorf("glyphs %v: want the emoji blank and the CJK character .notdef", gid)
	}
}

func TestShape_ThaiAndDevanagariReachTheirNotoFaces(t *testing.T) {
	t.Parallel()
	const thai, deva = 'ส', 'न'
	f := newTestFonts(t,
		fontFile{name: "a_sans.ttf", data: familyFace("Liberation Sans", "Regular", 400, false)},
		fontFile{name: "b_other.ttf", data: familyFace("Other", "Regular", 400, false, thai, deva)},
		fontFile{name: "c_deva.ttf", data: familyFace("Noto Sans Devanagari", "Regular", 400, false, deva)},
		fontFile{name: "d_thai.ttf", data: familyFace("Noto Sans Thai", "Regular", 400, false, thai)},
	)
	s := Style{Families: []string{"Liberation Sans"}, Size: 10}
	p, rep, err := f.Shape(context.Background(), []ParagraphRun{{Text: "A" + string(thai) + " " + string(deva), Style: s}}, ParagraphOptions{})
	if err != nil || len(rep.MissingGlyphs) != 0 {
		t.Fatalf("Shape: %v, report %+v", err, rep)
	}
	var keys []string
	for _, sp := range p.Line(0, p.Len(), false).Spans {
		if sp.Text != nil {
			keys = append(keys, sp.Text.Face.Key)
		}
	}
	joined := strings.Join(keys, " ")
	if !strings.Contains(joined, "d_thai") || !strings.Contains(joined, "c_deva") || strings.Contains(joined, "b_other") {
		t.Errorf("faces %v: want Thai and Devanagari from their Noto faces", keys)
	}
}
