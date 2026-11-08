package odp

import (
	"context"
	"math"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/slides"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/odf"
	"github.com/gotenberg/litho/pkg/internal/shape"
)

// The colours Impress's item pool paints with when no style gives one:
// the default shape fill, 0x729fcf, and line, 0x3465a4 (svx's
// COL_DEFAULT_SHAPE_FILLING and COL_DEFAULT_SHAPE_STROKE).
var (
	poolFill   = odf.Color{R: 0x72, G: 0x9f, B: 0xcf}
	poolStroke = odf.Color{R: 0x34, G: 0x65, B: 0xa4}
)

// hairline is the width a stroke of svg:stroke-width 0 carries beside
// Line.Hairline. Impress writes such a stroke with a PDF line width of 0,
// the thinnest line the device draws, which Hairline asks for; the width
// sizes its arrowheads and dashes, and draws the line where a builder
// does not honour Hairline: half a point, a device pixel at 150 dpi.
const hairline = 0.5

// cssColor converts an ODF colour to an opaque css.Color.
func cssColor(c odf.Color) css.Color {
	return css.Color{R: float64(c.R) / 255, G: float64(c.G) / 255, B: float64(c.B) / 255, A: 1}
}

// opacity returns the opacity of a fill: draw:opacity, else 1 less
// draw:transparency (ODF 1.3, Part 3, 20.152 and 20.169), 1 when
// neither is set. A draw:opacity-name gradient is approximated by its
// mean, with a warning.
func (r *reader) opacity(p odf.Props) float64 {
	if v, ok := odf.Percent(p.Get("draw:opacity")); ok {
		return math.Max(0, math.Min(1, v))
	}
	if v, ok := odf.Percent(p.Get("draw:transparency")); ok {
		return math.Max(0, math.Min(1, 1-v))
	}
	if g := r.draw.Opacities[p.Get("draw:opacity-name")]; g != nil {
		if g.StartValue != g.EndValue {
			r.warn("opacity-gradient", "a transparency gradient is drawn as its mean opacity")
		}
		return (g.StartValue + g.EndValue) / 2
	}
	return 1
}

// fill reads the fill of graphic or drawing-page properties p (ODF 1.3,
// Part 3, 20.118 draw:fill and the properties it selects): def is the
// draw:fill a shape takes when no style sets one, "solid" for a shape as
// Impress's pool gives it, "none" for a page.
func (r *reader) fill(p odf.Props, def string) slides.Fill {
	kind, ok := p.Lookup("draw:fill")
	if !ok {
		kind = def
	}
	alpha := r.opacity(p)
	color := func(key string, d odf.Color) css.Color {
		c, ok := odf.ParseColor(p.Get(key))
		if !ok {
			c = d
		}
		cc := cssColor(c)
		cc.A = alpha
		return cc
	}
	switch kind {
	case "solid":
		return slides.Fill{Kind: slides.FillSolid, Color: color("draw:fill-color", poolFill)}
	case "gradient":
		g := r.draw.Gradients[p.Get("draw:fill-gradient-name")]
		if g == nil {
			return slides.Fill{Kind: slides.FillSolid, Color: color("draw:fill-color", poolFill)}
		}
		return slides.Fill{Kind: slides.FillGradient, Gradient: r.gradient(g, alpha)}
	case "bitmap":
		img := r.draw.FillImages[p.Get("draw:fill-image-name")]
		if img == nil {
			return slides.Fill{}
		}
		pic := r.picture(img)
		if pic == nil {
			return slides.Fill{}
		}
		if alpha < 1 {
			r.warn("bitmap-opacity", "the transparency of a picture fill is not applied")
		}
		rep := p.Get("style:repeat")
		if rep == "" {
			rep = "repeat"
		}
		size := fillImageSize{w: pic.Width, h: pic.Height}
		size.read(p.Get("draw:fill-image-width"), &size.w, &size.relW)
		size.read(p.Get("draw:fill-image-height"), &size.h, &size.relH)
		if rep == "no-repeat" {
			return slides.Fill{Kind: slides.FillPicture, Picture: placedOnce(pic, p.Get("draw:fill-image-ref-point"), size)}
		}
		if rep != "repeat" {
			return slides.Fill{Kind: slides.FillPicture, Picture: pic}
		}
		t := tiling{ref: p.Get("draw:fill-image-ref-point"), size: size}
		t.refX, _ = odf.Percent(p.Get("draw:fill-image-ref-point-x"))
		t.refY, _ = odf.Percent(p.Get("draw:fill-image-ref-point-y"))
		if f := strings.Fields(p.Get("draw:tile-repeat-offset")); len(f) == 2 {
			if v, ok := odf.Percent(f[0]); ok {
				t.offset, t.vertical = clamp(v, 0, 1), f[1] == "vertical"
			}
		}
		if t.plain() {
			if size.w != pic.Width || size.h != pic.Height {
				sized := *pic
				sized.Width, sized.Height = size.w, size.h
				pic = &sized
			}
			return slides.Fill{Kind: slides.FillPicture, Picture: pic, Tile: true}
		}
		return slides.Fill{Kind: slides.FillPicture, Picture: t.picture(pic)}
	case "hatch":
		h := r.draw.Hatches[p.Get("draw:fill-hatch-name")]
		if h == nil {
			return slides.Fill{}
		}
		f := slides.Fill{Kind: slides.FillPattern, Pattern: hatchPattern(h), Color: cssColor(h.Color)}
		f.Color.A = alpha
		if p.Get("draw:fill-hatch-solid") == "true" {
			f.Background = color("draw:fill-color", poolFill)
		}
		r.warn("hatch", "hatches are drawn as the nearest preset pattern")
		return f
	}
	return slides.Fill{}
}

// gradient maps a draw:gradient onto the model's stops as Impress draws
// it, measured on the pinned LibreOffice: a linear gradient runs from
// the start colour to the end colour along draw:angle, its draw:border
// fraction painted in the start colour first, which Border carries; an
// axial one has the start colour in the middle and the end colour at
// both edges, the border in the end colour at the edges; a radial one
// has the end colour at its centre, draw:cx and draw:cy as CenterX and
// CenterY, and the start colour at half the box's diagonal, the border
// in the start colour outside. The border of an axial or radial
// gradient lies at both edges or outside, where Border, a band before
// the first stop, cannot say it: the stops carry it. Ellipsoid, square
// and rectangular gradients are drawn radial, with a warning.
func (r *reader) gradient(g *odf.Gradient, alpha float64) *slides.Gradient {
	col := func(c odf.Color) css.Color {
		cc := cssColor(c)
		cc.A = alpha
		return cc
	}
	stops := g.Stops
	if stops == nil {
		stops = []odf.GradientStop{{Offset: 0, Color: g.Start}, {Offset: 1, Color: g.End}}
	}
	b := g.Border
	out := &slides.Gradient{CenterX: 0.5, CenterY: 0.5}
	switch g.Style {
	case "axial":
		// Half the axis from an edge (the end) to the middle (the start).
		if b > 0 {
			out.Stops = append(out.Stops, slides.Stop{Position: 0, Color: col(stops[len(stops)-1].Color)})
		}
		for i := len(stops) - 1; i >= 0; i-- {
			t := b + (1-b)*(1-stops[i].Offset)
			out.Stops = append(out.Stops, slides.Stop{Position: t / 2, Color: col(stops[i].Color)})
		}
		for i := 1; i < len(stops); i++ {
			t := b + (1-b)*(1-stops[i].Offset)
			out.Stops = append(out.Stops, slides.Stop{Position: 1 - t/2, Color: col(stops[i].Color)})
		}
		if b > 0 {
			out.Stops = append(out.Stops, slides.Stop{Position: 1, Color: col(stops[len(stops)-1].Color)})
		}
		out.Angle = axisAngle(g.Angle)
	case "radial", "ellipsoid", "square", "rectangular":
		if g.Style != "radial" {
			r.warn("gradient-"+g.Style, g.Style+" gradients are drawn radial")
		}
		out.Radial = true
		out.CenterX, out.CenterY = g.CX, g.CY
		for i := len(stops) - 1; i >= 0; i-- {
			out.Stops = append(out.Stops, slides.Stop{Position: (1 - b) * (1 - stops[i].Offset), Color: col(stops[i].Color)})
		}
		if b > 0 {
			out.Stops = append(out.Stops, slides.Stop{Position: 1, Color: col(stops[0].Color)})
		}
	default:
		for _, s := range stops {
			out.Stops = append(out.Stops, slides.Stop{Position: s.Offset, Color: col(s.Color)})
		}
		out.Border = min(b, 0.999)
		out.Angle = axisAngle(g.Angle)
	}
	return out
}

// axisAngle converts a draw:angle, counterclockwise from a gradient
// running top to bottom, into the model's angle, clockwise from one
// running left to right.
func axisAngle(a float64) float64 {
	v := math.Mod(90-a, 360)
	if v < 0 {
		v += 360
	}
	return v
}

// hatchPattern maps a draw:hatch (ODF 1.3, Part 3, 16.42.5) onto the
// DrawingML pattern preset of the same lines (ECMA-376 Part 1, 20.1.10.51)
// by angle and spacing: horizontal, vertical or diagonal lines, crossed
// for a double or a triple hatch, at the angle nearest its draw:rotation,
// and the preset whose line spacing is nearest draw:distance. The presets'
// 8 by 8 cells, a pixel being 0.75 pt, space their lines 4 pixels apart
// (3 pt), the narrow ones 2 (1.5 pt), the large grid 8 (6 pt); a
// diagonal preset's lines lie 4 pixels apart along the axes, 2.1 pt
// apart across.
func hatchPattern(h *odf.Hatch) string {
	a := math.Mod(math.Round(h.Rotation/45)*45, 180)
	if a < 0 {
		a += 180
	}
	d := h.Distance
	narrow := d > 0 && d < (1.5+3)/2
	cross := h.Style == "double" || h.Style == "triple"
	switch {
	case cross && (a == 45 || a == 135):
		return "diagCross"
	case cross && d >= (3+6)/2:
		return "lgGrid"
	case cross:
		return "cross"
	case a == 0 && narrow:
		return "narHorz"
	case a == 0:
		return "horz"
	case a == 90 && narrow:
		return "narVert"
	case a == 90:
		return "vert"
	case a == 45:
		return "upDiag"
	}
	return "dnDiag"
}

// line reads the stroke of graphic properties p (ODF 1.3, Part 3, 20.167
// draw:stroke, 20.410 svg:stroke-width, 20.408 svg:stroke-color, 20.168
// draw:stroke-dash, 20.171 svg:stroke-linecap, draw:stroke-linejoin,
// 20.146 and 20.143 the markers): a stroke no style sets is solid, as
// Impress's pool draws it; a dash is drawn as svx draws it at the
// stroke's width; joins are round unless a style says otherwise.
func (r *reader) line(p odf.Props) slides.Line {
	stroke, ok := p.Lookup("draw:stroke")
	if !ok {
		stroke = "solid"
	}
	if stroke == "none" {
		return slides.Line{}
	}
	w, _ := odf.Length(p.Get("svg:stroke-width"))
	w = max(w, 0)
	c, ok := odf.ParseColor(p.Get("svg:stroke-color"))
	if !ok {
		c = poolStroke
	}
	ln := slides.Line{Width: w, Color: cssColor(c)}
	if v, ok := odf.Percent(p.Get("svg:stroke-opacity")); ok {
		ln.Color.A = math.Max(0, math.Min(1, v))
	}
	if stroke == "dash" {
		if d := r.draw.Dashes[p.Get("draw:stroke-dash")]; d != nil {
			ln.Dash = d.DashArray(w)
			if d.Style == "round" {
				ln.Cap = slides.CapRound
			}
		}
	}
	if w == 0 {
		ln.Width, ln.Hairline = hairline, true
	}
	switch p.Get("svg:stroke-linecap") {
	case "round":
		ln.Cap = slides.CapRound
	case "square":
		ln.Cap = slides.CapSquare
	}
	switch p.Get("draw:stroke-linejoin") {
	case "miter":
		ln.Join = slides.JoinMiter
	case "bevel":
		ln.Join = slides.JoinBevel
	default:
		ln.Join = slides.JoinRound
	}
	ln.Head = r.arrow(p, "start", ln.Width)
	ln.Tail = r.arrow(p, "end", ln.Width)
	return ln
}

// arrow reads a line end, draw:marker-start or draw:marker-end with its
// width (ODF 1.3, Part 3, 20.146 to 20.148, 20.143 to 20.145), mapped
// onto the DrawingML arrowhead of the nearest shape by the marker's name,
// and sized relative to the medium arrowhead, three times the line's
// width (ECMA-376 Part 1, 20.1.10.32 and 20.1.10.34): a 0.3 cm marker on
// a 0.1 cm line is the medium size, 1.
func (r *reader) arrow(p odf.Props, end string, lineWidth float64) slides.Arrow {
	name := p.Get("draw:marker-" + end)
	m := r.draw.Markers[name]
	if name == "" || m == nil {
		return slides.Arrow{}
	}
	a := slides.Arrow{Kind: markerKind(name), Width: 1, Length: 1}
	if mw, ok := odf.Length(p.Get("draw:marker-" + end + "-width")); ok && mw > 0 && lineWidth > 0 {
		a.Width = mw / (3 * lineWidth)
		a.Length = a.Width
		if m.ViewBox[2] > 0 {
			a.Length = a.Width * m.ViewBox[3] / m.ViewBox[2]
		}
	}
	return a
}

// markerKind maps a draw:marker's name onto the DrawingML arrowhead kind
// of the same shape: LibreOffice's markers "Circle", "Square 45" and
// "Diamond", "Arrow concave", "Line Arrow" and the arrows and triangles
// otherwise; the names are XML-encoded, "_20_" for a space.
func markerKind(name string) slides.ArrowKind {
	n := strings.ToLower(strings.ReplaceAll(name, "_20_", " "))
	switch {
	case strings.Contains(n, "circle"), strings.Contains(n, "oval"), strings.Contains(n, "dot"):
		return slides.ArrowOval
	case strings.Contains(n, "diamond"), strings.Contains(n, "square"):
		return slides.ArrowDiamond
	case strings.Contains(n, "concave"), strings.Contains(n, "stealth"):
		return slides.ArrowStealth
	case strings.Contains(n, "line arrow"), strings.Contains(n, "open"):
		return slides.ArrowOpen
	}
	return slides.ArrowTriangle
}

// textBodyProps reads how a shape's text sits in it from its graphic
// properties p (ODF 1.3, Part 3, 20.217 fo:padding, 20.174
// draw:textarea-vertical-align, 20.230 fo:wrap-option, 20.98
// draw:auto-grow-height, 20.130 draw:fit-to-size, 20.360
// style:shrink-to-fit, 19.232 fo:column-count, 20.404 style:writing-mode).
// A text frame wraps at its width unless it grows with its text
// (draw:auto-grow-width), whatever fo:wrap-option says, as Impress lays a
// text frame out; a shape's text wraps unless fo:wrap-option is no-wrap.
// A text frame grows with its text unless draw:auto-grow-height is
// false, Impress's default for a frame.
func (r *reader) textBodyProps(tb *slides.TextBody, p odf.Props, frame bool) {
	if tb == nil {
		return
	}
	if v, ok := odf.Length(p.Get("fo:padding")); ok {
		tb.Insets = [4]float64{v, v, v, v}
	}
	for i, side := range [4]string{"top", "right", "bottom", "left"} {
		if v, ok := odf.Length(p.Get("fo:padding-" + side)); ok {
			tb.Insets[i] = v
		}
	}
	switch p.Get("draw:textarea-vertical-align") {
	case "middle":
		tb.Anchor = slides.AnchorMiddle
	case "bottom":
		tb.Anchor = slides.AnchorBottom
	}
	// The block of lines across the text area (20.385
	// draw:textarea-horizontal-align): justify, Impress's block, and left
	// span the area.
	switch p.Get("draw:textarea-horizontal-align") {
	case "center":
		tb.HorizontalAnchor = slides.HorizontalCenter
	case "right":
		tb.HorizontalAnchor = slides.HorizontalEnd
	}
	if frame {
		tb.Wrap = p.Get("draw:auto-grow-width") != "true"
	} else {
		tb.Wrap = p.Get("fo:wrap-option") != "no-wrap"
	}
	grow := p.Get("draw:auto-grow-height")
	if grow == "true" || frame && grow == "" {
		tb.Autofit = slides.AutofitGrow
	}
	switch fit := p.Get("draw:fit-to-size"); {
	case p.Get("style:shrink-to-fit") == "true", fit == "shrink-to-fit":
		tb.Autofit = slides.AutofitShrink
	case fit == "true", fit == "all":
		tb.Autofit = slides.AutofitStretch
	}
	// LibreOffice's extension overflow-behavior clip, which it writes in
	// the loext namespace and reads in the style one too, cuts the text
	// at the shape's text area.
	tb.Clip = p.Get("loext:overflow-behavior") == "clip" || p.Get("style:overflow-behavior") == "clip"
	if n, err := strconv.Atoi(p.Get("fo:column-count")); err == nil && n > 1 {
		tb.Columns = min(n, maxColumns)
		tb.ColumnGap, _ = odf.Length(p.Get("fo:column-gap"))
	}
	switch p.Get("style:writing-mode") {
	case "tb-rl", "tb":
		tb.Vertical = css.VerticalRL
	case "tb-lr":
		tb.Vertical = css.VerticalLR
	case "tb-rl90":
		// LibreOffice's extension values of style:writing-mode beyond
		// ODF 1.3's 20.404: the text turned a quarter turn clockwise, or
		// counterclockwise reading bottom to top.
		tb.Vertical = css.SidewaysRL
	case "bt-lr":
		tb.Vertical = css.SidewaysLR
	case "rl-tb", "rl":
		tb.RightToLeft = true
	}
}

// maxColumns bounds the text columns of a shape: 16, Impress's own limit
// (the column dialog's maximum).
const maxColumns = 16

// fillImageSize is the size a picture fill paints its picture at
// (ODF 1.3, Part 3, 20.128 draw:fill-image-width and its height twin): a
// length, or a fraction of the filled area when relW or relH is set.
type fillImageSize struct {
	w, h       float64
	relW, relH float64
}

// read reads one side: a positive length or percentage replaces the
// natural size; 0 keeps it.
func (fillImageSize) read(v string, abs, rel *float64) {
	if f, ok := odf.Percent(v); ok && f > 0 {
		*rel = f
	} else if l, ok := odf.Length(v); ok && l > 0 {
		*abs = l
	}
}

// placedOnce returns a picture that, stretched over an area as the model
// draws a fill picture, paints pic once at size at the reference point
// ref of the area (ODF 1.3, Part 3, 20.125 draw:fill-image-ref-point:
// top-left, top, top-right, left, center, right, bottom-left, bottom,
// bottom-right; center when absent), as Impress draws a fill of
// style:repeat no-repeat (19.515).
func placedOnce(pic *layout.Picture, ref string, size fillImageSize) *layout.Picture {
	src := pic
	return &layout.Picture{Width: pic.Width, Height: pic.Height, NoRatio: true, Vector: func(ctx context.Context, area compose.Rect) []compose.Fragment {
		w, h := size.w, size.h
		if size.relW > 0 {
			w = area.Width * size.relW
		}
		if size.relH > 0 {
			h = area.Height * size.relH
		}
		x := area.X + (area.Width-w)/2
		y := area.Y + (area.Height-h)/2
		if strings.Contains(ref, "left") {
			x = area.X
		} else if strings.Contains(ref, "right") {
			x = area.X + area.Width - w
		}
		if strings.HasPrefix(ref, "top") {
			y = area.Y
		} else if strings.HasPrefix(ref, "bottom") {
			y = area.Y + area.Height - h
		}
		at := compose.Rect{X: x, Y: y, Width: w, Height: h}
		var content []compose.Fragment
		if src.Vector != nil {
			content = src.Vector(ctx, at)
		} else {
			content = []compose.Fragment{&compose.Image{Rect: at, Source: src.Source}}
		}
		return []compose.Fragment{&compose.Group{Clip: &compose.Path{Ops: shape.RectOps(area)}, Content: content}}
	}}
}

// clamp returns v within [lo, hi].
func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }

// maxTiles caps the tiles one tiled picture fill draws itself: 10,000.
// Anti-DoS guard, not spec-mandated: a slide tiled with a 1 cm picture
// holds a few hundred; past the cap the fill tiles from its corner.
const maxTiles = 10_000

// tiling is how a picture fill of style:repeat repeat (ODF 1.3, Part 3,
// 20.341) lays its tiles: their size, the reference point of the area a
// tile is aligned with (20.125 draw:fill-image-ref-point), the movement
// after it as fractions of a tile (20.126 and 20.127
// draw:fill-image-ref-point-x and -y), and the shift of every other row,
// or column when vertical, as a fraction of a tile (20.133
// draw:tile-repeat-offset), as Impress draws them.
type tiling struct {
	ref        string
	refX, refY float64
	offset     float64
	vertical   bool
	size       fillImageSize
}

// plain reports a tiling the model's corner-anchored Tile draws: tiles of
// a fixed size from the top-left corner, unshifted.
func (t tiling) plain() bool {
	return (t.ref == "" || t.ref == "top-left") && t.refX == 0 && t.refY == 0 && t.offset == 0 && t.size.relW == 0 && t.size.relH == 0
}

// picture returns a picture that, stretched over an area as the model
// draws a fill picture, paints pic tiled over it as t says, clipped to
// the area.
func (t tiling) picture(pic *layout.Picture) *layout.Picture {
	src := pic
	return &layout.Picture{Width: pic.Width, Height: pic.Height, NoRatio: true, Vector: func(ctx context.Context, area compose.Rect) []compose.Fragment {
		w, h := t.size.w, t.size.h
		if t.size.relW > 0 {
			w = area.Width * t.size.relW
		}
		if t.size.relH > 0 {
			h = area.Height * t.size.relH
		}
		if w <= 0 || h <= 0 || (area.Width/w+2)*(area.Height/h+2) > maxTiles {
			return nil
		}
		// The tile aligned with the reference point, then moved.
		x0, y0 := area.X+(area.Width-w)/2, area.Y+(area.Height-h)/2
		if strings.Contains(t.ref, "left") || t.ref == "" {
			x0 = area.X
		} else if strings.Contains(t.ref, "right") {
			x0 = area.X + area.Width - w
		}
		if strings.HasPrefix(t.ref, "top") || t.ref == "" {
			y0 = area.Y
		} else if strings.HasPrefix(t.ref, "bottom") {
			y0 = area.Y + area.Height - h
		}
		x0 += t.refX * w
		y0 += t.refY * h
		// The first row and column at or before the area's corner, of the
		// same parity as the aligned tile's.
		col0 := 2*math.Floor((area.X-x0)/w/2) - 2
		row0 := 2*math.Floor((area.Y-y0)/h/2) - 2
		var content []compose.Fragment
		for row := row0; y0+row*h < area.Y+area.Height+h; row++ {
			for col := col0; x0+col*w < area.X+area.Width+w; col++ {
				x, y := x0+col*w, y0+row*h
				if t.offset > 0 && t.vertical && int(col)%2 != 0 {
					y += t.offset * h
				} else if t.offset > 0 && !t.vertical && int(row)%2 != 0 {
					x += t.offset * w
				}
				if x+w <= area.X || y+h <= area.Y || x >= area.X+area.Width || y >= area.Y+area.Height {
					continue
				}
				if ctx.Err() != nil {
					return nil
				}
				at := compose.Rect{X: x, Y: y, Width: w, Height: h}
				if src.Vector != nil {
					content = append(content, src.Vector(ctx, at)...)
				} else {
					content = append(content, &compose.Image{Rect: at, Source: src.Source})
				}
			}
		}
		return []compose.Fragment{&compose.Group{Clip: &compose.Path{Ops: shape.RectOps(area)}, Content: content}}
	}}
}
