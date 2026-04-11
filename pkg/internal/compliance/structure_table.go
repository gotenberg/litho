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
				*state.results = append(*state.results,
					failf(Rule_14289_1_7_2_t05, "TR inside %s", parentType)...)
			}
		}
	case "THead":
		// THead must be inside Table.
		if parentType != "Table" && parentType != "" {
			*state.results = append(*state.results,
				failf(Rule_14289_1_7_2_t06, "THead inside %s", parentType)...)
		}
	case "TBody":
		// TBody must be inside Table.
		if parentType != "Table" && parentType != "" {
			*state.results = append(*state.results,
				failf(Rule_14289_1_7_2_t07, "TBody inside %s", parentType)...)
		}
	case "TFoot":
		// TFoot must be inside Table.
		if parentType != "Table" && parentType != "" {
			*state.results = append(*state.results,
				failf(Rule_14289_1_7_2_t08, "TFoot inside %s", parentType)...)
		}
	case "TD", "TH":
		// TD/TH must be inside TR.
		if parentType != "TR" && parentType != "" {
			*state.results = append(*state.results,
				failf(Rule_14289_1_7_2_t04, "%s inside %s instead of TR", typeName, parentType)...)
		}
	}
}

// validateTableColumnConsistency checks that all TR rows in a Table have the
// same number of columns, accounting for ColSpan attributes on TD/TH cells.
// PDF/UA-1 §7.2 t43 and PDF/UA-2 §8.2.5.26 t3/t4.
func validateTableColumnConsistency(ctx context.Context, state *walkState, tableElem ast.PdfDict) {
	rows := collectTableRows(ctx, state.doc, tableElem)
	if len(rows) < 2 {
		return
	}

	// Build effective column count per row using a proper grid model.
	// RowSpan causes a cell to occupy column positions in subsequent rows.
	// Columns "occupied" by RowSpan carry-over affect effective counts.
	effectiveCols := computeEffectiveColumns(ctx, state.doc, rows)
	if len(effectiveCols) < 2 || effectiveCols[0] == 0 {
		return
	}

	for i := 1; i < len(effectiveCols); i++ {
		if effectiveCols[i] != effectiveCols[0] {
			rule := tableColumnRule(state.profile)
			*state.results = append(*state.results,
				failf(rule, "Table row 1 has %d columns, row %d has %d columns",
					effectiveCols[0], i+1, effectiveCols[i])...)
			return
		}
	}
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
		cells := getRowCells(ctx, doc, row)

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

// getRowCells extracts TD/TH cell dicts from a TR element.
func getRowCells(ctx context.Context, doc *document.Document, tr ast.PdfDict) []ast.PdfDict {
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

func tableColumnRule(profile string) Rule {
	if profile == "PDF/UA-2" {
		return Rule_14289_2_8_2_5_26_t04
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
		if typeName != "TR" && typeName != "Caption" {
			*state.results = append(*state.results,
				failf(Rule_14289_1_7_2_t03, "%s contains %s instead of TR", parentType, typeName)...)
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
