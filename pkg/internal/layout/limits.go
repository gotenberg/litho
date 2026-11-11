package layout

import "github.com/gotenberg/litho/pkg/internal/limits"

// MaxBoxes bounds the boxes one [Layout] call generates; past it Layout
// fails with [ErrTooManyBoxes]. Anti-DoS guard, not spec-mandated: a box
// costs a few hundred bytes and some layout work, and a long book generates
// a few hundred thousand boxes at most, one per element and text run.
const MaxBoxes = 1 << 20

// boxCost is what one generated box costs the conversion's memory budget:
// the box, 584 bytes on a 64-bit platform, and its share of what the
// layout builds around it, its line boxes and the fragments it paints. The
// style it holds is charged apart, once whatever the number of boxes
// holding it ([styleCost]). A layout pass holds its boxes until the next
// pass or the end of the layout, which then release them.
//
// # Corpus evidence
//
//   - probe, html: at the peak of the layout of 1 MiB of paragraphs, the
//     boxes and the styles the engine copies for them hold 11 MiB of live
//     heap, the shaped paragraphs 64 MiB and the glyph runs 52 MiB.
//   - probe, html: 1 MiB of paragraphs, whose 7,672 boxes hold four
//     distinct styles: the Go heap peaks at 2.2 times the account's peak
//     at this cost, 2.6 times at 1 KiB a box.
const boxCost = 4 << 10

// styleCost is what one distinct style the boxes of a layout hold costs
// the conversion's memory budget: the [css.Style], 3,480 bytes on a 64-bit
// platform, and the entries that share it. Boxes styled alike hold one
// style, charged once.
const styleCost = 4 << 10

// paraRuneCost is what one character of a shaped paragraph costs the
// conversion's memory budget: its shaped glyphs, advances, breaks and
// levels, which the layout holds until the engine is dropped.
//
// # Corpus evidence
//
//   - probe, html: 1 MiB of paragraphs shapes into about 1 million
//     characters whose paragraphs hold 64 MiB of live heap, 64 bytes a
//     character.
const paraRuneCost = 64

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
// It is [limits.MaxSourceGroupDepth], which says why.
const maxGroupDepth = limits.MaxSourceGroupDepth

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

// maxTableColumns bounds the columns of one table: cells and columns past
// it are left out with a warning. It is the bound HTML puts on colspan.
// Anti-DoS guard, not spec-mandated: a page shows a few dozen columns.
const maxTableColumns = 1000

// maxTableRows bounds the rows of one table: rows past it are left out
// with a warning. It is the bound HTML puts on rowspan. Anti-DoS guard,
// not spec-mandated: 65534 rows fill over a thousand pages.
const maxTableRows = 65_534

// maxTableCells bounds the cells of one table: cells past it are left out
// with a warning. Anti-DoS guard, not spec-mandated: the largest tables of
// real documents, reports of tens of thousands of rows, hold a few hundred
// thousand cells, and every cell is a box counted against [MaxBoxes] too.
const maxTableCells = 1 << 19

// maxTableSlots bounds the grid of one table, rows times columns, which is
// also the total area the cells span: the rows past it are left out with a
// warning. The engine keeps a few words per slot for the grid and the
// collapsed borders. Anti-DoS guard, not spec-mandated: 2^21 slots are
// 50000 rows of 40 columns.
const maxTableSlots = 1 << 21
