// Package layout lays a styled content tree out on pages: a [Node] tree in,
// the pages of [compose] fragments, the structure tree, link targets and
// warnings out.
//
// It implements the visual formatting model of CSS 2.1 for paged media:
// box generation (section 9.2), block formatting with margin collapsing,
// floats and clearance (sections 8.3.1, 9.4.1, 9.5, 10.3, 10.6), inline
// formatting with line boxes (sections 9.4.2, 10.8 and CSS Text 3), lists
// (section 12.5), tables (chapter 17), flexible boxes (CSS Flexbox 1),
// grids (CSS Grid 1), the sizing keywords (CSS Sizing 3 and 4),
// multi-column layout (CSS Multicol 1),
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
// A node with a [Node.Footnote] is a footnote call (CSS GCPM 3, 3): when
// its line lands on a page, the body goes to that page's footnote area at
// the bottom of its page area if the area's max-height allows it and the
// page has room for it, and the page's slice of the flow ends that much
// higher; otherwise the body waits for the next page, which takes the
// first body waiting whatever its height, and a body refused for room
// ends its call's page after the call's line. A call in columns places its
// body on the page its column lies on, by its line's bottom there, and the
// columns are laid out again in the room the footnote area leaves; a body
// that lacks the room is given it once, and waits for the next page when
// the columns laid out again in it move its call off its page. The pages then cut the flow
// where each one's slice ends, as named pages do.
//
// A call whose body's style has a Note goes to the page's note area
// ([PageBox.NoteArea]) by the same rules: at the top of the page area,
// pushing the page's slice of the flow down, or, absolutely positioned,
// placed by its insets against the page area and taking no room.
//
// # Leaders and page marks
//
// A leader ([Node.Leader]) takes no width until its line is aligned, then
// fills the room the line leaves with the copies of its string that fit
// whole on a grid anchored at the line's end edge (CSS Generated Content
// 3, leader()). A page box with a bleed lays out on a sheet as much larger
// on every side, its background and marks painted over the sheet, its
// trim and bleed boxes recorded (CSS Page 3, 7).
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
// artifacts; the cells' borders and backgrounds are sliced at the cut.
// Rows a row-spanning cell joins break between them as other rows do, the
// spanning cell sliced at the cut, as Writer breaks them. A
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
// # Grids
//
// A grid container's in-flow children are its grid items, a run of text
// among them an anonymous one, in order-modified document order. The grid
// formatter expands the explicit grid's repeat() against the available
// space, places the items with the auto-placement algorithm (CSS Grid 1,
// 7 and 8), and sizes the columns, then the rows from the items laid out
// at their column widths, with the track sizing algorithm of section 11:
// intrinsic contributions by span, flexible tracks by the size of an fr,
// auto tracks stretched. justify-content and align-content distribute the
// free space, justify-self, align-self, auto margins and first baselines
// place each item in its area, and an absolutely positioned box whose
// containing block the grid generates takes the area its placement names.
// The rows are laid out top down where they land: a row whose items cannot
// start on its page moves to the next one, a taller one breaks inside its
// items. Subgrids, masonry and vertical writing modes are not laid out.
//
// # Sizing keywords
//
// min-content, max-content and fit-content widths resolve against the
// content's intrinsic widths, stretch against the containing block less
// the margins (CSS Sizing 3, 3; CSS Sizing 4, 3.1), when a box is laid out;
// in the block axis they size as auto, and bound min-height and max-height
// to the content height.
// # Multi-column layout
//
// A multi-column container lays its content out in columns (CSS Multicol
// 1), a fragmentation context of its own: the content is laid out in one
// column of the column width whose cuts are the columns, by the same break
// logic as the pages, and painting moves each column's slice to its place.
// The column count and width follow the pseudo-algorithm of CSS Multicol
// 1, 3.4; rules paint between the columns of a row that both hold content.
// column-fill: balance finds the least column height the content fits in
// by laying it out again, the content before a spanner always balancing;
// column-fill: auto fills the columns in turn. A spanner, column-span: all,
// splits the container, and the boxes around it, into column sets. A row
// the end of a page cuts continues on the next page with a new row, the
// last row balancing; content past the last column of an unfragmented row
// goes to overflow columns. column-height and column-wrap (CSS Multicol 2)
// wrap rows. Scroll containers and boxes with overflow: hidden are
// monolithic in columns, and absolutely positioned boxes break at the
// columns as the flow does.
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
// # Office documents
//
// The Office converters set the fields of css.Style no CSS property sets,
// which LibreOffice's layout of WordprocessingML and ODF is the reference
// for: TabStops lays tabs out on Office stops (alignment, leaders, bar
// stops and the line breaks around them), LineRule and MinLineHeight
// space lines as Writer does, the room a proportional spacing adds below
// a page's last line hanging past the page cut, JustifyShrink lets
// justified lines shrink their spaces, TextScale scales glyphs horizontally,
// PageAnchor anchors a frame to the page box, of the page AnchorPage names
// or of its anchor's, PageClamp keeps a frame or a float positioned from
// its paragraph inside its page box, and DecorationCloneAll
// draws a bordered paragraph whole on every page, and FontKerning none
// shapes without the font's kerning. [PageBox.Frame] draws page borders,
// [PageBox.FootnoteRule] the separator above a page's footnotes, and a
// [Node.Marker] whose Style has a width lays a list label out on the
// number position.
//
// # Not supported
//
// The vertical alignment of a section's content on its pages (Word's
// vAlign, Writer's page text alignment) has no equivalent: the
// converters lay it out at the top and warn. A float cannot be anchored
// to the page; text does not wrap around a page-anchored box.
// A box of a vertical writing mode in a horizontal flow is the root of an
// orthogonal flow whose text is set sideways, a horizontal layout turned
// a quarter turn into the box (writing.go); upright glyphs, vertical-lr's
// line order and vertical flows nested in vertical ones are not laid out.
// 3D transforms and hyphenation are out of scope;
// a footnote call inside an inline block or a flex item is laid out off
// the page cuts, and its body goes to a page after the flow (a table
// cell's calls go to the pages their lines land on, the table laid out
// again against the footnote areas);
// visibility:
// collapse on a flex item hides it as visibility: hidden does.
//
// # Concurrency
//
// [Layout] keeps its state per call: concurrent calls are safe, sharing
// only the [text.Fonts] index, which is safe for concurrent use.
package layout
