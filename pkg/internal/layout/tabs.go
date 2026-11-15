package layout

import (
	"math"
	"unicode/utf8"

	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"golang.org/x/text/number"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// officeTabs reports a block container whose tabs follow the Office rules
// [css.TabStop] documents: one whose TabStops is not nil.
func officeTabs(st *css.Style) bool { return st != nil && st.TabStops != nil }

// tabChoice is the stop a tab of an Office paragraph went to, for its
// leader.
type tabChoice struct {
	leader css.TabLeader
}

// officeStep returns the default tab interval of the Office container of
// style st: TabSize as a length, or as a count of the space's advance.
func (e *engine) officeStep(st *css.Style, lang string) float64 {
	switch st.TabSize.Kind {
	case css.Points:
		return max(clamp(st.TabSize.Value), 0)
	case css.Number:
		return max(clamp(st.TabSize.Value), 0) * e.spaceWidth(st, lang)
	case css.Auto, css.Percent, css.None, css.Normal, css.FlexContent, css.MinContent, css.MaxContent, css.FitContent, css.Stretch, css.Calc:
	}
	return defaultTabSize * e.spaceWidth(st, lang)
}

// nextStop returns the stop a tab at offset at goes to in the Office container
// root (ECMA-376 Part 1, 17.3.1.37 and 17.15.1.25; ODF 1.3 Part 3, 17.8
// and 20.389): the first custom stop, bar stops aside, whose position lies
// past at, else the first multiple of the default interval past both at and
// the last custom stop, a left stop. ok is false when there is no stop: a
// zero interval past the custom stops.
func (e *engine) nextStop(root *box, at float64) (css.TabStop, bool) {
	last := 0.0
	for _, ts := range root.st.TabStops {
		if ts.Align == css.TabBar || !numeric.NonNegative(ts.Pos) || math.IsInf(ts.Pos, 1) {
			continue
		}
		if ts.Pos > at+eps {
			return ts, true
		}
		last = max(last, ts.Pos)
	}
	step := e.officeStep(root.st, root.lang)
	if step <= eps {
		return css.TabStop{}, false
	}
	base := max(at, last)
	return css.TabStop{Pos: (math.Floor(base/step+eps) + 1) * step}, true
}

// officeTab returns the width of the tab at character i of p, a paragraph
// of the Office container root, when the tab starts at offset at from the start
// edge of root's content box on a line that ends at lineEnd, and whether
// the line must break before the tab: the first word after it does not
// fit beside it. w holds the widths of p's characters.
//
// The text the stop aligns runs from the tab to the next tab or the
// paragraph's end, without the spaces that hang there. A decimal stop
// aligns the first decimal separator of the paragraph's language in it,
// else its end. A stop past the line's end makes the tab fill the rest of
// the line, as Writer cuts it at the right margin.
func (e *engine) officeTab(root *box, p *ifcPara, w []float64, i int, at, lineEnd float64) (float64, bool) {
	ts, ok := e.nextStop(root, at)
	if !ok {
		return 0, false
	}
	if p.tabs == nil {
		p.tabs = map[int]tabChoice{}
	}
	p.tabs[i] = tabChoice{leader: ts.Leader}
	n := p.p.Len()
	end := i + 1
	for end < n && p.infos[p.p.RunOf(end)].kind != runTab {
		end++
	}
	stop := end
	for stop > i+1 && p.hangs(stop-1) {
		stop--
	}
	sep := e.decimalSeparator(root.lang)
	seg, before := 0.0, -1.0
	for j := i + 1; j < stop; j++ {
		if before < 0 && ts.Align == css.TabDecimal && p.p.Rune(j) == sep {
			before = seg
		}
		seg += w[j]
	}
	if before < 0 {
		before = seg
	}
	tw := ts.Pos - at
	switch ts.Align {
	case css.TabRight:
		tw -= seg
	case css.TabCenter:
		tw -= float64(seg / 2)
	case css.TabDecimal:
		tw -= before
	case css.TabLeft, css.TabBar:
	}
	tw = max(tw, 0)
	if ts.Pos > lineEnd+eps {
		return max(min(tw, lineEnd-at), 0), false
	}
	word := 0.0
	for j := i + 1; j < stop; j++ {
		if j > i+1 && p.allowed(j) {
			break
		}
		word += w[j]
	}
	return tw, at+tw+word > lineEnd+eps
}

// decimalSeparator returns the decimal separator of language lang, the
// character a decimal tab stop aligns on: the comma of French or German,
// the full stop of English and of an unknown language (CLDR, through
// golang.org/x/text).
func (e *engine) decimalSeparator(lang string) rune {
	if r, ok := e.decimals[lang]; ok {
		return r
	}
	sep := '.'
	if tag, err := language.Parse(lang); err == nil {
		s := message.NewPrinter(tag).Sprint(number.Decimal(1.5))
		if r, size := utf8.DecodeRuneInString(s[min(1, len(s)):]); size > 0 && r != '5' {
			sep = r
		}
	}
	if e.decimals == nil {
		e.decimals = map[string]rune{}
	}
	e.decimals[lang] = sep
	return sep
}

// leaderRune returns the character a tab leader repeats, 0 for none.
func leaderRune(l css.TabLeader) rune {
	switch l {
	case css.TabLeaderDots:
		return '.'
	case css.TabLeaderHyphens:
		return '-'
	case css.TabLeaderUnderscore:
		return '_'
	case css.TabLeaderMiddleDots:
		return '·'
	case css.TabLeaderNone:
	}
	return 0
}

// tabLeader returns the glyph run that fills the tab item it of a line of
// paragraph p with its stop's leader: the leader's character repeated in
// the tab's font, as Writer fills a tab portion. Writer counts the copies
// in whole twips, the tab's width over the character's advance truncated to
// a twip, and spreads them evenly over the tab, the first at its start
// and the last ending at its end. nil when the tab has no leader or no
// room. Under Word's rules, word set, it.word holds how Word sets the
// copies once the line's place on the page is known ([wordLeader]).
//
// # Corpus evidence
//
//   - libreoffice/odt: of 105 runs of six or more dots, hyphens or
//     underscores between text in Writer's text, 94 read joined to the
//     text on both sides and 7 to the text before them.
func (e *engine) tabLeader(p *ifcPara, it *lineItem, word bool) *typeset.Text {
	tc, ok := p.tabs[it.start]
	if !ok {
		return nil
	}
	r := leaderRune(tc.leader)
	if r == 0 || it.w <= eps || e.fontSize(it.box.st) <= 0 {
		return nil
	}
	para, _, err := e.fonts.Shape(e.ctx, []text.ParagraphRun{{Text: string(r), Style: e.textStyle(it.box.st, it.box.lang)}}, text.ParagraphOptions{})
	if err != nil {
		e.shapeFailed(err, "tab leader", -1)
		return nil
	}
	if para.Len() != 1 {
		return nil
	}
	pl := para.Line(0, 1, false)
	if len(pl.Spans) != 1 || pl.Spans[0].Text == nil || len(pl.Spans[0].Text.Glyphs) != 1 || pl.Width <= eps {
		return nil
	}
	cw := pl.Width
	twip := math.Floor(float64(cw*twips) + 1e-6)
	if twip < 1 {
		twip = 1
	}
	k := min(math.Floor(it.w*twips/twip+1e-6), maxLeaderCopies)
	if k < 1 {
		return nil
	}
	step := it.w / k
	src := pl.Spans[0].Text
	t := *src
	t.Glyphs = make([]typeset.Glyph, 0, int(k)) // fits int: at most maxLeaderCopies
	hs := hscale(src)
	for c := range int(k) { // fits int: at most maxLeaderCopies
		g := src.Glyphs[0]
		g.X = float64(c) * step / hs
		t.Glyphs = append(t.Glyphs, g)
	}
	if word && !it.rtl {
		it.word = e.wordLeader(&t, it, cw, twip/twips)
	}
	return &t
}

// wordLeader is how Word sets the copies of a tab leader: pitch apart,
// the advance of the leader's character truncated to a twip, on a grid
// that starts at the page's left edge, so that the copies of the lines of
// a table of contents stand in columns. The first copy stands on the
// first grid point at or after the tab's start and the last on the last
// grid point a pitch or more before the tab's end. A space of the leader's face opens
// the copies at the tab's start and another follows the last: the leader
// reads as a word of its own between the text on either side, as Word's
// text does. ISO 32000-1, 14.8.2.5 asks for word breaks stated by space
// characters. Writer spreads the copies over the whole tab and states no
// space ([engine.tabLeader]).
//
// # Corpus evidence
//
//   - libreoffice/docx ooxmlexport/TOC_field_f.docx: Word's text element of
//     each entry reads "Heading 1 ...... " with a space each side of the
//     dots; the 145 dots of 2.777 points (Calibri 11) stand 2.75 points
//     apart, the first 1.47 points after the "1" and the last ending 0.25
//     points before the page number, at 43 and 187 pitches from the page
//     edge.
//   - litho/docx fields-toc: the dots of entries whose text ends at
//     108.03, 109.54, 127.77 and 136.03 points start at 110.0, 110.0,
//     129.25 and 137.5, 40, 40, 47 and 50 pitches of 2.75 from the page
//     edge, and all end at 533.5, 0.5 points before the page number.
//   - litho/docx fields-toc, para-tabs, sink-report, fields-seq-captions;
//     openxmlsdk/docx Toc/TOC2.docx, bookmark/bookmark.docx: Word's text
//     reads a space each side of a leader; libreoffice/docx
//     ooxmlexport/TOC_field_b.docx, TOC_field_f.docx,
//     PreserveWfieldTOC.docx and PreserveXfieldTOC.docx read one before
//     it alone, the page number standing over the closing space.
type wordLeader struct {
	pitch, adv float64
	copy, sp   typeset.Glyph
}

// wordLeader returns how Word sets the copies of t, the leader run of the
// tab item it, whose character advances adv points and pitch once
// truncated to a twip; nil when the leader's face has no space, t then
// keeping Writer's copies.
func (e *engine) wordLeader(t *typeset.Text, it *lineItem, adv, pitch float64) *wordLeader {
	para, _, err := e.fonts.Shape(e.ctx, []text.ParagraphRun{{Text: " ", Style: e.textStyle(it.box.st, it.box.lang)}}, text.ParagraphOptions{})
	if err != nil {
		e.shapeFailed(err, "tab leader", -1)
		return nil
	}
	if para.Len() != 1 {
		return nil
	}
	pl := para.Line(0, 1, false)
	if len(pl.Spans) != 1 || pl.Spans[0].Text == nil || len(pl.Spans[0].Text.Glyphs) != 1 || pl.Spans[0].Text.Face != t.Face {
		return nil
	}
	c := t.Glyphs[0]
	c.X = 0
	return &wordLeader{pitch: pitch, adv: adv, copy: c, sp: typeset.Glyph{GID: pl.Spans[0].Text.Glyphs[0].GID, Text: " "}}
}

// glyphs returns the glyphs of a leader set by w over a tab of width w
// points whose start is x points from the page's left edge, in a run
// scaled horizontally by hs: the opening space, the copies, the closing
// space.
func (wl *wordLeader) glyphs(x, w, hs float64) []typeset.Glyph {
	first := math.Ceil(float64(x/wl.pitch) - 1e-6)
	last := math.Floor(float64((x+w)/wl.pitch)+1e-6) - 1
	n := min(max(last-first+1, 0), maxLeaderCopies)
	gs := make([]typeset.Glyph, 0, int(n)+2) // fits int: at most maxLeaderCopies
	gs = append(gs, wl.sp)
	end := 0.0
	for c := range int(n) { // fits int: at most maxLeaderCopies
		g := wl.copy
		at := float64((first+float64(c))*wl.pitch) - x
		g.X = at / hs
		gs = append(gs, g)
		end = at + wl.adv
	}
	if n > 0 {
		sp := wl.sp
		sp.X = end / hs
		gs = append(gs, sp)
	}
	return gs
}

// barRules returns the line-relative positions of the bar stops of the
// Office container root on a line placed at x in the content box cb:
// from the start edge of the content box, its left edge in a
// left-to-right container and its right edge in a right-to-left one.
func barRules(root *box, cb cblock, x float64) []float64 {
	var out []float64
	for _, ts := range root.st.TabStops {
		if ts.Align != css.TabBar || !numeric.NonNegative(ts.Pos) || math.IsInf(ts.Pos, 1) {
			continue
		}
		if root.st.Direction == css.RTL {
			out = append(out, cb.x+cb.w-ts.Pos-x)
		} else {
			out = append(out, cb.x+ts.Pos-x)
		}
	}
	return out
}

// hscale returns the horizontal scale of the glyph run t, 1 for none.
func hscale(t *typeset.Text) float64 {
	if t == nil || t.HScale <= 0 {
		return 1
	}
	return t.HScale
}

// afterLastTab returns the clusters of cls, the clusters of line l in
// visual order, that follow the line's last tab: the ones a justified
// Office line stretches, as Writer justifies only the text after the last
// tab portion. In a right-to-left line they lie left of it.
func afterLastTab(l *lineBox, cls []jcluster, rtl bool) []jcluster {
	last := -1
	for k, it := range l.items {
		if it.tab && (last < 0 || !rtl) {
			last = k
		}
	}
	if last < 0 {
		return cls
	}
	index := make(map[*lineItem]int, len(l.items))
	for k, it := range l.items {
		index[it] = k
	}
	out := cls[:0:0]
	for _, c := range cls {
		if k := index[c.it]; rtl && k < last || !rtl && k > last {
			out = append(out, c)
		}
	}
	return out
}

// barRules paints the bar tab stops of line l: a rule one CSS pixel wide
// across the line box, centred on each stop.
func (p *painter) barRules(l *lineBox, page int) {
	if len(l.bars) == 0 || l.barColor.A <= 0 || l.h <= 0 || l.barOwner == nil || l.barOwner.hidden {
		return
	}
	for _, x := range l.bars {
		r := typeset.Rect{X: l.x + x - css.Pt/2 + p.dx, Y: l.y + p.dy, Width: css.Pt, Height: l.h}
		p.emit(l.barOwner, true, &typeset.Path{Ops: rectOps(r), Fill: &typeset.Fill{Color: l.barColor}}, l.y, l.y+l.h, page)
	}
}
