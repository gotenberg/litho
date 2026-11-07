package layout

import (
	"context"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// This file is the contract between a converter and the layout engine. A
// converter builds a [Node] tree from its input, HTML with its computed
// styles or an Office document mapped onto the same vocabulary, and
// [Layout] turns it into pages of the composition model. A later wave adds
// fields; it does not change the meaning of one that exists.

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

// Picture is a decoded picture with its intrinsic size.
type Picture struct {
	Source *compose.ImageSource
	// Width and Height are the intrinsic size in points, both positive.
	Width, Height float64
	// Vector, when set, draws a vector picture, such as SVG, in place of
	// Source, which is then nil: it returns the fragments that paint the
	// picture into the rectangle r, in the caller's coordinates in points,
	// with nothing outside r. Their group nesting is at most
	// MaxVectorDepth. The engine copies a fragment before changing it, and
	// calls Vector from its own goroutine with its context.
	Vector func(ctx context.Context, r compose.Rect) []compose.Fragment
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
	Kind Kind
	// Style is the node's computed style. It is set on every node: a Text or
	// LineBreak node shares its parent's, a Replaced node carries its own
	// (its width, display and float are its own). The engine reads it and
	// never writes it.
	Style *css.Style
	// Text is the characters of a Text node.
	Text string
	// Picture is the picture of a Replaced node. When nil the node paints
	// nothing and keeps its box, the size Style gives it.
	Picture *Picture
	// Children are an Element's children in document order.
	Children []*Node

	// Role is the structure element role the node starts (compose.StructElem
	// Role), "" for a node that does not start one. The engine builds the
	// structure tree from the roles in document order and hangs every
	// fragment of the node's subtree from the nearest role.
	Role string
	// Lang is the node's language as a BCP 47 tag, "" to inherit.
	Lang string
	// Alt describes a Replaced node, or a Role Figure or Link, for a reader
	// who cannot see it.
	Alt string
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
	// nothing. Its Alt is what a reader hears for a picture. nil draws
	// the default marker in the item's Style.
	Marker *Node
	// ColSpan and RowSpan are the spans of a table cell, 0 for 1.
	ColSpan, RowSpan int
	// Span is the span of a table column or column group, 0 for 1.
	Span int
	// Leader marks a leader (CSS Generated Content 3, leader()): an
	// inline-level Element whose one Text child is the leader string.
	// The engine gives it no width of its own, keeps the content after
	// it in its parent on its line, and fills the room the line leaves
	// with copies of the string aligned on the line's end edge.
	Leader bool
	// Footnote, set on an inline Element, makes it a footnote call: the
	// node is laid out in the flow, and the footnote body Footnote holds
	// is laid out in the footnote area of the page the call lands on, or
	// of a later page when that one has no room (CSS GCPM 3, 3).
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
	MarginContent func(page PageInfo, pages int) []compose.Fragment
	// Placed, when not nil, receives what [Result.Anchors] will hold,
	// once the flow is laid out and before the first MarginContent call,
	// so that page-margin content can follow where anchored nodes landed:
	// the named strings and running elements of a page (CSS GCPM 3, 2).
	// Layout calls it once, from its own goroutine.
	Placed func(anchors map[string]compose.Dest)
	// Lang is the document language as a BCP 47 tag, used for a node
	// without one in its ancestry, "" for unknown.
	Lang string
	// Image loads the picture a style names by URL, for backgrounds and
	// list markers. nil refuses every URL. An error is reported as a
	// warning and the image is left out.
	Image func(ctx context.Context, url string) (*Picture, error)
	// MaxPages bounds the pages laid out; past it Layout fails with
	// [ErrTooManyPages]. Zero means [DefaultMaxPages].
	MaxPages int
	// Logger receives diagnostics, nil for none.
	Logger *slog.Logger
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
}

// Result is what [Layout] produced.
type Result struct {
	// Pages holds at least one page.
	Pages []*compose.Page
	// Structure is the structure tree, its root a Document element. A
	// fragment of a node without a Role in its ancestry is an artifact.
	Structure *compose.StructElem
	// Anchors maps every non-empty Node.ID that landed on a page to the
	// top-left corner of its first box.
	Anchors map[string]compose.Dest
	// Warnings lists the constructs the engine did not lay out as the
	// style asked, in document order.
	Warnings []compose.Warning
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
	Extents map[string]compose.Rect
}

// DefaultMaxPages is the page cap when Options.MaxPages is zero.
const DefaultMaxPages = 10_000

// Layout lays root out on pages. The output depends on root, fonts' index
// and opts alone. It returns ctx's error when ctx ends first.
func Layout(ctx context.Context, root *Node, fonts *text.Fonts, opts Options) (*Result, error) {
	return layout(ctx, root, fonts, opts)
}
