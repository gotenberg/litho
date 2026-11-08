package xlsx

import (
	"fmt"
	"sort"

	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
)

// relTable is the relationship type of a table part (ECMA-376 Part 1,
// 12.3.21).
const relTable = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/table"

// maxTables caps the tables of one sheet. Anti-DoS guard, not
// spec-mandated: a sheet holds a few tables.
const maxTables = 256

// tables reads the sheet's table parts (ECMA-376 Part 1, 18.5.1.2 table):
// a header cell the sheet leaves empty shows its column's name, as
// LibreOffice writes the names of a database range's columns into its
// header row, and the table's style (18.5.1.5 tableStyleInfo) formats its
// cells, as LibreOffice draws a database range's table style.
//
// # Corpus evidence
//
//   - xlsx: sc/TableEmptyHeaders.xlsx prints Column1 and Column2 over
//     header cells its sheet data leaves empty; sc/TableStyleTest.xlsx
//     prints TableStyleMedium2's dark blue header row of white bold text
//     and its light blue banded rows.
func (w *sheetReader) tables(parts *ooxml.Element) error {
	r := w.r
	n := 0
	for _, tp := range parts.Kids() {
		if !tp.Is(nsMain, "tablePart") || n >= maxTables {
			continue
		}
		n++
		id, _ := tp.AttrNS(ooxml.NSR, "id")
		rel, ok := r.pkg.RelationshipByID(w.part, id)
		if !ok || rel.External || rel.Type != relTable || !r.pkg.Has(rel.Target) {
			continue
		}
		root, err := r.pkg.ParseXML(r.ctx, rel.Target, understood)
		if err != nil {
			if isMalformed(err) {
				r.warn("malformed-part", fmt.Sprintf("table %s does not parse: %v", rel.Target, err))
				continue
			}
			return err
		}
		rg, ok := rangeRef(attr(root, "ref"))
		if !ok {
			continue
		}
		info := tableInfo{area: rg, header: intAttr(root, "headerRowCount", 1) != 0, total: intAttr(root, "totalsRowCount", 0) > 0}
		if si := root.Child(nsMain, "tableStyleInfo"); si != nil {
			info.firstCol, info.lastCol = boolAttr(si, "showFirstColumn"), boolAttr(si, "showLastColumn")
			info.rowStripes, info.colStripes = boolAttr(si, "showRowStripes"), boolAttr(si, "showColumnStripes")
			if ts := r.tableStyleNamed(attr(si, "name")); ts != nil {
				w.styleTable(ts, info)
			}
		}
		if !info.header {
			continue
		}
		col := rg.FirstColumn
		for _, c := range root.Child(nsMain, "tableColumns").Kids() {
			if !c.Is(nsMain, "tableColumn") {
				continue
			}
			if col > rg.LastColumn {
				break
			}
			if name := decodeXString(attr(c, "name")); name != "" {
				w.setEmptyText(rg.FirstRow, col, name)
			}
			col++
		}
	}
	return nil
}

// row returns the row at index row, added when the sheet lacks it.
func (w *sheetReader) row(row int) *sheet.Row {
	sh := w.sh
	if at, ok := w.rowAt[row]; ok {
		return &sh.Rows[at]
	}
	sh.Rows = append(sh.Rows, sheet.Row{Index: row})
	sort.SliceStable(sh.Rows, func(i, j int) bool { return sh.Rows[i].Index < sh.Rows[j].Index })
	for i, rw := range sh.Rows {
		w.rowAt[rw.Index] = i
	}
	return &sh.Rows[w.rowAt[row]]
}

// setEmptyText gives the cell at row and col the text s when it holds no
// value, adding the cell and the row when the sheet lacks them.
func (w *sheetReader) setEmptyText(row, col int, s string) {
	rw := w.row(row)
	for i := range rw.Cells {
		if rw.Cells[i].Column == col {
			if rw.Cells[i].Value.Kind == sheet.Empty {
				rw.Cells[i].Value = sheet.Value{Kind: sheet.Text, Text: s}
			}
			return
		}
	}
	rw.Cells = dedupCells(append(rw.Cells, sheet.Cell{Column: col, Value: sheet.Value{Kind: sheet.Text, Text: s}}))
}
