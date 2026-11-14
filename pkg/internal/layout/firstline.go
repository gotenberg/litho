package layout

import (
	"fmt"
	"slices"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// firstLine is the first formatted line of an inline formatting context
// that a ::first-line styles (CSS Pseudo 4, 2.1). The first paragraph of
// the context is collected and shaped a second time from copies of its
// boxes styled for the first line, so the line is broken with the widths
// it is drawn with; the lines after it come from the paragraph as the
// boxes' own styles shape it, from where the first line ends.
type firstLine struct {
	// wrap is the inline box the ::first-line generates around the line's
	// content, which paints its background and decorations; root stands
	// for the root inline box on the line, whose font and line height are
	// the ::first-line's (2.1.2: line-height there sets the first line
	// box's height both ways).
	wrap, root *box
	// d holds the copies, in tree order, and the first paragraph
	// collected from them, its one paragraph.
	d *ifcData
	// parent is the copy each box the paragraph holds without a copy of
	// its own sits in: an atomic inline, a float, a positioned box.
	parent map[*box]*box
}

// para returns the first paragraph as the first line shapes it.
func (f *firstLine) para() *ifcPara { return f.d.paras[0] }

// firstLineStyle returns the ::first-line style of the first formatted
// line of b's inline formatting context, nil for none: the ::first-line of
// b's own element, else of the nearest block container whose first
// formatted line b's first line is (CSS Pseudo 4, 2.1.1), inheriting from
// b's style. CSS Pseudo 4 nests the ::first-line of every such block,
// outermost first; litho follows Chromium, which applies the nearest
// alone.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 draws a p whose
//     div has a ::first-line of red 24px text on cyan and whose own
//     ::first-line sets a yellow background in black 16px on yellow; a
//     div's ::first-line of colour red reaches the first line of its first
//     child p at the p's 20px font size and letter spacing, past a float
//     before the p, and not past an empty div before it.
func (e *engine) firstLineStyle(b *box) *css.Style {
	fl := e.opts.FirstLine
	if fl == nil {
		return nil
	}
	for c, depth := b, 0; c != nil && depth < maxTreeDepth; c, depth = c.parent, depth+1 {
		if c.node != nil && !c.anon && c.kind == kindBlock && c.flex == nil && c.grid == nil && c.tpart != partWrapper && c.tpart != partTable {
			if st := fl.Pseudo(c.node, b.st); st != nil {
				return st
			}
		}
		if !opensParentLine(c) {
			return nil
		}
	}
	return nil
}

// opensParentLine reports whether the first formatted line of the block
// container c is its parent's: c is the parent's first in-flow
// block-level child and takes part in the parent's block formatting
// context, which is no table wrapper, flex or grid container (CSS Pseudo
// 4, 2.1.1). An anonymous block of collapsible white space alone holds no
// line and is skipped, as a float or a positioned box is.
func opensParentLine(c *box) bool {
	p := c.parent
	if p == nil || p.kind != kindBlock || c.kind != kindBlock || c.inline || c.float != css.FloatNone || c.abs ||
		c.tpart != partNone || c.flexItem || c.gridItem || c.ruby != nil || c.rubyLine || p.flex != nil || p.grid != nil || p.ortho {
		return false
	}
	if p.tpart != partNone && p.tpart != partCell && p.tpart != partCaption {
		return false
	}
	for _, k := range p.kids {
		if k.float != css.FloatNone || k.abs || blankAnon(k) {
			continue
		}
		return k == c
	}
	return false
}

// blankAnon reports an anonymous block whose in-flow inline content is
// collapsible white space alone, which makes no line box (CSS 2.1, 9.4.2):
// the floats and positioned boxes it holds sit outside any line.
func blankAnon(k *box) bool {
	if !k.anon || !k.inlineKids {
		return false
	}
	for _, c := range k.kids {
		if c.float == css.FloatNone && !c.abs && !blankInline([]*box{c}, 0) {
			return false
		}
	}
	return true
}

// buildFirstLine copies the inline boxes of b's inline formatting context
// under an inline box of the ::first-line style st, restyled for the
// first line, and collects and shapes the first paragraph from them.
//
// A box on the first line inherits from the ::first-line, its parent's
// copy, rather than from its parent (CSS Pseudo 4, 2.1.3): an inline box
// takes the style the styler gives, a text run or a break its parent's
// copy's, unless its inherited style differs from its box parent's.
// Atomic inlines, floats and positioned boxes keep their own style and
// first line: they are not copied.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 draws the first
//     line of a block whose text sits in a span of display: contents in
//     the ::first-line's colour and size, but keeps that text blue and of
//     the block's size when the span sets its colour to blue; the
//     background of the ::first-line covers both.
func (e *engine) buildFirstLine(b *box, st *css.Style) *firstLine {
	f := &firstLine{parent: map[*box]*box{}}
	f.root = &box{st: st, kind: kindBlock, lang: b.lang}
	e.countBox(st)
	wrap := &box{
		st: st, kind: kindInline, inline: true, first: true, last: true, parent: b,
		lang: b.lang, elem: b.elem, link: b.link, depth: b.depth + 1, hidden: st.Visibility != css.Visible,
	}
	wrap.decos = append(blockDecos(b), ownDecos(wrap, st)...)
	wrap.kids = e.copyKids(f, b.kids, wrap, 0)
	f.wrap = wrap
	c := &collector{e: e, root: b, d: &ifcData{}, space: true, firstOnly: true}
	c.cur = &ifcPara{browser: e.opts.BrowserRules}
	c.walk(wrap)
	if !c.done {
		c.d.paras = append(c.d.paras, c.cur)
	}
	p := c.d.paras[0]
	c.d.paras = c.d.paras[:1]
	if !slices.ContainsFunc(p.closes, func(x posBox) bool { return x.box == wrap }) {
		// The first paragraph ends at a forced break inside the line's
		// boxes: the ::first-line box closes there.
		p.closes = append(p.closes, posBox{pos: p.n, box: wrap})
	}
	c.d.order = make(map[*box]int, len(c.d.boxes))
	for i, k := range c.d.boxes {
		c.d.order[k] = i
	}
	f.d = c.d
	p.index()
	p.transformSpaces()
	e.shapePara(b, p)
	return f
}

// countBox counts a box of style st against [MaxBoxes] and the budget,
// as [engine.newBox] does.
func (e *engine) countBox(st *css.Style) {
	e.boxes++
	e.tick()
	if e.boxes > MaxBoxes {
		e.fail(fmt.Errorf("%w: more than %d", ErrTooManyBoxes, MaxBoxes))
	}
	e.charge(boxCost, "boxes")
	e.holdStyle(st)
}

// copyBox returns a copy of b styled st, counted as [engine.countBox]
// does and recorded as standing for b.
func (e *engine) copyBox(b *box, st *css.Style) *box {
	e.countBox(st)
	c := new(box)
	*c = *b
	c.st = st
	c.hidden = st.Visibility != css.Visible
	if e.cloneOf == nil {
		e.cloneOf = map[*box]*box{}
	}
	if o, ok := e.cloneOf[b]; ok {
		b = o
	}
	e.cloneOf[c] = b
	return c
}

// copyKids returns the copies of kids, the children of a box whose copy
// on the first line is parent, at depth below the line's root.
func (e *engine) copyKids(f *firstLine, kids []*box, parent *box, depth int) []*box {
	out := make([]*box, 0, len(kids))
	for _, k := range kids {
		if e.err != nil {
			return out
		}
		if k.kind != kindInline && k.kind != kindText && k.kind != kindBreak || k.float != css.FloatNone || k.abs || depth >= maxTreeDepth {
			f.parent[k] = parent
			out = append(out, k)
			continue
		}
		st := k.st
		switch {
		case k.kind == kindInline && k.node != nil:
			if r := e.opts.FirstLine.Restyle(k.node, parent.st); r != nil {
				st = r
			}
		case k.kind != kindInline && !k.marker && k.parent != nil && css.InheritsAlike(k.st, k.parent.st):
			// A text run or a break inherits what its box's parent does,
			// as it shares its parent's style (Node.Style), and takes the
			// copy's. One under an element of display: contents that sets
			// an inherited property keeps its own style, and an inside
			// marker the ::marker's.
			st = parent.st
		}
		c := e.copyBox(k, st)
		c.parent, c.depth = parent, parent.depth+1
		c.ifc, c.lines = nil, nil
		if k.kind == kindInline {
			c.decos = append(slices.Clip(parent.decos), ownDecos(c, st)...)
			c.kids = e.copyKids(f, k.kids, c, depth+1)
		} else {
			c.kids = nil
			c.decos = parent.decos
		}
		out = append(out, c)
	}
	return out
}

// blockDecos returns the decorations the inline content of b takes from
// b and its ancestors (CSS Text Decoration 3, 2.1): those of a text run or
// inline box of b whose decorating box is no inline box.
func blockDecos(b *box) []deco {
	stack := slices.Clone(b.kids)
	for steps := 0; len(stack) > 0 && steps < maxTreeDepth; steps++ {
		k := stack[0]
		stack = stack[1:]
		if k.float != css.FloatNone || k.abs {
			continue
		}
		switch k.kind { //nolint:exhaustive // atomic boxes decorate their content with their own
		case kindText:
			return blockOrigin(k.decos)
		case kindInline:
			if len(k.kids) == 0 {
				return blockOrigin(k.decos)
			}
			stack = append(slices.Clone(k.kids), stack...)
		}
	}
	return nil
}

// blockOrigin returns the decorations of ds whose decorating box is not
// an inline box.
func blockOrigin(ds []deco) []deco {
	var out []deco
	for _, d := range ds {
		if d.origin == nil || d.origin.kind != kindInline {
			out = append(out, d)
		}
	}
	return out
}

// boxRange returns the characters of p the runs of box k take, start to
// end; start is -1 when no run of p belongs to k.
func (p *ifcPara) boxRange(k *box) (int, int) {
	pos, start, end := 0, -1, -1
	for i, r := range p.runs {
		n := 1
		if r.Object == nil {
			n = utf8.RuneCountInString(r.Text)
		}
		if p.infos[i].box == k {
			if start < 0 {
				start = pos
			}
			end = pos + n
		}
		pos += n
	}
	return start, end
}

// rest returns where the lines after the first start in n, the first
// paragraph as the boxes' own styles shape it, when the first line ends
// at end of the paragraph the first line shapes. The two hold the same
// boxes in the same order; a text run whose text-transform differs on the
// first line may hold another count of characters there, and the
// position inside it is found again from its source text.
func (e *engine) rest(f *firstLine, n *ifcPara, end int) int {
	fp := f.para()
	if end >= fp.p.Len() {
		return n.p.Len()
	}
	fb := fp.infos[fp.p.RunOf(end)].box
	ob := fb
	if o, ok := e.cloneOf[fb]; ok {
		ob = o
	}
	fs, fe := fp.boxRange(fb)
	ns, ne := n.boxRange(ob)
	if fs < 0 || ns < 0 {
		return min(end, n.p.Len())
	}
	off := end - fs
	if fe-fs != ne-ns && fb.kind == kindText {
		off = transformedOffset(ob, fb.st.TextTransform, ob.st.TextTransform, off, fe-fs)
	}
	return min(ns+min(off, ne-ns), n.p.Len())
}

// transformedOffset returns the character of k's text, transformed by to
// and its white space collapsed, that the character off of the same text
// transformed by from stands for; flen is the count of characters k's
// text makes under from. Each source character is transformed alone: a
// case mapping that changes the count of characters changes it
// character by character, and a collapsible run of white space counts
// once under both.
func transformedOffset(k *box, from, to css.TextTransform, off, flen int) int {
	collapse := collapses(k.st.WhiteSpace)
	walk := func(upto int) (int, int) {
		var prevFrom, prevTo rune
		f, n := 0, 0
		space := false
		for _, r := range k.text {
			if upto >= 0 && f >= upto {
				break
			}
			if collapse && (r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f') {
				if !space {
					f++
					n++
				}
				space = true
				continue
			}
			space = false
			f += utf8.RuneCountInString(transform(string(r), from, k.lang, &prevFrom))
			n += utf8.RuneCountInString(transform(string(r), to, k.lang, &prevTo))
		}
		return f, n
	}
	total, _ := walk(-1)
	// A collapsible space that starts the text after another was removed:
	// it counts under neither.
	skip := max(total-flen, 0)
	_, n := walk(off + skip)
	return max(n-skip, 0)
}
