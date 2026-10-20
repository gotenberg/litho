package page

import (
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/render/raster"
	"github.com/gotenberg/litho/pkg/internal/render/shade"
	"github.com/gotenberg/litho/pkg/internal/render/surface"
)

// The pattern types: a tiling pattern (ISO 32000-1, 8.7.3.1, Table 75) and a
// shading pattern (8.7.4.2, Table 76).
const (
	patternTypeTiling  = 1
	patternTypeShading = 2
)

// resource returns the entry name of the resource category kind, nil when
// the resources hold none.
func (r *renderer) resource(s *scope, kind, name ast.PdfName) ast.PdfObject {
	dict, err := document.ResolveDict(r.ctx, r.doc, s.resources[kind])
	if err != nil || dict == nil {
		return nil
	}
	return dict[name]
}

// shadingConverter returns the converter for a shading's colour space.
func (r *renderer) shadingConverter(s *scope, sh *shade.Shading) Converter {
	space, err := graphics.DescribeColorSpace(r.ctx, r.doc, sh.ColorSpace())
	if err != nil {
		r.skip("shading colour space", err)
		return greyConverter{}
	}
	return r.converter(s, space)
}

// shading runs sh (ISO 32000-1, 8.7.4.2): it paints the named shading over
// the current clip, in the fill alpha (11.6.4.4).
func (r *renderer) shading(s *scope, op content.Operation) error {
	st := r.m.Current()
	if st.SkipExtraction || len(op.Operands) != 1 || r.clip().empty() {
		return nil
	}
	name, ok := op.Operands[0].(ast.PdfName)
	if !ok {
		return nil
	}
	obj := r.resource(s, "Shading", name)
	if obj == nil {
		r.skip("shading", fmt.Errorf("no /Shading resource %s", name))
		return nil
	}
	sh, err := r.compileShading(obj)
	if err != nil {
		if fatal(err) {
			return err
		}
		r.skip("shading", err)
		return nil
	}
	r.noteEffects(st)
	mask, err := r.clip().mask1(r.canvas.Width, r.canvas.Height, r.budget)
	if err != nil {
		return err
	}
	t := shade.Target{Canvas: r.canvas, Clip: mask, Opacity: surface.NewOpacity(st.FillAlpha), Convert: r.shadingConverter(s, sh)}
	if err := sh.Paint(r.ctx, t, r.toDevice()); err != nil {
		if fatal(err) {
			return err
		}
		r.skip("shading", err)
	}
	return nil
}

// compileShading compiles a shading object, once per indirect object a
// render paints.
func (r *renderer) compileShading(obj ast.PdfObject) (*shade.Shading, error) {
	ref, isRef := obj.(ast.PdfIndirectRef)
	if isRef {
		if sh, ok := r.shadings[ref]; ok {
			return sh, nil
		}
	}
	sh, err := shade.New(r.ctx, r.doc, obj, r.components)
	if err != nil {
		return nil, fmt.Errorf("compiling shading: %w", err)
	}
	if isRef {
		r.shadings[ref] = sh
	}
	return sh, nil
}

// patternFill paints cov with the pattern colour c (ISO 32000-1, 8.7.3.1). A
// shading pattern paints its shading through the pattern matrix, which maps
// into the default space of the stream the pattern resource belongs to; a
// tiling pattern is counted and paints nothing yet.
func (r *renderer) patternFill(s *scope, cov *raster.Coverage, c graphics.Color, a float64) error {
	if c.Pattern == "" {
		// 8.6.8: a Pattern space with no pattern selected paints nothing.
		return nil
	}
	obj := r.resource(s, "Pattern", ast.PdfName(c.Pattern))
	if obj == nil {
		r.skip("pattern", fmt.Errorf("no /Pattern resource %s", c.Pattern))
		return nil
	}
	pat, err := r.compilePattern(obj)
	if err != nil {
		if fatal(err) {
			return err
		}
		r.skip("pattern", err)
		return nil
	}
	if pat == nil {
		r.report.count(UnsupportedTilingPattern)
		return nil
	}
	mask, err := surface.NewMask(r.canvas.Width, r.canvas.Height, r.budget)
	if err != nil {
		return fmt.Errorf("%w: pattern coverage: %w", ErrTooLarge, err)
	}
	for y := range cov.Height {
		row := (cov.Y + y) * mask.Width
		copy(mask.A[row+cov.X:row+cov.X+cov.Width], cov.A[y*cov.Width:(y+1)*cov.Width])
	}
	t := shade.Target{Canvas: r.canvas, Clip: mask, Opacity: surface.NewOpacity(a), Convert: r.shadingConverter(s, pat.Shading())}
	if err := pat.Fill(r.ctx, t, s.base); err != nil {
		if fatal(err) {
			return err
		}
		r.skip("pattern", err)
	}
	return nil
}

// compilePattern compiles a shading pattern, once per indirect object a
// render paints, and returns nil for a tiling pattern.
func (r *renderer) compilePattern(obj ast.PdfObject) (*shade.Pattern, error) {
	ref, isRef := obj.(ast.PdfIndirectRef)
	if isRef {
		if pat, ok := r.patterns[ref]; ok {
			return pat, nil
		}
	}
	resolved, err := document.ResolveObject(r.ctx, r.doc, obj)
	if err != nil {
		return nil, fmt.Errorf("resolving pattern: %w", err)
	}
	var dict ast.PdfDict
	switch v := resolved.(type) {
	case ast.PdfDict:
		dict = v
	case ast.PdfStream:
		dict = v.Dict
	default:
		return nil, fmt.Errorf("a pattern is a dictionary or a stream, got %T", resolved)
	}
	kind, err := document.ResolveInt(r.ctx, r.doc, dict["PatternType"])
	if err != nil {
		return nil, fmt.Errorf("pattern /PatternType: %w", err)
	}
	switch kind {
	case patternTypeTiling:
		return nil, nil //nolint:nilnil // nil marks a tiling pattern, which the caller counts
	case patternTypeShading:
	default:
		return nil, fmt.Errorf("/PatternType %d is neither 1 nor 2", kind)
	}
	pat, err := shade.NewPattern(r.ctx, r.doc, obj, r.components)
	if err != nil {
		return nil, fmt.Errorf("compiling pattern: %w", err)
	}
	if isRef {
		r.patterns[ref] = pat
	}
	return pat, nil
}
