package compliance

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// validateTableContainment checks PDF/UA table containment rules.
func validateTableContainment(state *walkState, typeName ast.PdfName, parentType ast.PdfName) {
	switch typeName {
	case "TR":
		// TR must be inside Table, THead, TBody, or TFoot.
		switch parentType {
		case "Table", "THead", "TBody", "TFoot":
			// OK
		default:
			if parentType != "" {
				// §7.2 t4: TR parent must be Table/THead/TBody/TFoot.
				*state.results = append(*state.results,
					failf(Rule_14289_1_7_2_t04, "TR inside %s", parentType)...)
			}
		}
	case "THead":
		// §7.2 t5: THead must be inside Table.
		if parentType != "Table" && parentType != "" {
			*state.results = append(*state.results,
				failf(Rule_14289_1_7_2_t05, "THead inside %s", parentType)...)
		}
	case "TBody":
		// §7.2 t6: TBody must be inside Table.
		if parentType != "Table" && parentType != "" {
			*state.results = append(*state.results,
				failf(Rule_14289_1_7_2_t06, "TBody inside %s", parentType)...)
		}
	case "TFoot":
		// §7.2 t7: TFoot must be inside Table.
		if parentType != "Table" && parentType != "" {
			*state.results = append(*state.results,
				failf(Rule_14289_1_7_2_t07, "TFoot inside %s", parentType)...)
		}
	case "TD", "TH":
		// §7.2 t8 (TH) / t9 (TD): cell parent must be TR.
		if parentType != "TR" && parentType != "" {
			rule := Rule_14289_1_7_2_t09
			if typeName == "TH" {
				rule = Rule_14289_1_7_2_t08
			}
			*state.results = append(*state.results,
				failf(rule, "%s inside %s instead of TR", typeName, parentType)...)
		}
	}
}

// tableSectionChildRule selects the "section may contain only TR" rule by table
// section type: PDF/UA-1 §7.2 t36 (THead), t37 (TBody), t38 (TFoot).
func tableSectionChildRule(parentType ast.PdfName) Rule {
	switch parentType {
	case "THead":
		return Rule_14289_1_7_2_t36
	case "TBody":
		return Rule_14289_1_7_2_t37
	case "TFoot":
		return Rule_14289_1_7_2_t38
	}
	return Rule{}
}

// validateTableColumnConsistency checks that all TR rows in a Table have the
// same number of columns, accounting for ColSpan attributes on TD/TH cells.
// PDF/UA-1 §7.2 t43 and PDF/UA-2 §8.2.5.26 t3/t4.
// validateTableColumnConsistency checks table regularity (§8.2.5.26 / §7.2) and
// reports whether the table is column-regular. Header determination (t5/t6)
// assumes a rectangular grid, so an irregular table makes the positional
// header model meaningless. This function returns false when it fires a
// regularity violation so the caller skips header determination.
func validateTableColumnConsistency(ctx context.Context, state *walkState, tableElem ast.PdfDict) bool {
	rows := collectTableRows(ctx, state.doc, tableElem)
	if len(rows) < 2 {
		return true
	}

	// Under PDF/UA-2 a /RowSpan reaching past the last row of its THead, TBody
	// or TFoot is a row-span defect rather than a column-count one: ISO 14289-2,
	// 8.2.5.26 t2 requires the groupings those sections form to be regular.
	// Carrying such a span into the next section would widen its first row and
	// report the column rule instead, which is the wrong test of the clause.
	// PDF/UA-1 draws no section boundary here and reports its column rule §7.2
	// t42 for the same table, which is measured rather than assumed, so the
	// whole branch is scoped to PDF/UA-2.
	if state.profile == "PDF/UA-2" {
		if row, ok := rowSpanCrossingSection(ctx, state.doc, tableElem, rows); ok {
			*state.results = append(*state.results,
				failf(Rule_14289_2_8_2_5_26_t02,
					"Table row %d has a RowSpan reaching past the end of its section", row+1)...)
			return false
		}
	}

	// Build effective column count per row using a proper grid model.
	// RowSpan causes a cell to occupy column positions in subsequent rows.
	// Columns "occupied" by RowSpan carry-over affect effective counts.
	effectiveCols := computeEffectiveColumns(ctx, state.doc, rows)
	if len(effectiveCols) < 2 || effectiveCols[0] == 0 {
		return true
	}

	// The first row's column count defines the table width. Split the
	// violation by direction: a row WIDER than the first means a column span
	// overflows the grid, which fires the overflow rule; a NARROWER or empty
	// row leaves grid cells unfilled, which fires the gap rule. Overflow is
	// reported first because a span exceeding the grid is detectable during
	// cell placement, before the trailing empty-cell scan.
	n0 := effectiveCols[0]
	widerRow, narrowerRow := -1, -1
	for i := 1; i < len(effectiveCols); i++ {
		if effectiveCols[i] > n0 && widerRow < 0 {
			widerRow = i
		}
		if effectiveCols[i] < n0 && narrowerRow < 0 {
			narrowerRow = i
		}
	}
	if widerRow < 0 && narrowerRow < 0 {
		// Every row the table declares agrees on its column count, but a
		// /RowSpan may still reach past the last one. Those rows exist in the
		// grid without a TR to declare them, so comparing declared rows cannot
		// see the defect at all.
		return !reportRowSpanOverrun(ctx, state, rows)
	}
	wider := widerRow >= 0
	row := narrowerRow
	if wider {
		row = widerRow
	}
	rule := tableColumnRule(state.profile, wider)
	*state.results = append(*state.results,
		failf(rule, "Table row 1 has %d columns, row %d has %d columns",
			n0, row+1, effectiveCols[row])...)
	return false
}

// rowSpanCrossingSection reports the first row holding a cell whose /RowSpan
// reaches past the last row of the THead, TBody or TFoot it belongs to. Rows
// placed directly under the Table belong to no section and are exempt, since
// there is no grouping for the span to escape.
func rowSpanCrossingSection(ctx context.Context, doc *document.Document, tableElem ast.PdfDict, rows []ast.PdfDict) (int, bool) {
	ends := tableSectionRowEnds(ctx, doc, tableElem)
	if len(ends) == 0 {
		return 0, false
	}
	for ri, row := range rows {
		if ctx.Err() != nil {
			return 0, false
		}
		end, sectioned := ends[ri]
		if !sectioned {
			continue
		}
		for _, cell := range resolveRowCells(ctx, doc, row) {
			if rs := cellRowSpan(ctx, doc, cell); rs > 1 && ri+rs-1 > end {
				return ri, true
			}
		}
	}
	return 0, false
}

// tableSectionRowEnds maps each row index that sits inside a THead, TBody or
// TFoot to the last row index of that section. Rows directly under the Table
// are absent from the map. The row order matches [collectTableRows], which
// walks the same children in the same order.
func tableSectionRowEnds(ctx context.Context, doc *document.Document, tableElem ast.PdfDict) map[int]int {
	ends := make(map[int]int)
	index := 0

	children, err := document.ResolveArray(ctx, doc, tableElem["K"])
	if err != nil {
		child, derr := document.ResolveDict(ctx, doc, tableElem["K"])
		if derr != nil || child == nil {
			return ends
		}
		children = ast.PdfArray{child}
	}

	for _, item := range children {
		child, err := document.ResolveDict(ctx, doc, item)
		if err != nil || child == nil {
			continue
		}
		typeName, _ := document.ResolveName(ctx, doc, child["S"])
		if typeName == "TR" {
			index++
			continue
		}
		if typeName != "THead" && typeName != "TBody" && typeName != "TFoot" {
			continue
		}
		var sectionRows []ast.PdfDict
		collectRowsFrom(ctx, doc, child, &sectionRows)
		if len(sectionRows) == 0 {
			continue
		}
		last := index + len(sectionRows) - 1
		for i := index; i <= last; i++ {
			ends[i] = last
		}
		index = last + 1
	}
	return ends
}

// computeEffectiveColumns builds the effective column count for each row,
// accounting for both ColSpan and RowSpan. Uses a grid model where
// carry[col] tracks how many additional rows a column position is occupied.
func computeEffectiveColumns(ctx context.Context, doc *document.Document, rows []ast.PdfDict) []int {
	result := make([]int, len(rows))
	// carry[col] = remaining rows this column is occupied by a previous RowSpan.
	carry := make(map[int]int)

	for ri, row := range rows {
		colPos := 0 // Current column position in the grid.

		// Get cells for this row.
		cells := resolveRowCells(ctx, doc, row)

		for _, cell := range cells {
			// Skip columns occupied by RowSpan from previous rows.
			for carry[colPos] > 0 {
				colPos++
			}

			cs := cellColSpan(ctx, doc, cell)
			rs := cellRowSpan(ctx, doc, cell)

			// This cell occupies colPos..colPos+cs-1. Set carry to rs
			// (not rs-1) because the end-of-row decrement will subtract 1.
			if rs > 1 {
				for c := range cs {
					carry[colPos+c] = rs
				}
			}
			colPos += cs
		}

		// Count remaining carried columns after all explicit cells.
		for carry[colPos] > 0 {
			colPos++
		}

		result[ri] = colPos

		// Decrement carry counters for next row.
		for k, v := range carry {
			if v <= 1 {
				delete(carry, k)
			} else {
				carry[k] = v - 1
			}
		}
	}
	return result
}

// resolveRowCells extracts TD/TH cell dicts from a TR element.
func resolveRowCells(ctx context.Context, doc *document.Document, tr ast.PdfDict) []ast.PdfDict {
	kObj := tr["K"]
	if kObj == nil {
		return nil
	}
	arr, err := document.ResolveArray(ctx, doc, kObj)
	if err != nil {
		d, _ := document.ResolveDict(ctx, doc, kObj)
		if d != nil {
			return []ast.PdfDict{d}
		}
		return nil
	}
	var cells []ast.PdfDict
	for _, item := range arr {
		d, _ := document.ResolveDict(ctx, doc, item)
		if d == nil {
			continue
		}
		typeName, _ := document.ResolveName(ctx, doc, d["S"])
		if typeName == "TD" || typeName == "TH" {
			cells = append(cells, d)
		}
	}
	return cells
}

// collectTableRows extracts all TR elements from a Table, including those
// nested inside THead, TBody, and TFoot sections.
func collectTableRows(ctx context.Context, doc *document.Document, tableElem ast.PdfDict) []ast.PdfDict {
	var rows []ast.PdfDict
	kObj := tableElem["K"]
	if kObj == nil {
		return nil
	}

	arr, err := document.ResolveArray(ctx, doc, kObj)
	if err != nil {
		// Single child.
		dict, err := document.ResolveDict(ctx, doc, kObj)
		if err == nil && dict != nil {
			collectRowsFrom(ctx, doc, dict, &rows)
		}
		return rows
	}

	for _, item := range arr {
		dict, err := document.ResolveDict(ctx, doc, item)
		if err != nil || dict == nil {
			continue
		}
		collectRowsFrom(ctx, doc, dict, &rows)
	}
	return rows
}

func collectRowsFrom(ctx context.Context, doc *document.Document, elem ast.PdfDict, rows *[]ast.PdfDict) {
	typeName, _ := document.ResolveName(ctx, doc, elem["S"])
	if typeName == "TR" {
		*rows = append(*rows, elem)
		return
	}
	// THead, TBody, TFoot contain TR children.
	if typeName == "THead" || typeName == "TBody" || typeName == "TFoot" {
		kObj := elem["K"]
		if kObj == nil {
			return
		}
		arr, err := document.ResolveArray(ctx, doc, kObj)
		if err != nil {
			dict, _ := document.ResolveDict(ctx, doc, kObj)
			if dict != nil {
				collectRowsFrom(ctx, doc, dict, rows)
			}
			return
		}
		for _, item := range arr {
			dict, _ := document.ResolveDict(ctx, doc, item)
			if dict != nil {
				collectRowsFrom(ctx, doc, dict, rows)
			}
		}
	}
}

// cellColSpan returns the ColSpan value for a TD/TH cell (default 1).
func cellColSpan(ctx context.Context, doc *document.Document, cell ast.PdfDict) int {
	// ColSpan is in the cell's attribute dict /A or table attribute array.
	aObj := cell["A"]
	if aObj == nil {
		return 1
	}
	// /A can be a dict or array of dicts.
	aDict, err := document.ResolveDict(ctx, doc, aObj)
	if err == nil && aDict != nil {
		if cs, _ := document.ResolveInt(ctx, doc, aDict["ColSpan"]); cs > 0 {
			return int(cs)
		}
		return 1
	}
	aArr, err := document.ResolveArray(ctx, doc, aObj)
	if err != nil {
		return 1
	}
	for _, item := range aArr {
		d, _ := document.ResolveDict(ctx, doc, item)
		if d == nil {
			continue
		}
		if cs, _ := document.ResolveInt(ctx, doc, d["ColSpan"]); cs > 0 {
			return int(cs)
		}
	}
	return 1
}

// cellRowSpan returns the RowSpan value for a TD/TH cell (default 1).
func cellRowSpan(ctx context.Context, doc *document.Document, cell ast.PdfDict) int {
	aObj := cell["A"]
	if aObj == nil {
		return 1
	}
	aDict, err := document.ResolveDict(ctx, doc, aObj)
	if err == nil && aDict != nil {
		if rs, _ := document.ResolveInt(ctx, doc, aDict["RowSpan"]); rs > 0 {
			return int(rs)
		}
		return 1
	}
	aArr, err := document.ResolveArray(ctx, doc, aObj)
	if err != nil {
		return 1
	}
	for _, item := range aArr {
		d, _ := document.ResolveDict(ctx, doc, item)
		if d == nil {
			continue
		}
		if rs, _ := document.ResolveInt(ctx, doc, d["RowSpan"]); rs > 0 {
			return int(rs)
		}
	}
	return 1
}

// tableColumnRule selects the irregular-table rule by profile and direction.
// A row wider than the first fires the overflow variant (ISO 14289-2
// §8.2.5.26 t03 / ISO 14289-1 §7.2 t42); a narrower or empty row fires the
// gap variant (t04 / t43).
func tableColumnRule(profile string, wider bool) Rule {
	if profile == "PDF/UA-2" {
		if wider {
			return Rule_14289_2_8_2_5_26_t03
		}
		return Rule_14289_2_8_2_5_26_t04
	}
	if wider {
		return Rule_14289_1_7_2_t42
	}
	return Rule_14289_1_7_2_t43
}

// validateTableChildren checks Table children containment. Split from
// validateTableContainment for readability.
func validateTableChildren(state *walkState, typeName ast.PdfName, parentType ast.PdfName) {
	// Table children: Table may contain only TR, THead, TBody, TFoot, Caption.
	// THead/TBody/TFoot must contain only TR.
	// TR must contain only TH and TD.
	switch parentType {
	case "Table":
		switch typeName {
		case "TR", "THead", "TBody", "TFoot", "Caption":
			// Valid Table children.
		default:
			*state.results = append(*state.results,
				failf(Rule_14289_1_7_2_t03, "Table contains %s instead of TR/THead/TBody/TFoot/Caption", typeName)...)
		}
	case "THead", "TBody", "TFoot":
		// §7.2 t36 (THead) / t37 (TBody) / t38 (TFoot): may contain only TR.
		if typeName != "TR" && typeName != "Caption" {
			*state.results = append(*state.results,
				failf(tableSectionChildRule(parentType), "%s contains %s instead of TR", parentType, typeName)...)
		}
	case "TR":
		switch typeName {
		case "TH", "TD":
			// Valid TR children.
		default:
			*state.results = append(*state.results,
				failf(Rule_14289_1_7_2_t10, "TR contains %s instead of TH/TD", typeName)...)
		}
	}
}

// validateTableSectionCounts checks that a Table has at most 1 THead, 1 TFoot,
// and that TFoot/THead require TBody (§7.2 t11-t14).
func validateTableSectionCounts(ctx context.Context, state *walkState, tableElem ast.PdfDict) {
	kObj, ok := tableElem["K"]
	if !ok {
		return
	}
	arr, err := document.ResolveArray(ctx, state.doc, kObj)
	if err != nil {
		return
	}

	var theadCount, tfootCount, tbodyCount, captionCount int
	for _, item := range arr {
		d, err := document.ResolveDict(ctx, state.doc, item)
		if err != nil || d == nil {
			continue
		}
		childType, _ := document.ResolveName(ctx, state.doc, d["S"])
		switch childType {
		case "THead":
			theadCount++
		case "TFoot":
			tfootCount++
		case "TBody":
			tbodyCount++
		case "Caption":
			captionCount++
		}
	}

	if theadCount > 1 {
		*state.results = append(*state.results,
			failf(Rule_14289_1_7_2_t11, "Table has %d THead children (max 1)", theadCount)...)
	}
	if tfootCount > 1 {
		*state.results = append(*state.results,
			failf(Rule_14289_1_7_2_t12, "Table has %d TFoot children (max 1)", tfootCount)...)
	}
	if tfootCount > 0 && tbodyCount == 0 {
		*state.results = append(*state.results,
			failf(Rule_14289_1_7_2_t13, "Table has TFoot but no TBody")...)
	}
	if theadCount > 0 && tbodyCount == 0 {
		*state.results = append(*state.results,
			failf(Rule_14289_1_7_2_t14, "Table has THead but no TBody")...)
	}
	if captionCount > 1 {
		*state.results = append(*state.results,
			failf(Rule_14289_1_7_2_t39, "Table has %d Caption children (max 1)", captionCount)...)
	}
}

// maxTableGridSlots bounds the notional slot count (rows times columns) of the
// grid built for the cell-intersection scan. /ColSpan and /RowSpan are read
// from the file, so a crafted table can declare a grid of billions of slots.
// This is an anti-DoS guard, not spec-mandated: a real tagged table stays far
// below a million slots, since even a 2000-row, 500-column table is an
// outlier, so a table above the cap is left unscanned rather than expanded.
const maxTableGridSlots = 1 << 20

// tableGridSpan is one cell's grid extent, in the units the intersection scan
// places cells with.
type tableGridSpan struct {
	row     int
	col     int
	colSpan int
	rowSpan int
}

// tableGridRowCount returns the grid height, and reports false when the
// declared spans exceed [maxTableGridSlots]. Only the first TD/TH of each row
// contributes: a cell with /RowSpan n claims the next n-1 rows, which are then
// skipped. This mirrors the height veraPDF derives for ISO 14289-1 §7.2 t15,
// and the rule is a verdict on cell placement, so the grid it is evaluated
// against has to be the same one.
func tableGridRowCount(ctx context.Context, doc *document.Document, rows []ast.PdfDict) (int, bool) {
	count := 0
	for i := 0; i < len(rows); i++ {
		if ctx.Err() != nil {
			return 0, false
		}
		for _, cell := range resolveRowCells(ctx, doc, rows[i]) {
			rowSpan := cellRowSpan(ctx, doc, cell)
			if rowSpan > maxTableGridSlots || count > maxTableGridSlots-rowSpan {
				return 0, false
			}
			count += rowSpan
			if rowSpan > 1 {
				i += rowSpan - 1
			}
			break
		}
	}
	return count, true
}

// tableGridColumnCount returns the grid width, the sum of the /ColSpan values
// of the first row's cells, and reports false when that sum exceeds
// [maxTableGridSlots].
func tableGridColumnCount(ctx context.Context, doc *document.Document, row ast.PdfDict) (int, bool) {
	count := 0
	for _, cell := range resolveRowCells(ctx, doc, row) {
		colSpan := cellColSpan(ctx, doc, cell)
		if colSpan > maxTableGridSlots || count > maxTableGridSlots-colSpan {
			return 0, false
		}
		count += colSpan
	}
	return count, true
}

// tableCellIntersection places every TD/TH cell of the table on the grid and
// returns the first pair of cells that claim the same slot: the cell being
// placed, then the cell already holding the slot. found is false when the
// table has no such pair.
//
// Per ISO 14289-1 §7.2, a table cell shall not intersect another cell. A cell
// takes the first free slot of its row, then claims /RowSpan by /ColSpan slots
// from there, so a /RowSpan carried down from an earlier row shifts the
// following row's cells rather than colliding with them. An intersection needs
// a cell wide or tall enough to reach past a free slot into a claimed one.
//
// The scan stops without a verdict at the first cell whose span leaves the
// grid, and at the first row holding no cells: both are irregularities the
// table-level rules report (§7.2 t41 for a /RowSpan past the last row, t42/t43
// for a row whose columns do not add up), and a grid that is already
// inconsistent says nothing about intersection.
func tableCellIntersection(ctx context.Context, doc *document.Document, rows []ast.PdfDict) (placed, occupant tableGridSpan, found bool) {
	numRows, ok := tableGridRowCount(ctx, doc, rows)
	if !ok || numRows == 0 {
		return placed, occupant, false
	}
	numCols, ok := tableGridColumnCount(ctx, doc, rows[0])
	if !ok || numCols == 0 || numRows > maxTableGridSlots/numCols {
		return placed, occupant, false
	}

	grid := make(map[[2]int]tableGridSpan)
	for row := range rows {
		if ctx.Err() != nil {
			return placed, occupant, false
		}
		col, hasCell := 0, false
		for _, cell := range resolveRowCells(ctx, doc, rows[row]) {
			hasCell = true
			colSpan := cellColSpan(ctx, doc, cell)
			rowSpan := cellRowSpan(ctx, doc, cell)
			for col < numCols {
				if _, taken := grid[[2]int{row, col}]; !taken {
					break
				}
				col++
			}
			// The first half of each guard keeps the sum from overflowing;
			// both spans are capped by the grid they are compared against.
			if colSpan > numCols || col+colSpan > numCols {
				return placed, occupant, false
			}
			if rowSpan > numRows || row+rowSpan > numRows {
				return placed, occupant, false
			}
			span := tableGridSpan{row: row, col: col, colSpan: colSpan, rowSpan: rowSpan}
			for r := range rowSpan {
				for c := range colSpan {
					slot := [2]int{row + r, col + c}
					if held, taken := grid[slot]; taken {
						return span, held, true
					}
					grid[slot] = span
				}
			}
			col += colSpan
		}
		if !hasCell {
			return placed, occupant, false
		}
	}
	return placed, occupant, false
}

// validateTableCellIntersection fires ISO 14289-1 §7.2 t15 on each of the two
// cells that first claim the same grid slot. The rule is scoped to the cell,
// not the table, so an intersection is two findings: the cell being placed and
// the cell it lands on.
func validateTableCellIntersection(ctx context.Context, state *walkState, tableElem ast.PdfDict) {
	rows := collectTableRows(ctx, state.doc, tableElem)
	placed, occupant, found := tableCellIntersection(ctx, state.doc, rows)
	if !found {
		return
	}
	rule := Rule_14289_1_7_2_t15
	if state.profile == "PDF/UA-2" {
		rule = Rule_14289_2_8_2_5_26_t01
	}
	*state.results = append(*state.results,
		failf(rule, "table cell at row %d column %d intersects the cell at row %d column %d",
			placed.row+1, placed.col+1, occupant.row+1, occupant.col+1)...)
	*state.results = append(*state.results,
		failf(rule, "table cell at row %d column %d is intersected by the cell at row %d column %d",
			occupant.row+1, occupant.col+1, placed.row+1, placed.col+1)...)
}

// tableGridCell records one logical cell's placement in the table grid,
// using the same ColSpan/RowSpan carry model as computeEffectiveColumns.
type tableGridCell struct {
	elem    ast.PdfDict
	isTH    bool
	row     int
	col     int
	colSpan int
	rowSpan int
}

// buildTableGrid places every TD/TH cell of the given rows on the grid.
func buildTableGrid(ctx context.Context, doc *document.Document, rows []ast.PdfDict) []tableGridCell {
	var cells []tableGridCell
	carry := make(map[int]int)
	for ri, row := range rows {
		colPos := 0
		for _, cell := range resolveRowCells(ctx, doc, row) {
			for carry[colPos] > 0 {
				colPos++
			}
			cs := cellColSpan(ctx, doc, cell)
			rs := cellRowSpan(ctx, doc, cell)
			typeName, _ := document.ResolveName(ctx, doc, cell["S"])
			cells = append(cells, tableGridCell{
				elem: cell, isTH: typeName == "TH",
				row: ri, col: colPos, colSpan: cs, rowSpan: rs,
			})
			if rs > 1 {
				for c := range cs {
					carry[colPos+c] = rs
				}
			}
			colPos += cs
		}
		for k, v := range carry {
			if v <= 1 {
				delete(carry, k)
			} else {
				carry[k] = v - 1
			}
		}
	}
	return cells
}

// cellAttrValue returns the named key from a cell's /A attribute dict or
// array of dicts, or nil when absent.
func cellAttrValue(ctx context.Context, doc *document.Document, cell ast.PdfDict, key ast.PdfName) ast.PdfObject {
	aObj := cell["A"]
	if aObj == nil {
		return nil
	}
	if d, err := document.ResolveDict(ctx, doc, aObj); err == nil && d != nil {
		return d[key]
	}
	arr, err := document.ResolveArray(ctx, doc, aObj)
	if err != nil {
		return nil
	}
	for _, item := range arr {
		d, derr := document.ResolveDict(ctx, doc, item)
		if derr != nil || d == nil {
			continue
		}
		if v, ok := d[key]; ok {
			return v
		}
	}
	return nil
}

// cellHeaders returns the decoded /Headers ID list from a cell's attributes.
func cellHeaders(ctx context.Context, doc *document.Document, cell ast.PdfDict) []string {
	hObj := cellAttrValue(ctx, doc, cell, "Headers")
	if hObj == nil {
		return nil
	}
	arr, err := document.ResolveArray(ctx, doc, hObj)
	if err != nil {
		if s, ok := hObj.(ast.PdfString); ok {
			return []string{string(s)}
		}
		return nil
	}
	var out []string
	for _, item := range arr {
		if s, ok := item.(ast.PdfString); ok {
			out = append(out, string(s))
		}
	}
	return out
}

// cellScope returns the /Scope attribute name ("Row", "Column", "Both") or "".
func cellScope(ctx context.Context, doc *document.Document, cell ast.PdfDict) string {
	sObj := cellAttrValue(ctx, doc, cell, "Scope")
	if sObj == nil {
		return ""
	}
	name, err := document.ResolveName(ctx, doc, sObj)
	if err != nil {
		return ""
	}
	return string(name)
}

// overlaps reports whether two [start, start+span) grid ranges intersect.
func overlaps(aStart, aSpan, bStart, bSpan int) bool {
	return aStart < bStart+bSpan && bStart < aStart+aSpan
}

// validateTableHeaderDetermination checks PDF/UA-2 §8.2.5.26 t5/t6 on every
// TD of a table that contains TH cells. Per ISO 32000-2 Table 384, a TD is
// connected to a header when its /Headers list names a TH /ID in the same
// table, or when a TH /Scope covers the TD's grid position (Row: shared row;
// Column: shared column; Both: either). /Headers entries naming no TH in
// the table are unknown headers (t6); a TD with neither mechanism is
// unconnected (t5). A TH without /Scope connects nothing, because /Scope and
// /Headers are the only two header-association mechanisms ISO 32000-2 Table
// 384 defines: a header row that omits /Scope establishes no association.
func validateTableHeaderDetermination(ctx context.Context, state *walkState, tableElem ast.PdfDict) {
	rows := collectTableRows(ctx, state.doc, tableElem)
	if len(rows) == 0 {
		return
	}
	cells := buildTableGrid(ctx, state.doc, rows)

	thIDs := make(map[string]bool)
	hasTH := false
	// tableHasExplicitScope reports whether any TH carries a /Scope. veraPDF
	// applies the ISO 32000-2 §14.8.5.7 implicit-scope model (a no-/Scope TH
	// headers the cells below/right of it) only to a table that uses no explicit
	// /Scope at all. Once any TH carries /Scope the table is in explicit mode, and
	// a TH that omits /Scope connects nothing: §8.2.5.26 t5/t6 then fire on a TD
	// whose only positional headers are those no-/Scope THs.
	tableHasExplicitScope := false
	tableHasUnscopedTH := false
	for _, c := range cells {
		if !c.isTH {
			continue
		}
		hasTH = true
		if id, ok := c.elem["ID"].(ast.PdfString); ok {
			thIDs[string(id)] = true
		}
		if cellScope(ctx, state.doc, c.elem) != "" {
			tableHasExplicitScope = true
		} else {
			tableHasUnscopedTH = true
		}
	}
	if !hasTH {
		return
	}

	// veraPDF settles header determination by /Scope first and only falls back
	// to /Headers and a positional search when at least one TH carries no
	// /Scope (GFSETable.checkTable calls hasScope, and reaches hasHeaders only
	// when it returns false). A table whose TH cells are all scoped therefore
	// reaches neither t5 nor t6. The clause says nothing about that order, so
	// it is taken from the implementation and confirmed by probe: the same
	// table fires t5 with an unscoped TH and is silent with a scoped one.
	if state.profile == "PDF/UA-2" && !tableHasUnscopedTH {
		return
	}

	for _, td := range cells {
		if td.isTH {
			continue
		}
		if ctx.Err() != nil {
			return
		}
		// veraPDF exempts the cell in the top left corner from the fallback,
		// which is the one position no header can precede in either direction.
		if state.profile == "PDF/UA-2" && td.row == 0 && td.col == 0 {
			continue
		}

		headers := cellHeaders(ctx, state.doc, td.elem)
		var unknown []string
		connected := false
		for _, id := range headers {
			if thIDs[id] {
				connected = true
			} else {
				unknown = append(unknown, id)
			}
		}

		// Positional model per ISO 32000-2 Table 384: /Scope makes a TH
		// pertain to the REST of its column (cells below) or row (cells
		// after). A TD with no positionally pertinent TH at all (e.g. the
		// corner cell beside a header row) has nothing to determine and
		// passes vacuously, since §8.2.5.26 only requires a header where one
		// is positionally pertinent. A TD with pertinent THs whose /Scope
		// fails to cover it remains unconnected and fails.
		pertinent := false
		if !connected {
			for _, th := range cells {
				if !th.isTH {
					continue
				}
				aboveInColumn := overlaps(th.col, th.colSpan, td.col, td.colSpan) && th.row < td.row
				leftInRow := overlaps(th.row, th.rowSpan, td.row, td.rowSpan) && th.col < td.col
				if !aboveInColumn && !leftInRow {
					continue
				}
				pertinent = true
				switch cellScope(ctx, state.doc, th.elem) {
				case "Row":
					connected = leftInRow
				case "Column":
					connected = aboveInColumn
				case "Both":
					connected = true
				default:
					// No explicit /Scope. PDF/UA-1 §7.5 t1 literally requires TH
					// cells to carry a /Scope when the structure is not determinable
					// via Headers/IDs, so a no-/Scope TH connects nothing under UA-1.
					// PDF/UA-2 §8.2.5.26 instead applies the ISO 32000-2:2020,
					// 14.8.5.7 implicit-scope model: a TH with no /Scope headers the
					// cells below it in its column and to the right in its row. This
					// TH is positionally pertinent (above-in-column or left-in-row),
					// so it connects the TD. veraPDF applies this implicit model only
					// when the table uses no explicit /Scope anywhere; a table that
					// mixes scoped and unscoped THs is in explicit mode, where an
					// unscoped TH connects nothing.
					if state.profile == "PDF/UA-2" && !tableHasExplicitScope {
						connected = true
					}
				}
				if connected {
					break
				}
			}
			// A TD with no positionally pertinent TH is the case veraPDF
			// reports as "Headers for this table cell cannot be determined
			// algorithmically", so under PDF/UA-2 it stays unconnected.
			// PDF/UA-1 keeps the vacuous pass, which matches veraPDF there.
			if !pertinent && len(headers) == 0 && state.profile != "PDF/UA-2" {
				connected = true
			}
		}

		// PDF/UA-1 §7.5 splits the same TD model into complementary halves:
		// t1 fails an unconnected TD with NO unknown /Headers refs (a missing
		// association), t2 fails an unconnected TD whose /Headers name unknown
		// IDs (a dangling reference). PDF/UA-2 keys the same split as
		// §8.2.5.26 t5/t6.
		ua1 := state.profile == "PDF/UA-1"
		switch {
		case connected:
			continue
		case len(unknown) > 0:
			rule := Rule_14289_2_8_2_5_26_t06
			if ua1 {
				rule = Rule_14289_1_7_5_t02
			}
			*state.results = append(*state.results,
				failf(rule, "TD references unknown header ID(s) %v", unknown)...)
		default:
			rule := Rule_14289_2_8_2_5_26_t05
			if ua1 {
				rule = Rule_14289_1_7_5_t01
			}
			*state.results = append(*state.results,
				fail(rule, "TD has no determinable header cells in a table containing TH")...)
		}

		// veraPDF's fallback stops at the first cell whose headers it cannot
		// determine, so one table yields one verdict rather than one per cell.
		// Reporting every cell would name the same defect repeatedly and, since
		// the two tests split by whether the cell named unknown IDs, could
		// report both halves of the split for a single table.
		if state.profile == "PDF/UA-2" {
			return
		}
	}
}

// reportRowSpanOverrun fires when a /RowSpan reaches past the last row the
// table declares, and reports whether it did.
//
// Such a span puts cells in rows no TR declares, so the row-by-row column
// comparison never sees them: every declared row still agrees. Which rule
// answers for it turns on where the overrunning cell sits, measured against
// veraPDF across two, three and four column tables: a span starting in the
// first column answers the column-gap rule (ISO 14289-1 7.2 t43, ISO 14289-2
// 8.2.5.26 t4), and a span starting anywhere else answers the row-span rule
// (7.2 t41, 8.2.5.26 t2). That split follows veraPDF's cell placement rather
// than anything the clauses say, so it is recorded as measured behaviour.
func reportRowSpanOverrun(ctx context.Context, state *walkState, rows []ast.PdfDict) bool {
	col, row, ok := firstRowSpanOverrun(ctx, state.doc, rows)
	if !ok {
		return false
	}

	rule := Rule_14289_1_7_2_t41
	if state.profile == "PDF/UA-2" {
		rule = Rule_14289_2_8_2_5_26_t02
	}
	if col == 0 {
		rule = tableColumnRule(state.profile, false)
	}
	if rule.Specification == "" {
		return false
	}
	*state.results = append(*state.results,
		failf(rule, "Table row %d has a RowSpan reaching past the last row of the table",
			row+1)...)
	return true
}

// firstRowSpanOverrun returns the starting column and row of the first cell
// whose /RowSpan reaches past the last declared row.
//
// The placement mirrors [computeEffectiveColumns]: a cell takes the first
// column no carried span already holds, so the column reported is the one the
// cell actually occupies rather than its index among its row's cells.
func firstRowSpanOverrun(ctx context.Context, doc *document.Document, rows []ast.PdfDict) (col, row int, ok bool) {
	carry := make(map[int]int)
	for ri, rowDict := range rows {
		if ctx.Err() != nil {
			return 0, 0, false
		}
		colPos := 0
		for _, cell := range resolveRowCells(ctx, doc, rowDict) {
			for carry[colPos] > 0 {
				colPos++
			}
			cs := cellColSpan(ctx, doc, cell)
			rs := cellRowSpan(ctx, doc, cell)
			if ri+rs > len(rows) {
				return colPos, ri, true
			}
			if rs > 1 {
				for c := range cs {
					carry[colPos+c] = rs
				}
			}
			colPos += cs
		}
		for k, v := range carry {
			if v <= 1 {
				delete(carry, k)
			} else {
				carry[k] = v - 1
			}
		}
	}
	return 0, 0, false
}
