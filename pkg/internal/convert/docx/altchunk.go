package docx

import (
	"encoding/xml"
	"fmt"
	"path"
	"strconv"
	"strings"

	dom "golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/gotenberg/litho/pkg/internal/convert/html"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// The bounds of an imported HTML chunk. Anti-DoS guards, not
// spec-mandated: a chunk is a fragment of a page, its elements a few
// thousand at most; each element read is a paragraph, a run or a cell of
// the WordprocessingML it becomes.
const (
	// maxChunkBytes caps the size of an HTML chunk read.
	maxChunkBytes = 8 << 20
	// maxChunkElements caps the WordprocessingML elements a chunk
	// becomes.
	maxChunkElements = 1 << 16
	// maxChunkDepth caps the nesting of the HTML walked.
	maxChunkDepth = 128
)

// altChunk imports the alternative format chunk e (ECMA-376 Part 1,
// 17.17.2.1 altChunk) in place: an HTML part (its content type
// application/xhtml+xml or text/html, or an .htm, .html or .xhtml name)
// becomes WordprocessingML paragraphs and tables that flow in the body
// with the document's page geometry and styles, as Word imports it. A
// chunk of another kind (MHTML, RTF, a WordprocessingML document, plain
// text) is left out with a warning that names it.
//
// The HTML becomes what Word's import makes of it, its style sheets left
// out: a heading h1 to h6 a paragraph of the document's heading 1 to 6
// style, a p or a pre one in Times New Roman of 12 points with 14 points
// before and after, a list item a Normal paragraph indented 0.5 inch
// behind its bullet or number, a table one of single borders whose
// cells hold Normal paragraphs, b, strong, i, em, u, s, sub, sup and code
// their run properties, a link blue and underlined.
//
// # Corpus evidence
//
//   - litho/docx: altchunks prints its HTML chunk's h2 in the document's
//     Heading 2 style, its p in Times New Roman 12 points, its list items
//     and table cells in Calibri 11 points (Normal), the table's cells
//     boxed; its RTF and DOCX chunks print too, which litho leaves out.
func (d *document) altChunk(s *story, c *cond, e *ooxml.Element, depth int, out *[]blockNode) {
	id, _ := e.AttrNS(ooxml.NSR, "id")
	rel, ok := d.pkg.RelationshipByID(d.ctx, s.part, id)
	if !ok || rel.External {
		d.warn(warn.ContentUnsupported, warn.Omitted, "an imported chunk (altChunk) whose part is missing is left out")
		return
	}
	kind := chunkKind(d.pkg.ContentType(rel.Target), rel.Target)
	if kind != "HTML" {
		d.warn(warn.ContentUnsupported, warn.Omitted, "an imported chunk (altChunk) of "+kind+" is left out")
		return
	}
	data, err := d.pkg.Read(rel.Target)
	if err != nil {
		if d.stop(err) {
			return
		}
		d.warn(warn.ContentUnsupported, warn.Omitted, fmt.Sprintf("an imported HTML chunk cannot be read: %v", err))
		return
	}
	if len(data) > maxChunkBytes {
		d.warn(warn.LimitReached, warn.Omitted, fmt.Sprintf("an imported HTML chunk past %d bytes is left out", maxChunkBytes))
		return
	}
	// The chunk is an HTML document of its own: its byte order mark and
	// the charset it declares say how its bytes read, as a page's do.
	root, err := dom.Parse(strings.NewReader(html.Decode(data)))
	if err != nil {
		d.warn(warn.ContentUnsupported, warn.Omitted, fmt.Sprintf("an imported HTML chunk does not parse: %v", err))
		return
	}
	hb := &htmlBody{d: d, body: &ooxml.Element{Space: w, Local: "body"}}
	hb.walk(root, 0)
	hb.flush()
	if hb.full {
		d.warn(warn.LimitReached, warn.Omitted, fmt.Sprintf("an imported HTML chunk past %d elements is cut", maxChunkElements))
	}
	d.blocksInto(s, c, hb.body, depth+1, out)
}

// chunkKind names the format of a chunk part by its content type, else
// its extension: HTML, MHTML, RTF, a WordprocessingML document, plain
// text, or the content type itself.
func chunkKind(contentType, name string) string {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	switch {
	case ct == "application/xhtml+xml" || ct == "text/html":
		return "HTML"
	case ct == "message/rfc822" || ct == "multipart/related":
		return "MHTML"
	case ct == "application/rtf" || ct == "text/rtf":
		return "RTF"
	case strings.Contains(ct, "wordprocessingml"):
		return "WordprocessingML"
	case ct == "text/plain":
		return "plain text"
	}
	switch strings.ToLower(path.Ext(name)) {
	case ".htm", ".html", ".xhtml":
		return "HTML"
	case ".mht", ".mhtml":
		return "MHTML"
	case ".rtf":
		return "RTF"
	case ".docx", ".docm", ".dotx", ".dotm":
		return "WordprocessingML"
	case ".txt":
		return "plain text"
	}
	if ct != "" {
		return "type " + ct
	}
	return "an unknown type"
}

// htmlBody builds the WordprocessingML of an HTML chunk.
type htmlBody struct {
	d    *document
	body *ooxml.Element
	// cur is the paragraph inline content goes into, nil for none yet;
	// rPr the run properties every run of it takes.
	cur  *ooxml.Element
	rPr  []*ooxml.Element
	run  []*ooxml.Element // the run properties of the inline elements open
	into []*ooxml.Element // the containers blocks go into, the body first
	list []listLevel
	n    int
	full bool
	// space records that a collapsible space is due before the next text.
	space bool
}

// listLevel is an open ul or ol, with the number of its next item.
type listLevel struct {
	ordered bool
	next    int
}

// el returns a new WordprocessingML element of attributes kv (name,
// value pairs), counted against maxChunkElements.
func (hb *htmlBody) el(local string, kv ...string) *ooxml.Element {
	hb.n++
	if hb.n > maxChunkElements {
		hb.full = true
	}
	e := &ooxml.Element{Space: w, Local: local}
	for i := 0; i+1 < len(kv); i += 2 {
		e.Attrs = append(e.Attrs, xml.Attr{Name: xml.Name{Space: w, Local: kv[i]}, Value: kv[i+1]})
	}
	return e
}

// with returns e holding kids.
func with(e *ooxml.Element, kids ...*ooxml.Element) *ooxml.Element {
	e.Children = append(e.Children, kids...)
	return e
}

// container returns the element blocks go into.
func (hb *htmlBody) container() *ooxml.Element {
	if len(hb.into) > 0 {
		return hb.into[len(hb.into)-1]
	}
	return hb.body
}

// flush closes the open paragraph.
func (hb *htmlBody) flush() {
	if hb.cur != nil {
		with(hb.container(), hb.cur)
	}
	hb.cur, hb.space = nil, false
}

// para opens a paragraph of properties pPr and run properties rPr, which
// every run of it takes.
func (hb *htmlBody) para(pPr, rPr []*ooxml.Element) {
	hb.flush()
	hb.rPr = rPr
	hb.cur = with(hb.el("p"), with(hb.el("pPr"), pPr...))
}

// text adds the characters s to the open paragraph, opening a Normal one
// when none is; white space collapses as HTML collapses it.
func (hb *htmlBody) text(s string) {
	if hb.full {
		return
	}
	isSpace := func(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' }
	words := strings.Fields(s)
	if len(words) == 0 {
		hb.space = hb.space || len(s) > 0 && hb.cur != nil
		return
	}
	lead := hb.space || isSpace(s[0])
	if hb.cur == nil {
		hb.para(nil, nil)
		lead = false
	} else if len(hb.cur.Children) == 1 {
		// Nothing but the paragraph properties: no space starts a line.
		lead = false
	}
	txt := strings.Join(words, " ")
	if lead {
		txt = " " + txt
	}
	hb.space = isSpace(s[len(s)-1])
	t := hb.el("t")
	t.Attrs = append(t.Attrs, xml.Attr{Name: xml.Name{Space: "http://www.w3.org/XML/1998/namespace", Local: "space"}, Value: "preserve"})
	t.Text = txt
	rpr := hb.el("rPr")
	rpr.Children = append(append(rpr.Children, hb.rPr...), hb.run...)
	with(hb.cur, with(hb.el("r"), rpr, t))
}

// The run and paragraph properties of the HTML elements Word imports.
func (hb *htmlBody) webPara() ([]*ooxml.Element, []*ooxml.Element) {
	return []*ooxml.Element{hb.el("spacing", "before", "280", "after", "280")},
		[]*ooxml.Element{hb.el("rFonts", "ascii", "Times New Roman", "hAnsi", "Times New Roman", "cs", "Times New Roman"), hb.el("sz", "val", "24"), hb.el("szCs", "val", "24")}
}

// headingStyle returns the id of the document's paragraph style named
// "heading n", the least id of several, "" for none.
func (hb *htmlBody) headingStyle(n int) string {
	want := "heading " + strconv.Itoa(n)
	best := ""
	if st := hb.d.styles; st != nil {
		for id, s := range st.byID {
			if strings.EqualFold(s.name, want) && s.typ == "paragraph" && (best == "" || id < best) {
				best = id
			}
		}
	}
	return best
}

// walk converts the HTML node n.
func (hb *htmlBody) walk(n *dom.Node, depth int) {
	if depth > maxChunkDepth || hb.full {
		return
	}
	switch n.Type { //nolint:exhaustive // comments, doctypes and raw nodes draw nothing
	case dom.TextNode:
		hb.text(n.Data)
		return
	case dom.ElementNode, dom.DocumentNode:
	default:
		return
	}
	kids := func() {
		for k := n.FirstChild; k != nil; k = k.NextSibling {
			hb.walk(k, depth+1)
		}
	}
	switch n.DataAtom { //nolint:exhaustive // every other element is walked through for its content
	case atom.Head, atom.Script, atom.Style, atom.Title, atom.Template, atom.Noscript:
		return
	case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
		level := int(n.Data[1] - '0')
		var pPr, rPr []*ooxml.Element
		if id := hb.headingStyle(level); id != "" {
			pPr = []*ooxml.Element{hb.el("pStyle", "val", id)}
		} else {
			rPr = []*ooxml.Element{hb.el("b"), hb.el("sz", "val", strconv.Itoa(max(20, 40-4*level)))}
		}
		hb.para(pPr, rPr)
		kids()
		hb.flush()
	case atom.P, atom.Pre, atom.Address:
		pPr, rPr := hb.webPara()
		if n.DataAtom == atom.Pre {
			rPr = []*ooxml.Element{hb.el("rFonts", "ascii", "Courier New", "hAnsi", "Courier New"), hb.el("sz", "val", "20")}
		}
		hb.para(pPr, rPr)
		kids()
		hb.flush()
	case atom.Div, atom.Section, atom.Article, atom.Header, atom.Footer, atom.Main, atom.Nav, atom.Aside, atom.Blockquote, atom.Center, atom.Form, atom.Fieldset, atom.Figure, atom.Body, atom.Html:
		hb.flush()
		kids()
		hb.flush()
	case atom.Ul, atom.Ol:
		hb.flush()
		hb.list = append(hb.list, listLevel{ordered: n.DataAtom == atom.Ol, next: 1})
		kids()
		hb.list = hb.list[:len(hb.list)-1]
		hb.flush()
	case atom.Li:
		hb.item(kids)
	case atom.Table:
		hb.table(n, depth)
	case atom.Br:
		if hb.cur == nil {
			hb.para(nil, nil)
		}
		with(hb.cur, with(hb.el("r"), hb.el("br")))
		hb.space = false
	case atom.Hr, atom.Img, atom.Input, atom.Select, atom.Button, atom.Textarea, atom.Iframe, atom.Object, atom.Svg:
		return
	default:
		if props := hb.inline(n.DataAtom); props != nil {
			hb.run = append(hb.run, props...)
			kids()
			hb.run = hb.run[:len(hb.run)-len(props)]
			return
		}
		kids()
	}
}

// inline returns the run properties an inline element adds, nil for
// none.
func (hb *htmlBody) inline(a atom.Atom) []*ooxml.Element {
	switch a { //nolint:exhaustive // every other element adds no run property
	case atom.B, atom.Strong:
		return []*ooxml.Element{hb.el("b")}
	case atom.I, atom.Em, atom.Cite, atom.Var, atom.Dfn:
		return []*ooxml.Element{hb.el("i")}
	case atom.U, atom.Ins:
		return []*ooxml.Element{hb.el("u", "val", "single")}
	case atom.S, atom.Strike, atom.Del:
		return []*ooxml.Element{hb.el("strike")}
	case atom.Sub:
		return []*ooxml.Element{hb.el("vertAlign", "val", "subscript")}
	case atom.Sup:
		return []*ooxml.Element{hb.el("vertAlign", "val", "superscript")}
	case atom.Code, atom.Tt, atom.Kbd, atom.Samp:
		return []*ooxml.Element{hb.el("rFonts", "ascii", "Courier New", "hAnsi", "Courier New")}
	case atom.A:
		return []*ooxml.Element{hb.el("color", "val", "0000FF"), hb.el("u", "val", "single")}
	}
	return nil
}

// item converts a list item: a Normal paragraph indented 0.5 inch, its
// bullet or number hanging 0.25 inch before it, a level deeper each list
// it is nested in.
func (hb *htmlBody) item(kids func()) {
	mark := "•"
	lvl := max(len(hb.list), 1)
	if len(hb.list) > 0 {
		l := &hb.list[len(hb.list)-1]
		if l.ordered {
			mark = strconv.Itoa(l.next) + "."
		}
		l.next++
	}
	left := strconv.Itoa(720 * lvl)
	hb.para([]*ooxml.Element{hb.el("spacing", "before", "0", "after", "0"), hb.el("ind", "left", left, "hanging", "360"), with(hb.el("tabs"), hb.el("tab", "val", "left", "pos", left))}, nil)
	t := hb.el("t")
	t.Text = mark
	with(hb.cur, with(hb.el("r"), t), with(hb.el("r"), hb.el("tab")))
	kids()
	hb.flush()
}

// table converts a table: a row each tr of its own, its row groups' and
// a cell each td or th, the cells' content Normal paragraphs, every
// border single.
func (hb *htmlBody) table(n *dom.Node, depth int) {
	hb.flush()
	border := func(side string) *ooxml.Element { return hb.el(side, "val", "single", "sz", "4", "color", "auto") }
	tbl := with(hb.el("tbl"), with(hb.el("tblPr"),
		hb.el("tblW", "w", "0", "type", "auto"),
		with(hb.el("tblBorders"), border("top"), border("left"), border("bottom"), border("right"), border("insideH"), border("insideV"))))
	var rows func(*dom.Node, int)
	rows = func(p *dom.Node, dd int) {
		if dd > maxChunkDepth {
			return
		}
		for r := p.FirstChild; r != nil && !hb.full; r = r.NextSibling {
			switch r.DataAtom { //nolint:exhaustive // a table holds row groups and rows alone
			case atom.Thead, atom.Tbody, atom.Tfoot:
				rows(r, dd+1)
			case atom.Tr:
				tr := hb.el("tr")
				for c := r.FirstChild; c != nil && !hb.full; c = c.NextSibling {
					if c.DataAtom != atom.Td && c.DataAtom != atom.Th {
						continue
					}
					tc := hb.el("tc")
					hb.into = append(hb.into, tc)
					var bold []*ooxml.Element
					if c.DataAtom == atom.Th {
						bold = []*ooxml.Element{hb.el("b")}
					}
					hb.run = append(hb.run, bold...)
					for k := c.FirstChild; k != nil; k = k.NextSibling {
						hb.walk(k, dd+2)
					}
					hb.run = hb.run[:len(hb.run)-len(bold)]
					hb.flush()
					hb.into = hb.into[:len(hb.into)-1]
					if len(tc.Children) == 0 {
						with(tc, hb.el("p"))
					}
					with(tr, tc)
				}
				if len(tr.Children) > 0 {
					with(tbl, tr)
				}
			}
		}
	}
	rows(n, depth+1)
	with(hb.container(), tbl)
}
