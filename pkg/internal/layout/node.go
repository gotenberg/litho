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
}

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
	// ColSpan and RowSpan are the spans of a table cell, 0 for 1.
	ColSpan, RowSpan int
	// Span is the span of a table column or column group, 0 for 1.
	Span int
}

// Options parametrises one [Layout] call.
type Options struct {
	// Page is the page box: size and margins in points, every margin
	// resolved. Width and Height are positive and the margins leave a
	// positive page area.
	Page PageBox
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

// PageBox is a page's size and margins in points.
type PageBox struct {
	Width, Height float64
	// Margin is indexed by [css.Side].
	Margin [4]float64
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
}

// DefaultMaxPages is the page cap when Options.MaxPages is zero.
const DefaultMaxPages = 10_000

// Layout lays root out on pages. The output depends on root, fonts' index
// and opts alone. It returns ctx's error when ctx ends first.
func Layout(ctx context.Context, root *Node, fonts *text.Fonts, opts Options) (*Result, error) {
	return layout(ctx, root, fonts, opts)
}
