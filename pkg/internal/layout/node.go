package layout

import (
	"context"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// This file is the contract between a converter and the layout engine. A
// converter builds a [Node] tree from its input, HTML with its computed
// styles or an Office document mapped onto the same vocabulary, and
// [Layout] turns it into pages of the typeset model. A field added to
// it never changes the meaning of one that exists.

// Kind says what a [Node] is.
type Kind uint8

// The node kinds.
const (
	// Element is a styled container, the only kind with Children.
	Element Kind = iota
	// Text is a run of characters as the source wrote them. The engine
	// applies white-space processing and text-transform from Style.
	Text
	// LineBreak is a forced line break, what <br> produces, whatever the
	// white-space value in force.
	LineBreak
	// Replaced is an atomic box with intrinsic dimensions, an image.
	Replaced
)

// Picture is a picture with its intrinsic size: a raster one, held encoded
// in its Source, or a vector one its Vector draws.
type Picture struct {
	// Source is a raster picture as the file stores it (typeset.ImageSource),
	// nil for a vector picture.
	Source *typeset.ImageSource
	// Width and Height are the intrinsic size in points, both positive.
	Width, Height float64
	// Vector, when set, draws a vector picture, such as SVG, in place of
	// Source, which is then nil: it returns the fragments that paint the
	// picture into the rectangle r, in the caller's coordinates in points,
	// with nothing outside r. Their group nesting is at most
	// MaxVectorDepth. The engine copies a fragment before changing it, and
	// calls Vector from its own goroutine with its context.
	Vector func(ctx context.Context, r typeset.Rect) []typeset.Fragment
	// NoNaturalWidth and NoNaturalHeight mark a vector picture without a
	// natural width or height (CSS Images 3, 4.1), as an SVG whose width
	// or height is a percentage or missing. Width and Height then hold the
	// size the picture takes when nothing else sizes it: the other natural
	// dimension through the ratio, else 300 by 150 pixels (CSS Images 3,
	// 4.3). NoRatio marks a picture without a natural aspect ratio; with
	// it unset the ratio is Width over Height. A raster picture leaves all
	// three unset.
	NoNaturalWidth, NoNaturalHeight, NoRatio bool
}

// MaxVectorDepth is the deepest group nesting a [Picture.Vector] may
// return; the engine keeps room for it under its own groups.
const MaxVectorDepth = 5

// Node is one node of the content tree.
type Node struct {
	// Kind says what the node is.
	Kind Kind
	// Style is the node's computed style. It is set on every node: a Text or
	// LineBreak node shares its parent's, a Replaced node carries its own
	// (its width, display and float are its own). The engine reads it and
	// never writes it. Nodes styled alike may hold one style ([css.Styles],
	// [ShareStyles]): code that changes a node's style replaces it by a
	// changed copy and never writes through it.
	Style *css.Style
	// Text is the characters of a Text node.
	Text string
	// Picture is the picture of a Replaced node. When nil the node paints
	// nothing and keeps its box, the size Style gives it.
	Picture *Picture
	// Children are an Element's children in document order.
	Children []*Node

	// Role is the structure element role the node starts (typeset.StructElem
	// Role), "" for a node that does not start one. The engine builds the
	// structure tree from the roles in document order and hangs every
	// fragment of the node's subtree from the nearest role.
	Role string
	// Lang is the node's language as a BCP 47 tag, "" to inherit.
	Lang string
	// Alt describes a Replaced node, or a Role Figure or Link, for a reader
	// who cannot see it. A link without one is described by the text it
	// shows (typeset.Link Contents).
	Alt string
	// ActualText replaces the text of the node's subtree for a reader of
	// the structure tree (typeset.StructElem ActualText): the characters a
	// run of glyphs stands for when the glyphs' own codes do not say it, as
	// the private-use codes of a symbol font do not. It needs a Role, which
	// the engine gives the node a Span for when it has none. Empty replaces
	// nothing.
	ActualText string
	// ID names the node as a link target. [Result.Anchors] records where it
	// lands. An empty ID names nothing.
	ID string
	// Href makes the subtree a link: "#id" targets the node with that ID,
	// anything else is an external URI the converter has already resolved.
	Href string

	// ListValue is the ordinal of a list-item, 0 to number it after the
	// previous list-item sibling (1 for the first).
	ListValue int
	// Marker, when not nil, is the marker of a list-item as the converter
	// generated it from the ::marker pseudo-element (CSS Lists 3, 3.1):
	// its Style is the marker's computed style, and its Children, when
	// there are any, its content: the text of the Text children joins
	// into the marker's label, the first Replaced child with a Picture
	// draws in place of the label. A Marker without children draws the
	// default marker in its Style; one whose content is empty draws
	// nothing. Its Alt is what a reader hears for a picture, and for a
	// label of text the characters it stands for, the Lbl's ActualText
	// (ISO 32000-1, 14.9.4), as a symbol font's codes need. nil draws
	// the default marker in the item's Style. An outside marker whose
	// Style has a Width in Points is an Office list label: a box that
	// wide outside the item at its start edge, ending on the content
	// edge, with the label's start, centre or end on the box's start
	// edge, the number position, as the Style's TextAlign says.
	Marker *Node
	// ColSpan and RowSpan are the spans of a table cell, 0 for 1. The
	// engine copies them to the cell's structure element.
	ColSpan, RowSpan int
	// Scope says which cells a header cell, a node of Role TH, heads: its
	// row's, its column's or both (typeset.StructElem Scope). The zero
	// value says nothing.
	Scope typeset.TableScope
	// Numbering is the numbering system of the labels of a list, a node of
	// Role L (typeset.StructElem Numbering). The zero value says nothing.
	Numbering typeset.ListNumbering
	// PrintField describes the non-interactive form field a node of Role
	// Form shows (typeset.StructElem PrintField). The zero value says
	// nothing.
	PrintField typeset.PrintField
	// Span is the span of a table column or column group, 0 for 1.
	Span int
	// Leader marks a leader (CSS Generated Content 3, leader()): an
	// inline-level Element whose one Text child is the leader string.
	// The engine gives it no width of its own, keeps the content after
	// it in its parent on its line, and fills the room the line leaves
	// with copies of the string aligned on the line's end edge.
	Leader bool
	// ChangeBar, set on an inline Element or a Replaced node, marks
	// content a tracked change touches, as an Office marked view shows
	// it: every line box that holds part of the node's subtree, an empty
	// Element included, draws a change bar, a black hairline over the
	// line box's height 7.05 points left of the column the line sits in.
	// So does the line before one the node's content opens, and the line
	// after one it ends, in the same paragraph. The lines of an inline
	// block, a float or a positioned box draw none. The bar is an
	// artifact.
	ChangeBar bool
	// InitialLetter, set on a floating Element whose Style has an
	// InitialLetter, makes it an initial letter (CSS Inline 3, 4): the
	// first letter of its paragraph, a float of the start side, at the
	// font size that makes its cap height span InitialLetter.Size lines
	// of the paragraph, its cap height on the first line's. The line
	// height and font size of its Style are not used. A block that starts
	// with an initial letter, or that establishes a formatting context,
	// goes below the initial letters before it.
	InitialLetter bool
	// Footnote, set on an inline Element, makes it a footnote call: the
	// node is laid out in the flow, and the footnote body Footnote holds
	// is laid out in the footnote area of the page the call lands on, or
	// of a later page when that one has no room (CSS GCPM 3, 3). A body
	// whose Style has a Note is a note: it goes to the page's note area
	// ([PageBox.NoteArea]) by the same rule.
	Footnote *Node
}

// Options parametrises one [Layout] call.
type Options struct {
	// Page is the page box: size and margins in points, every margin
	// resolved. Width and Height are positive and the margins leave a
	// positive page area.
	Page PageBox
	// PageAt, when not nil, returns the page box of the page at a 0-based
	// index, for documents whose pages differ (CSS Page 3, 4.2: the :first,
	// :left and :right pages). It overrides Page; every box it returns
	// obeys Page's rules, or Layout fails with [ErrInvalidOptions]. The
	// flow is laid out at the width of the first page's area; each page
	// shows the next slice of it, as tall as its own area. Layout calls it
	// at most once per index, in index order, from its own goroutine.
	PageAt func(index int) PageBox
	// FitFirstPage makes the first page as tall as the content it shows
	// plus its top and bottom margins, at most its box's Height and at
	// least one point of page area: the flow is cut as the box says, and
	// the page is then trimmed to what it holds. A converter lays a
	// document out on one page this way (Chromium's single page, as
	// Gotenberg prints it).
	FitFirstPage bool
	// PixelFontMetrics rounds the ascent, descent and line gap of every
	// face to whole CSS pixels before the line boxes use them, as Blink
	// does for document text (see pixelMetrics); off, the faces' fractional
	// metrics apply, as WeasyPrint uses them.
	PixelFontMetrics bool
	// BrowserRules applies the rendering rules of the browser engine the
	// HTML converter follows, Blink, where CSS leaves the drawing to the
	// user agent: the shades of the groove, ridge, inset and outset borders
	// ([shades]), the shapes of the outside disc, circle and square list
	// markers ([symbolRect]), the position and thickness of an auto
	// underline and the decorations of text inside a decorating box of
	// another size. Off, as the Office converters leave it, the engine's own
	// rules apply: a darker shade of every colour, the marker's text, the
	// face's underline position.
	BrowserRules bool
	// FeatureValues, when not nil, returns the @font-feature-values of a
	// family, nil for none: font-variant-alternates resolves its names
	// against those of the family of the face a run is drawn in (CSS
	// Fonts 4, 6.9).
	FeatureValues func(family string) *css.FontFeatureValues
	// PageFor, when not nil, returns the page box of a page from its index,
	// its page type name and whether it is blank (CSS Page 3, 4.1 and
	// 4.2.3): the name is the start page value of the box a forced break
	// starts the page with, or the previous page's for a page an unforced
	// break starts, the first page taking the root's. It overrides PageAt
	// and Page under PageAt's rules, except that Layout may call it again
	// for an index whose name or blankness a later break decided.
	PageFor func(PageInfo) PageBox
	// MarginContent, when not nil, returns what is painted in the margins
	// of a page, in page coordinates, over the page box's background and
	// the canvas and under the page area's content (CSS Page 3, 5: the
	// page-margin boxes). pages is the document's page count. Layout calls
	// it once per page, in page order, from its own goroutine; it may call
	// Layout itself.
	MarginContent func(page PageInfo, pages int) []typeset.Fragment
	// MarginLinks, when not nil, returns the link regions of what
	// MarginContent painted on a page, in page coordinates. Layout calls
	// it right after MarginContent, for the same page. A link's Parent,
	// when not nil, is a Link element that holds no content, which Layout
	// adds to the root of the structure tree: page-margin content is a
	// pagination artifact, and the link annotation over it is tagged in a
	// Link element of its own (ISO 14289-1, 7.18.5).
	MarginLinks func(page PageInfo) []typeset.Link
	// Targets, when not nil, holds the anchors of another layout that an
	// internal link resolves against when no box of this one has its ID:
	// the anchors of the document, for its page-margin content.
	Targets map[string]typeset.Dest
	// Placed, when not nil, receives what [Result.Anchors] will hold,
	// once the flow is laid out and before the first MarginContent call,
	// so that page-margin content can follow where anchored nodes landed:
	// the named strings and running elements of a page (CSS GCPM 3, 2).
	// Layout calls it once, from its own goroutine.
	Placed func(anchors map[string]typeset.Dest)
	// Lang is the document language as a BCP 47 tag, used for a node
	// without one in its ancestry, "" for unknown.
	Lang string
	// Fallback is the rule that picks the face of a character a run's
	// font lacks: the one of the program whose output the converter
	// follows (see [text.Fallback]). The zero value follows fontconfig.
	Fallback text.Fallback
	// Image loads the picture a style names by URL, for backgrounds and
	// list markers. nil refuses every URL. An error is reported as a
	// warning and the image is left out.
	Image func(ctx context.Context, url string) (*Picture, error)
	// ClipSource returns the clipping regions of the SVG clipPath element
	// url names, for an element whose border box is box, in the same
	// coordinates (CSS Masking 1, 5.1): the content shows where it lies
	// inside every path of a region, in any of the regions. found reports
	// that url names a clipPath element; found with no region hides the
	// element. nil names none.
	ClipSource func(ctx context.Context, url string, box typeset.Rect) (regions [][]*typeset.Path, found bool)
	// MaskSource returns the content of the SVG mask element url names,
	// for an element whose border box is box, in the same coordinates
	// (CSS Masking 1, 6.1 and 7.1), and whether its mask-type is alpha
	// rather than luminance. found reports that url names a mask element.
	// The content hangs from no structure element. nil names none.
	MaskSource func(ctx context.Context, url string, box typeset.Rect) (content []typeset.Fragment, alpha, found bool)
	// MaxPages bounds the pages laid out; past it Layout fails with
	// [limits.ErrTooManyPages]. Zero means [DefaultMaxPages].
	MaxPages int
	// Logger receives diagnostics, nil for none.
	Logger *slog.Logger
	// FirstLine styles the first formatted line of the block containers
	// that have a ::first-line (CSS Pseudo 4, 2.1); nil styles none.
	FirstLine FirstLineStyler
	// release is when the layout releases the shaped text of its inline
	// formatting contexts, for a test that compares the policies.
	release releasePolicy
}

// FirstLineStyler gives the styles of the first formatted lines of a
// layout (CSS Pseudo 4, 2.1). Layout calls it from its own goroutine.
//
// The first formatted line of a block container is the first line box of
// its inline formatting context, or the first formatted line of its first
// in-flow block-level child when it holds blocks; a float, a positioned
// box and an anonymous block of collapsible white space are skipped, and
// an inline block, a table, a flex or grid item or container, or a table
// cell keeps its first line to itself. The line takes the ::first-line of
// the nearest block container it is the first formatted line of, its own
// block's first. Its content sits in an inline box of that style, which
// paints its background and decorations, and the line's root inline box
// takes the style's font and line height; the inline boxes on it take the
// styles Restyle gives. Only the first line box of a block container is
// its first formatted line: the line after a forced break is not.
type FirstLineStyler interface {
	// Pseudo returns the ::first-line style of the block container node
	// n inheriting from parent, the style of the block whose first
	// formatted line it styles, or nil when n has none.
	Pseudo(n *Node, parent *css.Style) *css.Style
	// Restyle returns the style of n, an inline Element on a first
	// formatted line, when it inherits from parent, its parent's style on
	// that line; nil keeps n's Style.
	Restyle(n *Node, parent *css.Style) *css.Style
}

// PageInfo names a page for [Options.PageFor] and [Options.MarginContent].
type PageInfo struct {
	// Index is the page's 0-based index.
	Index int
	// Name is the page type name, "" for an unnamed page (CSS Page 3,
	// 4.1).
	Name string
	// Blank reports a page that a forced break to a left or a right page
	// left empty (CSS Page 3, 4.2.3; CSS Fragmentation 3, 3.1).
	Blank bool
}

// PageBox is a page's size and margins in points.
type PageBox struct {
	// Width and Height are the page box's size in points.
	Width, Height float64
	// Margin is indexed by [css.Side].
	Margin [4]float64
	// Background is the page box's own background, painted on every page
	// under the canvas: a style whose Background fields alone are read
	// (see css.PageStyle). nil for none.
	Background *css.Style
	// Bleed is how far the sheet reaches past the page box on every side,
	// in points, finite and at least 0 (CSS Page 3, 7.2): the page is
	// laid out on the page box, the page background paints over the whole
	// sheet, and the page records its trim and bleed boxes.
	Bleed float64
	// Marks are the page marks painted in the bleed area (CSS Page 3,
	// 7.1).
	Marks css.Marks
	// Footnote is the style of the page's footnote area, the @footnote
	// rule (CSS GCPM 3, 3.4): its margins, borders, padding, background
	// and max-height apply. nil lays the area out unstyled.
	Footnote *css.Style
	// NoteArea is the style of the page's note area, the @note-area rule:
	// the bodies of the note calls ([Node.Footnote] whose Style has a
	// Note) that land on the page lay out in it. In the flow it sits at
	// the top of the page area and pushes the page's content down; an
	// absolutely positioned area is placed by its insets against the
	// page area and takes no room. nil lays the area out unstyled, in the
	// flow.
	NoteArea *css.Style
	// Frame, when not nil, is a border drawn on the page (WordprocessingML
	// pgBorders, ODF page-layout borders), over the page background and
	// under the margin content and the page area's content.
	Frame *PageFrame
	// FootnoteRule, when not nil, is the separator drawn above the page's
	// footnote area when the area holds a note, as Word and Writer draw
	// one. nil draws none; the area's borders come from Footnote.
	FootnoteRule *FootnoteRule
}

// PageFrame is a border box drawn on every page it applies to.
type PageFrame struct {
	// Inset is how far the frame's border box sits from each page edge,
	// in points, indexed by [css.Side]; finite, and leaving a positive
	// box.
	Inset [4]float64
	// Style supplies the frame's borders: its Border fields and BorderRadius
	// alone are read. Its padding, when any, is the room between the
	// border and the content, which the converter folds into Inset or the
	// margins; the engine draws the border where Inset puts it.
	Style *css.Style
}

// Result is what [Layout] produced.
type Result struct {
	// Pages holds at least one page.
	Pages []*typeset.Page
	// Structure is the structure tree, its root a Document element. A
	// fragment of a node without a Role in its ancestry is an artifact.
	Structure *typeset.StructElem
	// Anchors maps every non-empty Node.ID that landed on a page to the
	// top-left corner of its first box.
	Anchors map[string]typeset.Dest
	// Warnings lists the constructs the engine did not lay out as the
	// style asked, in document order.
	Warnings []typeset.Warning
	// ContentWidth is the right edge of the flow's scrollable overflow,
	// in points from the left edge of the first page's area (CSS Overflow
	// 3, 2.2): the border boxes and line contents of every box, those
	// inside a box that clips its overflow left out, what a page of
	// another width holds scaled to the first page's area width. It is at
	// least zero and exceeds the area's width when content overflows its
	// page, which a converter shrinks to fit as browsers print.
	ContentWidth float64
	// Extents maps every non-empty Node.ID of a block-level, inline-block
	// or replaced box that landed on a page to its border box less its
	// padding, the box that holds its content and borders, on the page
	// its first box landed on, in that page's coordinates. Inline boxes
	// are not listed.
	Extents map[string]typeset.Rect
}

// DefaultMaxPages is the page cap when Options.MaxPages is zero: the
// default of Limits.MaxConvertPages.
const DefaultMaxPages = limits.DefaultMaxConvertPages

// Layout lays root out on pages. The output depends on root, fonts' index
// and opts alone. It returns ctx's error when ctx ends first.
func Layout(ctx context.Context, root *Node, fonts *text.Fonts, opts Options) (*Result, error) {
	return layout(ctx, root, fonts, opts)
}
