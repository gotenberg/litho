package layout

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
)

// genCtx is what box generation hands down the content tree.
type genCtx struct {
	elem        *compose.StructElem
	structDepth int
	link        *link
	lang        string
	decos       []deco
	depth       int
}

// generate builds the box tree of root (CSS 2.1, 9.2). The root box is a
// block box whatever the root's display (CSS 2.1, 9.7).
func (e *engine) generate(root *Node) *box {
	ctx := genCtx{elem: e.root, structDepth: 1, lang: e.opts.Lang}
	st := root.Style
	if st == nil {
		st = css.Initial()
	}
	rb := e.newBox(root, st, kindBlock, ctx)
	rb.first, rb.last = true, true
	if root.Kind != Element {
		// A root that is not an element sits in an anonymous root block.
		rb.node = nil
		rb.anon = true
		rb.kids = e.genNode(root, rb, ctx, new(int))
		e.normalize(rb)
		return rb
	}
	if st.Display == css.DisplayNone {
		return rb
	}
	cctx := e.enter(root, rb, ctx)
	rb.elem = cctx.elem
	e.genChildren(rb, root.Children, cctx)
	return rb
}

// newBox returns a box of node with style st, counting it against
// [MaxBoxes].
func (e *engine) newBox(n *Node, st *css.Style, kind boxKind, ctx genCtx) *box {
	e.boxes++
	if e.boxes > MaxBoxes {
		e.fail(fmt.Errorf("%w: more than %d", ErrTooManyBoxes, MaxBoxes))
	}
	b := &box{node: n, st: st, kind: kind, first: true, last: true, lang: ctx.lang, elem: ctx.elem, link: ctx.link, depth: ctx.depth}
	b.hidden = st.Visibility != css.Visible
	if n != nil && n.ID != "" {
		b.id = n.ID
	}
	return b
}

// enter returns the context the children of n's box b see: its structure
// element, link, language and propagated decorations.
func (e *engine) enter(n *Node, b *box, ctx genCtx) genCtx {
	out := ctx
	out.depth = ctx.depth + 1
	if n.Lang != "" {
		out.lang = n.Lang
		b.lang = n.Lang
	}
	if n.Role != "" {
		switch {
		case n.Role == "Document" && ctx.elem == e.root && ctx.structDepth == 1:
			if e.root.Lang == "" && n.Lang != "" {
				e.root.Lang = n.Lang
			}
		case ctx.structDepth >= maxStructDepth:
			e.warnOnce(codeStructTooDeep, fmt.Sprintf("structure roles nested deeper than %d hang from the deepest element", maxStructDepth), -1)
		default:
			el := &compose.StructElem{Role: n.Role, Lang: n.Lang}
			if n.Alt != "" && (n.Kind == Replaced || n.Role == "Figure" || n.Role == "Link") {
				el.Alt = n.Alt
			}
			if ctx.elem != nil {
				ctx.elem.Kids = append(ctx.elem.Kids, el)
			}
			out.elem = el
			out.structDepth = ctx.structDepth + 1
		}
	}
	b.elem = out.elem
	if n.Href != "" {
		l := &link{href: n.Href, node: n}
		if out.elem != nil && out.elem.Role == "Link" {
			l.elem = out.elem
		}
		out.link = l
		b.link = l
	}
	if b.st.TextDecoration != 0 {
		out.decos = append(slices.Clip(ctx.decos), newDeco(b.st))
	}
	return out
}

// genChildren generates the boxes of children into the block container b,
// wraps its runs of table boxes in anonymous tables and its inline runs in
// anonymous blocks.
func (e *engine) genChildren(b *box, children []*Node, ctx genCtx) {
	e.adopt(b, e.genRaw(b, children, ctx))
}

// genNode generates the boxes of n, a child of parent: none, one, or for
// an inline element that holds a block, the pieces of the inline box with
// the blocks between them (CSS 2.1, 9.2.1.1). ordinal is the number of the
// last list item among n's siblings.
func (e *engine) genNode(n *Node, parent *box, ctx genCtx, ordinal *int) []*box {
	if n == nil || e.err != nil {
		return nil
	}
	if ctx.depth >= maxTreeDepth {
		e.warnOnce(codeTooDeep, fmt.Sprintf("content nested deeper than %d levels left out", maxTreeDepth), -1)
		return nil
	}
	st := n.Style
	if st == nil {
		st = parent.st
	}
	switch n.Kind {
	case Text:
		if n.Text == "" {
			return nil
		}
		b := e.newBox(n, st, kindText, ctx)
		b.inline = true
		b.text = n.Text
		b.decos = ctx.decos
		return []*box{b}
	case LineBreak:
		b := e.newBox(n, st, kindBreak, ctx)
		b.inline = true
		return []*box{b}
	case Replaced:
		if st.Display == css.DisplayNone || isColumn(st.Display) {
			return nil
		}
		b := e.newBox(n, st, kindReplaced, ctx)
		e.enter(n, b, ctx)
		b.abs = isAbs(st)
		b.float = st.Float
		b.inline = b.float == css.FloatNone && isInlineLevel(st.Display)
		if validPicture(n.Picture) {
			b.picture = n.Picture
		}
		return []*box{b}
	case Element:
	default:
		return nil
	}
	if st.Display == css.DisplayNone {
		return nil
	}
	if isColumn(st.Display) {
		// A column generates a box inside a table alone, where it paints
		// its background (CSS 2.1, 17.2).
		if parent == nil || parent.tpart != partTable {
			return nil
		}
		return []*box{e.genColumn(n, st, ctx)}
	}
	float := st.Float
	switch {
	case float == css.FloatNone && (st.Display == css.Inline || st.Display == css.InlineListItem):
		return e.genInline(n, st, ctx, ordinal)
	case st.Display == css.Table || st.Display == css.InlineTable:
		w := e.genTable(n, st, ctx, float)
		w.abs = isAbs(st)
		return []*box{w}
	case float == css.FloatNone && isTablePart(st.Display):
		return []*box{e.genPart(n, st, ctx)}
	case float == css.FloatNone && st.Display == css.InlineBlock:
		b := e.newBox(n, st, kindBlock, ctx)
		b.inline = true
		cctx := e.enter(n, b, ctx)
		cctx.decos = ownDecos(b.st)
		e.genChildren(b, n.Children, cctx)
		return []*box{b}
	}
	b := e.newBox(n, st, kindBlock, ctx)
	b.float = float
	b.abs = isAbs(st)
	cctx := e.enter(n, b, ctx)
	if float != css.FloatNone || b.abs {
		cctx.decos = ownDecos(b.st)
	}
	if st.Display == css.ListItem {
		*ordinal++
		if n.ListValue != 0 {
			*ordinal = n.ListValue
		}
		cctx = e.genMarker(n, b, cctx, *ordinal, false)
	}
	e.genChildren(b, n.Children, cctx)
	return []*box{b}
}

// validPicture reports a picture the engine can draw: a source and a
// finite, positive intrinsic size.
func validPicture(p *Picture) bool {
	return p != nil && p.Source != nil && p.Width > 0 && p.Height > 0 && !math.IsInf(p.Width, 0) && !math.IsInf(p.Height, 0)
}

// genInline generates an inline element: its inline box, split around the
// block-level boxes among its descendants (CSS 2.1, 9.2.1.1).
func (e *engine) genInline(n *Node, st *css.Style, ctx genCtx, ordinal *int) []*box {
	ib := e.newBox(n, st, kindInline, ctx)
	ib.inline = true
	cctx := e.enter(n, ib, ctx)
	ib.decos = cctx.decos
	if st.Display == css.InlineListItem {
		*ordinal++
		if n.ListValue != 0 {
			*ordinal = n.ListValue
		}
		cctx = e.genMarker(n, ib, cctx, *ordinal, true)
	}
	out := []*box{ib}
	cur := ib
	kids := make([]*box, 0, len(n.Children))
	for _, c := range n.Children {
		kids = append(kids, e.genNode(c, ib, cctx, ordinal)...)
	}
	for _, k := range e.wrapOrphans(ib, kids, true) {
		if k.inline || k.float != css.FloatNone || k.abs {
			k.parent = cur
			cur.kids = append(cur.kids, k)
			continue
		}
		cur.last = false
		out = append(out, k)
		piece := e.newBox(n, st, kindInline, ctx)
		*piece = *ib
		piece.kids = nil
		piece.first, piece.last = false, true
		piece.id = ""
		out = append(out, piece)
		cur = piece
	}
	return out
}

// ownDecos returns the decorations a box that does not inherit them, a
// float or an inline block, draws on its own text.
func ownDecos(st *css.Style) []deco {
	if st.TextDecoration == 0 {
		return nil
	}
	return []deco{newDeco(st)}
}

// isAbs reports an absolutely positioned style: absolute or fixed, out of
// flow (CSS 2.1, 9.6).
func isAbs(st *css.Style) bool {
	return st.Position == css.Absolute || st.Position == css.Fixed
}

// isInlineLevel reports whether a display value is inline-level.
func isInlineLevel(d css.Display) bool {
	return d == css.Inline || d == css.InlineBlock || d == css.InlineTable || d == css.InlineListItem
}

// isTablePart reports a display value of a caption or an internal table
// box other than a column (CSS 2.1, 17.2).
func isTablePart(d css.Display) bool {
	return d == css.TableRowGroup || d == css.TableHeaderGroup || d == css.TableFooterGroup ||
		d == css.TableRow || d == css.TableCell || d == css.TableCaption
}

// isColumn reports a table column or column group, which generates no box
// for content (CSS 2.1, 17.2).
func isColumn(d css.Display) bool {
	return d == css.TableColumn || d == css.TableColumnGroup
}

// normalize wraps every run of inline-level children of the block
// container b in an anonymous block when b also holds in-flow block-level
// children (CSS 2.1, 9.2.1.1), and marks b as establishing an inline
// formatting context otherwise. Floats and absolutely positioned boxes
// join the run they sit in; a run of them alone stays in the block flow.
func (e *engine) normalize(b *box) {
	hasBlock, hasInline := false, false
	for _, k := range b.kids {
		switch {
		case k.float != css.FloatNone || k.abs:
		case k.inline:
			hasInline = true
		default:
			hasBlock = true
		}
	}
	if !hasBlock {
		b.inlineKids = hasInline
		return
	}
	if !hasInline {
		return
	}
	var kids []*box
	for i := 0; i < len(b.kids); {
		k := b.kids[i]
		if !k.inline && k.float == css.FloatNone && !k.abs {
			kids = append(kids, k)
			i++
			continue
		}
		j := i
		inl := false
		for j < len(b.kids) && (b.kids[j].inline || b.kids[j].float != css.FloatNone || b.kids[j].abs) {
			inl = inl || b.kids[j].inline
			j++
		}
		if !inl {
			kids = append(kids, b.kids[i:j]...)
			i = j
			continue
		}
		a := &box{
			st: anonStyle(b.st), kind: kindBlock, anon: true, first: true, last: true, parent: b,
			lang: b.lang, elem: b.elem, link: b.link, depth: b.depth + 1, inlineKids: true,
		}
		e.boxes++
		for _, c := range b.kids[i:j] {
			c.parent = a
			a.kids = append(a.kids, c)
		}
		kids = append(kids, a)
		i = j
	}
	b.kids = kids
}

// anonStyle returns the style of an anonymous block inside a box of style
// parent: the inherited properties of the parent and the initial values of
// the others (CSS 2.1, 9.2.1.1).
func anonStyle(parent *css.Style) *css.Style {
	st := css.Initial()
	st.Display = css.Block
	st.Visibility = parent.Visibility
	st.Color = parent.Color
	st.FontFamilies = parent.FontFamilies
	st.FontSize = parent.FontSize
	st.FontWeight = parent.FontWeight
	st.FontStyle = parent.FontStyle
	st.FontVariant = parent.FontVariant
	st.LineHeight = parent.LineHeight
	st.TextAlign = parent.TextAlign
	st.TextIndent = parent.TextIndent
	st.TextTransform = parent.TextTransform
	st.LetterSpacing = parent.LetterSpacing
	st.WordSpacing = parent.WordSpacing
	st.WhiteSpace = parent.WhiteSpace
	st.WordBreak = parent.WordBreak
	st.OverflowWrap = parent.OverflowWrap
	st.TabSize = parent.TabSize
	st.Direction = parent.Direction
	st.ListStyleType = parent.ListStyleType
	st.ListStylePosition = parent.ListStylePosition
	st.ListStyleImage = parent.ListStyleImage
	st.BorderCollapse = parent.BorderCollapse
	st.BorderSpacing = parent.BorderSpacing
	st.CaptionSide = parent.CaptionSide
	st.EmptyCells = parent.EmptyCells
	st.Orphans = parent.Orphans
	st.Widows = parent.Widows
	return st
}

// genMarker generates the marker of the list item b numbered ordinal
// (CSS 2.1, 12.5): an outside marker box, or an inside marker as the first
// inline child. When the item's role is LI, the marker hangs from a Lbl
// element and the content from a LBody (ISO 32000-1, 14.8.4.3.3); it
// returns the context of the item's children. inside forces an inside
// marker, for an inline list item.
func (e *engine) genMarker(n *Node, b *box, ctx genCtx, ordinal int, inside bool) genCtx {
	st := b.st
	var pic *Picture
	var label, alt string
	switch m := n.Marker; {
	case m != nil && len(m.Children) > 0:
		if m.Style != nil {
			st = m.Style
		}
		var sb strings.Builder
		for _, c := range m.Children {
			switch {
			case c.Kind == Text:
				sb.WriteString(c.Text)
			case c.Kind == Replaced && pic == nil && validPicture(c.Picture):
				pic = c.Picture
			}
		}
		label, alt = sb.String(), m.Alt
		if pic != nil && alt == "" {
			alt = label
		}
	default:
		if m != nil && m.Style != nil {
			st = m.Style
		}
		pic = e.picture(st.ListStyleImage, -1)
		label = MarkerText(st, ordinal)
		alt = markerAlt(st, ordinal)
	}
	if pic == nil && label == "" {
		return ctx
	}
	markerElem := ctx.elem
	if n.Role == "LI" && ctx.elem != nil && ctx.elem.Role == "LI" && ctx.structDepth < maxStructDepth {
		lbl := &compose.StructElem{Role: "Lbl"}
		body := &compose.StructElem{Role: "LBody"}
		if pic != nil {
			lbl.ActualText = alt
		}
		ctx.elem.Kids = append(ctx.elem.Kids, lbl, body)
		markerElem = lbl
		ctx.elem = body
		ctx.structDepth++
	}
	if b.st.ListStylePosition == css.Outside && !inside {
		b.outside = &markerBox{text: label, picture: pic, elem: markerElem, st: st}
		return ctx
	}
	mctx := ctx
	mctx.elem = markerElem
	var m *box
	if pic != nil {
		m = e.newBox(nil, markerStyle(st), kindReplaced, mctx)
		m.picture = pic
	} else {
		m = e.newBox(nil, markerStyle(st), kindText, mctx)
		m.text = label
	}
	m.inline = true
	m.marker = true
	m.parent = b
	b.kids = append(b.kids, m)
	return ctx
}

// markerStyle returns the style of a list item's marker: the item's, as an
// inline box with no decoration of its own.
func markerStyle(item *css.Style) *css.Style {
	st := *item
	st.Display = css.Inline
	st.Float = css.FloatNone
	st.Position = css.Static
	st.ZIndex = css.ZIndex{Auto: true}
	st.Transform = nil
	st.Margin = [4]css.Dimension{css.Pts(0), css.Pts(0), css.Pts(0), css.Pts(0)}
	st.Padding = st.Margin
	st.Border = [4]css.Border{}
	st.BackgroundColor = css.Transparent
	st.BackgroundImage = ""
	st.BackgroundGradient = nil
	st.Width, st.Height = css.Dimension{}, css.Dimension{}
	st.MinWidth, st.MinHeight = css.Pts(0), css.Pts(0)
	st.MaxWidth, st.MaxHeight = css.Dimension{Kind: css.None}, css.Dimension{Kind: css.None}
	st.Opacity = 1
	st.TextIndent = css.Pts(0)
	st.TextTransform = css.TransformNone
	return &st
}
