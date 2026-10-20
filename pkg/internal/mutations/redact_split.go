package mutations

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// tjNumberPrecision is how many decimal places a TJ number written in place of
// removed glyphs keeps. A TJ number is in thousandths of text space units
// (ISO 32000-1, 9.4.3), so the rounding moves a kept glyph by at most half a
// millionth of an em: no reader can show it, and the shorter number keeps the
// output byte-stable across platforms.
const tjNumberPrecision = 1000

// splitShow is what [splitShowReplacement] returns: the bytes that replace one
// show-text operator whose glyphs a region only partly covers, and the text of
// each removed run in drawing order.
type splitShow struct {
	replacement []byte
	removedText []string
}

// splitShowReplacement rewrites the show-text operator ops[i] so that the
// glyphs picked marks (one entry per emitted glyph, in drawing order) are gone
// and the others stay exactly where they were.
//
// The operator becomes a TJ. Per ISO 32000-1, 9.4.3 a TJ string shows its
// glyphs as Tj does and a TJ number moves the text matrix by -n/1000 x Tfs x
// Th, or by -n/1000 x Tfs in vertical writing, painting nothing. Each maximal
// run of removed glyphs, together with every TJ number of the original array
// that touches it, becomes one number equal to their combined displacement, so
// the next kept glyph starts where it started before. A glyph's displacement
// (9.4.4) already holds its character and word spacing, both scaled by Th, so
// Tc, Tw and Tz keep applying to the kept glyphs unchanged. ' and " keep their
// line move and their spacing as the T*, Tw and Tc they stand for (9.4.2), as
// [removedShowReplacement] puts them back.
//
// Folding the touching numbers in is what answers the glyph-advance leak
// (arXiv 2206.02285): a kerning number between two glyphs depends on the pair,
// so a number kept beside a removed glyph would help spell it. Only the total
// width of each removed run survives, and the position of the next kept glyph
// shows that width anyway. A removed run at the end of the operator is dropped
// with no number when no later text depends on the pen
// ([laterTextDependsOnPen]), so nothing records its width at all.
//
// It refuses with [ErrPartialOperatorCoverage] whenever it cannot place the
// kept glyphs exactly:
//
//   - a code the extractor did not split where the font's encoding does, or
//     consumed without emitting a glyph, so its boundaries or its advance are
//     not what a viewer reads;
//   - a text rendering mode that adds the glyphs to the clipping path (modes 4
//     to 7, 9.3.6, Table 106), because removing a glyph from that path
//     changes what later content shows outside the region, as a clipping path
//     does for [ErrRegionCoversAClippedPath];
//   - a TJ number, in the original or in the rewrite, under a horizontal
//     scaling other than 100: the extractor moves the pen by a TJ number
//     without Th, where 9.4.4 scales it by Th, so the glyph boxes after one
//     are not where a viewer draws them, and selection over the original or
//     verification over the rewrite cannot be trusted;
//   - a font size or scaling of zero, or a text matrix that cannot be
//     inverted, where no number reproduces an advance.
//
// # Corpus evidence
//
//   - govdocs, pdfcpu, pdfjs: redacting page 1 of each of 2344 files under a
//     whole-page, a top-third, a left-half and a mid-page band region, 1427
//     redactions refused for partial coverage before splitting and 27 after.
//     758 now succeed, and in each the extracted glyphs outside the region
//     keep their text and position, no glyph is left inside it, and qpdf
//     --check reports no error; the 642 others now meet a later refusal (the
//     structure tree, the verification scan, a shared image, an annotation
//     appearance).
//   - govdocs thread_300_300338.pdf: the one exception, a kept glyph whose
//     box started exactly on the region's edge, at 306.0000000000001 before
//     and 306 after, which an inclusive overlap test reads as touching.
//   - pdfjs, pdfcpu, govdocs: of the 27 refusals left, 18 are a TJ number
//     under a horizontal scaling other than 100 (issue9650.pdf,
//     JBIG2Globals.pdf, thread_300_300655.pdf), 5 a code split against the
//     encoding (issue9915_reduced.pdf, bug898853.pdf) and 4 clipping text
//     (issue7101.pdf, glyph_accent.pdf, bug1749563.pdf).
func splitShowReplacement(
	ctx context.Context,
	ops []content.Operation,
	i int,
	picked []bool,
	glyphTexts []string,
	removed map[int]bool,
	glyphs []graphics.GlyphAdvance,
	advance graphics.ShowAdvance,
) (splitShow, error) {
	op := ops[i]
	refuse := func(format string, args ...any) (splitShow, error) {
		return splitShow{}, fmt.Errorf("%w: operation %d %q: "+format,
			append([]any{ErrPartialOperatorCoverage, i, op.Operator}, args...)...)
	}

	var items ast.PdfArray
	switch op.Operator {
	case "Tj", "'":
		if len(op.Operands) < 1 {
			return refuse("has no string to split")
		}
		items = ast.PdfArray{op.Operands[0]}
	case "\"":
		if len(op.Operands) < 3 {
			return refuse("has %d operands", len(op.Operands))
		}
		items = ast.PdfArray{op.Operands[2]}
	case "TJ":
		if len(op.Operands) != 1 {
			return refuse("has %d operands", len(op.Operands))
		}
		arr, ok := op.Operands[0].(ast.PdfArray)
		if !ok {
			return refuse("shows no array")
		}
		items = arr
	default:
		return refuse("is not a show-text operator")
	}

	scale := advance.FontSize * advance.HorizontalScale
	if advance.Vertical {
		scale = advance.FontSize
	}
	if scale == 0 || math.IsNaN(scale) || math.IsInf(scale, 0) {
		return refuse("draws at a font size or scaling of zero")
	}

	emitted := 0
	for _, g := range glyphs {
		switch {
		case !g.Aligned:
			return refuse("splits a code where the font's encoding does not")
		case !g.Emitted:
			return refuse("shows a code no glyph was read for")
		case g.RenderingMode >= 4 && g.RenderingMode <= 7: //nolint:mnd // ISO 32000-1, 9.3.6, Table 106: modes 4 to 7 add to the clipping path
			return refuse("adds its glyphs to the clipping path (text rendering mode %d)", g.RenderingMode)
		}
		emitted++
	}
	if emitted != len(picked) || emitted != len(glyphTexts) {
		return refuse("drew %d glyphs, and %d were tallied", emitted, len(picked))
	}

	// One piece per glyph or TJ number, in drawing order, so runs can be
	// folded across string boundaries.
	type piece struct {
		code    []byte // a glyph's code, nil for a number
		number  ast.PdfObject
		removed bool
		n       float64 // the TJ number the piece displaces by
		text    string
	}
	pieces := make([]piece, 0, len(glyphs)+len(items))
	next := 0
	for _, item := range items {
		switch v := item.(type) {
		case ast.PdfString:
			data := []byte(v)
			for len(data) > 0 {
				if next >= len(glyphs) {
					return refuse("shows more codes than were recorded")
				}
				g := glyphs[next]
				if g.Bytes <= 0 || g.Bytes > len(data) {
					return refuse("records a code of %d bytes where %d remain", g.Bytes, len(data))
				}
				n, err := glyphTJNumber(g, advance)
				if err != nil {
					return refuse("%v", err)
				}
				pieces = append(pieces, piece{code: data[:g.Bytes], removed: picked[next], n: n, text: glyphTexts[next]})
				data = data[g.Bytes:]
				next++
			}
		case ast.PdfInt, ast.PdfReal:
			if advance.HorizontalScale != 1 && !advance.Vertical {
				return refuse("positions with a TJ number under a horizontal scaling of %g", advance.HorizontalScale*100)
			}
			pieces = append(pieces, piece{number: v, n: document.Float(v)})
		default:
			// Anything else in a TJ array shows nothing and moves nothing
			// (9.4.3 admits only strings and numbers); keep it as it was.
			pieces = append(pieces, piece{number: v})
		}
	}
	if next != len(glyphs) {
		return refuse("recorded %d codes and shows %d", len(glyphs), next)
	}

	// Mark the numbers that touch a removed glyph, directly or through other
	// numbers, as part of its run.
	for k := range pieces {
		if !pieces[k].removed || pieces[k].code == nil {
			continue
		}
		for j := k - 1; j >= 0 && pieces[j].code == nil; j-- {
			pieces[j].removed = true
		}
		for j := k + 1; j < len(pieces) && pieces[j].code == nil; j++ {
			pieces[j].removed = true
		}
	}

	var out ast.PdfArray
	var removedText []string
	var kept bytes.Buffer
	flushKept := func() {
		if kept.Len() > 0 {
			out = append(out, ast.PdfString(kept.String()))
			kept.Reset()
		}
	}
	for k := 0; k < len(pieces); {
		p := pieces[k]
		if !p.removed {
			if p.code != nil {
				kept.Write(p.code)
			} else {
				flushKept()
				out = append(out, p.number)
			}
			k++
			continue
		}
		flushKept()
		var sum float64
		var text strings.Builder
		for ; k < len(pieces) && pieces[k].removed; k++ {
			sum += pieces[k].n
			text.WriteString(pieces[k].text)
		}
		if run := text.String(); run != "" {
			removedText = append(removedText, run)
		}
		sum = math.Round(sum*tjNumberPrecision) / tjNumberPrecision
		trailing := k == len(pieces)
		if sum != 0 && (!trailing || laterTextDependsOnPen(ops, i, removed)) {
			out = append(out, ast.PdfReal(sum))
		}
	}
	flushKept()
	if advance.HorizontalScale != 1 && !advance.Vertical {
		for _, item := range out {
			if _, isString := item.(ast.PdfString); !isString {
				return refuse("would position with a TJ number under a horizontal scaling of %g", advance.HorizontalScale*100)
			}
		}
	}

	var b bytes.Buffer
	switch op.Operator {
	case "\"":
		for k, setter := range []string{"Tw", "Tc"} {
			if _, err := writer.Serialize(ctx, &b, op.Operands[k]); err != nil {
				return splitShow{}, fmt.Errorf("writing the %s operand of operation %d: %w", setter, i, err)
			}
			b.WriteString(" " + setter + " ")
		}
		b.WriteString("T* ")
	case "'":
		b.WriteString("T* ")
	}
	if _, err := writer.Serialize(ctx, &b, out); err != nil {
		return splitShow{}, fmt.Errorf("writing the split run of operation %d: %w", i, err)
	}
	b.WriteString(" TJ")
	// Spaces on both sides keep the replacement from fusing with the tokens
	// around it, whatever separated them in the original.
	return splitShow{replacement: []byte(" " + b.String() + " "), removedText: removedText}, nil
}

// glyphTJNumber returns the TJ number whose displacement equals the advance
// glyph made, as [compensatingTJ] does for a whole operator.
func glyphTJNumber(glyph graphics.GlyphAdvance, advance graphics.ShowAdvance) (float64, error) {
	a := advance
	a.Before, a.After = glyph.Before, glyph.After
	tx, ty, ok := a.TextSpaceAdvance()
	if !ok {
		return 0, fmt.Errorf("a glyph draws in a text matrix that cannot be inverted")
	}
	d, scale := tx, a.FontSize*a.HorizontalScale
	if a.Vertical {
		d, scale = ty, a.FontSize
	}
	n := -d * 1000 / scale
	if math.IsNaN(n) || math.IsInf(n, 0) {
		return 0, fmt.Errorf("a glyph advances %g at a font size or scaling of zero", d)
	}
	return n, nil
}
