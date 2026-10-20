package typography

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// ResolveFontObject resolves the font dictionary obj, an indirect reference or
// a direct dictionary, into a [Font] through the document's font cache, as
// text extraction resolves a /Font resource: a reference is cached by its
// object number and shared with every other reader, a direct dictionary is
// resolved anew.
func ResolveFontObject(ctx context.Context, doc *document.Document, obj ast.PdfObject) (*Font, error) {
	ref, indirect := obj.(ast.PdfIndirectRef)
	if indirect {
		if cached, ok := doc.CachedFont(ref.ObjectNum); ok {
			if f, ok := cached.(*Font); ok {
				return f, nil
			}
		}
	}
	fontDict, err := document.ResolveDict(ctx, doc, obj)
	if err != nil {
		return nil, fmt.Errorf("resolving font dictionary: %w", err)
	}
	if fontDict == nil {
		return nil, ErrFontNotFound
	}
	font, err := ResolveFont(ctx, doc, fontDict)
	if err != nil {
		return nil, fmt.Errorf("resolving font: %w", err)
	}
	if indirect {
		doc.SetCachedFont(ref.ObjectNum, font)
	}
	return font, nil
}

// CodeWidth returns the horizontal advance of a character code, code being the
// bytes one code of a shown string takes, in thousandths of text space: the
// width text extraction advances the text matrix by ([advanceTextMatrix]),
// read at the key [Font.metricCID] gives a Type 0 font's code.
func (f *Font) CodeWidth(code []byte) float64 {
	return f.GlyphWidth(f.metricCID(code, codeValue(code)))
}

// CodeVerticalMetrics returns the vertical advance w1 and the position vector
// (vX, vY) of a character code in a vertical font, in thousandths of
// text space (ISO 32000-1, 9.7.4.3), read at the key text extraction reads
// them by.
func (f *Font) CodeVerticalMetrics(code []byte) (w1, vX, vY float64) {
	return f.VerticalMetrics(f.metricCID(code, codeValue(code)))
}

// codeValue returns the value text extraction reads a code at ([processText]):
// both bytes of a two-byte code, and the first byte of any other.
func codeValue(code []byte) int {
	switch len(code) {
	case 0:
		return 0
	case 2:
		return int(code[0])<<8 | int(code[1])
	default:
		return int(code[0])
	}
}

// Standard14StandIn returns the standard 14 font (ISO 32000-1, 9.6.2.2) whose
// metrics stand in for baseFont, as text extraction reads them: the name with
// its subset tag dropped, mapped through the metric-compatible families
// [stdFontMap] lists. ok is false when no standard font stands in for it.
func Standard14StandIn(baseFont string) (name string, ok bool) {
	name = normalizeBase14Name(baseFont)
	return name, isStandard14(name)
}
