package ods

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/numfmt"
	"github.com/gotenberg/litho/pkg/internal/odf"
)

// defaultStyleName is the cell style every cell style derives from (ODF
// 1.3, Part 3, 16.5; Calc's "Default").
const defaultStyleName = "Default"

// defaultStyle returns the Default cell style: the table-cell default
// style (16.4) under the common style Default, Liberation Sans 10 where
// neither names a font, the General format.
func (r *reader) defaultStyle() sheet.Style {
	st := r.resolve(odf.ScopeContent, defaultStyleName)
	if len(st.Font.Families) == 0 {
		st.Font.Families = []string{defaultFamily}
	}
	if st.Font.Size <= 0 {
		st.Font.Size = defaultFontSize
	}
	if st.Format == "" {
		st.Format = "General"
	}
	return st
}

// cellStyle returns the resolved table-cell style name of the content,
// nil for none, Default, or a name no style has.
func (r *reader) cellStyle(name string) *sheet.Style {
	if name == "" || name == defaultStyleName {
		return nil
	}
	if st, ok := r.cellSty[name]; ok {
		return st
	}
	if r.styles.Style(odf.ScopeContent, odf.FamilyCell, name) == nil {
		r.cellSty[name] = nil
		return nil
	}
	st := r.resolve(odf.ScopeContent, name)
	if len(st.Font.Families) == 0 {
		st.Font.Families = r.doc.Default.Font.Families
	}
	if st.Font.Size <= 0 {
		st.Font.Size = r.doc.Default.Font.Size
	}
	p := &st
	r.cellSty[name] = p
	return p
}

// resolve folds the cell, paragraph and text properties of the style
// name's chain (ODF 1.3, Part 3, 16.2) into a sheet style.
func (r *reader) resolve(scope odf.Scope, name string) sheet.Style {
	var st sheet.Style
	cp := r.styles.Resolve(scope, odf.FamilyCell, name, odf.CellProps)
	pp := r.styles.Resolve(scope, odf.FamilyCell, name, odf.ParagraphProps)
	st.Font = r.foldFont(sheet.Font{Size: defaultFontSize}, r.styles.Layers(scope, odf.FamilyCell, name, odf.TextProps))
	if c, ok := color(cp.Get("fo:background-color")); ok {
		st.Fill.Color = c
	}
	st.Border, st.Padding = borders(cp)
	tl, bl := cp.Get("style:diagonal-tl-br"), cp.Get("style:diagonal-bl-tr")
	if b := border(tl, cp.Get("style:diagonal-tl-br-widths")); b.Line != sheet.LineNone {
		st.Diagonal, st.DiagonalDown = b, true
	}
	if b := border(bl, cp.Get("style:diagonal-bl-tr-widths")); b.Line != sheet.LineNone {
		if !st.DiagonalDown {
			st.Diagonal = b
		}
		st.DiagonalUp = true
	}
	st.Align = alignment(cp, pp)
	dsName := r.styles.Inherited(scope, odf.FamilyCell, name, func(s *odf.Style) (string, bool) {
		return s.El.Lookup(odf.NSStyle, "data-style-name")
	})
	if dsName != "" {
		st.Format = r.dataCode(scope, dsName)
	}
	return st
}

// dataCode returns the format code of the data style name, with a
// [$-LCID] locale tag when the style names a locale whose separators are
// not en-US's (ODF 1.3 Part 3, 19.351 number:language and 19.348
// number:country), as Calc writes its numbers in the data style's locale.
//
// # Corpus evidence
//
//   - LibreOffice 26.8.2.2: 1234567.5 in a data style of two decimals with
//     grouping prints 1.234.567,50 for number:language de and
//     number:country DE, 1,234,567.50 for en US and for no language,
//     whatever the default cell style's fo:language.
func (r *reader) dataCode(scope odf.Scope, name string) string {
	code := r.data.Code(scope, name)
	el := r.data.Element(scope, name)
	if el == nil || code == "" || strings.Contains(code, "[$") {
		return code
	}
	lang := el.A(odf.NSNumber, "language")
	if c := el.A(odf.NSNumber, "country"); lang != "" && c != "" {
		lang += "-" + c
	}
	id, ok := numfmt.LCIDOf(lang)
	if !ok {
		return code
	}
	// Every section takes the tag: a section's locale is its own.
	tag := fmt.Sprintf("[$-%X]", id)
	var b strings.Builder
	b.WriteString(tag)
	quoted := false
	for i := 0; i < len(code); i++ {
		c := code[i]
		switch {
		case c == '\\' && i+1 < len(code):
			b.WriteByte(c)
			i++
			c = code[i]
		case c == '"':
			quoted = !quoted
		case c == ';' && !quoted:
			b.WriteByte(c)
			b.WriteString(tag)
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// spanFont returns the font of a text:span of style name over cur, or
// over the cell font base when cur is nil (ODF 1.3, Part 3, 6.1.7: a
// span's text properties inherit from the enclosing text).
func (r *reader) spanFont(base sheet.Font, cur *sheet.Font, name string) *sheet.Font {
	layers := r.styles.Layers(odf.ScopeContent, odf.FamilyText, name, odf.TextProps)
	if len(layers) == 0 {
		return cur
	}
	f := base
	if cur != nil {
		f = *cur
	}
	f = r.foldFont(f, layers)
	return &f
}

// foldFont lays the text property layers over f (ODF 1.3, Part 3, 20.x):
// the Western, Asian and complex font names through the font-face
// declarations (16.23), in that order, the size in points or percent,
// the weight, the posture, the underline, the line-through, the colour
// and the text position.
func (r *reader) foldFont(f sheet.Font, layers []odf.Props) sheet.Font {
	var fam [3]string
	for i, n := range f.Families {
		if i < len(fam) {
			fam[i] = n
		}
	}
	for _, l := range layers {
		if v, ok := l.Lookup("fo:font-size"); ok {
			if pt, ok := odf.Length(v); ok && pt > 0 {
				f.Size = pt
			} else if pc, ok := odf.Percent(v); ok && pc > 0 {
				f.Size *= pc
			}
		}
		for i, suffix := range []string{"", "-asian", "-complex"} {
			if name, ok := l.Lookup("style:font-name" + suffix); ok {
				if ff, ok := r.styles.Fonts[name]; ok {
					fam[i] = ff.Family
				} else {
					fam[i] = name
				}
			} else if v, ok := l.Lookup(familyKey(suffix)); ok {
				fam[i] = odf.Unquote(v)
			}
		}
		if v, ok := l.Lookup("fo:font-weight"); ok {
			n, err := strconv.Atoi(v)
			f.Bold = v == "bold" || err == nil && n >= 600
		}
		if v, ok := l.Lookup("fo:font-style"); ok {
			f.Italic = v == "italic" || v == "oblique"
		}
		if v, ok := l.Lookup("style:text-underline-style"); ok {
			f.Underline = sheet.NoUnderline
			if v != "none" && v != "" {
				f.Underline = sheet.Single
			}
		}
		if v, ok := l.Lookup("style:text-underline-type"); ok {
			switch {
			case v == "none":
				f.Underline = sheet.NoUnderline
			case v == "double" && (f.Underline != sheet.NoUnderline || !l.Has("style:text-underline-style")):
				f.Underline = sheet.Double
			}
		}
		if v, ok := l.Lookup("style:text-line-through-style"); ok {
			f.Strike = v != "none" && v != ""
		}
		if v, ok := l.Lookup("style:text-line-through-type"); ok && v == "none" {
			f.Strike = false
		}
		if v, ok := l.Lookup("fo:color"); ok {
			if c, ok := color(v); ok {
				f.Color = c
			}
		}
		if l.Get("style:use-window-font-color") == "true" {
			f.Color = css.Transparent
		}
		if v, ok := l.Lookup("style:text-position"); ok {
			f.Script = script(v)
		}
	}
	f.Families = nil
	for _, n := range fam {
		if n != "" && !contains(f.Families, n) {
			f.Families = append(f.Families, n)
		}
	}
	return f
}

// contains reports s in list.
func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// familyKey returns the font family attribute of a script suffix (ODF
// 1.3, Part 3, 20.192 and 20.270).
func familyKey(suffix string) string {
	if suffix == "" {
		return "fo:font-family"
	}
	return "style:font-family" + suffix
}

// script reads style:text-position (ODF 1.3, Part 3, 20.374): "super", a
// positive percentage, raises; "sub", a negative one, lowers.
func script(v string) sheet.Script {
	f := strings.Fields(v)
	if len(f) == 0 {
		return sheet.Baseline
	}
	switch f[0] {
	case "super":
		return sheet.Superscript
	case "sub":
		return sheet.Subscript
	}
	if p, ok := odf.Percent(f[0]); ok {
		switch {
		case p > 0:
			return sheet.Superscript
		case p < 0:
			return sheet.Subscript
		}
	}
	return sheet.Baseline
}

// color parses an ODF colour to an opaque css.Color; "transparent" and
// anything else is not a colour.
func color(v string) (css.Color, bool) {
	c, ok := odf.ParseColor(v)
	if !ok {
		return css.Color{}, false
	}
	return css.Color{R: float64(c.R) / 255, G: float64(c.G) / 255, B: float64(c.B) / 255, A: 1}, true
}

// sideNames are the side suffixes of the border and padding properties,
// in css.Side order.
var sideNames = [4]string{"top", "right", "bottom", "left"}

// cellPadding is Calc's default cell margin, 20 twips on every side
// (SvxMarginItem's default, the Default cell style's ATTR_MARGIN), which a
// style without fo:padding keeps.
//
// # Corpus evidence
//
//   - litho: figures.ods prints its left-aligned text 1 point right of the
//     column's edge, its right-aligned numbers 1 point left of the next
//     column's, and its baselines 1 point above the descent from the
//     row's bottom.
const cellPadding = 1

// borders reads fo:border, fo:padding and their sides (ODF 1.3, Part 3,
// 20.183 and 20.230), with style:border-line-width for the double lines
// (20.252); a side without padding keeps [cellPadding].
func borders(cp odf.Props) ([4]sheet.Border, [4]float64) {
	var bd [4]sheet.Border
	var pad [4]float64
	all, allW := cp.Get("fo:border"), cp.Get("style:border-line-width")
	allP, hasAllP := odf.Length(cp.Get("fo:padding"))
	for i, side := range sideNames {
		v, ok := cp.Lookup("fo:border-" + side)
		if !ok {
			v = all
		}
		w, ok := cp.Lookup("style:border-line-width-" + side)
		if !ok {
			w = allW
		}
		bd[i] = border(v, w)
		pad[i] = cellPadding
		if p, ok := odf.Length(cp.Get("fo:padding-" + side)); ok && p >= 0 {
			pad[i] = p
		} else if hasAllP && allP >= 0 {
			pad[i] = allP
		}
	}
	return bd, pad
}

// lineStyles maps the border styles of fo:border (XSL 1.0, 7.8.20, and
// LibreOffice's extensions) to the model's lines.
var lineStyles = map[string]sheet.Line{
	"solid": sheet.LineSolid, "dashed": sheet.LineDashed, "dotted": sheet.LineDotted,
	"dash-dot": sheet.LineDashDot, "dash-dot-dot": sheet.LineDashDotDot, "double": sheet.LineDouble,
	"double-thin": sheet.LineDouble, "fine-dashed": sheet.LineDashed, "groove": sheet.LineSolid,
	"ridge": sheet.LineSolid, "inset": sheet.LineSolid, "outset": sheet.LineSolid,
}

// borderWidths are the width keywords of fo:border, as CSS draws them.
var borderWidths = map[string]float64{"thin": 0.75, "medium": 2.25, "thick": 3.75}

// border parses an fo:border value, "0.74pt solid #808080", with the
// style:border-line-width of a double line, "inner distance outer".
func border(v, widths string) sheet.Border {
	b := sheet.Border{Color: css.Color{A: 1}}
	widthSet := false
	for _, f := range strings.Fields(v) {
		if pt, ok := odf.Length(f); ok {
			b.Width, widthSet = max(pt, 0), true
		} else if w, ok := borderWidths[f]; ok {
			b.Width, widthSet = w, true
		} else if l, ok := lineStyles[f]; ok {
			b.Line = l
		} else if c, ok := color(f); ok {
			b.Color = c
		}
	}
	if b.Line == sheet.LineNone {
		return sheet.Border{}
	}
	if !widthSet {
		b.Width = borderWidths["thin"]
	}
	if b.Line == sheet.LineDouble {
		if f := strings.Fields(widths); len(f) == 3 {
			in, ok1 := odf.Length(f[0])
			gap, ok2 := odf.Length(f[1])
			out, ok3 := odf.Length(f[2])
			if ok1 && ok2 && ok3 && in >= 0 && gap >= 0 && out >= 0 && in+gap+out > 0 {
				b.Width, b.Gap = in+gap+out, gap
			}
		}
	}
	if b.Width <= 0 {
		return sheet.Border{}
	}
	return b
}

// hAligns maps fo:text-align (ODF 1.3, Part 3, 20.216) to the model's
// alignments, for a left-to-right cell.
var hAligns = map[string]sheet.HAlign{
	"start": sheet.HLeft, "left": sheet.HLeft, "center": sheet.HCenter, "end": sheet.HRight,
	"right": sheet.HRight, "justify": sheet.HJustify,
}

// vAligns maps style:vertical-align (20.386); "automatic" is Calc's
// standard, the bottom.
var vAligns = map[string]sheet.VAlign{
	"top": sheet.VTop, "middle": sheet.VMiddle, "bottom": sheet.VBottom, "automatic": sheet.VBottom,
	"justify": sheet.VJustify,
}

// alignment reads the alignment properties of a cell style: the
// horizontal alignment with its source (20.388: value-type is the general
// alignment), style:repeat-content (20.345) as fill, fo:text-align-last
// justify as distributed, the vertical alignment, fo:wrap-option
// (20.230), style:shrink-to-fit (20.360), fo:margin-left as the indent
// (20.207), style:rotation-angle (20.351), style:direction ttb (20.252)
// and style:writing-mode rl-tb (20.404).
func alignment(cp, pp odf.Props) sheet.Alignment {
	var a sheet.Alignment
	ta, hasTA := pp.Lookup("fo:text-align")
	if src := cp.Get("style:text-align-source"); hasTA && src != "value-type" {
		a.Horizontal = hAligns[ta]
		if ta == "justify" && pp.Get("fo:text-align-last") == "justify" {
			a.Horizontal = sheet.HDistributed
		}
	}
	if cp.Get("style:repeat-content") == "true" {
		a.Horizontal = sheet.HFill
	}
	a.Vertical = vAligns[cp.Get("style:vertical-align")]
	a.Wrap = cp.Get("fo:wrap-option") == "wrap"
	a.Shrink = cp.Get("style:shrink-to-fit") == "true"
	if m, ok := odf.Length(pp.Get("fo:margin-left")); ok && m > 0 {
		a.Indent = m
	}
	if deg, ok := angle(cp.Get("style:rotation-angle")); ok {
		a.Rotation = deg
	}
	a.Stacked = cp.Get("style:direction") == "ttb"
	wm := pp.Get("style:writing-mode")
	if wm == "" {
		wm = cp.Get("style:writing-mode")
	}
	a.RightToLeft = strings.HasPrefix(wm, "rl")
	if a.RightToLeft {
		switch ta {
		case "start":
			if a.Horizontal == sheet.HLeft {
				a.Horizontal = sheet.HRight
			}
		case "end":
			if a.Horizontal == sheet.HRight {
				a.Horizontal = sheet.HLeft
			}
		}
	}
	return a
}

// angle reads an ODF angle (ODF 1.3, Part 3, 18.3.1: degrees, or with
// deg, rad or grad) as degrees counter-clockwise in [-90, 90]: an angle
// past a quarter turn turns the text over, which reads as its opposite.
func angle(v string) (float64, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}
	unit := 1.0
	for _, u := range []struct {
		suffix string
		f      float64
	}{{"deg", 1}, {"grad", 0.9}, {"rad", 180 / math.Pi}} {
		if s, ok := strings.CutSuffix(v, u.suffix); ok {
			v, unit = s, u.f
			break
		}
	}
	d, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil || math.IsNaN(d) || math.IsInf(d, 0) {
		return 0, false
	}
	d = math.Mod(d*unit, 360)
	if d < 0 {
		d += 360
	}
	switch {
	case d >= 270:
		d -= 360
	case d > 90:
		d -= 180
	}
	return d, true
}
