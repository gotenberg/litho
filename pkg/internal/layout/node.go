package layout

import (
	"context"
	"log/slog"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/effect"
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
	// picture into the rectangle r, in the caller's coordinates in points.
	// They may paint past r, as the shadow, glow or reflection of an
	// Office drawing reaches past its box; the engine places and breaks
	// the picture by r alone, clips it to its content box only when
	// object-fit moves r off that box, and tiles a background or a border
	// image at r's size, whose fragments keep within r. Their group
	// nesting is at most MaxVectorDepth. The engine copies a fragment
	// before changing it, and calls Vector from its own goroutine with its
	// context.
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
	// Gradient, when set, makes the picture a CSS gradient the engine
	// paints over r (CSS Images 3, 3), Source and Vector being nil: an
	// image with no natural size or ratio, which sets the three flags
	// above and holds in Width and Height the size it takes when nothing
	// else sizes it.
	Gradient *css.Gradient
	// HasBaseline gives the picture a baseline of its own, Baseline
	// points below its top edge, as a formula has one (MathML Core,
	// 3.1.1): an inline-level replaced box of the picture sits on its
	// line by that baseline, below its top border and padding, rather
	// than by its bottom margin edge (CSS 2.1, 10.8.1). The picture is
	// drawn unscaled from the top of the box's content edge.
	HasBaseline bool
	Baseline    float64
	// Embed, when set, makes the picture a document laid out on its own,
	// the content of a frame (HTML Standard, 4.8.5): Source, Vector and
	// Gradient are nil, the three NoNatural flags are set and Width and
	// Height hold the default object size, 300 by 150 CSS pixels (CSS 2.1,
	// 10.3.2 and 10.6.2). The engine calls it from its own goroutine with
	// its context and the size in points of the box's content box, its
	// viewport, and paints what it returns there, clipped to it:
	// object-fit and object-position do not apply. A nil result paints
	// nothing.
	Embed func(ctx context.Context, w, h float64) *Embedded
	// view, when set, is the part of the picture an object-view-box makes
	// its whole: Width and Height are then the part's size.
	view *viewBox
}

// Embedded is a document laid out on its own, as [Picture.Embed] returns
// it, in the coordinates of its viewport: the origin at the top left of
// the content box it paints into.
type Embedded struct {
	// Content paints the document. Its leaves hang from the elements of
	// Structure, or from none for an artifact; their group nesting is at
	// most [MaxEmbedDepth].
	Content []typeset.Fragment
	// Links are the document's links with a URI; a region is clipped to
	// the viewport as the content is, and each hangs from its Link
	// element of Structure.
	Links []typeset.Link
	// Structure is the root of the document's structure tree, nil for
	// none: its kids hang from the element of the box the document paints
	// into, which takes its Lang when it has none.
	Structure *typeset.StructElem
}

// MaxEmbedDepth is the deepest group nesting an [Embedded] may hold: 10,
// the room [MaxVectorDepth] leaves a vector picture, twice, since a frame
// holds a page of its own with its groups. Anti-DoS guard, not
// spec-mandated: the typeset model bounds the groups of a page, and a
// page of frames nested a few deep keeps under it. A deeper document is
// left out with a warning.
const MaxEmbedDepth = 2 * MaxVectorDepth

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
	// or the top of the document under [Options.BrowserRules] when the
	// fragment says so; anything else is an external URI the converter has
	// already resolved.
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
	// with copies of the string aligned on the line's end edge. The
	// copies paint as a layout artifact.
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
	// Comment, set on an inline Element of Role Annot, marks its content as
	// the text a comment covers: on each page the element reaches, the
	// engine places the comment over the box of each line that holds part
	// of the element (typeset.CommentMark Lines), and an empty element at
	// its point on its line (CommentMark At), its Annot element the
	// comment's structure element. A comment covering text in several
	// paragraphs marks one element in each. The comment draws nothing and
	// changes no layout: the element is laid out as one without it.
	Comment *typeset.Comment
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
	// Fieldset says which part of a fieldset element's boxes an Element
	// is (HTML Standard, 15.3.13), FieldsetNone for none. See
	// [FieldsetPart].
	Fieldset FieldsetPart
	// Generated reports the box of a ::before or ::after pseudo-element,
	// whose originating element is the node it is a child of: its implicit
	// anchor element (CSS Anchor Positioning 1, 2.3.1).
	Generated bool
}

// FieldsetPart is the part a node plays in the boxes of a fieldset
// element (HTML Standard, 15.3.13).
type FieldsetPart uint8

// The fieldset parts.
const (
	// FieldsetNone is a node of no fieldset part.
	FieldsetNone FieldsetPart = iota
	// FieldsetBox is the box of a fieldset: a block container that
	// establishes a block formatting context, of children its rendered
	// legend, when it has one, then its content box. The engine centres
	// the legend's border box on the block-start border and gives the
	// content box the room below the legend's margin box; the border is
	// drawn that much lower when the legend is taller than it, its
	// background with it, and not behind the legend. Its padding is laid
	// out on its content box.
	FieldsetBox
	// FieldsetLegend is the rendered legend of a FieldsetBox, its first
	// child, a block-level Element. Its width is the fit-content width
	// in the fieldset's content box when its Style gives none; auto
	// margins place it in that box as they place a block, and its
	// text-align otherwise: start, end, left, right or centre. Its
	// block-start margin is not laid out.
	FieldsetLegend
	// FieldsetContent is the anonymous fieldset content box of a
	// FieldsetBox, its last child: it takes the fieldset's padding as its
	// own and its negation as its margins, so its padding box covers the
	// fieldset's below the legend.
	FieldsetContent
)

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
	// document out on one page this way, on a box as tall as a single
	// page may be.
	FitFirstPage bool
	// FittedLimit, when positive, lays out pages sized from their content:
	// a page box side may pass the 14,400 points of ISO 32000-1, Annex
	// C.2 up to it, and every page is [typeset.Page.Fitted]. The converter
	// sets it to [typeset.FittedPageLimit] of the file's version. Zero
	// keeps every side within 14,400 points.
	FittedLimit float64
	// ViewportHeight, when positive, is the height in points of the initial
	// containing block, which otherwise is the page area (CSS 2.1, 10.1):
	// the root's percentage heights and the boxes positioned in the
	// initial containing block measure it, while the flow is still cut
	// at the page area. A converter that lays a single page out on a box
	// taller than the paper sets the height of the paper's page area here.
	// Its width stays the page area's.
	ViewportHeight float64
	// PixelFontMetrics rounds the ascent, descent and line gap of every
	// face to whole CSS pixels before the line boxes use them, and lays a
	// line-height length or number out on Blink's layout unit of 1/64 px,
	// as Blink does for document text (see pixelMetrics and
	// blinkLineHeight); off, the faces' fractional metrics and the
	// computed line-height apply, as WeasyPrint uses them.
	PixelFontMetrics bool
	// BrowserRules applies the rendering rules of the browser engine the
	// HTML converter follows, Blink, where CSS leaves the drawing to the
	// user agent: the shades of the groove, ridge, inset and outset borders
	// ([shades]), the shapes of the outside disc, circle and square list
	// markers ([symbolRect]), the position and thickness of an auto
	// underline, the decorations of text inside a decorating box of
	// another size, and the page area cutting what overflows into the side
	// margins. It also reads a link's fragment as a browser does: "#" and
	// a "#top" no ID matches go to the top of the document (HTML Standard,
	// 7.4.6.4). Off, as the Office converters leave it, the engine's own
	// rules apply: a darker shade of every colour, the marker's text, the
	// face's underline position, content drawn into the side margins, a
	// link that names an element or nothing.
	BrowserRules bool
	// FeatureValues, when not nil, returns the @font-feature-values of a
	// family, nil for none: font-variant-alternates resolves its names
	// against those of the family of the face a run is drawn in (CSS
	// Fonts 4, 6.9).
	FeatureValues func(family string) *css.FontFeatureValues
	// PaletteValues, when not nil, returns the @font-palette-values rules
	// of a name in cascade order, nil for none: font-palette resolves a
	// named palette against the last that lists the family of the face a
	// run is drawn in (CSS Fonts 4, 9.2).
	PaletteValues func(name string) []*css.PaletteValues
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
	// PositionChoices holds, for the anchored boxes of an earlier layout
	// of the same tree, the position option each took ([Result]
	// PositionChoices): the last successful position option, which a box
	// keeps while it fits (CSS Anchor Positioning 1, 5.4.1.1). nil for a
	// first layout.
	PositionChoices map[int]int
	// Principal is the style whose writing-mode and direction are the
	// principal writing mode, the used values of the root and so of the
	// initial containing block (CSS Writing Modes 3, 8): an HTML body's,
	// which the converter names; nil for the root's own.
	Principal *css.Style
	// PageGroups tracks the page groups of [PageInfo.Groups], which
	// [Options.PageFor] and [Options.MarginContent] then receive; off,
	// every page belongs to none.
	PageGroups bool
	// Paginated, when not nil, receives the info of every page of the
	// layout, in order, right after Placed, so that page-margin content
	// can follow the pages an anchored node landed on and those before it
	// (CSS Generated Content 3, 6: a cross-reference to a later page).
	// Layout calls it once, from its own goroutine.
	Paginated func(pages []PageInfo)
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
	// FilterSource returns the SVG filter element url names, for an
	// element whose border box is box, in the same coordinates (Filter
	// Effects 1, 6 and 7). found reports that url names a filter element.
	// nil names none.
	FilterSource func(ctx context.Context, url string, box typeset.Rect) (f *FilterRef, found bool)
	// MaxPages bounds the pages laid out; past it Layout fails with
	// [limits.ErrTooManyPages]. Zero means [DefaultMaxPages].
	MaxPages int
	// Logger receives diagnostics, nil for none.
	Logger *slog.Logger
	// EffectPixels, when not nil, counts the pixels of the Office text
	// effects ([css.Office.TextEffects]) this layout draws together with
	// those of the other layouts that share it, against
	// [effect.MaxPagePixels]: a converter that lays out each text body of
	// a page apart keeps one count for the page. nil counts each painting
	// of the layout apart. Layout uses it from its own goroutine.
	EffectPixels *effect.Page
	// FirstLine styles the first formatted line of the block containers
	// that have a ::first-line (CSS Pseudo 4, 2.1); nil styles none.
	FirstLine FirstLineStyler
	// ChargedStyle, when not nil, reports whether the conversion's memory
	// budget accounts a style already, for as long as the layout lives:
	// a box that holds such a style charges nothing for it. nil reports
	// none, and the layout charges every distinct style its boxes hold
	// ([limits.StyleCost]). Layout calls it from its own goroutine.
	ChargedStyle func(st *css.Style) bool
	// MeasureContainers asks for [Result.Containers].
	MeasureContainers bool
	// PageColor fills every page, its whole sheet, before anything else
	// is painted on it: the canvas surface a converter's colour scheme
	// gives (CSS Color Adjustment 1, 2.2). Transparent fills nothing.
	PageColor css.Color
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
	// Groups are the page groups the page belongs to, outermost first
	// (CSS GCPM 3, 3.2): those of the in-flow block-level boxes whose page
	// property names a page and whose break-before forces a page break.
	// A group holds the pages from the one its box starts on to the one
	// it ends on, and stops at a forced break before a box outside it.
	Groups []css.PageGroup
	// Height is the height in points of the page box the page is
	// assembled with, which [Options.FitFirstPage] trims on the first
	// page: set for [Options.MarginContent] and [Options.MarginLinks], so
	// that content placed from the bottom edge sits on the trimmed page;
	// 0 for [Options.PageFor], which sizes the box.
	Height float64
}

// sameInfo reports whether a and b name a page alike.
func sameInfo(a, b PageInfo) bool {
	return a.Index == b.Index && a.Name == b.Name && a.Blank == b.Blank && slices.Equal(a.Groups, b.Groups)
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

// PositionOption is the entry of position-try-fallbacks an anchored box
// took: its index, from 1, and its accepted @position-try properties.
type PositionOption struct {
	Entry int
	Try   *css.TryStyle
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
	// 3, 2.2): the border boxes and line contents of every box where its
	// relative offsets and transforms put them, an empty border box
	// moved by them adding nothing, those inside a box that clips its
	// overflow left out, what a page of another width holds scaled to the
	// first page's area width. It is at
	// least zero and exceeds the area's width when content overflows its
	// page, which a converter shrinks to fit as browsers print.
	ContentWidth float64
	// PositionChoices holds the position option each anchored box took,
	// 0 for its base styles and i for the i-th entry of its
	// position-try-fallbacks, keyed by the order the layout generated its
	// box in, which a tree prepared again from the same document keeps.
	// nil when no box tried one.
	PositionChoices map[int]int
	// PositionOptions maps the node of each anchored box that took an
	// entry of its position-try-fallbacks to that entry, which the
	// element's computed values hold once a converter cascades it again
	// ([css.ComputeOptions.PositionOption]); nil when every such box kept
	// its base styles.
	PositionOptions map[*Node]PositionOption
	// Extents maps every non-empty Node.ID of a block-level, inline-block
	// or replaced box that landed on a page to its border box less its
	// padding, the box that holds its content and borders, on the page
	// its first box landed on, in that page's coordinates. Inline boxes
	// are not listed.
	Extents map[string]typeset.Rect
	// Containers maps every node whose style is a container for size
	// queries ([css.Style.SizeContainer]) and whose first box is a laid
	// out block-level, inline-block or replaced box to the width and
	// height of that box's content box, in points, the block-axis size of
	// an inline-size container 0, when
	// [Options.MeasureContainers] asks; nil otherwise. A box split across
	// pages or columns has one inline size, the one it is laid out at in
	// its first fragment (CSS Conditional 5, 6.1).
	Containers map[*Node][2]float64
}

// DefaultMaxPages is the page cap when Options.MaxPages is zero: the
// default of Limits.MaxConvertPages.
const DefaultMaxPages = limits.DefaultMaxConvertPages

// Layout lays root out on pages. The output depends on root, fonts' index
// and opts alone. It returns ctx's error when ctx ends first.
func Layout(ctx context.Context, root *Node, fonts *text.Fonts, opts Options) (*Result, error) {
	return layout(ctx, root, fonts, opts)
}
