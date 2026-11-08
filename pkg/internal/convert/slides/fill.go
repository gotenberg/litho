package slides

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/shape"
)

// maxTiles caps the copies of a tiled picture drawn over one area.
// Anti-DoS guard, not spec-mandated: a slide tiled with a 16-point texture
// takes about 2,000 copies.
const maxTiles = 4096

// colorOf returns c as a composition colour.
func colorOf(c css.Color) compose.Color {
	return compose.Color{R: c.R, G: c.G, B: c.B, A: c.A}
}

// fill returns what paints the area ops encloses, box being the shape's
// box the fill is fitted to (ECMA-376 Part 1, 20.1.8: solidFill, gradFill,
// blipFill, pattFill): a solid colour, tinted by the path's fill mode
// (shape.Shade), and a gradient through the shape engine's fill; a
// picture or a pattern through the builder's own, which draw a vector
// picture and a pattern's pixel cell the engine does not.
func (b *builder) fill(f *Fill, box compose.Rect, ops []compose.PathOp, mode shape.FillMode) []compose.Fragment {
	if f == nil || len(ops) == 0 || !finite(box) {
		return nil
	}
	switch f.Kind {
	case FillSolid:
		if f.Color.A <= 0 {
			return nil
		}
		return shape.Fill(ops, box, shape.Paint{Kind: shape.PaintSolid, Color: shape.Shade(colorOf(f.Color), mode)})
	case FillGradient:
		if f.Gradient == nil || len(f.Gradient.Stops) < 2 {
			return nil
		}
		g := f.Gradient
		p := shape.Paint{Kind: shape.PaintGradient, Angle: g.Angle, Radial: g.Radial}
		// The border is a band of the first stop's colour the stops start
		// after (ODF 1.3 Part 3, 20.150 draw:border).
		border := 0.0
		if g.Border > 0 && g.Border < 1 {
			border = g.Border
			p.Stops = append(p.Stops, compose.ColorStop{Offset: 0, Color: colorOf(g.Stops[0].Color)})
		}
		for _, st := range g.Stops {
			p.Stops = append(p.Stops, compose.ColorStop{Offset: border + st.Position*(1-border), Color: colorOf(st.Color)})
		}
		out := shape.Fill(ops, box, p)
		if g.Radial {
			centreRadial(out, box, g.CenterX, g.CenterY)
		}
		return out
	case FillPicture:
		content := b.pictureFill(f.Picture, f.Tile, box)
		if len(content) == 0 {
			return nil
		}
		return []compose.Fragment{&compose.Group{Transform: compose.Identity, Opacity: 1, Clip: &compose.Path{Ops: ops, Fill: &compose.Fill{}}, Content: content}}
	case FillPattern:
		return b.patternFill(f, box, ops)
	case FillNone:
	}
	return nil
}

// centreRadial moves the radial shadings of frags, which the engine
// centres on box, to the centre (cx, cy), fractions of the box, keeping
// their size, as Impress draws a radial gradient whose centre is off the
// middle (ODF 1.3 Part 3, 20.150 draw:cx and draw:cy; ECMA-376 Part 1,
// 20.1.8.31 fillToRect). A zero centre is the middle.
func centreRadial(frags []compose.Fragment, box compose.Rect, cx, cy float64) {
	if cx == 0 && cy == 0 || math.IsNaN(cx) || math.IsNaN(cy) {
		return
	}
	cx, cy = min(max(cx, -1), 2), min(max(cy, -1), 2)
	for _, f := range frags {
		if p, ok := f.(*compose.Path); ok && p.Fill != nil && p.Fill.Shading != nil && p.Fill.Shading.Kind == compose.Radial {
			p.Fill.Shading.Transform.E = box.X + cx*box.Width
			p.Fill.Shading.Transform.F = box.Y + cy*box.Height
		}
	}
}

// pictureFill returns the fragments of the picture pic over box: stretched
// to it, or repeated at its natural size from its top-left corner when
// tile is set.
func (b *builder) pictureFill(pic *layout.Picture, tile bool, box compose.Rect) []compose.Fragment {
	if pic == nil || (pic.Source == nil && pic.Vector == nil) {
		b.warn(CodePicture, "a picture fill has no picture and paints nothing")
		return nil
	}
	if !tile || !(pic.Width > 0) || !(pic.Height > 0) {
		return b.picture(pic, box)
	}
	nx := math.Ceil(box.Width / pic.Width)
	ny := math.Ceil(box.Height / pic.Height)
	if nx*ny > maxTiles {
		b.warn(CodePicture, "a tiled picture fill needs too many copies and is stretched instead")
		return b.picture(pic, box)
	}
	var out []compose.Fragment
	for j := 0.0; j < ny; j++ {
		for i := 0.0; i < nx; i++ {
			r := compose.Rect{X: box.X + i*pic.Width, Y: box.Y + j*pic.Height, Width: pic.Width, Height: pic.Height}
			out = append(out, b.picture(pic, r)...)
		}
	}
	return out
}

// picture returns the fragments that paint pic into r.
func (b *builder) picture(pic *layout.Picture, r compose.Rect) []compose.Fragment {
	if pic == nil || r.Width <= 0 || r.Height <= 0 {
		return nil
	}
	if pic.Vector != nil {
		return pic.Vector(b.ctx, r)
	}
	if pic.Source == nil {
		return nil
	}
	return []compose.Fragment{&compose.Image{Rect: r, Source: pic.Source}}
}

// patternFill returns the pattern fill f of the area ops encloses within
// box as Impress draws a DrawingML preset pattern: the hatch its import
// maps the preset to, sets of parallel lines 0.1 point wide in the
// foreground colour, over the background colour (ECMA-376 Part 1,
// 20.1.8.47 pattFill, 20.1.10.51 ST_PresetPatternVal; LibreOffice's
// oox/inc/drawingml/hatchmap.hxx). A preset the map lacks paints the
// foreground at half opacity over the background.
//
// # Corpus evidence
//
//   - pptx: sd-data/n820786.pptx's narVert rectangle prints vertical lines
//     1.39 points apart at the page's 0.1-point width over its background,
//     and none of its 48 pattern swatches prints a picture.
func (b *builder) patternFill(f *Fill, box compose.Rect, ops []compose.PathOp) []compose.Fragment {
	var out []compose.Fragment
	if f.Background.A > 0 {
		out = append(out, &compose.Path{Ops: ops, Fill: &compose.Fill{Color: colorOf(f.Background)}})
	}
	if f.Color.A <= 0 {
		return out
	}
	h, ok := presetHatches[f.Pattern]
	// The hatch keeps its angle on the page: the shape's clockwise turn
	// turns it back (sd/tdf107608.pptx, a shape turned 270 degrees, prints
	// its ltDnDiag lines falling to the right).
	h.angle += 10 * b.hatchTurn
	lines := hatchOps(h, box)
	if !ok || lines == nil {
		b.warn(CodePattern, "the pattern "+quote(f.Pattern)+" is drawn as its foreground at half opacity")
		c := colorOf(f.Color)
		c.A /= 2
		return append(out, &compose.Path{Ops: ops, Fill: &compose.Fill{Color: c}})
	}
	return append(out, &compose.Group{
		Transform: compose.Identity, Opacity: 1,
		Clip:    &compose.Path{Ops: ops, Fill: &compose.Fill{}},
		Content: []compose.Fragment{&compose.Path{Ops: lines, Stroke: &compose.Stroke{Color: colorOf(f.Color), Width: hatchLineWidth}}},
	})
}

// hatchLineWidth is the width of a hatch's lines, in points: the line
// width Impress sets at the top of every page and leaves in place.
const hatchLineWidth = 0.1

// maxHatchLines caps the lines of one hatch drawn. Anti-DoS guard, not
// spec-mandated: the densest preset over a full slide takes about 3,000.
const maxHatchLines = 1 << 14

// hatchStyle is how many sets of lines a hatch draws: one; two crossing at
// a right angle; three, the third at 45 degrees to the first
// (drawing::HatchStyle).
type hatchStyle uint8

// The hatch styles.
const (
	hatchSingle hatchStyle = iota
	hatchDouble
	hatchTriple
)

// hatch is a hatch fill: its style, the distance between its lines in
// hundredths of a millimetre, and the angle of its first set of lines in
// tenths of a degree, counter-clockwise from the horizontal
// (drawing::Hatch).
type hatch struct {
	style    hatchStyle
	distance float64
	angle    float64
}

// hatchOps returns the lines of the hatch h over box, through its top-left
// corner, nil for a hatch without spacing or with too many lines.
func hatchOps(h hatch, box compose.Rect) []compose.PathOp {
	d := h.distance * 72 / 2540
	if !(d > 0) || !finite(box) {
		return nil
	}
	angles := []float64{h.angle / 10}
	switch h.style {
	case hatchDouble:
		angles = append(angles, h.angle/10+90)
	case hatchTriple:
		angles = append(angles, h.angle/10+90, h.angle/10+45)
	case hatchSingle:
	}
	cx, cy := box.X+box.Width/2, box.Y+box.Height/2
	r := math.Hypot(box.Width, box.Height) / 2
	var ops []compose.PathOp
	for _, a := range angles {
		// With y down, a counter-clockwise angle runs up the page.
		s, c := math.Sincos(a * math.Pi / 180)
		dx, dy := c, -s
		nx, ny := s, c
		// The lines pass through the top-left corner: the offsets along
		// the normal of the lines that cross the box.
		o := (box.X-cx)*nx + (box.Y-cy)*ny
		lo := math.Ceil((-r - o) / d)
		hi := math.Floor((r - o) / d)
		if hi-lo+1 > maxHatchLines || len(ops)/2+int(hi-lo+1) > maxHatchLines {
			return nil
		}
		for k := lo; k <= hi; k++ {
			px, py := cx+(o+k*d)*nx, cy+(o+k*d)*ny
			ops = append(ops,
				compose.PathOp{Kind: compose.MoveTo, P: [3]compose.Point{{X: px - r*dx, Y: py - r*dy}}},
				compose.PathOp{Kind: compose.LineTo, P: [3]compose.Point{{X: px + r*dx, Y: py + r*dy}}})
		}
	}
	return ops
}

// presetHatches maps the DrawingML preset patterns to the hatches
// Impress draws them as (LibreOffice's oox/inc/drawingml/hatchmap.hxx,
// createHatch, at the commit the corpora pin).
var presetHatches = map[string]hatch{
	"pct5":       {hatchSingle, 250, 450},
	"pct10":      {hatchSingle, 200, 450},
	"pct20":      {hatchSingle, 150, 450},
	"dashUpDiag": {hatchSingle, 150, 450},
	"pct25":      {hatchDouble, 200, 450},
	"pct30":      {hatchDouble, 175, 450},
	"pct40":      {hatchDouble, 150, 450},
	"weave":      {hatchDouble, 150, 450},
	"pct50":      {hatchDouble, 125, 450},
	"pct60":      {hatchTriple, 150, 450},
	"pct70":      {hatchTriple, 125, 450},
	"pct75":      {hatchTriple, 100, 450},
	"pct80":      {hatchTriple, 75, 450},
	"pct90":      {hatchTriple, 50, 450},
	"horz":       {hatchSingle, 100, 0},
	"wave":       {hatchSingle, 100, 0},
	"vert":       {hatchSingle, 100, 900},
	"ltHorz":     {hatchSingle, 50, 0},
	"narHorz":    {hatchSingle, 50, 0},
	"ltVert":     {hatchSingle, 50, 900},
	"narVert":    {hatchSingle, 50, 900},
	"dkHorz":     {hatchSingle, 25, 0},
	"dkVert":     {hatchSingle, 25, 900},
	"dashHorz":   {hatchSingle, 150, 0},
	"dashVert":   {hatchSingle, 150, 900},
	"cross":      {hatchDouble, 100, 0},
	"lgGrid":     {hatchDouble, 100, 0},
	"dnDiag":     {hatchSingle, 100, 1350},
	"wdDnDiag":   {hatchSingle, 100, 1350},
	"upDiag":     {hatchSingle, 100, 450},
	"wdUpDiag":   {hatchSingle, 100, 450},
	"ltDnDiag":   {hatchSingle, 50, 1350},
	"dkDnDiag":   {hatchSingle, 50, 1350},
	"ltUpDiag":   {hatchSingle, 50, 450},
	"dkUpDiag":   {hatchSingle, 50, 450},
	"dashDnDiag": {hatchSingle, 150, 1350},
	"diagCross":  {hatchDouble, 100, 450},
	"smCheck":    {hatchDouble, 50, 450},
	"lgCheck":    {hatchDouble, 100, 450},
	"smGrid":     {hatchDouble, 50, 0},
	"dotGrid":    {hatchDouble, 400, 0},
	"smConfetti": {hatchSingle, 200, 600},
	"lgConfetti": {hatchSingle, 100, 600},
	"horzBrick":  {hatchDouble, 300, 0},
	"diagBrick":  {hatchDouble, 300, 450},
	"dotDmnd":    {hatchDouble, 300, 450},
	"solidDmnd":  {hatchDouble, 100, 450},
	"openDmnd":   {hatchDouble, 100, 450},
	"plaid":      {hatchTriple, 200, 900},
	"sphere":     {hatchTriple, 100, 0},
	"divot":      {hatchTriple, 400, 450},
	"shingle":    {hatchSingle, 200, 1350},
	"trellis":    {hatchDouble, 75, 450},
	"zigZag":     {hatchSingle, 75, 0},
}
