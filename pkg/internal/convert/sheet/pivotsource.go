package sheet

import (
	"math"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

// maxPivotRecords caps the records a pivot table summarises. Anti-DoS
// guard, not spec-mandated: a quarter of a million rows is past what a
// printed summary is made from.
const maxPivotRecords = 1 << 18

// maxPivotFields caps the columns of a pivot table's source. Anti-DoS
// guard, not spec-mandated: Excel's pivot cache holds 16,384 fields, a
// real source a few dozen.
const maxPivotFields = 1024

// pivotSource is a pivot table's source range read: the fields' names
// and number formats, and the records.
type pivotSource struct {
	names   []string
	formats []string
	recs    [][]Value
	// items holds, per field, the distinct values in the order Calc's
	// cache sorts them, and ids every record's value as an index into it.
	items [][]Value
	ids   [][]int
	col   *collate.Collator
}

// itemKey identifies a value among a field's items: Calc's cache tells
// values, strings, errors and the empty value apart (ScDPItemData).
type itemKey struct {
	kind Kind
	num  float64
	text string
}

// keyOf returns the item key of v, a boolean reading as its number.
func keyOf(v Value) itemKey {
	switch v.Kind {
	case Number, Bool:
		return itemKey{kind: Number, num: v.Number}
	case Text:
		return itemKey{kind: Text, text: v.Text}
	case Error:
		return itemKey{kind: Error, text: v.Text}
	case Empty:
	}
	return itemKey{kind: Empty}
}

// itemRank orders the kinds of items as Calc's cache does: values, then
// strings, then errors, the empty item last (ScDPItemData::Compare).
func itemRank(k Kind) int {
	switch k {
	case Number, Bool:
		return 0
	case Text:
		return 1
	case Error:
		return 2
	case Empty:
	}
	return 3
}

// newCollator returns a collator comparing strings as Calc's default
// collator does, case insensitively (ScGlobal::GetCollator). A collator
// keeps buffers: each conversion makes its own.
func newCollator() *collate.Collator {
	return collate.New(language.AmericanEnglish, collate.IgnoreCase)
}

// compareItems orders two items as Calc's cache sorts them, strings by
// col.
func compareItems(col *collate.Collator, a, b Value) int {
	ra, rb := itemRank(a.Kind), itemRank(b.Kind)
	if ra != rb {
		return ra - rb
	}
	switch ra {
	case 0:
		switch {
		case a.Number < b.Number:
			return -1
		case a.Number > b.Number:
			return 1
		}
		return 0
	case 1, 2:
		if c := col.CompareString(a.Text, b.Text); c != 0 {
			return c
		}
		return strings.Compare(a.Text, b.Text)
	}
	return 0
}

// readPivotSource reads the source range of p from doc: the first row
// names the fields, each further row is a record (ScDPCache::InitFromDoc).
// The rows past the last one with a value count as one record of empty
// values, as Calc's cache appends one empty item for them. A field's
// number format is the last one other than General among its cells with
// a value. It returns false when the range is not on a sheet.
func (b *builder) readPivotSource(p *Pivot) (*pivotSource, bool) {
	if p.SourceSheet < 0 || p.SourceSheet >= len(b.doc.Sheets) || b.doc.Sheets[p.SourceSheet] == nil {
		return nil, false
	}
	rg := clampRange(p.Source)
	if rg.LastRow <= rg.FirstRow || rg.LastColumn < rg.FirstColumn {
		return nil, false
	}
	nf := min(rg.LastColumn-rg.FirstColumn+1, maxPivotFields)
	g := newGrid(b.doc, b.doc.Sheets[p.SourceSheet])
	// The data area ends at the last row holding a value in the range
	// (ScDocument::ShrinkToDataArea).
	last := rg.FirstRow
	for _, r := range g.rowOrder {
		if r <= rg.FirstRow || r > rg.LastRow {
			continue
		}
		for _, c := range g.rows[r].Cells {
			if c.Column >= rg.FirstColumn && c.Column < rg.FirstColumn+nf && hasContent(&c) {
				last = r
				break
			}
		}
	}
	if last == rg.FirstRow {
		// A source without a record makes no cache, and no output.
		return nil, false
	}
	nr := min(last-rg.FirstRow, maxPivotRecords)
	tail := rg.LastRow > last
	src := &pivotSource{names: make([]string, nf), formats: make([]string, nf), col: newCollator()}
	// The labels are made unique, case ignored, after the data layout
	// field's "Data", a repeated one numbered from 2
	// (dpcache.cxx, normalizeAddLabel).
	taken := map[string]bool{"data": true}
	for f := range nf {
		c := rg.FirstColumn + f
		cell := g.cell(rg.FirstRow, c)
		name := ""
		if hasContent(cell) {
			name = b.cellText(cell, g.baseStyle(rg.FirstRow, c, cell))
		}
		if name == "" {
			// An empty header names its field by its column
			// (createLabelString: "Column A").
			name = "Column " + columnName(c)
		}
		label := name
		for n := 2; taken[strings.ToLower(label)]; n++ {
			label = name + strconv.Itoa(n)
		}
		taken[strings.ToLower(label)] = true
		src.names[f] = label
	}
	for i := range nr {
		r := rg.FirstRow + 1 + i
		rec := make([]Value, nf)
		for f := range nf {
			c := rg.FirstColumn + f
			cell := g.cell(r, c)
			if cell == nil {
				continue
			}
			v := cell.Value
			if v.Kind == Text && v.Text == "" && len(cell.Runs) > 0 {
				var sb strings.Builder
				for _, run := range cell.Runs {
					sb.WriteString(run.Text)
				}
				v.Text = sb.String()
			}
			rec[f] = v
			if v.Kind == Number || v.Kind == Bool {
				if code := g.baseStyle(r, c, cell).Format; code != "" && !strings.EqualFold(code, "General") {
					src.formats[f] = code
				}
			}
		}
		src.recs = append(src.recs, rec)
	}
	if tail && nr < maxPivotRecords {
		src.recs = append(src.recs, make([]Value, nf))
	}
	src.index()
	return src, true
}

// index fills the items and the record ids of every field.
func (s *pivotSource) index() {
	nf := len(s.names)
	s.items = make([][]Value, nf)
	s.ids = make([][]int, nf)
	for f := range nf {
		seen := map[itemKey]bool{}
		var items []Value
		for _, rec := range s.recs {
			k := keyOf(rec[f])
			if !seen[k] {
				seen[k] = true
				items = append(items, rec[f])
			}
		}
		sort.SliceStable(items, func(i, j int) bool { return compareItems(s.col, items[i], items[j]) < 0 })
		at := make(map[itemKey]int, len(items))
		for i, v := range items {
			at[keyOf(v)] = i
		}
		ids := make([]int, len(s.recs))
		for i, rec := range s.recs {
			ids[i] = at[keyOf(rec[f])]
		}
		s.items[f], s.ids[f] = items, ids
	}
}

// cellText returns the text the cell shows, formatted by its style.
func (b *builder) cellText(cell *Cell, st *Style) string {
	if cell.Value.Kind == Text || cell.Value.Kind == Empty {
		if cell.Value.Text == "" && len(cell.Runs) > 0 {
			var sb strings.Builder
			for _, r := range cell.Runs {
				sb.WriteString(r.Text)
			}
			return sb.String()
		}
		return cell.Value.Text
	}
	return b.format(st.Format).Apply(numValue(cell.Value)).Text
}

// finite reports a value that is neither NaN nor infinite.
func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
