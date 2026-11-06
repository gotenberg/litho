package text

import (
	"fmt"
	"math"
	"strings"

	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/fontscan"

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
	// regard to case or spaces. An empty list means sans-serif.
	//
	// A generic family expands, in place, to these families, then to
	// go-text's own substitutions for it:
	//
	//   - serif: DejaVu Serif, Liberation Serif, Noto Serif, Times New Roman, Times
	//   - sans-serif: DejaVu Sans, Liberation Sans, Noto Sans, Arial, Helvetica
	//   - monospace: DejaVu Sans Mono, Liberation Mono, Noto Sans Mono, Courier New, Courier
	//   - cursive: Comic Neue, Comic Sans MS, URW Chancery L
	//   - fantasy: Impact, Papyrus
	//   - system-ui: the sans-serif list
	Families []string
	// Weight is the CSS weight, from 1 to 1000; 400 is regular and 700
	// bold. Zero means 400; values out of range are clamped.
	Weight int
	// Italic asks for an italic face, then an oblique one.
	Italic bool
	// Size is the font size in points, greater than 0 and at most 1000.
	Size float64
	// Lang is the BCP 47 language tag of the text, "" when unknown. It
	// selects language-specific shaping.
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

// queryFor returns the go-text query for style.
func queryFor(style Style) fontscan.Query {
	families := expandFamilies(style.Families)
	fontStyle := font.StyleNormal
	if style.Italic {
		fontStyle = font.StyleItalic
	}
	return fontscan.Query{
		Families: families,
		Aspect: font.Aspect{
			Style:   fontStyle,
			Weight:  font.Weight(style.weight()),
			Stretch: font.StretchNormal,
		},
	}
}

// Generic family preference lists, as [Style.Families] documents them. The
// generic keyword ends each list so go-text's own substitutions follow.
var (
	serifFamilies     = []string{"DejaVu Serif", "Liberation Serif", "Noto Serif", "Times New Roman", "Times", fontscan.Serif}
	sansSerifFamilies = []string{"DejaVu Sans", "Liberation Sans", "Noto Sans", "Arial", "Helvetica", fontscan.SansSerif}
	monospaceFamilies = []string{"DejaVu Sans Mono", "Liberation Mono", "Noto Sans Mono", "Courier New", "Courier", fontscan.Monospace}
	cursiveFamilies   = []string{"Comic Neue", "Comic Sans MS", "URW Chancery L", fontscan.Cursive}
	fantasyFamilies   = []string{"Impact", "Papyrus", fontscan.Fantasy}
)

// genericFamily returns the preference list of a generic family name, nil
// for a concrete family.
func genericFamily(name string) []string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "serif":
		return serifFamilies
	case "sans-serif", "system-ui":
		return sansSerifFamilies
	case "monospace":
		return monospaceFamilies
	case "cursive":
		return cursiveFamilies
	case "fantasy":
		return fantasyFamilies
	}
	return nil
}

// expandFamilies returns families with every generic family replaced by its
// preference list. The result never aliases families.
func expandFamilies(families []string) []string {
	if len(families) == 0 {
		return append([]string(nil), sansSerifFamilies...)
	}
	out := make([]string, 0, len(families))
	for _, name := range families {
		if list := genericFamily(name); list != nil {
			out = append(out, list...)
			continue
		}
		out = append(out, name)
	}
	return out
}
