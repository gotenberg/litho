package recolor

import (
	"context"
	"fmt"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/function"
)

// maxStitchDepth bounds how deep [session.resampleFunction] keeps the
// structure of nested stitching functions. Anti-DoS guard, not
// spec-mandated: the function package compiles at most a few levels, and a
// deeper part is sampled whole instead.
const maxStitchDepth = 4

// rewriteShading converts a shading (ISO 32000-1, 8.7.4.5) whose colour
// space converts: its /ColorSpace becomes the target, /Background converts,
// a /Function is sampled again into the target, and a mesh's vertex colours
// convert one by one. A shading in a kept space (a spot colour, an Indexed
// space) keeps its colour values and gets the rewritten definition.
func (s *session) rewriteShading(ctx context.Context, obj ast.PdfObject) (ast.PdfObject, bool, error) {
	if obj == nil {
		return nil, false, nil
	}
	return s.edit(ctx, obj, "shading", func(resolved ast.PdfObject) (ast.PdfObject, bool, error) {
		var dict ast.PdfDict
		stream, isStream := resolved.(ast.PdfStream)
		if isStream {
			dict = stream.Dict
		} else if d, ok := resolved.(ast.PdfDict); ok {
			dict = d
		} else {
			return nil, false, fmt.Errorf("%w: shading is %T", ErrUnsupportedSpace, resolved)
		}
		p := s.planDefinition(ctx, dict["ColorSpace"], nil)
		out := maps.Clone(dict)
		switch p.kind {
		case planIdentity:
			return nil, false, nil
		case planKeep:
			cs, ch, err := s.rewriteDefinition(ctx, dict["ColorSpace"])
			if err != nil || !ch {
				return nil, false, err
			}
			out["ColorSpace"] = cs
			return shadingOut(out, stream, isStream), true, nil
		case planConvert:
		case planPattern, planUnknown:
			// 8.7.4.5, Table 78: a shading's space is no Pattern space.
			return nil, false, fmt.Errorf("%w: shading colour space: %v", ErrUnsupportedSpace, p.err)
		}
		out["ColorSpace"] = s.t.spaceObject()
		if bg, ok := s.numbers(ctx, dict["Background"]); ok && len(bg) == p.n {
			conv := make([]float64, s.t.n)
			p.conv.Convert(bg, conv)
			out["Background"] = componentArray(conv)
		}
		kind, err := document.ResolveInt(ctx, s.doc, dict["ShadingType"])
		if err != nil {
			return nil, false, fmt.Errorf("%w: /ShadingType: %w", ErrUnsupportedSpace, err)
		}
		var data []byte
		switch {
		case dict["Function"] != nil:
			domain := []float64{0, 1}
			switch {
			case kind == 1:
				domain = []float64{0, 1, 0, 1}
				if v, ok := s.numbers(ctx, dict["Domain"]); ok && len(v) == 4 {
					domain = v
				}
			case kind <= 3:
				if v, ok := s.numbers(ctx, dict["Domain"]); ok && len(v) == 2 {
					domain = v
				}
			default:
				// 8.7.4.5.5: under a /Function a vertex carries the
				// parametric value t, decoded through the fifth and sixth
				// /Decode numbers.
				if v, ok := s.numbers(ctx, dict["Decode"]); ok && len(v) >= 6 {
					domain = []float64{min(v[4], v[5]), max(v[4], v[5])}
				}
			}
			fn, err := s.resampleFunction(ctx, dict["Function"], domain, p, 0)
			if err != nil {
				return nil, false, err
			}
			out["Function"] = fn
		case kind >= 4 && kind <= 7 && isStream:
			converted, decode, err := s.convertMesh(ctx, int(kind), dict, stream, p)
			if err != nil {
				return nil, false, err
			}
			data = converted
			out["BitsPerComponent"] = ast.PdfInt(meshComponentBits)
			out["Decode"] = decode
		default:
			return nil, false, fmt.Errorf("%w: shading type %d without the data its colour comes from", ErrUnsupportedSpace, kind)
		}
		s.report.Shadings++
		if data != nil {
			ns, err := newStream(out, data)
			if err != nil {
				return nil, false, err
			}
			return ns, true, nil
		}
		return shadingOut(out, stream, isStream), true, nil
	})
}

// shadingOut returns the shading with dictionary dict: a stream keeps its
// data as it is.
func shadingOut(dict ast.PdfDict, stream ast.PdfStream, isStream bool) ast.PdfObject {
	if isStream {
		return ast.NewLazyStream(dict, stream.Bytes)
	}
	return dict
}

// componentArray writes target components as a PDF array.
func componentArray(v []float64) ast.PdfArray {
	out := make(ast.PdfArray, len(v))
	for i, x := range v {
		out[i] = number(roundComponent(x))
	}
	return out
}

// resampleFunction returns a function giving, over domain, the target colour
// of the source colour obj gives: a function, or an array of one-output
// functions, one per component (8.7.4.5, Table 78). A stitching function
// keeps its subdomains, so the colour stops it joins stay sharp, and each
// part is sampled on its own (7.10.4); any other is sampled whole
// ([session.sampledFunction]).
func (s *session) resampleFunction(ctx context.Context, obj ast.PdfObject, domain []float64, p *spacePlan, depth int) (ast.PdfObject, error) {
	resolved, err := s.resolve(ctx, obj)
	if err != nil {
		return nil, err
	}
	if d, ok := s.functionDict(resolved); ok && depth < maxStitchDepth && len(domain) == 2 {
		if kind, _ := document.ResolveInt(ctx, s.doc, d["FunctionType"]); kind == 3 {
			return s.resampleStitching(ctx, d, p, depth)
		}
	}
	var fns []*function.Function
	if arr, ok := resolved.(ast.PdfArray); ok {
		for _, o := range arr {
			fn, err := function.New(ctx, s.doc, o)
			if err != nil {
				return nil, fmt.Errorf("shading function: %w", err)
			}
			fns = append(fns, fn)
		}
	} else {
		fn, err := function.New(ctx, s.doc, obj)
		if err != nil {
			return nil, fmt.Errorf("shading function: %w", err)
		}
		fns = []*function.Function{fn}
	}
	outputs := 0
	for _, fn := range fns {
		if fn.Inputs() != len(domain)/2 {
			return nil, fmt.Errorf("%w: a shading function of %d inputs over a domain of %d", ErrUnsupportedSpace, fn.Inputs(), len(domain)/2)
		}
		outputs += fn.Outputs()
	}
	if outputs != p.n {
		return nil, fmt.Errorf("%w: shading functions give %d outputs for %d components", ErrUnsupportedSpace, outputs, p.n)
	}
	src := make([]float64, p.n)
	ref, err := s.sampledFunction(ctx, domain, s.t.n, func(in, out []float64) {
		src = src[:0]
		for _, fn := range fns {
			v, err := fn.Eval(in)
			if err != nil || len(v) != fn.Outputs() {
				// A failed evaluation gives the low end of each output.
				v = make([]float64, fn.Outputs())
			}
			src = append(src, v...)
		}
		p.conv.Convert(src, out)
	})
	if err != nil {
		return nil, err
	}
	return ref, nil
}

// functionDict returns the dictionary of a function object.
func (s *session) functionDict(obj ast.PdfObject) (ast.PdfDict, bool) {
	switch v := obj.(type) {
	case ast.PdfDict:
		return v, true
	case ast.PdfStream:
		return v.Dict, true
	}
	return nil, false
}

// resampleStitching rebuilds a type 3 function (7.10.4) with the same
// domain, bounds and encodings and each part resampled into the target over
// the interval its encoding hands it.
func (s *session) resampleStitching(ctx context.Context, d ast.PdfDict, p *spacePlan, depth int) (ast.PdfObject, error) {
	parts, err := s.resolveArray(ctx, d["Functions"])
	if err != nil {
		return nil, fmt.Errorf("%w: stitching /Functions: %w", ErrUnsupportedSpace, err)
	}
	encode, ok := s.numbers(ctx, d["Encode"])
	if !ok || len(encode) != 2*len(parts) {
		return nil, fmt.Errorf("%w: stitching /Encode", ErrUnsupportedSpace)
	}
	// Compiling the whole function checks its structure and bounds.
	if _, err := function.New(ctx, s.doc, d); err != nil {
		return nil, fmt.Errorf("shading function: %w", err)
	}
	out := make(ast.PdfArray, len(parts))
	for i, part := range parts {
		lo, hi := min(encode[2*i], encode[2*i+1]), max(encode[2*i], encode[2*i+1])
		if lo == hi {
			hi = lo + 1
		}
		fn, err := s.resampleFunction(ctx, part, []float64{lo, hi}, p, depth+1)
		if err != nil {
			return nil, err
		}
		out[i] = fn
	}
	nd := ast.PdfDict{
		"FunctionType": ast.PdfInt(3),
		"Domain":       d["Domain"],
		"Bounds":       d["Bounds"],
		"Encode":       d["Encode"],
		"Functions":    out,
	}
	return nd, nil
}
