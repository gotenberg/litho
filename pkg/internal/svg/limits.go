package svg

// maxSourceBytes caps the SVG text Parse reads. Anti-DoS guard, not
// spec-mandated: icons and charts weigh a few kilobytes, and drawings
// exported from illustration tools a few megabytes.
const maxSourceBytes = 16 << 20

// maxElements caps the elements of one document, the ones a use element
// instantiates counted again. Anti-DoS guard, not spec-mandated: a dense
// chart holds a few thousand elements.
const maxElements = 100_000

// maxDepth caps the nesting of elements the renderer walks, through use
// references too. Anti-DoS guard protecting litho's own stack, not
// spec-mandated: real drawings nest a few dozen groups.
const maxDepth = 256

// maxUseDepth caps the chain of use elements that reference one another.
// Anti-DoS guard, not spec-mandated: SVG 1.1, 5.6 forbids circular
// references, which this also stops, and real documents chain two or
// three.
const maxUseDepth = 16

// maxUseCount caps the use elements instantiated in one rendering, so that
// uses of uses cannot grow the drawing exponentially. Anti-DoS guard, not
// spec-mandated: a map of repeated symbols holds a few thousand.
const maxUseCount = 20_000

// maxPathSegments caps the commands one path data string yields, and the
// points of one polyline or polygon. Anti-DoS guard, not spec-mandated: a
// detailed map outline holds tens of thousands of segments.
const maxPathSegments = 200_000

// maxGradientStops caps the stops of one gradient. Anti-DoS guard, not
// spec-mandated: gradients hold a handful of stops, and the composition
// model refuses more than 1024.
const maxGradientStops = 256

// maxTextRunes caps the characters of the text elements of one rendering.
// Anti-DoS guard, not spec-mandated: SVG labels hold a few words, and a
// labelled chart a few thousand characters.
const maxTextRunes = 50_000

// maxBBoxSteps caps the elements one rendering visits to compute bounding
// boxes, for objectBoundingBox clips, so that nested uses cannot make it
// exponential. Anti-DoS guard, not spec-mandated: a clipped group holds a
// handful of shapes.
const maxBBoxSteps = 100_000

// maxFragments caps the fragments one rendering produces. Anti-DoS guard,
// not spec-mandated: each shape yields one or two, and a dense chart a few
// thousand.
const maxFragments = 200_000

// maxGroupNesting caps the groups for opacity and clipping nested in one
// rendering: deeper ones paint without their group, with a warning. The
// composition model bounds group nesting at 24 for the whole page, which
// the HTML layout's own groups share. Anti-DoS guard, not spec-mandated:
// drawings rarely nest translucent groups more than twice.
const maxGroupNesting = 3

// maxStyleRules caps the rules of the style sheets of one document.
// Anti-DoS guard, not spec-mandated: an SVG's style element holds a few
// dozen rules.
const maxStyleRules = 10_000

// maxMatchSteps caps the selector tests of one document's cascade.
// Anti-DoS guard, not spec-mandated: elements times rules stays far under
// it for real documents.
const maxMatchSteps = 1 << 24

// maxDashes caps the entries of a stroke-dasharray. Anti-DoS guard, not
// spec-mandated: dash patterns hold two to six entries, and the
// composition model refuses more than 1024.
const maxDashes = 64

// maxCoordinate bounds every coordinate and length the renderer reads, in
// user units: a larger magnitude is clamped. Overflow guard, not
// spec-mandated: sums and products of such values stay finite, and 2^24
// pixels is far beyond any page.
const maxCoordinate = 1 << 24

// maxDrawCache caps the renderings a [Document] keeps, one per viewport
// size its content depends on. Anti-DoS guard, not spec-mandated: a
// picture is drawn at a handful of sizes in one document.
const maxDrawCache = 16

// maxWarnings caps the warnings of one document; the rest are dropped.
// Anti-DoS guard, not spec-mandated.
const maxWarnings = 64
