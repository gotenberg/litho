package pptx

import (
	"strings"

	"github.com/gotenberg/litho/pkg/internal/convert/slides"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
	"github.com/gotenberg/litho/pkg/internal/shape"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// relVMLDrawing relates a slide to its legacy VML drawing (ECMA-376 Part
// 1, 19.3.1.24 legacyDrawing; Part 4, 14.1 VML), where PowerPoint 2007
// keeps the ink annotations a pen drew on the slide.
const relVMLDrawing = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/vmlDrawing"

// maxInkStrokes caps the ink shapes read from one slide's VML drawing.
// Anti-DoS guard, not spec-mandated: a slide written on by hand holds a
// few dozen strokes, one shape a word.
const maxInkStrokes = 4096

// inkStrokes returns the ink annotations of the slide part, drawn over its
// shapes as PowerPoint prints them: each v:shape of the slide's legacy VML
// drawing that carries o:ink (Part 4, 14.2.2.13 ink) is its path stroked in
// its stroke colour, weight and opacity, with its end caps, unfilled, in
// the box its style places at the top level of the slide (14.1.2.19 style
// left, top, width, height; coordorigin, coordsize). The ink's own trace
// data (o:ink i) is not read: the path holds the same strokes.
//
// # Corpus evidence
//
//   - openxmlsdk/pptx: v2FxTestFiles/presentation/Ink2.pptx relates a VML
//     drawing of nineteen ink shapes and draws none in its shape tree:
//     PowerPoint prints "Ballpoint Pen" in purple 1-point strokes, "Felt
//     Tip Pen" in red 1.5-point strokes and "Highlighter" in yellow
//     10-point strokes a third opaque, where the shapes' styles place them.
func (r *reader) inkStrokes(part string) []*slides.Shape {
	vml := r.related(part, relVMLDrawing)
	if vml == "" {
		return nil
	}
	root, err := r.pkg.ParseXML(r.ctx, vml, nil)
	if err != nil {
		// PowerPoint writes the legacy drawing of an OLE object or a
		// control in HTML-like VML that may not parse as XML; such a part
		// holds previews the slide's own shapes draw, and its ink, if any,
		// is lost with a warning.
		r.warn(warn.ContentUnsupported, warn.Omitted, "a slide's legacy VML drawing does not parse: ink it holds does not print")
		return nil
	}
	var out []*slides.Shape
	for _, k := range root.Kids() {
		if len(out) >= maxInkStrokes {
			r.warn(warn.LimitReached, warn.Omitted, "ink strokes past the cap of a slide do not print")
			break
		}
		if !k.Is(ooxml.NSV, "shape") || k.Child(ooxml.NSO, "ink") == nil {
			continue
		}
		if sh := r.inkStroke(k); sh != nil {
			out = append(out, sh)
		}
	}
	return out
}

// inkStroke returns the shape of one ink annotation k, nil when it places
// or draws nothing.
func (r *reader) inkStroke(k *ooxml.Element) *slides.Shape {
	st := ooxml.ParseVMLStyle(attr(k, "style"))
	var box typeset.Rect
	for _, f := range []struct {
		name string
		v    *float64
	}{{"left", &box.X}, {"top", &box.Y}, {"width", &box.Width}, {"height", &box.Height}} {
		v, ok := st.Length(f.name, 0.75)
		if !ok || !numeric.Finite(v) {
			return nil
		}
		*f.v = v
	}
	src, ok := ooxml.VMLPath(k, nil)
	if !ok {
		return nil
	}
	c, problems := shape.VML(src)
	if len(problems) > 0 {
		r.warn(warn.ContentUnsupported, warn.Approximated, "an ink stroke's path is drawn in part: "+strings.Join(problems, "; "))
	}
	line := slides.Line{Width: 0.75, Color: css.Color{A: 1}, Cap: slides.CapFlat, Join: slides.JoinRound}
	if v, ok := k.Attr("strokecolor"); ok {
		if col, ok := ooxml.VMLColor(v); ok {
			line.Color = col
		}
	}
	if v, ok := k.Attr("strokeweight"); ok {
		if w, ok := ooxml.Measure(v, 1); ok && numeric.Finite(w) && w >= 0 {
			line.Width = min(w, maxInkWeight)
		}
	}
	if s := k.Child(ooxml.NSV, "stroke"); s != nil {
		if v, ok := s.Attr("opacity"); ok {
			if o, ok := ooxml.VMLFraction(v); ok {
				line.Color.A = float64(line.Color.A * o)
			}
		}
		switch attr(s, "endcap") {
		case "round":
			line.Cap = slides.CapRound
		case "square":
			line.Cap = slides.CapSquare
		}
	}
	return &slides.Shape{
		Kind:     slides.KindGeometry,
		Box:      box,
		Geometry: &shape.Geometry{Custom: c},
		Fill:     slides.Fill{Kind: slides.FillNone},
		Line:     line,
		Name:     attr(k, "id"),
	}
}

// maxInkWeight caps an ink stroke's weight in points. Anti-DoS guard, not
// spec-mandated: PowerPoint's widest pen is a highlighter of a few tens of
// points.
const maxInkWeight = 1000
