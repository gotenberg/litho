package shade

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/function"
	"github.com/gotenberg/litho/pkg/internal/render/geom"
)

// maxComponents bounds the colour components a shading's space declares.
// Anti-DoS, not spec-mandated as such: ISO 32000-1, Annex C, Table C.1 caps a
// DeviceN space at 32 colorants, the widest space a shading can name, so a
// caller reporting more describes no valid space.
const maxComponents = 32

// rampSize is the number of samples a one-input shading function is
// tabulated at. Evaluating the function once per pixel would cost a function
// call, an allocation and a colour conversion per pixel; the table costs them
// rampSize times per paint. Linear interpolation between samples reproduces a
// smooth function to well under one 8-bit level, and blurs a step in a
// stitched function over 1/1023 of the parametric span, under one pixel for
// any gradient narrower than 1023 pixels.
const rampSize = 1024

// ComponentsFunc reports how many colour components the colour space cs has.
// The caller resolves /ColorSpace (ISO 32000-1, 8.6), which this package does
// not interpret: an Indexed space has one component, a DeviceN space one per
// colorant.
type ComponentsFunc func(ctx context.Context, cs ast.PdfObject) (int, error)

// Shading is a compiled shading (ISO 32000-1, 8.7.4.5), ready to paint any
// number of times.
//
// Values are safe for concurrent use after [New] returns.
type Shading struct {
	kind  int
	ncomp int
	cs    ast.PdfObject

	background []float64
	bbox       geom.Rect
	hasBBox    bool

	// funcs is the /Function entry: one function with ncomp outputs, or
	// ncomp functions of one output each. Nil for a mesh without one.
	funcs []*function.Function

	// ramp tabulates a one-input function at rampSize points over
	// [rampLo, rampHi], ncomp values per point. Types 2, 3 and the meshes
	// that carry a /Function.
	ramp           []float64
	rampLo, rampHi float64

	// Type 1.
	domain [4]float64
	matrix geom.Matrix

	// Types 2 and 3.
	coords [6]float64
	t0, t1 float64
	extend [2]bool

	// Types 4 to 7. Each vertex or patch corner owns nvals values in
	// colors: ncomp components, or one parametric value t under a
	// /Function.
	nvals   int
	points  []geom.Point
	colors  []float64
	tris    [][3]int32
	patches []patch
}

// patch is one tensor-product patch: p[4*i+j] is control point p_ij of ISO
// 32000-1, 8.7.4.5.8, i along u and j along v, and c holds the colour
// offsets of corners c00, c03, c33 and c30, in that order.
type patch struct {
	p [16]geom.Point
	c [4]int32
}

// New compiles the shading obj, a dictionary for types 1 to 3 or a stream
// for types 4 to 7 (ISO 32000-1, 8.7.4.5, Table 78). components resolves the
// shading's /ColorSpace.
//
// A mesh's vertex data is read here, so every bound on it applies at
// compile time: [New] returns [ErrTooLarge] past them.
func New(ctx context.Context, doc *document.Document, obj ast.PdfObject, components ComponentsFunc) (*Shading, error) {
	resolved, err := document.ResolveObject(ctx, doc, obj)
	if err != nil {
		return nil, fmt.Errorf("resolving shading: %w", err)
	}
	var dict ast.PdfDict
	var stream ast.PdfStream
	isStream := false
	switch v := resolved.(type) {
	case ast.PdfDict:
		dict = v
	case ast.PdfStream:
		dict, stream, isStream = v.Dict, v, true
	default:
		return nil, fmt.Errorf("%w: a shading is a dictionary or stream, got %T", ErrMalformed, resolved)
	}

	kind, err := document.ResolveInt(ctx, doc, dict["ShadingType"])
	if err != nil {
		return nil, fmt.Errorf("%w: no /ShadingType: %w", ErrMalformed, err)
	}
	s := &Shading{kind: int(kind)}
	if s.kind < 1 || s.kind > 7 {
		return nil, fmt.Errorf("%w: /ShadingType %d", ErrUnsupportedType, s.kind)
	}

	s.cs = dict["ColorSpace"]
	if s.cs == nil {
		return nil, fmt.Errorf("%w: no /ColorSpace", ErrMalformed)
	}
	if s.ncomp, err = components(ctx, s.cs); err != nil {
		return nil, fmt.Errorf("resolving shading colour space: %w", err)
	}
	if s.ncomp < 1 || s.ncomp > maxComponents {
		return nil, fmt.Errorf("%w: colour space has %d components", ErrMalformed, s.ncomp)
	}

	if err := s.readCommon(ctx, doc, dict); err != nil {
		return nil, err
	}

	switch s.kind {
	case 1:
		err = s.initFunctionBased(ctx, doc, dict)
	case 2, 3:
		err = s.initGradient(ctx, doc, dict)
	default:
		if !isStream {
			return nil, fmt.Errorf("%w: a type %d shading shall be a stream", ErrMalformed, s.kind)
		}
		err = s.initMesh(ctx, doc, dict, stream)
	}
	if err != nil {
		return nil, err
	}
	return s, nil
}

// readCommon reads the entries Table 78 gives every type: /Background,
// /BBox and /Function. /AntiAlias is a hint this renderer does not need.
func (s *Shading) readCommon(ctx context.Context, doc *document.Document, dict ast.PdfDict) error {
	if bg, present := dict["Background"]; present {
		vals, err := numbers(ctx, doc, bg)
		if err != nil || len(vals) != s.ncomp {
			return fmt.Errorf("%w: /Background holds %d values for %d components", ErrMalformed, len(vals), s.ncomp)
		}
		s.background = vals
	}
	if box, present := dict["BBox"]; present {
		vals, err := numbers(ctx, doc, box)
		if err != nil || len(vals) != 4 {
			return fmt.Errorf("%w: /BBox is not a rectangle", ErrMalformed)
		}
		s.bbox = geom.Rect{
			Min: geom.Point{X: min(vals[0], vals[2]), Y: min(vals[1], vals[3])},
			Max: geom.Point{X: max(vals[0], vals[2]), Y: max(vals[1], vals[3])},
		}
		s.hasBBox = true
	}
	fn, present := dict["Function"]
	if !present {
		return nil
	}
	resolved, err := document.ResolveObject(ctx, doc, fn)
	if err != nil {
		return fmt.Errorf("resolving /Function: %w", err)
	}
	// Per Table 78, /Function is one function with n outputs or an array
	// of n functions with one output each, n the colour space's components.
	if arr, isArray := resolved.(ast.PdfArray); isArray {
		if len(arr) != s.ncomp {
			return fmt.Errorf("%w: /Function holds %d functions for %d components", ErrMalformed, len(arr), s.ncomp)
		}
		for i, part := range arr {
			f, err := function.New(ctx, doc, part)
			if err != nil {
				return fmt.Errorf("compiling shading function %d: %w", i, err)
			}
			if f.Outputs() != 1 {
				return fmt.Errorf("%w: shading function %d yields %d values, want 1", ErrMalformed, i, f.Outputs())
			}
			s.funcs = append(s.funcs, f)
		}
	} else {
		f, err := function.New(ctx, doc, fn)
		if err != nil {
			return fmt.Errorf("compiling shading function: %w", err)
		}
		if f.Outputs() != s.ncomp {
			return fmt.Errorf("%w: shading function yields %d values for %d components", ErrMalformed, f.Outputs(), s.ncomp)
		}
		s.funcs = []*function.Function{f}
	}
	// Per Table 79 a type 1 function takes two inputs (x, y); every other
	// type feeds one parametric value t (Tables 80, 81, 82, 84).
	inputs := 1
	if s.kind == 1 {
		inputs = 2
	}
	for i, f := range s.funcs {
		if f.Inputs() != inputs {
			return fmt.Errorf("%w: shading function %d takes %d inputs, want %d", ErrMalformed, i, f.Inputs(), inputs)
		}
	}
	return nil
}

// initFunctionBased reads a type 1 shading (ISO 32000-1, 8.7.4.5.2, Table
// 79): /Domain defaults to [0 1 0 1], /Matrix to the identity, and
// /Function is required.
func (s *Shading) initFunctionBased(ctx context.Context, doc *document.Document, dict ast.PdfDict) error {
	if s.funcs == nil {
		return fmt.Errorf("%w: a type 1 shading requires /Function", ErrMalformed)
	}
	s.domain = [4]float64{0, 1, 0, 1}
	if d, present := dict["Domain"]; present {
		vals, err := numbers(ctx, doc, d)
		if err != nil || len(vals) != 4 {
			return fmt.Errorf("%w: /Domain holds 4 numbers", ErrMalformed)
		}
		copy(s.domain[:], vals)
	}
	s.matrix = geom.Identity
	if m, present := dict["Matrix"]; present {
		vals, err := numbers(ctx, doc, m)
		if err != nil || len(vals) != 6 {
			return fmt.Errorf("%w: /Matrix holds 6 numbers", ErrMalformed)
		}
		copy(s.matrix[:], vals)
	}
	return nil
}

// initGradient reads an axial (Table 80) or radial (Table 81) shading:
// /Coords is required, /Domain defaults to [0 1], /Extend to [false false],
// and /Function is required.
func (s *Shading) initGradient(ctx context.Context, doc *document.Document, dict ast.PdfDict) error {
	if s.funcs == nil {
		return fmt.Errorf("%w: a type %d shading requires /Function", ErrMalformed, s.kind)
	}
	want := 4
	if s.kind == 3 {
		want = 6
	}
	vals, err := numbers(ctx, doc, dict["Coords"])
	if err != nil || len(vals) != want {
		return fmt.Errorf("%w: /Coords holds %d numbers, want %d", ErrMalformed, len(vals), want)
	}
	copy(s.coords[:], vals)
	// Per Table 81 both radii shall be non-negative.
	if s.kind == 3 && (s.coords[2] < 0 || s.coords[5] < 0) {
		return fmt.Errorf("%w: radial /Coords has a negative radius", ErrMalformed)
	}
	s.t0, s.t1 = 0, 1
	if d, present := dict["Domain"]; present {
		dom, err := numbers(ctx, doc, d)
		if err != nil || len(dom) != 2 {
			return fmt.Errorf("%w: /Domain holds 2 numbers", ErrMalformed)
		}
		s.t0, s.t1 = dom[0], dom[1]
	}
	if e, present := dict["Extend"]; present {
		arr, err := document.ResolveArray(ctx, doc, e)
		if err != nil || len(arr) != 2 {
			return fmt.Errorf("%w: /Extend holds 2 booleans", ErrMalformed)
		}
		for i, v := range arr {
			b, err := document.ResolveBool(ctx, doc, v)
			if err != nil {
				return fmt.Errorf("%w: /Extend entry %d: %w", ErrMalformed, i, err)
			}
			s.extend[i] = bool(b)
		}
	}
	return s.buildRamp(s.t0, s.t1)
}

// buildRamp tabulates the shading's one-input function over [lo, hi].
func (s *Shading) buildRamp(lo, hi float64) error {
	s.rampLo, s.rampHi = lo, hi
	s.ramp = make([]float64, rampSize*s.ncomp)
	in := []float64{0}
	for i := range rampSize {
		in[0] = lo + (hi-lo)*float64(i)/float64(rampSize-1)
		if err := s.evalFuncs(in, s.ramp[i*s.ncomp:(i+1)*s.ncomp]); err != nil {
			return fmt.Errorf("tabulating shading function at t=%g: %w", in[0], err)
		}
	}
	return nil
}

// evalFuncs evaluates /Function at in into out, ncomp values.
func (s *Shading) evalFuncs(in, out []float64) error {
	if len(s.funcs) == 1 {
		v, err := s.funcs[0].Eval(in)
		if err != nil {
			return fmt.Errorf("evaluating shading function: %w", err)
		}
		copy(out, v)
		return nil
	}
	for i, f := range s.funcs {
		v, err := f.Eval(in)
		if err != nil {
			return fmt.Errorf("evaluating shading function %d: %w", i, err)
		}
		out[i] = v[0]
	}
	return nil
}

// Type returns the /ShadingType, 1 to 7.
func (s *Shading) Type() int { return s.kind }

// ColorSpace returns the /ColorSpace entry as the file wrote it; the
// components [Shading.Paint] passes to a [Converter] are in this space.
func (s *Shading) ColorSpace() ast.PdfObject { return s.cs }

// Components returns the number of colour components of the shading's
// colour space.
func (s *Shading) Components() int { return s.ncomp }

// Background returns the /Background colour, and false when the shading
// has none.
func (s *Shading) Background() ([]float64, bool) {
	return append([]float64(nil), s.background...), s.background != nil
}

// BBox returns the /BBox in shading space, and false when the shading has
// none.
func (s *Shading) BBox() (geom.Rect, bool) { return s.bbox, s.hasBBox }

// numbers resolves obj as an array of numbers.
func numbers(ctx context.Context, doc *document.Document, obj ast.PdfObject) ([]float64, error) {
	if obj == nil {
		return nil, fmt.Errorf("%w: absent number array", ErrMalformed)
	}
	arr, err := document.ResolveArray(ctx, doc, obj)
	if err != nil {
		return nil, fmt.Errorf("resolving number array: %w", err)
	}
	out := make([]float64, 0, len(arr))
	for _, v := range arr {
		resolved, err := document.ResolveObject(ctx, doc, v)
		if err != nil {
			return nil, fmt.Errorf("resolving array element: %w", err)
		}
		n, isNum := document.Number(resolved)
		if !isNum {
			return nil, fmt.Errorf("%w: array holds a non-number %T", ErrMalformed, resolved)
		}
		out = append(out, n)
	}
	return out, nil
}
