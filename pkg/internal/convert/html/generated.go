package html

import (
	"context"
	"fmt"
	"strings"

	dom "golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
)

// listItem is the implicit counter of list items (CSS Lists 3, 4.6).
const listItem = "list-item"

// counterInst is one counter instance (CSS Lists 3, 4): its value, whether
// it counts down, and the level of the element that created it.
type counterInst struct {
	value    int
	reversed bool
	level    int
}

// counterState holds the counters in scope during the builder's document
// order walk (CSS Lists 3, 4.5). A level is the children of one element:
// an instance an element creates lives until its parent's children are
// done, so its following siblings and their descendants see it.
type counterState struct {
	byName map[string][]counterInst
	// created lists, per open level, the names that level created, in
	// order, so closing the level drops them.
	created [][]string
	live    int
	// quoteDepth is the nesting of open-quote and close-quote (CSS
	// Generated Content 3, 2.2), which the walk carries in document order.
	quoteDepth int
}

// level returns the current level's index.
func (c *counterState) level() int { return len(c.created) }

// open starts the level of an element's children.
func (c *counterState) open() { c.created = append(c.created, nil) }

// close ends the current level, dropping the counters it created.
func (c *counterState) close() {
	if len(c.created) == 0 {
		return
	}
	lvl := len(c.created)
	for _, name := range c.created[lvl-1] {
		s := c.byName[name]
		if k := len(s); k > 0 && s[k-1].level == lvl {
			c.byName[name] = s[:k-1]
			c.live--
		}
	}
	c.created = c.created[:lvl-1]
}

// innermost returns the innermost instance named name, nil for none.
func (c *counterState) innermost(name string) *counterInst {
	s := c.byName[name]
	if len(s) == 0 {
		return nil
	}
	return &s[len(s)-1]
}

// instantiate creates a counter named name on the current element (CSS
// Lists 3, 4.5, instantiate a counter): it replaces an instance the
// element or a previous sibling created. It reports false when
// maxCounters is reached.
func (c *counterState) instantiate(name string, value int, reversed bool) bool {
	lvl := c.level()
	if in := c.innermost(name); in != nil && in.level == lvl {
		in.value, in.reversed = value, reversed
		return true
	}
	if c.live >= maxCounters {
		return false
	}
	if c.byName == nil {
		c.byName = map[string][]counterInst{}
	}
	c.byName[name] = append(c.byName[name], counterInst{value: value, reversed: reversed, level: lvl})
	c.live++
	if lvl > 0 {
		c.created[lvl-1] = append(c.created[lvl-1], name)
	}
	return true
}

// values returns the values of every instance named name, outermost
// first (counters()).
func (c *counterState) values(name string) []int {
	s := c.byName[name]
	out := make([]int, len(s))
	for i, in := range s {
		out[i] = in.value
	}
	return out
}

// isListItem reports a display value that makes a list item, block or
// inline (CSS Lists 3, 2).
func isListItem(d css.Display) bool { return d == css.ListItem || d == css.InlineListItem }

// clampCounter keeps a counter value within ±maxListValue, the values the
// layout engine numbers.
func clampCounter(v int) int { return min(max(v, -maxListValue), maxListValue) }

// applyCounters runs the counter properties of the element or
// pseudo-element styled st (CSS Lists 3, 4.5): counter-reset, then
// counter-increment, with the implicit increment of list-item on a list
// item, then counter-set. n is the originating element for the HTML
// attributes that act as counter properties (HTML Standard, 15.3.7: ol
// start and reversed, li value), nil for a pseudo-element. It returns the
// value of list-item for a list item, 0 otherwise.
func (b *builder) applyCounters(n *dom.Node, st *css.Style) int {
	c := &b.counters
	inst := func(name string, v int, rev bool) {
		if !c.instantiate(name, clampCounter(v), rev) {
			b.warnOnce(CodeGeneratedLimit, "counters", fmt.Sprintf("more than %d counters in scope; the others were left out", maxCounters))
		}
	}
	for _, op := range st.CounterReset {
		v, rev := op.Value, op.Reversed
		switch {
		case n != nil && op.Name == listItem && n.Namespace == "" && n.DataAtom == atom.Ol:
			v, rev = b.olStart(n, op)
		case rev && op.Implicit:
			v = b.reversedStart(n)
		}
		inst(op.Name, v, rev)
	}
	isItem := isListItem(st.Display)
	implicit := isItem
	for _, op := range st.CounterIncrement {
		if op.Name == listItem {
			implicit = false
		}
		b.increment(op.Name, op.Value, inst)
	}
	if implicit {
		step := 1
		if in := c.innermost(listItem); in != nil && in.reversed {
			step = -1
		}
		b.increment(listItem, step, inst)
	}
	set := st.CounterSet
	if n != nil && isItem && n.Namespace == "" && n.DataAtom == atom.Li {
		if v, ok := attr(n, "value"); ok {
			if i, ok := parseInteger(v); ok {
				set = append(append([]css.CounterOp(nil), set...), css.CounterOp{Name: listItem, Value: i})
			}
		}
	}
	for _, op := range set {
		if in := c.innermost(op.Name); in != nil {
			in.value = clampCounter(op.Value)
		} else {
			inst(op.Name, op.Value, false)
		}
	}
	if !isItem {
		return 0
	}
	if in := c.innermost(listItem); in != nil {
		return in.value
	}
	return 0
}

// increment adds by to the innermost counter named name, instantiating it
// at 0 when none is in scope (CSS Lists 3, 4.5).
func (b *builder) increment(name string, by int, inst func(string, int, bool)) {
	in := b.counters.innermost(name)
	if in == nil {
		inst(name, 0, false)
		if in = b.counters.innermost(name); in == nil {
			return
		}
	}
	in.value = clampCounter(in.value + by)
}

// olStart returns the value the list-item counter an <ol> resets starts
// at, and whether it counts down (HTML Standard, 4.4.8 and 15.3.7): the
// value counter-reset gives, unless a start or reversed attribute
// decides it: one before start, or one past it for a reversed list.
func (b *builder) olStart(ol *dom.Node, op css.CounterOp) (int, bool) {
	_, revAttr := attr(ol, "reversed")
	start, hasStart := 0, false
	if v, ok := attr(ol, "start"); ok {
		start, hasStart = parseInteger(v)
	}
	if !hasStart && !revAttr {
		if op.Reversed && op.Implicit {
			return b.reversedStart(ol), true
		}
		return op.Value, op.Reversed
	}
	if !revAttr && !op.Reversed {
		return start - 1, false
	}
	if !hasStart {
		return b.reversedStart(ol), true
	}
	return start + 1, true
}

// reversedStart returns the starting value of a reversed list-item
// counter n resets without a value (CSS Lists 3, 4.1, reversed()): one
// past the count of its list items, the count stopping at the first item
// whose value attribute sets the counter.
func (b *builder) reversedStart(n *dom.Node) int {
	if n == nil {
		return 0
	}
	num, first := 0, true
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != dom.ElementNode {
			continue
		}
		if st := b.computed.Style(c); st == nil || !isListItem(st.Display) {
			continue
		}
		if first {
			num++
			first = false
		}
		if c.DataAtom == atom.Li && c.Namespace == "" {
			if v, ok := attr(c, "value"); ok {
				if i, ok := parseInteger(v); ok {
					num += i
					break
				}
			}
		}
		num++
	}
	return clampCounter(num)
}

// pseudo builds the box of the pseudo-element which of the element n,
// styled ps, after running its counter properties: nil when it has no
// content (CSS Generated Content 3, 1.1: normal and none generate no
// ::before or ::after box).
func (b *builder) pseudo(ctx context.Context, n *dom.Node, which css.PseudoElement) *layout.Node {
	ps := b.computed.Pseudo(n, which)
	if ps == nil || ps.Display == css.DisplayNone || ps.Content.Kind != css.ContentList {
		return nil
	}
	v := b.applyCounters(nil, ps)
	node := &layout.Node{Kind: layout.Element, Style: ps, ListValue: v}
	node.Children = b.generate(ctx, n, ps)
	return node
}

// marker returns the ::marker of the list item n styled st, numbered v,
// for layout.Node.Marker: nil when the engine's default marker draws it.
func (b *builder) marker(ctx context.Context, n *dom.Node, st *css.Style, v int) *layout.Node {
	ms := b.computed.Pseudo(n, css.PseudoMarker)
	if ms == nil {
		if v != 0 || st.ListStyleImage != "" {
			return nil
		}
		// The engine numbers a 0 ListValue after the previous item.
		return &layout.Node{Kind: layout.Element, Style: st, Children: []*layout.Node{{Kind: layout.Text, Style: st, Text: layout.MarkerText(st, 0)}}}
	}
	m := &layout.Node{Kind: layout.Element, Style: ms, Alt: ms.Content.Alt}
	switch ms.Content.Kind {
	case css.ContentNone:
		m.Children = []*layout.Node{{Kind: layout.Text, Style: ms}}
	case css.ContentList:
		m.Children = b.generate(ctx, n, ms)
		if len(m.Children) == 0 {
			m.Children = []*layout.Node{{Kind: layout.Text, Style: ms}}
		}
	case css.ContentNormal:
		if v == 0 && ms.ListStyleImage == "" {
			m.Children = []*layout.Node{{Kind: layout.Text, Style: ms, Text: layout.MarkerText(ms, 0)}}
		}
	}
	return m
}

// generate builds the nodes of the content list of a pseudo-element of n
// styled ps (CSS Generated Content 3, 1.1): adjacent text joins into one
// Text node, an image is a Replaced node, an image that cannot be drawn
// is left out with a warning.
func (b *builder) generate(ctx context.Context, n *dom.Node, ps *css.Style) []*layout.Node {
	var out []*layout.Node
	var sb strings.Builder
	cut := false
	write := func(s string) {
		if room := maxGeneratedBytes - sb.Len(); len(s) > room {
			s, cut = s[:max(room, 0)], true
		}
		sb.WriteString(s)
	}
	flush := func() {
		if sb.Len() > 0 {
			out = append(out, &layout.Node{Kind: layout.Text, Style: ps, Text: strings.ToValidUTF8(sb.String(), "�")})
			sb.Reset()
		}
	}
	c := &b.counters
	for _, it := range ps.Content.Items {
		switch it.Kind {
		case css.ItemString:
			write(it.Text)
		case css.ItemAttr:
			if v, ok := attr(n, it.Text); ok {
				write(v)
			} else {
				write(it.Fallback)
			}
		case css.ItemCounter:
			v := 0
			if in := c.innermost(it.Text); in != nil {
				v = in.value
			} else {
				// counter() of a counter not in scope instantiates it at 0
				// on the pseudo-element (CSS Lists 3, 4.5).
				_ = c.instantiate(it.Text, 0, false)
			}
			write(layout.CounterText(it.Style, v))
		case css.ItemCounters:
			vals := c.values(it.Text)
			if len(vals) == 0 {
				_ = c.instantiate(it.Text, 0, false)
				vals = []int{0}
			}
			for i, v := range vals {
				if i > 0 {
					write(it.Separator)
				}
				write(layout.CounterText(it.Style, v))
			}
		case css.ItemOpenQuote, css.ItemCloseQuote, css.ItemNoOpenQuote, css.ItemNoCloseQuote:
			write(b.quote(n, ps, it.Kind))
		case css.ItemImage:
			if pic := b.contentImage(ctx, it.Text); pic != nil {
				flush()
				out = append(out, &layout.Node{Kind: layout.Replaced, Style: anonInline(ps), Picture: pic})
			}
		case css.ItemLeader:
			if n := leaderNode(ps, it.Text); n != nil {
				flush()
				out = append(out, n)
			}
		case css.ItemNamedString, css.ItemElement, css.ItemContent:
			// string() and element() generate content in a page-margin
			// box alone, content() in string-set alone (CSS GCPM 3, 2).
		}
	}
	flush()
	if cut {
		b.warnOnce(CodeGeneratedLimit, "text", fmt.Sprintf("generated content past %d bytes was left out", maxGeneratedBytes))
	}
	return out
}

// contentImage loads the picture of a url() content item, nil with a
// warning when it cannot be drawn.
func (b *builder) contentImage(ctx context.Context, u string) *layout.Picture {
	if u == "" {
		return nil
	}
	pic, err := b.res.picture(ctx, u)
	if err != nil {
		if ctx.Err() == nil {
			b.warnings = append(b.warnings, compose.Warning{Code: warningCode(err), Message: fmt.Sprintf(
				"the generated image %s was left out: %v", displayURL(u), err)})
		}
		return nil
	}
	return pic
}

// quote returns the text of a quote item and moves the quote depth (CSS
// Generated Content 3, 2.2): open-quote draws the open quote of the
// current depth and nests, close-quote un-nests and draws the close
// quote, the no- forms only move the depth. A close quote at depth 0 does
// nothing; a depth past the pairs takes the last pair.
func (b *builder) quote(n *dom.Node, ps *css.Style, k css.ContentItemKind) string {
	c := &b.counters
	pairs := ps.Quotes.Pairs
	if ps.Quotes.None {
		pairs = nil
	} else if pairs == nil {
		pairs = langQuotes(b.langOf(n))
	}
	pick := func(d, i int) string {
		if len(pairs) == 0 {
			return ""
		}
		return pairs[min(d, len(pairs)-1)][i]
	}
	switch k { //nolint:exhaustive // the caller passes the quote items alone
	case css.ItemOpenQuote:
		s := pick(c.quoteDepth, 0)
		c.quoteDepth = min(c.quoteDepth+1, maxTreeDepth)
		return s
	case css.ItemNoOpenQuote:
		c.quoteDepth = min(c.quoteDepth+1, maxTreeDepth)
	case css.ItemCloseQuote:
		if c.quoteDepth == 0 {
			return ""
		}
		c.quoteDepth--
		return pick(c.quoteDepth, 1)
	case css.ItemNoCloseQuote:
		c.quoteDepth = max(c.quoteDepth-1, 0)
	}
	return ""
}

// langOf returns the language of the element n: its own or its nearest
// ancestor's lang attribute, else the document's.
func (b *builder) langOf(n *dom.Node) string {
	for a := n; a != nil; a = a.Parent {
		if a.Type != dom.ElementNode {
			continue
		}
		if v, ok := attr(a, "lang"); ok {
			return strings.TrimSpace(v)
		}
	}
	return b.scan.lang
}

// quoteTable maps a primary language subtag to its quotes, outer then
// inner pair, after the Unicode CLDR delimiters data that browsers draw
// for quotes: auto (CSS Generated Content 3, 2.1). Immutable lookup
// table.
var quoteTable = map[string][][2]string{
	"en": {{"“", "”"}, {"‘", "’"}},
	"fr": {{"« ", " »"}, {"“", "”"}},
	"de": {{"„", "“"}, {"‚", "‘"}},
	"cs": {{"„", "“"}, {"‚", "‘"}},
	"es": {{"«", "»"}, {"“", "”"}},
	"it": {{"«", "»"}, {"“", "”"}},
	"pt": {{"“", "”"}, {"‘", "’"}},
	"nl": {{"‘", "’"}, {"“", "”"}},
	"ru": {{"«", "»"}, {"„", "“"}},
	"pl": {{"„", "”"}, {"«", "»"}},
	"sv": {{"”", "”"}, {"’", "’"}},
	"ja": {{"「", "」"}, {"『", "』"}},
	"zh": {{"“", "”"}, {"‘", "’"}},
	"ko": {{"“", "”"}, {"‘", "’"}},
}

// langQuotes returns the quotes of the language tag lang, English ones for
// an unknown language.
func langQuotes(lang string) [][2]string {
	primary, _, _ := strings.Cut(strings.ToLower(lang), "-")
	if q, ok := quoteTable[primary]; ok {
		return q
	}
	return quoteTable["en"]
}

// anonInline returns the style of the anonymous inline replaced box of an
// image in generated content: the pseudo-element's inherited properties,
// the initial value of the others (CSS Generated Content 3, 1.1).
func anonInline(ps *css.Style) *css.Style {
	st := *ps
	in := css.Initial()
	st.Display = css.Inline
	st.Position, st.Float, st.Clear = in.Position, in.Float, in.Clear
	st.Width, st.Height = in.Width, in.Height
	st.MinWidth, st.MinHeight, st.MaxWidth, st.MaxHeight = in.MinWidth, in.MinHeight, in.MaxWidth, in.MaxHeight
	st.Top, st.Right, st.Bottom, st.Left = in.Top, in.Right, in.Bottom, in.Left
	st.Margin, st.Padding, st.Border, st.BorderRadius = in.Margin, in.Padding, in.Border, in.BorderRadius
	st.BackgroundColor, st.BackgroundImage, st.BackgroundGradient = in.BackgroundColor, in.BackgroundImage, in.BackgroundGradient
	st.BoxShadow, st.Outline, st.Opacity, st.Transform = nil, in.Outline, in.Opacity, nil
	st.ZIndex, st.VerticalAlign = in.ZIndex, in.VerticalAlign
	st.CounterReset, st.CounterIncrement, st.CounterSet = nil, nil, nil
	st.Content = css.Content{}
	return &st
}

// leaderNode returns the leader of a pseudo-element styled ps whose string
// is pattern (CSS Generated Content 3, leader()): an inline block holding
// the string with its spaces preserved, which the layout repeats to fill
// its line. An empty string draws no leader.
func leaderNode(ps *css.Style, pattern string) *layout.Node {
	if pattern == "" {
		return nil
	}
	st := anonInline(ps)
	st.Display, st.WhiteSpace = css.InlineBlock, css.Pre
	return &layout.Node{Kind: layout.Element, Style: st, Leader: true, Children: []*layout.Node{
		{Kind: layout.Text, Style: st, Text: strings.ToValidUTF8(pattern, "\uFFFD")},
	}}
}
