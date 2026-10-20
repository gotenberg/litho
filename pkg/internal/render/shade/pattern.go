package shade

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/render/geom"
)

// Pattern is a compiled shading pattern, /PatternType 2 (ISO 32000-1,
// 8.7.4.2, Table 76), ready to fill any number of shapes.
//
// Values are safe for concurrent use after [NewPattern] returns.
type Pattern struct {
	shading   *Shading
	matrix    geom.Matrix
	extGState ast.PdfObject
}

// NewPattern compiles the pattern dictionary obj. components resolves the
// /ColorSpace of its shading, as for [New]. A tiling pattern (/PatternType
// 1) returns [ErrUnsupportedType]: painting one runs a content stream.
func NewPattern(ctx context.Context, doc *document.Document, obj ast.PdfObject, components ComponentsFunc) (*Pattern, error) {
	resolved, err := document.ResolveObject(ctx, doc, obj)
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
		return nil, fmt.Errorf("%w: a pattern is a dictionary, got %T", ErrMalformed, resolved)
	}
	kind, err := document.ResolveInt(ctx, doc, dict["PatternType"])
	if err != nil {
		return nil, fmt.Errorf("%w: no /PatternType: %w", ErrMalformed, err)
	}
	if kind != 2 {
		return nil, fmt.Errorf("%w: /PatternType %d", ErrUnsupportedType, kind)
	}
	shObj, present := dict["Shading"]
	if !present {
		return nil, fmt.Errorf("%w: a shading pattern requires /Shading", ErrMalformed)
	}
	sh, err := New(ctx, doc, shObj, components)
	if err != nil {
		return nil, fmt.Errorf("compiling pattern shading: %w", err)
	}
	pat := &Pattern{shading: sh, matrix: geom.Identity, extGState: dict["ExtGState"]}
	if m, present := dict["Matrix"]; present {
		vals, err := numbers(ctx, doc, m)
		if err != nil || len(vals) != 6 {
			return nil, fmt.Errorf("%w: pattern /Matrix holds 6 numbers", ErrMalformed)
		}
		copy(pat.matrix[:], vals)
	}
	return pat, nil
}

// Shading returns the pattern's shading.
func (p *Pattern) Shading() *Shading { return p.shading }

// Matrix returns the pattern matrix, which maps pattern space to the
// default coordinate space of the pattern's parent content stream (8.7.3.1).
func (p *Pattern) Matrix() geom.Matrix { return p.matrix }

// ExtGState returns the pattern's /ExtGState entry, nil when absent. The
// integrator applies it; [Pattern.Fill] does not read it.
func (p *Pattern) ExtGState() ast.PdfObject { return p.extGState }

// Fill paints the pattern as the fill of a shape: t.Clip carries the
// shape's coverage, already intersected with the clip, and baseToDevice
// maps the default coordinate space of the pattern's parent content stream
// to device space, so the pattern matrix applies first (8.7.3.1).
//
// Unlike [Shading.Paint], a fill paints /Background wherever the coverage
// reaches but the shading's geometry does not (Table 78).
func (p *Pattern) Fill(ctx context.Context, t Target, baseToDevice geom.Matrix) error {
	return p.shading.paint(ctx, t, p.matrix.Mul(baseToDevice), true)
}
