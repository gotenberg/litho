package docx

import "github.com/gotenberg/litho/pkg/internal/compose"

// padTableRows makes every table of the structure tree under root count
// the same columns in each row, as ISO 14289-1, 7.2 and ISO 32000-1,
// 14.8.5.7, Table 349 ask: where a row leaves grid columns without a cell,
// the columns a gridBefore or gridAfter skips (ECMA-376 Part 1, 17.4.15
// and 17.4.14), an empty TD spanning them takes their place. A reader
// places a cell in the first column the cells before it and the row spans
// above leave free, so an empty cell inserted where the free run starts
// fills it. Nothing the page draws changes: the engine lays the row's
// cells as it did, and an empty cell holds no content.
//
// # Corpus evidence
//
//   - libreoffice/docx: layout/tdf128959.docx, layout/tdf134685.docx,
//     ooxmlexport/fdo38414.docx and ooxmlexport/fdo69644.docx start or end
//     rows with gridBefore or gridAfter, and litho/docx tables-merged does
//     too: veraPDF failed each at PDF/UA-1, 7.2 tests 42 and 43, before the
//     rows were padded.
func padTableRows(root *compose.StructElem) {
	if root == nil {
		return
	}
	stack := []*compose.StructElem{root}
	for len(stack) > 0 {
		e := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if e.Role == "Table" {
			padRows(tableRowsOf(e))
		}
		stack = append(stack, e.Kids...)
	}
}

// tableRowsOf returns the TR elements of the Table t in order, those of
// its row groups included (Table 337).
func tableRowsOf(t *compose.StructElem) []*compose.StructElem {
	var rows []*compose.StructElem
	for _, k := range t.Kids {
		switch k.Role {
		case "TR":
			rows = append(rows, k)
		case "THead", "TBody", "TFoot":
			for _, r := range k.Kids {
				if r.Role == "TR" {
					rows = append(rows, r)
				}
			}
		}
	}
	return rows
}

// padRows inserts in rows, the rows of one table, the empty cells that
// make each count the columns of the widest ([padTableRows]). A table
// wider than [maxGridColumns] is left as it is.
func padRows(rows []*compose.StructElem) {
	// covered[i] marks the columns of row i a cell takes, its own or one
	// spanning down from a row above.
	covered := make([][]bool, len(rows))
	starts := make([][]int, len(rows))
	widest := 0
	for i, r := range rows {
		col := 0
		for _, c := range r.Kids {
			if c.Role != "TD" && c.Role != "TH" {
				continue
			}
			for col < len(covered[i]) && covered[i][col] {
				col++
			}
			cs := max(c.ColSpan, 1)
			if col+cs > maxGridColumns {
				return
			}
			starts[i] = append(starts[i], col)
			for j := i; j < min(i+max(c.RowSpan, 1), len(rows)); j++ {
				for len(covered[j]) < col+cs {
					covered[j] = append(covered[j], false)
				}
				for k := col; k < col+cs; k++ {
					covered[j][k] = true
				}
			}
			col += cs
		}
		widest = max(widest, len(covered[i]))
	}
	for i, r := range rows {
		cov := covered[i]
		for len(cov) < widest {
			cov = append(cov, false)
		}
		kids := make([]*compose.StructElem, 0, len(r.Kids)+1)
		next := 0
		// gap returns an empty cell for the free run starting at col, the
		// run's end.
		gap := func(col int) (*compose.StructElem, int) {
			end := col
			for end < widest && !cov[end] {
				end++
			}
			td := &compose.StructElem{Role: "TD"}
			if end-col > 1 {
				td.ColSpan = end - col
			}
			return td, end
		}
		col := 0
		for _, c := range r.Kids {
			if c.Role == "TD" || c.Role == "TH" {
				at := starts[i][next]
				next++
				for col < at {
					if cov[col] {
						col++
						continue
					}
					td, end := gap(col)
					kids = append(kids, td)
					col = end
				}
				col = at + max(c.ColSpan, 1)
			}
			kids = append(kids, c)
		}
		for col < widest {
			if cov[col] {
				col++
				continue
			}
			td, end := gap(col)
			kids = append(kids, td)
			col = end
		}
		r.Kids = kids
	}
}
