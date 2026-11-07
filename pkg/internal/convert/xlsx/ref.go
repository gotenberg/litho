package xlsx

import (
	"strings"

	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
)

// maxRows and maxColumns are the sheet's bounds, the rows and columns
// Excel and LibreOffice Calc address (ECMA-376 Part 1, 18.3.1.73 r and
// 18.3.1.4 r name cells up to XFD1048576). A cell, row or column past
// them is not read.
const (
	maxRows    = 1 << 20
	maxColumns = 1 << 14
)

// cellRef parses an A1-style cell reference ("B3", "$B$3"), returning its
// 0-based row and column and whether it parsed within the sheet's bounds
// (ECMA-376 Part 1, 18.18.62 ST_CellRef).
func cellRef(s string) (row, col int, ok bool) {
	col, rest, okc := columnPart(s)
	row, rest, okr := rowPart(rest)
	return row, col, okc && okr && rest == ""
}

// columnPart reads the column letters at the start of s, after an
// optional $, returning the 0-based column and the rest.
func columnPart(s string) (int, string, bool) {
	s = strings.TrimPrefix(s, "$")
	col, n := 0, 0
	for n < len(s) {
		c := s[n]
		if c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}
		if c < 'A' || c > 'Z' {
			break
		}
		col = col*26 + int(c-'A'+1)
		n++
		if col > maxColumns {
			return 0, s, false
		}
	}
	if n == 0 {
		return 0, s, false
	}
	return col - 1, s[n:], true
}

// rowPart reads the row number at the start of s, after an optional $,
// returning the 0-based row and the rest.
func rowPart(s string) (int, string, bool) {
	s = strings.TrimPrefix(s, "$")
	row, n := 0, 0
	for n < len(s) && s[n] >= '0' && s[n] <= '9' {
		row = row*10 + int(s[n]-'0')
		n++
		if row > maxRows {
			return 0, s, false
		}
	}
	if n == 0 || row == 0 {
		return 0, s, false
	}
	return row - 1, s[n:], true
}

// rangeRef parses a cell range ("A1:C4", "$A$1", "A:B" for whole
// columns, "1:3" for whole rows; ECMA-376 Part 1, 18.18.63 ST_Ref and
// 18.17.2.3 the area references of formulas), its corners ordered.
func rangeRef(s string) (sheet.Range, bool) {
	first, last, two := strings.Cut(strings.TrimSpace(s), ":")
	if !two {
		last = first
	}
	if r0, c0, ok := cellRef(first); ok {
		r1, c1, ok := cellRef(last)
		if !ok {
			return sheet.Range{}, false
		}
		return ordered(sheet.Range{FirstRow: r0, FirstColumn: c0, LastRow: r1, LastColumn: c1}), true
	}
	if !two {
		return sheet.Range{}, false
	}
	if c0, rest, ok := columnPart(first); ok && rest == "" {
		c1, rest, ok := columnPart(last)
		if !ok || rest != "" {
			return sheet.Range{}, false
		}
		return ordered(sheet.Range{FirstRow: 0, FirstColumn: c0, LastRow: maxRows - 1, LastColumn: c1}), true
	}
	if r0, rest, ok := rowPart(first); ok && rest == "" {
		r1, rest, ok := rowPart(last)
		if !ok || rest != "" {
			return sheet.Range{}, false
		}
		return ordered(sheet.Range{FirstRow: r0, FirstColumn: 0, LastRow: r1, LastColumn: maxColumns - 1}), true
	}
	return sheet.Range{}, false
}

// ordered returns r with its first row and column before its last.
func ordered(r sheet.Range) sheet.Range {
	if r.FirstRow > r.LastRow {
		r.FirstRow, r.LastRow = r.LastRow, r.FirstRow
	}
	if r.FirstColumn > r.LastColumn {
		r.FirstColumn, r.LastColumn = r.LastColumn, r.FirstColumn
	}
	return r
}

// maxAreas caps the areas of one defined name. Anti-DoS guard, not
// spec-mandated: a print area lists a handful of ranges.
const maxAreas = 256

// sheetAreas parses the formula of a defined name that lists areas
// (_xlnm.Print_Area, _xlnm.Print_Titles: "'Sheet 1'!$A$1:$C$9,Sheet1!$1:$2";
// ECMA-376 Part 1, 18.2.5 and 18.17.2.3), returning the sheet each area
// names, "" when unqualified, with the area. Parts that are not areas are
// skipped.
func sheetAreas(formula string) (sheets []string, areas []sheet.Range) {
	for _, part := range splitTopLevel(formula) {
		if len(areas) >= maxAreas {
			break
		}
		part = strings.TrimSpace(part)
		part = strings.TrimPrefix(strings.TrimSuffix(part, ")"), "(")
		name, ref := "", part
		if i := strings.LastIndexByte(part, '!'); i >= 0 {
			name, ref = unquoteSheet(part[:i]), part[i+1:]
		}
		if r, ok := rangeRef(ref); ok {
			sheets = append(sheets, name)
			areas = append(areas, r)
		}
	}
	return sheets, areas
}

// splitTopLevel splits a formula at the commas outside quotes and
// parentheses.
func splitTopLevel(s string) []string {
	var out []string
	depth, start, quoted := 0, 0, false
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '\'':
			quoted = !quoted
		case quoted:
		case c == '(':
			depth++
		case c == ')':
			depth--
		case c == ',' && depth <= 0:
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

// unquoteSheet returns a sheet name as a formula writes it, its quotes
// removed and a doubled quote read as one (ECMA-376 Part 1, 18.17.2.3).
func unquoteSheet(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		s = strings.ReplaceAll(s[1:len(s)-1], "''", "'")
	}
	return s
}
