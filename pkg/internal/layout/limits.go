package layout

// MaxBoxes bounds the boxes one [Layout] call generates; past it Layout
// fails with [ErrTooManyBoxes]. Anti-DoS guard, not spec-mandated: a box
// costs a few hundred bytes and some layout work, and a long book generates
// a few hundred thousand boxes at most, one per element and text run.
const MaxBoxes = 1 << 20

// maxTreeDepth bounds the nesting of the boxes the engine generates: a node
// deeper than this is left out with a warning. Recursion guard, not
// spec-mandated: box generation, layout and painting recurse once per
// level, and real documents nest a few dozen levels deep.
const maxTreeDepth = 512

// maxStructDepth bounds the depth of the structure tree: a role deeper than
// this does not start an element, its fragments hang from the deepest one.
// It stays below the composition model's own bound of 256 levels. Anti-DoS
// guard, not spec-mandated: a tagged document nests a dozen roles.
const maxStructDepth = 200

// maxLinesPerBlock bounds the line boxes of one block container: the text
// past it is left out with a warning. Anti-DoS guard, not spec-mandated:
// 100000 lines fill some two thousand pages, far more than one paragraph or
// pre block of a real document.
const maxLinesPerBlock = 100_000

// maxFloats bounds the floats one block formatting context tracks at once:
// a float past it is laid out where it stands, without excluding the
// content beside it, with a warning. Anti-DoS guard, not spec-mandated:
// placing a float scans the floats tracked, and pages hold a handful.
const maxFloats = 1024

// maxGlyphsPerLine bounds the glyphs one line box draws: the glyphs past it
// are left out with a warning. Anti-DoS guard, not spec-mandated: a line of
// text fills a page width with a few hundred glyphs, and a line that never
// wraps runs off the page anyway.
const maxGlyphsPerLine = 1 << 16

// maxRelayouts bounds the extra layout passes of one Layout call: the
// shrink-to-fit fitting of a box beside floats, the second pass of a float
// at its final place, and the passes that honour break-inside: avoid,
// orphans, widows and break-after: avoid. Past it the engine keeps the
// first pass with a warning. Anti-DoS guard, not spec-mandated: nested
// boxes that each avoid breaks would otherwise double the work per level.
const maxRelayouts = 10_000

// maxFitPasses bounds the attempts to fit one block formatting context root
// beside floats (CSS 2.1, 9.5) before it is placed below them. Anti-DoS
// guard, not spec-mandated: each attempt moves past one float, and a page
// rarely holds more than a few beside each other.
const maxFitPasses = 16

// maxFragmentationPasses bounds the passes one inline formatting context
// takes to honour orphans and widows. Anti-DoS guard, not spec-mandated:
// each pass fixes the page breaks the previous one found, and two settle
// every real paragraph.
const maxFragmentationPasses = 4

// maxBackgroundTiles bounds the tiles of one repeated background image:
// past it the image is drawn once, with a warning. Anti-DoS guard, not
// spec-mandated: a page-sized box tiled with a 4 by 4 point image takes
// some 6000 tiles.
const maxBackgroundTiles = 10_000

// maxGroupDepth bounds the groups, for opacity and clipping, nested on a
// page: deeper ones paint their content without the group, with a warning.
// It stays below the composition model's bound of 24, leaving room for the
// page group. Anti-DoS guard, not spec-mandated.
const maxGroupDepth = 20

// maxLength bounds every length the engine reads from a style, in points:
// a longer one acts as this one. Overflow guard, not spec-mandated: sums of
// such lengths stay far from overflow, and 2^20 points, 370 metres, is
// beyond any page.
const maxLength = 1 << 20

// maxPageFragments bounds the fragments, groups included, one page holds:
// the rest of the page is left out with a warning. It stays below the
// composition model's bound of a million. Anti-DoS guard, not
// spec-mandated: a dense page holds a few thousand.
const maxPageFragments = 500_000

// maxMarkerValue bounds the ordinal a list marker spells out: a larger one
// is clamped. Anti-DoS guard, not spec-mandated: the alphabetic counter
// styles spell a number in about log26(n) letters, and lists never run that
// long.
const maxMarkerValue = 1 << 30
