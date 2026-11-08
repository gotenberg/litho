package layout

import (
	"math"
	"unicode/utf8"

	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"golang.org/x/text/number"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/text"
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
	return 8 * e.spaceWidth(st, lang)
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
		if ts.Align == css.TabBar || !(ts.Pos >= 0) || math.IsInf(ts.Pos, 1) {
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
		tw -= seg / 2
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
// room.
//
// # Corpus evidence
//
//   - libreoffice/docx: ooxmlexport/TOC_field_f.docx draws 145 dots of 2.772 points
//     in a tab of 400.9 points, 2.765 points apart, where whole advances
//     fit 144.
func (e *engine) tabLeader(p *ifcPara, it *lineItem) *compose.Text {
	tc, ok := p.tabs[it.start]
	if !ok {
		return nil
	}
	r := leaderRune(tc.leader)
	if r == 0 || it.w <= eps || e.fontSize(it.box.st) <= 0 {
		return nil
	}
	para, _, err := e.fonts.Shape(e.ctx, []text.ParagraphRun{{Text: string(r), Style: e.textStyle(it.box.st, it.box.lang)}}, text.ParagraphOptions{})
	if err != nil || para.Len() != 1 {
		return nil
	}
	pl := para.Line(0, 1, false)
	if len(pl.Spans) != 1 || pl.Spans[0].Text == nil || len(pl.Spans[0].Text.Glyphs) != 1 || pl.Width <= eps {
		return nil
	}
	cw := pl.Width
	twip := math.Floor(cw*20 + 1e-6)
	if twip < 1 {
		twip = 1
	}
	k := min(math.Floor(it.w*20/twip+1e-6), maxLeaderCopies)
	if k < 1 {
		return nil
	}
	step := it.w / k
	src := pl.Spans[0].Text
	t := *src
	t.Glyphs = make([]compose.Glyph, 0, int(k)) // fits int: at most maxLeaderCopies
	hs := hscale(src)
	for c := range int(k) { // fits int: at most maxLeaderCopies
		g := src.Glyphs[0]
		g.X = float64(c) * step / hs
		t.Glyphs = append(t.Glyphs, g)
	}
	return &t
}

// barRules returns the line-relative positions of the bar stops of the
// Office container root on a line placed at x in the content box cb:
// from the start edge of the content box, its left edge in a
// left-to-right container and its right edge in a right-to-left one.
func barRules(root *box, cb cblock, x float64) []float64 {
	var out []float64
	for _, ts := range root.st.TabStops {
		if ts.Align != css.TabBar || !(ts.Pos >= 0) || math.IsInf(ts.Pos, 1) {
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
func hscale(t *compose.Text) float64 {
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
		r := compose.Rect{X: l.x + x - css.Pt/2 + p.dx, Y: l.y + p.dy, Width: css.Pt, Height: l.h}
		p.emit(l.barOwner, true, &compose.Path{Ops: rectOps(r), Fill: &compose.Fill{Color: l.barColor}}, l.y, l.y+l.h, page)
	}
}
