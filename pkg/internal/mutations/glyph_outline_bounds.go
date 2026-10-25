package mutations

import (
	"context"
	"log/slog"
	"sync"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/render/geom"
	"github.com/gotenberg/litho/pkg/internal/render/glyph"
)

// maxGlyphOutlineBounds caps the glyph bounds one [GlyphOutlines] keeps.
// Anti-DoS guard, not spec-mandated: a page shows a few hundred distinct
// glyphs, a CJK document a few thousand, so 65,536 entries (a few MB) hold
// every glyph of a real document, while a crafted one showing every code of
// many four-byte CMaps empties the cache and reads its glyphs again rather
// than growing it.
const maxGlyphOutlineBounds = 1 << 16

// GlyphOutlines bounds glyphs by their outlines for a redaction's selection
// (see [RedactOptions.GlyphOutlines]): it loads each font once through the
// renderer's glyph loader, so the outline it bounds is the one litho draws
// for the code, and keeps each glyph's bounds.
//
// A GlyphOutlines is safe for concurrent use.
type GlyphOutlines struct {
	doc   *document.Document
	fonts *glyph.Cache

	mu     sync.Mutex
	bounds map[glyphOutlineKey]glyphOutlineBounds
}

// glyphOutlineKey names one glyph: the loaded font and the bytes of the code.
type glyphOutlineKey struct {
	font *glyph.Font
	code string
}

// glyphOutlineBounds is what [GlyphOutlines.Bounds] found for a glyph.
type glyphOutlineBounds struct {
	box [4]float64
	ok  bool
}

// NewGlyphOutlines returns an empty [GlyphOutlines] over doc, which draws the
// fonts doc does not embed with the programs substitutes supplies, as the
// renderer does ([glyph.Options.Substitutes]); nil supplies none.
func NewGlyphOutlines(doc *document.Document, substitutes glyph.Provider) *GlyphOutlines {
	return &GlyphOutlines{
		doc:    doc,
		fonts:  glyph.NewCache(doc, glyph.Options{Substitutes: substitutes}),
		bounds: make(map[glyphOutlineKey]glyphOutlineBounds),
	}
}

// install makes machine's text extraction bound glyphs by their outlines
// through o ([graphics.Machine.GlyphOutlineBounds]). A nil o installs
// nothing, and the machine bounds glyphs by their advance boxes.
func (o *GlyphOutlines) install(machine *graphics.Machine) {
	if o != nil {
		machine.GlyphOutlineBounds = o.Bounds
	}
}

// Bounds is a [graphics.GlyphBoundsFunc]: the bounds, in text space at a
// font size of 1, of the outline of the glyph code selects in the font
// resources names font.
//
// A glyph is bounded by the program the document embeds (ISO 32000-1, 9.9),
// or by the substitute the provider supplies for a font it does not embed: a
// viewer draws such a font with a program of its own choosing (9.6.2.2),
// whose outline no reading of the file states, so the substitute the caller
// renders with is the reading there is. A font with no substitute draws
// placeholder boxes, and a glyph the program cannot draw, or one that paints
// nothing, has no outline to bound. Each of those keeps its advance box. A Type 3 glyph is bounded by
// what its description paints, before this is asked.
//
// The bounds are those of the outline's points, Bézier control points
// included: a curve lies inside the hull of its control points, so the box
// holds the curve and reaches past it by at most a control point's overshoot.
// The outline of a glyph in vertical writing mode is placed with its origin
// moved by its position vector (9.7.4.3), as the renderer draws it.
func (o *GlyphOutlines) Bounds(ctx context.Context, resources ast.PdfDict, font string, code []byte) ([4]float64, bool) {
	if o == nil || len(code) == 0 || resources == nil {
		return [4]float64{}, false
	}
	f, err := o.fonts.Resource(ctx, resources, font)
	if err != nil || f == nil {
		// The text extractor resolves the font through the same resources
		// and draws a stand-in for one it cannot read; that glyph keeps its
		// advance box, as it does without outline bounds.
		o.doc.Logger.Debug("redaction: loading a font to bound its glyph outlines", slog.String("font", font), slog.Any("err", err))
		return [4]float64{}, false
	}
	key := glyphOutlineKey{font: f, code: string(code)}
	o.mu.Lock()
	cached, hit := o.bounds[key]
	o.mu.Unlock()
	if hit {
		return cached.box, cached.ok
	}

	var value uint32
	for _, b := range code {
		value = value<<8 | uint32(b)
	}
	g, err := f.Glyph(ctx, glyph.Code{Bytes: code, Value: value})
	if err != nil {
		// Glyph fails only on a done context, which says nothing about the
		// glyph: nothing is cached.
		return [4]float64{}, false
	}
	found := glyphOutlineBounds{}
	if (g.Source == glyph.SourceEmbedded || g.Source == glyph.SourceSubstitute) && !g.Unreadable && !g.Outline.Empty() {
		m := g.Matrix.Mul(geom.Matrix{1, 0, 0, 1, -g.V.X, -g.V.Y})
		if r, ok := g.Outline.Transform(m).Bounds(); ok {
			found = glyphOutlineBounds{box: [4]float64{r.Min.X, r.Min.Y, r.Max.X, r.Max.Y}, ok: true}
		}
	}

	o.mu.Lock()
	if len(o.bounds) >= maxGlyphOutlineBounds {
		o.bounds = make(map[glyphOutlineKey]glyphOutlineBounds)
	}
	o.bounds[key] = found
	o.mu.Unlock()
	return found.box, found.ok
}
