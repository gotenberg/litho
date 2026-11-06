package layout

import (
	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
)

// boxKind says what a [box] is.
type boxKind uint8

const (
	// kindBlock is a block container: a block, a list item, an inline
	// block, a float, or an anonymous block wrapping inline content.
	kindBlock boxKind = iota
	// kindInline is a non-replaced inline box.
	kindInline
	// kindText is a run of characters inside an inline formatting context.
	kindText
	// kindBreak is a forced line break.
	kindBreak
	// kindReplaced is a replaced box, inline-level or block-level.
	kindReplaced
)

// link is the target of an Href subtree.
type link struct {
	href string
	// elem is the Link structure element of the subtree, nil when it has
	// none.
	elem *compose.StructElem
	// node is the node the Href sits on.
	node *Node
}

// deco is one text decoration a box draws on its text (CSS 2.1, 16.3.1).
type deco struct {
	lines css.TextDecoration
	color css.Color
}

// box is one box of the box tree: generated once from the content tree,
// laid out as many times as the engine needs, and painted once.
type box struct {
	node *Node
	st   *css.Style
	kind boxKind
	// inline reports an inline-level box: an inline box, a text run, a
	// break, an inline block or an inline replaced box.
	inline bool
	// float is the float side, FloatNone for a box in flow.
	float css.Float
	// anon reports an anonymous block box (CSS 2.1, 9.2.1.1).
	anon bool
	// inlineKids reports a block container whose children are all
	// inline-level (or floats), which establishes an inline formatting
	// context.
	inlineKids bool
	// first and last are false on the pieces of an inline box split
	// around a block (CSS 2.1, 9.2.1.1) that do not start or end it: they
	// draw no start or end edge.
	first, last bool
	parent      *box
	kids        []*box
	// text is the characters of a text box; marker reports a marker's text
	// or image box, whose white space is preserved.
	text   string
	marker bool
	// lang is the language in force.
	lang string
	// elem is the structure element the box's fragments hang from, nil for
	// artifacts.
	elem *compose.StructElem
	link *link
	// decos are the text decorations propagated to the box's text.
	decos []deco
	// id is the anchor the box records, "" for none: only the first box of
	// a node carries its ID.
	id string
	// outside is the outside marker of a list item, nil for none.
	outside *markerBox
	// hidden reports visibility: hidden or collapse: laid out, not painted.
	hidden bool
	// depth is the nesting depth in the box tree.
	depth int

	// Layout results, in flow coordinates: x and y are the border box's
	// top-left corner, w and h the content box's size.
	x, y, w, h float64
	m, bd, pd  [4]float64
	lines      []*lineBox
	// baseline is the offset of the last line box's baseline from the
	// border box top, for an inline block; hasBaseline says whether there
	// is one.
	baseline    float64
	hasBaseline bool
	laidOut     bool
	// anchor is where the box's first fragment landed, for Result.Anchors.
	anchorX, anchorY float64
	anchorSet        bool
	// ifc is the shaped inline content of a block container.
	ifc *ifcData
	// intrinsic caches the min- and max-content border box widths.
	minW, maxW float64
	measured   bool
	// picture is the picture of a replaced box, nil for none.
	picture *Picture
}

// markerBox is the outside marker of a list item (CSS 2.1, 12.5).
type markerBox struct {
	text    string
	picture *Picture
	elem    *compose.StructElem
	// Layout results: the marker's left edge and baseline, flow
	// coordinates.
	x, baseline float64
	// glyph runs, laid out relative to the marker's baseline origin.
	runs   []*compose.Text
	width  float64
	ascent float64
	placed bool
}

// contentX returns the left edge of the content box.
func (b *box) contentX() float64 { return b.x + b.bd[css.Left] + b.pd[css.Left] }

// contentY returns the top edge of the content box.
func (b *box) contentY() float64 { return b.y + b.bd[css.Top] + b.pd[css.Top] }

// borderW returns the border box width.
func (b *box) borderW() float64 {
	return b.w + b.bd[css.Left] + b.bd[css.Right] + b.pd[css.Left] + b.pd[css.Right]
}

// borderH returns the border box height.
func (b *box) borderH() float64 {
	return b.h + b.bd[css.Top] + b.bd[css.Bottom] + b.pd[css.Top] + b.pd[css.Bottom]
}

// marginW returns the margin box width.
func (b *box) marginW() float64 { return b.borderW() + b.m[css.Left] + b.m[css.Right] }

// marginH returns the margin box height.
func (b *box) marginH() float64 { return b.borderH() + b.m[css.Top] + b.m[css.Bottom] }

// establishesBFC reports whether a block-level box establishes a new block
// formatting context (CSS 2.1, 9.4.1): floats, inline blocks, boxes with
// overflow other than visible, and the root.
func (b *box) establishesBFC() bool {
	return b.parent == nil || b.float != css.FloatNone || (b.inline && b.kind == kindBlock) ||
		b.st.Overflow != css.OverflowVisible || b.kind == kindReplaced
}

// isStackingContext reports whether the box paints as a unit above the
// in-flow content of its parent: an opacity below 1 (CSS Color 3, 3.2) or
// relative positioning (CSS 2.1, Appendix E, step 8).
func (b *box) isStackingContext() bool {
	return b.parent != nil && (b.st.Opacity < 1 || b.st.Position == css.Relative) && b.kind != kindText && b.kind != kindInline
}

// shift moves the box and everything laid out inside it by (dx, dy).
func (b *box) shift(dx, dy float64) {
	if dx == 0 && dy == 0 {
		return
	}
	b.x += dx
	b.y += dy
	b.anchorX += dx
	b.anchorY += dy
	for _, l := range b.lines {
		l.x += dx
		l.y += dy
		for _, it := range l.items {
			if it.atomic != nil {
				it.atomic.shift(dx, dy)
			}
		}
	}
	if b.outside != nil {
		b.outside.x += dx
		b.outside.baseline += dy
	}
	for _, k := range b.kids {
		switch k.kind {
		case kindBlock, kindReplaced:
			if !k.inline {
				k.shift(dx, dy)
			}
		case kindInline:
			k.shiftInline(dx, dy)
		case kindText, kindBreak:
			k.anchorX += dx
			k.anchorY += dy
		}
	}
}

// shiftInline moves the anchors of an inline box and the blocks laid out
// inside it: its floats, whose positions are absolute.
func (b *box) shiftInline(dx, dy float64) {
	b.anchorX += dx
	b.anchorY += dy
	for _, k := range b.kids {
		switch {
		case k.kind == kindInline:
			k.shiftInline(dx, dy)
		case k.float != css.FloatNone:
			k.shift(dx, dy)
		case k.kind == kindText || k.kind == kindBreak:
			k.anchorX += dx
			k.anchorY += dy
		}
	}
}
