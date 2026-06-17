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
// reports whether the table is column-regular. veraPDF's SETable.checkRegular
// returns false on any irregularity and the caller then skips header
// determination (t5/t6); this function mirrors that contract by returning false
// when it fires a regularity violation.
func validateTableColumnConsistency(ctx context.Context, state *walkState, tableElem ast.PdfDict) bool {
	rows := collectTableRows(ctx, state.doc, tableElem)
	if len(rows) < 2 {
		return true
	}

	// Build effective column count per row using a proper grid model.
	// RowSpan causes a cell to occupy column positions in subsequent rows.
	// Columns "occupied" by RowSpan carry-over affect effective counts.
	effectiveCols := computeEffectiveColumns(ctx, state.doc, rows)
	if len(effectiveCols) < 2 || effectiveCols[0] == 0 {
		return true
	}

	// veraPDF takes the first row's column count as the table width and splits
	// the violation: a row WIDER than the first (a column span overflowing the
	// grid, wrongColumnSpan == null) fires the overflow rule; a NARROWER or
	// empty row (unfilled grid cells, wrongColumnSpan set) fires the gap rule.
	// Overflow takes precedence (veraPDF detects it in its placement pass
	// before the trailing empty-cell scan).
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
		return true
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
// veraPDF fires the overflow variant (ISO 14289-2 §8.2.5.26 t03 / ISO 14289-1
// §7.2 t42) when a row is wider than the first, and the gap variant (t04 / t43)
// when a row is narrower or empty.
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
// TD of a table that contains TH cells, mirroring veraPDF's SETD model. A
// TD is connected when its /Headers list names a TH /ID in the same table,
// or when a TH /Scope covers the TD's grid position (Row: shared row;
// Column: shared column; Both: either). /Headers entries naming no TH in
// the table are unknown headers (t6); a TD with neither mechanism is
// unconnected (t5). A TH without /Scope connects nothing: veraPDF's model
// recognizes only the two explicit mechanisms (its 8.2.5.26-t05-fail-a
// fixture has a scope-less header row and still fails).
func validateTableHeaderDetermination(ctx context.Context, state *walkState, tableElem ast.PdfDict) {
	rows := collectTableRows(ctx, state.doc, tableElem)
	if len(rows) == 0 {
		return
	}
	cells := buildTableGrid(ctx, state.doc, rows)

	thIDs := make(map[string]bool)
	hasTH := false
	for _, c := range cells {
		if !c.isTH {
			continue
		}
		hasTH = true
		if id, ok := c.elem["ID"].(ast.PdfString); ok {
			thIDs[string(id)] = true
		}
	}
	if !hasTH {
		return
	}

	for _, td := range cells {
		if td.isTH {
			continue
		}
		if ctx.Err() != nil {
			return
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
		// passes vacuously; veraPDF's 8.2.5.26-t01 pass fixtures leave
		// such cells unflagged while its t05-fail fixture (pertinent THs
		// whose /Scope names are broken) still fails.
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
				}
				if connected {
					break
				}
			}
			if !pertinent && len(headers) == 0 {
				connected = true
			}
		}

		// veraPDF's UA-1 7.5 tests split the same TD model into
		// complementary halves: t1 fails an unconnected TD with NO unknown
		// /Headers refs (test `hasConnectedHeader != false ||
		// unknownHeaders != ''`), t2 fails an unconnected TD whose
		// /Headers name unknown IDs (`... || unknownHeaders == ''`).
		// PDF/UA-2 keys the same split as §8.2.5.26 t5/t6.
		ua1 := state.profile == "PDF/UA-1"
		switch {
		case connected:
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
	}
}
