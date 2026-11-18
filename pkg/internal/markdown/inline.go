package markdown

import (
	"math"
	"sort"
	"strings"
)

// mark is the Markdown delimiter pair of an inline role.
type mark struct {
	open, close string
	// code marks a code span: its text is written as it reads, unescaped,
	// and holds no other mark (CommonMark, 6.1).
	code bool
}

// span is a run of the glyphs of a block that a mark wraps: [from, to) in
// the glyphs of the run.
type span struct {
	from, to int
	mark     mark
}

// interval is a span placed in the text of the block, [start, end) in bytes.
type interval struct {
	start, end int
	mark       mark
}

// inline renders the content of n as one line of Markdown, and returns the
// page of its first glyph, -1 when it reads as no text.
func (w *writer) inline(n *node) (string, int) {
	var refs []glyphRef
	var spans []span
	var walk func(n *node)
	walk = func(n *node) {
		from := len(refs)
		for _, g := range n.glyphs {
			if !w.used[g.page][g.index] {
				w.used[g.page][g.index] = true
				refs = append(refs, g)
			}
		}
		for _, kid := range n.kids {
			walk(kid)
		}
		if m, ok := inlineMarks[n.role]; ok && len(refs) > from {
			spans = append(spans, span{from: from, to: len(refs), mark: m})
		}
	}
	walk(n)
	if len(refs) == 0 {
		return "", -1
	}
	spans = append(spans, w.linkSpans(refs)...)

	text := w.text(refs)
	if text == "" {
		return "", -1
	}
	sort.SliceStable(spans, func(a, b int) bool {
		if spans[a].from != spans[b].from {
			return spans[a].from < spans[b].from
		}
		return spans[a].to > spans[b].to
	})
	var placed []interval
	cursor := 0
	for _, s := range spans {
		part := w.text(refs[s.from:s.to])
		if part == "" {
			continue
		}
		at := strings.Index(text[cursor:], part)
		if at < 0 {
			continue
		}
		iv := interval{start: cursor + at, end: cursor + at + len(part), mark: s.mark}
		cursor = iv.start
		if nests(placed, iv) {
			placed = append(placed, iv)
		}
	}
	return render(text, placed), refs[0].page
}

// nests reports whether iv either holds or sits within each interval of
// placed it overlaps: Markdown delimiters nest but cannot cross, and a code
// span holds text alone (CommonMark, 6.1).
func nests(placed []interval, iv interval) bool {
	for _, p := range placed {
		if iv.start >= p.end || p.start >= iv.end {
			continue
		}
		inside := iv.start >= p.start && iv.end <= p.end
		outside := iv.start <= p.start && iv.end >= p.end
		switch {
		case !inside && !outside:
			return false
		case inside && p.mark.code, outside && iv.mark.code:
			return false
		}
	}
	return true
}

// render writes text with the marks of placed around their intervals,
// escaping the text outside code spans.
func render(text string, placed []interval) string {
	type edge struct {
		at    int
		open  bool
		depth int // the interval's length, so outer marks open first and close last
		order int // the interval's place, which breaks a tie of depth
		mark  mark
	}
	edges := make([]edge, 0, 2*len(placed))
	for i, iv := range placed {
		edges = append(edges,
			edge{at: iv.start, open: true, depth: iv.end - iv.start, order: i, mark: iv.mark},
			edge{at: iv.end, open: false, depth: iv.end - iv.start, order: i, mark: iv.mark})
	}
	sort.SliceStable(edges, func(a, b int) bool {
		ea, eb := edges[a], edges[b]
		if ea.at != eb.at {
			return ea.at < eb.at
		}
		if ea.open != eb.open {
			return !ea.open // close the marks ending here before opening others
		}
		if ea.depth != eb.depth {
			return ea.open == (ea.depth > eb.depth)
		}
		return ea.open == (ea.order < eb.order)
	})
	var sb strings.Builder
	pos, inCode := 0, 0
	write := func(to int) {
		if inCode > 0 {
			sb.WriteString(text[pos:to])
		} else {
			sb.WriteString(escapeInline(text[pos:to]))
		}
		pos = to
	}
	for _, e := range edges {
		write(e.at)
		if e.open {
			sb.WriteString(e.mark.open)
			if e.mark.code {
				inCode++
			}
			continue
		}
		sb.WriteString(e.mark.close)
		if e.mark.code {
			inCode--
		}
	}
	write(len(text))
	return sb.String()
}

// text assembles refs, in order, into one line: the glyphs of each page
// assembled as text extraction assembles them, line breaks read as spaces.
func (w *writer) text(refs []glyphRef) string {
	var parts []string
	for i := 0; i < len(refs); {
		j := i
		var indices []int
		for ; j < len(refs) && refs[j].page == refs[i].page; j++ {
			indices = append(indices, refs[j].index)
		}
		if t := collapse(w.pages[refs[i].page].Shown.TextOf(w.ctx, indices)); t != "" {
			parts = append(parts, t)
		}
		i = j
	}
	return strings.Join(parts, " ")
}

// linkSpans returns the runs of refs whose glyphs sit inside a link to a URI.
func (w *writer) linkSpans(refs []glyphRef) []span {
	var spans []span
	for i := 0; i < len(refs); {
		link, ok := w.linkAt(refs[i])
		if !ok {
			i++
			continue
		}
		j := i + 1
		for j < len(refs) {
			next, ok := w.linkAt(refs[j])
			if !ok || next != link {
				break
			}
			j++
		}
		spans = append(spans, span{from: i, to: j, mark: mark{open: "[", close: "](" + linkDestination(link.URI) + ")"}})
		i = j
	}
	return spans
}

// linkAt returns the link whose rectangle holds the middle of the glyph g.
func (w *writer) linkAt(g glyphRef) (Link, bool) {
	page := w.pages[g.page]
	if len(page.Links) == 0 {
		return Link{}, false
	}
	el := page.Shown.Elements[g.index]
	x := el.X + math.Cos(el.Angle)*el.Width/2
	y := el.Y + math.Sin(el.Angle)*el.Width/2 + math.Abs(el.FontSize)*0.3
	for _, link := range page.Links {
		r := link.Rect
		x0, x1 := min(r[0], r[2]), max(r[0], r[2])
		y0, y1 := min(r[1], r[3]), max(r[1], r[3])
		if x >= x0 && x <= x1 && y >= y0 && y <= y1 {
			return link, true
		}
	}
	return Link{}, false
}

// linkDestination writes uri as a link destination (CommonMark, 6.3): in
// angle brackets when it holds a space, a parenthesis or a control
// character.
func linkDestination(uri string) string {
	if !strings.ContainsFunc(uri, func(r rune) bool { return r <= ' ' || r == '(' || r == ')' || r == '<' || r == '>' }) {
		return uri
	}
	return "<" + strings.NewReplacer("<", "%3C", ">", "%3E", "\n", "%0A", "\r", "%0D").Replace(uri) + ">"
}

// collapse reads every run of white space in s as one space, and trims it.
func collapse(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// inlineEscaper escapes the characters that open or close Markdown inline
// constructs (CommonMark, 2.4).
var inlineEscaper = strings.NewReplacer(
	`\`, `\\`, "`", "\\`", `*`, `\*`, `_`, `\_`, `[`, `\[`, `]`, `\]`, `<`, `\<`,
)

func escapeInline(s string) string {
	return inlineEscaper.Replace(s)
}

// escapeLineStart escapes what would make the start of a paragraph's text
// read as another block: a heading, a block quote, a list item, a thematic
// break or a fenced code block (CommonMark, 4 and 5).
func escapeLineStart(s string) string {
	if s == "" {
		return s
	}
	switch s[0] {
	case '#', '>', '=', '~', '|':
		return `\` + s
	case '-', '+':
		if len(s) == 1 || s[1] == ' ' || s[1] == s[0] {
			return `\` + s
		}
	}
	i := 0
	for i < len(s) && i < 9 && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i > 0 && i < len(s) && (s[i] == '.' || s[i] == ')') && (i+1 == len(s) || s[i+1] == ' ') {
		return s[:i] + `\` + s[i:]
	}
	return s
}

// unescape removes the backslashes [escapeInline] and [escapeLineStart] add.
func unescape(s string) string {
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
		}
		sb.WriteByte(s[i])
	}
	return sb.String()
}
