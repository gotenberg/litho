package compose

import "github.com/gotenberg/litho/pkg/internal/limits"

// maxPages caps the pages of one document at the ceiling of
// Limits.MaxConvertPages, [limits.MaxConvertPagesCeiling]: a converter holds
// its pages to the caller's budget, and this bounds what any model can make
// Write allocate before any content. Anti-DoS guard, not spec-mandated: ISO
// 32000-1 sets no page count, and every page costs a page object, a content
// stream and a /Kids entry.
const maxPages = limits.MaxConvertPagesCeiling

// maxFragmentsPerPage caps the fragments of one page, nested group content
// and links included. Anti-DoS guard, not spec-mandated. A dense page set as
// one run per word holds a few thousand fragments, and a page of 100k glyphs
// drawn one glyph per run still fits ten times over.
const maxFragmentsPerPage = 1_000_000

// maxGlyphsPerRun caps the glyphs of one [Text] run. Anti-DoS guard, not
// spec-mandated. A run is a line or a paragraph in practice; a page of 100k
// glyphs written as a single run still fits ten times over.
const maxGlyphsPerRun = 1_000_000

// maxOpsPerPath caps the operators of one [Path] or clip. Anti-DoS guard,
// not spec-mandated. A detailed map outline or a chart polyline holds tens
// of thousands of segments; a million leaves room for generated art.
const maxOpsPerPath = 1_000_000

// maxDashEntries caps the dash array of one [Stroke]. Anti-DoS guard, not
// spec-mandated: ISO 32000-1, 8.4.3.6 sets no length, and real dash patterns
// hold two to six entries.
const maxDashEntries = 1024

// maxGroupDepth caps the nesting of [Group] fragments at
// [limits.MaxGroupDepth], which says why. Real layouts nest a handful of
// levels.
const maxGroupDepth = limits.MaxGroupDepth

// maxOutlineEntries caps the entries of the outline, at every level.
// Anti-DoS guard, not spec-mandated. A 2000-page report with a heading per
// half page holds a few thousand entries.
const maxOutlineEntries = 100_000

// MaxStructElems caps the elements of the structure tree: [Document.Validate]
// refuses a tree with more. Anti-DoS guard, not spec-mandated. A 2000-page report tagged to the span holds a few
// hundred thousand elements at most.
const MaxStructElems = 1_000_000

// maxTreeDepth caps the depth of the outline and the structure tree, whose
// walks recurse. Anti-DoS guard protecting litho's own stack, not
// spec-mandated: ISO 32000-1, 12.3.3 and 14.7.2 set no depth, and real
// documents nest under twenty levels.
const maxTreeDepth = 256

// maxImagePixels caps the pixels of one image, width times height, at the
// ceiling of Limits.MaxImagePixels, [limits.MaxImagePixelsCeiling]: a
// converter holds each picture to the caller's budget, and this bounds what
// any model hands Write. Anti-DoS guard, not spec-mandated: [EncodeImage]
// reads a decoded picture of up to 4 bytes a pixel and [ImageSource.Decode]
// makes one.
const maxImagePixels = limits.MaxImagePixelsCeiling

// maxPageSize is the largest page side in points: ISO 32000-1, Annex C.2,
// Table C.1 bounds the page size at 14400 units (200 inches) in either
// dimension. It is the implementation limit, not a litho guard.
const maxPageSize = 14400

// maxShadingStops caps the stops of one [Shading]. Anti-DoS guard, not
// spec-mandated: ISO 32000-1, 7.10.4 sets no bound on a stitching
// function's subfunctions, and Write emits one per pair of stops. litho's
// own renderer compiles at most 1024 functions for one function object, so
// a shading of 1024 stops, 1023 segments under the stitching function,
// still renders with it. A CSS gradient holds a handful of stops, and a
// repeating one unrolled over its box a few hundred.
const maxShadingStops = 1024
