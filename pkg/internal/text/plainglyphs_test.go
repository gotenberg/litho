package text

import (
	"bytes"
	"image"
	"image/png"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
)

// plainGlyphFonts indexes Test Sans, "Test Mono Emoji", a text face
// covering U+263A and U+231A, "Test Colour Emoji", a colour bitmap face
// without outlines covering both and U+26BD, and "Test Layers", a layered
// colour face whose U+2764 has an outline and whose U+2B50 has none.
func plainGlyphFonts(t *testing.T) *Fonts {
	t.Helper()
	var png1 bytes.Buffer
	if err := png.Encode(&png1, image.NewNRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	mono := fontbuild.TrueType{
		PostScriptName: "TestMonoEmoji", Family: "Test Mono Emoji", Subfamily: "Regular", NotdefAdvance: 500,
		Glyphs: []fontbuild.Glyph{
			{Name: "space", Unicode: ' ', Advance: spaceAdvance, Empty: true},
			{Name: "smile", Unicode: 0x263A, Advance: 800},
			{Name: "watch", Unicode: 0x231A, Advance: 800},
		},
	}.Build()
	bitmap := fontbuild.BitmapGlyph{PNG: png1.Bytes(), Width: 4, Height: 4, Top: 4, Advance: 4}
	cblc, cbdt := fontbuild.ColorBitmaps(4, map[uint16]fontbuild.BitmapGlyph{1: bitmap, 2: bitmap, 3: bitmap})
	colour := fontbuild.TrueType{
		PostScriptName: "TestColourEmoji", Family: "Test Colour Emoji", Subfamily: "Regular", NotdefAdvance: 1000,
		Glyphs: []fontbuild.Glyph{
			{Name: "smile", Unicode: 0x263A, Advance: 1000, Empty: true},
			{Name: "watch", Unicode: 0x231A, Advance: 1000, Empty: true},
			{Name: "ball", Unicode: 0x26BD, Advance: 1000, Empty: true},
			{Name: "space", Unicode: ' ', Advance: 1000, Empty: true},
		},
		Extra: map[string][]byte{"CBLC": cblc, "CBDT": cbdt},
	}.Build()
	colr, cpal := fontbuild.ColorLayers(map[uint16][]fontbuild.Layer{1: {{GID: 1, Palette: 0}}, 2: {{GID: 1, Palette: 0}}},
		[][][4]uint8{{{255, 0, 0, 255}}})
	layers := fontbuild.TrueType{
		PostScriptName: "TestLayers", Family: "Test Layers", Subfamily: "Regular", NotdefAdvance: 1000,
		Glyphs: []fontbuild.Glyph{
			{Name: "heart", Unicode: 0x2764, Advance: 1000},
			{Name: "star", Unicode: 0x2B50, Advance: 1000, Empty: true},
		},
		Extra: map[string][]byte{"COLR": colr, "CPAL": cpal},
	}.Build()
	return newTestFonts(t, append(sansFamily(),
		fontFile{name: "mono.ttf", data: mono}, fontFile{name: "colour.ttf", data: colour}, fontFile{name: "layers.ttf", data: layers})...)
}

// TestLayout_PlainGlyphsDrawNoColourGlyph locks what
// [Fonts.WithPlainGlyphs] changes of a layout. An emoji takes text
// presentation, the emoji presentation selector included, so the face
// without colour glyphs draws it. A colour face draws a character only
// with an outline of its own: a face of bitmaps alone covers nothing and
// a layered glyph without an outline is not covered, so the character
// only such a glyph maps is reported missing; a layered glyph with an
// outline is drawn, its run plain. The index it came from lays out as
// before.
func TestLayout_PlainGlyphsDrawNoColourGlyph(t *testing.T) {
	f := plainGlyphFonts(t)
	plain := f.WithPlainGlyphs()
	if plain.WithPlainGlyphs() != plain {
		t.Error("WithPlainGlyphs on its own result returned another index")
	}
	if plain.ForConversion() != plain {
		t.Error("ForConversion on a plain index returned another index")
	}
	s := Style{Families: []string{"Test Sans"}, Size: 10, Fallback: FallbackChromium}
	cases := []struct {
		name          string
		text          string
		r             rune
		colour, plain string
	}{
		{"default emoji", "A⌚", 0x231A, "colour.ttf", "mono.ttf"},
		{"emoji presentation selector", "A☺️", 0x263A, "colour.ttf", "mono.ttf"},
		{"layered glyph with an outline", "A❤️", 0x2764, "layers.ttf", "layers.ttf"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, side := range []struct {
				fonts *Fonts
				want  string
				plain bool
			}{{f, c.colour, false}, {plain, c.plain, true}} {
				lines, rep := layout(t, side.fonts, c.text, s, 1000, Collapse)
				if got := faceOf(t, lines, c.r); !strings.Contains(got, side.want) {
					t.Errorf("plain %t: face %q, want %s", side.plain, got, side.want)
				}
				if len(rep.MissingEmoji) != 0 || len(rep.MissingGlyphs) != 0 {
					t.Errorf("plain %t: missing %q and %q, want none", side.plain, string(rep.MissingEmoji), string(rep.MissingGlyphs))
				}
				for _, l := range lines {
					for _, run := range l.Runs {
						if want := side.plain && strings.Contains(run.Face.Key, "layers.ttf"); run.Plain != want {
							t.Errorf("plain %t: run of %s has Plain %t, want %t", side.plain, run.Face.Key, run.Plain, want)
						}
					}
				}
			}
		})
	}
	for _, c := range []struct {
		name string
		text string
		r    rune
		face string
	}{
		{"only a face of bitmaps covers", "A⚽", 0x26BD, "colour.ttf"},
		{"only a layered glyph without an outline covers", "A⭐", 0x2B50, "layers.ttf"},
	} {
		t.Run(c.name, func(t *testing.T) {
			lines, rep := layout(t, f, c.text, s, 1000, Collapse)
			if got := faceOf(t, lines, c.r); !strings.Contains(got, c.face) || len(rep.MissingEmoji) != 0 {
				t.Errorf("with colour glyphs: face %q, missing %q, want %s and none", got, string(rep.MissingEmoji), c.face)
			}
			lines, rep = layout(t, plain, c.text, s, 1000, Collapse)
			if !slices.Equal(rep.MissingEmoji, []rune{c.r}) {
				t.Errorf("without colour glyphs: missing %q, want %q", string(rep.MissingEmoji), string(c.r))
			}
			if got := faceOf(t, lines, c.r); strings.Contains(got, c.face) {
				t.Errorf("without colour glyphs: %s draws the character", got)
			}
		})
	}
}

// TestLayout_PlainGlyphsReportTheRunsOwnColourFace checks the face a run
// names first: a run is drawn by it where no other face covers a
// character, so the glyph of a face of bitmaps alone, which has no
// outline, is reported missing, while its space is not.
func TestLayout_PlainGlyphsReportTheRunsOwnColourFace(t *testing.T) {
	f := plainGlyphFonts(t)
	s := Style{Families: []string{"Test Colour Emoji"}, Size: 10, Fallback: FallbackChromium}
	if _, rep := layout(t, f, "\u26bd \u26bd", s, 1000, Preserve); len(rep.MissingEmoji) != 0 || len(rep.MissingGlyphs) != 0 {
		t.Errorf("with colour glyphs: missing %q and %q, want none", string(rep.MissingEmoji), string(rep.MissingGlyphs))
	}
	_, rep := layout(t, f.WithPlainGlyphs(), "\u26bd \u26bd", s, 1000, Preserve)
	if !slices.Equal(rep.MissingEmoji, []rune{0x26BD}) || len(rep.MissingGlyphs) != 0 {
		t.Errorf("without colour glyphs: missing emoji %q and glyphs %q, want the ball alone", string(rep.MissingEmoji), string(rep.MissingGlyphs))
	}
}

// TestLayout_PlainGlyphsKeepDocumentFaces checks that an index of document
// faces made from a plain index stays plain.
func TestLayout_PlainGlyphsKeepDocumentFaces(t *testing.T) {
	plain := plainGlyphFonts(t).WithPlainGlyphs()
	f := plain.WithFaces(NewFaceSet(nil).Declare("Some Web Font"))
	if f == plain {
		t.Fatal("WithFaces returned the index it was given: the set declares a family")
	}
	s := Style{Families: []string{"Test Sans"}, Size: 10, Fallback: FallbackChromium}
	lines, _ := layout(t, f, "A\u231a", s, 1000, Collapse)
	if got := faceOf(t, lines, 0x231A); !strings.Contains(got, "mono.ttf") {
		t.Errorf("face %q, want mono.ttf", got)
	}
}
