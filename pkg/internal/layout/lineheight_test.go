package layout

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
)

// tallFallback indexes Ahem and a face covering U+2603 alone whose
// ascent is 0.95 em and descent 0.05 em, unlike Ahem's 0.8 and 0.2.
func tallFallback(t *testing.T) *text.Fonts {
	t.Helper()
	prog := fontbuild.TrueType{
		PostScriptName: "Tall", Family: "Tall", Subfamily: "Regular", NotdefAdvance: 1000, NoOS2: true,
		Glyphs: []fontbuild.Glyph{{Name: "snowman", Unicode: '☃', Advance: 1000}},
	}.Build()
	n := int(binary.BigEndian.Uint16(prog[4:]))
	for i := range n {
		rec := prog[12+16*i:]
		if string(rec[:4]) == "hhea" {
			off := binary.BigEndian.Uint32(rec[8:])
			binary.BigEndian.PutUint16(prog[off+4:], 950)
			binary.BigEndian.PutUint16(prog[off+6:], uint16(0x10000-50))
		}
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tall.ttf"), prog, 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := text.NewFonts(context.Background(), nil, []string{"testdata", dir})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// cjkSerif indexes Ahem, a face named Liberation Serif covering X with
// an ascent of 0.9 em and a descent of 0.1 em, and one named Noto Serif
// CJK JP covering X with an ascent of 1.1 em and a descent of 0.3 em.
func cjkSerif(t *testing.T) *text.Fonts {
	t.Helper()
	dir := t.TempDir()
	for _, face := range []struct {
		file, ps, family string
		ascent, descent  uint16
	}{
		{"serif.ttf", "LiberationSerif", "Liberation Serif", 900, 100},
		{"cjk.ttf", "NotoSerifCJKjp-Regular", "Noto Serif CJK JP", 1100, 300},
	} {
		prog := fontbuild.TrueType{
			PostScriptName: face.ps, Family: face.family, Subfamily: "Regular", NotdefAdvance: 1000, NoOS2: true,
			Glyphs: []fontbuild.Glyph{{Name: "space", Unicode: ' ', Advance: 500, Empty: true}, {Name: "X", Unicode: 'X', Advance: 1000}},
		}.Build()
		n := int(binary.BigEndian.Uint16(prog[4:]))
		for i := range n {
			rec := prog[12+16*i:]
			if string(rec[:4]) == "hhea" {
				off := binary.BigEndian.Uint32(rec[8:])
				binary.BigEndian.PutUint16(prog[off+4:], face.ascent)
				binary.BigEndian.PutUint16(prog[off+6:], -face.descent)
			}
		}
		if err := os.WriteFile(filepath.Join(dir, face.file), prog, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	f, err := text.NewFonts(context.Background(), nil, []string{"testdata", dir})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// Under Chromium's fallback rule, serif in Japanese is Noto Serif CJK JP,
// for the line's strut as for its text: line-height normal is that face's
// 11 above the baseline and 3 below, where serif in English is Liberation
// Serif's 9 and 1.
func TestLineHeight_GenericFamilyInLanguage(t *testing.T) {
	f := cjkSerif(t)
	for _, tc := range []struct {
		name     string
		lang     string
		fallback text.Fallback
		want     float64
	}{
		{"chromium ja", "ja", text.FallbackChromium, px(14)},
		{"chromium en", "en", text.FallbackChromium, px(10)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := sty(func(s *css.Style) {
				s.FontFamilies = []string{"serif"}
				s.LineHeight = css.Dimension{Kind: css.Normal}
			})
			root := el(st, txt(st, "X"))
			root.Lang = tc.lang
			pb := page(200, 200)
			e := newEngine(context.Background(), f, Options{Page: pb, Fallback: tc.fallback}, pb.Width, pb.Height)
			rb := e.generate(doc(root))
			e.layoutRoot(rb)
			if e.err != nil {
				t.Fatal(e.err)
			}
			if got := kid(t, rb, 0, 0).h; !near(got, tc.want) {
				t.Errorf("line box %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLineHeight_FallbackFace(t *testing.T) {
	f := tallFallback(t)
	tests := []struct {
		name string
		lh   css.Dimension
		want float64
	}{
		// line-height: 1 lays the line out on the first available
		// font's metrics alone, whichever face draws the glyphs.
		{"number", css.Dimension{Kind: css.Number, Value: 1}, px(10)},
		// normal takes the fallback face's own ascent: 9.5 above the
		// baseline and Ahem's 2 below.
		{"normal", css.Dimension{Kind: css.Normal}, px(11.5)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := sty(func(s *css.Style) { s.LineHeight = tt.lh })
			pb := page(200, 200)
			e := newEngine(context.Background(), f, Options{Page: pb}, pb.Width, pb.Height)
			rb := e.generate(doc(el(st, txt(st, "X☃X"))))
			e.layoutRoot(rb)
			if e.err != nil {
				t.Fatal(e.err)
			}
			if got := kid(t, rb, 0, 0).h; !near(got, tt.want) {
				t.Errorf("line box %v, want %v", got, tt.want)
			}
		})
	}
}

// The initial font-family is the browser's standard font, which Chromium
// draws in Japanese with Noto Sans CJK JP; an author's serif is Noto Serif
// CJK JP.
func TestTextStyle_InitialFamilyIsTheStandardFont(t *testing.T) {
	pb := page(200, 200)
	e := newEngine(context.Background(), ahem(t), Options{Page: pb, Fallback: text.FallbackChromium}, pb.Width, pb.Height)
	initial := css.Initial()
	serif := css.Initial()
	serif.FontFamilies = []string{"serif"}
	if got := e.textStyle(initial, "ja").Families; len(got) == 0 || got[0] != "Noto Sans CJK JP" {
		t.Errorf("initial family in ja = %q, want Noto Sans CJK JP first", got)
	}
	if got := e.textStyle(serif, "ja").Families; len(got) == 0 || got[0] != "Noto Serif CJK JP" {
		t.Errorf("serif in ja = %q, want Noto Serif CJK JP first", got)
	}
}

// A line that holds a forced break alone takes the line heights of the
// inline boxes the break sits in (CSS 2.1, 10.8; CSS Inline 3, 2.3): a
// <br> in a span of line-height 50px makes a line 50px tall, whatever
// the break's own line height, as Chromium lays it out.
func TestLineHeight_BreakAloneTakesItsInlineBoxes(t *testing.T) {
	block := sty()
	span := inlineSty(func(s *css.Style) { s.LineHeight = css.Dimension{Kind: css.Number, Value: 5} })
	brSt := inlineSty(func(s *css.Style) { s.LineHeight = css.Dimension{Kind: css.Number, Value: 20} })
	pb := page(200, 300)
	e := newEngine(context.Background(), ahem(t), Options{Page: pb, BrowserRules: true}, pb.Width, pb.Height)
	rb := e.generate(doc(el(block, el(span, br(brSt)), txt(block, "X"))))
	e.layoutRoot(rb)
	if e.err != nil {
		t.Fatal(e.err)
	}
	lines := kid(t, rb, 0, 0).lines
	if len(lines) != 2 {
		t.Fatalf("%d lines, want 2", len(lines))
	}
	if got := lines[0].h; !near(got, px(50)) {
		t.Errorf("line of the break %v, want %v", got, px(50))
	}
	if got := lines[1].y - lines[0].y; !near(got, px(50)) {
		t.Errorf("second line %v below the first, want %v", got, px(50))
	}
}
