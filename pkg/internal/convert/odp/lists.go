package odp

import (
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/convert/slides"
	"github.com/gotenberg/litho/pkg/internal/odf"
)

// The list Impress lays out for a text:list that names no list style
// (measured on the pinned LibreOffice): each level indented 0.6 cm more
// than the last, its label 0.6 cm wide, a bullet of OpenSymbol at 45% of
// the text's size.
const (
	defaultListStep  = 0.6 * 72 / 2.54
	defaultListLabel = 0.6 * 72 / 2.54
	// defaultBulletScale is the size of a bullet or number whose level
	// sets none, as a fraction of the text's: Impress draws it at 45%.
	defaultBulletScale = 0.45
)

// listLevel returns the level lv of ls, nil when ls leaves it out or is
// nil.
func listLevel(ls *odf.ListStyle, lv int) *odf.ListLevel {
	if ls == nil || lv < 0 {
		return nil
	}
	return ls.Levels[min(lv, odf.MaxListLevels-1)]
}

// listIndents returns a list paragraph's left margin and first-line
// offset from its level of ls (ODF 1.3, Part 3, 17.20
// style:list-level-properties): the text at text:space-before plus
// text:min-label-width, the label at text:space-before. Impress lays a
// level in the label-alignment mode (text:list-level-position-and-space-
// mode) out with no indent at all, the label then the text from the
// paragraph's edge (measured on the pinned LibreOffice). A list of no
// style takes Impress's default list.
func (r *reader) listIndents(ls *odf.ListStyle, lv int) (float64, float64) {
	l := listLevel(ls, lv)
	if l == nil {
		return float64(lv)*defaultListStep + defaultListLabel, -defaultListLabel
	}
	if l.Props.Get("text:list-level-position-and-space-mode") == "label-alignment" {
		return 0, 0
	}
	sb, _ := odf.Length(l.Props.Get("text:space-before"))
	mlw, _ := odf.Length(l.Props.Get("text:min-label-width"))
	sb, mlw = max(sb, 0), max(mlw, 0)
	return sb + mlw, -mlw
}

// bullet returns the label of a paragraph opening a list item at level
// lv of ls (ODF 1.3, Part 3, 16.33 text:list-level-style-bullet, 16.32
// text:list-level-style-number, 16.34 text:list-level-style-image): nil
// for a level that shows none. A number's StartAt restarts the count
// where the file states a start (the contract's 0 continues it);
// listStart marks the first item of a list that does not continue
// another.
func (r *reader) bullet(ls *odf.ListStyle, lv int, item *odf.Element, listStart bool) *slides.Bullet {
	l := listLevel(ls, lv)
	if l == nil {
		// Impress's default outline bullet, StarBats' U+F095, which
		// draws as OpenSymbol's bullet.
		return &slides.Bullet{Kind: slides.BulletChar, Char: "\uf095", Families: []string{"StarBats"}, Scale: defaultBulletScale}
	}
	b := &slides.Bullet{Scale: defaultBulletScale}
	if v := l.Text.Get("fo:font-size"); v != "" {
		if f, ok := odf.Percent(v); ok && f > 0 {
			b.Scale = f
		} else if p, ok := odf.Length(v); ok && p > 0 {
			b.Points = p
		}
	} else if f, ok := odf.Percent(l.El.A(odf.NSText, "bullet-relative-size")); ok && f > 0 {
		b.Scale = f
	}
	if c, ok := odf.ParseColor(l.Text.Get("fo:color")); ok && l.Text.Get("style:use-window-font-color") != "true" {
		b.Color = cssColor(c)
	}
	switch l.Kind {
	case "bullet":
		ch := l.El.A(odf.NSText, "bullet-char")
		if ch == "" {
			return nil
		}
		fam := r.fontFamily(l.Text.Get("style:font-name"), l.Text.Get("fo:font-family"))
		ch, fam = symbolChar(ch, fam)
		b.Kind, b.Char = slides.BulletChar, ch
		if fam != "" {
			b.Families = []string{fam}
		}
	case "number", "outline":
		f := l.El.A(odf.NSStyle, "num-format")
		if f == "" {
			return nil
		}
		b.Kind = slides.BulletNumber
		b.Format = numberScheme(f, l.El.A(odf.NSStyle, "num-prefix"), l.El.A(odf.NSStyle, "num-suffix"))
		// An item's text:start-value restarts the count (19.868); the
		// level's restarts it where a list starts, when it is not 1, the
		// count every run of numbered paragraphs starts at.
		if n, err := strconv.Atoi(l.El.A(odf.NSText, "start-value")); err == nil && n > 1 && listStart {
			b.StartAt = n
		}
		if n, err := strconv.Atoi(item.A(odf.NSText, "start-value")); err == nil && n >= 1 {
			b.StartAt = n
		}
	case "image":
		pic := r.picture(l.El)
		if pic == nil {
			return nil
		}
		b.Kind, b.Picture = slides.BulletPicture, pic
		if h, ok := odf.Length(l.Props.Get("fo:height")); ok && h > 0 {
			b.Points, b.Scale = h, 0
		}
	default:
		return nil
	}
	return b
}

// numberScheme maps an ODF number format (ODF 1.3, Part 3, 19.504
// style:num-format, with style:num-prefix and style:num-suffix) onto the
// DrawingML autonumber scheme of the same digits and punctuation
// (ECMA-376 Part 1, 20.1.10.62 ST_TextAutonumberScheme): "1" with "." is
// arabicPeriod, "a" with ")" alphaLcParenR, "(" and ")" a ParenBoth
// scheme, none a Plain one (romanUcPlain, which DrawingML lacks for any
// digits but Arabic, and the builder prints bare). A punctuation the
// schemes lack takes the period; a digit set they lack, the Arabic
// digits.
func numberScheme(format, prefix, suffix string) string {
	base := "arabic"
	switch format {
	case "a":
		base = "alphaLc"
	case "A":
		base = "alphaUc"
	case "i":
		base = "romanLc"
	case "I":
		base = "romanUc"
	}
	switch {
	case strings.HasSuffix(prefix, "(") && strings.HasPrefix(suffix, ")"):
		return base + "ParenBoth"
	case strings.HasPrefix(suffix, ")"):
		return base + "ParenR"
	case suffix == "" && prefix == "":
		// A number of no punctuation prints bare: "I", "a", "1".
		return base + "Plain"
	}
	return base + "Period"
}

// symbolChar returns the character and family a bullet of a list style
// in the font family draws as: a StarBats character mapped onto
// OpenSymbol, as LibreOffice converts the StarOffice symbol fonts of a
// document's list styles on import (unotools' font conversion tables),
// the text then holding the mapped character; any other character as it
// is, for the builder to recode.
//
// # Corpus evidence
//
//   - odp: pitch.odp's level of StarBats U+F095 prints "•" in its text,
//     where pitch.fodp's default outline bullet, Impress's own StarBats
//     U+F095, prints U+F095.
func symbolChar(ch, family string) (string, string) {
	f := strings.ToLower(strings.ReplaceAll(family, " ", ""))
	switch f {
	case "starbats":
		rs := []rune(ch)
		if len(rs) != 1 {
			return ch, family
		}
		c := rs[0]
		if c >= 0x20 && c <= 0xff {
			c += 0xf000
		}
		if c >= 0xf020 && c <= 0xf0ff {
			if m := starBats[c-0xf020]; m != 0 {
				return string(rune(m)), "OpenSymbol"
			}
		}
	}
	return ch, family
}

// starBats maps the StarBats characters U+F020 to U+F0FF onto OpenSymbol
// (unotools/source/misc/fontcvt.cxx, aStarBatsTab), 0 for none.
var starBats = [224]uint16{
	0x0020, 0x263a, 0x25cf, 0x274d, 0x25a0, 0x25a1, 0xE000, 0x2751, 0x2752, 0xE001, 0xE002, 0xE003, 0x2756, 0xE004, 0xE005, 0x27a2,
	0xE006, 0x2794, 0x2713, 0x2612, 0x2611, 0x27b2, 0x261b, 0x270d, 0x270e, 0xE007, 0x2714, 0xE008, 0xE009, 0xE00A, 0x274f, 0x2750,
	0xE00B, 0xE00C, 0xE00D, 0xE00E, 0x2722, 0x2723, 0x2724, 0x2725, 0x2733, 0x2734, 0x2735, 0x2736, 0x2737, 0x2738, 0x2739, 0x2717,
	0x2718, 0x2719, 0x271a, 0x271b, 0x271c, 0x272b, 0x272c, 0x272d, 0x272e, 0x272f, 0x2730, 0, 0xE00F, 0x278a, 0x278b, 0x278c,
	0x278d, 0x278e, 0x278f, 0x2790, 0x2791, 0x2792, 0x2793, 0xE010, 0x2780, 0x2781, 0x2782, 0x2783, 0x2784, 0x2785, 0x2786, 0x2787,
	0x2788, 0x2789, 0xE011, 0xE012, 0x260e, 0xE013, 0xE014, 0xE015, 0xE016, 0xE017, 0xE018, 0xE019, 0xE01A, 0x261e, 0xE01B, 0,
	0x20ac, 0, 0x201a, 0x0192, 0x201e, 0x2026, 0x2020, 0x2021, 0xE01c, 0x2030, 0x0160, 0x2039, 0x0152, 0, 0x017d, 0,
	0, 0x2018, 0x2019, 0x201c, 0x201d, 0x2022, 0x2013, 0x2014, 0xE01d, 0x2122, 0x0161, 0x203a, 0x0153, 0, 0x017e, 0x0178,
	0, 0x21e7, 0x21e8, 0x21e9, 0x21e6, 0xE01e, 0xE01f, 0x00a7, 0xE020, 0xE021, 0xE022, 0x00ab, 0xE023, 0x2639, 0xE024, 0xE025,
	0xE026, 0xE027, 0xE028, 0x21e5, 0x21e4, 0x2192, 0x2193, 0x2190, 0x2191, 0xE029, 0xE02a, 0x00bb, 0xE02b, 0xE02c, 0xE02d, 0xE02e,
	0xE02f, 0xE030, 0xE031, 0xE032, 0x25be, 0x25b4, 0x25bf, 0x25b5, 0xE033, 0xE034, 0xE035, 0x2702, 0x2708, 0x2721, 0x273f, 0x2744,
	0x25d7, 0x2759, 0xE036, 0xE037, 0x2762, 0x2663, 0x2665, 0x2660, 0x2194, 0x2195, 0x2798, 0x279a, 0x27b8, 0, 0x00b6, 0,
	0x00a2, 0x00a4, 0x00a5, 0xE038, 0x20a1, 0x20a2, 0x20a3, 0x20a4, 0x20a9, 0x20ab, 0x20a8, 0xE039, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xE03a,
}
