// Package layout lays a styled content tree out on pages: a [Node] tree in,
// the pages of [compose] fragments, the structure tree, link targets and
// warnings out.
//
// It implements the visual formatting model of CSS 2.1 for paged media:
// box generation (section 9.2), block formatting with margin collapsing,
// floats and clearance (sections 8.3.1, 9.4.1, 9.5, 10.3, 10.6), inline
// formatting with line boxes (sections 9.4.2, 10.8 and CSS Text 3), lists
// (section 12.5), tables (chapter 17), flexible boxes (CSS Flexbox 1),
// backgrounds and borders (CSS Backgrounds 3), and page fragmentation (CSS Fragmentation 3 and CSS 2.1
// section 13.3).
//
// # Fragmentation
//
// The engine lays the document out in one tall column, as wide as the
// first page's area, cut at every page area height: each page's own when
// [Options.PageAt] gives the pages different boxes. A line box, a replaced
// box or a box that avoids breaks inside, which would straddle a cut,
// moves down to the next page: the gap it leaves is a pagination strut. A
// monolithic piece taller than a page overflows onto the pages below and
// the content after it follows its full height; a line taller than a page
// for its text alone takes one page. Margins adjoining an unforced break
// truncate (CSS Fragmentation 3, 5.2), and a container whose content
// starts on a later page than its top moves to that page. Forced breaks
// to a left or a right page leave a blank page when needed. Siblings whose
// page values differ meet at a forced break, and the page after it takes
// the name of the box that starts it (CSS Page 3, 4.1), which
// [Options.PageFor] turns into a page box; the children a box of auto
// width lays out on a page wider or narrower than the first are laid out
// at that page's width. Painting then slices the column into pages, so a
// box that spans a cut paints its background and borders on every page it
// crosses, sliced at the cut (box-decoration-break: slice).
// [Options.MarginContent] paints each page's margins, the page-margin
// boxes, under the page area's content.
//
// # Formatting contexts
//
// A block container's content is laid out by a formatter chosen per
// display value: block flow, inline flow, the table formatter of a table
// wrapper box, and the flex formatter of a flex container.
//
// # Tables
//
// A table generates a wrapper box holding its captions and the table box,
// with the anonymous rows, cells and tables CSS 2.1, 17.2.1 asks for. The
// columns take the automatic layout the way browsers do it, or the fixed
// one; rows take the height of their cells, aligned on a shared baseline,
// at the top, middle or bottom. Borders are separated by border-spacing or
// collapsed, the conflicts resolved per CSS 2.1, 17.6.2.1. Percentage
// heights on rows and cells resolve against the table's height, and
// visibility: collapse removes rows, row groups and columns with their
// border spacing. A row that does not fit what is left of a page breaks
// inside its cells at a line boundary, as Chromium and WeasyPrint break
// table rows: each cell's content continues at the top of the row's piece
// on the next page, under a copy of the header group, and the page it
// leaves ends with a copy of the footer group, the copies painted as
// artifacts; the cells' borders and backgrounds are sliced at the cut. A
// row that avoids breaks inside it, or none of whose lines fits, moves to
// the next page whole; one with nothing to break at, taller than a page
// area, overflows onto the pages below.
//
// # Flexible boxes
//
// A flex container's in-flow children are its flex items, a run of text
// among them an anonymous one, sorted in order-modified document order. The
// flex formatter runs the algorithm of CSS Flexbox 1, section 9: flex base
// and hypothetical main sizes with the automatic minimum size, lines,
// flexible lengths, cross sizes with stretching and baseline alignment,
// auto margins, justify-content, align-items, align-self, align-content and
// gaps, in the horizontal writing mode alone. Each item is measured off the
// page cuts and laid out where it lands: a row container breaks between
// its lines and inside its items, the items of a line starting on the same
// page; a column container breaks between and inside its items. Items
// paint as inline blocks do, in order-modified document order, and an item
// with a z-index creates a stacking context.
//
// # Positioning
//
// Relative positioning moves a box, inline boxes included, when it paints
// (CSS 2.1, 9.4.3). An absolutely positioned box leaves the flow, which
// records its static position; once the flow is laid out, the positioned
// boxes are laid out in tree order in their containing blocks (CSS 2.1,
// 10.1, 10.3.7, 10.3.8, 10.6.4 and 10.6.5), in the coordinates of the tall
// column, so they fragment across the page cuts like the flow. A fixed box
// is laid out once on the page area and painted on every page (CSS 2.1,
// 9.6.1). Painting follows the stacking contexts of CSS 2.1, 9.9 and
// Appendix E: z-index, opacity and 2D transforms (CSS Transforms 1) create
// them; a transform paints its box through a group transform.
//
// # Not supported
//
// Grid, vertical writing modes, 3D transforms, generated content,
// hyphenation and multi-column layout are out of scope; visibility:
// collapse on a flex item hides it as visibility: hidden does.
//
// # Concurrency
//
// [Layout] keeps its state per call: concurrent calls are safe, sharing
// only the [text.Fonts] index, which is safe for concurrent use.
package layout
