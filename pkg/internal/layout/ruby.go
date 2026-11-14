package layout

import (
	"math"
	"strings"
	"unicode"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// rubyCol is one column of a ruby container (CSS Ruby 1, 2.3): a base and
// the annotation paired with it, laid out as one atomic inline of its
// ruby's inline box. The base and the annotation are blocks of one line
// each, as wide as the wider of the two.
type rubyCol struct {
	// container is the inline box of the ruby the column belongs to.
	container *box
	// base is the anonymous block of the base's content, ann the
	// annotation's block, nil for a base without one.
	base, ann *box
	// breakAs is the first character of the base, which the column breaks
	// as in its line (text.Object BreakAs), 0 for a base without one.
	breakAs rune
	// inStart and inEnd are the room the base leaves at the column's
	// left and right edges once aligned, which bounds the overhang.
	inStart, inEnd float64
	// over and under report annotations over or under the base, its own
	// or those of a ruby nested in it, overTop the top of the em boxes
	// of those over it and underBottom the bottom of those under it,
	// below the column's top.
	over, under          bool
	overTop, underBottom float64
	// baseAbove and baseBelow are the base's line extent above and below
	// its baseline, which the line holding the column grows to hold
	// before the annotations over and under it (rubyRoom).
	baseAbove, baseBelow float64
	// textAbove and textBelow are the extent of the base's content, the em
	// boxes of its text, above and below its baseline: the room above and
	// under them is room an annotation of the line before or after may
	// take.
	textAbove, textBelow float64
}

// genRuby generates the boxes of a ruby container n of style st (CSS Ruby
// 1, 2): its inline box, holding one column for each base and the
// annotation that follows it. A run of children that are not annotations
// is a base (HTML Standard, 15.3.4); white space alone between an
// annotation and the next base, and at the container's start and end, is
// dropped (CSS Ruby 1, 2.5.1); an annotation with no base before it pairs
// with an empty base. node is the ruby's element, nil for the anonymous
// ruby a block ruby holds.
func (e *engine) genRuby(node *Node, st *css.Style, ctx genCtx) []*box {
	rb := e.newBox(node, st, kindInline, ctx)
	rb.inline = true
	cctx := ctx
	if node != nil {
		cctx = e.enter(node, rb, ctx)
	}
	rb.decos = cctx.decos
	var children []*Node
	if node != nil {
		children = node.Children
	}
	return []*box{e.rubyColumns(rb, children, cctx)}
}

// rubyColumns adds to the ruby inline box rb the columns of children, and
// returns rb.
func (e *engine) rubyColumns(rb *box, children []*Node, ctx genCtx) *box {
	var base []*Node
	content := false
	flush := func(ann *Node) {
		if !content && ann == nil {
			return
		}
		col := e.rubyColumn(rb, base, ann, ctx)
		col.parent = rb
		rb.kids = append(rb.kids, col)
		base, content = nil, false
	}
	for _, c := range children {
		if e.err != nil {
			break
		}
		switch {
		case isAnnotation(c):
			flush(c)
		case c.Kind == Text && strings.Trim(c.Text, " \t\n\r\f") == "" && !content:
			// Inter-segment and leading white space (CSS Ruby 1, 2.5.1).
		case c.Kind == Element && c.Style != nil && c.Style.Display == css.DisplayNone:
			// An rp, hidden, separates nothing.
			if content {
				base = append(base, c)
			}
		default:
			base = append(base, c)
			content = true
		}
	}
	flush(nil)
	return rb
}

// isAnnotation reports a child of a ruby container that is a ruby
// annotation box: an in-flow element of display ruby-text.
func isAnnotation(n *Node) bool {
	return n.Kind == Element && n.Style != nil && n.Style.Display == css.RubyText &&
		n.Style.Float == css.FloatNone && !isAbs(n.Style)
}

// rubyColumn returns the column of the ruby inline box rb that pairs the
// base nodes base with the annotation ann, nil for none.
func (e *engine) rubyColumn(rb *box, base []*Node, ann *Node, ctx genCtx) *box {
	cst := *e.anonOf(rb.st)
	cst.Display = css.InlineBlock
	cst.TextIndent = css.Dimension{}
	cst.TextIndentHanging, cst.TextIndentEachLine = false, false
	cst.WhiteSpace = nowrapOf(cst.WhiteSpace)
	colSt := e.shareStyle(&cst)
	ctx, tagged := e.rubyElem(base, ann, ctx)
	col := e.newBox(nil, colSt, kindBlock, ctx)
	col.inline, col.anon = true, true
	r := &rubyCol{container: rb, breakAs: firstRune(base)}
	col.ruby = r
	bst := cst
	bst.Display = css.Block
	bctx := ctx
	if tagged && !heldByRB(base) {
		bctx = e.structChild(ctx, "RB")
	}
	b := e.newBox(nil, e.shareStyle(&bst), kindBlock, bctx)
	b.anon, b.rubyLine = true, true
	e.genChildren(b, base, bctx)
	b.parent = col
	r.base = b
	col.kids = append(col.kids, b)
	if ann != nil {
		ast := *ann.Style
		ast.TextIndent = css.Dimension{}
		ast.TextIndentHanging, ast.TextIndentEachLine = false, false
		ast.WhiteSpace = nowrapOf(ast.WhiteSpace)
		a := e.newBox(ann, e.shareStyle(&ast), kindBlock, ctx)
		a.rubyLine, a.rubyAnn = true, true
		an := ann
		if !tagged && ann.Role == "RT" {
			// An annotation outside a Ruby element is a Span: ISO 32000-1,
			// 14.8.4.4.2 puts an RT inside a Ruby alone.
			own := *ann
			own.Role = "Span"
			an = &own
		}
		actx := e.enter(an, a, ctx)
		actx.decos = ownDecos(a, a.st)
		e.genChildren(a, ann.Children, actx)
		a.parent = col
		r.ann = a
		col.kids = append(col.kids, a)
	}
	return col
}

// nowrapOf returns the white-space value that keeps ws's handling of
// spaces and does not wrap: a ruby base and its annotation hold one line.
func nowrapOf(ws css.WhiteSpace) css.WhiteSpace {
	switch ws { //nolint:exhaustive // the other values do not wrap already
	case css.WhiteSpaceNormal:
		return css.Nowrap
	case css.PreWrap:
		return css.Pre
	case css.PreLine:
		return css.PreLineNowrap
	case css.BreakSpaces:
		return css.BreakSpacesNowrap
	}
	return ws
}

// firstRune returns the first character of the text of nodes that is not
// white space, 0 for none.
func firstRune(nodes []*Node) rune {
	for _, n := range nodes {
		if n.Kind == Element && n.Style != nil && n.Style.Display == css.DisplayNone {
			continue
		}
		if n.Kind == Text {
			for _, r := range n.Text {
				if !unicode.IsSpace(r) {
					return r
				}
			}
		}
		if r := firstRune(n.Children); r != 0 {
			return r
		}
	}
	return 0
}

// layoutRuby lays the ruby column a out (CSS Ruby 1, 3): its base and its
// annotation each on one line as wide as the wider of the two, the
// annotation's em box against the base's, over or under it as
// ruby-position says (4.1). The column's box spans the base's line and
// the annotation's em box, so that the line holding the column grows to
// hold the annotation where its half-leading does not, as Chromium lays
// a ruby out: its annotation's em box sits on the em box of the base's
// first available font, split as the font's typographic ascender and
// descender split it, and counts in the line's height for that em box
// alone.
func (e *engine) layoutRuby(a *box, lcb cblock) {
	a.laidOut = true
	resolveEdges(a, lcb.w)
	r := a.ruby
	_, w := e.intrinsic(r.base)
	if r.ann != nil {
		_, aw := e.intrinsic(r.ann)
		w = max(w, aw)
	}
	a.w = w
	a.x, a.y = a.m[css.Left], a.m[css.Top]
	e.noPaginate++
	e.layoutOwnContext(a, cblock{w: w})
	e.noPaginate--
	m := e.faceMetrics(a.st, a.lang)
	var bl float64
	top, bottom := r.base.y, r.base.y+r.base.h
	if y, ok := firstBaseline(r.base); ok && len(r.base.lines) > 0 {
		bl = y
		l := r.base.lines[0]
		top, bottom = min(top, l.y), max(bottom, l.y+l.h)
		if last := r.base.lines[len(r.base.lines)-1]; last != l {
			bottom = max(bottom, last.y+last.h)
		}
	} else {
		// An empty base takes its ruby's strut.
		lh := e.lineHeight(a.st, a.lang)
		up, _ := e.leading(lh - m.Ascent - m.Descent)
		bl = r.base.y + m.Ascent + up
		top, bottom = r.base.y, r.base.y+lh
	}
	r.baseAbove, r.baseBelow = bl-top, bottom-bl
	r.textAbove, r.textBelow = baseText(r.base, bl, m.Ascent, m.Descent)
	r.inStart, r.inEnd = lineInsets(r.base, w)
	emTop, emBottom := bl-m.EmAscent, bl+m.EmDescent
	// A ruby nested in the base sets its own annotations against its
	// base: this one sits beyond them (CSS Ruby 1, 2.4), and the line
	// holding this column makes room for them.
	inTop, inBottom := math.Inf(1), math.Inf(-1)
	for _, l := range r.base.lines {
		for _, it := range l.items {
			if in := it.atomic; in != nil && in.ruby != nil {
				if in.ruby.over {
					inTop = min(inTop, in.y+in.ruby.overTop)
				}
				if in.ruby.under {
					inBottom = max(inBottom, in.y+in.ruby.underBottom)
				}
			}
		}
	}
	overTop, underBottom := inTop, inBottom
	if ann := r.ann; ann != nil && len(ann.lines) > 0 {
		am := e.faceMetrics(ann.st, ann.lang)
		abl, _ := firstBaseline(ann)
		// The ruby's ruby-position places its annotations, whatever an
		// annotation sets, as Chromium reads it (CSS Ruby 1, 4.1 lets
		// it apply to the annotation).
		if r.container.st.RubyPosition == css.RubyUnder {
			want := max(emBottom, inBottom) + am.EmAscent
			ann.shift(0, want-abl)
			underBottom = want + am.EmDescent
		} else {
			want := min(emTop, inTop) - am.EmDescent
			ann.shift(0, want-abl)
			overTop = want - am.EmAscent
		}
	}
	r.over, r.under = !math.IsInf(overTop, 1), !math.IsInf(underBottom, -1)
	if r.over {
		top = min(top, overTop)
	}
	if r.under {
		bottom = max(bottom, underBottom)
	}
	r.overTop, r.underBottom = overTop-top, underBottom-top
	a.y = top
	a.h = max(bottom-top, 0)
	a.baseline, a.hasBaseline = bl-top, true
}

// baseText returns the extent of the content of the ruby base b above and
// below its baseline bl: the em boxes of the items of its lines, ascent
// and descent, the strut of its ruby, for a base without any.
func baseText(b *box, bl, ascent, descent float64) (float64, float64) {
	above, below := math.Inf(-1), math.Inf(-1)
	for _, l := range b.lines {
		for _, it := range l.items {
			above = max(above, bl-(l.y+it.baseline-it.ascent))
			below = max(below, l.y+it.baseline+it.descent-bl)
		}
	}
	if math.IsInf(above, -1) {
		return ascent, descent
	}
	return above, below
}

// lineInsets returns the room the one line of block b leaves at the left
// and the right of width w, 0 and 0 for a block of another line count.
func lineInsets(b *box, w float64) (float64, float64) {
	if len(b.lines) != 1 || len(b.lines[0].items) == 0 {
		return 0, 0
	}
	l := b.lines[0]
	left, right := l.items[0].x, 0.0
	for _, it := range l.items {
		left = min(left, it.x)
		right = max(right, it.x+it.w)
	}
	return max(left, 0), max(w-right, 0)
}

// rubyAlign returns the offset of the content of l, a line of a ruby
// base or annotation, free points narrower than its column, after it
// spreads the room as ruby-align says (CSS Ruby 1, 4.2): start, centre,
// over the justification opportunities alone (space-between) or with
// half as much at each end (space-around), centred where the content has
// no opportunity. Chromium caps the room at each end of space-around at
// the font size of the content.
func (s *inlineState) rubyAlign(l *lineBox, free float64, ltr bool) float64 {
	st := s.root.st
	center := float64(free / 2)
	switch st.RubyAlign {
	case css.RubyStart:
		if ltr {
			return 0
		}
		return free
	case css.RubyCenter:
		return center
	case css.RubySpaceBetween:
		if s.justifyAs(l, free, css.JustifyAuto) {
			return 0
		}
		return center
	case css.RubySpaceAround:
	}
	n := justifyCount(l, css.JustifyAuto)
	if n == 0 {
		return center
	}
	inset := min(free/float64(n+1)/2, s.e.fontSize(st))
	if s.justifyAs(l, free-2*inset, css.JustifyAuto) {
		return inset
	}
	return center
}

// rubyOverhang sets the overhang of the ruby column a, character i of
// p: an annotation wider than its base hangs over the text beside the
// column by up to half the annotation's font size on each side, within
// the room the base leaves on that side, as Chromium lays it out (CSS
// Ruby 1, 4.3, ruby-overhang: auto, leaves the amount to the user
// agent). The text beside it must be a text run at most as large as the
// ruby's, with no inline box opening or closing in between; another
// column takes none.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 lays a 60px
//     annotation over a 20px base out alike under ruby-overhang: none and
//     spaces, beside spaces, brackets, middle dots and letters.
func (e *engine) rubyOverhang(p *ifcPara, i int, a *box) {
	r := a.ruby
	if r.ann == nil || len(r.ann.lines) == 0 || len(r.base.lines) == 0 {
		// An annotation over an empty base hangs over nothing.
		return
	}
	half := e.fontSize(r.ann.st) / 2
	size := e.fontSize(r.container.st)
	beside := func(j, at int) bool {
		if j < 0 || j >= p.p.Len() {
			return false
		}
		info := p.infos[p.p.RunOf(j)]
		if info.kind != runText || e.fontSize(info.box.st) > size+eps {
			return false
		}
		for _, o := range p.opens {
			if o.pos == at && o.box != r.container {
				return false
			}
		}
		for _, c := range p.closes {
			if c.pos == at && c.box != r.container {
				return false
			}
		}
		return true
	}
	start, end := css.Left, css.Right
	if a.st.Direction == css.RTL {
		start, end = end, start
	}
	room := [4]float64{css.Left: r.inStart, css.Right: r.inEnd}
	if a.st.RubyOverhang != css.RubyOverhangAuto {
		// The annotation hangs over the spaces beside the column alone,
		// as far as they reach: under none as under spaces, as Chromium
		// lays none out.
		var before, after float64
		for j := i - 1; beside(j, j+1); j-- {
			w, whole := overhangSpace(p.p.Rune(j), false)
			before += float64(w * p.p.Advance(j))
			if !whole {
				break
			}
		}
		for j := i + 1; beside(j, j); j++ {
			w, whole := overhangSpace(p.p.Rune(j), true)
			after += float64(w * p.p.Advance(j))
			if !whole {
				break
			}
		}
		a.m[start] = -min(before, room[start])
		a.m[end] = -min(after, room[end])
		return
	}
	if beside(i-1, i) {
		a.m[start] = -min(half, room[start])
	}
	if beside(i+1, i+1) {
		a.m[end] = -min(half, room[end])
	}
}

// overhangSpace returns the share of the advance of r, a character beside
// a ruby column, an annotation under ruby-overhang: spaces hangs over,
// and whether the annotation may hang on past it; after reports a
// character after the column. The draft of CSS Ruby 1, 5.1 lets it hang
// over preserved white space, the no-break space and the other space
// separators, half of a fullwidth opening punctuation after the column
// and of a closing one before it, the halves its glyph leaves blank, and
// a quarter of a fullwidth middle dot on either side.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 lays a 60px
//     annotation over a 20px base between two characters and two spaces,
//     no-break spaces, ideographic spaces or thin spaces out as wide as
//     the annotation and the two characters, two no-break spaces on a
//     side alike; with U+FF08 after the column or U+FF09 before it, 10px
//     wider than without it; with U+30FB on each side, 20px wider.
func overhangSpace(r rune, after bool) (float64, bool) {
	switch {
	case r == '\t' || r == '\u00A0' || unicode.Is(unicode.Zs, r):
		return 1, true
	case r == '\u30FB' || r == '\uFF65':
		return 0.25, false
	case after && unicode.Is(unicode.Ps, r) && fullwidth(r), !after && unicode.Is(unicode.Pe, r) && fullwidth(r):
		return 0.5, false
	}
	return 0, false
}

// fullwidth reports a character of the CJK Symbols and Punctuation block
// or of the fullwidth forms of the Halfwidth and Fullwidth Forms block,
// whose punctuation takes a whole em.
func fullwidth(r rune) bool {
	return r >= 0x3000 && r <= 0x303F || r >= 0xFF00 && r <= 0xFF60
}

// newRubyBox returns the inline box of an anonymous ruby container of
// style st.
func (e *engine) newRubyBox(st *css.Style, ctx genCtx) *box {
	rb := e.newBox(nil, st, kindInline, ctx)
	rb.inline, rb.anon = true, true
	rb.decos = ctx.decos
	return rb
}

// annotations paints the ruby annotations of the columns on line l after
// the line's content, as Chromium paints them: the text of a line reads
// before its annotations.
func (p *painter) annotations(l *lineBox, page int) {
	for _, it := range l.items {
		a := it.atomic
		if a == nil || a.ruby == nil || a.ruby.ann == nil || !a.ruby.ann.laidOut || !p.paints(effectOwner(it.box, parentBox)) {
			continue
		}
		ox, oy := inlineOffset(it.box.parent)
		p.dx += ox
		p.dy += oy
		saved := p.mono
		p.mono = page
		p.context(a.ruby.ann)
		p.mono = saved
		p.dx -= ox
		p.dy -= oy
	}
}

// rubyRoom grows the line l, and returns by how much, by what the annotations over its ruby
// columns need past room, the room above it, and by what the annotations
// under the line before it, reaching under past it, need past the room
// above l's content. CSS Ruby 1, 3.6 leaves the amount to the user agent
// so long as lines of the same ruby do not overlap; litho lays the line
// out as Chromium does, on screen and in print: an annotation over a
// ruby takes the line's own half-leading, the room under the previous
// line's content and annotations, and the margins and padding before a
// block's first line, and the line grows by the rest; an annotation under
// a ruby takes its line's half-leading, then the room above the next
// line's content and annotations, and the next line moves down by the
// rest; a border takes nothing. litho counts the room within one block:
// the last line of the block before it gives none.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1, 20px Ahem at
//     line heights of 20, 24, 30 and 40px with 10px and 16px annotations,
//     on screen and through its PDF route: a line of 24px holding a 10px
//     annotation under its base is followed 30px lower by the next line
//     (the annotation reaches 8px under the line, the next line's
//     half-leading takes 2px); two such lines in a row are 30px apart; a
//     line with an annotation under it followed by a line with one over
//     it is 40px from it; an annotation over a line takes the half-leading
//     under the line before it, ruby or not.
func (s *inlineState) rubyRoom(l *lineBox, room, under float64) float64 {
	grow := s.rubyGrowth(l, room, under)
	if grow <= eps {
		return 0
	}
	l.h += grow
	l.baseline += grow
	for _, it := range l.items {
		it.baseline += grow
	}
	for _, f := range l.frags {
		f.top += grow
		f.bottom += grow
		f.base += grow
	}
	return grow
}

// rubyGrowth returns how much rubyRoom grows the line l, given room and
// under.
func (s *inlineState) rubyGrowth(l *lineBox, room, under float64) float64 {
	if s.root.rubyLine {
		// A ruby nested in a base: the column holding it makes the room.
		return 0
	}
	return max(l.annOver-max(room, 0), 0) + max(under-l.startRoom, 0)
}

// lowerLetter moves the initial letter f, float at of the formatting
// context, down by grow, the room its first line grows by for the ruby
// annotations over it: the letter's cap height sits on the line's, which
// moves down with its content (CSS Inline 3, 4.5.4), as Chromium places
// it.
//
// # Corpus evidence
//
//   - wpt: css-inline/initial-letter/initial-letter-block-position-drop-over-ruby,
//     a 3-line letter beside a first line of 24px that a 10px annotation
//     over its ruby grows by 8px starts 8px lower.
func (s *inlineState) lowerLetter(f *box, at int, grow float64) {
	if f == nil || grow <= eps {
		return
	}
	f.shift(0, grow)
	if at >= 0 && at < len(s.bc.floats) {
		s.bc.floats[at].y0 += grow
		s.bc.floats[at].y1 += grow
	}
}

// rubyEnd returns how much the block of the inline formatting context
// grows past its last line for the annotations under that line, which
// reach under past it by under: they take the block's end padding, not
// its border, as Chromium lays a block out (rubyRoom).
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 grows a block of
//     24px lines of 20px Ahem whose last line holds a 10px annotation
//     under its base by 8px with no end padding, by 4px with 4px, by none
//     with 8px or 20px, and by 8px with a 10px end border and no padding;
//     a margin under the block takes nothing.
func (s *inlineState) rubyEnd(under float64) float64 {
	if s.root.rubyLine || under <= eps {
		return 0
	}
	return max(under-s.root.pd[css.Bottom], 0)
}

// rubyElem returns the context a column of the base nodes base and the
// annotation ann generates in: in a tagged document, a Ruby structure
// element of its own when the column has both, since ISO 14289-2,
// 8.2.5.23 gives a Ruby the children RB, RT or RB, RP, RT, RP alone, and
// the context unchanged otherwise. tagged reports the Ruby made.
func (e *engine) rubyElem(base []*Node, ann *Node, ctx genCtx) (genCtx, bool) {
	if ctx.elem == nil || ann == nil || firstRune(base) == 0 || ctx.structDepth+2 >= maxStructDepth {
		return ctx, false
	}
	return e.structChild(ctx, "Ruby"), true
}

// structChild returns ctx with a new structure element of role as its
// element, the last kid of ctx's.
func (e *engine) structChild(ctx genCtx, role string) genCtx {
	el := &typeset.StructElem{Role: role}
	ctx.elem.Kids = append(ctx.elem.Kids, el)
	ctx.elem = el
	ctx.structDepth++
	return ctx
}

// heldByRB reports a base whose content RB elements hold: the nodes
// that draw are elements of role RB alone.
func heldByRB(base []*Node) bool {
	held := false
	for _, n := range base {
		switch {
		case n.Kind == Element && n.Role == "RB":
			held = true
		case n.Kind == Text && strings.Trim(n.Text, " \t\n\r\f") == "",
			n.Kind == Element && n.Style != nil && n.Style.Display == css.DisplayNone:
		default:
			return false
		}
	}
	return held
}
