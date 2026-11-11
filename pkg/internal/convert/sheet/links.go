package sheet

import (
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compose"
)

// sheetLink marks the target of a cell's link into the book itself while
// the pages are laid out: the layout engine keeps it as a URI, and
// a [sheetLinker] turns it into the page that shows its cell.
const sheetLink = "x-litho-sheet:"

// cellHref returns the link target a cell's Href is laid out with: a
// target in the book ("#Sheet!A1") marked as a [sheetLink], any other
// unchanged.
func cellHref(href string) string {
	if rest, ok := strings.CutPrefix(href, "#"); ok {
		return sheetLink + rest
	}
	return href
}

// sheetTarget reads a link target in the book: a sheet's name, quoted or
// not, then optionally "!" (a workbook's) or "." (an OpenDocument's) and
// an A1 cell, its "$" signs ignored (ECMA-376 Part 1, 18.3.1.47 location;
// ODF 1.3 Part 3, 19.910 xlink:href). It returns the sheet's index in doc,
// the cell, whether a cell is named, and false when no sheet has that
// name.
func sheetTarget(doc *Document, target string) (sheet, row, col int, cell, ok bool) {
	name, ref := target, ""
	if strings.HasPrefix(target, "'") {
		if end := strings.Index(target[1:], "'"); end >= 0 {
			name, ref = target[1:end+1], strings.TrimLeft(target[end+2:], "!.")
		}
	} else if i := strings.LastIndexAny(target, "!."); i >= 0 {
		name, ref = target[:i], target[i+1:]
		if sheetIndex(doc, name) < 0 && sheetIndex(doc, target) >= 0 {
			name, ref = target, ""
		}
	}
	sheet = sheetIndex(doc, name)
	if sheet < 0 {
		return 0, 0, 0, false, false
	}
	if ref != "" {
		p := &fparser{src: ref}
		if r, c, okRef := p.cellRef(); okRef {
			return sheet, r, c, true, true
		}
	}
	return sheet, 0, 0, false, true
}

// sheetIndex returns the index of the sheet of doc named name, ignoring
// case, -1 for none.
func sheetIndex(doc *Document, name string) int {
	for i, sh := range doc.Sheets {
		if sh != nil && strings.EqualFold(sh.Name, name) {
			return i
		}
	}
	return -1
}

// sheetLinker turns the links of the pages that mark a target in the
// book into links to the page that prints their cell, or the first page
// of their sheet, as LibreOffice exports a link into the book; a link to a
// sheet or a name that prints no page is dropped. pages are the planned
// pages, at[k] the 1-based page pages[k] is, tables[i] the Table element
// of page i+1, which the link names as its structure destination (ISO
// 14289-2, 8.8).
//
// A page's links are resolved once the page is laid out, before it is
// handed over: a link to a later page names that page's Table once the
// page is laid out, through the destination it keeps in waiting, which
// the sink reads at the end.
//
// # Corpus evidence
//
//   - libreoffice/xlsx: sc/hyperlinks.xlsx links "#Offering!$B$2", which LibreOffice
//     exports as a link to page 3, the first of sheet Offering.
type sheetLinker struct {
	doc    *Document
	pages  []*pagePlan
	at     []int
	tables []*compose.StructElem
	// waiting holds, by 1-based page, the destinations to that page made
	// before it was laid out.
	waiting map[int][]*compose.Dest
}

// table records t as the Table element of the next page laid out and
// gives it to the destinations waiting for that page.
func (sl *sheetLinker) table(t *compose.StructElem) {
	sl.tables = append(sl.tables, t)
	page := len(sl.tables)
	for _, d := range sl.waiting[page] {
		d.Elem = t
	}
	delete(sl.waiting, page)
}

// resolve resolves the links of pg, the last page laid out.
func (sl *sheetLinker) resolve(pg *compose.Page) {
	links := pg.Links[:0]
	for _, l := range pg.Links {
		target, ok := strings.CutPrefix(l.URI, sheetLink)
		if !ok {
			links = append(links, l)
			continue
		}
		if page := targetPage(sl.doc, target, sl.pages, sl.at); page > 0 {
			d := &compose.Dest{Page: page}
			if page <= len(sl.tables) {
				d.Elem = sl.tables[page-1]
			} else {
				sl.waiting[page] = append(sl.waiting[page], d)
			}
			l.URI, l.Dest = "", d
			links = append(links, l)
		}
	}
	pg.Links = links
}

// targetPage returns the 1-based page that prints the link target in the
// book target, 0 for none.
func targetPage(doc *Document, target string, pages []*pagePlan, at []int) int {
	si, r, c, cell, ok := sheetTarget(doc, target)
	if !ok {
		return 0
	}
	first := 0
	for k, p := range pages {
		if p.sp.idx != si || k >= len(at) || at[k] == 0 {
			continue
		}
		if first == 0 {
			first = at[k]
		}
		if cell && r >= p.rows.first && r <= p.rows.last && c >= p.cols.first && c <= p.cols.last {
			return at[k]
		}
	}
	return first
}
