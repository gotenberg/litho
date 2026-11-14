package text

import (
	"bytes"
	"image"
	"image/png"
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
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

// TestOfficeEmoji checks that a glyph of a colour face in a run that asks
// for Office's emoji advances as Segoe UI Emoji does, its drawing centred,
// and takes Segoe UI Emoji's ascent and descent with Word's metrics alone;
// a glyph of another face, and every glyph of a run that does not ask,
// keep their own.
func TestOfficeEmoji(t *testing.T) {
	f := emojiFonts(t)
	for _, tc := range []struct {
		name         string
		office, word bool
		adv, ascent  float64
	}{
		{"not asked", false, true, 10, 0},
		{"PowerPoint", true, false, 13.73, 0},
		{"Word", true, true, 13.73, 10.79},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := Style{Families: []string{"Test Sans"}, Size: 10, Fallback: FallbackFontconfig, OfficeEmoji: tc.office, WordMetrics: tc.word}
			p := shape(t, f, []ParagraphRun{{Text: "A⌚B", Style: s}}, ParagraphOptions{})
			if got := p.Advance(1); math.Abs(got-tc.adv) > 0.01 {
				t.Errorf("emoji advance %.3f, want %.3f", got, tc.adv)
			}
			plain := shape(t, f, []ParagraphRun{{Text: "A⌚B", Style: Style{Families: s.Families, Size: 10, Fallback: FallbackFontconfig}}}, ParagraphOptions{})
			if p.Advance(0) != plain.Advance(0) || p.Advance(2) != plain.Advance(2) {
				t.Errorf("letters advance %v and %v, want %v and %v", p.Advance(0), p.Advance(2), plain.Advance(0), plain.Advance(2))
			}
			found := false
			for _, sp := range p.Line(0, 3, false).Spans {
				if sp.Start != 1 || sp.Text == nil {
					continue
				}
				found = true
				// The drawing starts half the added advance in.
				if x := sp.Text.Glyphs[0].X; math.Abs(x-(tc.adv-10)/2) > 0.01 {
					t.Errorf("emoji drawn at %.3f in its piece, want %.3f", x, (tc.adv-10)/2)
				}
				if tc.ascent > 0 && (math.Abs(sp.Ascent-tc.ascent) > 0.01 || math.Abs(sp.Descent-2.51) > 0.01) {
					t.Errorf("emoji ascent %.2f descent %.2f, want %.2f and 2.51", sp.Ascent, sp.Descent, tc.ascent)
				}
				if tc.ascent == 0 && math.Abs(sp.Ascent-10.79) < 0.01 {
					t.Errorf("emoji ascent %.2f, want the face's own", sp.Ascent)
				}
			}
			if !found {
				t.Error("no piece holds the emoji alone")
			}
		})
	}
}

// TestOfficeEmoji_FlagKeepsItsAdvance checks that a flag of a colour face
// in a run that asks for Office's emoji keeps the face's advance, where
// another emoji of the face takes Segoe UI Emoji's.
func TestOfficeEmoji_FlagKeepsItsAdvance(t *testing.T) {
	var png1 bytes.Buffer
	if err := png.Encode(&png1, image.NewNRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	bitmap := fontbuild.BitmapGlyph{PNG: png1.Bytes(), Width: 4, Height: 4, Top: 4, Advance: 4}
	cblc, cbdt := fontbuild.ColorBitmaps(4, map[uint16]fontbuild.BitmapGlyph{1: bitmap, 2: bitmap, 3: bitmap})
	colour := fontbuild.TrueType{
		PostScriptName: "TestFlagEmoji", Family: "Test Flag Emoji", Subfamily: "Regular", NotdefAdvance: 1000,
		Glyphs: []fontbuild.Glyph{
			{Name: "F", Unicode: 0x1F1EB, Advance: 1000, Empty: true},
			{Name: "R", Unicode: 0x1F1F7, Advance: 1000, Empty: true},
			{Name: "watch", Unicode: 0x231A, Advance: 1000, Empty: true},
		},
		Extra: map[string][]byte{"CBLC": cblc, "CBDT": cbdt},
	}.Build()
	f := newTestFonts(t, append(sansFamily(), fontFile{name: "flag.ttf", data: colour})...)
	s := Style{Families: []string{"Test Flag Emoji"}, Size: 10, Fallback: FallbackFontconfig, OfficeEmoji: true}
	p := shape(t, f, []ParagraphRun{{Text: "\U0001F1EB\U0001F1F7\u231a", Style: s}}, ParagraphOptions{})
	plain := s
	plain.OfficeEmoji = false
	own := shape(t, f, []ParagraphRun{{Text: "\U0001F1EB\U0001F1F7", Style: plain}}, ParagraphOptions{})
	if flag, want := p.Advance(0)+p.Advance(1), own.Advance(0)+own.Advance(1); flag != want || flag == 0 {
		t.Errorf("flag advance %.3f, want the face's %.3f", flag, want)
	}
	if got := p.Advance(2); math.Abs(got-13.73) > 0.01 {
		t.Errorf("watch advance %.3f, want 13.73", got)
	}
}
