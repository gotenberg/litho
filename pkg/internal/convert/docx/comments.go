package docx

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// maxComments bounds the comments of a document. Anti-DoS guard, not
// spec-mandated: each comment becomes an annotation, a structure element
// and its text held until the file is written, and a reviewed document
// holds tens of comments, a heavily reviewed one hundreds.
const maxComments = 10_000

// commentCheck is how many comments are read between two checks of the
// context.
const commentCheck = 256

// comments holds the comments of a document and the ranges open while
// its text is built ([document.inlines]). It is nil when the conversion
// leaves the comments out.
type comments struct {
	byID map[string]*commentEntry
	// open lists the comments whose range is open, in the order they
	// opened.
	open []*commentEntry
}

// commentEntry is one comment of comments.xml (ECMA-376 Part 1,
// 17.13.4.2).
type commentEntry struct {
	c *typeset.Comment
	// reply marks a reply, which its thread's comment carries: its own
	// range places nothing.
	reply bool
	// id is the comment's w:id.
	id string
	// covered reports a range that holds content; placed a comment set at
	// its point.
	covered, placed bool
}

// readComments reads the comments of the document from its comments part
// (ECMA-376 Part 1, 17.13.4.2) and their threads from Word's
// commentsExtended part ([MS-DOCX] 2.6.106, w15:commentEx). Each comment
// keeps its author (w:author), date (w:date) and text, its paragraphs
// one a line. A comment whose last paragraph's w14:paraId names, through
// w15:paraIdParent, another comment's last paragraph is a reply: the
// first comment of its chain carries it, as Word lists a thread. A
// thread's comment whose w15:done is true is resolved.
func (d *document) readComments() {
	name := d.pkg.RelatedPart(d.ctx, d.main, ooxml.RelComments)
	cs := &comments{byID: map[string]*commentEntry{}}
	d.comments = cs
	if name == "" {
		return
	}
	root, err := d.pkg.ParseXML(d.ctx, name, understood)
	if err != nil {
		d.warn(warn.PartMalformed, warn.Omitted, "the comments part does not parse; its comments are left out")
		return
	}
	byPara := map[string]*commentEntry{}
	var order []*commentEntry
	defer func() { cs.dropEmpty(order) }()
	for i, e := range root.Kids() {
		if i%commentCheck == 0 && d.ctx.Err() != nil {
			return
		}
		if !e.Is(w, "comment") {
			continue
		}
		if len(order) >= maxComments {
			d.warn(warn.LimitReached, warn.Omitted, fmt.Sprintf("the document holds more than %d comments; the rest are left out", maxComments))
			break
		}
		id := attrOr(e, "id")
		if _, dup := cs.byID[id]; dup {
			continue
		}
		c := &typeset.Comment{Author: d.commentText(attrOr(e, "author"))}
		c.Date, c.Zoned = commentDate(attrOr(e, "date"))
		var paras []string
		last := ""
		d.commentParas(e, &paras, &last, 0)
		c.Text = d.commentText(strings.Join(paras, "\n"))
		ce := &commentEntry{c: c, id: id}
		cs.byID[id] = ce
		order = append(order, ce)
		if last != "" {
			byPara[last] = ce
		}
	}
	ext := d.pkg.RelatedPart(d.ctx, d.main, ooxml.RelCommentsExtended)
	if ext == "" {
		return
	}
	xroot, err := d.pkg.ParseXML(d.ctx, ext, slices.Concat(understood, []string{ooxml.NSW15}))
	if err != nil {
		d.warn(warn.PartMalformed, warn.Omitted, "the comments extension part does not parse; the threads of the comments are left out")
		return
	}
	parent := map[*commentEntry]*commentEntry{}
	for i, e := range xroot.Kids() {
		if i%commentCheck == 0 && d.ctx.Err() != nil {
			return
		}
		if !e.Is(ooxml.NSW15, "commentEx") {
			continue
		}
		pid, _ := e.AttrNS(ooxml.NSW15, "paraId")
		ce := byPara[pid]
		if ce == nil {
			continue
		}
		if v, _ := e.AttrNS(ooxml.NSW15, "done"); v == "1" || v == "true" {
			ce.c.Resolved = true
		}
		if pp, ok := e.AttrNS(ooxml.NSW15, "paraIdParent"); ok {
			if p := byPara[pp]; p != nil && p != ce {
				parent[ce] = p
			}
		}
	}
	// The first comment of a chain carries the thread. Each chain is walked
	// once, its comments given their first; a loop has none.
	first := map[*commentEntry]*commentEntry{}
	for _, ce := range order {
		if _, ok := parent[ce]; !ok {
			continue
		}
		var path []*commentEntry
		p := ce
		for range len(order) + 1 {
			if f, ok := first[p]; ok {
				p = f
				break
			}
			q, ok := parent[p]
			if !ok {
				break
			}
			path = append(path, p)
			p = q
		}
		for _, x := range path {
			first[x] = p
		}
		if p == ce || parent[p] != nil {
			continue
		}
		if len(p.c.Replies) >= typeset.MaxCommentReplies {
			d.warn(warn.LimitReached, warn.Omitted, fmt.Sprintf("a comment thread holds more than %d replies; the rest are left out", typeset.MaxCommentReplies))
			continue
		}
		ce.reply = true
		ce.c.Resolved = false
		p.c.Replies = append(p.c.Replies, ce.c)
	}
}

// commentParas appends the text of each paragraph of a comment's content
// e to paras, a table's cells and content controls entered, and sets last
// to the w14:paraId of the last paragraph, the one w15:commentEx names
// ([MS-DOCX] 2.6.106).
func (d *document) commentParas(e *ooxml.Element, paras *[]string, last *string, depth int) {
	if depth > maxNesting {
		return
	}
	for _, k := range e.Kids() {
		switch {
		case k.Is(w, "p"):
			var sb strings.Builder
			commentRuns(k, &sb, depth+1)
			*paras = append(*paras, sb.String())
			*last, _ = k.AttrNS(ooxml.NSW14, "paraId")
		case k.Is(w, "tbl"), k.Is(w, "tr"), k.Is(w, "tc"), k.Is(w, "sdt"), k.Is(w, "sdtContent"), k.Is(w, "customXml"), k.Is(w, "ins"):
			d.commentParas(k, paras, last, depth+1)
		}
	}
}

// commentRuns writes the text of the runs under e to sb (17.3.3): a tab
// as a tab, a break as a line feed, a symbol as its character; deleted
// text, field instructions and the annotation reference mark write
// nothing.
func commentRuns(e *ooxml.Element, sb *strings.Builder, depth int) {
	if depth > maxNesting {
		return
	}
	for _, k := range e.Kids() {
		if k.Space != w {
			continue
		}
		switch k.Local {
		case "t":
			sb.WriteString(k.Text)
		case "tab", "ptab":
			sb.WriteByte('\t')
		case "br", "cr":
			sb.WriteByte('\n')
		case "sym":
			sb.WriteString(symbol(k))
		case "noBreakHyphen":
			sb.WriteRune('‑')
		case "del", "instrText", "delText", "pPr", "rPr":
		default:
			commentRuns(k, sb, depth+1)
		}
	}
}

// commentText returns s cut to [typeset.MaxCommentText] bytes at a
// character boundary, with a warning when it cuts, and valid UTF-8.
func (d *document) commentText(s string) string {
	s = strings.ToValidUTF8(s, "�")
	if len(s) <= typeset.MaxCommentText {
		return s
	}
	d.warn(warn.LimitReached, warn.Omitted, fmt.Sprintf("a comment holds more than %d bytes of text; the rest is left out", typeset.MaxCommentText))
	cut := typeset.MaxCommentText
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// commentDate reads a comment's w:date, an xsd:dateTime (ECMA-376 Part 1,
// 17.13.4.2; 22.9.2.1 ST_DateTime): with its offset from UTC when it
// states one, else as its clock reads. A date that does not parse is
// none.
func commentDate(v string) (time.Time, bool) {
	if v == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t, true
	}
	if t, err := time.Parse("2006-01-02T15:04:05", v); err == nil {
		return t, false
	}
	return time.Time{}, false
}

// start opens the range of comment id (17.13.4.4 commentRangeStart) in
// sink, nil between paragraphs: the next paragraph opens it.
func (cs *comments) start(id string, sink *inlineSink) {
	ce := cs.byID[id]
	if ce == nil || ce.reply || slices.Contains(cs.open, ce) {
		return
	}
	cs.open = append(cs.open, ce)
	if sink != nil {
		sink.openComment(ce)
	}
}

// end closes the range of comment id (17.13.4.3 commentRangeEnd) in sink,
// nil between paragraphs.
func (cs *comments) end(id string, sink *inlineSink) {
	ce := cs.byID[id]
	i := slices.Index(cs.open, ce)
	if ce == nil || i < 0 {
		return
	}
	cs.open = slices.Delete(cs.open, i, i+1)
	if sink != nil {
		sink.closeComment(ce)
	}
}

// reference places comment id at its reference mark (17.13.4.5
// commentReference) when no range of its own covers text: an empty Annot
// element at the point, in the paragraph's style ps, as a bookmark sits.
func (cs *comments) reference(id string, ps *css.Style, sink *inlineSink) {
	ce := cs.byID[id]
	if ce == nil || ce.reply || ce.placed || ce.covered || slices.Contains(cs.open, ce) {
		return
	}
	ce.placed = true
	sink.add(&layout.Node{Kind: layout.Element, Style: inlineStyle(ps), Role: "Annot", Comment: ce.c})
}

// resume opens in sink, a new paragraph's, the ranges still open.
func (cs *comments) resume(sink *inlineSink) {
	for _, ce := range cs.open {
		sink.openComment(ce)
	}
}

// apart sets the open ranges aside while a story of its own is built, a
// note's body or a text box's, whose text no range around its anchor
// covers, and returns what puts them back. A range the story opens and
// leaves open ends with it.
func (cs *comments) apart() func() {
	if cs == nil {
		return func() {}
	}
	saved := cs.open
	cs.open = nil
	return func() { cs.open = saved }
}

// commentsHere reports whether the comments of story s are written: those
// of the body, its tables, notes and text boxes. A header or a footer is
// laid out on every page apart from the body, and its comments are left
// out with a warning. Word offers no comment in a header or a footer, and
// LibreOffice opens no DOCX document that holds one.
//
// # Corpus evidence
//
//   - probe, docx: LibreOffice 26.8 of the pinned image refuses to load a
//     document whose header, or whose footnote, holds a comment's range
//     and reference; a comment in a text box it exports.
func (d *document) commentsHere(s *story) bool {
	if s.fields == nil {
		return true
	}
	d.warn(warn.ContentUnsupported, warn.Omitted, "comments in headers and footers are not written")
	return false
}

// dropEmpty leaves out the comments of order that hold no text and no
// reply, as if the document held none: an annotation without text says
// nothing a reader reads, and ISO 14289-1, 7.18.1 asks every annotation
// for its content.
func (cs *comments) dropEmpty(order []*commentEntry) {
	for _, ce := range order {
		if !ce.reply && len(ce.c.Replies) == 0 && strings.TrimSpace(ce.c.Text) == "" {
			delete(cs.byID, ce.id)
		}
	}
}
