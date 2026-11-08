package xlsx

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
)

// The relationship types of the pivot table parts (ECMA-376 Part 1,
// 12.3.13 and 12.3.12).
const (
	relPivotTable   = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/pivotTable"
	relPivotCache   = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/pivotCacheDefinition"
	relPivotRecords = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/pivotCacheRecords"
)

// maxPivotTables caps the pivot tables of a workbook. Anti-DoS guard, not
// spec-mandated: a workbook holds a handful.
const maxPivotTables = 256

// maxPivotRecords caps the records restored from a pivot cache.
// Anti-DoS guard, not spec-mandated: the sheet builder summarises at most
// as many.
const maxPivotRecords = 1 << 18

// maxPivotItems caps the items read for one pivot or cache field.
// Anti-DoS guard, not spec-mandated: Excel shows at most 1,048,576 items
// a field, a printed table a few thousand.
const maxPivotItems = 1 << 16

// pendingPivot is a pivot table read, whose source names a sheet, a
// defined name or a table that is resolved once every sheet is read.
type pendingPivot struct {
	sh    *sheet.Sheet
	cache *pivotCache
	pivot sheet.Pivot
	// srcSheet, srcName and srcRange are the cache's worksheet source
	// (18.10.1.98 worksheetSource).
	srcSheet, srcName string
	srcRange          sheet.Range
	hasRange          bool
}

// cacheField is a pivot cache field: its shared items, which the pivot
// fields' items index, and whether it maps to a column of the source.
type cacheField struct {
	name     string
	items    []sheet.Value
	database bool
}

// pivotCache is a pivot cache definition read (18.10.1.67).
type pivotCache struct {
	srcSheet, srcName string
	srcRange          sheet.Range
	hasRange          bool
	worksheet         bool
	external          bool
	fields            []cacheField
	// records is the cache's records part, dummy the index of the hidden
	// sheet its records were written to, -1 before.
	records string
	dummy   int
	rows    int
}

// pivotCacheParts reads the workbook's pivotCaches (18.2.22): the part of
// each cache by its id.
func (r *reader) pivotCacheParts(main string, root *ooxml.Element) map[int]string {
	out := map[int]string{}
	for _, c := range root.Child(nsMain, "pivotCaches").Kids() {
		if !c.Is(nsMain, "pivotCache") {
			continue
		}
		id, _ := c.AttrNS(ooxml.NSR, "id")
		rel, ok := r.pkg.RelationshipByID(main, id)
		if !ok || rel.External || !r.pkg.Has(rel.Target) {
			continue
		}
		out[intAttr(c, "cacheId", -1)] = rel.Target
	}
	return out
}

// pivotTables reads the pivot table parts the worksheet relates to
// (18.10.1.73 pivotTableDefinition), as LibreOffice imports them
// (oox::xls::PivotTable): the location, the row, column, page and data
// fields with their layout, subtotals, items and sorting, the grand
// totals, the header row.
func (w *sheetReader) pivotTables() error {
	r := w.r
	// A worksheet without relationships relates to no pivot table.
	rels, _ := r.pkg.Relationships(w.part)
	for _, rel := range rels {
		if rel.Type != relPivotTable || rel.External || !r.pkg.Has(rel.Target) {
			continue
		}
		if len(r.pivots) >= maxPivotTables {
			break
		}
		root, err := r.pkg.ParseXML(r.ctx, rel.Target, understood)
		if err != nil {
			if isMalformed(err) {
				r.warn("malformed-part", fmt.Sprintf("pivot table %s does not parse: %v", rel.Target, err))
				continue
			}
			return err
		}
		if !root.Is(nsMain, "pivotTableDefinition") {
			continue
		}
		cachePart := r.pivotCaches[intAttr(root, "cacheId", -1)]
		if cachePart == "" {
			cachePart = r.pkg.RelatedPart(rel.Target, relPivotCache)
		}
		cache, err := r.pivotCache(cachePart)
		if err != nil {
			return err
		}
		if p, ok := w.pivotTable(root, cache); ok {
			r.pivots = append(r.pivots, p)
		}
	}
	return nil
}

// pivotCache reads the cache definition part name, nil when it does not
// parse.
func (r *reader) pivotCache(name string) (*pivotCache, error) {
	if c, ok := r.caches[name]; ok {
		return c, nil
	}
	if name == "" || !r.pkg.Has(name) {
		return nil, nil
	}
	root, err := r.pkg.ParseXML(r.ctx, name, understood)
	if err != nil {
		if isMalformed(err) {
			r.warn("malformed-part", fmt.Sprintf("pivot cache %s does not parse: %v", name, err))
			r.caches[name] = nil
			return nil, nil
		}
		return nil, err
	}
	c := &pivotCache{records: r.pkg.RelatedPart(name, relPivotRecords), dummy: -1}
	if src := root.Child(nsMain, "cacheSource"); src != nil {
		c.worksheet = attr(src, "type") == "worksheet"
		if ws := src.Child(nsMain, "worksheetSource"); ws != nil {
			c.srcSheet = decodeXString(attr(ws, "sheet"))
			c.srcName = decodeXString(attr(ws, "name"))
			c.srcRange, c.hasRange = rangeRef(attr(ws, "ref"))
			id, _ := ws.AttrNS(ooxml.NSR, "id")
			c.external = id != ""
		}
	}
	for _, f := range root.Child(nsMain, "cacheFields").Kids() {
		if !f.Is(nsMain, "cacheField") || len(c.fields) >= maxPivotItems {
			continue
		}
		cf := cacheField{name: decodeXString(attr(f, "name")), database: attr(f, "formula") == ""}
		if g := f.Child(nsMain, "fieldGroup"); g != nil {
			if _, ok := g.Attr("base"); ok {
				cf.database = false
			}
			if g.Child(nsMain, "rangePr") != nil || g.Child(nsMain, "discretePr") != nil {
				cf.database = false
			}
		}
		for _, it := range f.Child(nsMain, "sharedItems").Kids() {
			if len(cf.items) >= maxPivotItems {
				break
			}
			cf.items = append(cf.items, r.cacheItem(it))
		}
		c.fields = append(c.fields, cf)
	}
	r.caches[name] = c
	return c, nil
}

// cacheItem returns the value of a shared item (18.10.1.67): a number, a
// string, a date as its serial, a boolean, an error, or the missing item.
func (r *reader) cacheItem(it *ooxml.Element) sheet.Value {
	v := attr(it, "v")
	switch it.Local {
	case "n":
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			return sheet.Value{Kind: sheet.Number, Number: f}
		}
	case "s":
		return sheet.Value{Kind: sheet.Text, Text: decodeXString(v)}
	case "d":
		if f, ok := isoSerial(v); ok {
			return sheet.Value{Kind: sheet.Number, Number: f}
		}
	case "b":
		n := 0.0
		if ooxml.OnOff(v, false) {
			n = 1
		}
		return sheet.Value{Kind: sheet.Bool, Number: n}
	case "e":
		return sheet.Value{Kind: sheet.Error, Text: v}
	}
	return sheet.Value{}
}

// pivotFunction returns the function of an ST_DataConsolidateFunction
// (18.18.17), as LibreOffice maps a data field's: "count" counts every
// value, "countNums" the numbers.
func pivotFunction(s string) sheet.PivotFunction {
	switch s {
	case "count":
		return sheet.PivotCount
	case "average":
		return sheet.PivotAverage
	case "max":
		return sheet.PivotMax
	case "min":
		return sheet.PivotMin
	case "product":
		return sheet.PivotProduct
	case "countNums":
		return sheet.PivotCountNums
	case "stdDev":
		return sheet.PivotStdDev
	case "stdDevp":
		return sheet.PivotStdDevP
	case "var":
		return sheet.PivotVar
	case "varp":
		return sheet.PivotVarP
	}
	return sheet.PivotSum
}

// pivotTable reads one pivot table definition over its cache, false when
// LibreOffice would not rebuild it: no name, no worksheet source, a
// field it groups or computes, a data field shown as a reference value.
func (w *sheetReader) pivotTable(root *ooxml.Element, cache *pivotCache) (pendingPivot, bool) {
	r := w.r
	name := decodeXString(attr(root, "name"))
	loc := root.Child(nsMain, "location")
	area, ok := rangeRef(attr(loc, "ref"))
	if cache == nil || !cache.worksheet || name == "" || !ok {
		return pendingPivot{}, false
	}
	pp := pendingPivot{sh: w.sh, cache: cache, srcSheet: cache.srcSheet, srcName: cache.srcName, srcRange: cache.srcRange, hasRange: cache.hasRange}
	clear := area
	p := sheet.Pivot{
		Name: name, Clear: &clear,
		RowGrand:       boolOrTrue(root, "rowGrandTotals"),
		ColumnGrand:    boolOrTrue(root, "colGrandTotals"),
		HideHeader:     intAttr(loc, "firstHeaderRow", 0) == 0,
		ExpandCollapse: boolOrTrue(root, "showDrill"),
	}
	// The database index of every cache field: its column in the source
	// (PivotCache::getCacheDatabaseIndex).
	dbIndex := make([]int, len(cache.fields))
	n := 0
	for i, f := range cache.fields {
		dbIndex[i] = -1
		if f.database {
			dbIndex[i] = n
			n++
		}
	}
	var defs []*ooxml.Element
	for _, f := range root.Child(nsMain, "pivotFields").Kids() {
		if f.Is(nsMain, "pivotField") && len(defs) < maxPivotItems {
			defs = append(defs, f)
		}
	}
	unsupported := ""
	field := func(x int) (sheet.PivotField, bool) {
		if x == -2 {
			return sheet.PivotField{Field: sheet.PivotDataLayout}, true
		}
		if x < 0 || x >= len(defs) || x >= len(cache.fields) || dbIndex[x] < 0 {
			unsupported = "a grouped or calculated field"
			return sheet.PivotField{}, false
		}
		pf := pivotField(defs[x], dbIndex[x], cache.fields[x].items)
		pf.Name = cache.fields[x].name
		return pf, true
	}
	for _, axis := range []struct {
		name string
		to   *[]sheet.PivotField
	}{{"rowFields", &p.Rows}, {"colFields", &p.Columns}} {
		for _, f := range root.Child(nsMain, axis.name).Kids() {
			if !f.Is(nsMain, "field") {
				continue
			}
			if pf, ok := field(intAttr(f, "x", -1)); ok {
				*axis.to = append(*axis.to, pf)
			}
		}
	}
	for _, f := range root.Child(nsMain, "pageFields").Kids() {
		if !f.Is(nsMain, "pageField") {
			continue
		}
		x := intAttr(f, "fld", -1)
		pf, ok := field(x)
		if !ok || pf.Field == sheet.PivotDataLayout {
			continue
		}
		pf.Selected = pageSelection(defs[x], intAttr(f, "item", -1), cache.fields[x].items)
		p.Pages = append(p.Pages, pf)
	}
	for _, d := range root.Child(nsMain, "dataFields").Kids() {
		if !d.Is(nsMain, "dataField") {
			continue
		}
		x := intAttr(d, "fld", -1)
		if x < 0 || x >= len(cache.fields) || dbIndex[x] < 0 {
			unsupported = "a grouped or calculated data field"
			continue
		}
		if s := attr(d, "showDataAs"); s != "" && s != "normal" {
			unsupported = "a data field shown as " + s
			continue
		}
		p.Data = append(p.Data, sheet.PivotData{Field: dbIndex[x], Name: cache.fields[x].name, Function: pivotFunction(attr(d, "subtotal")), Caption: decodeXString(attr(d, "name"))})
	}
	if unsupported != "" {
		r.warn("unsupported-content", fmt.Sprintf("pivot table %q holds %s: its output prints as the file saved it", name, unsupported))
		return pendingPivot{}, false
	}
	// Page fields sit above the location, with an empty row between,
	// as LibreOffice places them (PivotTable::finalizeImport).
	p.Row, p.Column = area.FirstRow, area.FirstColumn
	if len(p.Pages) > 0 {
		p.Row = max(p.Row-len(p.Pages)-1, 0)
	}
	pp.pivot = p
	return pp, true
}

// boolOrTrue returns e's ST_Boolean attribute local, true when absent:
// the default of every pivot and data bar flag read here.
func boolOrTrue(e *ooxml.Element, local string) bool {
	v, ok := e.Attr(local)
	if !ok {
		return true
	}
	return ooxml.OnOff(v, true)
}

// pivotField reads a row, column or page field's settings (18.10.1.69
// pivotField) as LibreOffice converts them
// (PivotTableField::convertRowColPageField).
func pivotField(e *ooxml.Element, column int, shared []sheet.Value) sheet.PivotField {
	pf := sheet.PivotField{Field: column}
	outline := boolOrTrue(e, "outline")
	top := boolOrTrue(e, "subtotalTop")
	switch {
	case top && outline && boolOrTrue(e, "compact"):
		pf.Layout = sheet.PivotCompact
	case outline && top:
		pf.Layout = sheet.PivotOutlineTop
	case outline:
		pf.Layout = sheet.PivotOutlineBottom
	default:
		pf.Layout = sheet.PivotTabular
	}
	for _, s := range []struct {
		attr string
		fn   sheet.PivotFunction
	}{
		{"sumSubtotal", sheet.PivotSum},
		{"countASubtotal", sheet.PivotCount},
		{"avgSubtotal", sheet.PivotAverage},
		{"maxSubtotal", sheet.PivotMax},
		{"minSubtotal", sheet.PivotMin},
		{"productSubtotal", sheet.PivotProduct},
		{"countSubtotal", sheet.PivotCountNums},
		{"stdDevSubtotal", sheet.PivotStdDev},
		{"stdDevPSubtotal", sheet.PivotStdDevP},
		{"varSubtotal", sheet.PivotVar},
		{"varPSubtotal", sheet.PivotVarP},
	} {
		if boolAttr(e, s.attr) {
			pf.Subtotals = append(pf.Subtotals, s.fn)
		}
	}
	if len(pf.Subtotals) == 0 && boolOrTrue(e, "defaultSubtotal") {
		pf.Subtotals = []sheet.PivotFunction{sheet.PivotAuto}
	}
	pf.AddEmpty = boolAttr(e, "insertBlankRow")
	pf.ShowEmpty = boolOrTrue(e, "showAll")
	switch attr(e, "sortType") {
	case "ascending":
		pf.Sort = sheet.PivotAscending
	case "descending":
		pf.Sort = sheet.PivotDescending
	default:
		pf.Sort = sheet.PivotManual
	}
	for _, it := range e.Child(nsMain, "items").Kids() {
		if !it.Is(nsMain, "item") || attr(it, "t") != "" && attr(it, "t") != "data" {
			continue
		}
		x := intAttr(it, "x", -1)
		if x < 0 || x >= len(shared) {
			continue
		}
		if pf.Sort == sheet.PivotManual {
			pf.Order = append(pf.Order, shared[x])
		}
		if boolAttr(it, "h") {
			pf.Hidden = append(pf.Hidden, shared[x])
		}
	}
	return pf
}

// pageSelection returns the member a page field selects (18.10.1.68
// pageField item, an index into the field's items, which the
// specification calls a cache item): the item it names, or the one
// visible item of a field that allows several; nil for none
// (PivotTableField::convertPageField).
func pageSelection(e *ooxml.Element, item int, shared []sheet.Value) *sheet.Value {
	var data []*ooxml.Element
	for _, it := range e.Child(nsMain, "items").Kids() {
		if it.Is(nsMain, "item") && (attr(it, "t") == "" || attr(it, "t") == "data") {
			data = append(data, it)
		}
	}
	x := -1
	if boolAttr(e, "multipleItemSelectionAllowed") {
		for _, it := range data {
			if boolAttr(it, "h") {
				continue
			}
			if x >= 0 {
				return nil
			}
			x = intAttr(it, "x", -1)
		}
	} else if item >= 0 && item < len(data) {
		x = intAttr(data[item], "x", -1)
	}
	if x < 0 || x >= len(shared) {
		return nil
	}
	v := shared[x]
	return &v
}

// resolvePivots places every pivot table read on its sheet once its
// source resolves to a range of a sheet of the book: the sheet the cache
// names, or the defined name or table it names. A pivot table whose
// source does not resolve keeps the cells the file saved for it.
func (r *reader) resolvePivots(doc *sheet.Document) {
	for _, pp := range r.pivots {
		idx, rg, ok := r.pivotSource(doc, &pp)
		if !ok {
			r.warn("unsupported-content", fmt.Sprintf("pivot table %q: its source is not a range of the workbook, its output prints as the file saved it", pp.pivot.Name))
			continue
		}
		p := pp.pivot
		p.SourceSheet, p.Source = idx, rg
		pp.sh.Pivots = append(pp.sh.Pivots, p)
	}
}

// pivotSource resolves a cache's source to a sheet index and a range
// (PivotCache::finalizeInternalSheetSource): a defined name or a table
// first, its totals rows left out, else the range on the named sheet.
func (r *reader) pivotSource(doc *sheet.Document, pp *pendingPivot) (int, sheet.Range, bool) {
	sheetIndex := func(name string) int {
		for i, sh := range doc.Sheets {
			if strings.EqualFold(sh.Name, name) {
				return i
			}
		}
		return -1
	}
	if pp.srcName != "" {
		for _, t := range r.tableAreas {
			if strings.EqualFold(t.name, pp.srcName) {
				rg := t.area
				rg.LastRow -= t.totals
				if rg.LastRow <= rg.FirstRow {
					return 0, sheet.Range{}, false
				}
				for i, sh := range doc.Sheets {
					if sh == t.sh {
						return i, rg, true
					}
				}
			}
		}
		for _, d := range r.allNames {
			if !strings.EqualFold(d.name, pp.srcName) {
				continue
			}
			sheets, areas := sheetAreas(d.formula)
			if len(areas) != 1 || sheets[0] == "" {
				continue
			}
			if i := sheetIndex(sheets[0]); i >= 0 {
				return i, areas[0], true
			}
		}
		return 0, sheet.Range{}, false
	}
	i := sheetIndex(pp.srcSheet)
	if pp.cache.external || (i < 0 && pp.srcSheet != "") {
		// A source in another workbook, or on a sheet deleted since, is
		// restored from the cache's records on a sheet of its own, which
		// does not print (PivotCache::prepareSourceDataSheet).
		return r.cacheSheet(doc, pp)
	}
	if i < 0 || !pp.hasRange || pp.srcRange.LastRow <= pp.srcRange.FirstRow {
		return 0, sheet.Range{}, false
	}
	return i, pp.srcRange, true
}

// cacheDateFormat is the number format of the dates a cache restores:
// Calc's en-US default date format.
const cacheDateFormat = "MM/DD/YY"

// cacheSheet writes the records of pp's cache to a hidden sheet,
// "DPCache_" and the source sheet's name, its first row the cache fields'
// names (18.10.1.68 pivotCacheRecords: each r a record, an x an index
// into the field's shared items), and returns its index and range.
func (r *reader) cacheSheet(doc *sheet.Document, pp *pendingPivot) (int, sheet.Range, bool) {
	c := pp.cache
	if c.dummy >= 0 {
		return c.dummy, c.dummyRange(), true
	}
	if c.records == "" || pp.srcName != "" || pp.srcSheet == "" || len(c.fields) == 0 || len(doc.Sheets) >= maxSheets {
		return 0, sheet.Range{}, false
	}
	root, err := r.pkg.ParseXML(r.ctx, c.records, understood)
	if err != nil {
		return 0, sheet.Range{}, false
	}
	sh := &sheet.Sheet{Name: "DPCache_" + pp.srcSheet, Hidden: true}
	r.defaultSetup(sh)
	date := &sheet.Style{Format: cacheDateFormat}
	head := sheet.Row{Index: 0}
	for i, f := range c.fields {
		head.Cells = append(head.Cells, sheet.Cell{Column: i, Value: sheet.Value{Kind: sheet.Text, Text: f.name}})
	}
	sh.Rows = append(sh.Rows, head)
	for _, rec := range root.Kids() {
		if !rec.Is(nsMain, "r") || len(sh.Rows) > maxPivotRecords || r.cells > maxCells {
			continue
		}
		row := sheet.Row{Index: len(sh.Rows)}
		for i, it := range rec.Kids() {
			if i >= len(c.fields) {
				break
			}
			var v sheet.Value
			if it.Local == "x" {
				x := intAttr(it, "v", -1)
				if x < 0 || x >= len(c.fields[i].items) {
					continue
				}
				v = c.fields[i].items[x]
			} else {
				v = r.cacheItem(it)
			}
			if v.Kind == sheet.Empty || v.Kind == sheet.Error {
				// An error item restores as an empty cell.
				//
				// # Corpus evidence
				//
				//   - libreoffice/xlsx: sc/tdf122471.xlsx, whose cache restores an
				//     external source, prints its #N/A item as a column
				//     "(empty)".
				continue
			}
			cell := sheet.Cell{Column: i, Value: v}
			if it.Local == "d" {
				cell.Style = date
			}
			row.Cells = append(row.Cells, cell)
			r.cells++
		}
		sh.Rows = append(sh.Rows, row)
	}
	c.dummy, c.rows = len(doc.Sheets), len(sh.Rows)
	doc.Sheets = append(doc.Sheets, sh)
	return c.dummy, c.dummyRange(), c.rows > 1
}

// dummyRange returns the range of the cache's records sheet.
func (c *pivotCache) dummyRange() sheet.Range {
	return sheet.Range{LastRow: max(c.rows-1, 1), LastColumn: len(c.fields) - 1}
}
