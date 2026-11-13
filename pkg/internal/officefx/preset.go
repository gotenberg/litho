package officefx

import (
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// presetShadow is the rule of one preset shadow (ECMA-376 Part 1,
// 20.1.8.49 prstShdw; 20.1.10.52 ST_PresetShadowVal names the presets and
// gives no geometry): one or two hard copies of the shape, without blur.
// Each copy is the shape scaled by sx and sy and skewed by kx degrees
// (skew then scale, as an outer shadow's kx and sx, sy) about the point
// of the shape's bounds, its line included, that align names; the near
// copy then moves dist along dir in the shadow's colour, the far copy,
// painted first, moves far times as far in that colour lightened
// presetLighten of the way to white. text says whether the copies hold
// the shape's text; word whether Word's rules draw the preset.
type presetShadow struct {
	sx, sy, kx float64
	align      css.RectAlign
	far        float64
	text, word bool
}

// presetLean is the skew, in degrees, of the perspective presets
// (shdw3, shdw4, shdw7, shdw8, shdw11, shdw12, shdw15, shdw16): before
// their half-height scale, it leans the copy's far edge 60 degrees from
// the vertical; its tangent is the square root of 3 over 2.
const presetLean = 40.893394649130904

// presetLighten is how far toward white the far copy of a preset shadow
// lightens the shadow's colour: four fifths, 808080 to E6E6E6.
const presetLighten = 0.8

// presetShadows holds the twenty presets, shdw1 first.
//
// # Corpus evidence
//
//   - probe, pptx: one slide of rectangles 78.7 by 55.1 points with a 1
//     point line, each preset once 6 points toward 45 degrees and once 12
//     points toward 0 degrees in 808080. PowerPoint prints every copy as
//     vector paths, the fill then the line, in gray 0.502 (far copies
//     0.902), each clipped to what lies outside the shape. The rule of
//     each row puts the 46 fill copies within 0.02 point of PowerPoint's
//     corners and the line copies within 0.45 point (PowerPoint strokes
//     the moved outline; a skewed copy here skews the pen). The lean of
//     the perspective presets is the same for shapes 78.5 and 79.2 points
//     wide (48.0 points over a 27.7 point copy), so it follows the
//     height. The text is copied in shdw1, shdw2, shdw5 and shdw6 alone.
//     shdw11, shdw12, shdw15 and shdw16 print as shdw3, shdw4, shdw7 and
//     shdw8, shdw18 as shdw17, at both settings.
//   - litho/pptx: effects-shadow prints shdw1, shdw13 and shdw18 on
//     rounded rectangles 6 points toward 45 degrees as these rows give.
//   - litho/docx: shapes-effects-3d prints its rounded rectangle without
//     a line under shdw17 1.41 points toward 45 degrees in Word as two
//     vector copies, gray 0.902 one point left and up, then gray 0.502 one
//     point right and down: shdw17's row. No reference shows another
//     preset in Word.
var presetShadows = [20]presetShadow{
	{sx: 1, sy: 1, align: alignBottom, text: true},          // shdw1
	{sx: 1, sy: 1, align: alignBottom, text: true},          // shdw2
	{sx: 1, sy: 0.5, kx: presetLean, align: alignBottom},    // shdw3
	{sx: 1, sy: 0.5, kx: -presetLean, align: alignBottom},   // shdw4
	{sx: 1, sy: 1, align: alignBottom, text: true},          // shdw5
	{sx: 1, sy: 1, align: alignBottom, text: true},          // shdw6
	{sx: 1, sy: -0.5, kx: presetLean, align: alignBottom},   // shdw7
	{sx: 1, sy: -0.5, kx: -presetLean, align: alignBottom},  // shdw8
	{sx: 0.75, sy: 0.75, align: css.RectAlign{}},            // shdw9
	{sx: 1.25, sy: 1.25, align: css.RectAlign{X: 1, Y: 1}},  // shdw10
	{sx: 1, sy: 0.5, kx: presetLean, align: alignBottom},    // shdw11
	{sx: 1, sy: 0.5, kx: -presetLean, align: alignBottom},   // shdw12
	{sx: 1, sy: 1, align: alignBottom, far: 2},              // shdw13
	{sx: 1, sy: 1, align: alignBottom},                      // shdw14
	{sx: 1, sy: -0.5, kx: presetLean, align: alignBottom},   // shdw15
	{sx: 1, sy: -0.5, kx: -presetLean, align: alignBottom},  // shdw16
	{sx: 1, sy: 1, align: alignBottom, far: -1, word: true}, // shdw17
	{sx: 1, sy: 1, align: alignBottom, far: -1},             // shdw18
	{sx: 1, sy: 0.5, align: alignBottom},                    // shdw19
	{sx: 1, sy: -1, align: alignBottom},                     // shdw20
}

// alignBottom is the middle of a box's bottom edge.
var alignBottom = css.RectAlign{X: 0.5, Y: 1}

// PresetCopies returns the copies of the preset shadow p that rules
// draw, in paint order, as outer shadows without blur (their Color, Dist,
// Dir, SX, SY, KX and Align set) whose alignment refers to the bounds of
// what the shape paints, its line included; and whether the copies hold
// the shape's text. ok is false for a name that is not one of the twenty
// presets and for a preset no reference shows on rules; the caller warns.
// A preset of a transparent colour has no copy.
func PresetCopies(p *css.OfficePreset, rules css.OfficeRules) (copies []css.OfficeShadow, text, ok bool) {
	n, ok := presetIndex(p.Name)
	if !ok {
		return nil, false, false
	}
	row := presetShadows[n]
	if rules != css.OfficePowerPoint && !row.word {
		return nil, false, false
	}
	if !(p.Color.A > 0) {
		return nil, row.text, true
	}
	near := css.OfficeShadow{Color: p.Color, Dist: p.Dist, Dir: p.Dir, SX: row.sx, SY: row.sy, KX: row.kx, Align: row.align}
	if row.far == 0 {
		return []css.OfficeShadow{near}, row.text, true
	}
	far := near
	far.Color = lighten(p.Color)
	if row.far < 0 {
		// The far copy falls the other way: dist stays a length.
		far.Dir += 180
	} else {
		far.Dist = float64(p.Dist * row.far)
	}
	return []css.OfficeShadow{far, near}, row.text, true
}

// presetIndex returns the index into presetShadows of the preset name,
// shdw1 to shdw20.
func presetIndex(name string) (int, bool) {
	digits, ok := strings.CutPrefix(name, "shdw")
	if !ok || digits == "" || digits[0] == '0' {
		return 0, false
	}
	n, err := strconv.Atoi(digits)
	if err != nil || n < 1 || n > len(presetShadows) {
		return 0, false
	}
	return n - 1, true
}

// lighten returns c moved presetLighten of the way to white, its alpha
// kept.
func lighten(c css.Color) css.Color {
	l := func(v float64) float64 { return v + float64((1-v)*presetLighten) }
	return css.Color{R: l(c.R), G: l(c.G), B: l(c.B), A: c.A}
}

// PresetUnder returns the copies of the preset shadow p of src that rules
// draw ([PresetCopies]), in paint order, as vector groups to paint under
// it: src.Content in each copy's colour ([Recolor]), mapped by the copy's
// skew, scale and offset about the bounds of src.Paths, strokes grown by
// half their width, at the colour's opacity. ok is false where
// [PresetCopies] gives false. A source of no silhouette has no copy.
func PresetUnder(src *Source, p *css.OfficePreset, rules css.OfficeRules) (copies []typeset.Fragment, ok bool) {
	shadows, _, ok := PresetCopies(p, rules)
	if !ok || len(src.Paths) == 0 {
		return nil, ok
	}
	b := pathBounds(src.Paths)
	if !usable(b) {
		return nil, true
	}
	for i := range shadows {
		sh := &shadows[i]
		c := typeset.Color{R: sh.Color.R, G: sh.Color.G, B: sh.Color.B, A: 1}
		copies = append(copies, &typeset.Group{Transform: shadowMatrix(sh, b), Opacity: min(sh.Color.A, 1), Content: Recolor(src.Content, c)})
	}
	return copies, true
}

// maxRecolorDepth bounds the groups [Recolor] descends. Anti-DoS guard,
// not spec-mandated: the converters nest groups
// [limits.MaxSourceGroupDepth] deep, and a shape adds a few of its own.
const maxRecolorDepth = limits.MaxSourceGroupDepth + 8

// Recolor returns a copy of frags painted in the one colour c, a hard
// shadow's: fills, strokes and text in c, pictures as their rectangles
// filled with c. A copy of a text keeps no structure parent.
func Recolor(frags []typeset.Fragment, c typeset.Color) []typeset.Fragment {
	return recolor(frags, c, 0)
}

// recolor is [Recolor] at the group depth depth.
func recolor(frags []typeset.Fragment, c typeset.Color, depth int) []typeset.Fragment {
	var out []typeset.Fragment
	for _, f := range frags {
		switch f := f.(type) {
		case *typeset.Path:
			p := &typeset.Path{Ops: f.Ops}
			if f.Fill != nil {
				p.Fill = &typeset.Fill{Color: c, Rule: f.Fill.Rule}
			}
			if f.Stroke != nil {
				st := *f.Stroke
				st.Color = c
				p.Stroke = &st
			}
			out = append(out, p)
		case *typeset.Text:
			t := *f
			t.Color = c
			t.Parent = nil
			out = append(out, &t)
		case *typeset.Image:
			r := f.Rect
			pt := func(x, y float64) [3]typeset.Point { return [3]typeset.Point{{X: x, Y: y}} }
			out = append(out, &typeset.Path{Ops: []typeset.PathOp{
				{Kind: typeset.MoveTo, P: pt(r.X, r.Y)},
				{Kind: typeset.LineTo, P: pt(r.X+r.Width, r.Y)},
				{Kind: typeset.LineTo, P: pt(r.X+r.Width, r.Y+r.Height)},
				{Kind: typeset.LineTo, P: pt(r.X, r.Y+r.Height)},
				{Kind: typeset.ClosePath},
			}, Fill: &typeset.Fill{Color: c}})
		case *typeset.Group:
			if depth < maxRecolorDepth {
				g := *f
				g.Content = recolor(f.Content, c, depth+1)
				out = append(out, &g)
			}
		}
	}
	return out
}
