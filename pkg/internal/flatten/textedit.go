package flatten

import (
	"context"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/icc"
	pdfimage "github.com/gotenberg/litho/pkg/internal/graphics/image"
)

// TextEdit is what is drawn around one show operator so that, once its
// transparency is gone, it draws as it did: Prefix before it, Suffix after
// it, which puts back the state Prefix changed.
type TextEdit struct {
	Prefix, Suffix string
}

// textOutcome is how a show operator draws once transparency is gone.
type textOutcome struct {
	edit TextEdit
	// edited reports an edit to draw.
	edited bool
	// blended reports colours composited over the paper, exact where the
	// text lies on the paper, and approx transparency that no edit
	// reproduces, which the text then drops.
	blended, approx bool
}

// textOutcome returns how a show operator under st draws once transparency
// is gone. Text stays text, so its transparency is reproduced, where it can
// be, by what an opaque glyph can carry:
//
//   - a fill or stroke under a constant alpha of 0 paints nothing (ISO
//     32000-2, 11.3.7.2), which the rendering mode that leaves it out
//     reproduces exactly, the clipping modes clipping still (9.3.6, Table
//     104);
//   - a fill or stroke under a constant alpha between 0 and 1, with the
//     Normal, Multiply or Darken blend mode and no soft mask, is drawn in
//     its colour composited over white paper, which is exact where the text
//     lies on the paper: each of those modes gives the source colour over
//     white (11.3.5, Table 134), and 11.3.7.2 makes the result the alpha's
//     share of it and the rest the paper's;
//   - anything else, a soft mask, another blend mode, a pattern, is drawn
//     opaque in the text's own colour, an approximation.
func (s *scanner) textOutcome(ctx context.Context, st *graphics.State, resources ast.PdfDict) textOutcome {
	var out textOutcome
	mode := st.Text.Tmode
	if mode < 0 || mode > 7 {
		return out
	}
	p := textPaints(mode)
	visible := paints{fill: p.fill && st.FillAlpha > 0, stroke: p.stroke && st.StrokeAlpha > 0}
	var prefix, suffix []string
	if visible != p {
		next := 3
		switch {
		case visible.fill && visible.stroke:
			next = 2
		case visible.fill:
			next = 0
		case visible.stroke:
			next = 1
		}
		if mode >= 4 {
			next += 4
		}
		prefix = append(prefix, strconv.Itoa(next)+" Tr")
		suffix = append(suffix, strconv.Itoa(mode)+" Tr")
	}
	patterned := (visible.fill && s.colorTransparent(ctx, st.Fill, resources, 0, nil)) ||
		(visible.stroke && s.colorTransparent(ctx, st.Stroke, resources, 0, nil))
	if s.painting(ctx, st, visible, resources, 0, nil) {
		var colorPrefix, colorSuffix []string
		ok := paperBlendMode(st.BlendMode) && st.SoftMask == nil && !patterned
		for _, c := range []struct {
			on     bool
			color  graphics.Color
			alpha  float64
			stroke bool
		}{
			{visible.fill, st.Fill, st.FillAlpha, false},
			{visible.stroke, st.Stroke, st.StrokeAlpha, true},
		} {
			if !ok || !c.on || c.alpha >= 1 {
				continue
			}
			over, blends := paperBlend(c.color, s.paperFamily(ctx, c.color, st.RenderingIntent), c.alpha)
			if !blends {
				ok = false
				continue
			}
			op := colorOperator(c.color, c.stroke)
			colorPrefix = append(colorPrefix, components(over)+" "+op)
			colorSuffix = append([]string{components(c.color.Components) + " " + op}, colorSuffix...)
		}
		if ok {
			out.blended = true
			prefix = append(prefix, colorPrefix...)
			suffix = append(colorSuffix, suffix...)
		} else {
			out.approx = true
		}
	}
	if len(prefix) > 0 {
		out.edited = true
		out.edit = TextEdit{Prefix: strings.Join(prefix, " "), Suffix: strings.Join(suffix, " ")}
	}
	return out
}

// paperBlendMode reports whether a blend mode gives the source colour over
// white paper (ISO 32000-2, 11.3.5, Table 134): Normal, Multiply and Darken
// do, B(1, Cs) = Cs, in an additive space and, through the complements
// 11.3.3 blends a subtractive space in, in a subtractive one.
func paperBlendMode(mode string) bool {
	switch mode {
	case "", "Normal", "Multiply", "Darken":
		return true
	}
	return false
}

// paperFamily returns the family of the space colour c is drawn in under the
// rendering intent: c's own, but for an ICCBased space whose profile litho
// cannot use, which ISO 32000-1, 8.6.5.5, Table 66 has a reader replace by
// its /Alternate, and the renderer with it ([pdfimage.SubstituteICCAlternate]).
// That alternate's family decides where white paper lies, since the colour
// values are taken as they are. It returns "" for an alternate that does not
// describe.
func (s *scanner) paperFamily(ctx context.Context, c graphics.Color, intent string) string {
	if c.Space.Family != graphics.FamilyICCBased || c.Space.Object == nil {
		return c.Space.Family
	}
	alt, ok := pdfimage.SubstituteICCAlternate(ctx, s.doc, c.Space.Object, icc.ParseIntent(intent), pdfimage.RoleSpace)
	if !ok {
		return c.Space.Family
	}
	space, err := graphics.DescribeColorSpace(ctx, s.doc, alt)
	if err != nil {
		return ""
	}
	return space.Family
}

// paperBlend returns colour c, drawn in a space of the family
// ([scanner.paperFamily]), composited at alpha over white paper, in c's own
// space, and false for a space with no such formula. In an additive space
// (grey, RGB, their calibrated and ICC forms) white is every component at 1;
// in a subtractive one (CMYK, a spot colour's tints) it is every component at
// 0, no ink (ISO 32000-2, 8.6.6.4).
func paperBlend(c graphics.Color, family string, alpha float64) ([]float64, bool) {
	if c.Pattern != "" || len(c.Components) == 0 {
		return nil, false
	}
	var paper float64
	switch family {
	case graphics.FamilyDeviceGray, graphics.FamilyDeviceRGB, graphics.FamilyCalGray, graphics.FamilyCalRGB:
		paper = 1
	case graphics.FamilyDeviceCMYK, graphics.FamilySeparation, graphics.FamilyDeviceN:
		paper = 0
	case graphics.FamilyICCBased:
		switch len(c.Components) {
		case 1, 3:
			paper = 1
		case 4:
			paper = 0
		default:
			return nil, false
		}
	default:
		return nil, false
	}
	out := make([]float64, len(c.Components))
	for i, v := range c.Components {
		out[i] = alpha*v + (1-alpha)*paper
	}
	return out, true
}

// colorOperator returns the operator that sets a colour in c's space, the
// current one: sc for the spaces it covers, scn for ICCBased and the spot
// colour spaces (ISO 32000-2, 8.6.8, Table 73), in upper case for stroking.
func colorOperator(c graphics.Color, stroke bool) string {
	op := "sc"
	switch c.Space.Family {
	case graphics.FamilyICCBased, graphics.FamilySeparation, graphics.FamilyDeviceN:
		op = "scn"
	}
	if stroke {
		return strings.ToUpper(op)
	}
	return op
}

// components writes colour components as content stream operands.
func components(v []float64) string {
	parts := make([]string, len(v))
	for i, c := range v {
		parts[i] = number(c)
	}
	return strings.Join(parts, " ")
}
