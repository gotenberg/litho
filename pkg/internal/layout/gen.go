package layout

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// genCtx is what box generation hands down the content tree.
type genCtx struct {
	elem        *typeset.StructElem
	structDepth int
	// list is the nearest L element, nil outside a list.
	list *typeset.StructElem
	// item is the LI element the node just entered starts, nil for any
	// other node: the marker's Lbl joins it.
	item *typeset.StructElem
	// label reports content inside a Lbl element, which holds no block
	// (see [labelForbids]).
	label bool
	// form reports content inside a Form element, which starts no
	// element of its own.
	form  bool
	link  *link
	lang  string
	decos []deco
	depth int
	// vertical reports content inside an orthogonal flow, which keeps
	// the writing mode of its root, frame.
	vertical bool
	frame    css.WritingMode
}

// generate builds the box tree of root (CSS 2.1, 9.2). The root box is a
// block box whatever the root's display (CSS 2.1, 9.7).
func (e *engine) generate(root *Node) *box {
	ctx := genCtx{elem: e.root, structDepth: 1, lang: e.opts.Lang, vertical: e.turn != css.HorizontalTB, frame: e.turn}
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
	if st.Display == css.Table || st.Display == css.InlineTable {
		// A root table is a table wrapper box in the initial containing
		// block, as any block-level table is; inline-table computes to
		// table on the root (CSS 2.1, 9.7 and 17.4).
		w := e.genTable(root, st, ctx, css.FloatNone)
		w.first, w.last, w.inline = true, true, false
		w.abs = isAbs(st)
		return w
	}
	e.principalDirection(rb, root)
	cctx := e.enter(root, rb, ctx)
	rb.elem = cctx.elem
	// The root is absolutely positioned as any element is (CSS 2.1, 9.3.1;
	// CSS Position 3, 2), its containing block the initial one, or the
	// page area for a fixed root (10.1).
	rb.abs = isAbs(st)
	if st.Display == css.Flex {
		e.genFlex(rb, root.Children, cctx)
		return rb
	}
	if isGrid(st.Display) {
		e.genGrid(rb, root.Children, cctx)
		return rb
	}
	e.genChildren(rb, root.Children, cctx)
	return rb
}

// principalDirection gives the root box rb of the element root the
// direction of its body ([principalChild]), under the browser rules: CSS Writing Modes 4, 8 takes the used
// direction of the root from the body's computed one and leaves the
// root's computed value, which its children inherit, as it is.
func (e *engine) principalDirection(rb *box, root *Node) {
	if !e.opts.BrowserRules {
		return
	}
	if k := principalChild(root); k != nil && k.Style.Direction != rb.st.Direction {
		st := *rb.st
		st.Direction = k.Style.Direction
		e.holdStyle(&st)
		rb.st = &st
	}
}

// newBox returns a box of node with style st, counting it against
// [MaxBoxes].
func (e *engine) newBox(n *Node, st *css.Style, kind boxKind, ctx genCtx) *box {
	e.boxes++
	e.tick()
	if e.boxes > MaxBoxes {
		e.fail(fmt.Errorf("%w: more than %d", ErrTooManyBoxes, MaxBoxes))
	}
	e.charge(boxCost, "boxes")
	if n != nil && n.Control != nil {
		st = e.controlStyle(n.Control, st, ctx.lang)
	}
	if ctx.vertical {
		st = e.turnedStyle(st, ctx.frame)
	}
	e.holdStyle(st)
	b := &box{node: n, st: st, kind: kind, first: true, last: true, lang: ctx.lang, elem: ctx.elem, link: ctx.link, depth: ctx.depth}
	b.hidden = st.Visibility != css.Visible
	b.seq = e.boxes
	if n != nil && n.ID != "" {
		b.id = n.ID
	}
	return b
}

// enter returns the context the children of n's box b see: its structure
// element, link, language and propagated decorations. A nil b is an
// element of display: contents, which starts its structure element and
// language and draws no box: it makes no link and decorates nothing (see
// [engine.genContents]).
//
// An LI holds a Lbl and a LBody alone (ISO 32000-2, 14.8.4.8.3; ISO/TS
// 32005, Table 5): the item's content hangs from a LBody the LI starts
// with, and [engine.genMarker] puts the marker's Lbl before it. A Lbl or a
// LBody a node names as a list's child, a definition list's term or
// description, joins an LI of the list, one per group (see [listGroup]).
func (e *engine) enter(n *Node, b *box, ctx genCtx) genCtx {
	out := ctx
	out.depth = ctx.depth + 1
	out.item = nil
	if n.Lang != "" {
		out.lang = n.Lang
		if b != nil {
			b.lang = n.Lang
		}
	}
	role := n.Role
	if b == nil && role == "Link" {
		// A Link element holds a link annotation (ISO 32000-1, 14.8.4.4.2,
		// Table 340), which an element without a box does not make: its
		// content hangs from the enclosing element.
		role = ""
	}
	if ctx.label && labelForbids[role] {
		// A block inside a label, a definition term holding a paragraph,
		// starts no element: its content hangs from the Lbl.
		role = ""
	}
	if role == "" && n.ActualText != "" {
		// ActualText is an entry of a structure element (ISO 32000-1,
		// 14.9.4): a node without a role takes a Span to carry it.
		role = "Span"
	}
	if role == "" && n.Lang != "" && !strings.EqualFold(n.Lang, ctx.lang) && b != nil && (b.kind == kindInline || b.kind == kindText) {
		// So is a language: an inline node in a language other than the
		// text around it takes a Span stating it (ISO 32000-1, 14.9.2.2),
		// or the structure would not say what language its text is in
		// (ISO 14289-1, 7.2).
		role = "Span"
	}
	if ctx.form {
		// What a form control shows, a button's content, hangs from its
		// Form element, which ISO/TS 32005, Table 5 lets hold no
		// paragraph, span, link, heading, list item or other Form, and
		// which a reader reads as one field.
		role = ""
	}
	parent := ctx.elem
	switch {
	case parent == nil || parent.Role != "L" || role == "":
	case (role == "Lbl" || role == "LBody") && ctx.structDepth+1 < maxStructDepth:
		parent = listGroup(parent, role)
		out.structDepth++
	case !listKids[role] && ctx.structDepth+2 < maxStructDepth:
		parent = itemBody(parent)
		out.structDepth += 2
	}
	own := ctx.elem
	if role != "" {
		switch {
		case role == "Document" && ctx.elem == e.root && ctx.structDepth == 1:
			if e.root.Lang == "" && n.Lang != "" {
				e.root.Lang = n.Lang
			}
		case out.structDepth >= maxStructDepth:
			e.warnOnce(warn.LimitReached, warn.Approximated, fmt.Sprintf("structure roles nested deeper than %d hang from the deepest element", maxStructDepth), -1)
		default:
			el := &typeset.StructElem{Role: role, Lang: n.Lang, Scope: n.Scope, Numbering: n.Numbering, ActualText: n.ActualText}
			if n.Role == "TH" || n.Role == "TD" {
				// A span is the converter's count of the grid cells the
				// cell covers; past typeset.MaxCellSpan no table holds
				// that many.
				el.ColSpan = min(max(n.ColSpan, 0), typeset.MaxCellSpan)
				el.RowSpan = min(max(n.RowSpan, 0), typeset.MaxCellSpan)
			}
			if n.Role == "L" {
				out.list = el
			}
			if role == "Lbl" {
				out.label = true
			}
			if n.Role == "Form" {
				el.PrintField = n.PrintField
				out.form = true
			}
			if n.Alt != "" && (n.Kind == Replaced || role == "Figure" || role == "Link") {
				el.Alt = n.Alt
			}
			if parent != nil {
				parent.Kids = append(parent.Kids, el)
			}
			own = el
			out.elem = el
			out.structDepth++
			if role == "LI" && out.structDepth < maxStructDepth {
				body := &typeset.StructElem{Role: "LBody"}
				el.Kids = append(el.Kids, body)
				out.elem, out.item = body, el
				out.structDepth++
				if n.ItemRole != "" && out.structDepth < maxStructDepth {
					// The item's own element, a heading, names it as a
					// link target.
					inner := &typeset.StructElem{Role: n.ItemRole, Lang: n.Lang}
					body.Kids = append(body.Kids, inner)
					out.elem, own = inner, inner
					out.structDepth++
				}
			}
		}
	}
	if b == nil {
		return out
	}
	b.elem = own
	if out.elem != ctx.elem && n.Kind == Replaced && n.Picture != nil && n.Picture.Embed != nil {
		// The embedded document's structure hangs from this element when
		// it is painted.
		if e.embeds == nil {
			e.embeds = map[*typeset.StructElem]int{}
		}
		e.embeds[out.elem] = out.structDepth
	}
	if n.Href != "" {
		l := &link{href: n.Href, node: n}
		if out.elem != nil && out.elem.Role == "Link" {
			l.elem = out.elem
		}
		out.link = l
		b.link = l
	}
	if b.st.TextDecoration != 0 {
		out.decos = append(slices.Clip(ctx.decos), newDeco(b, b.st))
	}
	return out
}

// labelForbids are the roles ISO/TS 32005, Table 5 forbids inside a Lbl,
// which veraPDF's PDF/UA-2 profile tests: the grouping, block, list and
// table types. ISO 32000-1, 14.8.4.3.3, Table 336 makes a Lbl the term or
// the number that names an item and gives it no structure of its own. The
// ruby and warichu parts, which Table 5 forbids directly under a Lbl, stay:
// they sit in their Ruby or Warichu, which a Lbl may hold. Immutable lookup
// table.
var labelForbids = map[string]bool{
	"Document": true, "DocumentFragment": true, "Part": true, "Art": true, "Sect": true,
	"Div": true, "Aside": true, "BlockQuote": true, "Caption": true, "Index": true,
	"TOC": true, "TOCI": true, "Title": true,
	"P": true, "H": true, "H1": true, "H2": true, "H3": true, "H4": true, "H5": true, "H6": true,
	"L": true, "LI": true, "Lbl": true, "LBody": true,
	"Table": true, "THead": true, "TBody": true, "TFoot": true, "TR": true, "TH": true, "TD": true,
}

// listGroup returns the LI of the list l that an element of role, Lbl or
// LBody, joins: the list's last LI, unless it holds no element yet or the
// element is a Lbl and the LI holds a LBody already, and a new LI
// otherwise. A definition list's group is one or more terms followed by
// one or more descriptions (HTML Standard, 4.4.9), and its LI holds the
// terms as Lbl elements and the descriptions as LBody elements (ISO
// 32000-1, 14.8.4.3.3, Table 336: in a dictionary list the label is the
// term defined, the body its definition). So a term after a description
// starts the next group, and a description with no term before it starts
// a group of its own.
func listGroup(l *typeset.StructElem, role string) *typeset.StructElem {
	if k := len(l.Kids); k > 0 {
		if last := l.Kids[k-1]; last.Role == "LI" && len(last.Kids) > 0 &&
			(role == "LBody" || !slices.ContainsFunc(last.Kids, func(c *typeset.StructElem) bool { return c.Role == "LBody" })) {
			return last
		}
	}
	li := &typeset.StructElem{Role: "LI"}
	l.Kids = append(l.Kids, li)
	return li
}

// listKids are the roles an L holds as its own kids: ISO 32000-1,
// 14.8.4.3.3, Table 336 gives a list items, a caption and nested lists,
// which ISO 14289-1, 7.2 and ISO 32005, Table 5 hold it to; a Lbl or a
// LBody joins an item of its own ([listGroup]). Immutable lookup table.
var listKids = map[string]bool{"L": true, "LI": true, "Caption": true, "Lbl": true, "LBody": true}

// itemBody returns the LBody that an element of the list l other than
// [listKids], a paragraph between items, hangs from: that of the list's
// last LI, whose later paragraph it is, or the LBody of a new LI with no
// Lbl when the list has no item yet, a list header (ODF 1.3, Part 3,
// 5.3.3 text:list-header) or content an HTML list holds before its first
// item. An LI always holds a LBody (ISO 32000-1, 14.8.4.3.3, Table 336).
func itemBody(l *typeset.StructElem) *typeset.StructElem {
	if k := len(l.Kids); k > 0 {
		if last := l.Kids[k-1]; last.Role == "LI" {
			if j := len(last.Kids); j > 0 && last.Kids[j-1].Role == "LBody" {
				return last.Kids[j-1]
			}
			body := &typeset.StructElem{Role: "LBody"}
			last.Kids = append(last.Kids, body)
			return body
		}
	}
	body := &typeset.StructElem{Role: "LBody"}
	l.Kids = append(l.Kids, &typeset.StructElem{Role: "LI", Kids: []*typeset.StructElem{body}})
	return body
}

// genChildren generates the boxes of children into the block container b,
// wraps its runs of table boxes in anonymous tables and its inline runs in
// anonymous blocks.
func (e *engine) genChildren(b *box, children []*Node, ctx genCtx) {
	ortho := !ctx.vertical && b.st.WritingMode.Vertical() && orthoEligible(b)
	if ctx.vertical && e.opts.BrowserRules && b.st.WritingMode == css.HorizontalTB && orthoEligible(b) {
		// A horizontal box inside a vertical flow roots an orthogonal flow
		// too (CSS Writing Modes 4, 7.3): its content is laid out on the
		// page as it is.
		ortho = true
	}
	if ortho {
		ctx.vertical, ctx.frame = b.st.WritingMode.Vertical(), b.st.WritingMode
	}
	e.adopt(b, e.genRaw(b, children, ctx))
	if ortho {
		e.wrapOrtho(b)
	}
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
		e.warnOnce(warn.LimitReached, warn.Omitted, fmt.Sprintf("content nested deeper than %d levels left out", maxTreeDepth), -1)
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
		if n.ActualText != "" {
			e.enter(n, b, ctx)
		}
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
			b.picture = viewBoxPicture(zoomedPicture(n.Picture, st.UsedZoom()), st.ObjectViewBox)
			if ctx.vertical && e.opts.BrowserRules && b.picture.view == nil {
				b.turn = ctx.frame
				b.picture = turnedPicture(b.picture)
			}
		}
		return []*box{b}
	case Element:
	default:
		return nil
	}
	if st.Display == css.DisplayNone {
		return nil
	}
	if st.Display == css.Contents {
		return e.genContents(n, parent, ctx, ordinal)
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
	if cl := e.chains[n]; cl != nil && (st.Display == css.Block || st.Display == css.InlineBlock || st.Display == css.ListItem) {
		// A link of a chain paints its share of the chain's content and
		// lays none of its children out (chain.go).
		if cl.auto {
			own := *st
			own.Height = css.Pts(cl.h)
			if own.BoxSizing == css.BorderBox {
				own.Height = css.Pts(cl.h + own.Border[css.Top].Width + own.Border[css.Bottom].Width + pts(own.Padding[css.Top]) + pts(own.Padding[css.Bottom]))
			}
			st = &own
		}
		b := e.newBox(n, st, kindBlock, ctx)
		b.float = float
		b.abs = isAbs(st)
		b.inline = float == css.FloatNone && !b.abs && st.Display == css.InlineBlock
		b.chain = cl
		e.enter(n, b, ctx)
		return []*box{b}
	}
	switch {
	case float == css.FloatNone && st.Display == css.Ruby:
		return e.genRuby(n, st, ctx)
	case float == css.FloatNone && (st.Display == css.Inline || st.Display == css.InlineListItem || st.Display == css.RubyText):
		// An annotation outside a ruby is an inline box, as Chromium
		// lays it out.
		return e.genInline(n, st, ctx, ordinal)
	case st.Display == css.Table || st.Display == css.InlineTable:
		w := e.genTable(n, st, ctx, float)
		w.abs = isAbs(st)
		return []*box{w}
	case float == css.FloatNone && isTablePart(st.Display):
		return []*box{e.genPart(n, st, ctx)}
	case st.Display == css.Flex || st.Display == css.InlineFlex || isGrid(st.Display):
		b := e.newBox(n, st, kindBlock, ctx)
		b.float = float
		b.abs = isAbs(st)
		b.inline = float == css.FloatNone && !b.abs && (st.Display == css.InlineFlex || st.Display == css.InlineGrid)
		cctx := e.enter(n, b, ctx)
		if float != css.FloatNone || b.abs || b.inline {
			cctx.decos = ownDecos(b, b.st)
		}
		if isGrid(st.Display) {
			e.genGrid(b, n.Children, cctx)
		} else {
			e.genFlex(b, n.Children, cctx)
		}
		return []*box{b}
	case float == css.FloatNone && st.Display == css.InlineBlock:
		b := e.newBox(n, st, kindBlock, ctx)
		b.inline = true
		cctx := e.enter(n, b, ctx)
		cctx.decos = ownDecos(b, b.st)
		if st.ListItemRoot {
			// An inline-level list item of flow-root inside: an inline
			// block with a marker (CSS Display 3, 2).
			*ordinal++
			if n.ListValue != 0 {
				*ordinal = n.ListValue
			}
			cctx = e.genMarker(n, b, cctx, *ordinal, false)
		}
		e.genChildren(b, n.Children, cctx)
		return []*box{b}
	}
	b := e.newBox(n, st, kindBlock, ctx)
	b.float = float
	b.abs = isAbs(st)
	b.initial = n.InitialLetter && float != css.FloatNone && st.InitialLetter.Size >= 1
	cctx := e.enter(n, b, ctx)
	if float != css.FloatNone || b.abs {
		cctx.decos = ownDecos(b, b.st)
	} else {
		cctx.decos = propagatedDecos(cctx.decos, b)
	}
	if st.Display == css.ListItem {
		*ordinal++
		if n.ListValue != 0 {
			*ordinal = n.ListValue
		}
		cctx = e.genMarker(n, b, cctx, *ordinal, false)
	}
	if st.Display == css.BlockRuby {
		// A block ruby is a block box holding one ruby container (CSS
		// Ruby 1, 2.1).
		rst := *e.anonOf(st)
		rst.Display = css.Ruby
		e.adopt(b, []*box{e.rubyColumns(e.newRubyBox(e.shareStyle(&rst), cctx), n.Children, cctx)})
		return []*box{b}
	}
	e.genChildren(b, n.Children, cctx)
	return []*box{b}
}

// genContents generates the boxes of the children of n, an element of
// display: contents, in its place among the children of parent (CSS
// Display 3, 2.5): n draws no box, so its own box properties, float and
// position apply to nothing, it holds no marker, and its children join
// parent's anonymous table, flex and block boxes as its siblings do. Its
// inherited properties reach its children through their computed styles.
// It still starts its structure element and its language; it is no
// decorating box (CSS Text Decoration 3, 2.1), and it is no link target or
// link, nor a Link element: those need a box to place.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 draws no
//     underline of a display: contents span, draws an ancestor's through
//     it, ignores its float, position, vertical-align, border and
//     background, writes no link annotation for an a of display: contents,
//     and gives a display: contents li no marker and no ordinal.
func (e *engine) genContents(n *Node, parent *box, ctx genCtx, ordinal *int) []*box {
	cctx := e.enter(n, nil, ctx)
	var out []*box
	for _, c := range n.Children {
		if e.err != nil {
			break
		}
		out = append(out, e.genNode(c, parent, cctx, ordinal)...)
	}
	return out
}

// validPicture reports a picture the engine can draw: a source or a
// vector, and a finite, positive intrinsic size.
func validPicture(p *Picture) bool {
	return p != nil && (p.Source != nil || p.Vector != nil || p.Gradient != nil || p.Embed != nil) && numeric.PositiveFinite(p.Width) && numeric.PositiveFinite(p.Height)
}

// genInline generates an inline element: its inline box, split around the
// block-level boxes among its descendants (CSS 2.1, 9.2.1.1). Each piece
// of a box that clones its decorations draws its start and end edges
// (CSS Fragmentation 3, 5.4: each box fragment is wrapped independently);
// the pieces of a sliced one draw them on the first and the last alone.
func (e *engine) genInline(n *Node, st *css.Style, ctx genCtx, ordinal *int) []*box {
	ib := e.newBox(n, st, kindInline, ctx)
	ib.inline = true
	cctx := e.enter(n, ib, ctx)
	ib.decos = cctx.decos
	e.genFootnote(n, ib, ctx)
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
		cur.last = clones(st)
		if k.host == nil {
			k.host = ib
		}
		out = append(out, k)
		piece := e.newBox(n, st, kindInline, ctx)
		*piece = *ib
		piece.kids = nil
		piece.first, piece.last = clones(st), true
		piece.id, piece.fnBody = "", nil
		out = append(out, piece)
		cur = piece
	}
	return out
}

// propagatedDecos returns the decorations ds that reach the in-flow block
// container b from an ancestor, drawn on b's text by the anonymous inline
// box that wraps it: CSS Text Decoration 3, 2 propagates them to that box,
// so they take b's font size, and an underline under the text b's face,
// keeping their colour, style and lines (2.3). Its own decorations, last in
// ds, stay as they are.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 draws the blue underline of a 16px Ahem
//     block 10px thick under a 100px block child and 5px thick under a
//     50px grid item, and an underline of 40px DejaVu Sans twice as thick
//     under an 80px grid item.
func propagatedDecos(ds []deco, b *box) []deco {
	var out []deco
	for i, d := range ds {
		if d.origin == b {
			continue
		}
		if out == nil {
			out = slices.Clone(ds)
		}
		d.origin, d.size = b, min(max(clamp(b.st.FontSize), 0), maxFontSize)
		out[i] = d
	}
	if out == nil {
		return ds
	}
	return out
}

// ownDecos returns the decorations a box b of style st that does not
// inherit them, a float or an inline block, draws on its own text.
func ownDecos(b *box, st *css.Style) []deco {
	if st.TextDecoration == 0 {
		return nil
	}
	return []deco{newDeco(b, st)}
}

// isAbs reports an absolutely positioned style: absolute or fixed, out of
// flow (CSS 2.1, 9.6).
func isAbs(st *css.Style) bool {
	return st.Position == css.Absolute || st.Position == css.Fixed
}

// isInlineLevel reports whether a display value is inline-level.
func isInlineLevel(d css.Display) bool {
	return d == css.Inline || d == css.InlineBlock || d == css.InlineTable || d == css.InlineListItem || d == css.InlineFlex || d == css.InlineGrid ||
		d == css.Ruby || d == css.RubyText
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
			st: e.anonOf(b.st), kind: kindBlock, anon: true, first: true, last: true, parent: b,
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
	st.FontKerning = parent.FontKerning
	st.LineHeight = parent.LineHeight
	st.TextAlign = parent.TextAlign
	st.TextIndent = parent.TextIndent
	st.TextIndentHanging = parent.TextIndentHanging
	st.TextIndentEachLine = parent.TextIndentEachLine
	st.TextTransform = parent.TextTransform
	st.LetterSpacing = parent.LetterSpacing
	st.WordSpacing = parent.WordSpacing
	st.WhiteSpace = parent.WhiteSpace
	st.WordBreak = parent.WordBreak
	st.OverflowWrap = parent.OverflowWrap
	st.TabSize = parent.TabSize
	// The Office paragraph properties reach the anonymous block that holds
	// a paragraph's lines, as the inherited text properties do.
	st.TabStops = parent.TabStops
	st.MinLineHeight = parent.MinLineHeight
	st.LineRule = parent.LineRule
	st.WriterLines = parent.WriterLines
	st.TextScale = parent.TextScale
	st.Direction = parent.Direction
	// writing-mode and text-orientation are inherited (CSS Writing Modes
	// 4, 3.1 and 5.1): the text of an anonymous block stands in its line as
	// its parent's does.
	st.WritingMode = parent.WritingMode
	st.TextOrientation = parent.TextOrientation
	st.ListStyleType = parent.ListStyleType
	st.ListStylePosition = parent.ListStylePosition
	st.ListStyleImage = parent.ListStyleImage
	st.BorderCollapse = parent.BorderCollapse
	st.BorderSpacing = parent.BorderSpacing
	st.CaptionSide = parent.CaptionSide
	st.EmptyCells = parent.EmptyCells
	st.Orphans = parent.Orphans
	st.Widows = parent.Widows
	st.RubyPosition = parent.RubyPosition
	st.RubyAlign = parent.RubyAlign
	st.RubyOverhang = parent.RubyOverhang
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
	// authored is set for a marker of authored text whose Alt names what
	// its characters stand for.
	authored := false
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
		authored = pic == nil && alt != ""
		if pic != nil && alt == "" {
			alt = label
		}
	default:
		if m != nil && m.Style != nil {
			st = m.Style
		}
		pic = zoomedPicture(e.picture(st.ListStyleImage, -1), st.UsedZoom())
		label = MarkerText(st, ordinal)
		alt = markerAlt(st, ordinal)
	}
	if pic == nil && label == "" {
		return ctx
	}
	outside := b.st.ListStylePosition == css.Outside && !inside
	// A ::marker takes no box properties (CSS Pseudo 4, 3.2): under
	// [Options.BrowserRules] its width sizes nothing, and only an Office
	// marker's width makes a marker box.
	boxW := 0.0
	if !e.opts.BrowserRules {
		boxW = markerBoxWidth(n.Marker)
	}
	// Under [Options.BrowserRules], a bullet of disc, circle or square
	// and a disclosure triangle paint as a shape ([symbolRect], [engine.symbolMarkerOf]); a
	// ::marker's content, an Office marker's box or the engine's own rules
	// draw it as text.
	symbol := e.opts.BrowserRules && pic == nil && (n.Marker == nil || len(n.Marker.Children) == 0) &&
		symbolType(st.ListStyleType)
	// Under [Options.BrowserRules] the converter hands the ::marker's
	// cascaded style in.
	cascaded := e.opts.BrowserRules && n.Marker != nil && n.Marker.Style != nil
	markerElem := ctx.elem
	if n.Role == "LI" && ctx.item != nil {
		lbl := &typeset.StructElem{Role: "Lbl"}
		if pic != nil || symbol || authored {
			lbl.ActualText = alt
		}
		ctx.item.Kids = slices.Insert(ctx.item.Kids, 0, lbl)
		if l := ctx.list; l != nil && l.Numbering == typeset.NumberingUnset {
			// ISO 14289-2, 8.2.5.25: a list whose items carry a Lbl
			// states its numbering.
			l.Numbering = listNumbering(st, pic != nil || n.Marker != nil && len(n.Marker.Children) > 0)
		}
		markerElem = lbl
	}
	if outside {
		if cascaded {
			// The text of an inside marker is transformed as any text
			// is; an outside marker's is shaped alone.
			var prev rune
			label = transform(label, st.TextTransform, b.lang, &prev)
		}
		b.outside = &markerBox{text: label, picture: pic, elem: markerElem, st: st, boxW: boxW, symbol: symbol}
		return ctx
	}
	mctx := ctx
	mctx.elem = markerElem
	var m *box
	switch {
	case pic != nil:
		m = e.newBox(nil, e.markerOf(st, cascaded), kindReplaced, mctx)
		m.picture = pic
	case symbol:
		m = e.newBox(nil, e.symbolMarkerOf(e.markerOf(st, cascaded), b.lang), kindReplaced, mctx)
		m.symbol = true
	default:
		m = e.newBox(nil, e.markerOf(st, cascaded), kindText, mctx)
		m.text = label
	}
	m.inline = true
	m.marker = true
	m.parent = b
	b.kids = append(b.kids, m)
	return ctx
}

// listNumbering returns the ListNumbering attribute (ISO 32000-1, 14.8.5.5,
// Table 347; ISO 32000-2, Table 382) of a list whose markers have style
// st: the value of its list-style-type when the attribute has one, Ordered
// or Unordered for the other counter styles, and Unordered for a picture
// or a marker of authored content (custom).
func listNumbering(st *css.Style, custom bool) typeset.ListNumbering {
	if custom {
		return typeset.NumberingUnordered
	}
	switch st.ListStyleType {
	case css.Disc:
		return typeset.NumberingDisc
	case css.Circle:
		return typeset.NumberingCircle
	case css.Square:
		return typeset.NumberingSquare
	case css.Decimal:
		return typeset.NumberingDecimal
	case css.LowerRoman:
		return typeset.NumberingLowerRoman
	case css.UpperRoman:
		return typeset.NumberingUpperRoman
	case css.LowerAlpha:
		return typeset.NumberingLowerAlpha
	case css.UpperAlpha:
		return typeset.NumberingUpperAlpha
	case css.DecimalLeadingZero, css.LowerGreek, css.Armenian, css.Georgian:
		return typeset.NumberingOrdered
	case css.ListNone, css.ListString, css.ListNamed, css.DisclosureOpen, css.DisclosureClosed:
	}
	return typeset.NumberingUnordered
}

// markerStyle returns the style of a list item's marker: the item's, as an
// inline box with no decoration of its own. cascaded reports item to be
// the cascaded style of a ::marker, whose white-space and text-transform
// the user agent sheet sets and an author may set again (CSS Lists 3,
// 3.1.1); any other style takes the sheet's values, pre and none.
func markerStyle(item *css.Style, cascaded bool) *css.Style {
	st := *item
	st.Display = css.Inline
	st.Float = css.FloatNone
	st.Position = css.Static
	st.ZIndex = css.ZIndex{Auto: true}
	st.Transform, st.Translate, st.Rotate, st.Scale = nil, nil, nil, nil
	st.Margin = [4]css.Dimension{css.Pts(0), css.Pts(0), css.Pts(0), css.Pts(0)}
	st.Padding = st.Margin
	st.Border = [4]css.Border{}
	st.BackgroundColor = css.Transparent
	st.ClearBackgroundImages()
	st.Width, st.Height = css.Dimension{}, css.Dimension{}
	st.MinWidth, st.MinHeight = css.Pts(0), css.Pts(0)
	st.MaxWidth, st.MaxHeight = css.Dimension{Kind: css.None}, css.Dimension{Kind: css.None}
	st.Opacity = 1
	st.TextIndent = css.Pts(0)
	if !cascaded {
		st.TextTransform = css.TransformNone
		st.WhiteSpace = css.Pre
		return &st
	}
	st.WhiteSpace = markerWrap(st.WhiteSpace)
	return &st
}

// markerWrap returns the white-space a ::marker's text lays out by: its
// own, its lines wrapping. CSS Lists 3, 3.1.1 sets pre on ::marker and
// notes that pre does not quite behave as markers should (CSS WG issues
// 4448 and 4891); Chromium keeps the spaces of a marker that computes to
// pre and wraps its text, and litho follows Chromium.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 (Gotenberg 9.4.1) computes white-space:
//     pre on ::marker, and an inside marker "ab cd" of a 0px wide list
//     item draws on two lines; "abcd" under word-break: break-all or
//     overflow-wrap: anywhere on four.
//   - wpt: css-pseudo/marker-word-break, marker-line-break and
//     marker-overflow-wrap wrap ::marker text that computes to pre, and
//     Chromium draws them as their references.
func markerWrap(ws css.WhiteSpace) css.WhiteSpace {
	switch ws {
	case css.Pre:
		return css.PreWrap
	case css.Nowrap:
		return css.WhiteSpaceNormal
	case css.PreLineNowrap:
		return css.PreLine
	case css.BreakSpacesNowrap:
		return css.BreakSpaces
	case css.WhiteSpaceNormal, css.PreWrap, css.PreLine, css.BreakSpaces:
	}
	return ws
}

// genFlex generates the children of the flex container b (CSS Flexbox 1,
// 4): each in-flow child box is a flex item, which neither floats nor
// clears; each run of text between them is wrapped in an anonymous flex
// item, unless it holds white space alone, which is not rendered. The
// items are sorted in order-modified document order (5.4), an absolutely
// positioned child taking order 0 for painting.
func (e *engine) genFlex(b *box, children []*Node, ctx genCtx) {
	raw := e.wrapOrphans(b, e.genRaw(b, children, ctx), false)
	var kids []*box
	for i := 0; i < len(raw); {
		k := raw[i]
		if k.abs {
			kids = append(kids, k)
			i++
			continue
		}
		if !k.inline {
			k.float = css.FloatNone
			k.flexItem = true
			kids = append(kids, k)
			i++
			continue
		}
		j := i
		blank := true
		for j < len(raw) && raw[j].inline && !raw[j].abs {
			blank = blank && isSpace(raw[j])
			j++
		}
		if !blank {
			a := &box{
				st: e.anonOf(b.st), kind: kindBlock, anon: true, first: true, last: true, parent: b,
				lang: b.lang, elem: b.elem, link: b.link, depth: b.depth + 1, inlineKids: true, flexItem: true,
			}
			a.hidden = a.st.Visibility != css.Visible
			e.boxes++
			for _, c := range raw[i:j] {
				c.parent = a
				a.kids = append(a.kids, c)
			}
			kids = append(kids, a)
		}
		i = j
	}
	order := func(k *box) int {
		if k.abs {
			return 0
		}
		return k.st.Order
	}
	slices.SortStableFunc(kids, func(x, y *box) int { return cmp.Compare(order(x), order(y)) })
	for _, k := range kids {
		k.parent = b
	}
	b.kids = kids
	b.flex = &flexData{}
}
