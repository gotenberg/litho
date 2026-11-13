package svg

import (
	"context"
	"errors"
	"fmt"
	"image"
	"math"
	"strings"

	"golang.org/x/net/html"

	"github.com/gotenberg/litho/pkg/internal/effect"
	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// maxFilterPrimitives caps the primitives of one filter element. Anti-DoS
// guard, not spec-mandated: each primitive is a pass over the filter
// region's pixels, and real filters chain a handful.
const maxFilterPrimitives = 64

// maxTransferValues caps the values of one transfer function's table, and
// colourMatrixValues is the count a matrix of feColorMatrix holds (Filter
// Effects 1, 9.6). Anti-DoS guard, not spec-mandated: a table of 256
// steps already gives every byte its own.
const (
	maxTransferValues  = 256
	colourMatrixValues = 20
)

// errFilterSkipped reports a filter the renderer draws the content
// through unfiltered: it names a primitive the effect engine has not.
var errFilterSkipped = errors.New("filter primitive not drawn")

// leftOutPrimitives are the filter primitives the effect engine has not
// (see package effect): a filter holding one draws its content
// unfiltered, with a warning.
var leftOutPrimitives = map[string]bool{
	"feTurbulence": true, "feConvolveMatrix": true, "feDisplacementMap": true,
	"feDiffuseLighting": true, "feSpecularLighting": true, "feImage": true,
}

// primNames are the filter primitives' names by their lower case: an
// HTML parser lowercases the names its table of SVG elements does not
// list, feDropShadow among them (HTML Standard, 13.2.6.5).
var primNames = map[string]string{}

func init() {
	for name := range filterPrims {
		primNames[strings.ToLower(name)] = name
	}
	for name := range leftOutPrimitives {
		primNames[strings.ToLower(name)] = name
	}
}

// primName returns the name of the filter primitive n, as Filter Effects
// 1 spells it, "" for an element that is none.
func primName(n *html.Node) string {
	return primNames[strings.ToLower(n.Data)]
}

// filterPrims are the filter primitives the renderer draws.
var filterPrims = map[string]bool{
	"feGaussianBlur": true, "feOffset": true, "feFlood": true, "feColorMatrix": true,
	"feComponentTransfer": true, "feComposite": true, "feBlend": true, "feMerge": true,
	"feMorphology": true, "feTile": true, "feDropShadow": true,
}

// filterGraph is a filter element resolved for one element: its region
// and its primitives, in the user space of the element it filters.
type filterGraph struct {
	r  *renderer
	el *html.Node
	// x, y, w, h are the filter region (Filter Effects 1, 7.2).
	x, y, w, h float64
	// box is the filtered element's bounding box, and obb reports
	// primitiveUnits objectBoundingBox, whose lengths are fractions of
	// it.
	box   bbox
	obb   bool
	prims []*html.Node
	st    *style
}

// filterGraph returns the filter element of id resolved for an element
// whose bounding box is box (Filter Effects 1, 7): its region, x, y,
// width and height in filterUnits, objectBoundingBox by default at -10%,
// -10%, 120% and 120%. found reports an id that names a filter element; a
// graph with an empty region, or one in bounding box units of an element
// without area, draws nothing.
func (r *renderer) filterGraph(id string, box bbox) (g *filterGraph, found bool) {
	fe := r.d.ids[id]
	if fe == nil || fe.Data != "filter" {
		return nil, false
	}
	fs := r.d.styleOf(fe, 0)
	g = &filterGraph{r: r, el: fe, box: box, st: fs, obb: attrOf(fe, "primitiveUnits") == "objectBoundingBox"}
	bw, bh := box.x1-box.x0, box.y1-box.y0
	hasBox := box.ok && numeric.Positive(bw) && numeric.Positive(bh)
	user := attrOf(fe, "filterUnits") == "userSpaceOnUse"
	region := func(k string, a axis, def string) float64 {
		l, ok := parseLength(attrOf(fe, k))
		if _, set := attrOK(fe, k); !set || !ok {
			l, _ = parseLength(def)
		}
		if user {
			return l.resolve(r.lc(fs), a)
		}
		v := l.v
		if l.u == unitPercent {
			v /= 100
		}
		if a == axisX {
			return v * bw
		}
		return v * bh
	}
	if !user && !hasBox || g.obb && !hasBox {
		return g, true
	}
	g.x, g.y = region("x", axisX, "-10%"), region("y", axisY, "-10%")
	g.w, g.h = region("width", axisX, "120%"), region("height", axisY, "120%")
	if !user {
		g.x, g.y = box.x0+g.x, box.y0+g.y
	}
	for c := fe.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode || c.Namespace != "svg" {
			continue
		}
		if len(g.prims) >= maxFilterPrimitives {
			r.d.warn(warn.LimitReached, warn.Omitted, fmt.Sprintf("a filter of more than %d primitives is drawn with its first %d", maxFilterPrimitives, maxFilterPrimitives))
			break
		}
		g.prims = append(g.prims, c)
	}
	return g, true
}

// empty reports a graph that draws nothing: a region without area.
func (g *filterGraph) empty() bool {
	return !(g.w > 0) || !(g.h > 0) || !numeric.Finite(g.x) || !numeric.Finite(g.y)
}

// check returns an error wrapping errFilterSkipped when the graph names a
// primitive the renderer does not draw.
func (g *filterGraph) check() error {
	for _, p := range g.prims {
		if name := primName(p); leftOutPrimitives[name] {
			return fmt.Errorf("%w: %s", errFilterSkipped, name)
		}
	}
	return nil
}

// sigma returns the smallest standard deviation of the graph's blurs in
// user units, 0 for a graph that blurs nothing: what sets its resolution.
func (g *filterGraph) sigma() float64 {
	s := 0.0
	for _, p := range g.prims {
		if name := primName(p); name != "feGaussianBlur" && name != "feDropShadow" {
			continue
		}
		sx, sy := g.deviation(p)
		for _, v := range []float64{sx, sy} {
			if v > 0 && (s == 0 || v < s) {
				s = v
			}
		}
	}
	return s
}

// deviation returns the stdDeviation of p in user units along x and y.
func (g *filterGraph) deviation(p *html.Node) (float64, float64) {
	v, ok := parseNumberList(attrOf(p, "stdDeviation"), 2)
	if !ok || len(v) == 0 {
		return 0, 0
	}
	sx, sy := v[0], v[0]
	if len(v) == 2 {
		sy = v[1]
	}
	return g.length(sx, axisX), g.length(sy, axisY)
}

// length returns v, a number of primitiveUnits along a, in user units.
func (g *filterGraph) length(v float64, a axis) float64 {
	if !g.obb {
		return v
	}
	if a == axisX {
		return float64(v * (g.box.x1 - g.box.x0))
	}
	return float64(v * (g.box.y1 - g.box.y0))
}

// fresult is the result of one primitive: a surface over the whole
// filter region, its subregion in pixels, outside which it is
// transparent, and the colour space of its colours.
type fresult struct {
	s      *effect.Surface
	rect   image.Rectangle
	linear bool
}

// run applies the graph to the source graphic src, an RGBA surface of k
// pixels a user unit whose pixel (0, 0) has its corner at (ox, oy) in
// user units, and returns the result in sRGB (Filter Effects 1, 8 and 9).
// The caller releases src; the result is new.
func (g *filterGraph) run(ctx context.Context, src *effect.Surface, k, ox, oy float64) (*effect.Surface, error) {
	px := func(x, y, w, h float64) image.Rectangle {
		x0 := int(math.Floor(min(max(float64((x-ox)*k), -1), float64(src.W+1))))         // fits int: clamped to the surface
		y0 := int(math.Floor(min(max(float64((y-oy)*k), -1), float64(src.H+1))))         // fits int: clamped to the surface
		x1 := int(math.Ceil(min(max(float64(float64(x+w-ox)*k), -1), float64(src.W+1)))) // fits int: clamped to the surface
		y1 := int(math.Ceil(min(max(float64(float64(y+h-oy)*k), -1), float64(src.H+1)))) // fits int: clamped to the surface
		return image.Rect(x0, y0, x1, y1).Intersect(image.Rect(0, 0, src.W, src.H))
	}
	region := px(g.x, g.y, g.w, g.h)
	var owned []*effect.Surface
	defer func() {
		for _, s := range owned {
			s.Release()
		}
	}()
	fresh := func() (*effect.Surface, error) {
		s, err := effect.New(ctx, effect.RGBA, src.W, src.H, src.Grid)
		if err == nil {
			owned = append(owned, s)
		}
		return s, err
	}
	clone := func(s *effect.Surface) (*effect.Surface, error) {
		c, err := s.Clone(ctx)
		if err == nil {
			owned = append(owned, c)
		}
		return c, err
	}
	source := &fresult{s: src, rect: region}
	var alpha *fresult
	results := map[string]*fresult{}
	var last *fresult
	input := func(name string) (*fresult, error) {
		switch name {
		case "SourceGraphic":
			return source, nil
		case "SourceAlpha":
			if alpha == nil {
				a, err := clone(src)
				if err != nil {
					return nil, err
				}
				for i := 0; i < len(a.Pix); i += 4 {
					a.Pix[i], a.Pix[i+1], a.Pix[i+2] = 0, 0, 0
				}
				alpha = &fresult{s: a, rect: region}
			}
			return alpha, nil
		case "BackgroundImage", "BackgroundAlpha", "FillPaint", "StrokePaint":
			s, err := fresh()
			if err != nil {
				return nil, err
			}
			return &fresult{s: s, rect: region}, nil
		}
		if res, ok := results[name]; ok && name != "" {
			return res, nil
		}
		// No input, or a result no earlier primitive names: the previous
		// result, the source for the first (Filter Effects 1, 8.3).
		if last != nil {
			return last, nil
		}
		return source, nil
	}
	// in returns a copy of the input name of p in the colour space linear
	// asks, its subregion with it.
	in := func(name string, linear bool) (*effect.Surface, image.Rectangle, error) {
		res, err := input(name)
		if err != nil {
			return nil, image.Rectangle{}, err
		}
		c, err := clone(res.s)
		if err != nil {
			return nil, image.Rectangle{}, err
		}
		if err := convertSpace(ctx, c, res.linear, linear); err != nil {
			return nil, image.Rectangle{}, err
		}
		return c, res.rect, nil
	}
	for _, p := range g.prims {
		name := primName(p)
		if !filterPrims[name] {
			continue
		}
		ps := g.r.d.styleOf(p, 0)
		linear := !ps.sRGBFilters
		out, rect, err := g.primitive(ctx, p, name, ps, linear, k, in, fresh)
		if err != nil {
			return nil, err
		}
		if out == nil {
			continue
		}
		sub := g.subregion(p, rect, region, px)
		clearOutside(out, sub)
		res := &fresult{s: out, rect: sub, linear: linear}
		if name := attrOf(p, "result"); name != "" {
			results[name] = res
		}
		last = res
	}
	if last == nil {
		// A filter of no primitive draws nothing (Filter Effects 1, 8.3).
		return effect.New(ctx, effect.RGBA, src.W, src.H, src.Grid)
	}
	out, err := last.s.Clone(ctx)
	if err != nil {
		return nil, err
	}
	if err := convertSpace(ctx, out, last.linear, false); err != nil {
		out.Release()
		return nil, err
	}
	return out, nil
}

// subregion returns the primitive subregion of p in pixels (Filter
// Effects 1, 8.2): its x, y, width and height in primitiveUnits, each
// absent one taken from def, the union of its inputs' subregions or the
// filter region; within the filter region.
func (g *filterGraph) subregion(p *html.Node, def, region image.Rectangle, px func(x, y, w, h float64) image.Rectangle) image.Rectangle {
	if def.Empty() {
		def = region
	}
	_, hasX := attrOK(p, "x")
	_, hasY := attrOK(p, "y")
	_, hasW := attrOK(p, "width")
	_, hasH := attrOK(p, "height")
	if !hasX && !hasY && !hasW && !hasH {
		return def.Intersect(region)
	}
	k := float64(region.Dx()) / g.w
	ox, oy := g.x-float64(region.Min.X)/k, g.y-float64(region.Min.Y)/k
	// The default edges in user units.
	dx0, dy0 := ox+float64(def.Min.X)/k, oy+float64(def.Min.Y)/k
	dx1, dy1 := ox+float64(def.Max.X)/k, oy+float64(def.Max.Y)/k
	at := func(key string, a axis, has bool, def float64) float64 {
		if !has {
			return def
		}
		l, ok := parseLength(attrOf(p, key))
		if !ok {
			return def
		}
		if !g.obb {
			return l.resolve(g.r.lc(g.st), a)
		}
		v := l.v
		if l.u == unitPercent {
			v /= 100
		}
		if a == axisX {
			if key == "x" {
				return g.box.x0 + float64(v*(g.box.x1-g.box.x0))
			}
			return float64(v * (g.box.x1 - g.box.x0))
		}
		if key == "y" {
			return g.box.y0 + float64(v*(g.box.y1-g.box.y0))
		}
		return float64(v * (g.box.y1 - g.box.y0))
	}
	x, y := at("x", axisX, hasX, dx0), at("y", axisY, hasY, dy0)
	w, h := at("width", axisX, hasW, dx1-dx0), at("height", axisY, hasH, dy1-dy0)
	if !(w > 0) || !(h > 0) {
		return image.Rectangle{}
	}
	return px(x, y, w, h).Intersect(region)
}

// primitive applies the filter primitive p of name, styled ps, in the colour
// space linear asks, at k pixels a user unit, and returns its result and
// the union of its inputs' subregions, empty for a primitive with none.
func (g *filterGraph) primitive(
	ctx context.Context, p *html.Node, name string, ps *style, linear bool, k float64,
	in func(string, bool) (*effect.Surface, image.Rectangle, error), fresh func() (*effect.Surface, error),
) (*effect.Surface, image.Rectangle, error) {
	// num reads a number attribute of p, 0 when it is absent or invalid,
	// the default of every one the primitives read here.
	num := func(key string) float64 {
		v, _ := parseNumber(attrOf(p, key)) // an invalid number reads as absent: 0
		return v
	}
	switch name {
	case "feFlood":
		s, err := fresh()
		if err != nil {
			return nil, image.Rectangle{}, err
		}
		c, ok := g.r.colorOf(ps.floodColor, ps)
		if !ok && ps.floodColor.kind != paintCurrent {
			return s, image.Rectangle{}, nil
		}
		ec := effect.ColorOf(typeset.Color{R: c.R, G: c.G, B: c.B, A: float64(c.A * ps.floodAlpha)})
		if linear {
			ec.R, ec.G, ec.B = toLinear[ec.R], toLinear[ec.G], toLinear[ec.B]
		}
		return s, image.Rectangle{}, effect.Flood(ctx, s, ec)
	case "feMerge":
		s, err := fresh()
		if err != nil {
			return nil, image.Rectangle{}, err
		}
		var union image.Rectangle
		for c := p.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode || !strings.EqualFold(c.Data, "feMergeNode") {
				continue
			}
			node, rect, err := in(attrOf(c, "in"), linear)
			if err != nil {
				return nil, image.Rectangle{}, err
			}
			union = union.Union(rect)
			if err := effect.Composite(ctx, s, node, effect.Over); err != nil {
				return nil, image.Rectangle{}, err
			}
		}
		return s, union, nil
	}
	s, rect, err := in(attrOf(p, "in"), linear)
	if err != nil {
		return nil, image.Rectangle{}, err
	}
	switch name {
	case "feGaussianBlur":
		sx, sy := g.deviation(p)
		if sx < 0 || sy < 0 || sx == 0 && sy == 0 {
			// A negative or zero deviation disables the blur: the result
			// is the input (Filter Effects 1, 9.14).
			return s, rect, nil
		}
		edge := effect.EdgeNone
		switch attrOf(p, "edgeMode") {
		case "duplicate":
			edge = effect.EdgeDuplicate
		case "wrap":
			edge = effect.EdgeWrap
		}
		return s, rect, effect.Blur(ctx, s, effect.GaussianKernel(float64(sx*k)), effect.GaussianKernel(float64(sy*k)), edge)
	case "feOffset":
		return s, rect, effect.Offset(ctx, s, float64(g.length(num("dx"), axisX)*k), float64(g.length(num("dy"), axisY)*k))
	case "feColorMatrix":
		return s, rect, effect.ColorMatrix(ctx, s, colourMatrix(p))
	case "feComponentTransfer":
		var luts [4]effect.LUT
		for i, name := range []string{"feFuncR", "feFuncG", "feFuncB", "feFuncA"} {
			luts[i] = transferOf(p, name).Table()
		}
		return s, rect, effect.Transfer(ctx, s, &luts[0], &luts[1], &luts[2], &luts[3])
	case "feMorphology":
		v, ok := parseNumberList(attrOf(p, "radius"), 2)
		if !ok || len(v) == 0 {
			return s, rect, nil
		}
		rx, ry := v[0], v[0]
		if len(v) == 2 {
			ry = v[1]
		}
		if !(rx > 0) || !(ry > 0) {
			// A radius that is not positive disables the primitive
			// (Filter Effects 1, 9.17).
			return s, rect, nil
		}
		x, y := pixels(float64(g.length(rx, axisX)*k)), pixels(float64(g.length(ry, axisY)*k))
		if attrOf(p, "operator") == "dilate" {
			return s, rect, effect.Dilate(ctx, s, x, y)
		}
		return s, rect, effect.Erode(ctx, s, x, y)
	case "feTile":
		out, err := fresh()
		if err != nil {
			return nil, image.Rectangle{}, err
		}
		return out, image.Rectangle{}, effect.Tile(ctx, out, s, rect.Min.X, rect.Min.Y, rect.Max.X, rect.Max.Y)
	case "feComposite", "feBlend":
		dst, rect2, err := in(attrOf(p, "in2"), linear)
		if err != nil {
			return nil, image.Rectangle{}, err
		}
		union := rect.Union(rect2)
		if name == "feBlend" {
			mode, ok := blendModes[strings.ToLower(attrOf(p, "mode"))]
			if !ok {
				mode = typeset.BlendNormal
			}
			return dst, union, effect.Blend(ctx, dst, s, effect.BlendMode(mode))
		}
		switch op := attrOf(p, "operator"); op {
		case "arithmetic":
			return dst, union, effect.Arithmetic(ctx, dst, s, num("k1"), num("k2"), num("k3"), num("k4"))
		case "in", "out", "atop", "xor":
			ops := map[string]effect.Operator{"in": effect.In, "out": effect.Out, "atop": effect.Atop, "xor": effect.Xor}
			return dst, union, effect.Composite(ctx, dst, s, ops[op])
		}
		return dst, union, effect.Composite(ctx, dst, s, effect.Over)
	case "feDropShadow":
		return g.dropShadow(ctx, p, ps, linear, k, s, rect, fresh)
	}
	return s, rect, nil
}

// dropShadow applies feDropShadow (Filter Effects 1, 9.10): the input's
// alpha blurred by stdDeviation, moved by dx and dy, flooded with
// flood-color and flood-opacity, under the input.
func (g *filterGraph) dropShadow(ctx context.Context, p *html.Node, ps *style, linear bool, k float64, s *effect.Surface, rect image.Rectangle, fresh func() (*effect.Surface, error)) (*effect.Surface, image.Rectangle, error) {
	cov, err := effect.AlphaOf(ctx, s)
	if err != nil {
		return nil, image.Rectangle{}, err
	}
	defer cov.Release()
	sx, sy := g.deviation(p)
	if sx > 0 || sy > 0 {
		if err := effect.Blur(ctx, cov, effect.GaussianKernel(float64(max(sx, 0)*k)), effect.GaussianKernel(float64(max(sy, 0)*k)), effect.EdgeNone); err != nil {
			return nil, image.Rectangle{}, err
		}
	}
	dx, dy := 2.0, 2.0
	if v, ok := parseNumber(attrOf(p, "dx")); ok {
		dx = v
	}
	if v, ok := parseNumber(attrOf(p, "dy")); ok {
		dy = v
	}
	if err := effect.Offset(ctx, cov, float64(g.length(dx, axisX)*k), float64(g.length(dy, axisY)*k)); err != nil {
		return nil, image.Rectangle{}, err
	}
	c, _ := g.r.colorOf(ps.floodColor, ps)
	ec := effect.ColorOf(typeset.Color{R: c.R, G: c.G, B: c.B, A: float64(c.A * ps.floodAlpha)})
	if linear {
		ec.R, ec.G, ec.B = toLinear[ec.R], toLinear[ec.G], toLinear[ec.B]
	}
	shadow, err := effect.Tint(ctx, cov, ec)
	if err != nil {
		return nil, image.Rectangle{}, err
	}
	out, err := fresh()
	if err != nil {
		shadow.Release()
		return nil, image.Rectangle{}, err
	}
	copy(out.Pix, shadow.Pix)
	shadow.Release()
	return out, rect, effect.Composite(ctx, out, s, effect.Over)
}

// colourMatrix returns the matrix of the feColorMatrix p (Filter Effects
// 1, 9.6): its type, matrix by default, and its values; a matrix that
// does not hold 20 numbers is the identity.
func colourMatrix(p *html.Node) effect.Matrix {
	vals, ok := parseNumberList(attrOf(p, "values"), colourMatrixValues)
	_, set := attrOK(p, "values")
	switch attrOf(p, "type") {
	case "saturate":
		if !set {
			return effect.SaturateMatrix(1)
		}
		if !ok || len(vals) != 1 {
			return effect.IdentityMatrix
		}
		return effect.SaturateMatrix(vals[0])
	case "hueRotate":
		if !set || !ok || len(vals) != 1 {
			return effect.HueRotateMatrix(0)
		}
		return effect.HueRotateMatrix(vals[0])
	case "luminanceToAlpha":
		return effect.LuminanceToAlphaMatrix()
	}
	if !ok || len(vals) != colourMatrixValues {
		return effect.IdentityMatrix
	}
	var m effect.Matrix
	copy(m[:], vals)
	return m
}

// transferOf returns the transfer function of the child name of the
// feComponentTransfer p (Filter Effects 1, 9.7), the identity when it
// has none.
func transferOf(p *html.Node, name string) effect.TransferFunc {
	var fn *html.Node
	for c := p.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && strings.EqualFold(c.Data, name) {
			// The last child of a kind wins.
			fn = c
		}
	}
	if fn == nil {
		return effect.TransferFunc{}
	}
	num := func(key string, def float64) float64 {
		if v, ok := parseNumber(attrOf(fn, key)); ok {
			return v
		}
		return def
	}
	vals, ok := parseNumberList(attrOf(fn, "tableValues"), maxTransferValues)
	if !ok {
		vals = nil
	}
	switch attrOf(fn, "type") {
	case "table":
		return effect.TransferFunc{Kind: effect.TransferTable, Values: vals}
	case "discrete":
		return effect.TransferFunc{Kind: effect.TransferDiscrete, Values: vals}
	case "linear":
		return effect.TransferFunc{Kind: effect.TransferLinear, Slope: num("slope", 1), Intercept: num("intercept", 0)}
	case "gamma":
		return effect.TransferFunc{Kind: effect.TransferGamma, Amplitude: num("amplitude", 1), Exponent: num("exponent", 1), Offset: num("offset", 0)}
	}
	return effect.TransferFunc{}
}

// pixels returns v rounded to a whole number of pixels, within
// [0, effect.MaxKernelRadius].
func pixels(v float64) int {
	return int(min(max(math.Round(v), 0), effect.MaxKernelRadius)) // fits int: within [0, MaxKernelRadius]
}

// clearOutside makes the pixels of s outside r transparent.
func clearOutside(s *effect.Surface, r image.Rectangle) {
	for y := range s.H {
		row := s.Pix[y*s.W*4 : (y+1)*s.W*4]
		if y < r.Min.Y || y >= r.Max.Y {
			clear(row)
			continue
		}
		clear(row[:max(r.Min.X, 0)*4])
		clear(row[min(r.Max.X, s.W)*4:])
	}
}

// convertSpace converts the colours of s from linearRGB to sRGB or back,
// as from and to say (Filter Effects 1, 17): eight bits a channel lose
// the darkest steps of sRGB, so a round trip moves a colour by up to 6
// levels, most of them under 2.
func convertSpace(ctx context.Context, s *effect.Surface, from, to bool) error {
	switch {
	case from == to:
		return nil
	case to:
		return effect.Transfer(ctx, s, &toLinear, &toLinear, &toLinear, &effect.IdentityLUT)
	}
	return effect.Transfer(ctx, s, &toSRGB, &toSRGB, &toSRGB, &effect.IdentityLUT)
}

// filtered returns the content of the element n, styled st, whose
// bounding box is box, drawn through the filter element of id as an
// image in n's user space, an artifact: the source graphic drawn by
// effect.Paint over the filter region at the resolution of package
// effect, the graph run on it, even over no content. It returns the
// content itself, with a warning, for a filter holding a primitive the
// engine has not, and nothing for an id that names no filter element and
// for a filter region without area.
//
// # Corpus evidence
//
//   - resvg filters/filter/invalid-FuncIRI: resvg draws nothing of an
//     element whose filter names no element, as SVG 1.1 has it;
//     on-an-empty-group-1: it floods the region of a filter on an empty
//     group.
func (r *renderer) filtered(n *html.Node, st *style, id string, box *bbox, content []typeset.Fragment) []typeset.Fragment {
	var b bbox
	if box != nil {
		b = *box
	} else {
		b = r.bboxOf(n, st, identity, 0)
	}
	g, found := r.filterGraph(id, b)
	if !found || g.empty() {
		return nil
	}
	if err := g.check(); err != nil {
		r.d.warn(warn.ContentUnsupported, warn.Approximated, fmt.Sprintf("a filter is not drawn: %v; the content is drawn unfiltered", err))
		return content
	}
	img, err := g.draw(r.ctx, content, &r.filterPixels)
	if err != nil {
		r.d.warn(warn.ContentUnsupported, warn.Approximated, fmt.Sprintf("a filter is not drawn: %v; the content is drawn unfiltered", err))
		return content
	}
	if img == nil {
		return nil
	}
	return []typeset.Fragment{img}
}

// draw draws content through g over its region at the resolution package
// effect plans for its smallest blur, a user unit taken as a CSS pixel.
func (g *filterGraph) draw(ctx context.Context, content []typeset.Fragment, pixels *effect.Page) (*typeset.Image, error) {
	res, ok := effect.Plan(float64(g.sigma()*ptPerPx), float64(g.w*ptPerPx), float64(g.h*ptPerPx))
	if !ok {
		return nil, nil
	}
	k := float64(res.Scale * ptPerPx)
	if n := math.Floor(k); n >= 1 && !res.Reduced {
		// A whole number of pixels a user unit keeps the edges of shapes
		// on whole units between pixels, and a tile of whole units a
		// whole number of pixels, so its copies do not drift.
		w, h := int(math.Ceil(float64(g.w*n)-1e-9)), int(math.Ceil(float64(g.h*n)-1e-9)) // fits int: at most res.W and res.H
		k, res.W, res.H = n, max(w, 1), max(h, 1)
	}
	if !pixels.Take(res) {
		return nil, fmt.Errorf("filters past %d pixels", effect.MaxPagePixels)
	}
	grid := effect.Grid{X: g.x, Y: g.y, Scale: k}
	ss, err := effect.Paint(ctx, []effect.Content{{Fragments: content, Transform: identity, Grid: grid, W: res.W, H: res.H}})
	if err != nil {
		return nil, err
	}
	src := ss[0]
	defer src.Release()
	out, err := g.run(ctx, src, k, g.x, g.y)
	if err != nil {
		return nil, err
	}
	defer out.Release()
	return effect.Encode(ctx, out)
}
