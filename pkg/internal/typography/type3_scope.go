package typography

import (
	"context"
	"log/slog"
	"reflect"
	"sync"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/typography/encodings"
)

// maxType3ScopeEntries caps the bounds one Type 3 font caches by the
// resources its glyphs were shown against ([type3ScopeCache]). Anti-DoS
// guard, not spec-mandated: each entry holds a resource dictionary alive, and
// a file whose every page states its own resources would otherwise grow the
// cache with its page count. Past the cap a glyph is bounded again at every
// show, which costs time and no correctness. A real document shows a font
// against a few resource dictionaries, one per page or form at most, and a
// few dozen glyphs, so the cap is far above what it meets.
const maxType3ScopeEntries = 4096

// type3ScopeKey names one glyph bounded against one resource dictionary:
// the glyph's name, and the identity of the map the dictionary is.
type type3ScopeKey struct {
	glyph     string
	resources uintptr
}

// type3ScopeEntry is a cached bound and the dictionary it was found
// against, held so that the map its key names cannot be collected and its
// address reused by another dictionary while the entry stands.
type type3ScopeEntry struct {
	resources ast.PdfDict
	ink       type3InkBounds
}

// type3ScopeCache holds the bounds [Font.type3ScopedInk] found, by glyph and
// resources. It is safe for concurrent use: a Font is shared by every reader
// of the document's font cache.
type type3ScopeCache struct {
	mu      sync.Mutex
	entries map[type3ScopeKey]type3ScopeEntry
}

// type3ScopedInk returns the ink, in glyph space, of the Type 3 glyph code
// selects, bounded against resources, the resources of the content stream
// that shows it, page, form or glyph description. ISO 32000-1, 9.6.5,
// Table 112 makes the font's /Resources optional and has a glyph
// description without them take the resources of the page it is shown on,
// a PDF 1.1 practice; the renderer draws such a glyph against the
// resources of the stream that shows it (render/page, renderer.type3Glyph),
// so the glyph is bounded against those. It reports false when code names no
// glyph [resolveType3Font] left to bound here, or the glyph cannot be
// bounded again: no resources, a font already being bounded this way below
// (a glyph showing itself through the resources), or one more descent past
// [maxType3InkDepth]. The bound may still be unbounded, as for a shading
// over a clip the description never set.
//
// The bound is cached per glyph and resource dictionary, so the glyphs of a
// page cost one walk each however often they are shown.
//
// # Corpus evidence
//
//   - All corpora: of the 133 files holding a Type 3 font, pdfjs pr4922.pdf
//     alone has a font stating no /Resources whose glyphs name resources.
//   - pdfjs pr4922.pdf: each d1 glyph shows text in a font only the page
//     names. Bounded by the page, the centred quarter of page 1 removed all
//     45 glyphs; it now removes the 26 under it and keeps "p20-40:p15" and
//     "Page 12", litho's and poppler's renderings unchanged outside it.
//   - All corpora: over the first ten pages of the other 132 files, no text
//     and no ink box moves.
//   - litho t3scope_2b.pdf: glyphs painting a form and showing text through
//     the page and through a form binding the names apart. The quarter and
//     the band refused over the form's glyphs, and now remove the page's
//     glyphs under them alone.
func (f *Font) type3ScopedInk(ctx context.Context, doc *document.Document, resources ast.PdfDict, code int) (type3InkBounds, bool) {
	if f.type3Scoped == nil || resources == nil || doc == nil || code < 0 || code > 255 {
		return type3InkBounds{}, false
	}
	r, ok := f.Encoding[byte(code)] //nolint:gosec // code bounds-checked above
	if !ok {
		return type3InkBounds{}, false
	}
	name := encodings.RuneToGlyph(r)
	stream, ok := f.type3Scoped[name]
	if !ok {
		return type3InkBounds{}, false
	}
	chain := type3ChainFrom(ctx)
	if chain.scoped[f] || chain.depth+1 >= maxType3InkDepth {
		return type3InkBounds{}, false
	}
	key := type3ScopeKey{glyph: name, resources: reflect.ValueOf(resources).Pointer()}
	cache := f.type3ScopeInk
	cache.mu.Lock()
	entry, hit := cache.entries[key]
	cache.mu.Unlock()
	if hit {
		return entry.ink, true
	}
	inner := context.WithValue(ctx, type3ChainKey{}, chain.descendScoped(f))
	g, err := extractType3Glyph(inner, doc, stream, type3Painter{doc: doc, resources: resources})
	if err != nil {
		doc.Logger.Debug("bounding Type3 glyph against its shower's resources", slog.String("glyph", name), slog.Any("err", err))
		return type3InkBounds{}, false
	}
	// A walk the context cancelled bounds nothing it did not see, and
	// reports so ([type3PaintBounds]); it is not cached.
	ink := type3GlyphInkBounds(g)
	if ctx.Err() != nil {
		return ink, true
	}
	cache.mu.Lock()
	if cache.entries == nil {
		cache.entries = make(map[type3ScopeKey]type3ScopeEntry)
	}
	if len(cache.entries) < maxType3ScopeEntries {
		cache.entries[key] = type3ScopeEntry{resources: resources, ink: ink}
	}
	cache.mu.Unlock()
	return ink, true
}
