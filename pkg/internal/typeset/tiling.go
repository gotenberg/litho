package typeset

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// patternCost is what one tiling pattern costs the conversion's memory
// budget until the file is written: its dictionary, which waits for the
// end of the file with the other objects, and the name that selects it in
// its page's resources. The cell's content stream is charged apart, as a
// stream kept in memory.
//
// # Corpus evidence
//
//   - probe, html: a page of 2,000 paths, each filled with a pattern
//     whose cell is a gradient, holds 3,230 bytes of live heap a pattern
//     in the writer once the page is written, 488 of them in the streams
//     (TestPatternCostCoversAPattern); a cell with a translucent stop adds
//     its soft mask, charged at [maskCost].
const patternCost = 3 << 10

// tilingFill fills the path pa with its tiling pattern: a coloured tiling
// pattern of constant spacing (ISO 32000-1, 8.7.3.1, Table 75: PatternType
// 1, PaintType 1, TilingType 1) whose cell paints the pattern's content,
// selected as the fill colour in the Pattern colour space (8.6.6.2). A
// pattern's matrix maps its space to the default coordinates of the stream
// that paints it, not to the current ones (8.7.2), so it carries the
// transform in force: the cell lands where it would have under it. The
// pattern paints at the opacity of the fill (11.6.7).
func (p *pageWriter) tilingFill(ctx context.Context, pa *Path, opacity float64) error {
	t := pa.Fill.Tiling
	m := multiply(multiply(tilingMatrix(t), Matrix{A: 1, D: 1, E: p.off.X, F: p.off.Y}), p.local)
	ref, err := p.tilingPattern(ctx, t, m)
	if err != nil {
		return err
	}
	p.mark(pa.Parent, pa.Order)
	a := p.currentAlpha()
	a.fill = opacity
	p.setAlpha(a)
	p.patterns = append(p.patterns, xobjUse{name: fmt.Sprintf("P%d", len(p.patterns)+1), ref: ref})
	fmt.Fprintf(&p.buf, "/Pattern cs /%s scn\n", p.patterns[len(p.patterns)-1].name)
	if err := p.ops(ctx, pa.Ops); err != nil {
		return err
	}
	if pa.Fill.Rule == EvenOdd {
		p.buf.WriteString("f*\n")
	} else {
		p.buf.WriteString("f\n")
	}
	return nil
}

// tilingPattern writes the pattern of t placed by the matrix m and
// returns its reference: its cell's content is written as a stream of its
// own with its own resources, as an artifact, its bounding box the cell.
//
// The cell starts from full opacity and the normal blend mode without
// saying so: ISO 32000-1, 11.6.7 encloses the pattern definition in an
// implicit transparency group and states that "As always for transparency
// groups, those parameters related to transparency (blend mode, soft mask,
// and alpha constant) shall be initialized to their standard default
// values", whatever state the stream that paints it is in; the fill's own
// state composites the cell's result. A reset written at the head of the
// cell says nothing more, and a reader that runs a lone cell in the
// painting state lets it replace the fill's blend mode.
//
// # Corpus evidence
//
//   - litho/html backgrounds-layers: poppler 26.09 and 26.11 run the one
//     cell of a picture filled under Multiply in the current state, and a
//     written /BM /Normal at its head drew the picture unblended.
func (p *pageWriter) tilingPattern(ctx context.Context, t *Tiling, m Matrix) (ast.PdfIndirectRef, error) {
	if err := p.w.mem.ChargeUnits(1, patternCost, "patterns"); err != nil {
		return ast.PdfIndirectRef{}, fmt.Errorf("writing a pattern: %w", err)
	}
	p.w.charged += patternCost
	cw := newPageWriter(p.w, p.index, &Page{Width: t.Width, Height: t.Height})
	cw.steps, cw.bare = p.steps, true
	if err := cw.fragments(ctx, t.Content, 1); err != nil {
		return ast.PdfIndirectRef{}, err
	}
	p.steps = cw.steps
	data := cw.buf.Bytes()
	p.w.hashBytes(data)
	stream, err := p.w.flateStream(ast.PdfDict{
		"Type":        ast.PdfName("Pattern"),
		"PatternType": ast.PdfInt(1),
		"PaintType":   ast.PdfInt(1),
		"TilingType":  ast.PdfInt(1),
		"BBox":        ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), fine(t.Width), fine(t.Height)},
		"XStep":       fine(t.XStep),
		"YStep":       fine(t.YStep),
		"Matrix":      ast.PdfArray{fine(m.A), fine(m.B), fine(m.C), fine(m.D), fine(m.E), fine(m.F)},
		"Resources":   cw.resources(),
	}, data)
	if err != nil {
		return ast.PdfIndirectRef{}, fmt.Errorf("writing page %d: %w", p.index+1, err)
	}
	return p.w.alloc.Alloc(stream), nil
}
