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

// deco is one text decoration a box draws on its text (CSS 2.1, 16.3.1):
// the lines, colour, style, thickness and underline offset of the
// decorating box (CSS Text Decoration 4, 2 and 3).
type deco struct {
	lines css.TextDecoration
	color css.Color
	style css.DecorationStyle
	// thick is the thickness in points, autoThickness or fromFont.
	thick float64
	// offset is the underline offset below the baseline in points, valid
	// when hasOffset.
	offset    float64
	hasOffset bool
}

// The keyword thicknesses of a [deco].
const (
	autoThickness = -1
	fromFont      = -2
)

// newDeco returns the decoration a box of style st draws on its text.
func newDeco(st *css.Style) deco {
	d := deco{lines: st.TextDecoration, color: st.TextDecorationColor, style: st.TextDecorationStyle, thick: autoThickness}
	size := max(clamp(st.FontSize), 0)
	switch t := st.TextDecorationThickness; t.Kind {
	case css.Points, css.Percent:
		d.thick = max(resolve(t, size), 0)
	case css.Normal:
		d.thick = fromFont
	case css.Auto, css.None, css.Number, css.FlexContent:
	}
	if o := st.TextUnderlineOffset; o.Kind == css.Points || o.Kind == css.Percent {
		d.offset, d.hasOffset = resolve(o, size), true
	}
	return d
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
	// abs reports an absolutely or fixed positioned box, out of flow (CSS
	// 2.1, 9.6). sx and sy are its static position (CSS 2.1, 10.3.7 and
	// 10.6.4) in flow coordinates, the start edge of its hypothetical box,
	// srtl whether that start is the right edge, staticSet whether the
	// flow reached it. cbBox is its containing block's box, nil for the
	// initial one or the page; fixed reports a box positioned on the page,
	// which repeats on every page (CSS 2.1, 9.6.1).
	abs       bool
	sx, sy    float64
	srtl      bool
	staticSet bool
	cbBox     *box
	fixed     bool
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

	// tpart is the box's part in a table (CSS 2.1, 17.2), partNone outside
	// one. tdata is the table model of a table wrapper box and of its table
	// box, cell the grid slot of a cell.
	tpart tablePart
	tdata *tableData
	cell  *cellData

	// flex is the flex layout state of a flex container (CSS Flexbox 1),
	// nil for any other box; flexItem reports a flex item.
	flex     *flexData
	flexItem bool

	// grid is the grid layout state of a grid container (CSS Grid 1),
	// nil for any other box; gridItem reports a grid item.
	grid     *gridData
	gridItem bool
}

// markerBox is the outside marker of a list item (CSS 2.1, 12.5).
type markerBox struct {
	text    string
	picture *Picture
	elem    *compose.StructElem
	// st is the marker's style: the ::marker's, else the item's.
	st *css.Style
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
// formatting context (CSS 2.1, 9.4.1): floats, absolutely positioned
// boxes, inline blocks, boxes with
// overflow other than visible, flow-root boxes (CSS Display 3, 2.3), and
// the root; and the table wrapper boxes, captions and cells (CSS 2.1,
// 17.4).
func (b *box) establishesBFC() bool {
	return b.parent == nil || b.float != css.FloatNone || b.abs || (b.inline && b.kind == kindBlock) ||
		b.st.Overflow != css.OverflowVisible || b.kind == kindReplaced || b.st.Display == css.FlowRoot ||
		b.tpart == partWrapper || b.tpart == partCaption || b.tpart == partCell ||
		b.flex != nil || b.flexItem || b.grid != nil || b.gridItem
}

// layered reports whether the box paints in the positioned layers of its
// stacking context rather than with the in-flow content: a positioned
// box, or one that creates a stacking context (CSS 2.1, 9.9.1 and Appendix
// E, steps 2, 8 and 9), a flex item with a z-index among them (CSS
// Flexbox 1, 4.3).
func (b *box) layered() bool {
	if b.parent == nil || b.tpart.internal() || b.kind == kindText || b.kind == kindInline || b.kind == kindBreak {
		return false
	}
	return b.st.Position != css.Static || b.st.Opacity < 1 || b.transformed() || b.isItem() && !b.st.ZIndex.Auto
}

// isStackingContext reports whether the box creates a stacking context: a
// positioned box with an integer z-index (CSS 2.1, 9.9.1), a fixed box
// (CSS Position 3, 3.4), an opacity below 1 (CSS Color 3, 3.2) or a
// transform (CSS Transforms 1, 6).
func (b *box) isStackingContext() bool {
	if !b.layered() {
		return false
	}
	return (b.st.Position != css.Static || b.isItem()) && !b.st.ZIndex.Auto || b.st.Position == css.Fixed || b.st.Opacity < 1 || b.transformed()
}

// zLevel returns the stack level of a stacking context: its z-index, 0
// for one that a positioned z-index does not create.
func (b *box) zLevel() int {
	if b.st.Position == css.Static && !b.isItem() || b.st.ZIndex.Auto {
		return 0
	}
	return b.st.ZIndex.Level
}

// transformed reports a box its transform applies to: a block-level or
// atomic box, not a non-replaced inline box (CSS Transforms 1, 3).
func (b *box) transformed() bool {
	return len(b.st.Transform) > 0 && (b.kind == kindBlock || b.kind == kindReplaced) && !b.tpart.internal()
}

// shift moves the box and everything laid out inside it by (dx, dy).
func (b *box) shift(dx, dy float64) {
	if dx == 0 && dy == 0 {
		return
	}
	b.x += dx
	b.y += dy
	b.sx += dx
	b.sy += dy
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
	if b.tpart == partTable {
		// Column boxes hold no content and are no kids of the table box.
		for _, c := range b.tdata.colBoxes {
			c.shift(dx, dy)
		}
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
		case k.float != css.FloatNone || k.abs:
			k.shift(dx, dy)
		case k.kind == kindText || k.kind == kindBreak:
			k.anchorX += dx
			k.anchorY += dy
		}
	}
}
