package slides

import (
	"image"
	"image/color"
	"math"
	"sort"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
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
// blipFill, pattFill).
func (b *builder) fill(f *Fill, box compose.Rect, ops []compose.PathOp) []compose.Fragment {
	if f == nil || len(ops) == 0 || !finite(box) {
		return nil
	}
	switch f.Kind {
	case FillSolid:
		if f.Color.A <= 0 {
			return nil
		}
		return []compose.Fragment{&compose.Path{Ops: ops, Fill: &compose.Fill{Color: colorOf(f.Color)}}}
	case FillGradient:
		sh := gradientShading(f.Gradient, box)
		if sh == nil {
			return nil
		}
		return []compose.Fragment{&compose.Path{Ops: ops, Fill: &compose.Fill{Shading: sh}}}
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

// gradientShading returns the shading of the gradient g over box, nil for
// a gradient without two stops (ECMA-376 Part 1, 20.1.8.33 gradFill,
// 20.1.8.41 lin, 20.1.8.46 path). A linear gradient runs across the whole
// box along its angle, its first stop on the corner the angle points away
// from; a radial one runs from the box's centre to its corners.
func gradientShading(g *Gradient, box compose.Rect) *compose.Shading {
	if g == nil || len(g.Stops) < 2 {
		return nil
	}
	stops := make([]compose.ColorStop, 0, len(g.Stops)+2)
	for _, s := range g.Stops {
		p := s.Position
		if math.IsNaN(p) {
			continue
		}
		stops = append(stops, compose.ColorStop{Offset: min(max(p, 0), 1), Color: colorOf(s.Color)})
	}
	if len(stops) < 2 {
		return nil
	}
	sort.SliceStable(stops, func(i, j int) bool { return stops[i].Offset < stops[j].Offset })
	if stops[0].Offset > 0 {
		stops = append([]compose.ColorStop{{Offset: 0, Color: stops[0].Color}}, stops...)
	}
	if last := stops[len(stops)-1]; last.Offset < 1 {
		stops = append(stops, compose.ColorStop{Offset: 1, Color: last.Color})
	}
	visible := false
	for _, s := range stops {
		visible = visible || s.Color.A > 0
	}
	if !visible {
		return nil
	}
	cx, cy := box.X+box.Width/2, box.Y+box.Height/2
	if g.Radial {
		r := math.Hypot(box.Width, box.Height) / 2
		if r <= 0 {
			return nil
		}
		return &compose.Shading{Kind: compose.Radial, From: compose.Point{X: cx, Y: cy}, To: compose.Point{X: cx, Y: cy}, ToRadius: r, Stops: stops, Transform: compose.Identity}
	}
	s, c := math.Sincos(g.Angle * math.Pi / 180)
	half := (math.Abs(box.Width*c) + math.Abs(box.Height*s)) / 2
	if half <= 0 {
		half = 0.5
	}
	return &compose.Shading{
		Kind:  compose.Axial,
		From:  compose.Point{X: cx - c*half, Y: cy - s*half},
		To:    compose.Point{X: cx + c*half, Y: cy + s*half},
		Stops: stops, Transform: compose.Identity,
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
