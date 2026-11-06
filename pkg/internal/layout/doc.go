// Package layout lays a styled content tree out on pages: a [Node] tree in,
// the pages of [compose] fragments, the structure tree, link targets and
// warnings out.
//
// It implements the visual formatting model of CSS 2.1 for paged media:
// box generation (section 9.2), block formatting with margin collapsing,
// floats and clearance (sections 8.3.1, 9.4.1, 9.5, 10.3, 10.6), inline
// formatting with line boxes (sections 9.4.2, 10.8 and CSS Text 3), lists
// (section 12.5), backgrounds and borders (CSS Backgrounds 3), and page
// fragmentation (CSS Fragmentation 3 and CSS 2.1 section 13.3).
//
// # Fragmentation
//
// The engine lays the document out in one tall column whose height is cut
// every page area height. A line box, a replaced box or a box that avoids
// breaks inside, which would straddle a cut, moves down to the next page:
// the gap it leaves is a pagination strut. Painting then slices the column
// into pages, so a box that spans a cut paints its background and borders
// on every page it crosses, sliced at the cut (box-decoration-break:
// slice).
//
// # Formatting contexts
//
// A block container's content is laid out by a formatter chosen per
// display value: block flow and inline flow now. A table formatter plugs in
// as one more case of formatterFor without touching the block and inline
// code.
//
// # Not supported
//
// Tables are laid out as blocks, a row and a cell each a block box.
// Absolute and fixed positioning lay out as static, with a warning. Flex,
// grid, transforms, generated content, hyphenation and multi-column layout
// are out of scope.
//
// # Concurrency
//
// [Layout] keeps its state per call: concurrent calls are safe, sharing
// only the [text.Fonts] index, which is safe for concurrent use.
package layout
