package mutations

import (
	"bytes"
	"context"
	"fmt"
	"math"

	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// removedShowReplacement returns the bytes that take the place of the
// show-text operator at ops[i] once its text is removed.
//
// Removing the operator also removes what it did besides painting glyphs. Per
// ISO 32000-1, 9.4.2 ' moves to the next line before it shows its string, and
// " also sets the word and character spacing, so those effects are put back as
// T*, Tw and Tc: every later line would otherwise start in the wrong place.
// And per 9.4.4 every show operator advances the text matrix by the width of
// what it drew, which is what positions the next glyph on the same line. When
// later text depends on that advance, a TJ holding a single number moves the
// text matrix by the same amount and paints nothing (9.4.3), so the rest of the
// line stays where it was. Without it the next run slid left into the cleared
// area, which changes the page outside the region.
//
// The advance is written only when later text depends on it. The position of
// that text already shows where the removed run ended, so the number adds
// nothing it does not; with nothing after it on the line, the number would be
// the only record of the removed run's width.
func removedShowReplacement(
	ctx context.Context,
	ops []content.Operation,
	i int,
	removed map[int]bool,
	advances map[int]graphics.ShowAdvance,
) ([]byte, error) {
	op := ops[i]
	var b bytes.Buffer

	switch op.Operator {
	case "\"":
		// aw ac string " is aw Tw ac Tc string '.
		if len(op.Operands) < 3 {
			return nil, fmt.Errorf("%w: operation %d is %q with %d operands", ErrTextAdvanceUnknown, i, op.Operator, len(op.Operands))
		}
		for k, setter := range []string{"Tw", "Tc"} {
			if _, err := writer.Serialize(ctx, &b, op.Operands[k]); err != nil {
				return nil, fmt.Errorf("writing the %s operand of operation %d: %w", setter, i, err)
			}
			b.WriteString(" " + setter + " ")
		}
		b.WriteString("T*")
	case "'":
		b.WriteString("T*")
	}

	if laterTextDependsOnPen(ops, i, removed) {
		n, err := compensatingTJ(i, advances)
		if err != nil {
			return nil, err
		}
		if n != 0 {
			if b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteString("[" + writer.FormatReal(n) + "] TJ")
		}
	}

	if b.Len() == 0 {
		return nil, nil
	}
	// Spaces on both sides keep the replacement from fusing with the tokens
	// around it, whatever separated them in the original.
	return []byte(" " + b.String() + " "), nil
}

// laterTextDependsOnPen reports whether a show-text operator after ops[i], in
// the same text object, is positioned by the advance ops[i] made.
//
// Only Tj and TJ start where the previous glyph ended. Td, TD, Tm, T*, ' and "
// all position from the line matrix (ISO 32000-1, 9.4.2), which a show operator
// never moves, and ET ends the text object. A later operator that is itself
// being removed does not end the chain: its own advance is put back only when
// something after it depends on it, and that something depends on this one
// too.
func laterTextDependsOnPen(ops []content.Operation, i int, removed map[int]bool) bool {
	for j := i + 1; j < len(ops); j++ {
		switch ops[j].Operator {
		case "ET", "BT", "Td", "TD", "Tm", "T*", "'", "\"":
			return false
		case "Tj", "TJ":
			if !removed[j] {
				return true
			}
		}
	}
	return false
}

// compensatingTJ returns the TJ number whose displacement equals the advance
// recorded for operation i. Per ISO 32000-1, 9.4.3 a TJ number n moves the
// text matrix by -n/1000 x Tfs x Th horizontally, or by -n/1000 x Tfs in
// vertical writing.
func compensatingTJ(i int, advances map[int]graphics.ShowAdvance) (float64, error) {
	a, ok := advances[i]
	if !ok {
		return 0, fmt.Errorf("%w: operation %d has no recorded advance", ErrTextAdvanceUnknown, i)
	}
	tx, ty, ok := a.TextSpaceAdvance()
	if !ok {
		return 0, fmt.Errorf("%w: operation %d draws in a text matrix that cannot be inverted", ErrTextAdvanceUnknown, i)
	}
	advance, scale := tx, a.FontSize*a.HorizontalScale
	if a.Vertical {
		advance, scale = ty, a.FontSize
	}
	if advance == 0 {
		return 0, nil
	}
	n := -advance * 1000 / scale
	if scale == 0 || math.IsNaN(n) || math.IsInf(n, 0) {
		return 0, fmt.Errorf("%w: operation %d advances %g at a font size or scale of zero", ErrTextAdvanceUnknown, i, advance)
	}
	return n, nil
}
