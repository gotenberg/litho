package odf

import (
	"math"
	"strconv"
	"strings"
)

// DrawStyles indexes the drawing styles of a document (ODF 1.3, Part 3,
// 16.42): the gradients, hatches, fill images, opacity gradients, markers
// and stroke dashes its graphic and drawing-page styles name through
// draw:fill-gradient-name, draw:fill-hatch-name, draw:fill-image-name,
// draw:opacity-name, draw:marker-start and -end and draw:stroke-dash. They
// are common styles of office:styles, keyed by draw:name; the first of a
// name wins. A DrawStyles is read-only once built and safe for concurrent
// reads.
type DrawStyles struct {
	Gradients  map[string]*Gradient
	Opacities  map[string]*Gradient
	Hatches    map[string]*Hatch
	FillImages map[string]*Element
	Markers    map[string]*Marker
	Dashes     map[string]*StrokeDash

	legacy bool
}

// Gradient is a draw:gradient (ODF 1.3, Part 3, 16.42.1) or, for an
// opacity gradient, a draw:opacity (16.42.7), whose Start and End are then
// the opacities in StartValue and EndValue.
type Gradient struct {
	// Style is draw:style (19.218): linear, axial, radial, ellipsoid,
	// square or rectangular; linear when absent.
	Style string
	// CX and CY are the centre of a radial, ellipsoid, square or
	// rectangular gradient as fractions of the area (19.129, 19.130), 0.5
	// when absent.
	CX, CY float64
	// Start and End are draw:start-color and draw:end-color (19.214,
	// 19.141), black and white when absent, already multiplied by
	// draw:start-intensity and draw:end-intensity (19.216, 19.143).
	Start, End Color
	// StartValue and EndValue are an opacity gradient's draw:start and
	// draw:end as fractions (19.212, 19.139), 1 and 0 when absent.
	StartValue, EndValue float64
	// Angle is draw:angle in degrees, counterclockwise, 0 drawing the
	// start colour at the top (19.112), read as [ParseAngle] reads it.
	Angle float64
	// Border is draw:border as a fraction (19.114): the part of the
	// gradient's length the start colour fills unblended.
	Border float64
	// Stops are LibreOffice's loext:gradient-stop children, a gradient of
	// more than two colours by svg:offset, nil when the gradient has
	// none; they replace Start and End when set.
	Stops []GradientStop
}

// GradientStop is one loext:gradient-stop of a multi-colour gradient.
type GradientStop struct {
	Offset float64
	Color  Color
}

// Hatch is a draw:hatch (ODF 1.3, Part 3, 16.42.5).
type Hatch struct {
	// Style is draw:style: single, double or triple (19.218).
	Style string
	// Color is draw:color (19.122); Distance draw:distance in points
	// (19.134); Rotation draw:rotation in degrees, counterclockwise
	// (19.209).
	Color    Color
	Distance float64
	Rotation float64
}

// Marker is a draw:marker (ODF 1.3, Part 3, 16.42.8): the outline of a
// line end in its own coordinate space.
type Marker struct {
	// ViewBox is svg:viewBox (19.574): x, y, width and height.
	ViewBox [4]float64
	// D is svg:d, the path data (19.530).
	D string
}

// StrokeDash is a draw:stroke-dash (ODF 1.3, Part 3, 16.42.9): Dots1 dots
// of Dots1Length, Dots2 dots of Dots2Length, each followed by Distance.
type StrokeDash struct {
	// Style is draw:style: rect or round (19.218), the dots' caps.
	Style string
	// Dots1 and Dots2 are the dot counts (draw:dots1 and draw:dots2).
	Dots1, Dots2 int
	// Dots1Length, Dots2Length and Distance are draw:dots1-length,
	// draw:dots2-length and draw:distance (19.134): lengths in points, or,
	// when Relative is set, fractions of the stroke width, which
	// LibreOffice reads every value as when one of them is a percentage.
	// A zero length draws a dot, or a gap, as long as the stroke is wide.
	Dots1Length, Dots2Length, Distance float64
	Relative                           bool
}

// maxDashDots bounds a stroke dash's dot counts: 32. Anti-DoS guard, not
// spec-mandated: LibreOffice's dialog offers up to 99 but its presets use
// three at most, and each dot costs two entries of a dash array.
const maxDashDots = 32

// NewDrawStyles indexes the drawing styles of p: those of styles.xml's
// office:styles, then content.xml's, which LibreOffice never writes but a
// flat document's one tree holds.
func NewDrawStyles(p *Package) *DrawStyles {
	d := &DrawStyles{
		Gradients: map[string]*Gradient{}, Opacities: map[string]*Gradient{}, Hatches: map[string]*Hatch{},
		FillImages: map[string]*Element{}, Markers: map[string]*Marker{}, Dashes: map[string]*StrokeDash{},
		legacy: LegacyAngles(p),
	}
	parts := []*Element{p.Styles, p.Content}
	if p.Flat {
		parts = parts[:1]
	}
	for _, part := range parts {
		for _, sec := range []string{"styles", "automatic-styles"} {
			d.add(Section(part, sec))
		}
	}
	return d
}

// add records the drawing styles of an office:styles element.
func (d *DrawStyles) add(sec *Element) {
	if sec == nil {
		return
	}
	for _, el := range sec.Children {
		if el.Name.Space != NSDraw {
			continue
		}
		name := el.A(NSDraw, "name")
		if name == "" {
			continue
		}
		switch el.Name.Local {
		case "gradient":
			if d.Gradients[name] == nil {
				d.Gradients[name] = parseGradient(el, false, d.legacy)
			}
		case "opacity":
			if d.Opacities[name] == nil {
				d.Opacities[name] = parseGradient(el, true, d.legacy)
			}
		case "hatch":
			if d.Hatches[name] == nil {
				h := &Hatch{Style: el.A(NSDraw, "style")}
				h.Color, _ = ParseColor(el.A(NSDraw, "color"))
				h.Distance, _ = Length(el.A(NSDraw, "distance"))
				h.Rotation, _ = ParseAngle(el.A(NSDraw, "rotation"), d.legacy)
				d.Hatches[name] = h
			}
		case "fill-image":
			if d.FillImages[name] == nil {
				d.FillImages[name] = el
			}
		case "marker":
			if d.Markers[name] == nil {
				m := &Marker{D: el.A(NSSVG, "d")}
				if vb, ok := ParseViewBox(el.A(NSSVG, "viewBox")); ok {
					m.ViewBox = vb
				}
				d.Markers[name] = m
			}
		case "stroke-dash":
			if d.Dashes[name] == nil {
				d.Dashes[name] = parseDash(el)
			}
		}
	}
}

// parseGradient reads a draw:gradient, or a draw:opacity when opacity is
// set.
func parseGradient(el *Element, opacity, legacy bool) *Gradient {
	g := &Gradient{Style: el.A(NSDraw, "style"), CX: 0.5, CY: 0.5, End: Color{255, 255, 255}, StartValue: 1}
	if g.Style == "" {
		g.Style = "linear"
	}
	if v, ok := Percent(el.A(NSDraw, "cx")); ok {
		g.CX = clamp01(v)
	}
	if v, ok := Percent(el.A(NSDraw, "cy")); ok {
		g.CY = clamp01(v)
	}
	if v, ok := ParseAngle(el.A(NSDraw, "angle"), legacy); ok {
		g.Angle = v
	}
	if v, ok := Percent(el.A(NSDraw, "border")); ok {
		g.Border = clamp01(v)
	}
	if opacity {
		if v, ok := Percent(el.A(NSDraw, "start")); ok {
			g.StartValue = clamp01(v)
		}
		if v, ok := Percent(el.A(NSDraw, "end")); ok {
			g.EndValue = clamp01(v)
		} else {
			g.EndValue = 0
		}
		return g
	}
	if c, ok := ParseColor(el.A(NSDraw, "start-color")); ok {
		g.Start = c
	}
	if c, ok := ParseColor(el.A(NSDraw, "end-color")); ok {
		g.End = c
	}
	if v, ok := Percent(el.A(NSDraw, "start-intensity")); ok {
		g.Start = intensity(g.Start, v)
	}
	if v, ok := Percent(el.A(NSDraw, "end-intensity")); ok {
		g.End = intensity(g.End, v)
	}
	for _, s := range el.All(NSLOExt, "gradient-stop") {
		if len(g.Stops) >= maxGradientStops {
			break
		}
		off, err := strconv.ParseFloat(strings.TrimSpace(s.A(NSSVG, "offset")), 64)
		if err != nil || math.IsNaN(off) {
			continue
		}
		c, ok := ParseColor(s.A(NSLOExt, "color-value"))
		if !ok {
			continue
		}
		g.Stops = append(g.Stops, GradientStop{Offset: clamp01(off), Color: c})
	}
	if len(g.Stops) < 2 {
		g.Stops = nil
	}
	return g
}

// maxGradientStops bounds the stops of one gradient read: 64. Anti-DoS
// guard, not spec-mandated: LibreOffice's multi-colour gradients come
// from OOXML ones, which carry ten stops at most in practice.
const maxGradientStops = 64

// intensity scales a colour by an intensity in [0, 1], as LibreOffice
// draws draw:start-intensity and draw:end-intensity: towards black.
func intensity(c Color, v float64) Color {
	v = clamp01(v)
	f := func(x uint8) uint8 { return uint8(math.Round(float64(x) * v)) }
	return Color{R: f(c.R), G: f(c.G), B: f(c.B)}
}

// parseDash reads a draw:stroke-dash.
func parseDash(el *Element) *StrokeDash {
	s := &StrokeDash{Style: el.A(NSDraw, "style")}
	count := func(local string) int {
		n, err := strconv.Atoi(strings.TrimSpace(el.A(NSDraw, local)))
		if err != nil || n < 0 {
			return 0
		}
		return min(n, maxDashDots)
	}
	vals := [3]string{el.A(NSDraw, "dots1-length"), el.A(NSDraw, "dots2-length"), el.A(NSDraw, "distance")}
	for _, v := range vals {
		if strings.Contains(v, "%") {
			s.Relative = true
		}
	}
	var out [3]float64
	for i, v := range vals {
		if p, ok := Percent(v); ok {
			out[i] = max(p, 0)
		} else if l, ok := Length(v); ok {
			out[i] = max(l, 0)
		}
	}
	s.Dots1, s.Dots2 = count("dots1"), count("dots2")
	s.Dots1Length, s.Dots2Length, s.Distance = out[0], out[1], out[2]
	return s
}

// clamp01 returns v clamped to [0, 1].
func clamp01(v float64) float64 { return math.Max(0, math.Min(1, v)) }

// ParseAngle parses an ODF angle (ODF 1.3, Part 3, 18.3.1, angle): a
// number with deg, grad or rad, or a number without a unit in degrees, as
// degrees in [0, 360), and whether it parsed. legacy reads a number
// without a unit as tenths of a degree instead, as LibreOffice reads the
// draw:angle of a gradient and the draw:rotation of a hatch of a document
// [LegacyAngles] reports.
func ParseAngle(s string, legacy bool) (float64, bool) {
	s = strings.TrimSpace(s)
	scale := 1.0
	if legacy {
		scale = 0.1
	}
	for _, u := range []struct {
		suffix string
		f      float64
	}{{"deg", 1}, {"grad", 0.9}, {"rad", 180 / math.Pi}} {
		if v, ok := strings.CutSuffix(s, u.suffix); ok {
			s, scale = strings.TrimSpace(v), u.f
			break
		}
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > 1e9 {
		return 0, false
	}
	v = math.Mod(v*scale, 360)
	if v < 0 {
		v += 360
	}
	return v, true
}

// LegacyAngles reports a document whose unitless gradient and hatch
// angles are tenths of a degree: one of ODF 1.0 or 1.1 (office:version
// below 1.2), or of ODF 1.2 written by OpenOffice.org, Apache OpenOffice
// or LibreOffice before 7.0, which wrote them so (LibreOffice's
// tdf#89475 detection in xmloff's gradient import). ODF 1.2 and later
// read them as degrees.
func LegacyAngles(p *Package) bool {
	root := p.Content
	if root == nil {
		root = p.Styles
	}
	ver := strings.TrimSpace(root.A(NSOffice, "version"))
	switch {
	case ver == "", ver == "1.0", ver == "1.1":
		return true
	case ver != "1.2":
		return false
	}
	gen := strings.TrimSpace(Section(p.Meta, "meta").Child(NSMeta, "generator").TextContent())
	switch {
	case strings.HasPrefix(gen, "OpenOffice"), strings.HasPrefix(gen, "Apache_OpenOffice"), strings.HasPrefix(gen, "StarOffice"):
		return true
	case strings.HasPrefix(gen, "LibreOffice/"), strings.HasPrefix(gen, "LibreOfficeDev/"):
		// A development build names itself LibreOfficeDev (odp:
		// sd/tdf127379's LibreOfficeDev/6.4 gradients of draw:angle 300
		// print turned 30 degrees).
		_, ver, _ := strings.Cut(gen, "/")
		major, _, _ := strings.Cut(ver, ".")
		n, err := strconv.Atoi(major)
		return err == nil && n < 7
	}
	return false
}

// ParseViewBox parses svg:viewBox, four numbers (ODF 1.3, Part 3,
// 19.574; SVG 1.1, 7.7), and whether it parsed with a positive width and
// height.
func ParseViewBox(s string) ([4]float64, bool) {
	f := strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == ',' || r == '\t' || r == '\n' || r == '\r' })
	var vb [4]float64
	if len(f) != 4 {
		return vb, false
	}
	for i, x := range f {
		v, err := strconv.ParseFloat(x, 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > 1e12 {
			return vb, false
		}
		vb[i] = v
	}
	return vb, vb[2] > 0 && vb[3] > 0
}

// smallestDash is the shortest dash, dot or gap LibreOffice draws of an
// absolute stroke dash, and the width it takes a hairline's to be: 26.95
// hundredths of a millimetre (svx's SMALLEST_DASH_WIDTH), 0.764 points.
const smallestDash = 0.2695 * 72 / 25.4

// DashArray returns the dash pattern of s for a stroke width in points,
// dash then gap, repeated, as LibreOffice draws it (svx's
// XDash::CreateDotDashArray): the Dots1 dots, each followed by the
// distance, then the Dots2 dots. A relative length is a fraction of the
// width, a zero one the width itself; an absolute length is at least
// [smallestDash], a zero one at least the width; a hairline counts as
// [smallestDash] wide. nil when s draws no dot.
func (s *StrokeDash) DashArray(width float64) []float64 {
	if s == nil || s.Dots1+s.Dots2 == 0 {
		return nil
	}
	if width <= 0 {
		width = smallestDash
	}
	resolve := func(v float64) float64 {
		switch {
		case s.Relative && v > 0:
			return v * width
		case s.Relative, v == 0:
			return max(v, width)
		}
		return max(v, smallestDash)
	}
	d1, d2, gap := resolve(s.Dots1Length), resolve(s.Dots2Length), resolve(s.Distance)
	out := make([]float64, 0, 2*(s.Dots1+s.Dots2))
	for range s.Dots1 {
		out = append(out, d1, gap)
	}
	for range s.Dots2 {
		out = append(out, d2, gap)
	}
	return out
}
