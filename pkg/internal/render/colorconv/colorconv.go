package colorconv

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/icc"
	"github.com/gotenberg/litho/pkg/internal/graphics/image"
)

// Converter converts the components of a colour in one colour space to sRGB
// channels in [0, 1]. It has the shape of the shading painter's converter,
// so a Converter serves a shading as it serves a fill.
type Converter interface {
	RGB(components []float64) (r, g, b float32)
}

// Options are what a colour's conversion depends on beyond its space and the
// resources in force.
type Options struct {
	// Intent is the rendering intent's PDF name, as the ri operator or an
	// ExtGState /RI sets it (ISO 32000-1, 8.6.5.8). "" and an unrecognised
	// name select relative colorimetric, the graphics state's initial intent.
	Intent string
	// Page is the page dictionary, whose /OutputIntents replace the
	// document's for that page (ISO 32000-2, 14.11.5). It may be nil.
	Page ast.PdfDict
	// Cache, when not nil, keeps converters across calls, so a space
	// selected many times is read once. It serves one page: it keys on the
	// space, the intent and the default spaces of the resources, not on Page.
	Cache *Cache
}

// For returns the converter of the colour space space, painted with the
// resource dictionary resources, whose /DefaultGray, /DefaultRGB and
// /DefaultCMYK stand in for the device spaces (ISO 32000-1, 8.6.5.6): the
// page's for page content, a form's, pattern's or appearance's own for its
// content, a transparency group's for the group.
//
// A Pattern space converts the colour of an uncoloured pattern, in its base
// space (8.7.3.3); one without a base returns [ErrPattern]. A Separation or
// DeviceN space over the colorant None only returns [ErrPaintsNothing]. An
// unresolved space returns [ErrUnresolved], and one that cannot be converted
// an error wrapping [ErrUnsupported].
func For(ctx context.Context, doc *document.Document, space graphics.ColorSpace, resources ast.PdfDict, opts Options) (Converter, error) {
	switch space.Family {
	case "":
		return nil, fmt.Errorf("%w: resource %q", ErrUnresolved, space.Resource)
	case graphics.FamilyPattern:
		if space.Base == nil {
			return nil, ErrPattern
		}
		return For(ctx, doc, *space.Base, resources, opts)
	}
	obj := space.Object
	if obj == nil {
		obj = ast.PdfName(space.Family)
	}
	return ForObject(ctx, doc, obj, resources, opts)
}

// ForObject returns the converter of the colour space object obj, a family
// name or a colour space array as a shading's or an image's /ColorSpace
// holds it, under the same rules as [For]. A Pattern space returns
// [ErrPattern]: a shading or an image cannot be in one.
func ForObject(ctx context.Context, doc *document.Document, obj ast.PdfObject, resources ast.PdfDict, opts Options) (Converter, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if isPattern(ctx, doc, obj) {
		return nil, ErrPattern
	}
	intent := icc.ParseIntent(opts.Intent)
	key, cacheable := cacheKey(ctx, doc, obj, resources, intent)
	if cacheable && opts.Cache != nil {
		if e, ok := opts.Cache.get(key); ok {
			return e.conv, e.err
		}
	}
	conv, err := build(ctx, doc, obj, resources, opts.Page, intent)
	if cacheable && opts.Cache != nil && ctx.Err() == nil {
		opts.Cache.put(key, cacheEntry{conv: conv, err: err})
	}
	return conv, err
}

// build reads the space through the image decoder's colour reading.
func build(ctx context.Context, doc *document.Document, obj ast.PdfObject, resources, page ast.PdfDict, intent icc.Intent) (Converter, error) {
	sc, err := image.NewSampleColor(ctx, doc, obj, resources, page, intent)
	if err != nil {
		if errors.Is(err, image.ErrUnsupportedColorSpace) {
			return nil, fmt.Errorf("%w: %w", ErrUnsupported, err)
		}
		return nil, fmt.Errorf("reading colour space: %w", err)
	}
	if sc.PaintsNothing() {
		return nil, ErrPaintsNothing
	}
	return newConverter(doc.Logger, sc), nil
}

// isPattern reports a Pattern space, named or as an array.
func isPattern(ctx context.Context, doc *document.Document, obj ast.PdfObject) bool {
	resolved, err := document.ResolveObject(ctx, doc, obj)
	if err != nil {
		return false
	}
	switch v := resolved.(type) {
	case ast.PdfName:
		return v == graphics.FamilyPattern
	case ast.PdfArray:
		if len(v) == 0 {
			return false
		}
		family, err := document.ResolveName(ctx, doc, v[0])
		return err == nil && family == graphics.FamilyPattern
	}
	return false
}

// memoBits sizes the table a converter remembers colours in: 4096 slots, a
// few tens of kilobytes. Anti-DoS guard, not spec-mandated: the table is
// direct-mapped and never grows, so a page of a million distinct colours
// costs a million conversions and no memory, while the few hundred colours
// of a real page all fit.
const memoBits = 12

// memoSlot is one remembered colour: the quantised samples and their sRGB.
type memoSlot struct {
	key     string
	r, g, b float32
}

// converter converts colours through a [image.SampleColor], remembering the
// colour last converted in each slot of a direct-mapped table keyed on the
// quantised samples.
type converter struct {
	logger *slog.Logger
	mu     sync.Mutex
	sc     *image.SampleColor
	buf    []byte
	memo   []memoSlot
	failed bool
}

func newConverter(logger *slog.Logger, sc *image.SampleColor) *converter {
	return &converter{logger: logger, sc: sc, buf: make([]byte, sc.Components())}
}

// RGB converts one colour. Components beyond the space's count are ignored,
// missing ones read as the low end of their range, and every component is
// clipped to its range (ISO 32000-1, 8.6). A tint transform that fails to
// evaluate paints black, logged once at Debug.
func (c *converter) RGB(components []float64) (r, g, b float32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sc.Samples(components, c.buf)
	if c.memo == nil {
		c.memo = make([]memoSlot, 1<<memoBits)
	}
	// FNV-1a over the samples, folded to the table's size.
	h := uint64(14695981039346656037)
	for _, v := range c.buf {
		h = (h ^ uint64(v)) * 1099511628211
	}
	slot := &c.memo[h>>(64-memoBits)]
	if slot.key == string(c.buf) && slot.key != "" {
		return slot.r, slot.g, slot.b
	}
	px, err := c.sc.Convert(c.buf)
	if err != nil && !c.failed {
		c.failed = true
		c.logger.Debug("evaluating a tint transform for a colour; painting black", slog.Any("err", err))
	}
	*slot = memoSlot{
		key: string(c.buf),
		r:   float32(px.R) / 255,
		g:   float32(px.G) / 255,
		b:   float32(px.B) / 255,
	}
	return slot.r, slot.g, slot.b
}

// maxCachedSpaces bounds the converters a [Cache] keeps. Anti-DoS guard, not
// spec-mandated: a real page selects a handful of colour spaces, and past the
// bound a space is simply read again on each selection.
const maxCachedSpaces = 1024

// maxKeyBytes bounds the key a [Cache] stores a space under. Anti-DoS guard,
// not spec-mandated: a key spells out the space's definition, which for an
// inline colour table runs to 768 bytes, and a space whose key would pass the
// bound is not cached rather than stored at any size.
const maxKeyBytes = 4096

// Cache keeps the converters [For] and [ForObject] build, failures included,
// for one page. The zero value is ready to use, and a Cache is safe for
// concurrent use.
type Cache struct {
	mu sync.Mutex
	m  map[string]cacheEntry
}

type cacheEntry struct {
	conv Converter
	err  error
}

func (c *Cache) get(key string) (cacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[key]
	return e, ok
}

func (c *Cache) put(key string, e cacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = make(map[string]cacheEntry)
	}
	if len(c.m) < maxCachedSpaces {
		c.m[key] = e
	}
}

// cacheKey spells out what a conversion depends on: the intent, the space as
// written, and the default colour spaces of the resources, which may stand in
// for the space or its base. It reports false when the key passes
// [maxKeyBytes].
func cacheKey(ctx context.Context, doc *document.Document, obj ast.PdfObject, resources ast.PdfDict, intent icc.Intent) (string, bool) {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%d|%v", intent, obj)
	if resources != nil {
		spaces, err := document.ResolveDict(ctx, doc, resources["ColorSpace"])
		if err == nil && spaces != nil {
			for _, name := range []ast.PdfName{"DefaultGray", "DefaultRGB", "DefaultCMYK"} {
				if def, ok := spaces[name]; ok {
					fmt.Fprintf(&sb, "|%s=%v", name, def)
				}
			}
		}
	}
	return sb.String(), sb.Len() <= maxKeyBytes
}
