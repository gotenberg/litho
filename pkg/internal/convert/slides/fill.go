package slides

import (
	"image"
	"image/color"
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

// maxPatternPixels caps the pixels of the picture a pattern fill is drawn
// as. Anti-DoS guard, not spec-mandated: a full slide at a pattern pixel
// of 0.75 point is about a million pixels.
const maxPatternPixels = 1 << 22

// patternPixel is the side of one pixel of a pattern fill's 8 by 8 cell,
// in points: a pixel at 96 dots per inch, the screen resolution the
// DrawingML presets are drawn at.
const patternPixel = 0.75

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
// box: the preset's 8 by 8 cell repeated over the box in the foreground
// and background colours, clipped to the area (ECMA-376 Part 1, 20.1.8.47
// pattFill, 20.1.10.51 ST_PresetPatternVal). A preset the builder does not
// know paints the foreground at half opacity over the background.
func (b *builder) patternFill(f *Fill, box compose.Rect, ops []compose.PathOp) []compose.Fragment {
	cell, ok := patternCell(f.Pattern)
	w := int(math.Ceil(box.Width / patternPixel))
	h := int(math.Ceil(box.Height / patternPixel))
	if !ok || w <= 0 || h <= 0 || w*h > maxPatternPixels {
		b.warn(CodePattern, "the pattern "+quote(f.Pattern)+" is drawn as its foreground at half opacity")
		var out []compose.Fragment
		if f.Background.A > 0 {
			out = append(out, &compose.Path{Ops: ops, Fill: &compose.Fill{Color: colorOf(f.Background)}})
		}
		if f.Color.A > 0 {
			c := colorOf(f.Color)
			c.A /= 2
			out = append(out, &compose.Path{Ops: ops, Fill: &compose.Fill{Color: c}})
		}
		return out
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	fg, bg := nrgba(f.Color), nrgba(f.Background)
	for y := range h {
		row := cell[y%8]
		for x := range w {
			if row&(0x80>>(x%8)) != 0 {
				img.SetNRGBA(x, y, fg)
			} else {
				img.SetNRGBA(x, y, bg)
			}
		}
	}
	r := compose.Rect{X: box.X, Y: box.Y, Width: float64(w) * patternPixel, Height: float64(h) * patternPixel}
	return []compose.Fragment{&compose.Group{
		Transform: compose.Identity, Opacity: 1,
		Clip:    &compose.Path{Ops: ops, Fill: &compose.Fill{}},
		Content: []compose.Fragment{&compose.Image{Rect: r, Source: &compose.ImageSource{Image: img}}},
	}}
}

// nrgba returns c as an 8-bit colour.
func nrgba(c css.Color) color.NRGBA {
	q := func(v float64) uint8 { return uint8(math.Round(min(max(v, 0), 1) * 255)) }
	return color.NRGBA{R: q(c.R), G: q(c.G), B: q(c.B), A: q(c.A)}
}

// patternCell returns the 8 by 8 cell of a DrawingML preset pattern, a
// row per byte, the most significant bit leftmost, a set bit in the
// foreground colour (ECMA-376 Part 1, 20.1.10.51 ST_PresetPatternVal, as
// PowerPoint and Impress draw the presets).
func patternCell(name string) ([8]byte, bool) {
	switch name {
	case "pct5":
		return [8]byte{0x80, 0, 0, 0, 0x08, 0, 0, 0}, true
	case "pct10":
		return [8]byte{0x80, 0, 0x08, 0, 0x80, 0, 0x08, 0}, true
	case "pct20":
		return [8]byte{0x88, 0, 0x22, 0, 0x88, 0, 0x22, 0}, true
	case "pct25":
		return [8]byte{0x88, 0x22, 0x88, 0x22, 0x88, 0x22, 0x88, 0x22}, true
	case "pct30":
		return [8]byte{0xaa, 0x44, 0xaa, 0x11, 0xaa, 0x44, 0xaa, 0x11}, true
	case "pct40":
		return [8]byte{0xaa, 0x55, 0xaa, 0x55, 0xaa, 0x55, 0xaa, 0x51}, true
	case "pct50":
		return [8]byte{0xaa, 0x55, 0xaa, 0x55, 0xaa, 0x55, 0xaa, 0x55}, true
	case "pct60":
		return [8]byte{0xee, 0x55, 0xbb, 0x55, 0xee, 0x55, 0xbb, 0x55}, true
	case "pct70":
		return [8]byte{0xee, 0xbb, 0xee, 0xbb, 0xee, 0xbb, 0xee, 0xbb}, true
	case "pct75":
		return [8]byte{0xee, 0xff, 0xbb, 0xff, 0xee, 0xff, 0xbb, 0xff}, true
	case "pct80":
		return [8]byte{0xf7, 0xff, 0x7f, 0xff, 0xf7, 0xff, 0x7f, 0xff}, true
	case "pct90":
		return [8]byte{0xff, 0xff, 0xf7, 0xff, 0xff, 0xff, 0x7f, 0xff}, true
	case "horz", "ltHorz":
		return [8]byte{0xff, 0, 0, 0, 0xff, 0, 0, 0}, true
	case "dkHorz":
		return [8]byte{0xff, 0xff, 0, 0, 0xff, 0xff, 0, 0}, true
	case "narHorz":
		return [8]byte{0xff, 0, 0xff, 0, 0xff, 0, 0xff, 0}, true
	case "vert", "ltVert":
		return [8]byte{0x88, 0x88, 0x88, 0x88, 0x88, 0x88, 0x88, 0x88}, true
	case "dkVert":
		return [8]byte{0xcc, 0xcc, 0xcc, 0xcc, 0xcc, 0xcc, 0xcc, 0xcc}, true
	case "narVert":
		return [8]byte{0xaa, 0xaa, 0xaa, 0xaa, 0xaa, 0xaa, 0xaa, 0xaa}, true
	case "dnDiag", "ltDnDiag":
		return [8]byte{0x88, 0x44, 0x22, 0x11, 0x88, 0x44, 0x22, 0x11}, true
	case "dkDnDiag", "wdDnDiag":
		return [8]byte{0xcc, 0x66, 0x33, 0x99, 0xcc, 0x66, 0x33, 0x99}, true
	case "upDiag", "ltUpDiag":
		return [8]byte{0x11, 0x22, 0x44, 0x88, 0x11, 0x22, 0x44, 0x88}, true
	case "dkUpDiag", "wdUpDiag":
		return [8]byte{0x33, 0x66, 0xcc, 0x99, 0x33, 0x66, 0xcc, 0x99}, true
	case "cross", "smGrid":
		return [8]byte{0xff, 0x88, 0x88, 0x88, 0xff, 0x88, 0x88, 0x88}, true
	case "lgGrid":
		return [8]byte{0xff, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80}, true
	case "diagCross", "smCheck":
		return [8]byte{0x99, 0x66, 0x66, 0x99, 0x99, 0x66, 0x66, 0x99}, true
	case "lgCheck":
		return [8]byte{0xf0, 0xf0, 0xf0, 0xf0, 0x0f, 0x0f, 0x0f, 0x0f}, true
	case "openDmnd":
		return [8]byte{0x80, 0x41, 0x22, 0x14, 0x08, 0x14, 0x22, 0x41}, true
	case "dotGrid":
		return [8]byte{0xaa, 0, 0x80, 0, 0x80, 0, 0x80, 0}, true
	case "dashHorz":
		return [8]byte{0xf0, 0, 0, 0, 0x0f, 0, 0, 0}, true
	case "dashVert":
		return [8]byte{0x80, 0x80, 0x80, 0x80, 0x08, 0x08, 0x08, 0x08}, true
	case "horzBrick":
		return [8]byte{0xff, 0x80, 0x80, 0x80, 0xff, 0x08, 0x08, 0x08}, true
	}
	return [8]byte{}, false
}
