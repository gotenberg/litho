package css

// The fields of [Style] that no CSS property sets, which the Office
// converters fill from their document models and the layout engine
// honours: [Style.TabStops], [Style.MinLineHeight], [Style.TextScale],
// [Columns.Tracks] and the fields of [Office].

// TabStop is one tab stop of a block container (ECMA-376 Part 1,
// 17.3.1.37; ODF 1.3 Part 3, 17.8): a tab character in a line of the
// container advances to the next stop past the line's current position.
//
// A container whose [Style.TabStops] is not nil lays every tab out as
// LibreOffice does for WordprocessingML and ODF:
//
//   - The tab goes to the first stop, bar stops aside, whose Pos lies
//     past the tab's position, measured from the start edge of the
//     container's content box (its end edge in a right-to-left
//     container), however close; past the last stop, to the next
//     multiple of [Style.TabSize] past both, a left stop.
//   - The text after the tab, up to the next tab or the paragraph's end,
//     is aligned on the stop as Align says. Text too wide to align that
//     way starts where the tab started: the tab takes no room.
//   - A stop past the line's end makes the tab fill the rest of the line;
//     what follows goes to the next line.
//   - When the first word after the tab does not fit on the line beside
//     it, the line breaks before the tab, which advances again on the
//     next line, unless it starts the line.
//   - A justified line stretches only its text after its last tab.
//
// The tab characters must survive white space processing: the container
// sets a [WhiteSpace] that preserves them, pre-wrap as the Office formats
// keep every space.
type TabStop struct {
	// Pos is the stop's position in points from the start edge of the
	// containing block's content box, the edge the paragraph's indent is
	// measured from. It is finite and at least 0.
	Pos float64
	// Align says what the text after the tab, up to the next tab or the
	// end of the line, aligns on the stop.
	Align TabAlign
	// Leader fills the room the tab leaves, from the tab's start to the
	// stop, with the leader's character repeated.
	Leader TabLeader
}

// TabAlign is the alignment of a [TabStop].
type TabAlign uint8

// The tab stop alignments.
const (
	// TabLeft starts the text at the stop.
	TabLeft TabAlign = iota
	// TabCenter centres the text on the stop.
	TabCenter
	// TabRight ends the text at the stop.
	TabRight
	// TabDecimal puts the first decimal separator of the text, else its
	// end, at the stop.
	TabDecimal
	// TabBar draws a vertical rule at the stop across every line box of
	// the container, in the container's colour; no tab advances to it.
	TabBar
)

// TabLeader is the fill of a [TabStop].
type TabLeader uint8

// The tab leaders.
const (
	// TabLeaderNone leaves the room blank.
	TabLeaderNone TabLeader = iota
	// TabLeaderDots fills it with full stops.
	TabLeaderDots
	// TabLeaderHyphens fills it with hyphens.
	TabLeaderHyphens
	// TabLeaderUnderscore fills it with low lines.
	TabLeaderUnderscore
	// TabLeaderMiddleDots fills it with middle dots.
	TabLeaderMiddleDots
)

// LineRule selects how the line height of a block container sizes its
// line boxes and places their baseline. The Office formats space lines
// differently from CSS: [LineRuleCSS] is CSS's rule, the others are
// Writer's, which LibreOffice applies to WordprocessingML and ODF alike
// (ECMA-376 Part 1, 17.3.1.33 spacing lineRule; ODF 1.3 Part 3, 20.315
// fo:line-height). The rule is the block container's; the inline boxes of
// its lines share one baseline under an Office rule, whatever their own
// line-height.
type LineRule uint8

// The line rules.
const (
	// LineRuleCSS sizes each inline box by its line-height with the
	// leading split above and below its glyphs (CSS 2.1, 10.8.1).
	LineRuleCSS LineRule = iota
	// LineRuleExact makes every line box exactly LineHeight tall, a
	// length (WordprocessingML lineRule exact, ODF fo:line-height as a
	// length): the baseline sits four fifths of the height below the line
	// top, whatever the glyphs, which overflow a line too short for them.
	LineRuleExact
	// LineRuleMultiple spaces the line boxes by LineHeight, a number, a
	// multiple of their natural height (WordprocessingML lineRule auto, ODF
	// fo:line-height as a percentage). The natural height is the tallest
	// ascent plus line gap over the line's glyph runs and atomic inlines
	// above the shared baseline, plus the deepest descent below it. At 1
	// and above the baseline keeps its natural place and the room added
	// goes below the glyphs, LineHeight less 1 times the natural height of
	// the glyph runs alone: an atomic inline taller than the text, an
	// as-character picture, adds its own height unscaled, as Writer sizes
	// such a line. Below 1 the whole natural height shrinks and the
	// baseline sits four fifths of it below the line top.
	LineRuleMultiple
)

// DecorationCloneAll gives each fragment of a box its own borders,
// padding, radii, shadows and background at every break, page breaks
// included, as Writer draws a bordered paragraph across a page break: the
// lines leave room for the bottom edges before each cut and start below
// the top edges after it. [DecorationClone] clones at column breaks alone,
// as Chromium prints. No CSS value sets it.
const DecorationCloneAll = DecorationClone + 1

// ColumnTrack is one column of a row of columns of different widths, as
// WordprocessingML lists them (ECMA-376 Part 1, 17.6.4 cols with
// w:equalWidth="0", each 17.6.3 col giving w:w and w:space) and ODF
// gives them (ODF 1.3 Part 3, 17.13 style:columns, each style:column with
// style:rel-width, fo:start-indent and fo:end-indent).
//
// The columns of a container whose [Columns.Tracks] is not empty sit side
// by side, the first at the start edge of its content box: each Gap after
// its column but the last's, and the content width less those gaps shared
// out between the columns in proportion to their Width. Column rules sit
// in the middle of the gaps. Content flowing from one column into a
// narrower or wider one breaks its lines at that column's width; a block
// keeps the width of the widest column, so its background and borders do.
//
// A WordprocessingML col maps to Width w:w and Gap w:space, in points
// both. An ODF style:column of rel-width r with indents s and e on a
// content width W maps to Width r/R*W - s - e, R the sum of the
// rel-widths, and Gap e plus the next column's s; the first column's s and
// the last one's e are the container's padding.
type ColumnTrack struct {
	// Width is the column's share of the room the gaps leave, finite and
	// greater than 0; the shares are relative to their sum.
	Width float64
	// Gap is the room after the column in points, finite and at least 0;
	// the last column's is ignored.
	Gap float64
}

// Office holds the fields of [Style] no CSS property sets that the Office
// converters fill for the frames, chains and borders of their documents,
// as LibreOffice's Writer lays WordprocessingML and ODF out. [Style]
// embeds it: the zero value changes nothing, and none is inherited.
type Office struct {
	// FrameWrap is how the flow wraps around an absolutely positioned box
	// that is page-anchored ([Style.PageAnchor]) or page-clamped
	// ([Style.PageClamp]), Writer's wrap of a frame or a drawing
	// (ODF 1.3 Part 3, 20.393 style:wrap; ECMA-376 Part 1, 20.4.2.15 to
	// 20.4.2.19, the wrap elements of an anchor): the lines of every
	// block formatting context the box's border box, grown by
	// [Office.WrapGap], overlaps on its page leave its band as a float
	// would, before and after the box's anchor alike. The zero value,
	// [FrameWrapThrough], lets the text run under or over the box, the
	// box taking no room. See [FrameWrap] for the modes.
	FrameWrap FrameWrap
	// WrapGap is the room the text keeps from the border box of a
	// wrapped box ([Office.FrameWrap]), in points, indexed by [Side],
	// each finite and at least 0: the frame's fo:margin (ODF 1.3 Part 3,
	// 20.199 to 20.204, the spacing to the text), the anchor's distT,
	// distR, distB and distL (ECMA-376 Part 1, 20.4.2.3). It moves the box
	// none: the insets place the border box.
	WrapGap [4]float64
	// Chain names the chain of linked boxes the box is a link of, "" for
	// none (ODF 1.3 Part 3, 19.112 draw:chain-next-name; ECMA-376 Part 1,
	// 20.4.2.18 wps:linkedTxbx id): the chain's content flows from one
	// link into the next as the pages' content flows from page to page.
	// The first link, [Office.ChainIndex] 0, holds the content of the
	// whole chain as its children; the converter leaves the other links
	// without children, and the engine ignores any they have. The content
	// is laid out at the content width of the first link and cut at its
	// content height; what does not fit continues at the top of the next
	// link in ChainIndex order, laid out at that link's width, wherever
	// the link sits, on another page included, and so on; what the last
	// link has no room for is not painted. A link's height is its own,
	// definite from its style, its content never growing it (Writer
	// disables the automatic height of a linked frame); a link without a
	// definite height grows to hold all the content left, and the links
	// after it stay empty. Links of one chain
	// share a name; two links with the same name and index keep the
	// first in tree order, the later one empty.
	Chain string
	// ChainIndex is the box's place in its chain ([Office.Chain]), from 0
	// for the first link: the ODF reader counts the draw:chain-next-name
	// links from the frame no other names, the WordprocessingML one takes
	// wps:linkedTxbx seq. Indexes need not be consecutive: the links
	// follow in increasing index.
	ChainIndex int
	// StrokeBorders paints the box's solid borders as Writer prints them:
	// each side a line stroked along the middle of the side at the side's
	// width, its ends meeting the adjacent sides', and at least
	// [MinBorderStroke] wide, where a CSS border is a filled band. The
	// band of a border thinner than a device pixel averages to grey on a
	// raster; a stroked line is drawn at least a pixel wide. A width of 0
	// draws no border. The other border styles and the radii paint as CSS
	// paints them.
	StrokeBorders bool
}

// MinBorderStroke is the least width, in points, at which a border of
// [Office.StrokeBorders] is stroked: LibreOffice strokes a border of one
// twip (0.05 pt), Writer's hairline, two twips wide, and wider borders at
// their width.
const MinBorderStroke = 0.1

// FrameWrap is how the flow wraps around a page-anchored or page-clamped
// box ([Office.FrameWrap]), Writer's wrap modes (ODF 1.3 Part 3, 20.393
// style:wrap). WordprocessingML's wrapSquare, wrapTight and wrapThrough
// with wrapText bothSides map to [FrameWrapParallel], left and right to
// [FrameWrapLeft] and [FrameWrapRight], largest to [FrameWrapLargest];
// wrapTopAndBottom to [FrameWrapNone]; wrapNone to [FrameWrapThrough]
// (ECMA-376 Part 1, 20.4.2.15 to 20.4.2.19, 20.4.3.7 ST_WrapText).
type FrameWrap uint8

// The frame wrap modes.
const (
	// FrameWrapThrough lets the text run through the box, which paints
	// in front of it or behind it by its z-index (style:wrap run-through).
	FrameWrapThrough FrameWrap = iota
	// FrameWrapNone keeps the text above and below the box, none beside
	// it (style:wrap none): a line overlapping its band moves below it.
	FrameWrapNone
	// FrameWrapParallel lets the text beside the box on both sides
	// (style:wrap parallel).
	FrameWrapParallel
	// FrameWrapLeft lets the text on the box's left alone (style:wrap
	// left).
	FrameWrapLeft
	// FrameWrapRight lets the text on the box's right alone (style:wrap
	// right).
	FrameWrapRight
	// FrameWrapDynamic lets the text on each side wide enough for it, as
	// Writer's optimal wrap decides per line (style:wrap dynamic): a side
	// narrower than Writer's least text width takes no text.
	FrameWrapDynamic
	// FrameWrapLargest lets the text on the side with more room alone
	// (style:wrap biggest, wrapText largest).
	FrameWrapLargest
)
