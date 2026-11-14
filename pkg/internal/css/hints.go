package css

import (
	"fmt"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/text/unicode/bidi"
)

// HTMLHints returns the presentational hints of an HTML element (HTML
// Standard, 15.3 and 15.4): the legacy attributes that style it, as
// declarations the cascade places at the author origin with zero
// specificity, before the author's stylesheets (CSS 2.1, 6.4.4).
//
// Covered:
//
//   - body: bgcolor, text, marginwidth, marginheight, leftmargin,
//     rightmargin, topmargin, bottommargin; its link attribute colours the
//     document's :link elements.
//   - width and height on img, table, td, th, col, colgroup, tr (height),
//     hr (width), iframe, embed, object, video, canvas and input.
//   - align on div, p, h1 to h6, legend, caption, table, thead, tbody,
//     tfoot, tr, td, th, hr, img, object, embed, iframe and input; valign
//     on the table sections, rows and cells.
//   - bgcolor on table, thead, tbody, tfoot, tr, td, th and marquee.
//   - table: border (widths and outset style; the cells' 1px inset
//     borders come from the user agent stylesheet), frame, rules (the
//     table's collapsed hidden border, and the borders of its cells, row
//     groups, column groups and rows), cellspacing, and cellpadding
//     applied to its cells.
//   - td and th: nowrap.
//   - font: color, face, size (the legacy 1 to 7 and relative +n/-n
//     scale).
//   - img, object and embed: hspace, vspace, border.
//   - hr: color, noshade, size.
//   - br: clear. ol, ul and li: type. li: value, as counter-set of the
//     list-item counter.
//
// The dir and hidden attributes are user agent stylesheet rules, as the
// HTML Standard writes them, except dir=auto, whose direction comes from
// the element's text: a direction hint. vlink and alink colour nothing, since
// :visited and :active never match. The background attribute is not
// covered.
func HTMLHints(n *html.Node) []Declaration {
	if n != nil && n.Type == html.ElementNode && n.Namespace == "math" {
		return mathHints(n)
	}
	if n == nil || n.Type != html.ElementNode || n.Namespace != "" {
		return nil
	}
	var h hints
	switch n.Data {
	case "body":
		h.color(n, "bgcolor", "background-color")
		h.color(n, "text", "color")
		h.pixels(n, "marginheight", "margin-top", "margin-bottom")
		h.pixels(n, "topmargin", "margin-top")
		h.pixels(n, "bottommargin", "margin-bottom")
		h.pixels(n, "marginwidth", "margin-left", "margin-right")
		h.pixels(n, "leftmargin", "margin-left")
		h.pixels(n, "rightmargin", "margin-right")
	case "a", "area", "link":
		if hasAttr(n, "href") {
			if body := documentBody(n); body != nil {
				h.color(body, "link", "color")
			}
		}
	case "div", "p", "h1", "h2", "h3", "h4", "h5", "h6":
		h.textAlign(n)
	case "legend":
		h.legendAlign(n)
	case "caption":
		if v, ok := attr(n, "align"); ok {
			switch lower(strings.TrimSpace(v)) {
			case "top", "bottom":
				h.add("caption-side", lower(strings.TrimSpace(v)))
			default:
				h.textAlign(n)
			}
		}
	case "table":
		h.color(n, "bgcolor", "background-color")
		h.dimension(n, "width", "width", true)
		h.dimension(n, "height", "height", false)
		if v, ok := attr(n, "align"); ok {
			switch lower(strings.TrimSpace(v)) {
			case "left":
				h.add("float", "left")
			case "right":
				h.add("float", "right")
			case "center":
				h.add("margin-left", "auto")
				h.add("margin-right", "auto")
			}
		}
		if tableRules(n) != "" {
			h.add("border-style", "hidden")
			h.add("border-collapse", "collapse")
		}
		if v, ok := attr(n, "border"); ok {
			px, ok := nonNegativeInt(v)
			if !ok {
				px = 1
			}
			if px > 0 {
				h.add("border-width", fmt.Sprintf("%dpx", px))
				h.add("border-style", "outset")
			}
		}
		if v, ok := attr(n, "frame"); ok {
			if sides, ok := frameSides[lower(strings.TrimSpace(v))]; ok {
				px := 1
				if b, ok := attr(n, "border"); ok {
					if px, ok = nonNegativeInt(b); !ok {
						px = 1
					}
				}
				styles := make([]string, len(sides))
				for i, framed := range sides {
					styles[i] = "hidden"
					if framed {
						styles[i] = "solid"
					}
				}
				h.add("border-width", fmt.Sprintf("%dpx", px))
				h.add("border-style", strings.Join(styles, " "))
			}
		}
		if v, ok := attr(n, "cellspacing"); ok {
			if px, ok := nonNegativeInt(v); ok {
				h.add("border-spacing", fmt.Sprintf("%dpx", px))
			}
		}
	case "thead", "tbody", "tfoot", "tr", "td", "th":
		h.color(n, "bgcolor", "background-color")
		h.cellAlign(n)
		if v, ok := attr(n, "valign"); ok {
			switch k := lower(strings.TrimSpace(v)); k {
			case "top", "middle", "bottom", "baseline":
				h.add("vertical-align", k)
			}
		}
		if n.Data == "tr" {
			h.dimension(n, "height", "height", false)
			if tableRules(rowTable(n)) == "rows" {
				h.add("border-block-width", "1px")
				h.add("border-block-style", "solid")
			}
		} else if n.Data != "td" && n.Data != "th" && tableRules(parentTable(n)) == "groups" {
			h.add("border-block-width", "1px")
			h.add("border-block-style", "solid")
		}
		if n.Data == "td" || n.Data == "th" {
			h.dimension(n, "width", "width", true)
			h.dimension(n, "height", "height", true)
			if hasAttr(n, "nowrap") {
				h.add("white-space", "nowrap")
			}
			if t := cellTable(n); t != nil {
				if v, ok := attr(t, "cellpadding"); ok {
					if px, ok := nonNegativeInt(v); ok {
						h.add("padding", fmt.Sprintf("%dpx", px))
					}
				}
			}
			switch tableRules(rowTable(n.Parent)) {
			case "none", "groups", "rows":
				h.add("border-width", "1px")
				h.add("border-style", "none")
			case "cols":
				h.add("border-width", "1px")
				h.add("border-block-style", "none")
				h.add("border-inline-style", "solid")
			case "all":
				h.add("border-width", "1px")
				h.add("border-style", "solid")
			}
		}
	case "col", "colgroup":
		h.dimension(n, "width", "width", false)
		if n.Data == "colgroup" && tableRules(parentTable(n)) == "groups" {
			h.add("border-inline-width", "1px")
			h.add("border-inline-style", "solid")
		}
	case "img", "object", "embed", "iframe", "video", "canvas", "input":
		h.dimension(n, "width", "width", false)
		h.dimension(n, "height", "height", false)
		if n.Data != "video" && n.Data != "canvas" {
			h.replacedAlign(n)
		}
		if n.Data == "img" || n.Data == "object" || n.Data == "embed" {
			h.pixels(n, "hspace", "margin-left", "margin-right")
			h.pixels(n, "vspace", "margin-top", "margin-bottom")
		}
		if n.Data == "img" || n.Data == "object" {
			if v, ok := attr(n, "border"); ok {
				if px, ok := nonNegativeInt(v); ok {
					h.add("border-width", fmt.Sprintf("%dpx", px))
					h.add("border-style", "solid")
				}
			}
		}
	case "marquee":
		h.color(n, "bgcolor", "background-color")
	case "font":
		h.color(n, "color", "color")
		if v, ok := attr(n, "face"); ok && strings.TrimSpace(v) != "" {
			h.add("font-family", v)
		}
		if v, ok := attr(n, "size"); ok {
			if k, ok := legacyFontSize(v); ok {
				h.add("font-size", k)
			}
		}
	case "hr":
		h.dimension(n, "width", "width", false)
		if v, ok := attr(n, "align"); ok {
			switch lower(strings.TrimSpace(v)) {
			case "left":
				h.add("margin-left", "0")
				h.add("margin-right", "auto")
			case "right":
				h.add("margin-left", "auto")
				h.add("margin-right", "0")
			case "center":
				h.add("margin-left", "auto")
				h.add("margin-right", "auto")
			}
		}
		if c, ok := attr(n, "color"); ok {
			if col, ok := legacyColor(c); ok {
				h.add("border-color", col)
				h.add("background-color", col)
			}
		}
		if hasAttr(n, "noshade") {
			h.add("border-style", "solid")
			if !hasAttr(n, "color") {
				h.add("background-color", "gray")
				h.add("border-color", "gray")
			}
		}
		if v, ok := attr(n, "size"); ok {
			if px, ok := nonNegativeInt(v); ok && px > 0 {
				if px == 1 {
					h.add("border-bottom-width", "0")
				} else {
					h.add("height", fmt.Sprintf("%dpx", px-2))
				}
			}
		}
	case "br":
		if v, ok := attr(n, "clear"); ok {
			switch k := lower(strings.TrimSpace(v)); k {
			case "left", "right", "both":
				h.add("clear", k)
			case "all":
				h.add("clear", "both")
			}
		}
	case "ol", "ul", "li":
		if v, ok := attr(n, "type"); ok {
			if t, ok := listType(n.Data, strings.TrimSpace(v)); ok {
				h.add("list-style-type", t)
			}
		}
		// li[value] sets the list-item counter (HTML Standard, 4.4.8;
		// CSS Lists 3, appendix A): an author's counter-set replaces it.
		if v, ok := attr(n, "value"); ok && n.Data == "li" {
			if i, ok := htmlInteger(v); ok {
				h.add("counter-set", fmt.Sprintf("list-item %d", i))
			}
		}
	}
	// A bdi element without a valid dir attribute takes its direction from
	// its text, as dir=auto does (HTML Standard, 4.5.24 and 3.2.6.4).
	if v, ok := attr(n, "dir"); ok && lower(strings.TrimSpace(v)) == "auto" || n.Data == "bdi" && !validDir(n) {
		if d, ok := autoDirection(n); ok {
			h.add("direction", d)
		}
	}
	return h.list
}

// htmlInteger parses an integer by the HTML Standard's rules for parsing
// integers (2.3.4.1): leading white space, a sign, digits, anything after
// them ignored; false for no digit. The value is clamped to [MinInteger,
// MaxInteger], as an <integer> is.
func htmlInteger(s string) (int, bool) {
	s = strings.TrimLeft(s, "\t\n\f\r ")
	neg := false
	if s != "" && (s[0] == '-' || s[0] == '+') {
		neg = s[0] == '-'
		s = s[1:]
	}
	var v int64
	digits := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			break
		}
		// Past MaxInteger + 1 the value only grows: capping it there
		// keeps the product inside int64.
		v = min(v*10+int64(r-'0'), MaxInteger+1)
		digits++
	}
	if digits == 0 {
		return 0, false
	}
	if neg {
		v = -v
	}
	return ClampInteger(v), true
}

// maxDirAutoRunes bounds the characters dir=auto reads looking for a
// strong one. Anti-DoS guard, not spec-mandated: nested dir=auto elements
// would otherwise read their text once per level; a paragraph's first
// strong character comes well within 4,096 characters.
const maxDirAutoRunes = 4096

// autoDirection returns the directionality of the dir=auto element n
// (HTML Standard, 3.2.6.4): rtl when the first character of bidirectional
// class L, R or AL in its text, skipping bdi, script, style and textarea
// elements and the elements with a dir attribute of their own, is R or
// AL, ltr when it is L, and false when there is none.
//
// The walk keeps its own stack of the siblings left to read, so a deep
// tree costs memory, not goroutine stack.
func autoDirection(n *html.Node) (string, bool) {
	budget := maxDirAutoRunes
	var rest []*html.Node
	for c := n.FirstChild; c != nil || len(rest) > 0; {
		if c == nil {
			c, rest = rest[len(rest)-1], rest[:len(rest)-1]
			continue
		}
		if budget <= 0 {
			return "", false
		}
		next := c.NextSibling
		switch c.Type { //nolint:exhaustive // only text and elements carry characters
		case html.TextNode:
			for _, r := range c.Data {
				budget--
				if budget < 0 {
					return "", false
				}
				p, _ := bidi.LookupRune(r)
				switch p.Class() { //nolint:exhaustive // the weak and neutral classes are skipped
				case bidi.L:
					return "ltr", true
				case bidi.R, bidi.AL:
					return "rtl", true
				}
			}
		case html.ElementNode:
			if c.Namespace != "" || c.Data == "bdi" || c.Data == "script" || c.Data == "style" || c.Data == "textarea" || hasAttr(c, "dir") {
				break
			}
			if c.FirstChild != nil {
				rest = append(rest, next)
				next = c.FirstChild
			}
		}
		c = next
	}
	return "", false
}

// hints accumulates declarations.
type hints struct{ list []Declaration }

func (h *hints) add(prop, value string) {
	if d := Hint(prop, value); d.declares() {
		h.list = append(h.list, d)
	}
}

// color maps a legacy colour attribute onto props.
func (h *hints) color(n *html.Node, name string, props ...string) {
	v, ok := attr(n, name)
	if !ok {
		return
	}
	c, ok := legacyColor(v)
	if !ok {
		return
	}
	for _, p := range props {
		h.add(p, c)
	}
}

// pixels maps a non-negative integer attribute onto pixel lengths.
func (h *hints) pixels(n *html.Node, name string, props ...string) {
	v, ok := attr(n, name)
	if !ok {
		return
	}
	px, ok := nonNegativeInt(v)
	if !ok {
		return
	}
	for _, p := range props {
		h.add(p, fmt.Sprintf("%dpx", px))
	}
}

// dimension maps a dimension attribute onto prop (HTML, 2.3.4.4); nonZero
// ignores a zero value, as the rules for parsing non-zero dimension values
// ask.
func (h *hints) dimension(n *html.Node, name, prop string, nonZero bool) {
	v, ok := attr(n, name)
	if !ok {
		return
	}
	value, pct, ok := parseDimensionValue(v)
	if !ok || (nonZero && value == 0) {
		return
	}
	unit := "px"
	if pct {
		unit = "%"
	}
	h.add(prop, fmt.Sprintf("%g%s", value, unit))
}

// textAlign maps align on a block (HTML, 15.3.3).
func (h *hints) textAlign(n *html.Node) {
	v, ok := attr(n, "align")
	if !ok {
		return
	}
	switch k := lower(strings.TrimSpace(v)); k {
	case "left", "right", "center", "justify":
		h.add("text-align", k)
	case "middle":
		h.add("text-align", "center")
	}
}

// legendAlign maps align on a legend to text-align, which places a
// rendered legend in its fieldset's border as Chromium places it. The
// HTML Standard maps the attribute to justify-self (15.3.13), which
// Chromium does not lay out on a legend; an author's text-align overrides
// the attribute, as it overrides a presentational hint.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 places a legend
//     of align=center in the middle of its fieldset's border, one of
//     align=right with text-align: left at the start, one of
//     justify-self: center or end at the start, on screen.
func (h *hints) legendAlign(n *html.Node) {
	h.textAlign(n)
}

// cellAlign maps align on table parts (HTML, 15.3.10).
func (h *hints) cellAlign(n *html.Node) {
	v, ok := attr(n, "align")
	if !ok {
		return
	}
	switch k := lower(strings.TrimSpace(v)); k {
	case "left", "right", "center", "justify":
		h.add("text-align", k)
	case "middle", "absmiddle":
		h.add("text-align", "center")
	}
}

// replacedAlign maps align on an embedded element (HTML, 15.4.3).
func (h *hints) replacedAlign(n *html.Node) {
	v, ok := attr(n, "align")
	if !ok {
		return
	}
	switch lower(strings.TrimSpace(v)) {
	case "left":
		h.add("float", "left")
	case "right":
		h.add("float", "right")
	case "top":
		h.add("vertical-align", "top")
	case "middle", "absmiddle", "absbottom", "center":
		h.add("vertical-align", "middle")
	case "bottom", "baseline":
		h.add("vertical-align", "baseline")
	}
}

// documentBody returns the body element of n's document.
func documentBody(n *html.Node) *html.Node {
	top := n
	for top.Parent != nil {
		top = top.Parent
	}
	for h := top.FirstChild; h != nil; h = h.NextSibling {
		if !isHTML(h, "html") {
			continue
		}
		for b := h.FirstChild; b != nil; b = b.NextSibling {
			if isHTML(b, "body") {
				return b
			}
		}
	}
	return nil
}

// cellTable returns the table a cell belongs to, three levels up at most:
// the row, the row group and the table.
// frameSides maps a table's frame attribute to the sides it draws, top,
// right, bottom and left, the others hidden (HTML Standard, 15.3.8). The Standard draws them
// outset, as wide as the border attribute or medium; litho follows
// Chromium, which draws them solid, as wide as the border attribute or 1
// pixel. Immutable lookup table.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 draws frame=box
//     as a 1 pixel solid black border, frame=box border=5 as a 5 pixel
//     one, frame=hsides as 1 pixel solid top and bottom borders, and no
//     border for frame=box border=0 or frame=void border=5.
var frameSides = map[string][4]bool{
	"void": {}, "above": {true, false, false, false}, "below": {false, false, true, false},
	"hsides": {true, false, true, false}, "lhs": {false, false, false, true}, "rhs": {false, true, false, false},
	"vsides": {false, true, false, true}, "box": {true, true, true, true}, "border": {true, true, true, true},
}

// tableRules returns the rules attribute of the table t, lowered, when it
// is one of the values the HTML Standard, 15.3.8, styles (none, groups,
// rows, cols, all), and "" otherwise or for a nil t.
func tableRules(t *html.Node) string {
	if t == nil {
		return ""
	}
	v, _ := attr(t, "rules")
	switch v = lower(strings.TrimSpace(v)); v {
	case "none", "groups", "rows", "cols", "all":
		return v
	}
	return ""
}

// parentTable returns the parent of n when it is a table, nil otherwise.
func parentTable(n *html.Node) *html.Node {
	if p := n.Parent; p != nil && isHTML(p, "table") {
		return p
	}
	return nil
}

// rowTable returns the table of the row tr as the rules selectors of the
// HTML Standard, 15.3.8, reach it: its parent, or the parent of its
// thead, tbody or tfoot; nil for any other place or a nil tr.
func rowTable(tr *html.Node) *html.Node {
	if tr == nil || !isHTML(tr, "tr") {
		return nil
	}
	if t := parentTable(tr); t != nil {
		return t
	}
	if g := tr.Parent; g != nil && (isHTML(g, "thead") || isHTML(g, "tbody") || isHTML(g, "tfoot")) {
		return parentTable(g)
	}
	return nil
}

func cellTable(n *html.Node) *html.Node {
	p := n.Parent
	for i := 0; i < 3 && p != nil; i++ {
		if isHTML(p, "table") {
			return p
		}
		p = p.Parent
	}
	return nil
}

func isASCIIWS(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\f' || c == '\r' }

// maxHintInt caps the integers presentational hints read: 10^7, a page
// many kilometres wide. Anti-DoS guard, not spec-mandated: it keeps the
// formatted lengths short.
const maxHintInt = 10_000_000

// nonNegativeInt implements the rules for parsing non-negative integers
// (HTML, 2.3.4.2): leading whitespace, an optional plus, digits, anything
// after ignored.
func nonNegativeInt(s string) (int, bool) {
	i := 0
	for i < len(s) && isASCIIWS(s[i]) {
		i++
	}
	if i < len(s) && s[i] == '+' {
		i++
	}
	start := i
	n := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		if n < maxHintInt {
			n = n*10 + int(s[i]-'0')
		}
		i++
	}
	return min(n, maxHintInt), i > start
}

// parseDimensionValue implements the rules for parsing dimension values
// (HTML, 2.3.4.4): a number of pixels, or a percentage.
func parseDimensionValue(s string) (float64, bool, bool) {
	i := 0
	for i < len(s) && isASCIIWS(s[i]) {
		i++
	}
	start := i
	v := 0.0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		if v < maxHintInt {
			v = float64(v*10) + float64(s[i]-'0')
		}
		i++
	}
	if i == start {
		return 0, false, false
	}
	if i < len(s) && s[i] == '.' {
		i++
		scale := 0.1
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			v += float64(float64(s[i]-'0') * scale)
			scale /= 10
			i++
		}
	}
	v = min(v, maxHintInt)
	return v, i < len(s) && s[i] == '%', true
}

// legacyFontSizes are the absolute-size keywords of font size 1 to 7
// (HTML, 15.3.4).
var legacyFontSizes = [8]string{"", "x-small", "small", "medium", "large", "x-large", "xx-large", "xxx-large"}

// legacyFontSize implements the rules for parsing a legacy font size
// (HTML, 15.3.4).
func legacyFontSize(s string) (string, bool) {
	i := 0
	for i < len(s) && isASCIIWS(s[i]) {
		i++
	}
	mode := byte(0)
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		mode = s[i]
		i++
	}
	n, ok := nonNegativeInt(s[i:])
	if !ok || (i < len(s) && isASCIIWS(s[i])) {
		return "", false
	}
	switch mode {
	case '+':
		n += 3
	case '-':
		n = 3 - n
	}
	return legacyFontSizes[max(1, min(7, n))], true
}

// listType maps the type attribute of ol, ul and li (HTML, 15.3.7): the
// ordered types are case-sensitive, the unordered ones are not.
func listType(tag, v string) (string, bool) {
	switch v {
	case "1":
		return "decimal", tag != "ul"
	case "a":
		return "lower-alpha", tag != "ul"
	case "A":
		return "upper-alpha", tag != "ul"
	case "i":
		return "lower-roman", tag != "ul"
	case "I":
		return "upper-roman", tag != "ul"
	}
	switch k := lower(v); k {
	case "disc", "circle", "square", "none":
		return k, tag != "ol"
	}
	return "", false
}

// legacyColor implements the rules for parsing a legacy colour value
// (HTML, 2.3.6.4), returning a CSS hex colour.
func legacyColor(input string) (string, bool) {
	s := strings.Trim(input, " \t\n\f\r")
	if s == "" || eqFold(s, "transparent") {
		return "", false
	}
	if rgb, ok := namedColors[lower(s)]; ok {
		return fmt.Sprintf("#%02x%02x%02x", rgb[0], rgb[1], rgb[2]), true
	}
	if len(s) == 4 && s[0] == '#' && isHexDigit(rune(s[1])) && isHexDigit(rune(s[2])) && isHexDigit(rune(s[3])) {
		return s, true
	}
	var b strings.Builder
	for _, r := range s {
		if r > 0xFFFF {
			b.WriteString("00")
		} else {
			b.WriteRune(r)
		}
	}
	runes := []rune(b.String())
	if len(runes) > 128 {
		runes = runes[:128]
	}
	if len(runes) > 0 && runes[0] == '#' {
		runes = runes[1:]
	}
	for i, r := range runes {
		if !isHexDigit(r) {
			runes[i] = '0'
		}
	}
	for len(runes) == 0 || len(runes)%3 != 0 {
		runes = append(runes, '0')
	}
	n := len(runes) / 3
	parts := [3][]rune{runes[:n], runes[n : 2*n], runes[2*n:]}
	if n > 8 {
		for i := range parts {
			parts[i] = parts[i][n-8:]
		}
		n = 8
	}
	for n > 2 && parts[0][0] == '0' && parts[1][0] == '0' && parts[2][0] == '0' {
		for i := range parts {
			parts[i] = parts[i][1:]
		}
		n--
	}
	if n > 2 {
		for i := range parts {
			parts[i] = parts[i][:2]
		}
	}
	var out strings.Builder
	out.WriteByte('#')
	for _, p := range parts {
		if len(p) == 1 {
			out.WriteByte('0')
		}
		out.WriteString(string(p))
	}
	return out.String(), true
}

// validDir reports an element whose dir attribute is ltr, rtl or auto.
func validDir(n *html.Node) bool {
	v, _ := attr(n, "dir")
	switch lower(strings.TrimSpace(v)) {
	case "ltr", "rtl", "auto":
		return true
	}
	return false
}
