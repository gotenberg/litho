package odt

import (
	"encoding/xml"
	"math"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/intconv"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/odf"
	"github.com/gotenberg/litho/pkg/internal/symbolfont"
	"github.com/gotenberg/litho/pkg/internal/typeset"
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
	// style names the item's list style.
	style string
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
		if lc.style != nil {
			item.style = lc.style.Name
		}
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
	if len(out) > 0 && len(b.listNums) < maxLists {
		if b.listPath == nil {
			b.listPath = map[*layout.Node][]int{}
		}
		id := len(b.listNums)
		b.listNums = append(b.listNums, listNumbering(lc.style, lc.level))
		for _, n := range out {
			b.listPath[n] = append([]int{id}, b.listPath[n]...)
		}
	}
	return out
}

// maxLists caps the lists tagged as L elements; the blocks of the lists
// past it hang from their parent's element. Anti-DoS guard, not
// spec-mandated: a long text document holds a few thousand lists.
const maxLists = 1 << 16

// listNumbering returns the numbering system of the labels of level, from
// 1, of the list style ls (ODF 1.3 Part 3, 16.32: its bullets, pictures
// or style:num-format): the ListNumbering of its L element (ISO 32000-1,
// 14.8.5.5, Table 347; ISO 32000-2, Table 382). A bullet is a disc, a
// circle or a square by its character, any other bullet or a picture is
// unordered, a number format Table 347 has no name for is ordered, a
// level without a number whose prefix or suffix still prints is
// unordered, and a level without a label is none.
func listNumbering(ls *odf.ListStyle, level int) typeset.ListNumbering {
	if ls == nil || level < 1 || level > odf.MaxListLevels || ls.Levels[level-1] == nil {
		return typeset.NumberingNone
	}
	lvl := ls.Levels[level-1]
	switch lvl.Kind {
	case "bullet":
		switch lvl.El.A(odf.NSText, "bullet-char") {
		case "", "\u2022", "\u25cf":
			return typeset.NumberingDisc
		case "\u25cb", "\u25e6":
			return typeset.NumberingCircle
		case "\u25a0", "\u25aa":
			return typeset.NumberingSquare
		}
		return typeset.NumberingUnordered
	case "image":
		return typeset.NumberingUnordered
	}
	switch lvl.El.A(odf.NSStyle, "num-format") {
	case "1":
		return typeset.NumberingDecimal
	case "I":
		return typeset.NumberingUpperRoman
	case "i":
		return typeset.NumberingLowerRoman
	case "A":
		return typeset.NumberingUpperAlpha
	case "a":
		return typeset.NumberingLowerAlpha
	case "":
		if lvl.El.A(odf.NSStyle, "num-prefix") != "" || lvl.El.A(odf.NSStyle, "num-suffix") != "" {
			// A level without a number still prints its prefix and
			// suffix as a label: a Lbl, which ISO 14289-2, 8.2.5.25 asks
			// a numbering other than None of. It marks the items without
			// ordering them.
			return typeset.NumberingUnordered
		}
		return typeset.NumberingNone
	}
	return typeset.NumberingOrdered
}

// numberedParagraph builds a text:numbered-paragraph (ODF 1.3, Part 3,
// 5.3.6): its paragraph labelled at its text:level of the list its
// paragraph style's list style gives, as the same paragraph nested in
// that many lists would be.
func (b *builder) numberedParagraph(c *odf.Element, ctx *blockCtx) []*layout.Node {
	level, err := intconv.Atoi(c.A(odf.NSText, "level"))
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
	if v, err := intconv.Atoi(it.A(odf.NSText, "start-value")); err == nil && v >= 0 {
		cnt.n[l] = min(v, intconv.MaxCountStart)
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
		// Writer prints a bullet level's character alone: the
		// style:num-prefix and style:num-suffix the schema allows on it,
		// and its loext:num-list-format, are not printed ([bulletLabel]).
		item.label = bulletLabel(lvl)
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

// bulletLabel returns the label of a bullet level: its text:bullet-char,
// a bullet when it names none.
//
// # Corpus evidence
//
//   - libreoffice/odt odfexport/tdf146264.odt: a bullet level of
//     style:num-suffix "." and loext:num-list-format "%1%." prints "•"
//     alone.
//   - probe, odt: through gotenberg/gotenberg:9.4.1, bullet levels of
//     num-prefix "(" and num-suffix ")", of num-list-format "[%1%]" with
//     the prefix "[" and suffix "]", and of num-list-format "%1%" with the
//     suffix ".", each print "•" alone.
func bulletLabel(lvl *odf.ListLevel) string {
	if c := lvl.El.A(odf.NSText, "bullet-char"); c != "" {
		return c
	}
	return "•"
}

// levelStart returns a level's text:start-value, 1 by default.
func levelStart(lvl *odf.ListLevel) int {
	if lvl == nil {
		return 1
	}
	if v, err := intconv.Atoi(lvl.El.A(odf.NSText, "start-value")); err == nil && v >= 0 {
		return min(v, intconv.MaxCountStart)
	}
	return 1
}

// levelLabel formats the label of level l of the list lc. A level of
// loext:is-legal true, LibreOffice's extension for Word's legal
// numbering (ECMA-376 Part 1, 17.9.4 isLgl), writes every number its
// label shows in Arabic numerals, its own included: a format of other
// numerals becomes "1", and the Arabic ones, zero-padded or not, stay
// ([arabicFormat]).
//
// # Corpus evidence
//
//   - libreoffice/odt odfexport/IsLegal.fodt: Writer labels the legal
//     second outline level, of format "01, 02, 03, ...", "Sect 1.01" under
//     a first level of format I.
//   - probe, odt: through gotenberg/gotenberg:9.4.1, a legal outline level
//     of format I under a level of format A prints "S 2.1", the level
//     below it "B.II.a", and a legal list level of format i under one of
//     format A "2.1".
func (b *builder) levelLabel(lc *listCtx, l int) string {
	lvl := lc.style.Levels[l]
	legal := lvl.El.A(odf.NSLOExt, "is-legal") == "true"
	show := 1
	if v, err := intconv.Atoi(lvl.El.A(odf.NSText, "display-levels")); err == nil && v > 1 {
		show = min(v, l+1)
	}
	var parts []string
	for d := l - show + 1; d <= l; d++ {
		dl := lc.style.Levels[d]
		format := "1"
		if dl != nil {
			format = dl.El.A(odf.NSStyle, "num-format")
		}
		if legal && !arabicFormat(format) {
			format = "1"
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

// arabicFormat reports a style:num-format that writes Arabic numerals:
// "1" and LibreOffice's zero-padded "01, 02, 03, ..." family.
func arabicFormat(format string) bool {
	first, _, _ := strings.Cut(format, ", ")
	return format == "1" || strings.Trim(first, "0") == "1" && strings.HasPrefix(first, "0")
}

// outlineLabel returns the label of a heading at level from the outline
// style (ODF 1.3, Part 3, 16.34), "" when the level is not numbered; the
// heading counters advance. The label ends with what follows it: a tab,
// a space, nothing, or for loext:label-followed-by newline, LibreOffice's
// extension of text:label-followed-by, a line feed, the heading's text
// then starting on the next line.
//
// # Corpus evidence
//
//   - libreoffice/odt odfexport/chapter-number-new-line.odt: Writer prints
//     the number "1" of a heading whose outline level says newline on a
//     line of its own, the heading's text on the line below.
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
	if item.lvl.Props.Get("loext:label-followed-by") == "newline" {
		return item.label + "\n"
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
// properties p of a paragraph of the item, whose style chain is chain,
// its own style last: Writer applies the list level's indent, then the
// paragraph's automatic style's own indent when it sets one. A paragraph
// whose list style comes from its common style keeps the indent of that
// style, or of a style it derives from, set at or below the style that
// names the list style ([levelIndents]).
func (item *itemCtx) indent(p paraState, chain []*odf.Style) paraState {
	if item == nil || item.lvl == nil {
		return p
	}
	var own *odf.Style
	if len(chain) > 0 {
		own = chain[len(chain)-1]
	}
	lp := item.lvl.Props
	switch {
	case !levelIndents(chain, item.style):
		// The paragraph style's indent stands.
	case lp.Get("text:list-level-position-and-space-mode") == "label-alignment":
		if ml, ok := odf.Length(lp.Get("fo:margin-left")); ok {
			p.margin[css.Left] = ml
		}
		if ti, ok := odf.Length(lp.Get("fo:text-indent")); ok {
			p.indent = ti
		}
	default:
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

// levelIndents reports whether the list level's indent applies to a
// paragraph of a list item of the list style list whose paragraph style
// chain is chain, its own style last: when its styles name no list style
// or another one, the list's being the paragraph's own then, when its
// automatic style names one, or when, walking its common styles from the
// nearest, a style naming a list style comes before one setting an indent
// (fo:margin-left or fo:text-indent), the indent of a style counting
// before its list style; a common style that sets an indent first keeps
// it, as Writer decides
// whether a list level's indents apply (SwTextNode::
// AreListLevelIndentsApplicable). ODF 1.3 Part 3, 19.880 leaves it to the
// application.
//
// # Corpus evidence
//
//   - libreoffice/odt: odfexport/tdf114287.odt prints the items of a list
//     whose paragraph style sets both the list style and a 50 mm indent
//     at that indent, and those of one whose automatic style names the
//     list style over a common style of a 50 mm indent at the list
//     level's 13.3 mm.
func levelIndents(chain []*odf.Style, list string) bool {
	named := ""
	for i := len(chain) - 1; i >= 0 && named == ""; i-- {
		named = chain[i].ListStyle
	}
	if named != list {
		return true
	}
	for i := len(chain) - 1; i >= 0; i-- {
		st := chain[i]
		if st.Auto {
			if st.ListStyle != "" {
				return true
			}
			continue
		}
		if pp := st.Props(odf.ParagraphProps); pp.Has("fo:margin-left") || pp.Has("fo:text-indent") {
			return false
		}
		if st.ListStyle != "" {
			return true
		}
	}
	return true
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
	lt := b.foldText(t, lvl.Text, percentOfBelow)
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
	// A bullet of a symbol font draws as LibreOffice draws it, recoded
	// into OpenSymbol where the font is missing.
	sym := &layout.Node{Kind: layout.Text, Style: mst, Text: item.label}
	office.SymbolNode(sym, b.fonts, symbolfont.RuleLibreOffice, b.symbols)
	mst.FontFamilies = sym.Style.FontFamilies
	sym.Style = mst
	label := sym.Text
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
			// The start margin of a right-to-left paragraph is its
			// right one.
			start := css.Left
			if st.Direction == css.RTL {
				start = css.Right
			}
			ml := st.Margin[start].Value
			first := ml + st.TextIndent.Value
			stop := ml
			if v, ok := odf.Length(lp.Get("text:list-tab-stop-position")); ok && lp.Get("text:label-followed-by") == "listtab" {
				stop = v
			}
			if stop <= first {
				stop = ml
			}
			if end := first + b.textWidth(label, mst); end > stop {
				// A label past the list tab stop pushes the text to the
				// next default tab stop, counted from the indent, as
				// Writer does.
				dist := defaultTabDistance
				if st.TabSize.Kind == css.Points && st.TabSize.Value > 0 {
					dist = st.TabSize.Value
				}
				stop = ml + float64(dist*math.Ceil((end-ml)/dist+1e-9))
			}
			if stop > ml {
				st.TextIndent = css.Pts(stop - ml)
			} else {
				st.TextIndent = css.Pts(0)
			}
			st.ListStylePosition = css.Outside
			mst.Width = css.Pts(max(stop-first, 0))
			if stop > ml && align == css.AlignStart {
				// The label box ends on the content edge: a label that
				// starts before it begins at the first line's start, and
				// one at or past it begins on it, the text indent moving
				// the text on, as Writer sets a label wider than the gap.
				mst.Width = css.Pts(max(ml-first, 0))
			}
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
		if sym.ActualText != "" {
			// A marker's text nodes are drawn as one label: the marker
			// carries what they stand for (ISO 19005-2, 6.2.11.7.3).
			marker.Alt = strings.TrimSuffix(sym.ActualText+strings.TrimPrefix(label, sym.Text), " ")
		}
		sym.Text = label
		marker.Children = []*layout.Node{sym}
	}
	n.Marker = marker
}

// xmlName returns the XML name space:local.
func xmlName(space, local string) xml.Name { return xml.Name{Space: space, Local: local} }

// xmlAttr returns the attribute space:local="value".
func xmlAttr(space, local, value string) xml.Attr {
	return xml.Attr{Name: xmlName(space, local), Value: value}
}
