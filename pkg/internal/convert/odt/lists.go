package odt

import (
	"encoding/xml"
	"math"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/odf"
)

// listCounter numbers the items of one list and the lists that continue
// it, one counter per level.
type listCounter struct {
	n    [odf.MaxListLevels]int
	used [odf.MaxListLevels]bool
}

// listState is the numbering of every list of the document.
type listState struct {
	// last maps a list style to the counter of the last list of that
	// style, which text:continue-numbering continues; byID maps an
	// xml:id to its list's counter, which text:continue-list continues.
	last map[string]*listCounter
	byID map[string]*listCounter
	// outline numbers the headings through the outline style.
	outline listCounter
}

// listCtx is the list a block sits in.
type listCtx struct {
	style   *odf.ListStyle
	name    string
	level   int
	counter *listCounter
}

// itemCtx is the list item a paragraph sits in: its level's properties
// indent every paragraph of the item, and the first paragraph takes the
// label.
type itemCtx struct {
	lvl   *odf.ListLevel
	label string
	// pic is the label of an image bullet.
	pic *layout.Picture
	// value is the item's number.
	value int
	// header marks a text:list-header, which has no label.
	header bool
	// placed reports the label is placed: later paragraphs are indented
	// alone.
	placed bool
}

// list builds a text:list (ODF 1.3, Part 3, 5.3): its items, each
// paragraph indented as its level's list-level-properties say and the
// first paragraph of each numbered item labelled.
func (b *builder) list(c *odf.Element, ctx *blockCtx) []*layout.Node {
	if b.lists == nil {
		b.lists = &listState{last: map[string]*listCounter{}, byID: map[string]*listCounter{}}
	}
	outer := ctx.list
	lc := &listCtx{level: 1}
	if outer != nil {
		*lc = *outer
		lc.level = min(outer.level+1, odf.MaxListLevels)
	}
	if name := c.A(odf.NSText, "style-name"); name != "" {
		lc.name = name
		lc.style = b.styles.List(b.scope, name)
	}
	if outer == nil {
		switch {
		case c.A(odf.NSText, "continue-list") != "" && b.lists.byID[c.A(odf.NSText, "continue-list")] != nil:
			lc.counter = b.lists.byID[c.A(odf.NSText, "continue-list")]
		case c.A(odf.NSText, "continue-numbering") == "true" && b.lists.last[lc.name] != nil:
			lc.counter = b.lists.last[lc.name]
		default:
			lc.counter = &listCounter{}
		}
		b.lists.last[lc.name] = lc.counter
	}
	if id := c.A(odf.NSXML, "id"); id != "" {
		b.lists.byID[id] = lc.counter
	}
	var lvl *odf.ListLevel
	if lc.style != nil {
		lvl = lc.style.Levels[lc.level-1]
	}
	var out []*layout.Node
	for _, it := range c.Children {
		if b.err != nil {
			break
		}
		header := it.Is(odf.NSText, "list-header")
		if !header && !it.Is(odf.NSText, "list-item") {
			continue
		}
		item := &itemCtx{lvl: lvl, header: header}
		if !header && lvl != nil {
			b.number(item, lc, it)
		}
		inner := *ctx
		inner.list = lc
		inner.item = item
		if header {
			inner.item = nil
		}
		inner.indent = item
		out = append(out, b.blocks(it, inner)...)
	}
	return out
}

// numberedParagraph builds a text:numbered-paragraph (ODF 1.3, Part 3,
// 5.3.6): its paragraph labelled at its text:level of the list its
// paragraph style's list style gives, as the same paragraph nested in
// that many lists would be.
func (b *builder) numberedParagraph(c *odf.Element, ctx *blockCtx) []*layout.Node {
	level, err := strconv.Atoi(c.A(odf.NSText, "level"))
	if err != nil || level < 1 {
		level = 1
	}
	level = min(level, odf.MaxListLevels)
	style := ""
	for _, p := range c.Children {
		if p.Is(odf.NSText, "p") || p.Is(odf.NSText, "h") {
			style = b.styles.Inherited(b.scope, odf.FamilyParagraph, b.paraStyleName(p), func(s *odf.Style) (string, bool) {
				return s.ListStyle, s.ListStyle != ""
			})
			break
		}
	}
	item := &odf.Element{Name: xmlName(odf.NSText, "list-item"), Children: c.Children}
	for l := level; l > 1; l-- {
		item = &odf.Element{Name: xmlName(odf.NSText, "list-item"), Children: []*odf.Element{
			{Name: xmlName(odf.NSText, "list"), Children: []*odf.Element{item}},
		}}
	}
	list := &odf.Element{Name: xmlName(odf.NSText, "list"), Children: []*odf.Element{item}}
	list.Attr = append(list.Attr, xmlAttr(odf.NSText, "style-name", style), xmlAttr(odf.NSText, "continue-numbering", "true"))
	return b.list(list, ctx)
}

// number numbers the item it of the list lc and formats its label from
// its level (ODF 1.3, Part 3, 16.32 and 16.33): the level's counter goes
// up by one, or starts at the item's text:start-value, the deeper levels
// restart, and the label joins text:display-levels levels, each in its
// own format, with the level's prefix and suffix.
func (b *builder) number(item *itemCtx, lc *listCtx, it *odf.Element) {
	lvl := item.lvl
	cnt := lc.counter
	l := lc.level - 1
	start := levelStart(lvl)
	if v, err := strconv.Atoi(it.A(odf.NSText, "start-value")); err == nil && v >= 0 {
		cnt.n[l] = v
	} else if !cnt.used[l] {
		cnt.n[l] = start
	} else {
		cnt.n[l]++
	}
	cnt.used[l] = true
	for d := l + 1; d < odf.MaxListLevels; d++ {
		cnt.used[d] = false
	}
	item.value = cnt.n[l]
	switch lvl.Kind {
	case "bullet":
		item.label = lvl.El.A(odf.NSText, "bullet-char")
		if item.label == "" {
			item.label = "•"
		}
		item.label = lvl.El.A(odf.NSStyle, "num-prefix") + item.label + lvl.El.A(odf.NSStyle, "num-suffix")
	case "image":
		if href := lvl.El.A(odf.NSXLink, "href"); href != "" {
			item.pic = b.picture(href, nil)
		}
		if item.pic == nil {
			item.label = "•"
		}
	default:
		item.label = b.levelLabel(lc, l)
	}
}

// levelStart returns a level's text:start-value, 1 by default.
func levelStart(lvl *odf.ListLevel) int {
	if lvl == nil {
		return 1
	}
	if v, err := strconv.Atoi(lvl.El.A(odf.NSText, "start-value")); err == nil && v >= 0 {
		return v
	}
	return 1
}

// levelLabel formats the label of level l of the list lc.
func (b *builder) levelLabel(lc *listCtx, l int) string {
	lvl := lc.style.Levels[l]
	show := 1
	if v, err := strconv.Atoi(lvl.El.A(odf.NSText, "display-levels")); err == nil && v > 1 {
		show = min(v, l+1)
	}
	var parts []string
	for d := l - show + 1; d <= l; d++ {
		dl := lc.style.Levels[d]
		format := "1"
		if dl != nil {
			format = dl.El.A(odf.NSStyle, "num-format")
		}
		n := lc.counter.n[d]
		if !lc.counter.used[d] {
			n = levelStart(dl)
		}
		s := formatNumber(n, format)
		if dl != nil && (format == "a" || format == "A") && dl.El.A(odf.NSStyle, "num-letter-sync") == "true" && n > 0 {
			s = alphaSync(n)
			if format == "A" {
				s = strings.ToUpper(s)
			}
		}
		if s != "" || d == l {
			parts = append(parts, s)
		}
	}
	label := strings.Join(parts, ".")
	if label == "" && lvl.El.A(odf.NSStyle, "num-prefix") == "" && lvl.El.A(odf.NSStyle, "num-suffix") == "" {
		return ""
	}
	return lvl.El.A(odf.NSStyle, "num-prefix") + label + lvl.El.A(odf.NSStyle, "num-suffix")
}

// outlineLabel returns the label of a heading at level from the outline
// style (ODF 1.3, Part 3, 16.34), "" when the level is not numbered; the
// heading counters advance.
func (b *builder) outlineLabel(level int) string {
	os := b.styles.Outline
	if os == nil || level < 1 || level > odf.MaxListLevels || os.Levels[level-1] == nil {
		return ""
	}
	if b.lists == nil {
		b.lists = &listState{last: map[string]*listCounter{}, byID: map[string]*listCounter{}}
	}
	lc := &listCtx{style: os, level: level, counter: &b.lists.outline}
	item := &itemCtx{lvl: os.Levels[level-1]}
	b.number(item, lc, &odf.Element{})
	if item.label == "" {
		return ""
	}
	switch item.lvl.Props.Get("text:label-followed-by") {
	case "space":
		return item.label + " "
	case "nothing":
		return item.label
	}
	return item.label + "\t"
}

// indent lays the item's level indentation over the paragraph
// properties p of a paragraph of the item, whose own style is own:
// Writer applies the list level's indent, then the paragraph's
// automatic style's own indent when it sets one.
func (item *itemCtx) indent(p paraState, own *odf.Style) paraState {
	if item == nil || item.lvl == nil {
		return p
	}
	lp := item.lvl.Props
	if lp.Get("text:list-level-position-and-space-mode") == "label-alignment" {
		if ml, ok := odf.Length(lp.Get("fo:margin-left")); ok {
			p.margin[css.Left] = ml
		}
		if ti, ok := odf.Length(lp.Get("fo:text-indent")); ok {
			p.indent = ti
		}
	} else {
		sb, _ := odf.Length(lp.Get("text:space-before"))
		mlw, _ := odf.Length(lp.Get("text:min-label-width"))
		p.margin[css.Left] += sb + mlw
	}
	if own != nil && own.Auto {
		pp := own.Props(odf.ParagraphProps)
		if v, ok := odf.Length(pp.Get("fo:margin-left")); ok {
			p.margin[css.Left] = v
		}
		if v, ok := odf.Length(pp.Get("fo:text-indent")); ok {
			p.indent = v
		}
	}
	if item.placed || item.header {
		// An item's later paragraphs and a list header start at the
		// text position.
		if lp.Get("text:list-level-position-and-space-mode") == "label-alignment" {
			p.indent = 0
		}
	}
	return p
}

// attach makes the paragraph n, of text properties t, the labelled first
// paragraph of the item (ODF 1.3, Part 3, 17.20): a list item whose
// marker is the label, placed as the level's position mode says.
func (item *itemCtx) attach(b *builder, n *layout.Node, t textState) {
	item.placed = true
	if item.header || item.lvl == nil {
		return
	}
	lvl := item.lvl
	lt := b.foldText(t, lvl.Text)
	lt.under, lt.through, lt.over = decoLine{}, decoLine{}, decoLine{}
	if cs := lvl.El.A(odf.NSText, "style-name"); cs != "" {
		lt = b.spanText(lt, cs)
	}
	if lvl.Kind == "bullet" {
		if rs, ok := odf.Percent(lvl.El.A(odf.NSText, "bullet-relative-size")); ok && rs > 0 {
			lt.size *= rs
		}
	}
	mst := baseStyle()
	applyText(mst, lt, false)
	mst.Display = css.Inline
	st := *n.Style
	st.Display = css.ListItem
	n.Style = &st
	n.Role = "LI"
	n.ListValue = max(item.value, 1)
	marker := &layout.Node{Kind: layout.Element, Style: mst}
	label := item.label
	lp := lvl.Props
	align := css.AlignStart
	switch lp.Get("fo:text-align") {
	case "center":
		align = css.AlignCenter
	case "end", "right":
		align = css.AlignEnd
	}
	mst.TextAlign = align
	if lp.Get("text:list-level-position-and-space-mode") == "label-alignment" {
		switch lp.Get("text:label-followed-by") {
		case "space":
			st.ListStylePosition = css.Inside
			if label != "" {
				label += " "
			}
		case "nothing":
			st.ListStylePosition = css.Inside
		default:
			// listtab: the text starts at the list tab stop, else at the
			// paragraph's indent; the label sits from the first line's
			// start (margin plus text indent) in a box that reaches it.
			ml := st.Margin[css.Left].Value
			start := ml + st.TextIndent.Value
			stop := ml
			if v, ok := odf.Length(lp.Get("text:list-tab-stop-position")); ok && lp.Get("text:label-followed-by") == "listtab" {
				stop = v
			}
			if stop <= start {
				stop = ml
			}
			if end := start + b.textWidth(label, mst); end > stop {
				// A label past the list tab stop pushes the text to the
				// next default tab stop, counted from the indent, as
				// Writer does.
				dist := defaultTabDistance
				if st.TabSize.Kind == css.Points && st.TabSize.Value > 0 {
					dist = st.TabSize.Value
				}
				stop = ml + dist*math.Ceil((end-ml)/dist+1e-9)
			}
			if stop > ml {
				st.TextIndent = css.Pts(stop - ml)
			} else {
				st.TextIndent = css.Pts(0)
			}
			st.ListStylePosition = css.Outside
			mst.Width = css.Pts(max(stop-start, 0))
		}
	} else {
		// label-width-and-position: the label box is min-label-width wide
		// and ends where the text starts.
		mlw, _ := odf.Length(lp.Get("text:min-label-width"))
		st.ListStylePosition = css.Outside
		mst.Width = css.Pts(max(mlw, 0))
	}
	if item.pic != nil {
		pst := baseStyle()
		pst.Display = css.Inline
		if w, ok := odf.Length(lp.Get("fo:width")); ok && w > 0 {
			pst.Width = css.Pts(w)
		}
		if h, ok := odf.Length(lp.Get("fo:height")); ok && h > 0 {
			pst.Height = css.Pts(h)
		}
		marker.Children = []*layout.Node{{Kind: layout.Replaced, Style: pst, Picture: item.pic}}
		marker.Alt = "•"
	} else {
		marker.Children = []*layout.Node{{Kind: layout.Text, Style: mst, Text: label}}
	}
	n.Marker = marker
}

// xmlName returns the XML name space:local.
func xmlName(space, local string) xml.Name { return xml.Name{Space: space, Local: local} }

// xmlAttr returns the attribute space:local="value".
func xmlAttr(space, local, value string) xml.Attr {
	return xml.Attr{Name: xmlName(space, local), Value: value}
}
