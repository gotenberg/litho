package markdown

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/tagging"
	"github.com/gotenberg/litho/pkg/internal/typography"
)

// Page is one page the writer reads, as the caller extracted it.
type Page struct {
	// Page is the page of the document.
	Page *document.Page
	// Index is its 0-based index in the document's page tree.
	Index int
	// Shown is the text the page shows, extracted from the operations
	// [tagging.PageContent] holds.
	Shown typography.ShownText
	// Ranges is the byte range in that content of each operation the
	// glyphs' [typography.ShownText.PageOps] name.
	Ranges []content.ByteRange
	// Links are the page's links to a URI.
	Links []Link
	// Recognized is the text optical character recognition read on a page
	// that shows none, "" when there is none.
	Recognized string
}

// Link is a link annotation whose action opens a URI (ISO 32000-1, 12.6.4.7).
type Link struct {
	// Rect is the annotation's /Rect, in default user space.
	Rect [4]float64
	// URI is the target.
	URI string
}

// Options configures [Write].
type Options struct {
	// Artifacts keeps the running headers, footers and page numbers the
	// layout sets apart. A tagged document's artifacts are left out of the
	// pages' text by their extraction instead.
	Artifacts bool
}

// Write returns the Markdown of pages, which are in document order. A
// document whose structure tree reaches the glyphs of pages is read through
// it, and any other through the structure [tagging.Classify] derives from the
// layout.
func Write(ctx context.Context, doc *document.Document, pages []Page, opts Options) (string, error) {
	glyphs := newGlyphIndex(pages)
	pageIndex := make(map[ast.PdfIndirectRef]int, len(pages))
	for i, p := range pages {
		pageIndex[p.Page.Ref] = i
	}

	root, err := structTree(ctx, doc, pageIndex, glyphs)
	if err != nil {
		return "", err
	}
	if !reachesGlyphs(root) {
		classified, err := tagging.Classify(ctx, nil, doc)
		if err != nil {
			return "", fmt.Errorf("reading the layout: %w", err)
		}
		docPage := make(map[int]int, len(pages))
		for i, p := range pages {
			docPage[p.Index] = i
		}
		root = classifiedTree(classified, func(index int) int {
			if i, ok := docPage[index]; ok {
				return i
			}
			return -1
		}, glyphs)
	}

	w := &writer{ctx: ctx, pages: pages, opts: opts, used: make([][]bool, len(pages))}
	for i, p := range pages {
		w.used[i] = make([]bool, len(p.Shown.Elements))
	}
	body := w.blocks(root)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return joinBlocks(w.withLeftovers(body)), nil
}

// reachesGlyphs reports whether the tree holds a glyph.
func reachesGlyphs(n *node) bool {
	if n == nil {
		return false
	}
	return len(n.glyphs) > 0 || slices.ContainsFunc(n.kids, reachesGlyphs)
}

// block is a Markdown block and the index of the page its first glyph sits
// on, -1 when it holds none.
type block struct {
	text string
	page int
}

// writer renders nodes as Markdown blocks. Each glyph is written once: used
// marks those written, and those set aside as furniture.
type writer struct {
	ctx   context.Context
	pages []Page
	opts  Options
	used  [][]bool
}

// headingLevels maps the heading roles to a Markdown heading level (ISO
// 32000-1, Table 334; ISO 32000-2, 14.8.4.3.2 adds H and Title).
var headingLevels = map[string]int{
	"Title": 1, "H": 1, "H1": 1, "H2": 2, "H3": 3, "H4": 4, "H5": 5, "H6": 6,
}

// paragraphRoles are the block-level roles whose content reads as one
// paragraph (ISO 32000-1, 14.8.4.3 and 14.8.4.4).
var paragraphRoles = map[string]bool{
	"P": true, "Caption": true, "TOCI": true, "BibEntry": true, "Lbl": true,
}

// blockRoles are the roles that start a block of their own wherever they sit.
var blockRoles = map[string]bool{
	"L": true, "LI": true, "LBody": true, "Table": true, "TR": true, "TH": true, "TD": true,
	"THead": true, "TBody": true, "TFoot": true, "Figure": true, "BlockQuote": true,
	"TOC": true, "Index": true, roleArtifact: true,
}

// inlineMarks are the inline-level roles Markdown writes with a delimiter
// (ISO 32000-1, 14.8.4.4; ISO 32000-2 adds Strong and Em).
var inlineMarks = map[string]mark{
	"Strong": {open: "**", close: "**"},
	"Em":     {open: "*", close: "*"},
	"Code":   {open: "`", close: "`", code: true},
}

// inlineRoles are the inline-level roles whose content joins the text around
// it (ISO 32000-1, Table 338 and 14.8.4.4).
var inlineRoles = map[string]bool{
	"Span": true, "Strong": true, "Em": true, "Code": true, "Link": true, "Quote": true,
	"Reference": true, "Note": true, "Annot": true, "Sub": true, "Ruby": true, "RB": true,
	"RT": true, "RP": true, "Warichu": true, "WT": true, "WP": true, "Formula": true,
}

func isBlock(n *node) bool {
	_, heading := headingLevels[n.role]
	return heading || paragraphRoles[n.role] || blockRoles[n.role]
}

// holdsBlock reports whether a descendant of n is a block.
func holdsBlock(n *node) bool {
	return slices.ContainsFunc(n.kids, func(k *node) bool { return isBlock(k) || holdsBlock(k) })
}

// blocks renders n.
func (w *writer) blocks(n *node) []block {
	if w.ctx.Err() != nil {
		return nil
	}
	if level, ok := headingLevels[n.role]; ok && !holdsBlock(n) {
		return w.textBlock(n, strings.Repeat("#", level)+" ")
	}
	switch {
	case n.role == roleArtifact:
		if w.opts.Artifacts {
			return w.textBlock(n, "")
		}
		w.markUsed(n)
		return nil
	case paragraphRoles[n.role] && !holdsBlock(n):
		return w.textBlock(n, "")
	case n.role == "L":
		return w.list(n)
	case n.role == "Table":
		return w.table(n)
	case n.role == "Figure" && n.alt != "":
		w.markUsed(n)
		return []block{{text: "![" + escapeInline(collapse(n.alt)) + "]()", page: firstPage(n)}}
	case n.role == "BlockQuote":
		var out []block
		for _, b := range w.container(n) {
			out = append(out, block{text: prefixLines(b.text, "> ", ">"), page: b.page})
		}
		return out
	}
	return w.container(n)
}

// container renders the kids of n in order. Content and inline elements
// between two blocks form a paragraph, and so does each kid that holds no
// block.
func (w *writer) container(n *node) []block {
	var pending []*node
	var blocks []block
	flush := func() {
		if len(pending) > 0 {
			blocks = append(blocks, w.textBlock(&node{kids: pending}, "")...)
			pending = nil
		}
	}
	for _, kid := range n.kids {
		switch {
		case kid.role == "" || inlineRoles[kid.role]:
			pending = append(pending, kid)
		case !isBlock(kid) && !holdsBlock(kid):
			flush()
			blocks = append(blocks, w.textBlock(kid, "")...)
		default:
			flush()
			blocks = append(blocks, w.blocks(kid)...)
		}
	}
	flush()
	return blocks
}

// textBlock renders the content of n as one block led by prefix, none when
// it reads as no text.
func (w *writer) textBlock(n *node, prefix string) []block {
	text, page := w.inline(n)
	if text == "" {
		return nil
	}
	if prefix == "" {
		text = escapeLineStart(text)
	}
	return []block{{text: prefix + text, page: page}}
}

// list renders an L element (ISO 32000-1, 14.8.4.3.3).
func (w *writer) list(n *node) []block {
	ordered, orderedKnown := numberingOrdered(n.numbering)
	type item struct {
		label  string
		blocks []block
	}
	var items []item
	for _, kid := range n.kids {
		if kid.role == "L" && len(items) > 0 {
			// A list set straight in a list continues the item before it.
			last := &items[len(items)-1]
			last.blocks = append(last.blocks, w.list(kid)...)
			continue
		}
		it := item{}
		rest := &node{role: "LBody"}
		for _, part := range itemParts(kid) {
			if part.role == "Lbl" && it.label == "" {
				it.label, _ = w.inline(part)
				continue
			}
			rest.kids = append(rest.kids, part)
		}
		if holdsBlock(rest) {
			it.blocks = w.container(rest)
		} else {
			it.blocks = w.textBlock(rest, "")
		}
		if len(it.blocks) > 0 || it.label != "" {
			items = append(items, it)
		}
	}
	if len(items) == 0 {
		return nil
	}

	if !orderedKnown {
		if items[0].label != "" {
			ordered = orderedLabel.MatchString(unescape(items[0].label))
		} else if len(items[0].blocks) > 0 {
			if m := listMarker.FindString(items[0].blocks[0].text); m != "" {
				ordered = orderedLabel.MatchString(strings.TrimSpace(unescape(m)))
			}
		}
	}
	start := 1
	if ordered && items[0].label != "" {
		if digits := leadingDigits.FindString(unescape(items[0].label)); digits != "" {
			if v, err := strconv.Atoi(digits); err == nil && v < 1_000_000_000 {
				start = v
			}
		}
	}

	var lines []string
	page := -1
	for i, it := range items {
		marker := "- "
		if ordered {
			marker = strconv.Itoa(start+i) + ". "
		}
		indent := strings.Repeat(" ", len(marker))
		if len(it.blocks) == 0 {
			lines = append(lines, strings.TrimRight(marker, " "))
			continue
		}
		if page < 0 {
			page = it.blocks[0].page
		}
		for j, b := range it.blocks {
			text := b.text
			if j == 0 && it.label == "" {
				// The layout leaves an item's marker in its text.
				text = escapeLineStart(listMarker.ReplaceAllString(text, ""))
			}
			if j == 0 {
				lines = append(lines, marker+prefixLines(text, indent, "")[len(indent):])
			} else {
				lines = append(lines, prefixLines(text, indent, ""))
			}
		}
	}
	return []block{{text: strings.Join(lines, "\n"), page: page}}
}

// itemParts returns the parts of a list item: the kids of an LI, or the item
// itself when the list holds an element of another type.
func itemParts(n *node) []*node {
	if n.role == "LI" {
		return n.kids
	}
	return []*node{n}
}

// numberingOrdered reads a /ListNumbering value (ISO 32000-1, Table 347):
// whether it numbers the items, and whether it says.
func numberingOrdered(numbering string) (ordered, known bool) {
	switch numbering {
	case "Decimal", "UpperRoman", "LowerRoman", "UpperAlpha", "LowerAlpha":
		return true, true
	case "Disc", "Circle", "Square":
		return false, true
	}
	return false, false
}

var (
	// listMarker matches the marker that leads a list item's text, as
	// Markdown escaping leaves it: a bullet, or a number or a letter closed
	// by a period or a parenthesis.
	listMarker = regexp.MustCompile(`^(?:\\?[-*+•◦▪▫‣⁃●○■□–—·]|\\?\(?[0-9]{1,9}\\?[.)]|\\?\(?[A-Za-z]\\?[.)])\s+`)
	// orderedLabel matches a label that numbers its item.
	orderedLabel  = regexp.MustCompile(`^\(?(?:[0-9]{1,9}|[A-Za-z]|[ivxlcdmIVXLCDM]{1,8})[.)]?$`)
	leadingDigits = regexp.MustCompile(`[0-9]{1,9}`)
)

// table renders a Table element as a GitHub Flavored Markdown table, its
// first row the header row.
func (w *writer) table(n *node) []block {
	var rows [][]string
	page := -1
	var collect func(*node)
	collect = func(n *node) {
		for _, kid := range n.kids {
			if kid.role != "TR" {
				collect(kid)
				continue
			}
			var row []string
			for _, cell := range kid.kids {
				text, p := w.inline(cell)
				if page < 0 {
					page = p
				}
				row = append(row, strings.ReplaceAll(text, "|", `\|`))
			}
			if len(row) > 0 {
				rows = append(rows, row)
			}
		}
	}
	collect(n)
	if len(rows) == 0 {
		return nil
	}
	cols := 0
	for _, row := range rows {
		cols = max(cols, len(row))
	}
	var sb strings.Builder
	writeRow := func(cells []string) {
		sb.WriteByte('|')
		for i := range cols {
			cell := ""
			if i < len(cells) {
				cell = cells[i]
			}
			sb.WriteString(" " + cell + " |")
		}
		sb.WriteByte('\n')
	}
	writeRow(rows[0])
	sb.WriteString("|" + strings.Repeat(" --- |", cols) + "\n")
	for _, row := range rows[1:] {
		writeRow(row)
	}
	return []block{{text: strings.TrimSuffix(sb.String(), "\n"), page: page}}
}

// markUsed marks the glyphs of n written, so they are not kept as text the
// structure does not reach.
func (w *writer) markUsed(n *node) {
	for _, g := range n.glyphs {
		w.used[g.page][g.index] = true
	}
	for _, kid := range n.kids {
		w.markUsed(kid)
	}
}

// firstPage returns the page of the first glyph n holds, -1 when it holds
// none.
func firstPage(n *node) int {
	if len(n.glyphs) > 0 {
		return n.glyphs[0].page
	}
	for _, kid := range n.kids {
		if p := firstPage(kid); p >= 0 {
			return p
		}
	}
	return -1
}

// withLeftovers places, after the blocks of each page, the text of the page
// no block holds: content the structure does not reach, such as a form
// XObject's text in an untagged page or a tagged page's artifacts, and the
// text recognized on a scanned page.
func (w *writer) withLeftovers(body []block) []block {
	var out []block
	next := 0
	flushThrough := func(page int) {
		for ; next <= page && next < len(w.pages); next++ {
			out = append(out, w.leftovers(next)...)
		}
	}
	for _, b := range body {
		if b.page >= 0 {
			flushThrough(b.page - 1)
		}
		out = append(out, b)
	}
	flushThrough(len(w.pages) - 1)
	return out
}

// leftovers returns a paragraph for each line of the text no block of page
// holds.
func (w *writer) leftovers(page int) []block {
	var indices []int
	for i, used := range w.used[page] {
		if !used {
			indices = append(indices, i)
		}
	}
	text := w.pages[page].Recognized
	if len(indices) > 0 {
		text = w.pages[page].Shown.TextOf(w.ctx, indices)
	}
	var out []block
	for line := range strings.SplitSeq(text, "\n") {
		if line = collapse(line); line != "" {
			out = append(out, block{text: escapeLineStart(escapeInline(line)), page: page})
		}
	}
	return out
}

// joinBlocks joins blocks with a blank line between them.
func joinBlocks(blocks []block) string {
	texts := make([]string, len(blocks))
	for i, b := range blocks {
		texts[i] = b.text
	}
	out := strings.Join(texts, "\n\n")
	if out != "" {
		out += "\n"
	}
	return out
}

// prefixLines prefixes each line of text, and a blank line with blank.
func prefixLines(text, prefix, blank string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if line == "" {
			lines[i] = blank
		} else {
			lines[i] = prefix + line
		}
	}
	return strings.Join(lines, "\n")
}
