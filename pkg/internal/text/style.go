package text

import (
	"fmt"
	"math"

	"github.com/gotenberg/litho/pkg/internal/compose"
)

// maxFontSize bounds [Style.Size], and the magnitude of [Style.LetterSpacing]
// and [Style.WordSpacing], in points. Overflow guard, not spec-mandated:
// go-text's HarfBuzz scales glyph metrics in 32-bit integers, which stay
// exact for any glyph at sizes up to 1000 points, and documents never set
// type larger than a poster headline.
const maxFontSize = 1000

// Style is what a converter asks for one run of text.
type Style struct {
	// Families lists family names in preference order, matched without
	// regard to case or spaces against a face's family and typographic
	// family names. An empty list means sans-serif. A concrete family
	// brings its metric-compatible aliases right after it (see
	// [Fonts.Match]).
	//
	// A generic family expands, in place, to these families, each with its
	// metric-compatible aliases, after fontconfig's 60-latin.conf:
	//
	//   - serif: DejaVu Serif, Times New Roman, Thorndale AMT, Luxi Serif,
	//     Nimbus Roman No9 L, Nimbus Roman, Times, Noto Serif
	//   - sans-serif: DejaVu Sans, Verdana, Arial, Albany AMT, Luxi Sans,
	//     Nimbus Sans L, Nimbus Sans, Helvetica, Lucida Sans Unicode, BPG
	//     Glaho International, Tahoma, Noto Sans
	//   - monospace: DejaVu Sans Mono, Inconsolata, Andale Mono, Courier
	//     New, Cumberland AMT, Luxi Mono, Nimbus Mono L, Nimbus Mono, Nimbus
	//     Mono PS, Courier, Noto Sans Mono
	//   - cursive: TeX Gyre Chorus, URW Chancery L, Z003, ITC Zapf
	//     Chancery, Zapfino, Comic Sans MS, Comic Neue
	//   - fantasy: Impact, Copperplate Gothic Std, Cooper Std, Bauhaus Std,
	//     Papyrus
	//   - system-ui: Cantarell, Noto Sans UI, Segoe UI, then the sans-serif
	//     list
	//
	// With a Chinese, Japanese or Korean [Style.Lang], the Noto CJK family
	// of the language (Noto Sans CJK JP, SC, TC, HK or KR; Noto Serif CJK
	// for serif; Noto Sans Mono CJK for monospace) comes before the list,
	// as fontconfig's 70-noto-cjk.conf does. Since serif lists DejaVu Serif
	// first, it picks DejaVu Serif over Liberation Serif when both are
	// installed; [BrowserFamilies] rewrites a list to pick the faces
	// Chromium picks.
	Families []string
	// Weight is the CSS weight, from 1 to 1000; 400 is regular and 700
	// bold. Zero means 400; values out of range are clamped.
	Weight int
	// Italic asks for an italic face, then an oblique one.
	Italic bool
	// Oblique, when not zero with Italic, asks for an oblique face of that
	// angle in degrees, CSS font-style: oblique <angle>: a face without
	// one is slanted by it rather than by the default synthetic angle
	// (CSS Fonts 4, 2.4 and 5.5).
	Oblique float64
	// Stretch is the width asked for, in percent of the normal width, CSS
	// font-stretch (CSS Fonts 4, 2.3); 0 means 100.
	Stretch float64
	// NoSyntheticBold and NoSyntheticItalic forbid synthesising the bold
	// and the italic a face lacks, CSS font-synthesis without weight or
	// without style (CSS Fonts 4, 5.5).
	NoSyntheticBold, NoSyntheticItalic bool
	// NoSpacingTrim keeps the full width of adjacent fullwidth
	// punctuation, CSS text-spacing-trim: space-all; by default it is
	// trimmed as Chromium's text-spacing-trim: normal does (see
	// [Fonts.Shape]).
	NoSpacingTrim bool
	// Size is the font size in points, greater than 0 and at most 1000.
	Size float64
	// Lang is the BCP 47 language tag of the text, "" when unknown. It
	// selects language-specific shaping and, for Chinese, Japanese and
	// Korean, the Noto CJK variant matching and fallback prefer.
	Lang string
	// Color fills the glyphs.
	Color compose.Color
	// LetterSpacing is added after every character cluster, in points,
	// as CSS letter-spacing does. It may be negative; its magnitude is at
	// most 1000.
	LetterSpacing float64
	// WordSpacing is added to every word separator (U+0020 SPACE, U+00A0
	// NO-BREAK SPACE and a few script-specific separators), in points. It
	// may be negative; its magnitude is at most 1000.
	WordSpacing float64
}

// validate reports whether s can be laid out.
func (s Style) validate() error {
	if !(s.Size > 0 && s.Size <= maxFontSize) {
		return fmt.Errorf("%w: size %v outside (0, %d]", ErrInvalidStyle, s.Size, maxFontSize)
	}
	if !finiteWithin(s.LetterSpacing, maxFontSize) {
		return fmt.Errorf("%w: letter spacing %v outside [-%d, %d]", ErrInvalidStyle, s.LetterSpacing, maxFontSize, maxFontSize)
	}
	if !finiteWithin(s.Oblique, 90) || !finiteWithin(s.Stretch, 1000) || s.Stretch < 0 {
		return fmt.Errorf("%w: oblique angle %v or stretch %v out of range", ErrInvalidStyle, s.Oblique, s.Stretch)
	}
	if !finiteWithin(s.WordSpacing, maxFontSize) {
		return fmt.Errorf("%w: word spacing %v outside [-%d, %d]", ErrInvalidStyle, s.WordSpacing, maxFontSize, maxFontSize)
	}
	return nil
}

// finiteWithin reports whether v is finite and |v| <= bound.
func finiteWithin(v, bound float64) bool {
	return !math.IsNaN(v) && math.Abs(v) <= bound
}

// weight returns the CSS weight s asks for, defaulted and clamped.
func (s Style) weight() float32 {
	switch {
	case s.Weight == 0:
		return 400
	case s.Weight < 1:
		return 1
	case s.Weight > 1000:
		return 1000
	}
	return float32(s.Weight)
}

// stretch returns the width s asks for in percent, defaulted.
func (s Style) stretch() float64 {
	if s.Stretch <= 0 {
		return 100
	}
	return s.Stretch
}
