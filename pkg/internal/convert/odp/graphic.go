package odp

import (
	"math"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/convert/slides"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/odf"
)

// The colours Impress's item pool paints with when no style gives one:
// the default shape fill, 0x729fcf, and line, 0x3465a4 (svx's
// COL_DEFAULT_SHAPE_FILLING and COL_DEFAULT_SHAPE_STROKE).
var (
	poolFill   = odf.Color{R: 0x72, G: 0x9f, B: 0xcf}
	poolStroke = odf.Color{R: 0x34, G: 0x65, B: 0xa4}
)

// hairline is the width a stroke of svg:stroke-width 0 is drawn at.
// Impress writes such a stroke with a PDF line width of 0, the thinnest
// line the device draws; the model strokes nothing at 0, so the reader
// gives it half a point, a device pixel at 150 dpi.
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
		r.warn("opacity-gradient", "a transparency gradient is drawn as its mean opacity")
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
		if rep == "no-repeat" {
			r.warn("bitmap-no-repeat", "a picture fill placed once at its size is stretched over the shape")
		}
		if _, ok := p.Lookup("draw:fill-image-width"); ok && rep == "repeat" {
			r.warn("bitmap-tile-size", "a tiled picture fill's tile size and reference point are not applied: the tiles take the picture's size")
		}
		return slides.Fill{Kind: slides.FillPicture, Picture: pic, Tile: rep == "repeat"}
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
// fraction painted in the start colour first; an axial one has the start
// colour in the middle and the end colour at both edges, the border in
// the end colour at the edges; a radial one has the end colour at the
// centre and the start colour at half the box's diagonal, the border in
// the start colour outside. Ellipsoid, square and rectangular gradients
// are drawn radial, and every centre is the box's, with a warning.
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
	out := &slides.Gradient{}
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
		if g.CX != 0.5 || g.CY != 0.5 {
			r.warn("gradient-centre", "a radial gradient's centre is drawn at the shape's centre")
		}
		out.Radial = true
		for i := len(stops) - 1; i >= 0; i-- {
			out.Stops = append(out.Stops, slides.Stop{Position: (1 - b) * (1 - stops[i].Offset), Color: col(stops[i].Color)})
		}
		if b > 0 {
			out.Stops = append(out.Stops, slides.Stop{Position: 1, Color: col(stops[0].Color)})
		}
	default:
		if b > 0 {
			out.Stops = append(out.Stops, slides.Stop{Position: 0, Color: col(stops[0].Color)})
		}
		for _, s := range stops {
			out.Stops = append(out.Stops, slides.Stop{Position: b + (1-b)*s.Offset, Color: col(s.Color)})
		}
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
// DrawingML pattern preset of the same lines (ECMA-376 Part 1, 20.1.10.51):
// horizontal, vertical or diagonal lines, crossed for a double or a
// triple hatch, at the angle nearest its draw:rotation. The spacing is
// the preset's.
func hatchPattern(h *odf.Hatch) string {
	a := math.Mod(math.Round(h.Rotation/45)*45, 180)
	if a < 0 {
		a += 180
	}
	cross := h.Style == "double" || h.Style == "triple"
	switch {
	case cross && (a == 45 || a == 135):
		return "diagCross"
	case cross:
		return "cross"
	case a == 0:
		return "horz"
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
		ln.Width = hairline
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

// effects warns for the graphic effects the model does not carry: a
// shadow (ODF 1.3, Part 3, 20.158 draw:shadow), a glow and a soft edge.
func (r *reader) effects(p odf.Props) {
	if p.Get("draw:shadow") == "visible" {
		r.warn("shadow", "shape shadows are not drawn")
	}
	if v, ok := odf.Length(p.Get("loext:glow-radius")); ok && v > 0 {
		r.warn("glow", "glow effects are not drawn")
	}
	if v, ok := odf.Length(p.Get("loext:softedge-radius")); ok && v > 0 {
		r.warn("soft-edge", "soft edges are not drawn")
	}
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
		r.warn("fit-to-size", "text stretched to its shape's size is drawn at its own size")
	}
	if n, err := strconv.Atoi(p.Get("fo:column-count")); err == nil && n > 1 {
		tb.Columns = min(n, maxColumns)
		tb.ColumnGap, _ = odf.Length(p.Get("fo:column-gap"))
	}
	switch p.Get("style:writing-mode") {
	case "tb-rl", "tb":
		tb.Vertical = css.VerticalRL
	case "tb-lr":
		tb.Vertical = css.VerticalLR
	case "rl-tb", "rl":
		tb.RightToLeft = true
	}
}

// maxColumns bounds the text columns of a shape: 16, Impress's own limit
// (the column dialog's maximum).
const maxColumns = 16
