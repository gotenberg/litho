package odt

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/intconv"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/odf"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// maxComments bounds the comments of a document. Anti-DoS guard, not
// spec-mandated: each comment becomes an annotation, a structure element
// and its text held until the file is written, and a reviewed document
// holds tens of comments, a heavily reviewed one hundreds.
const maxComments = 10_000

// commentCheck is how many elements the scan for comments walks between
// two checks of the context.
const commentCheck = 1024

// comments holds the comments of a document's body and the ranges open
// while its paragraphs are built. It is nil when the conversion leaves the
// comments out.
type comments struct {
	byElem map[*odf.Element]*commentEntry
	byName map[string]*commentEntry
	// ended holds the names an office:annotation-end closes.
	ended map[string]bool
	// open lists the comments whose range is open, in the order they
	// opened, across the paragraphs built.
	open []*commentEntry
	// marks holds the marker nodes [builder.commentMark] made in the
	// paragraph being built, which [builder.wrapComments] takes out.
	marks map[*layout.Node]commentMark
}

// commentEntry is one office:annotation of the body.
type commentEntry struct {
	c *typeset.Comment
	// reply marks a reply, which its thread's comment carries: it places
	// nothing of its own.
	reply bool
	// el is the comment's office:annotation.
	el *odf.Element
}

// commentMark is what a marker node stands for: the start or the end of
// a comment's range, or a comment on a point.
type commentMark struct {
	ce   *commentEntry
	kind markKind
}

type markKind uint8

const (
	markStart markKind = iota
	markEnd
	markPoint
)

// readComments reads the comments of the body (ODF 1.3, Part 3, 14.1
// office:annotation): each one's author (dc:creator), date (dc:date) and
// paragraphs, one a line. A comment that LibreOffice marks as a reply
// (loext:parent-name, naming another comment's office:name) joins the
// thread of the first comment of its chain, in document order; one
// marked loext:resolved is resolved.
func (b *builder) readComments(body *odf.Element) {
	cs := &comments{byElem: map[*odf.Element]*commentEntry{}, byName: map[string]*commentEntry{}, ended: map[string]bool{}, marks: map[*layout.Node]commentMark{}}
	b.comments = cs
	var order []*commentEntry
	parent := map[*commentEntry]string{}
	steps := 0
	type item struct {
		e     *odf.Element
		depth int
	}
	stack := []item{{body, 0}}
	for len(stack) > 0 {
		it := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if steps++; steps%commentCheck == 0 && b.ctx.Err() != nil {
			return
		}
		e := it.e
		switch {
		case e.Is(odf.NSOffice, "annotation-end"):
			cs.ended[e.A(odf.NSOffice, "name")] = true
			continue
		case e.Is(odf.NSOffice, "annotation"):
			if len(order) >= maxComments {
				b.warn("comments-limit", warn.LimitReached, warn.Omitted, fmt.Sprintf("the document holds more than %d comments; the rest are left out", maxComments))
				continue
			}
			c := &typeset.Comment{
				Author:   b.commentText(e.Child(odf.NSDC, "creator").TextContent()),
				Text:     b.commentText(annotationText(e)),
				Resolved: e.A(odf.NSLOExt, "resolved") == "true",
			}
			c.Date, c.Zoned = commentDate(strings.TrimSpace(e.Child(odf.NSDC, "date").TextContent()))
			ce := &commentEntry{c: c, el: e}
			cs.byElem[e] = ce
			order = append(order, ce)
			if name := e.A(odf.NSOffice, "name"); name != "" {
				if _, dup := cs.byName[name]; !dup {
					cs.byName[name] = ce
				}
			}
			if p := e.A(odf.NSLOExt, "parent-name"); p != "" {
				parent[ce] = p
			}
			// A comment's own paragraphs hold no comment.
			continue
		}
		if it.depth >= odf.MaxDepth {
			continue
		}
		for _, k := range slices.Backward(e.Children) {
			if !k.IsText() {
				stack = append(stack, item{k, it.depth + 1})
			}
		}
	}
	// The first comment of a chain carries the thread. Each chain is walked
	// once, its comments given their first; a loop has none.
	up := func(ce *commentEntry) *commentEntry {
		if name, ok := parent[ce]; ok {
			return cs.byName[name]
		}
		return nil
	}
	first := map[*commentEntry]*commentEntry{}
	for _, ce := range order {
		if up(ce) == nil {
			continue
		}
		var path []*commentEntry
		p := ce
		for range len(order) + 1 {
			if f, ok := first[p]; ok {
				p = f
				break
			}
			q := up(p)
			if q == nil {
				break
			}
			path = append(path, p)
			p = q
		}
		for _, x := range path {
			first[x] = p
		}
		if p == ce || up(p) != nil {
			continue
		}
		if len(p.c.Replies) >= typeset.MaxCommentReplies {
			b.warn("comment-replies-limit", warn.LimitReached, warn.Omitted, fmt.Sprintf("a comment thread holds more than %d replies; the rest are left out", typeset.MaxCommentReplies))
			continue
		}
		ce.reply = true
		ce.c.Resolved = false
		p.c.Replies = append(p.c.Replies, ce.c)
	}
	// A comment that holds no text and no reply is left out, as if the
	// document held none: its annotation would say nothing a reader
	// reads, and ISO 14289-1, 7.18.1 asks every annotation for its
	// content.
	for _, ce := range order {
		if !ce.reply && len(ce.c.Replies) == 0 && strings.TrimSpace(ce.c.Text) == "" {
			delete(cs.byElem, ce.el)
		}
	}
}

// annotationText returns the text of an office:annotation's paragraphs,
// one a line, its lists' paragraphs included: a text:s as its spaces, a
// text:tab as a tab and a text:line-break as a line feed (ODF 1.3, Part 3,
// 6.1).
func annotationText(e *odf.Element) string {
	var paras []string
	var walk func(*odf.Element, int)
	walk = func(el *odf.Element, depth int) {
		if depth > odf.MaxDepth {
			return
		}
		for _, k := range el.Children {
			switch {
			case k.Is(odf.NSText, "p"), k.Is(odf.NSText, "h"):
				var sb strings.Builder
				annotationRun(k, &sb, depth+1)
				paras = append(paras, sb.String())
			case k.Is(odf.NSText, "list"), k.Is(odf.NSText, "list-item"), k.Is(odf.NSText, "list-header"):
				walk(k, depth+1)
			}
		}
	}
	walk(e, 0)
	return strings.Join(paras, "\n")
}

// annotationRun writes the inline content of el to sb.
func annotationRun(el *odf.Element, sb *strings.Builder, depth int) {
	if depth > odf.MaxDepth {
		return
	}
	for _, k := range el.Children {
		switch {
		case k.IsText():
			sb.WriteString(k.Text)
		case k.Is(odf.NSText, "s"):
			n := 1
			if v, err := intconv.Atoi(k.A(odf.NSText, "c")); err == nil && v > 1 {
				n = min(v, maxSpaces)
			}
			sb.WriteString(strings.Repeat(" ", n))
		case k.Is(odf.NSText, "tab"):
			sb.WriteByte('\t')
		case k.Is(odf.NSText, "line-break"):
			sb.WriteByte('\n')
		case k.Name.Space == odf.NSText:
			annotationRun(k, sb, depth+1)
		}
	}
}

// commentText returns s cut to [typeset.MaxCommentText] bytes at a
// character boundary, with a warning when it cuts, and valid UTF-8.
func (b *builder) commentText(s string) string {
	s = strings.ToValidUTF8(s, "�")
	if len(s) <= typeset.MaxCommentText {
		return s
	}
	b.warn("comment-text-limit", warn.LimitReached, warn.Omitted, fmt.Sprintf("a comment holds more than %d bytes of text; the rest is left out", typeset.MaxCommentText))
	cut := typeset.MaxCommentText
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// commentDate reads a comment's dc:date, an xsd:dateTime (ODF 1.3, Part
// 3, 14.1; 19.21 of the metadata elements): with its offset from UTC
// when it states one, else as its clock reads. A date that does not parse
// is none.
func commentDate(v string) (time.Time, bool) {
	if v == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
		return t, true
	}
	for _, layout := range []string{"2006-01-02T15:04:05.999999999", "2006-01-02"} {
		if t, err := time.Parse(layout, v); err == nil {
			return t, false
		}
	}
	return time.Time{}, false
}

// commentMark appends to out the marker of the office:annotation or
// office:annotation-end c: the start of a comment's range when an
// office:annotation-end of its name follows, else its point (ODF 1.3,
// Part 3, 14.1 and 14.2). A marker is an empty inline element, as a
// bookmark is, which [builder.wrapComments] takes out of the paragraph
// it was made in. A comment in a header or a footer is left out with a
// warning: it is laid out on every page apart from the body.
func (b *builder) commentMark(c *odf.Element, out *[]*layout.Node) {
	cs := b.comments
	if b.hf != nil {
		if c.Is(odf.NSOffice, "annotation") {
			b.warn("header-comments", warn.ContentUnsupported, warn.Omitted, "comments in headers and footers are not written")
		}
		return
	}
	if c.Is(odf.NSOffice, "annotation-end") {
		ce := cs.byName[c.A(odf.NSOffice, "name")]
		if ce == nil || ce.reply {
			return
		}
		n := b.anchor("")
		cs.marks[n] = commentMark{ce: ce, kind: markEnd}
		*out = append(*out, n)
		return
	}
	ce := cs.byElem[c]
	if ce == nil || ce.reply {
		return
	}
	kind := markPoint
	if name := c.A(odf.NSOffice, "name"); name != "" && cs.ended[name] && cs.byName[name] == ce {
		kind = markStart
	}
	n := b.anchor("")
	cs.marks[n] = commentMark{ce: ce, kind: kind}
	*out = append(*out, n)
}

// wrapComments takes the comment markers out of kids, a paragraph's
// inline content, and returns it with the content each comment's range
// covers inside an Annot element carrying the comment (layout.Node
// Comment), and an empty one at a comment's point. A range runs from its
// start marker to its end marker in document order, through the
// paragraphs between them; content under several ranges sits in an Annot
// element of each, nested in the order the ranges opened. A node that
// holds markers is entered rather than wrapped.
func (b *builder) wrapComments(kids []*layout.Node, depth int) []*layout.Node {
	cs := b.comments
	if cs == nil || depth > odf.MaxDepth {
		return kids
	}
	var out, group []*layout.Node
	var groupOpen []*commentEntry
	flush := func() {
		if len(group) == 0 {
			return
		}
		nodes := group
		for i := len(groupOpen) - 1; i >= 0; i-- {
			nodes = []*layout.Node{b.node(&layout.Node{Kind: layout.Element, Style: baseStyle(), Role: "Annot", Comment: groupOpen[i].c, Children: nodes})}
		}
		out = append(out, nodes...)
		group, groupOpen = nil, nil
	}
	for _, n := range kids {
		if m, ok := cs.marks[n]; ok {
			delete(cs.marks, n)
			switch m.kind {
			case markStart:
				if !slices.Contains(cs.open, m.ce) {
					flush()
					cs.open = append(cs.open, m.ce)
				}
			case markEnd:
				if i := slices.Index(cs.open, m.ce); i >= 0 {
					flush()
					cs.open = slices.Delete(cs.open, i, i+1)
				}
			case markPoint:
				flush()
				out = append(out, b.node(&layout.Node{Kind: layout.Element, Style: baseStyle(), Role: "Annot", Comment: m.ce.c}))
			}
			continue
		}
		if n.Kind == layout.LineBreak || n.Style != nil && (n.Style.Float != css.FloatNone || n.Style.Position == css.Absolute || n.Style.Position == css.Fixed) {
			// A frame out of the line and a line break stay where the
			// paragraph's own checks find them; the range goes round them.
			flush()
			out = append(out, n)
			continue
		}
		if b.holdsMarks(n, depth) {
			flush()
			n.Children = b.wrapComments(n.Children, depth+1)
			out = append(out, n)
			continue
		}
		if len(cs.open) == 0 {
			flush()
			out = append(out, n)
			continue
		}
		if !slices.Equal(groupOpen, cs.open) {
			flush()
			groupOpen = slices.Clone(cs.open)
		}
		group = append(group, n)
	}
	flush()
	return out
}

// holdsMarks reports whether a comment marker lies in n's subtree.
func (b *builder) holdsMarks(n *layout.Node, depth int) bool {
	if len(b.comments.marks) == 0 || depth > odf.MaxDepth {
		return false
	}
	for _, k := range n.Children {
		if _, ok := b.comments.marks[k]; ok || b.holdsMarks(k, depth+1) {
			return true
		}
	}
	return false
}

// apart sets the open ranges aside while content of its own is built, a
// note's body or a frame's text, whose paragraphs no range around its
// anchor covers, and returns what puts them back.
func (cs *comments) apart() func() {
	if cs == nil {
		return func() {}
	}
	saved := cs.open
	cs.open = nil
	return func() { cs.open = saved }
}
