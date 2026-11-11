package sheet

import (
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compose"
)

// sheetLink marks the target of a cell's link into the book itself while
// the pages are laid out: the layout engine keeps it as a URI, and
// [resolveSheetLinks] turns it into the page that shows its cell.
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

// resolveSheetLinks turns the links of out's pages that mark a target in
// the book into links to the page that prints their cell, or the first
// page of their sheet, as LibreOffice exports a link into the book; a
// link to a sheet or a name that prints no page is dropped. pages are the
// planned pages, at[k] the 1-based page of out pages[k] is, tables[i] the
// Table element of page i+1, which the link names as its structure
// destination (ISO 14289-2, 8.8).
//
// # Corpus evidence
//
//   - libreoffice/xlsx: sc/hyperlinks.xlsx links "#Offering!$B$2", which LibreOffice
//     exports as a link to page 3, the first of sheet Offering.
func resolveSheetLinks(doc *Document, out *compose.Document, pages []*pagePlan, at []int, tables []*compose.StructElem) {
	for _, pg := range out.Pages {
		links := pg.Links[:0]
		for _, l := range pg.Links {
			target, ok := strings.CutPrefix(l.URI, sheetLink)
			if !ok {
				links = append(links, l)
				continue
			}
			if page := targetPage(doc, target, pages, at); page > 0 {
				l.URI, l.Dest = "", &compose.Dest{Page: page, Elem: tables[page-1]}
				links = append(links, l)
			}
		}
		pg.Links = links
	}
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
