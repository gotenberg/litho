package layout

import (
	"fmt"
	"math"
	"slices"
	"unicode"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// lineBox is one line box of an inline formatting context (CSS 2.1,
// 9.4.2).
type lineBox struct {
	// x and y are the line box's top-left corner in flow coordinates, w
	// its width, h its height and baseline the offset of its baseline from
	// y.
	x, y, w, h float64
	baseline   float64
	items      []*lineItem
	frags      []*inlineFrag
	// overflows reports a line taller than its page area for an atomic
	// inline taller than the area: it overflows onto the pages below with
	// that monolithic content (CSS Fragmentation 3, 4.1). A line taller
	// than the area for its text alone takes one page and is cut there.
	overflows bool
	// slack is the room an Office proportional line spacing adds below
	// the line's glyphs, which may hang past a page cut: the line fits a
	// page when the rest of it does, and then ends at the cut.
	slack float64
	// hangW is the advance of the spaces that hang at the line's logical
	// end, drawn on its left when hangLeft (a right-to-left paragraph),
	// else on its right.
	hangW    float64
	hangLeft bool
	// bars are the line-relative positions of the bar tab stops of an
	// Office container, drawn across the line in barColor.
	bars     []float64
	barColor typeset.Color
	barOwner *box
	// changed is the block container of a line that holds content a
	// tracked change touches (Node.ChangeBar), nil for any other line;
	// changeHead reports a line whose first item is such content: the
	// line before it in its paragraph draws a bar too.
	changed    *box
	changeHead bool
	// annOver and annUnder are how far the ruby annotations over and
	// under the line's columns reach above its top and below its bottom;
	// startRoom and endRoom are the room above and under the line's
	// content and annotations, which the annotations of the lines beside
	// it take before a line grows (rubyRoom).
	annOver, annUnder, startRoom, endRoom float64
	// annEnd is how far the block grows under the line, its last, for
	// the annotations under the line (rubyEnd).
	annEnd float64
	// trimmedOver and trimmedUnder report a line whose over or under
	// edge a block container already trimmed by text-box-trim: the
	// innermost one that asks decides (CSS Inline 3, 6.2).
	trimmedOver, trimmedUnder bool
	// colTrimmed reports a line moved up past the top of its column by the
	// column's text-box-trim ([inlineState.columnTrim]).
	colTrimmed bool
	// withEnd reports a last line moved to the next page to keep its
	// block's end padding and border ([inlineState.endEdges]): the break
	// before it does not count against orphans and widows.
	withEnd bool
}

// lineItem is one piece of a line: a glyph run of a text box, or an
// atomic inline. Positions are relative to the line box.
type lineItem struct {
	box *box
	// text is the glyph run, its Origin relative to the line box; nil for
	// an atomic inline or a piece that draws no glyph.
	text   *typeset.Text
	atomic *box
	x, w   float64
	// baseline is the item's baseline below the line top, ascent and
	// descent its extent, and gap the line gap of a glyph run's face.
	baseline, ascent, descent float64
	gap                       float64
	ulPos, ulThick            float64
	stPos, stThick            float64
	// start and end are the item's characters in its paragraph, hang how
	// many of them hang at the line end, and rtl reports glyphs drawn
	// right to left.
	start, end, hang int
	rtl              bool
	// tab reports a tab, whose glyph run, when any, is its leader.
	tab bool
}

// inlineFrag is the part of an inline box on one line, painted with its
// background and borders (CSS 2.1, 8.6 and CSS Backgrounds 3, 5.4).
type inlineFrag struct {
	box *box
	// x0 and x1 are the border box's left and right edges, top and bottom
	// its top and bottom, and base its baseline, relative to the line box.
	x0, x1, top, bottom float64
	base                float64
	// left and right report whether the fragment draws the box's left and
	// right edges (box-decoration-break: slice).
	left, right bool
}

// inlineEdges returns the widths inline box b takes at its logical start
// and end: margin, border and padding of its start side on its first
// fragment and of its end side on its last, the start side being the left
// one in a left-to-right box and the right one in a right-to-left box (CSS
// 2.1, 8.6 and 9.10).
func inlineEdges(b *box) (float64, float64) {
	st, en := css.Left, css.Right
	if b.st.Direction == css.RTL {
		st, en = en, st
	}
	l, r := 0.0, 0.0
	if b.first {
		l = b.m[st] + b.bd[st] + b.pd[st]
	}
	if b.last {
		r = b.m[en] + b.bd[en] + b.pd[en]
	}
	return l, r
}

// edgeSides reports whether the fragment of inline box b that
// holds its logical start (start) or end (end) draws its left or right
// edge: a left-to-right box draws its left edge on its first fragment, a
// right-to-left one on its last.
func edgeSides(b *box, start, end bool) (left, right bool) {
	if b.st.Direction == css.RTL {
		return b.last && end, b.first && start
	}
	return b.first && start, b.last && end
}

// prepareInline resolves the edges of the inline boxes of d and lays its
// atomic inlines out against the containing block cb, setting the advance
// of their object characters.
func (e *engine) prepareInline(d *ifcData, cb cblock) {
	cbw := cb.w
	for _, k := range d.boxes {
		if k.kind == kindInline {
			resolveEdges(k, cbw)
			// Vertical padding and borders paint but take no room.
		}
	}
	for _, p := range d.paras {
		if p.p == nil {
			continue
		}
		for i := range p.p.Len() {
			info := p.infos[p.p.RunOf(i)]
			switch info.kind {
			case runAtomic:
				if e.err != nil {
					return
				}
				if isLeader(info.box) {
					e.prepareLeader(info.box, cbw)
					p.p.SetAdvance(i, 0)
					continue
				}
				e.layoutAtomic(info.box, cb)
				if info.box.ruby != nil {
					e.rubyOverhang(p, i, info.box)
				}
				p.p.SetAdvance(i, max(info.box.marginW(), 0))
			case runEmpty:
				p.p.SetAdvance(i, 0)
			case runText, runTab:
			}
		}
	}
}

// paraWidths returns the width every character of p takes on a line: its
// advance, the start edges of the boxes opening before it and the end edges
// of those closing after it. object, when set, gives an atomic's width
// instead of its laid-out advance.
func (p *ifcPara) paraWidths(object func(*box) float64) []float64 {
	n := p.p.Len()
	w := make([]float64, n)
	for i := range n {
		w[i] = p.p.Advance(i)
		if object != nil {
			if info := p.infos[p.p.RunOf(i)]; info.kind == runAtomic {
				w[i] = object(info.box)
			}
		}
	}
	for _, o := range p.opens {
		if o.pos < n {
			l, _ := inlineEdges(o.box)
			w[o.pos] += l
		}
	}
	for _, c := range p.closes {
		if c.pos > 0 && c.pos <= n {
			_, r := inlineEdges(c.box)
			w[c.pos-1] += r
		}
	}
	return w
}

// wsAt returns the white-space value in force at character i of p.
func (p *ifcPara) wsAt(i int) css.WhiteSpace {
	return p.infos[p.p.RunOf(i)].ws
}

// styleAt returns the style of the run of character i of p.
func (p *ifcPara) styleAt(i int) *css.Style {
	return p.infos[p.p.RunOf(i)].box.st
}

// allowed reports a soft wrap opportunity before character i of p (CSS
// Text 3, 5): a UAX #14 opportunity where white space allows wrapping and
// word-break keep-all does not suppress it, any grapheme boundary between
// letters under word-break: break-all, any grapheme boundary under
// line-break: anywhere (5.3), and the boundary after every preserved space
// under white-space: break-spaces (3).
func (p *ifcPara) allowed(i int) bool {
	if i <= 0 || i >= p.p.Len() {
		return false
	}
	before, after := p.wsAt(i-1), p.wsAt(i)
	if !wraps(before) && !wraps(after) || p.leaderGlue(i) {
		return false
	}
	if p.leaderAt(i) {
		return true
	}
	a, b := p.p.Rune(i-1), p.p.Rune(i)
	if a == softHyphen && p.styleAt(i-1).Hyphens == css.HyphensNone {
		// hyphens: none breaks no word at its soft hyphens (CSS Text 3,
		// 5.4).
		return false
	}
	if p.styleAt(i).LineBreak == css.LineBreakAnywhere && wraps(after) && p.p.GraphemeStart(i) && (after == css.BreakSpaces || !isPreservedSpace(b)) {
		// A preserved space that hangs at a line's end (CSS Text 3,
		// 4.1.3) stays with the character before it: a break before it
		// would set it alone on a line.
		return true
	}
	if before == css.BreakSpaces && isPreservedSpace(a) && p.infos[p.p.RunOf(i-1)].kind != runAtomic {
		return true
	}
	if after == css.BreakSpaces && isPreservedSpace(b) {
		// Spaces stay with the word before them: they wrap only after
		// themselves.
		return false
	}
	if p.p.Break(i) == text.NoBreak {
		// break-all on either side opens the boundary between two letters,
		// as at the edge of a span that sets it.
		breakAll := p.styleAt(i).WordBreak == css.BreakAll || p.styleAt(i-1).WordBreak == css.BreakAll
		if breakAll && p.p.GraphemeStart(i) && !unicode.IsSpace(b) && !unicode.IsPunct(b) && !unicode.IsSpace(a) {
			return true
		}
		return (p.tailoredBreak(i) || p.hyphenBeforeDigit(i)) && !p.keptTogether(i, a, b)
	}
	if p.strictBreak(i) {
		return false
	}
	if p.p.Break(i) == text.BreakWord && p.styleAt(i).WordBreak == css.WordBreakManual {
		// word-break: manual detects no word boundary in text written
		// without spaces (CSS Text 4, 5.2).
		return false
	}
	return !p.keptTogether(i, a, b)
}

// keptTogether reports a boundary between two letters or digits that
// word-break: keep-all closes (CSS Text 3, 5.3): the opportunities of UAX
// #14 and those line-break opens between the typographic letter units of
// CJK alike; one between two Hangul characters that
// [css.Office.KeepHangul] closes; and one between two emoji that
// [css.Office.KeepEmoji] closes.
func (p *ifcPara) keptTogether(i int, a, b rune) bool {
	st := p.styleAt(i)
	return st.WordBreak == css.KeepAll && isWordChar(a) && isWordChar(b) ||
		st.KeepHangul && unicode.Is(unicode.Hangul, a) && unicode.Is(unicode.Hangul, b) ||
		st.KeepEmoji && text.EmojiJoins(a, b)
}

// hangsPunct reports whether character i of p is punctuation that may
// hang past the end of a line of the block container root
// ([css.Office.HangPunctuation]): an ideographic comma or full stop, or
// a fullwidth comma or full stop, after which the line may break.
func (p *ifcPara) hangsPunct(root *box, i int) bool {
	if root == nil || !root.st.HangPunctuation {
		return false
	}
	switch p.p.Rune(i) {
	case '、', '。', '，', '．':
		return i+1 >= p.p.Len() || p.allowed(i+1)
	}
	return false
}

// isPreservedSpace reports a space or tab that break-spaces keeps and
// wraps after.
func isPreservedSpace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\u3000'
}

// isWordChar reports a letter or digit, which keep-all keeps together.
func isWordChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// breakAnywhere reports whether the run of character i may break between
// any two grapheme clusters when nothing else fits: overflow-wrap
// break-word or anywhere (CSS Text 3, 5.5).
func (p *ifcPara) breakAnywhere(i int) bool {
	st := p.styleAt(i)
	return wraps(st.WhiteSpace) && (st.OverflowWrap == css.BreakWord || st.OverflowWrap == css.Anywhere || st.WordBreak == css.BreakAll)
}

// hangs reports whether a space at character i hangs at a line end:
// preserved spaces under white-space: pre do not (CSS Text 3, 4.1.3).
func (p *ifcPara) hangs(i int) bool {
	return !keepsSpaces(p.wsAt(i)) && text.IsHangingSpace(p.p.Rune(i))
}

// collapsibleSpace reports a collapsible space at character i, removed at
// the start of a line (CSS Text 3, 4.1.2).
func (p *ifcPara) collapsibleSpace(i int) bool {
	return p.p.Rune(i) == ' ' && collapses(p.wsAt(i)) && p.infos[p.p.RunOf(i)].kind == runText
}

// defaultTabSize is the initial value of tab-size, in spaces (CSS Text 3,
// 4.2), which a tab-size of no other kind takes.
const defaultTabSize = 8

// tabWidth returns the advance of a preserved tab at line offset x: to the
// next tab stop, a multiple of tab-size (CSS Text 3, 4.2), the next one
// when closer than half a space. A number of spaces counts the space's
// advance with its letter-spacing and word-spacing, which the shaped space
// already carries. A tab in an initial letter measures in its paragraph's
// space ([tabBox]).
func (e *engine) tabWidth(b *box, x float64) float64 {
	b = tabBox(b)
	space := e.spaceWidth(b.st, b.lang)
	step := float64(defaultTabSize * space)
	switch b.st.TabSize.Kind {
	case css.Number:
		step = float64(max(clamp(b.st.TabSize.Value), 0) * space)
	case css.Points:
		step = max(clamp(b.st.TabSize.Value), 0)
	case css.Auto, css.Percent, css.None, css.Normal, css.FlexContent, css.MinContent, css.MaxContent, css.FitContent, css.Stretch, css.Calc:
	}
	if step <= eps {
		return 0
	}
	next := float64((math.Floor(x/step+eps) + 1) * step)
	if next-x < space/2 {
		next += step
	}
	return next - x
}

// breakLine returns the end of the line that starts at character start of
// p, in a width of avail with the first offset x0, following the rules of
// CSS Text 3, 5: break at the last opportunity that fits, else at a
// grapheme boundary when overflow-wrap allows, else overflow to the next
// opportunity. fits reports whether anything fits. root is the block
// container and org the offset of the line's start from the start edge of
// its content box, where the tab stops of an Office container are
// measured from.
func (e *engine) breakLine(p *ifcPara, w []float64, start int, x0, avail float64, root *box, org float64) (int, bool) {
	end, fits := e.breakLineIn(p, w, start, x0, avail, root, org)
	if !fits || end >= p.p.Len() || root == nil || root.st.JustifyShrink <= 0 || lineAlign(root.st, false) != css.AlignJustify {
		return end, fits
	}
	return e.shrinkBreak(p, w, start, end, x0, avail, root, org), true
}

// breakLineIn returns where the line from start breaks in avail, as
// breakLine does without shrinking the word separators.
//
// # Corpus evidence
//
//   - libreoffice/docx ooxmlexport/fdo38414.docx, fdo76586.docx and
//     libreoffice/odt odfimport/fdo56272.odt: setting the first cluster of
//     a line too narrow for it alone on a line drops their text F1
//     against Word's and Writer's PDFs (0.660 to 0.132 for fdo38414,
//     which grows a page): Office paragraphs let such a line overflow.
func (e *engine) breakLineIn(p *ifcPara, w []float64, start int, x0, avail float64, root *box, org float64) (int, bool) {
	n := p.p.Len()
	x := x0
	last, over := -1, -1
	office := root != nil && officeTabs(root.st)
	// hung is the advance the annotations of ruby columns took from the
	// tabs before, which the next tab stops are measured past.
	hung := 0.0
	for i := start; i < n; i++ {
		// A break at a soft hyphen fits when the hyphen it adds does; one
		// that does not still ends a line nothing else ends.
		if i > start && p.allowed(i) {
			if !p.hyphenates(i) || x+e.hyphenWidth(p, i) <= avail+eps {
				last = i
			} else {
				over = i
			}
		}
		wi := w[i]
		if info := p.infos[p.p.RunOf(i)]; info.kind == runTab {
			brk := false
			if office {
				wi, brk = e.officeTab(root, p, w, i, org+x, org+avail)
			} else {
				wi = e.tabWidth(info.box, x+hung)
				h := e.tabOverhang(p, i, wi)
				wi -= h
				hung += h
			}
			p.p.SetAdvance(i, wi)
			w[i] = wi
			if brk && i > start {
				// The tab and the word after it move to the next line
				// together, as Writer moves a tab portion.
				return i, true
			}
		}
		if x+wi > avail+eps && !p.hangs(i) && !p.hangsPunct(root, i) {
			if last > start {
				return last, true
			}
			if over > start {
				return over, false
			}
			if i > start && p.breakAnywhere(i) {
				j := i
				for j > start+1 && !p.p.GraphemeStart(j) {
					j--
				}
				if j > start {
					return j, true
				}
			}
			if ow := p.styleAt(i).OverflowWrap; e.opts.BrowserRules && i == start && wraps(p.wsAt(i)) && (ow == css.Anywhere || ow == css.BreakWord) {
				// Not even the first grapheme cluster fits: it takes the
				// line alone, with the spaces that hang after it (CSS
				// Text 3, 5.5 and 4.1.3). An Office paragraph keeps the
				// line overflowing, as Word and Writer print it.
				j := i + 1
				for j < n && !p.p.GraphemeStart(j) {
					j++
				}
				for j < n && p.hangs(j) {
					j++
				}
				return j, false
			}
			if i == start || last < 0 {
				// Nothing fits: the line overflows to the next opportunity.
				for j := i + 1; j < n; j++ {
					if p.allowed(j) {
						return j, false
					}
				}
				return n, false
			}
		}
		x += wi
	}
	return n, true
}

// vbox is an inline box present on a line, for vertical alignment (CSS 2.1,
// 10.8): its baseline shift from the root baseline, positive down, and its
// extent above and below its baseline.
type vbox struct {
	b      *box
	parent *vbox
	shift  float64
	// above and below are the box's half-leading extent (A' and D'), ca and
	// cd its content area's.
	above, below float64
	ca, cd       float64
	size         float64
	lh           float64
	// xh is the box's x-height, for vertical-align: middle.
	xh float64
	// aligned reports top or bottom alignment, settled against the line
	// box; inAligned a box inside one.
	aligned   bool
	inAligned bool
}

// inlineState is the state of one pass over an inline formatting context.
type inlineState struct {
	e      *engine
	root   *box
	d      *ifcData
	cb     cblock
	bc     *bfc
	lines  []*lineBox
	vboxes map[*box]*vbox
	// rootV is the root inline box.
	rootV *vbox
	// tail is the block-end padding and border of the root that follow
	// the line being placed, its last, 0 for any other line, and moved
	// reports that they moved it ([inlineState.position]).
	tail  float64
	moved bool
	// under is the room under the text of the line being placed that a
	// column's trim-end lets hang past the column's end
	// ([inlineState.columnTrim]).
	under float64
	// glyphs counts the glyphs of the line being built.
	glyphs int
	// placed holds the floats already placed.
	placed map[*box]bool
	// keep makes buildLine keep the spaces at the line end, which a line
	// cut before an overflow mark draws (CSS Overflow 4, 5.1).
	keep bool
	// seg is set while the line being laid out is a piece of a line cut
	// by a float of css.FrameWrapBoth: it starts at segTop, in the piece
	// of the band past segFrom, and the pieces before it end at
	// segBottom.
	seg                     bool
	segTop, segFrom, segBot float64
	// unfit counts the lines whose content overflows their band.
	unfit int
	// fl is the first formatted line while it is built, nil otherwise:
	// its boxes hang from its copies (firstline.go).
	fl *firstLine
}

// band returns the left and right edges of the space beside the floats in
// the band [y, y+h) of the containing block, past the pieces of the line
// laid out at y already.
func (s *inlineState) band(y, h float64) (float64, float64) {
	cb := s.cb
	if s.seg && math.Abs(y-s.segTop) <= eps {
		return s.bc.bandFrom(y, h, cb.x, cb.x+cb.w, s.segFrom)
	}
	return s.bc.lineBand(y, h, cb.x, cb.x+cb.w)
}

// nextPiece reports whether the line at y of height h has a piece of
// space past from, the floats of css.FrameWrapBoth cutting it.
func (s *inlineState) nextPiece(y, h, from float64) bool {
	if !s.bc.hasBoth() {
		return false
	}
	l, r := s.bc.bandFrom(y, h, s.cb.x, s.cb.x+s.cb.w, from)
	return r > l+eps && l >= from-eps
}

// placeWordFloats places the floats of css.Office.WordAnchor of p, top
// being the top of the first line of its block container.
func (s *inlineState) placeWordFloats(p *ifcPara, top float64) {
	if len(s.lines) > 0 {
		top = s.lines[0].y
	}
	for _, f := range p.floats {
		if s.placed[f.box] || !f.box.st.WordAnchor.Set {
			continue
		}
		s.placed[f.box] = true
		s.e.layoutWordFloat(f.box, s.cb, s.bc, top)
	}
}

// layoutInline lays out the inline formatting context of b in the content
// box cb, from cursor with the margins pending in mc, and returns the
// bottom of its last line box. It takes up to [maxFragmentationPasses]
// passes to honour orphans and widows (CSS 2.1, 13.3.3).
func (e *engine) layoutInline(b *box, cb cblock, bc *bfc, cursor float64, mc *marginState) float64 {
	d := e.ifcOf(b)
	if e.err != nil {
		return cursor
	}
	e.prepareInline(d, cb)
	if f := e.firstLines[b]; f != nil {
		e.prepareInline(f.d, cb)
	}
	forced := map[int]bool{}
	floats, warnings := len(bc.floats), len(e.warnings)
	saved := mc.clone()
	clampSaved := e.clampSnapshot()
	var end float64
	if b.st.TextWrapStyle == css.WrapStyleBalance && e.foot == nil && e.clamp == nil && e.balance.root == nil {
		// A line clamp container's lines are not balanced: what it keeps
		// depends on the lines it counts.
		restore := func() {
			e.clampRestore(clampSaved)
			bc.floats = bc.floats[:floats]
			e.rollbackWarnings(warnings)
			*mc = saved.clone()
		}
		e.balanceLines(b, d, cb, bc, cursor, mc, restore)
		restore()
		defer func() { e.balance = balanceState{} }()
	}
	for pass := range maxFragmentationPasses {
		e.clampRestore(clampSaved)
		end = e.flowLines(b, d, cb, bc, cursor, mc, forced)
		if !e.paginating() || e.err != nil || pass == maxFragmentationPasses-1 {
			break
		}
		changed := e.pushWordFloats(b, d, forced) || e.fixOrphans(b, forced)
		if !changed || !e.relayout(cursor) {
			break
		}
		bc.floats = bc.floats[:floats]
		e.rollbackWarnings(warnings)
		*mc = saved.clone()
	}
	if e.opts.release == releaseEagerly && e.clamp == nil {
		// A line clamp marks its last line once its container is laid
		// out, from the shaped text.
		e.releaseShaped(b)
	}
	return end
}

// maxBalancedLines is the most lines text-wrap: balance balances: a block
// of more lines breaks as auto does, as Chromium lays it out (CSS Text 4,
// 6.2 lets the user agent limit the lines it balances).
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 (Gotenberg 9.4.1) balances paragraphs of
//     2 to 6 lines and breaks one of 7 lines or more greedily.
const maxBalancedLines = 6

// maxWrapBalancePasses bounds the layouts of one block text-wrap: balance
// tries. Anti-DoS guard, not spec-mandated: each halves the range of the
// inset searched, and 16 halvings narrow a page-wide block's to a hundredth
// of a point.
const maxWrapBalancePasses = 16

// balanceState is the inset text-wrap: balance narrows the lines of root
// by, and the lines that overflowed in the last layout of a block.
type balanceState struct {
	root  *box
	inset float64
	unfit int
}

// balanceLines finds the inset text-wrap: balance lays the lines of b out
// with (CSS Text 4, 6.2): the widest that keeps as many lines, each
// fitting as many as did, narrowing the band of every line by it as
// Chromium does, so a line beside a float keeps the float's room. It
// lays b out once to count its lines, then halves the range of insets
// [maxWrapBalancePasses] times at most, calling restore before each layout,
// and leaves the inset found in e.balance for the layouts that follow.
func (e *engine) balanceLines(b *box, d *ifcData, cb cblock, bc *bfc, cursor float64, mc *marginState, restore func()) {
	e.balance = balanceState{root: b}
	forced := map[int]bool{}
	restore()
	e.flowLines(b, d, cb, bc, cursor, mc, forced)
	n, unfit := len(b.lines), e.balance.unfit
	if e.err != nil || n < 2 || n > maxBalancedLines || e.paginating() && e.pageOf(b.lines[0].y) != e.pageOf(b.lines[n-1].y) {
		return
	}
	lo, hi := 0.0, cb.w
	for range maxWrapBalancePasses {
		mid := (lo + hi) / 2
		e.balance.inset = mid
		restore()
		e.flowLines(b, d, cb, bc, cursor, mc, forced)
		if e.err != nil {
			return
		}
		if len(b.lines) == n && e.balance.unfit <= unfit {
			lo = mid
		} else {
			hi = mid
		}
	}
	e.balance.inset = lo
}

// pushWordFloats adds the first line of b to forced when a float of
// css.Office.WordAnchor positioned from b's paragraph would end below its
// page's area, b's first line not starting the page: Word moves the
// paragraph, and the drawing with it, to the next page. It reports
// whether it added it.
//
// # Corpus evidence
//
//   - litho/docx images-wrap: a 93.6 pt picture of wrapThrough at the top
//     of a paragraph whose top lies 60 pt above the bottom margin moves
//     to the next page with its paragraph and the heading kept with it.
func (e *engine) pushWordFloats(b *box, d *ifcData, forced map[int]bool) bool {
	if forced[0] || len(b.lines) == 0 || e.atPageTop(b.lines[0].y) {
		return false
	}
	for _, p := range d.paras {
		for _, f := range p.floats {
			a := f.box.st.WordAnchor
			if !a.Set || a.FromPage || !f.box.laidOut {
				continue
			}
			pg := e.pageOf(f.box.anchorTop + eps)
			if f.box.anchorTop+a.Y+f.box.borderH() > e.pageTop(pg)+e.areaH(pg)+eps {
				// The paragraph moves whole: the breaks found for its
				// lines where it was hold no more.
				clear(forced)
				forced[0] = true
				return true
			}
		}
	}
	return false
}

// fixOrphans adds to forced the line indices that must start a page so
// that every page break inside b leaves at least orphans lines before it
// and widows lines after it, and reports whether it added one.
func (e *engine) fixOrphans(b *box, forced map[int]bool) bool {
	lines := b.lines
	// A count past the lines acts as one more than them; the cap keeps
	// k - (widows - after) - segStart inside a 32-bit int.
	orphans, widows := min(max(b.st.Orphans, 1), len(lines)+1), min(max(b.st.Widows, 1), len(lines)+1)
	changed := false
	segStart := 0
	for k := 1; k < len(lines); k++ {
		if e.pageOf(lines[k].top()) == e.pageOf(lines[k-1].top()) {
			continue
		}
		if lines[k].withEnd {
			// Chromium breaks before a last line its block's end padding
			// moves whatever orphans and widows ask.
			segStart = k
			continue
		}
		next := len(lines)
		for j := k + 1; j < len(lines); j++ {
			if e.pageOf(lines[j].top()) != e.pageOf(lines[j-1].top()) {
				next = j
				break
			}
		}
		before, after := k-segStart, next-k
		switch {
		case before < orphans && segStart == 0 && !forced[0] && !e.atPageTop(lines[0].y) && (b.st.WordRules > 0 || !avoidedBefore(b)):
			// Moving the first lines would break before b where
			// break-before or break-after avoids it: orphans and widows
			// give way first (CSS Fragmentation 3, 4.4), as Chromium
			// lays them out. Word moves the blocks kept with b along
			// (css.Style.WordRules).
			forced[0] = true
			changed = true
		case after < widows && next == len(lines):
			// A block too short to leave orphans lines before the break
			// and widows after it keeps its orphans and breaks, as
			// Chromium does.
			k2 := k - (widows - after)
			switch {
			case k2-segStart >= orphans && k2 > segStart && !forced[k2]:
				forced[k2] = true
				changed = true
			case b.st.WordRules > 0 && segStart == 0 && !forced[0] && !e.atPageTop(lines[0].y):
				// Word moves the whole paragraph instead
				// (css.Style.WordRules).
				forced[0] = true
				changed = true
			}
		}
		segStart = k
	}
	return changed
}

// flowLines breaks the paragraphs of d into line boxes and places them from
// cursor, line by line, beside the floats of bc, and returns the bottom of
// the last line. A line index in forced starts a new page.
func (e *engine) flowLines(b *box, d *ifcData, cb cblock, bc *bfc, cursor float64, mc *marginState, forced map[int]bool) float64 {
	s := &inlineState{e: e, root: b, d: d, cb: cb, bc: bc, vboxes: map[*box]*vbox{}, placed: map[*box]bool{}}
	for _, k := range d.boxes {
		k.anchorSet = false
	}
	fl := e.firstLines[b]
	if fl != nil {
		for _, k := range fl.d.boxes {
			k.anchorSet = false
		}
	}
	b.lines = nil
	s.rootV = s.newVbox(b, nil)
	y := cursor
	first := true
	// room is what lies above the next line that a ruby annotation over
	// it may take: the margins and padding before the first line, the
	// room under the previous line's content after it; under is how far
	// the annotations under the previous line reach below it (rubyRoom).
	room, under := 0.0, 0.0
	indent := resolve(b.st.TextIndent, cb.w)
	after := afterBlock(b)
	strutH := s.rootV.above + s.rootV.below
	afterForced := false
	rootV := s.rootV
	for pi, p := range d.paras {
		if e.err != nil {
			return y
		}
		// The first line box is the first formatted line: the first
		// paragraph's, not the one after a forced break (CSS Pseudo 4,
		// 2.1.1).
		var first1 *firstLine
		if pi == 0 {
			first1 = fl
		}
		prevForced := afterForced
		afterForced = p.forced
		if (p.forced || p.p != nil && p.p.Len() > 0) && e.clampDiscards() {
			// A line clamp container discards what follows its last line
			// (CSS Overflow 4, 6).
			return s.discard(d, p, y)
		}
		if p.p == nil || p.p.Len() == 0 {
			s.recordEmptyAnchors(p, 0, 0, &lineBox{x: cb.x, y: pendingTop(y, mc, first)})
			s.placeFloats(p, 0, math.MaxInt, pendingTop(y, mc, first), mc, first)
			s.staticRest(p, y, mc, first)
			if p.forced || p.sizeless {
				top := y
				if first {
					top = e.pastMargin(y, mc.strut, mc.kept)
					mc.commitFloats(top)
				}
				l := &lineBox{x: cb.x, w: cb.w, h: strutH, baseline: s.rootV.above}
				if first1 != nil {
					// An empty first line takes the ::first-line's strut.
					v := s.newVbox(first1.root, nil)
					l.h, l.baseline = v.above+v.below, v.above
				}
				if emptyChanged(p, s.root) {
					l.changed = s.root
				}
				s.officeLine(l)
				top = s.position(top, l.h-l.slack, forced)
				s.cutSlack(l, top)
				l.y = top
				if first {
					mc.commit(top)
					first = false
				}
				y = top + l.h
				s.lines = append(s.lines, l)
				room, under = 0, 0
			}
			y = s.clearBreak(p, y)
			continue
		}
		w := e.lineWidths(p)
		n := p.p.Len()
		// lp is the paragraph the next line is broken from, lw its widths
		// and ln its length: the first line's own paragraph for the first
		// formatted line, then p.
		lp, lw, ln := p, w, n
		lineStrut := strutH
		if first1 != nil {
			lp = first1.para()
			lw, ln = e.lineWidths(lp), lp.p.Len()
			s.fl, s.d, s.rootV = first1, first1.d, s.newVbox(first1.root, nil)
			lineStrut = s.rootV.above + s.rootV.below
		}
		pos := 0
		paraFirst := true
		for pos < ln {
			if e.err != nil {
				return y
			}
			if len(s.lines) >= maxLinesPerBlock {
				e.warnOnce(warn.LimitReached, warn.Omitted, fmt.Sprintf("a block of more than %d lines was cut short", maxLinesPerBlock), y)
				b.lines = s.lines
				return y
			}
			if !paraFirst {
				for pos < ln && lp.collapsibleSpace(pos) {
					pos++
				}
				if pos >= ln {
					break
				}
				if e.clampDiscards() {
					return s.discard(d, p, y)
				}
			}
			top := y
			if e.foot != nil && e.paginating() {
				// The bodies waiting for a page take their place on this
				// line's page before it is laid out.
				e.inPages(top, top, func(t, _ float64) { e.flushNotes(e.pageOf(t+eps), t) })
			}
			if first {
				// The margins resolve here: the floats placed under them
				// take their place before the line fits beside them.
				top = e.pastMargin(y, mc.strut, mc.kept)
				mc.commitFloats(top)
			}
			if paraFirst {
				at := top
				if len(s.lines) == 0 && forced[0] && e.paginating() && !e.atPageTop(at) {
					// The first line starts the next page: so does the
					// paragraph the floats are positioned from.
					at = e.nextPage(at)
				}
				s.placeWordFloats(lp, at)
			}
			lettered, letterAt, letterBox := false, -1, (*box)(nil)
			if letter := initialLetterAt(lp, pos); letter != nil && len(s.lines) == 0 && !s.placed[letter] {
				// A paragraph that starts with an initial letter goes below
				// the initial letters before it (CSS Inline 3, 4.9.2).
				top = max(top, bc.clearInitial())
				for _, f := range lp.floats {
					// The floats before the letter in the source are placed
					// first, as their line would place them.
					if f.box == letter {
						break
					}
					if f.pos == pos && !s.placed[f.box] {
						s.placed[f.box] = true
						e.layoutFloat(f.box, cb, bc, top)
					}
				}
				s.placed[letter] = true
				letterAt = e.layoutInitialLetter(letter, letterBlock(b, cb, indent), bc, top)
				top += e.initialDrop(letter)
				lettered, letterBox = true, letter
			}
			s.placeFloats(lp, pos, pos, top, mc, false)
			lineIndent := 0.0
			if indents(b.st, len(s.lines) == 0 && !after, paraFirst && prevForced) && !lettered {
				lineIndent = indent
			}
			if first {
				// The top padding is room behind a top border too.
				room = top - y + b.pd[css.Top]
			}
			fitTop := top
			top, end, left, right, l := s.fitRubyLine(lp, lw, pos, lineIndent, top, lineStrut, room, under)
			floatsBefore := len(bc.floats)
			for range len(lp.floats) {
				if !s.placeFloatsBeside(lp, pos, end, l, top) {
					break
				}
				// A float placed beside the content before it narrows the
				// line, which is broken again beside it.
				top, end, left, right, l = s.fitRubyLine(lp, lw, pos, lineIndent, top, lineStrut, room, under)
			}
			if lettered {
				s.lowerLetter(letterBox, letterAt, s.rubyGrowth(l, room, under))
			}
			s.tail, s.moved = 0, false
			if end == ln && isLastPara(d, p) {
				s.tail = s.endEdges()
			}
			over := s.columnTrim(l)
			if newTop := s.position(top, l.h-l.slack, forced); newTop != top {
				top = newTop
				nl, nr := s.band(top, l.h)
				if len(bc.floats) == floatsBefore && (math.Abs(nl-left) > eps || math.Abs(nr-right) > eps) {
					// The page cut moved the line to where the floats
					// leave another width: break it again there.
					top, end, _, _, l = s.fitLine(lp, lw, pos, lineIndent, top, lineStrut)
					s.rubyRoom(l, room, under)
					top = s.position(top, l.h-l.slack, forced)
					nl, nr = s.band(top, l.h)
				}
				l.x, l.w = nl, nr-nl
			}
			l.withEnd = s.moved
			if over > 0 && e.atPageTop(top) {
				// The first formatted line of a column starts at the
				// column's top by its text edge.
				top -= over
				l.trimmedOver, l.colTrimmed = true, true
			}
			s.cutSlack(l, top)
			// A line fits the clamp with the annotations under it, as
			// Chromium clamps a line whose annotation would overflow.
			if e.clampOverflows(top, l.h+l.annUnder) {
				return s.discard(d, p, y)
			}
			l = s.overflowMark(lp, l, pos, end, lineIndent)
			last := end == ln && (p.forced || isLastPara(d, p))
			if e.clampKeep(&clampLine{s: s, p: lp, l: l, start: pos, end: end, indent: lineIndent}) {
				last = true
			}
			s.align(l, lineIndent, last)
			if first {
				// The margins resolve where the line would start: a line
				// CSS floats push down moves inside its block, not the
				// block (CSS 2.1, 9.5), unless it moves to another page. A
				// paragraph a Word drawing pushes down starts at its first
				// line, its bookmark with it, as Word places it
				// (hasOfficeExclusion).
				at := top
				if !bc.hasOfficeExclusion() && (!e.paginating() || e.pageOf(fitTop+eps) == e.pageOf(top+eps)) {
					at = fitTop
				}
				mc.commit(at)
				first = false
			}
			l.y = top
			s.settle(l)
			if !paraFirst && l.changeHead {
				// The line before one a change opens draws a bar too.
				s.lines[len(s.lines)-1].changed = s.root
			}
			s.lines = append(s.lines, l)
			room, under = l.endRoom, l.annUnder
			s.recordAnchors(l)
			s.recordEmptyAnchors(lp, pos, end, l)
			e.noteLine(lp, pos, end, l)
			s.staticLine(lp, pos, end, l)
			y = top + e.advance(l)
			if col := e.pageOf(top + l.baseline); s.under > 0 && e.pageOf(y-eps) > col {
				// The trimmed room under a column's last line hangs past
				// the column's end: the next line starts the next column.
				y = e.pageTop(col + 1)
				l.trimmedUnder = true
			}
			if s.seg && math.Abs(top-s.segTop) <= eps {
				y = max(y, s.segBot)
			}
			s.seg = false
			if end < ln && s.nextPiece(top, l.h, l.x+l.w) {
				// Word sets the text on both sides of a float of
				// css.FrameWrapBoth: the line goes on in the next piece.
				s.seg, s.segTop, s.segFrom, s.segBot = true, top, l.x+l.w, y
				y = top
			}
			s.placeFloats(lp, pos+1, end, y, mc, false)
			paraFirst = false
			pos = end
			if s.fl != nil {
				// The lines after the first go on in p from where the
				// first ends.
				pos = e.rest(s.fl, p, end)
				lp, lw, ln, lineStrut = p, w, n, strutH
				s.fl, s.d, s.rootV = nil, d, rootV
			}
		}
		s.placeFloats(p, n, math.MaxInt, pendingTop(y, mc, first), mc, first)
		s.staticRest(p, y, mc, first)
		y = s.clearBreak(p, y)
	}
	b.lines = s.lines
	e.balance.unfit = s.unfit
	if g := s.rubyEnd(under); g > 0 {
		s.lines[len(s.lines)-1].annEnd = g
		y += g
	}
	return y
}

// letterBlock returns the part of the containing block cb of the
// paragraph b, of text indent indent, an initial letter that starts its
// first line is placed in. CSS Inline 3, 4.7.2 moves the letter by the
// indent of its line, which then has no indent of its own; litho follows
// Chromium, which moves it by twice the indent.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 puts a 3-line
//     initial letter of a paragraph of text-indent 30px 60px in, and the
//     text of its first line and of the lines beside it at its end
//     edge; the reference of css/css-inline/initial-letter/
//     initial-letter-indentation-rtl draws the letter of a 10px indent
//     20px in.
func letterBlock(b *box, cb cblock, indent float64) cblock {
	if !indents(b.st, true, false) || indent == 0 {
		return cb
	}
	in := min(2*indent, cb.w)
	if b.st.Direction != css.RTL {
		cb.x += in
	}
	cb.w -= in
	return cb
}

// initialLetterAt returns the initial letter anchored at character pos
// of p, nil for none.
func initialLetterAt(p *ifcPara, pos int) *box {
	for _, f := range p.floats {
		if f.pos == pos && f.box.initial {
			return f.box
		}
	}
	return nil
}

// lineWidths returns the width every character of p takes on a line, a
// leader's with room for one copy of its string.
func (e *engine) lineWidths(p *ifcPara) []float64 {
	w := p.paraWidths(nil)
	for _, lp := range p.leaders {
		// A leader needs room for one copy of its string to share a line
		// with what precedes it, as WeasyPrint breaks leaders.
		if lp.pos < len(w) {
			w[lp.pos] += e.leaderOf(lp.box).w
		}
	}
	return w
}

// indents reports whether text-indent indents a line of the block of
// style st (CSS Text 3, 8.1): the block's first line, and with each-line
// the first line after each forced break too; hanging indents every other
// line instead.
func indents(st *css.Style, first, afterForced bool) bool {
	on := first || st.TextIndentEachLine && afterForced
	return on != st.TextIndentHanging
}

// afterBlock reports an anonymous block box b that an in-flow block of its
// parent precedes: its first line is not the parent's first formatted line,
// which alone text-indent indents (CSS Text 3, 8.1; CSS 2.1, 9.2.1.1: the
// anonymous box inherits text-indent but holds no first line of the
// element).
func afterBlock(b *box) bool {
	if !b.anon || b.parent == nil {
		return false
	}
	for _, k := range b.parent.kids {
		if k == b {
			return false
		}
		if k.float == css.FloatNone && !k.abs {
			return true
		}
	}
	return false
}

// pendingTop returns y with the margins pending in mc added when pending.
func pendingTop(y float64, mc *marginState, pending bool) float64 {
	if pending {
		return y + mc.strut.sum()
	}
	return y
}

// fitRubyLine fits the line of p starting at pos as fitLine does, and
// grows it for its ruby annotations and those under the line before it,
// room and under (rubyRoom). The content of a line so grown sits lower:
// where the floats leave another width there, the line is broken again
// in that band, as Chromium places a line its annotations move beside an
// initial letter.
//
// # Corpus evidence
//
//   - wpt: css-inline/initial-letter/initial-letter-block-position-drop-under-ruby-tall,
//     the line after a 20px line holding an 80px annotation under its
//     base starts under the letter, not beside it.
func (s *inlineState) fitRubyLine(p *ifcPara, w []float64, pos int, indent, top, h, room, under float64) (float64, int, float64, float64, *lineBox) {
	top, end, left, right, l := s.fitLine(p, w, pos, indent, top, h)
	g := s.rubyRoom(l, room, under)
	if g <= eps {
		return top, end, left, right, l
	}
	if nl, nr := s.band(top+g, l.h-g); math.Abs(nl-left) <= eps && math.Abs(nr-right) <= eps {
		return top, end, left, right, l
	}
	top, end, left, right, l = s.fitLine(p, w, pos, indent, top+g, h)
	return top - s.rubyRoom(l, room, under), end, left, right, l
}

// fitLine breaks the line of p starting at pos beside the floats, from
// top down, and returns where it lands, its end, the band it was broken in
// and the line built. The line is first fitted beside the floats over the
// strut's height h; when the line built turns out taller and a float
// narrows the band over its full height, it is broken again in that band
// (CSS 2.1, 9.5: line boxes beside a float are shortened to make room for
// its margin box).
func (s *inlineState) fitLine(p *ifcPara, w []float64, pos int, indent, top, h float64) (float64, int, float64, float64, *lineBox) {
	e, bc, cb := s.e, s.bc, s.cb
	var end int
	var left, right float64
	var l *lineBox
	var fits bool
	tries := len(bc.floats) + 1 + bc.shapeTries()
	if bc.hasBoth() {
		// Each float of css.FrameWrapBoth adds a piece to try.
		tries *= 2
	}
	for range maxFitPasses {
		for range tries {
			left, right = s.band(top, h)
			org := left - cb.x
			if s.root.st.Direction == css.RTL {
				org = cb.x + cb.w - right
			}
			avail := right - left
			if s.root == e.balance.root {
				avail -= e.balance.inset
			}
			end, fits = e.breakLine(p, w, pos, indent, avail, s.root, org)
			if fits && end < p.p.Len() && !p.allowed(end) && s.nextPiece(top, h, right) {
				// Word breaks no word in a piece of a line cut by a float
				// of css.FrameWrapBoth: the word goes to the next piece.
				fits = false
			}
			if fits || !bc.lineNarrowed(top, h, cb.x, cb.x+cb.w) {
				break
			}
			if s.nextPiece(top, h, right) {
				// The words do not fit in this piece of a line cut by a
				// float of css.FrameWrapBoth: they try the next.
				s.seg, s.segTop, s.segFrom = true, top, right
				if s.segBot < top {
					s.segBot = top
				}
				continue
			}
			if s.seg && math.Abs(top-s.segTop) <= eps && s.segBot > top+eps {
				s.seg = false
				top = s.segBot
				continue
			}
			s.seg = false
			next := bc.lineBelow(top, h)
			if math.IsInf(next, 1) {
				break
			}
			top = next
		}
		l = s.buildLine(p, pos, end, left, right)
		if l.h <= h+eps {
			break
		}
		if l2, r2 := s.band(top, l.h); l2 <= left+eps && r2 >= right-eps {
			break
		}
		h = l.h
	}
	if bc.hasArea() {
		// Beside a float area the line takes the room the area leaves
		// over its own height, which a band of the strut's height or of
		// a longer try may not: the area narrows each band its own way
		// (CSS Shapes 1, 2), and Chromium places the line in its own.
		if l2, r2 := s.band(top, l.h); l2 < left-eps || r2 > right+eps {
			left, right = l2, r2
			l = s.buildLine(p, pos, end, left, right)
		}
	}
	if !fits {
		s.unfit++
	}
	return top, end, left, right, l
}

// clearBreak returns where the line after the paragraph p starts when the
// paragraph ended at y: below the floats the clear value of its line break
// names (CSS 2.1, 9.5.2, which HTML applies to br).
func (s *inlineState) clearBreak(p *ifcPara, y float64) float64 {
	if !p.forced || p.clear == css.ClearNone {
		return y
	}
	return max(y, s.bc.clearTo(p.clear))
}

// advance returns how far the line l, placed, moves the flow down: its
// height, or the page area height for a line taller than its page for its
// text alone, which is cut at the page bottom. It sets l.overflows.
func (e *engine) advance(l *lineBox) float64 {
	l.overflows = false
	if !e.paginating() {
		return l.h
	}
	area := e.areaH(e.pageOf(l.y + eps))
	if l.h <= area+eps {
		return l.h
	}
	for _, it := range l.items {
		if it.atomic != nil && it.atomic.marginH() > area+eps {
			l.overflows = true
			e.overflowColumn(l.y, l.h)
			return l.h
		}
	}
	return area
}

// top returns where the line l starts in its page or column: its top, or
// its baseline for a line a column's text-box-trim moved up past the
// column's top ([inlineState.columnTrim]).
func (l *lineBox) top() float64 {
	if l.colTrimmed {
		return l.y + l.baseline
	}
	return l.y
}

// isLastPara reports whether p is the last paragraph of d.
func isLastPara(d *ifcData, p *ifcPara) bool {
	return len(d.paras) > 0 && d.paras[len(d.paras)-1] == p
}

// bandOf returns the left edge and width of the line box space at y, for a
// line of height h, in cb beside the floats of bc.
func bandOf(bc *bfc, y, h float64, cb cblock) (float64, float64) {
	l, r := bc.lineBand(y, h, cb.x, cb.x+cb.w)
	return l, r - l
}

// position returns where the line of height h whose top would be at top
// lands: on the next page when it straddles a cut or when its index is
// forced.
func (s *inlineState) position(top, h float64, forced map[int]bool) float64 {
	e := s.e
	if !e.paginating() {
		return top
	}
	if forced[len(s.lines)] && !e.atPageTop(top) {
		top = e.nextPage(top)
	}
	if s.root.st.BoxDecorationBreak == css.DecorationCloneAll {
		return s.clonedPosition(top, h)
	}
	y := e.fit(top, max(h-s.under, 0))
	if t := s.tail; t > 0 && y == top && !e.atPageTop(y) && e.pageOf(y) != e.pageOf(y+h+t-metricSlack) && h+t <= e.areaH(e.pageOf(y)+1)+eps {
		y = e.fit(e.nextPage(y), h)
		s.moved = true
	}
	return y
}

// columnTrim sets s.under for the line l of a multi-column container
// whose text-box-trim trims the lines of each column, and returns how far
// a line at the top of a column moves up: the first formatted line of a
// column starts at its text-box-edge over metric and the last one ends at
// its under metric (CSS Inline 3, 6.2: the property applies to each
// column). A line of the container's own, or of an anonymous block of its
// own text, that ends past the column's end
// by no more than its trimmed room ends that column; Chromium trims the
// end of no column whose last line lies in a child block.
//
// # Corpus evidence
//
//   - wpt, css/css-inline/text-box-trim/text-box-trim-multicol-001.html:
//     Chromium sets four 80px lines of 40px Ahem in a 290px column of a
//     container of trim-both, the first starting at the column's top by
//     its glyphs, the last ending 10px from its end.
//   - wpt, css/css-inline/text-box-trim/text-box-trim-multicol-005.html:
//     with the lines in a child block, Chromium starts the second column
//     at the glyphs of its first line and sets three lines in the first
//     column, the fourth not fitting untrimmed.
func (s *inlineState) columnTrim(l *lineBox) float64 {
	s.under = 0
	c := s.e.cols
	if c == nil || c.trim == css.TrimNone || !s.e.paginating() {
		return 0
	}
	overEdge, underEdge := s.e.trimEdges(s.root.st, s.root.lang)
	if (c.trim == css.TrimEnd || c.trim == css.TrimBoth) && (s.root == c.owner || s.root.anon && s.root.parent == c.owner) {
		s.under = max(l.h-l.baseline-underEdge, 0)
	}
	if c.trim == css.TrimStart || c.trim == css.TrimBoth {
		return max(l.baseline-overEdge, 0)
	}
	return 0
}

// endEdges returns the block-end padding and border of the root that its
// last line keeps with it, under BrowserRules: a page or column cut does
// not run through them, the line moves to the next fragmentainer with
// them when they fit there together, as Chromium breaks a block. The
// edges of a multi-column container lie outside its columns. CSS
// Fragmentation 3, 4.1 makes no break point inside a box's padding or
// border; it leaves the break between the last line and the end edge
// (class C) allowed, and Chromium avoids it.
//
// # Corpus evidence
//
//   - wpt, css/css-break/break-at-end-container-edge-004.html: Chromium
//     moves the last 40px line of a block of padding-bottom 60px into the
//     last 100px column with the padding, rather than leaving the padding
//     to start that column.
//   - probe, html: Chromium's PDF route moves the last of two and of three
//     40px lines of a block of padding-bottom 60px to the next page with
//     the padding under the default orphans and widows of 2, the lines
//     before it staying.
func (s *inlineState) endEdges() float64 {
	b := s.root
	if !s.e.opts.BrowserRules || b.kind != kindBlock || isMulticol(b) || b.st.BoxDecorationBreak != css.DecorationSlice {
		return 0
	}
	if b.st.Height.Kind != css.Auto {
		return 0
	}
	return b.pd[css.Bottom] + b.bd[css.Bottom]
}

// cutSlack ends the line l, placed at top, at the page cut its slack
// hangs past, as Writer lets the room a proportional line spacing adds
// below the last line of a page fall into the page's margin.
func (s *inlineState) cutSlack(l *lineBox, top float64) {
	e := s.e
	if l.slack <= 0 || !e.paginating() || s.root.st.BoxDecorationBreak == css.DecorationCloneAll {
		return
	}
	cut := e.nextPage(top)
	if top+l.h > cut+eps && top+l.h-l.slack <= cut+eps && cut > top+eps {
		l.h = cut - top
	}
}

// placeFloats lays out the floats of p anchored at a character position in
// [from, to] at or below top, recording them in mc when margins are
// pending.
func (s *inlineState) placeFloats(p *ifcPara, from, to int, top float64, mc *marginState, pending bool) {
	for _, f := range p.floats {
		if f.pos < from || f.pos > to || s.placed[f.box] {
			continue
		}
		s.placed[f.box] = true
		if pending {
			mc.alignFloats(top)
		}
		i := s.e.layoutFloat(f.box, s.cb, s.bc, top)
		if pending {
			mc.floatPlaced(f.box, s.bc, i, top)
		}
	}
}

// placeFloatsBeside places the floats anchored inside the line (start,
// end], the break after it included, at the line's top when they fit
// beside its content, moving the line beside them (CSS 2.1, 9.5.1, rule
// 8): a float met at the end of a line's content still belongs to that
// line. The others wait for the line's bottom.
//
// Under [Options.BrowserRules] a float that fits beside the content
// before it, but not beside the whole line, is placed at the line's top
// too, and placeFloatsBeside reports true: the line is then broken again
// beside it. CSS 2.1, 9.5.1 lets a float sit on the current line; Chromium
// places it there whenever the line's content so far leaves it room.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 puts a 100px left
//     float met after "In this paragraph " in a paragraph that fills its
//     line at the top of the first line, the line's text beside it; litho
//     put it under the first line.
//   - wpt: css/css-grid/grid-model/grid-empty-floating-positioned-and-inline-001
//     floats two empty grids from the middle of a full line.
func (s *inlineState) placeFloatsBeside(p *ifcPara, start, end int, l *lineBox, top float64) bool {
	refit := false
	for _, f := range p.floats {
		// A float at the paragraph's end belongs to its last line too.
		if f.pos <= start || f.pos > end || f.pos == end && end < p.p.Len() || s.placed[f.box] {
			continue
		}
		resolveEdges(f.box, s.cb.w)
		fw := f.box.m[css.Left] + f.box.m[css.Right] + hEdges(f.box)
		if f.box.kind == kindReplaced {
			w, _ := s.e.replacedSize(f.box, s.cb)
			fw += w
		} else if w, ok := specWidth(f.box, f.box.st.Width, s.cb.w); ok {
			fw += w
		} else {
			fw += s.e.shrinkToFit(f.box, s.cb.w)
		}
		if contentWidth(l)+fw > l.w+eps {
			if !s.e.opts.BrowserRules || contentWidthBefore(l, f.pos)+fw > l.w+eps {
				continue
			}
			refit = true
		}
		s.e.layoutFloat(f.box, s.cb, s.bc, top)
		s.placed[f.box] = true
		l.x, l.w = bandOf(s.bc, top, l.h, s.cb)
	}
	return refit
}

// contentWidthBefore returns the width the items of l before the
// character position pos span.
func contentWidthBefore(l *lineBox, pos int) float64 {
	w := 0.0
	for _, it := range l.items {
		if it.start < pos {
			w = max(w, it.x+it.w)
		}
	}
	return w
}

// contentWidth returns the width the items of l span.
func contentWidth(l *lineBox) float64 {
	w := 0.0
	for _, it := range l.items {
		w = max(w, it.x+it.w)
	}
	return w
}

// newVbox returns the vertical alignment record of the inline box b, or
// of the root inline box when parent is nil.
func (s *inlineState) newVbox(b *box, parent *vbox) *vbox {
	e := s.e
	m := e.faceMetrics(b.st, b.lang)
	lh := e.lineHeight(b.st, b.lang)
	up, down := e.leading(lh - (m.Ascent + m.Descent))
	v := &vbox{b: b, parent: parent, above: m.Ascent + up, below: m.Descent + down, ca: m.Ascent, cd: m.Descent, size: e.fontSize(b.st), lh: lh, xh: m.XHeight}
	if parent == nil {
		return v
	}
	v.inAligned = parent.inAligned || parent.aligned
	v.shift = e.alignShift(b.st.VerticalAlign, parent, v.above, v.below, lh)
	if k := b.st.VerticalAlign.Kind; k == css.AlignTop || k == css.AlignBottom {
		v.aligned = true
		v.shift = parent.shift
	}
	return v
}

// leading splits the leading lead of an inline box, its line height less
// its content area, into the half-leading above its content and the one
// below (CSS 2.1, 10.8.1: half each). With pixel font metrics the leading
// is a whole number of Blink's layout units of 1/64 px, and the part
// above is half of it, truncated to a unit, then floored to a whole CSS
// pixel, the part below the rest, as Blink splits it
// (CalculateLeadingSpace).
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 starts the
//     background of a 16 px Liberation Serif span at the top of its line
//     at line-height: normal (a leading of 1 px), 1 px below it at
//     line-height: 20px (3 px), 2 px below it at 21px (4 px); a 20 px
//     DejaVu Sans line (1 px) starts it at the top.
//   - probe, html: a 15.33 px Liberation Sans line at line-height 1.5
//     (a leading of 383/64 px) puts 2 px above its content, so a span
//     lowered by 4 px under it ends the line 25 px tall.
func (e *engine) leading(lead float64) (float64, float64) {
	if !e.opts.PixelFontMetrics {
		return lead / 2, lead / 2
	}
	half := math.Trunc(math.Round(lead/css.Pt*64) / 2)
	up := float64(math.Floor(half/64) * css.Pt)
	return up, lead - up
}

// alignShift returns the baseline shift of a box with extent above and
// below its baseline, aligned in parent by va (CSS 2.1, 10.8.1). lh is the
// box's line height, for percentages.
func (e *engine) alignShift(va css.VerticalAlign, parent *vbox, above, below, lh float64) float64 {
	ps := parent.shift
	switch va.Kind {
	case css.Sub:
		if e.opts.BrowserRules {
			return ps + blinkScriptShift(parent.size, 5)
		}
		return ps + float64(parent.size*0.2)
	case css.Super:
		if e.opts.BrowserRules {
			return ps - blinkScriptShift(parent.size, 3)
		}
		return ps - parent.size/3
	case css.TextTop:
		return ps - parent.ca + above
	case css.TextBottom:
		return ps + parent.cd - below
	case css.Middle:
		return ps - float64(parent.xh/2) + float64((above-below)/2)
	case css.AlignLength:
		return ps - resolve(va.Length, lh)
	case css.Baseline, css.AlignTop, css.AlignBottom:
	}
	return ps
}

// blinkScriptShift returns how far Blink lowers a baseline under
// vertical-align: sub (part 5) or raises it under super (part 3) in a
// parent of font size size: that part of the size floored to Blink's
// layout unit of 1/64 px, plus one pixel. CSS 2.1, 10.8.1 leaves "the
// proper position" of both to the user agent; outside BrowserRules the
// shift is the part of the size alone. Each product is rounded on its own,
// so every machine computes the same shift.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 lowers a 16 px
//     sub of the parent's size by 4.1875 px, a 17 px one by 4.390625 px
//     and a 20 px one by 5 px, raises a 16 px super by 6.328125 px and a
//     17 px one by 6.65625 px; a smaller sub under a 16 px parent moves as
//     far as one of the parent's size.
func blinkScriptShift(size, part float64) float64 {
	return float64((float64(math.Floor(size/css.Pt/part*64)/64) + 1) * css.Pt)
}

// vboxOf returns the vertical alignment record of inline box b, creating
// the records of its ancestors up to the root.
func (s *inlineState) vboxOf(b *box) *vbox {
	if b == nil || b == s.root {
		return s.rootV
	}
	if v, ok := s.vboxes[b]; ok {
		return v
	}
	v := s.newVbox(b, s.vboxOf(b.parent))
	s.vboxes[b] = v
	return v
}

// buildLine builds the line box of characters [start, end) of p in the
// band from left to right, its items from the line start before
// alignment.
func (s *inlineState) buildLine(p *ifcPara, start, end int, left, right float64) *lineBox {
	e := s.e
	s.vboxes = map[*box]*vbox{}
	// The spaces at the line end hang (CSS Text 3, 4.1.3): they are drawn
	// but neither fit nor align. The collapsible ones among them are
	// removed instead (4.1.2) and draw nothing.
	hang := !s.keep && end > start && !keepsSpaces(p.wsAt(end-1))
	hangFrom := end
	// A ruby base or annotation is an inline box of its ruby, which litho
	// lays out on a line of its own: other spaces at its end stay in its
	// content, as Chromium lays them out, and only the collapsible ones go
	// (CSS Ruby 1, 2.5.1).
	for hang && hangFrom > start && text.IsHangingSpace(p.p.Rune(hangFrom-1)) && (!s.root.rubyLine || p.collapsibleSpace(hangFrom-1)) {
		hangFrom--
	}
	trim := end
	for trim > hangFrom && p.collapsibleSpace(trim-1) {
		trim--
	}
	pl := p.p.LineTrimmed(start, end, trim)
	l := &lineBox{x: left, w: right - left, hangLeft: p.p.Direction() == text.RightToLeft}
	for i := hangFrom; i < trim; i++ {
		l.hangW += p.p.Advance(i)
	}
	if hangFrom == end && end > start && p.hangsPunct(s.root, end-1) {
		// The punctuation that ends a wrapped line hangs when the line
		// holds it only past its end.
		w := 0.0
		for i := start; i < end; i++ {
			w += p.p.Advance(i)
		}
		if w > l.w+eps {
			l.hangW += p.p.Advance(end - 1)
		}
	}
	// A box has one fragment on the line, or one a visual run of its spans
	// on a reordered line.
	var vis *visualEdges
	if reorderedLine(pl.Spans) {
		vis = s.newVisualEdges(p, pl.Spans, start, end)
	}
	type fragKey struct {
		b   *box
		run int
	}
	type extent struct{ x0, x1 float64 }
	ext := map[fragKey]*extent{}
	cur := 0
	widen := func(b *box, x0, x1 float64) {
		k := fragKey{b: b}
		if vis != nil {
			k.run = vis.run(b, cur)
		}
		if g, ok := ext[k]; ok {
			g.x0, g.x1 = min(g.x0, x0), max(g.x1, x1)
			return
		}
		ext[k] = &extent{x0, x1}
	}
	s.glyphs = 0
	x := 0.0
	opened, closed := map[*box]bool{}, map[*box]bool{}
	for _, o := range p.openAt[start] {
		// A box whose start the line before holds has its start edge
		// there.
		opened[o] = opened[o] || p.trailsAt(o, start)
	}
	// A line broken at a soft hyphen ends with the hyphenate-character,
	// drawn after the soft hyphen in its inline box (CSS Text 3, 5.4).
	var hyMark *hyphenMark
	if end > start && p.hyphenates(end) {
		hyMark = e.hyphen(p.infos[p.p.RunOf(end-1)].box)
	}
	addHyphen := func(b *box) {
		for _, hs := range hyMark.spans {
			if hs.Text == nil {
				continue
			}
			t := *hs.Text
			t.Glyphs = slices.Clone(t.Glyphs)
			ascent, descent, gap := hs.Ascent, hs.Descent, hs.LineGap
			if e.opts.PixelFontMetrics {
				ascent, descent, gap = pixelMetrics(ascent, descent, gap)
			}
			it := &lineItem{
				box: b, text: &t, x: x + hs.X, w: hs.Width, ascent: ascent, descent: descent, gap: gap,
				ulPos: hs.UnderlinePosition, ulThick: hs.UnderlineThickness, stPos: hs.StrikeoutPosition, stThick: hs.StrikeoutThickness,
				start: end, end: end, rtl: hs.RTL,
			}
			l.items = append(l.items, it)
			for a := s.parentInline(b); a != nil; a = s.parentInline(a) {
				widen(a, it.x, it.x+it.w)
			}
		}
		x += hyMark.width
	}
	for i, sp := range pl.Spans {
		info := p.infos[sp.Run]
		cur = i
		// A left-to-right box draws its start edge on the left of its
		// content and its end edge on the right; a right-to-left box the
		// other way round, whatever the direction of the text inside.
		if vis != nil {
			s.edges(p, vis, i, true, start, end, &x, widen)
		} else {
			s.closeEdges(p, sp.End, true, closed, &x, widen)
			s.openEdges(p, sp.Start, true, opened, &x, widen)
		}
		ascent, descent, gap := sp.Ascent, sp.Descent, sp.LineGap
		if s.e.opts.PixelFontMetrics {
			ascent, descent, gap = pixelMetrics(ascent, descent, gap)
		}
		holds := hyMark != nil && sp.Start < end && end <= sp.End
		if holds && sp.RTL {
			// A right-to-left run ends on its left.
			addHyphen(info.box)
		}
		it := &lineItem{
			box: info.box, x: x, w: sp.Width, ascent: ascent, descent: descent, gap: gap,
			ulPos: sp.UnderlinePosition, ulThick: sp.UnderlineThickness, stPos: sp.StrikeoutPosition, stThick: sp.StrikeoutThickness,
			start: sp.Start, end: sp.End, rtl: sp.RTL, hang: max(sp.End-max(sp.Start, hangFrom), 0),
		}
		switch info.kind {
		case runAtomic:
			it.atomic = info.box
		case runText:
			if sp.Text != nil && s.glyphs+len(sp.Text.Glyphs) <= maxGlyphsPerLine {
				it.text = sp.Text
				s.glyphs += len(sp.Text.Glyphs)
			} else if sp.Text != nil {
				e.warnOnce(warn.LimitReached, warn.Omitted, fmt.Sprintf("a line of more than %d glyphs was cut short", maxGlyphsPerLine), -1)
			}
		case runTab:
			it.tab = true
			if officeTabs(s.root.st) {
				it.text = e.tabLeader(p, it)
			}
		case runEmpty:
		}
		x += sp.Width
		l.items = append(l.items, it)
		if holds && !sp.RTL {
			addHyphen(info.box)
		}
		for a := s.parentInline(info.box); a != nil; a = s.parentInline(a) {
			widen(a, it.x, it.x+it.w)
		}
		if info.kind == runEmpty {
			widen(info.box, it.x, it.x)
		}
		if vis != nil {
			s.edges(p, vis, i, false, start, end, &x, widen)
		} else {
			s.openEdges(p, sp.Start, false, opened, &x, widen)
			s.closeEdges(p, sp.End, false, closed, &x, widen)
		}
	}
	if vis == nil && end > start && !l.hangLeft {
		for _, o := range p.openAt[end] {
			if p.trailsAt(o, end) && o.st.Direction != css.RTL {
				// Its start takes no room: the fragment is empty.
				opened[o] = true
				widen(o, x, x)
				s.vboxOf(o)
			}
		}
	}
	if officeTabs(s.root.st) {
		l.bars = barRules(s.root, s.cb, left)
		l.barColor, _ = colorOf(s.root.st.Color)
		l.barOwner = s.root
	}
	for k := range ext {
		// Any box on the line will do: the loop only finds whether one is
		// changed.
		if k.b.node != nil && k.b.node.ChangeBar {
			l.changed = s.root
		}
	}
	for i, it := range l.items {
		if changeMarked(it.box, s.root) {
			l.changed = s.root
			l.changeHead = l.changeHead || i == 0
		}
	}
	l.frags = nil
	present := make([]fragKey, 0, len(ext))
	for k := range ext {
		if k.b.kind == kindInline {
			present = append(present, k)
		}
	}
	slices.SortFunc(present, func(a, b fragKey) int {
		if d := s.d.order[a.b] - s.d.order[b.b]; d != 0 {
			return d
		}
		return a.run - b.run
	})
	for _, k := range present {
		g := ext[k]
		f := &inlineFrag{box: k.b, x0: g.x0, x1: g.x1}
		f.left, f.right = edgeSides(k.b, p.opensIn(k.b, start, end), p.closesIn(k.b, start, end))
		if vis != nil {
			// The edges go to the outer fragments (CSS Writing Modes 3,
			// 2.4.5).
			f.left = f.left && k.run == 0
			f.right = f.right && k.run == vis.lastRun(k.b)
		}
		l.frags = append(l.frags, f)
	}
	s.emptyStruts(p, start, end)
	s.vertical(l)
	return l
}

// alignedExtent returns the top and bottom of the aligned subtree of the
// top or bottom aligned box v (CSS 2.1, 10.8.1: "the top of the aligned
// subtree is aligned with the top of the line box"): v's own box and those
// of the inline boxes inside it, with the extents of the atomic boxes
// inside it, lo and hi of each, in line coordinates.
func (s *inlineState) alignedExtent(v *vbox, atomics [][2]float64) (float64, float64) {
	lo, hi := v.shift-v.above, v.shift+v.below
	for _, w := range s.vboxes {
		if w == v || w.aligned || !insideVbox(w, v) {
			continue
		}
		lo, hi = min(lo, w.shift-w.above), max(hi, w.shift+w.below)
	}
	for _, a := range atomics {
		lo, hi = min(lo, a[0]), max(hi, a[1])
	}
	return lo, hi
}

// insideVbox reports whether the vertical box w lies inside v.
func insideVbox(w, v *vbox) bool {
	for a := w.parent; a != nil; a = a.parent {
		if a == v {
			return true
		}
	}
	return false
}

// emptyStruts gives a vertical box to every inline box that opens on the
// line of characters [start, end) of p, an empty one included: an empty
// inline box still has its line-height and font, which grow the line it
// sits on (CSS 2.1, 10.8: "Empty inline elements generate empty inline
// boxes, but these boxes still have margins, padding, borders and a line
// height"). A line of such boxes alone is no line (9.4.2): this runs only
// for a line that is laid out.
//
// # Corpus evidence
//
//   - wpt: css/CSS2/linebox/empty-inline-003 (an empty span of
//     line-height 5 beside an X of line-height 1) draws its reference in
//     Chromium 156.
func (s *inlineState) emptyStruts(p *ifcPara, start, end int) {
	last := end
	if end < p.p.Len() {
		last = end - 1
	}
	// The collector appends p.opens in position order.
	i, _ := slices.BinarySearchFunc(p.opens, start, func(o posBox, pos int) int { return o.pos - pos })
	for ; i < len(p.opens) && p.opens[i].pos <= last; i++ {
		s.vboxOf(p.opens[i].box)
	}
}

// parentInline returns the inline box b sits in, nil at the root.
func (s *inlineState) parentInline(b *box) *box {
	if s.fl != nil {
		if p, ok := s.fl.parent[b]; ok {
			return p
		}
	}
	if b == nil || b.parent == nil || b.parent == s.root || b.parent.kind != kindInline {
		return nil
	}
	return b.parent
}

// opensIn reports whether inline box b opens in the characters [start,
// end) of p.
func (p *ifcPara) opensIn(b *box, start, end int) bool {
	pos, ok := p.openPos[b]
	switch {
	case !ok:
		return false
	case pos == start && p.trailsAt(b, pos):
		return false
	case pos == end && end > start && p.trailsAt(b, pos):
		return true
	}
	return pos >= start && (pos < end || pos == end && end == p.n)
}

// closesIn reports whether inline box b closes in the characters [start,
// end) of p.
func (p *ifcPara) closesIn(b *box, start, end int) bool {
	pos, ok := p.closePos[b]
	return ok && (pos > start && pos <= end || pos == start && start == 0)
}

// openEdges advances x past the start edges of the inline boxes opening at
// pos that sit on the left of their content when left, a left-to-right
// box's, else on its right, a right-to-left box's.
func (s *inlineState) openEdges(p *ifcPara, pos int, left bool, opened map[*box]bool, x *float64, widen func(*box, float64, float64)) {
	// openAt lists the boxes outermost first: on the right of the content
	// the innermost comes first.
	list := p.openAt[pos]
	if !left {
		list = reversed(list)
	}
	for _, o := range list {
		if opened[o] || (o.st.Direction != css.RTL) != left {
			continue
		}
		opened[o] = true
		if o.first {
			s.edge(o, left, x, widen)
		}
	}
}

// closeEdges advances x past the end edges of the inline boxes closing at
// pos that sit on the left of their content when left, a right-to-left
// box's, else on its right, a left-to-right box's.
func (s *inlineState) closeEdges(p *ifcPara, pos int, left bool, closed map[*box]bool, x *float64, widen func(*box, float64, float64)) {
	// closeAt lists the boxes innermost first: on the left of the content
	// the outermost comes first.
	list := p.closeAt[pos]
	if left {
		list = reversed(list)
	}
	for _, c := range list {
		if closed[c] || (c.st.Direction == css.RTL) != left {
			continue
		}
		closed[c] = true
		if c.last {
			s.edge(c, left, x, widen)
		}
	}
}

// edge advances x past the left edge of b when left, its margin outside its
// border box, else past its right edge, recording the border box's side.
func (s *inlineState) edge(b *box, left bool, x *float64, widen func(*box, float64, float64)) {
	if left {
		*x += b.m[css.Left]
		widen(b, *x, *x)
		*x += b.bd[css.Left] + b.pd[css.Left]
		return
	}
	*x += b.pd[css.Right] + b.bd[css.Right]
	widen(b, *x, *x)
	*x += b.m[css.Right]
}

// vertical computes the line box height and the baseline of every item
// (CSS 2.1, 10.8).
func (s *inlineState) vertical(l *lineBox) {
	root := s.rootV
	top, bottom := -root.above, root.below
	include := func(v *vbox, t, b float64) {
		if v != nil && (v.inAligned || v.aligned) {
			return
		}
		top, bottom = min(top, t), max(bottom, b)
	}
	type atomicV struct {
		it           *lineItem
		v            *vbox
		above, below float64
		aligned      bool
	}
	var atomics []atomicV
	annTop, annBottom := math.Inf(1), math.Inf(-1)
	for _, it := range l.items {
		parent := s.vboxOf(s.parentInline(it.box))
		if it.atomic != nil {
			a := it.atomic
			above := a.marginH()
			if a.hasBaseline {
				above = a.m[css.Top] + a.baseline
			} else if c, ok := s.e.centralBaseline(parent.b); ok {
				// The central baseline of a box without one is the middle
				// of its margin box (CSS Inline 3, 4.2.1).
				above = float64(a.marginH()/2) + c
			}
			below := a.marginH() - above
			if r := a.ruby; r != nil && (r.over || r.under) && a.st.VerticalAlign.Kind != css.AlignTop && a.st.VerticalAlign.Kind != css.AlignBottom {
				// The annotations over and under a column take the room
				// above and under the line before they grow it (rubyRoom).
				shift := s.e.alignShift(a.st.VerticalAlign, parent, above, below, s.e.lineHeight(a.st, a.lang))
				it.baseline = shift
				it.ascent, it.descent = above, below
				lineAbove, lineBelow := above, below
				if r.over {
					annTop = min(annTop, shift-above)
					lineAbove = r.baseAbove
				}
				if r.under {
					annBottom = max(annBottom, shift+below)
					lineBelow = r.baseBelow
				}
				include(parent, shift-lineAbove, shift+lineBelow)
				atomics = append(atomics, atomicV{it: it, v: parent, above: above, below: below})
				continue
			}
			av := atomicV{it: it, v: parent, above: above, below: below}
			shift := s.e.alignShift(a.st.VerticalAlign, parent, above, below, s.e.lineHeight(a.st, a.lang))
			if k := a.st.VerticalAlign.Kind; k == css.AlignTop || k == css.AlignBottom {
				av.aligned = true
				shift = parent.shift
			}
			it.baseline = shift
			it.ascent, it.descent = above, below
			if !av.aligned {
				include(parent, shift-above, shift+below)
			}
			atomics = append(atomics, av)
			continue
		}
		var v *vbox
		if it.box.kind == kindInline {
			v = s.vboxOf(it.box)
		} else {
			v = parent
		}
		it.baseline = v.shift
		if ext, over, ok := s.emphasisExtent(it); ok && !v.inAligned && !v.aligned {
			// Emphasis marks over the text take the room above the line
			// as a ruby annotation does (rubyRoom); marks under it grow
			// the line.
			if over {
				annTop = min(annTop, v.shift-ext)
			} else {
				include(v, v.shift, v.shift+ext)
			}
		}
		// An empty inline box kept for its edges has no glyphs to lay out:
		// its own box, included below, is all it adds (CSS 2.1, 10.8.1).
		empty := it.text == nil && it.ascent == 0 && it.descent == 0
		if v.b.st.LineHeight.Kind == css.Normal && it.text != nil {
			// line-height: normal lays a glyph run out at the normal line
			// height of the face that draws it, a fallback face included,
			// as CSS Inline 3, 5.1 lets the user agent do and browsers do:
			// text a fallback face draws taller grows the line.
			lh := it.ascent + it.descent + max(it.gap, 0)
			up, down := s.e.leading(lh - (it.ascent + it.descent))
			include(v, v.shift-wider(it.ascent+up, v.above), v.shift+wider(it.descent+down, v.below))
		} else if it.text != nil || it.box.kind == kindInline && !empty {
			// Any other line-height lays the box out on its first
			// available font's metrics alone, whichever face draws its
			// glyphs (CSS 2.1, 10.8.1), as Chromium's ComputedLineHeight
			// and the inline box's text metrics do: a fallback face of
			// other proportions does not grow the line.
			include(v, v.shift-v.above, v.shift+v.below)
		}
	}
	for _, v := range s.vboxes {
		include(v, v.shift-v.above, v.shift+v.below)
	}
	// Top and bottom aligned boxes settle against the line box (CSS 2.1,
	// 10.8.1), growing it when taller.
	aligned := make([]*box, 0, len(s.vboxes))
	for k, v := range s.vboxes {
		if v.aligned && !v.inAligned {
			aligned = append(aligned, k)
		}
	}
	slices.SortFunc(aligned, func(a, b *box) int { return s.d.order[a] - s.d.order[b] })
	for _, k := range aligned {
		v := s.vboxes[k]
		var inside [][2]float64
		for _, av := range atomics {
			if !av.aligned && (av.v == v || insideVbox(av.v, v)) {
				inside = append(inside, [2]float64{av.it.baseline - av.above, av.it.baseline + av.below})
			}
		}
		lo, hi := s.alignedExtent(v, inside)
		h := hi - lo
		if h > bottom-top {
			if k.st.VerticalAlign.Kind == css.AlignTop {
				bottom = top + h
			} else {
				top = bottom - h
			}
		}
		old := v.shift
		if k.st.VerticalAlign.Kind == css.AlignTop {
			v.shift = top + (old - lo)
		} else {
			v.shift = bottom - (hi - old)
		}
		for _, w := range s.vboxes {
			for a := w.parent; a != nil; a = a.parent {
				if a == v {
					w.shift += v.shift - old
					break
				}
			}
		}
	}
	for _, av := range atomics {
		if !av.aligned {
			continue
		}
		h := av.above + av.below
		if h > bottom-top {
			if av.it.atomic.st.VerticalAlign.Kind == css.AlignTop {
				bottom = top + h
			} else {
				top = bottom - h
			}
		}
		if av.it.atomic.st.VerticalAlign.Kind == css.AlignTop {
			av.it.baseline = top + av.above
		} else {
			av.it.baseline = bottom - av.below
		}
	}
	for _, it := range l.items {
		if it.atomic == nil {
			v := s.vboxOf(s.parentInline(it.box))
			if it.box.kind == kindInline {
				v = s.vboxOf(it.box)
			}
			it.baseline = v.shift
		}
	}
	if above, ok := s.markerRoom(); ok {
		top = min(top, -above)
	}
	l.h = bottom - top
	l.baseline = -top
	l.annOver = max(top-annTop, 0)
	l.annUnder = max(annBottom-bottom, 0)
	contentTop, content := annTop-top, annBottom-top
	for _, it := range l.items {
		it.baseline += l.baseline
		above, below := it.ascent, it.descent
		if a := it.atomic; a != nil && a.ruby != nil {
			// A ruby column's content is its base's text: the room above
			// and under it is room the annotations beside it may take.
			above, below = a.ruby.textAbove, a.ruby.textBelow
		}
		contentTop = min(contentTop, it.baseline-above)
		content = max(content, it.baseline+below)
	}
	l.startRoom, l.endRoom = 0, 0
	if !math.IsInf(contentTop, 1) {
		l.startRoom = max(contentTop, 0)
	}
	if !math.IsInf(content, -1) {
		l.endRoom = max(l.h-content, 0)
	}
	for _, f := range l.frags {
		v := s.vboxOf(f.box)
		b := v.shift + l.baseline
		f.base = b
		f.top = b - v.ca - f.box.pd[css.Top] - f.box.bd[css.Top]
		f.bottom = b + v.cd + f.box.pd[css.Bottom] + f.box.bd[css.Bottom]
	}
	s.officeLine(l)
}

// wider returns a when it exceeds b by more than the rounding of shaped
// metrics, b otherwise: a face's shaped extent agrees with its indexed
// metrics to a few thousandths of a point.
func wider(a, b float64) float64 {
	if a > b+metricSlack {
		return a
	}
	return b
}

// metricSlack is the difference between two vertical metrics below which
// they count as equal.
const metricSlack = 0.01

// align applies text-align and text-indent to the items of l (CSS Text 3,
// 6.1 and 8.1), justifying at the word separators unless last.
func (s *inlineState) align(l *lineBox, indent float64, last bool) {
	s.fillLeaders(l, indent)
	st := s.root.st
	ltr := st.Direction != css.RTL
	if st.UnicodeBidi == css.Plaintext {
		// A line of unicode-bidi: plaintext takes its start and end from
		// its paragraph's direction, the one its text gives (CSS Text 3,
		// 8.3): hangLeft is that direction.
		ltr = !l.hangLeft
	}
	width := 0.0
	for _, it := range l.items {
		width = max(width, it.x+it.w)
	}
	for _, f := range l.frags {
		width = max(width, f.x1)
	}
	width -= l.hangW
	free := l.w - width - indent
	start, endOff := indent, 0.0
	if !ltr {
		start, endOff = free, indent
	}
	off := start
	align := lineAlign(st, last)
	if s.root.rubyLine {
		// A ruby base or annotation takes ruby-align instead (CSS Ruby
		// 1, 4.2).
		off = 0
		if free > eps {
			off = s.rubyAlign(l, free, ltr)
		}
		free = 0
	}
	if free > 0 {
		switch align {
		case css.AlignStart, css.AlignAuto:
		case css.AlignEnd:
			off = endOff
			if ltr {
				off = indent + free
			}
		case css.AlignLeft:
			off = indent
			if !ltr {
				off = 0
			}
		case css.AlignRight:
			off = indent + free
			if !ltr {
				off = free
			}
		case css.AlignCenter:
			off = float64(free / 2)
			if ltr {
				off += indent
			}
		case css.AlignJustify, css.AlignJustifyAll:
			// A line with no justification opportunity aligns to its
			// start (CSS Text 3, 7.1).
			if s.justify(l, free) {
				off = indent
				if !ltr {
					off = 0
				}
			}
		}
	}
	if free < -eps && st.JustifyShrink > 0 && s.justify(l, free) {
		// A line that took more text by shrinking its word separators
		// shrinks them to fit (css.Style.JustifyShrink).
		off = indent
		if !ltr {
			off = 0
		}
	}
	if l.hangLeft {
		// The hanging spaces come first, left of the aligned content.
		off -= l.hangW
	}
	for _, it := range l.items {
		it.x += off
		if it.text != nil {
			it.text.Origin = typeset.Point{X: it.x, Y: it.baseline}
		}
	}
	for _, f := range l.frags {
		f.x0 += off
		f.x1 += off
	}
}

// lineAlign returns the alignment of a line of a block of style st, last
// for the last line or one before a forced break: text-align-last there,
// whose auto takes text-align with justify as start (CSS Text 3, 6.3), and
// justify-all justifying every line (6.1).
func lineAlign(st *css.Style, last bool) css.TextAlign {
	align := st.TextAlign
	if align == css.AlignJustifyAll {
		align = css.AlignJustify
		if last && st.TextAlignLast == css.AlignAuto {
			return css.AlignJustify
		}
	}
	if !last {
		return align
	}
	switch st.TextAlignLast {
	case css.AlignAuto:
		if align == css.AlignJustify {
			return css.AlignStart
		}
		return align
	case css.AlignJustifyAll:
		return css.AlignJustify
	case css.AlignStart, css.AlignEnd, css.AlignLeft, css.AlignRight, css.AlignCenter, css.AlignJustify:
	}
	return st.TextAlignLast
}

// settle moves the atomic inlines of l, placed at l.y, to their place.
func (s *inlineState) settle(l *lineBox) {
	for _, it := range l.items {
		if a := it.atomic; a != nil {
			a.shift(l.x+it.x-(a.x-a.m[css.Left]), l.y+it.baseline-it.ascent-(a.y-a.m[css.Top]))
		}
	}
}

// recordAnchors records where the boxes of l first land, for
// Result.Anchors.
func (s *inlineState) recordAnchors(l *lineBox) {
	for _, f := range l.frags {
		if !f.box.anchorSet {
			s.anchor(f.box, l.x+f.x0, l.y+f.top)
		}
	}
	for _, it := range l.items {
		if b := it.box; b != nil && !b.anchorSet {
			s.anchor(b, l.x+it.x, l.y+it.baseline-it.ascent)
		}
	}
}

// anchor records that b first lands at (x, y), and so does the box a copy
// on a first formatted line stands for.
func (s *inlineState) anchor(b *box, x, y float64) {
	b.anchorX, b.anchorY, b.anchorSet = x, y, true
	if o, ok := s.e.cloneOf[b]; ok && !o.anchorSet {
		o.anchorX, o.anchorY, o.anchorSet = x, y, true
	}
}

// recordEmptyAnchors records where the empty inline boxes with an ID, or
// positioned, that open in the characters [start, end) of p land on the
// line l: at the item that follows them, on the line's top. Such a box
// makes no fragment of its own (CSS 2.1, 9.4.2), yet the converter anchors
// running elements and named strings to it, and a positioned one holds
// its absolutely positioned descendants there (10.1).
func (s *inlineState) recordEmptyAnchors(p *ifcPara, start, end int, l *lineBox) {
	for _, o := range p.opens {
		b := o.box
		if b.id == "" && b.st.Position == css.Static || b.anchorSet || o.pos < start || o.pos > end || o.pos == end && end < p.n {
			continue
		}
		x := 0.0
		for _, it := range l.items {
			if it.start >= o.pos {
				x = it.x
				break
			}
			x = it.x + it.w
		}
		s.anchor(b, l.x+x, l.y)
	}
}

// rubyOverhangs lays the ruby columns of p out and sets their overhang,
// which the min- and max-content widths of their line count, as Chromium
// counts it: a column whose annotation hangs over the text beside it
// takes the room its base and annotation leave.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 makes a
//     max-content box of a 60px annotation over a 20px base between two
//     20px characters 90px wide, 100px under ruby-overhang: none.
func (e *engine) rubyOverhangs(p *ifcPara) {
	if p.p == nil {
		return
	}
	for i := range p.p.Len() {
		info := p.infos[p.p.RunOf(i)]
		if info.kind != runAtomic || info.box.ruby == nil {
			continue
		}
		e.layoutAtomic(info.box, cblock{})
		e.rubyOverhang(p, i, info.box)
		if e.err != nil {
			return
		}
	}
}

// inlineIntrinsic returns the min- and max-content widths of the inline
// formatting context of b (CSS Sizing 3, 5.1): its widest unbreakable
// piece and its widest paragraph.
func (e *engine) inlineIntrinsic(b *box) (float64, float64) {
	d := e.ifcOf(b)
	if e.err != nil {
		return 0, 0
	}
	boxes := d.boxes
	if f := e.firstLines[b]; f != nil {
		boxes = append(slices.Clip(boxes), f.d.boxes...)
	}
	for _, k := range boxes {
		if k.kind == kindInline {
			resolveEdges(k, 0)
		}
	}
	lo, hi := 0.0, 0.0
	indent := max(resolve(b.st.TextIndent, 0), 0)
	for pi, p := range d.paras {
		// The floats of a paragraph sit beside its line in max-content
		// layout, side by side until one clears the others (CSS Sizing 3,
		// 5.2, as browsers lay floats out at the max-content width).
		floats := 0.0
		for _, f := range p.floats {
			fl, fh := e.contribution(f.box)
			lo, hi = max(lo, fl), max(hi, fh)
			if f.box.st.Clear != css.ClearNone {
				floats = 0
			}
			floats += fh
			hi = max(hi, floats)
		}
		if p.p == nil {
			continue
		}
		e.rubyOverhangs(p)
		minOf := func(a *box) float64 {
			if a.ruby != nil {
				return a.marginW()
			}
			l, _ := e.contribution(a)
			return l
		}
		minW := p.paraWidths(minOf)
		// The max-content line of the first paragraph is its first
		// formatted line, which a ::first-line styles.
		mp := p
		if f := e.firstLines[b]; pi == 0 && f != nil && f.para().p != nil {
			mp = f.para()
			e.rubyOverhangs(mp)
			lo = max(lo, firstSegment(mp, mp.paraWidths(minOf)))
		}
		maxW := mp.paraWidths(func(a *box) float64 {
			if a.ruby != nil {
				return a.marginW()
			}
			_, h := e.contribution(a)
			return h
		})
		n := p.p.Len()
		x := 0.0
		if indents(b.st, pi == 0 && !afterBlock(b), pi > 0 && d.paras[pi-1].forced) {
			x = indent
		}
		trail, hung := 0.0, 0.0
		for i := range mp.p.Len() {
			wi := maxW[i]
			if info := mp.infos[mp.p.RunOf(i)]; info.kind == runTab {
				if officeTabs(b.st) {
					wi, _ = e.officeTab(b, mp, maxW, i, x, math.Inf(1))
				} else {
					wi = e.tabWidth(info.box, x+hung)
					h := e.tabOverhang(mp, i, wi)
					wi -= h
					hung += h
				}
			}
			x += wi
			if mp.hangs(i) {
				trail += wi
			} else {
				trail = 0
			}
		}
		hi = max(hi, x-trail+floats)
		seg := 0.0
		trail, hung = 0, 0
		anywhere := false
		for i := range n {
			if i > 0 && (p.allowed(i) || anywhere && p.p.GraphemeStart(i)) {
				lo = max(lo, seg-trail)
				seg, trail, hung = 0, 0, 0
			}
			st := p.styleAt(i)
			anywhere = (st.OverflowWrap == css.Anywhere || st.LineBreak == css.LineBreakAnywhere) && wraps(st.WhiteSpace)
			wi := minW[i]
			if info := p.infos[p.p.RunOf(i)]; info.kind == runTab {
				if officeTabs(b.st) {
					wi, _ = e.officeTab(b, p, minW, i, seg, math.Inf(1))
				} else {
					wi = e.tabWidth(info.box, seg+hung)
					h := e.tabOverhang(p, i, wi)
					wi -= h
					hung += h
				}
			}
			seg += wi
			if p.hangs(i) {
				trail += wi
			} else {
				trail = 0
			}
		}
		lo = max(lo, seg-trail)
	}
	return lo, max(hi, lo)
}

// firstSegment returns the width of the characters of p before its first
// soft wrap opportunity, the spaces that hang there left out: the
// min-content contribution of a first line's first word.
func firstSegment(p *ifcPara, w []float64) float64 {
	seg, trail := 0.0, 0.0
	for i := range p.p.Len() {
		if i > 0 && p.allowed(i) {
			break
		}
		seg += w[i]
		if p.hangs(i) {
			trail += w[i]
		} else {
			trail = 0
		}
	}
	return seg - trail
}

// reversed returns a reversed copy of bs.
func reversed(bs []*box) []*box {
	out := slices.Clone(bs)
	slices.Reverse(out)
	return out
}
