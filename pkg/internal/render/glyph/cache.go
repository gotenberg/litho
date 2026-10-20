package glyph

import (
	"context"
	"fmt"
	"sync"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// Cache loads the fonts of one document once each, keyed by the object
// number of their dictionary, so a page that selects a font a thousand times
// parses its program once. The programs the cached fonts hold count against
// the document's font program budget, [limits.Limits.MaxFontProgCacheBytes]:
// a load that would pass it empties the cache first, and a font whose program
// alone passes it is loaded for each request and never kept.
//
// A Cache is safe for concurrent use.
type Cache struct {
	doc  *document.Document
	opts Options

	mu    sync.Mutex
	fonts map[int]cacheEntry
	bytes int64
}

// cacheEntry is a loaded font, or the error loading it gave, which is cached
// too so a broken font is not reread at every Tf.
type cacheEntry struct {
	font *Font
	err  error
}

// NewCache returns an empty cache over doc.
func NewCache(doc *document.Document, opts Options) *Cache {
	return &Cache{doc: doc, opts: opts, fonts: make(map[int]cacheEntry)}
}

// Font returns the font fontObj, an indirect reference to a font dictionary
// or a direct one, loaded by [Load]. A direct dictionary has no object number
// to key on and is loaded anew.
func (c *Cache) Font(ctx context.Context, fontObj ast.PdfObject) (*Font, error) {
	ref, indirect := fontObj.(ast.PdfIndirectRef)
	if !indirect {
		return Load(ctx, c.doc, fontObj, c.opts)
	}
	c.mu.Lock()
	entry, ok := c.fonts[ref.ObjectNum]
	c.mu.Unlock()
	if ok {
		return entry.font, entry.err
	}
	font, err := Load(ctx, c.doc, fontObj, c.opts)
	if err != nil && ctx.Err() != nil {
		// A cancelled load says nothing about the font.
		return nil, err
	}
	size := int64(0)
	if font != nil {
		size = int64(font.size())
	}
	limit := c.doc.Limits().MaxFontProgCacheBytes()
	if size > limit {
		return font, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if existing, ok := c.fonts[ref.ObjectNum]; ok {
		// Another goroutine loaded it meanwhile; keep one copy.
		return existing.font, existing.err
	}
	if c.bytes+size > limit {
		c.fonts = make(map[int]cacheEntry)
		c.bytes = 0
	}
	c.fonts[ref.ObjectNum] = cacheEntry{font: font, err: err}
	c.bytes += size
	return font, err
}

// Resource returns the font a /Font resource dictionary names name, the
// operand of a Tf operator (ISO 32000-1, 9.3.1).
func (c *Cache) Resource(ctx context.Context, resources ast.PdfDict, name string) (*Font, error) {
	fonts, err := document.ResolveDict(ctx, c.doc, resources["Font"])
	if err != nil {
		return nil, fmt.Errorf("resolving /Font resources: %w", err)
	}
	obj, ok := fonts[ast.PdfName(name)]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrFontNotFound, name)
	}
	return c.Font(ctx, obj)
}
