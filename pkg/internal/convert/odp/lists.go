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
		// A bullet of a symbol font keeps its character and family: the
		// builder recodes StarBats, StarSymbol and the other symbol fonts
		// as Impress does.
		fam := r.fontFamily(l.Text.Get("style:font-name"), l.Text.Get("fo:font-family"))
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
// scheme. A punctuation the schemes lack takes the period; a digit set
// they lack, the Arabic digits.
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
	case suffix == "" && prefix == "" && base == "arabic":
		return "arabicPlain"
	}
	return base + "Period"
}
