package text

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/language"
	"github.com/go-text/typesetting/shaping"

	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/typeset"
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
	// [Fonts.Match]). A name in double quotes is a family name and never a
	// generic family, as a quoted CSS family name is (CSS Fonts 4,
	// 2.1.1): "\"serif\"" matches a face named serif; the quotes do not
	// count in the match.
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
	// as fontconfig's 70-noto-cjk.conf does, except under
	// [FallbackChromium], where the list itself names the family of the
	// language ([BrowserFamilies]). Since serif lists DejaVu Serif first,
	// it picks DejaVu Serif over Liberation Serif when both are installed;
	// [BrowserFamilies] rewrites a list to pick the faces Chromium picks.
	Families []string
	// Weight is the CSS weight, from 1 to 1000; 400 is regular and 700
	// bold. Zero means 400; values out of range are clamped.
	Weight int
	// Italic asks for an italic face, then an oblique one.
	Italic bool
	// Fallback is the rule that picks the face of a character the matched
	// face lacks (see [Fonts.Layout]); the zero value follows fontconfig.
	Fallback Fallback
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
	// Autospace is the space, in points, added between an ideograph and
	// a letter of another script when AutospaceAlpha is set, or a digit
	// when AutospaceNumeric is, CSS text-autospace (CSS Text 4, 8.2). Its
	// magnitude is at most 1000.
	Autospace                        float64
	AutospaceAlpha, AutospaceNumeric bool
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
	Color typeset.Color
	// LetterSpacing is added after every character cluster, in points,
	// as CSS letter-spacing does. It may be negative; its magnitude is at
	// most 1000.
	LetterSpacing float64
	// WordSpacing is added to every word separator (U+0020 SPACE, U+00A0
	// NO-BREAK SPACE and a few script-specific separators), in points. It
	// may be negative; its magnitude is at most 1000.
	WordSpacing float64
	// Scale scales the glyphs horizontally, the Office formats' text
	// scale: [Fonts.Shape] shapes at the natural advances, lays the
	// paragraph out at the scaled ones and writes each run's glyph
	// positions unscaled with the scale as its [typeset.Text.HScale]. Letter
	// spacing is added after scaling, unscaled. 0 and 1 draw at the
	// natural width; otherwise it lies in [typeset.MinHScale,
	// typeset.MaxHScale]. [Fonts.Layout] ignores it.
	Scale float64
	// NoKerning shapes the run without the font's kerning, CSS
	// font-kerning: none (CSS Fonts 4, 6.3): the shaper is handed the kern
	// feature at 0, which turns off the GPOS kern feature and the kern
	// table alike. The default kerns.
	NoKerning bool
	// Settings holds the font settings few runs set, nil for none: a
	// style that sets none costs no more than a pointer.
	Settings *FontSettings
	// WordMetrics gives the line box of a run the vertical metrics Word
	// lays a Microsoft face out with when a metric-compatible stand-in
	// draws the face the style names, as Word prints WordprocessingML:
	// its ascent and descent, and no line gap. Only the line box changes:
	// the glyphs keep the stand-in's outlines and advances. A face whose
	// stand-in records Word's metrics keeps them, and when the index
	// holds the named face itself, it draws the run and nothing changes.
	WordMetrics bool
	// OfficeEmoji lays out the emoji a colour face draws as Word and
	// PowerPoint lay out Segoe UI Emoji, the face they draw emoji in, which
	// the index does not hold: each glyph of a colour face advances
	// [segoeEmojiAdvance] ems, its drawing centred in it, and, with
	// WordMetrics, takes Segoe UI Emoji's ascent and descent for its line
	// box. The glyphs keep the colour face's drawing. [Fonts.Layout]
	// ignores it.
	OfficeEmoji bool
	// Orientation sets the run's characters upright or sideways in a
	// vertical line (see [Orientation]); the zero value sets the run as
	// horizontal text. [Fonts.Layout] ignores it.
	Orientation Orientation
}

// FontSettings are the font settings of a [Style] that most text leaves
// at their defaults. A FontSettings is shared between styles and never
// modified once a style holds it.
type FontSettings struct {
	// Features are OpenType features handed to the shaper after the
	// font's defaults and the kerning NoKerning turns off, in order: a
	// later setting of a tag overrides an earlier one, as the CSS feature
	// resolution asks (CSS Fonts 4, 7.2). A tag is four bytes from U+20
	// to U+7E; another entry is ignored.
	Features []Feature
	// SizeAdjust scales each face so that its metric keeps a constant
	// fraction of Size, CSS font-size-adjust (see [SizeAdjust]).
	SizeAdjust SizeAdjust
	// LanguageSystem is the OpenType language system tag the text is
	// shaped with in place of the one Lang maps to, CSS
	// font-language-override (CSS Fonts 4, 6.14): up to four characters
	// from U+20 to U+7E, padded with spaces. "" keeps Lang's.
	LanguageSystem string
	// Variations are the axis values of a variable face set after those
	// the style's weight, stretch, slant and size and the face's
	// descriptors imply, in order, a later value of a tag overriding an
	// earlier one: CSS font-variation-settings (CSS Fonts 4, 8.2 and 7.2).
	// A face that is not variable ignores them.
	Variations []Variation
	// Palette selects the colours of the layered glyphs of a colour face,
	// CSS font-palette (CSS Fonts 4, 9.1); nil draws each face in its
	// first palette.
	Palette *Palette
	// Emoji is the presentation emoji characters take when no variation
	// selector follows them, CSS font-variant-emoji (CSS Fonts 4, 6.12).
	Emoji EmojiVariant
	// NoOpticalSizing leaves the opsz axis of a variable face at its
	// default, CSS font-optical-sizing: none (CSS Fonts 4, 8.1); by
	// default it follows the size (see [Fonts.Layout]).
	NoOpticalSizing bool
}

// settings returns the font settings of s, the zero value for none.
func (s *Style) settings() *FontSettings {
	if s.Settings == nil {
		return &noSettings
	}
	return s.Settings
}

// noSettings is the zero [FontSettings], never modified.
var noSettings FontSettings

// equalSettings reports whether a and b hold the same font settings.
func equalSettings(a, b *FontSettings) bool {
	if a == b {
		return true
	}
	if a == nil {
		a = &noSettings
	}
	if b == nil {
		b = &noSettings
	}
	return a.SizeAdjust == b.SizeAdjust && a.LanguageSystem == b.LanguageSystem && slices.Equal(a.Features, b.Features) &&
		slices.Equal(a.Variations, b.Variations) && a.NoOpticalSizing == b.NoOpticalSizing && a.Emoji == b.Emoji && a.Palette == b.Palette
}

// Feature is one OpenType feature setting: its tag and value, 0 for off
// and 1 for on, or the index of an alternate.
type Feature struct {
	Tag   string
	Value uint32
}

// kernTag is the OpenType kerning feature (OpenType feature registry,
// kern).
var kernTag = opentype.MustNewTag("kern")

// kernOff is the feature list that shapes without kerning.
var kernOff = []shaping.FontFeature{{Tag: kernTag, Value: 0}}

// features returns the font features the shaper is handed for s, nil for
// the font's defaults.
func (s Style) features() []shaping.FontFeature {
	feats := s.settings().Features
	if len(feats) == 0 {
		if s.NoKerning {
			return kernOff
		}
		return nil
	}
	out := make([]shaping.FontFeature, 0, len(feats)+1)
	if s.NoKerning {
		out = append(out, kernOff[0])
	}
	for _, f := range feats {
		if tag, ok := featureTag(f.Tag); ok {
			out = append(out, shaping.FontFeature{Tag: tag, Value: f.Value})
		}
	}
	return out
}

// featureTag returns the OpenType tag of s, four bytes from U+20 to U+7E
// (OpenType, Data types: Tag).
func featureTag(s string) (opentype.Tag, bool) {
	if len(s) != 4 {
		return 0, false
	}
	for i := range 4 {
		if s[i] < 0x20 || s[i] > 0x7e {
			return 0, false
		}
	}
	return opentype.NewTag(s[0], s[1], s[2], s[3]), true
}

// languageSystem returns the language the shaper is handed for the
// language system tag of s, false when s has none: HarfBuzz's private use
// subtag hbot with the tag's bytes in hexadecimal, which selects that
// language system whatever its BCP 47 equivalent (hb_ot_tag_to_language).
// A tag shorter than four characters is padded with spaces, as
// hb_tag_from_string does, and a longer one is cut.
func (s Style) languageSystem() (language.Language, bool) {
	t := s.settings().LanguageSystem
	if t == "" {
		return "", false
	}
	if len(t) > 4 {
		t = t[:4]
	}
	t += strings.Repeat(" ", 4-len(t))
	tag, ok := featureTag(t)
	if !ok {
		return "", false
	}
	return language.Language(fmt.Sprintf("x-hbot-%08x", uint32(tag))), true
}

// scale returns the horizontal scale of s, 1 for none.
func (s Style) scale() float64 {
	if s.Scale == 0 {
		return 1
	}
	return s.Scale
}

// validate reports whether s can be laid out.
func (s Style) validate() error {
	if !numeric.Positive(s.Size) || s.Size > maxFontSize {
		return fmt.Errorf("%w: size %v outside (0, %d]", ErrInvalidStyle, s.Size, maxFontSize)
	}
	if !finiteWithin(s.LetterSpacing, maxFontSize) {
		return fmt.Errorf("%w: letter spacing %v outside [-%d, %d]", ErrInvalidStyle, s.LetterSpacing, maxFontSize, maxFontSize)
	}
	if !finiteWithin(s.Oblique, 90) || !finiteWithin(s.Stretch, 1000) || s.Stretch < 0 {
		return fmt.Errorf("%w: oblique angle %v or stretch %v out of range", ErrInvalidStyle, s.Oblique, s.Stretch)
	}
	if !finiteWithin(s.Autospace, maxFontSize) {
		return fmt.Errorf("%w: autospace %v outside [-%d, %d]", ErrInvalidStyle, s.Autospace, maxFontSize, maxFontSize)
	}
	if !finiteWithin(s.WordSpacing, maxFontSize) {
		return fmt.Errorf("%w: word spacing %v outside [-%d, %d]", ErrInvalidStyle, s.WordSpacing, maxFontSize, maxFontSize)
	}
	if s.Scale != 0 && (!numeric.Finite(s.Scale) || s.Scale < typeset.MinHScale || s.Scale > typeset.MaxHScale) {
		return fmt.Errorf("%w: scale %v outside [%v, %v]", ErrInvalidStyle, s.Scale, typeset.MinHScale, typeset.MaxHScale)
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
