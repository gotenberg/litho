package sheet

import (
	"math"
	"sort"
	"strconv"
)

// maxPivotEntries caps the rows or the columns of one pivot table's
// result. Anti-DoS guard, not spec-mandated: Calc stops a result past its
// sheet's 1,048,576 rows (SC_MINCOUNT_LIMIT), and a printed one is far
// smaller.
const maxPivotEntries = 1 << 16

// maxPivotResultCells caps the data cells of one pivot table's result,
// rows times columns. Anti-DoS guard, not spec-mandated: a million cells
// print on some twenty thousand pages.
const maxPivotResultCells = 1 << 20

// pivotLevel is one field of a pivot table's row or column axis.
type pivotLevel struct {
	f *PivotField
	// data marks the data layout field, whose members are the data
	// fields.
	data bool
	// order lists the items shown, in display order.
	order []int
}

// pivotCell is a member result: what one level shows on one row or
// column of the result (com.sun.star.sheet.MemberResult).
type pivotCell struct {
	// has marks a member shown, cont a row continuing the member above,
	// sub a subtotal or grand total caption.
	has, cont, sub bool
	caption        string
	// value is the member's value, shown as a value when it is a number.
	value Value
}

// pivotEntry is one row or column of a pivot table's result.
type pivotEntry struct {
	cells []pivotCell
	// key identifies the records the entry summarises: the items of the
	// real levels it constrains.
	key string
	// measure is the data field the entry shows, -1 when the other axis
	// or the only data field decides.
	measure int
	// force is the subtotal function of a subtotal entry, PivotAuto for
	// each data field's own.
	force PivotFunction
	// data marks an entry that shows results; a title row or an empty
	// line shows none.
	data bool
}

// pivotAxis is a pivot table's row or column axis.
type pivotAxis struct {
	levels  []pivotLevel
	entries []pivotEntry
	// dataLevel is the index of the data layout level, -1 for none.
	dataLevel int
	// byKey lists the data entries by key.
	byKey map[string][]int
	// row marks the row axis: column fields lay out tabular, without
	// empty lines (ScDPLevel::SetEnableLayout).
	row bool
}

// pivotRun computes one pivot table.
type pivotRun struct {
	b    *builder
	p    *Pivot
	src  *pivotSource
	recs []int
	// itemCaps caches the captions of each source field's items.
	itemCaps [][]string
	// overflow marks a result past the caps.
	overflow bool
}

// captions returns the captions of the items of the source field f,
// computed once.
func (pr *pivotRun) captions(f int) []string {
	if pr.itemCaps == nil {
		pr.itemCaps = make([][]string, len(pr.src.names))
	}
	if pr.itemCaps[f] == nil {
		items := pr.src.items[f]
		caps := make([]string, len(items))
		for i, v := range items {
			caps[i] = pr.caption(f, v)
		}
		pr.itemCaps[f] = caps
	}
	return pr.itemCaps[f]
}

// shownItems returns, per item of the field f's source column, whether
// its member is shown: the members the field hides by name are not, nor
// for a page field with a selection those it does not select (Calc finds
// a member by its formatted name, ScDPSaveDimension::GetMemberByName,
// and a selected page hides the others, SetCurrentPage).
func (pr *pivotRun) shownItems(f *PivotField, page bool) []bool {
	caps := pr.captions(f.Field)
	shown := make([]bool, len(caps))
	names := map[string]bool{}
	for _, v := range f.Hidden {
		names[pr.caption(f.Field, v)] = true
	}
	sel := ""
	if page && f.Selected != nil {
		sel = pr.caption(f.Field, *f.Selected)
	}
	for i, c := range caps {
		shown[i] = !names[c]
		if page && f.Selected != nil {
			shown[i] = c == sel
		}
	}
	return shown
}

// active returns the records the pivot table summarises: those whose
// page, row and column fields hold a shown member
// (ScDPSource::FilterCacheByPageDimensions, ScDPResultMember::IsValid).
func (pr *pivotRun) active() []int {
	type filter struct {
		field int
		shown []bool
	}
	var filters []filter
	add := func(fs []PivotField, page bool) {
		for i := range fs {
			f := &fs[i]
			if f.Field < 0 || f.Field >= len(pr.src.names) || (len(f.Hidden) == 0 && (!page || f.Selected == nil)) {
				continue
			}
			filters = append(filters, filter{field: f.Field, shown: pr.shownItems(f, page)})
		}
	}
	add(pr.p.Pages, true)
	add(pr.p.Rows, false)
	add(pr.p.Columns, false)
	var out []int
	for i := range pr.src.recs {
		ok := true
		for _, ft := range filters {
			if !ft.shown[pr.src.ids[ft.field][i]] {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, i)
		}
	}
	return out
}

// level returns the axis level of the field f.
func (pr *pivotRun) level(f *PivotField) pivotLevel {
	lv := pivotLevel{f: f, data: f.Field == PivotDataLayout}
	if lv.data {
		return lv
	}
	items := pr.src.items[f.Field]
	caps := pr.captions(f.Field)
	shown := pr.shownItems(f, false)
	idx := make([]int, len(items))
	for i := range idx {
		idx[i] = i
	}
	switch f.Sort {
	case PivotDescending:
		// The empty item stays last (ScDPItemData sorts it after all).
		sort.SliceStable(idx, func(a, b int) bool {
			ea, eb := items[idx[a]].Kind == Empty, items[idx[b]].Kind == Empty
			if ea != eb {
				return eb
			}
			return idx[a] > idx[b]
		})
	case PivotManual:
		// The members the file lists first, in its order, then the rest
		// ascending (ScDPSaveDimension member positions).
		pos := map[string]int{}
		for i, v := range f.Order {
			c := pr.caption(f.Field, v)
			if _, ok := pos[c]; !ok {
				pos[c] = i
			}
		}
		sort.SliceStable(idx, func(a, b int) bool {
			pa, oka := pos[caps[idx[a]]]
			pb, okb := pos[caps[idx[b]]]
			switch {
			case oka && okb:
				return pa < pb
			case oka != okb:
				return oka
			}
			return idx[a] < idx[b]
		})
	case PivotAscending:
	}
	for _, i := range idx {
		if shown[i] {
			lv.order = append(lv.order, i)
		}
	}
	return lv
}

// axis builds the row or column axis of the fields fs, with a grand
// total when grand is set.
func (pr *pivotRun) axis(fs []PivotField, grand, row bool) *pivotAxis {
	a := &pivotAxis{dataLevel: -1, byKey: map[string][]int{}, row: row}
	real := false
	for i := range fs {
		f := &fs[i]
		if f.Field != PivotDataLayout && (f.Field < 0 || f.Field >= len(pr.src.names)) {
			continue
		}
		if f.Field == PivotDataLayout {
			// The data layout field shows only for two data fields or
			// more (ScDPSource::FillCalcInfo).
			if a.dataLevel >= 0 || len(pr.p.Data) < 2 {
				continue
			}
			a.dataLevel = len(a.levels)
		} else {
			real = true
		}
		a.levels = append(a.levels, pr.level(f))
	}
	if len(a.levels) == 0 {
		a.entries = []pivotEntry{{measure: -1, data: true}}
		a.index()
		return a
	}
	ab := &axisBuilder{pr: pr, a: a}
	ab.children(0, pr.recs, "", -1)
	if grand && real {
		// The grand total is the root member's subtotal, "Total Result",
		// or one per data field when the data layout field is on this
		// axis (ScDPResultMember::FillMemberResults, bForceSubTotal).
		for _, m := range ab.measures(a.dataLevel >= 0, -1) {
			e := ab.entry("", m)
			e.data = true
			e.cells[0] = pivotCell{has: true, sub: true, caption: "Total " + pr.measureString(m, PivotAuto)}
			ab.push(e)
		}
	}
	if len(a.entries) == 0 {
		a.entries = []pivotEntry{{cells: make([]pivotCell, len(a.levels)), measure: -1}}
	}
	a.index()
	return a
}

// index fills byKey.
func (a *pivotAxis) index() {
	for i := range a.entries {
		if a.entries[i].data {
			a.byKey[a.entries[i].key] = append(a.byKey[a.entries[i].key], i)
		}
	}
}

// axisBuilder generates an axis's entries.
type axisBuilder struct {
	pr *pivotRun
	a  *pivotAxis
}

// entry returns a new entry of key and measure.
func (ab *axisBuilder) entry(key string, measure int) pivotEntry {
	return pivotEntry{cells: make([]pivotCell, len(ab.a.levels)), key: key, measure: measure}
}

// push appends e unless the axis is full.
func (ab *axisBuilder) push(e pivotEntry) {
	if len(ab.a.entries) >= maxPivotEntries {
		ab.pr.overflow = true
		return
	}
	ab.a.entries = append(ab.a.entries, e)
}

// measures returns the data fields subtotals are shown for: every one
// when all is set, else the measure m alone.
func (ab *axisBuilder) measures(all bool, m int) []int {
	if !all {
		return []int{m}
	}
	out := make([]int, len(ab.pr.p.Data))
	for i := range out {
		out[i] = i
	}
	return out
}

// children generates the members of level li over the records recs.
func (ab *axisBuilder) children(li int, recs []int, key string, measure int) {
	if li >= len(ab.a.levels) || ab.pr.overflow {
		return
	}
	lv := &ab.a.levels[li]
	if lv.data {
		for m := range ab.pr.p.Data {
			ab.member(li, m, recs, key, m)
		}
		return
	}
	f := lv.f.Field
	ids := ab.pr.src.ids[f]
	shown := lv.order
	if !lv.f.ShowEmpty {
		// Only the members with data, as Calc's late initialisation
		// creates them (ScDPResultMember::LateInitFrom).
		present := map[int]bool{}
		for _, r := range recs {
			present[ids[r]] = true
		}
		shown = nil
		for _, i := range lv.order {
			if present[i] {
				shown = append(shown, i)
			}
		}
	}
	for _, item := range shown {
		var sub []int
		for _, r := range recs {
			if ids[r] == item {
				sub = append(sub, r)
			}
		}
		ab.member(li, item, sub, key+strconv.Itoa(item)+",", measure)
	}
}

// subtotals returns the visible subtotal functions of the level lv
// (ScDPResultMember::GetSubTotalCount).
func subtotals(lv *pivotLevel) []PivotFunction {
	if lv.data {
		return nil
	}
	return lv.f.Subtotals
}

// member generates the rows of the member item (or data field) of level
// li: its title, its children, its subtotals and its empty line, as
// ScDPResultMember::FillMemberResults lays them out.
func (ab *axisBuilder) member(li, item int, recs []int, key string, measure int) {
	a, pr := ab.a, ab.pr
	lv := &a.levels[li]
	var caption string
	var value Value
	if lv.data {
		caption = pr.measureString(item, PivotAuto)
		value = Value{Kind: Text, Text: caption}
	} else {
		value = pr.src.items[lv.f.Field][item]
		caption = pr.caption(lv.f.Field, value)
	}
	start := len(a.entries)
	if li == len(a.levels)-1 {
		e := ab.entry(key, measure)
		e.data = true
		ab.push(e)
	} else {
		// The innermost level's layout is off (ScDPSource::CreateRes_Impl):
		// a level with children lays out by its own.
		layout := lv.f.Layout
		if !a.row {
			layout = PivotTabular
		}
		outline := layout != PivotTabular
		top := layout == PivotOutlineTop || layout == PivotCompact
		subs := subtotals(lv)
		all := a.dataLevel > li
		visible := len(subs)
		if all {
			visible *= len(pr.p.Data)
		}
		inTitle := outline && top && visible == 1
		if outline {
			e := ab.entry(key, measure)
			if inTitle {
				e.data = true
				e.force = subs[0]
				if all {
					e.measure = 0
				}
			}
			ab.push(e)
		}
		ab.children(li+1, recs, key, measure)
		if visible > 0 && !inTitle {
			for _, fn := range subs {
				for _, m := range ab.measures(all, measure) {
					e := ab.entry(key, m)
					e.data = true
					e.force = fn
					e.cells[li] = pivotCell{has: true, sub: true, caption: caption + " " + pr.measureString(m, fn)}
					ab.push(e)
				}
			}
		}
		if lv.f.AddEmpty && a.row {
			ab.push(ab.entry(key, measure))
		}
	}
	if start >= len(a.entries) {
		return
	}
	c := &a.entries[start].cells[li]
	if !c.sub {
		*c = pivotCell{has: true, caption: caption, value: value}
	}
	for k := start + 1; k < len(a.entries); k++ {
		if !a.entries[k].cells[li].sub {
			a.entries[k].cells[li].cont = true
		}
	}
}

// caption returns what a member of the source field f shows: its value
// formatted by the field's number format, "(empty)" for none
// (ScDPCache::GetFormattedString, STR_EMPTYDATA).
func (pr *pivotRun) caption(f int, v Value) string {
	var s string
	switch v.Kind {
	case Text, Error:
		s = v.Text
	case Number, Bool:
		s = pr.b.format(pr.src.formats[f]).Apply(numValue(v)).Text
	case Empty:
	}
	if s == "" {
		return "(empty)"
	}
	return s
}

// functionName returns Calc's name of a summary function
// (ScDPUtil::getDisplayedMeasureName, the STR_FUN_TEXT strings).
func functionName(fn PivotFunction) string {
	switch fn {
	case PivotSum:
		return "Sum"
	case PivotCount, PivotCountNums:
		return "Count"
	case PivotAverage:
		return "Average"
	case PivotMax:
		return "Max"
	case PivotMin:
		return "Min"
	case PivotProduct:
		return "Product"
	case PivotStdDev, PivotStdDevP:
		return "StDev"
	case PivotVar, PivotVarP:
		return "Var"
	case PivotMedian:
		return "Median"
	case PivotAuto:
	}
	return ""
}

// measureString returns the caption of the data field m in a subtotal of
// the function fn: "Result" when m is -1 or the only data field, else the
// data field's caption, or "Sum - name"; a forced function names itself
// (ScDPResultData::GetMeasureString).
func (pr *pivotRun) measureString(m int, fn PivotFunction) string {
	if m < 0 || (len(pr.p.Data) == 1 && fn == PivotAuto) {
		if fn != PivotAuto {
			return functionName(fn)
		}
		return "Result"
	}
	return pr.dataName(m, fn)
}

// dataName returns the name the data field m shows: its caption, or the
// function's name and the field's, the function fn when not PivotAuto.
func (pr *pivotRun) dataName(m int, fn PivotFunction) string {
	if m < 0 || m >= len(pr.p.Data) {
		return "(empty)"
	}
	d := &pr.p.Data[m]
	if d.Caption != "" {
		return d.Caption
	}
	if fn == PivotAuto {
		fn = d.Function
	}
	name := ""
	if d.Field >= 0 && d.Field < len(pr.src.names) {
		name = pr.src.names[d.Field]
	}
	if n := functionName(fn); n != "" {
		return n + " - " + name
	}
	return name
}

// recordKeys returns the keys of record r on the axis a, one per depth of
// its real levels, the empty key first.
func (pr *pivotRun) recordKeys(a *pivotAxis, r int) []string {
	keys := []string{""}
	k := ""
	for i := range a.levels {
		if a.levels[i].data {
			continue
		}
		k += strconv.Itoa(pr.src.ids[a.levels[i].f.Field][r]) + ","
		keys = append(keys, k)
	}
	return keys
}

// pivotAgg accumulates one result (ScDPAggData).
type pivotAgg struct {
	// count is the values counted, -1 after an error.
	count      int
	sum, prod  float64
	mean, m2   float64
	minV, maxV float64
	vals       []float64
}

// add adds the value v under the function fn (ScDPAggData::Update):
// the empty value counts for nothing, a string only for PivotCount, an
// error turns the others into an error.
func (g *pivotAgg) add(v Value, fn PivotFunction) {
	if g.count < 0 || v.Kind == Empty {
		return
	}
	if fn != PivotCount {
		switch v.Kind {
		case Error:
			g.count = -1
			return
		case Text:
			return
		case Empty, Number, Bool:
		}
	}
	g.count++
	x := v.Number
	switch fn {
	case PivotSum, PivotAverage:
		g.sum += x
	case PivotProduct:
		if g.count == 1 {
			g.prod = x
		} else {
			g.prod *= x
		}
	case PivotMax:
		if g.count == 1 || x > g.maxV {
			g.maxV = x
		}
	case PivotMin:
		if g.count == 1 || x < g.minV {
			g.minV = x
		}
	case PivotStdDev, PivotStdDevP, PivotVar, PivotVarP:
		d := x - g.mean
		g.mean += d / float64(g.count)
		g.m2 += d * (x - g.mean)
	case PivotMedian:
		if len(g.vals) < maxPivotRecords {
			g.vals = append(g.vals, x)
		}
	case PivotAuto, PivotCount, PivotCountNums:
	}
}

// result returns the value of the accumulated result under fn, false for
// an empty one (ScDPAggData::Calculate): "#VALUE!" for an error.
func (g *pivotAgg) result(fn PivotFunction) (Value, bool) {
	if g == nil || g.count == 0 {
		return Value{}, false
	}
	errV := Value{Kind: Error, Text: "#VALUE!"}
	if g.count < 0 {
		return errV, true
	}
	n := float64(g.count)
	var x float64
	switch fn {
	case PivotSum:
		x = g.sum
	case PivotProduct:
		x = g.prod
	case PivotCount, PivotCountNums:
		x = n
	case PivotAverage:
		x = g.sum / n
	case PivotMax:
		x = g.maxV
	case PivotMin:
		x = g.minV
	case PivotVar, PivotStdDev:
		if g.count < 2 {
			return errV, true
		}
		x = g.m2 / (n - 1)
		if fn == PivotStdDev {
			x = math.Sqrt(x)
		}
	case PivotVarP, PivotStdDevP:
		x = g.m2 / n
		if fn == PivotStdDevP {
			x = math.Sqrt(x)
		}
	case PivotMedian:
		v := append([]float64(nil), g.vals...)
		sort.Float64s(v)
		if len(v) == 0 {
			return Value{}, false
		}
		x = v[len(v)/2]
		if len(v)%2 == 0 {
			x = (v[len(v)/2-1] + v[len(v)/2]) / 2
		}
	case PivotAuto:
		return Value{}, false
	}
	if !finite(x) {
		return errV, true
	}
	return Value{Kind: Number, Number: x}, true
}

// results computes the data cells of rows by cols: per data entry pair,
// the accumulated records of both.
func (pr *pivotRun) results(rows, cols *pivotAxis) []*pivotAgg {
	nr, nc := len(rows.entries), len(cols.entries)
	if nr*nc > maxPivotResultCells || len(pr.p.Data) == 0 {
		if nr*nc > maxPivotResultCells {
			pr.overflow = true
		}
		return nil
	}
	aggs := make([]*pivotAgg, nr*nc)
	for _, r := range pr.recs {
		var re, ce []int
		for _, k := range pr.recordKeys(rows, r) {
			re = append(re, rows.byKey[k]...)
		}
		for _, k := range pr.recordKeys(cols, r) {
			ce = append(ce, cols.byKey[k]...)
		}
		for _, i := range re {
			for _, j := range ce {
				m, fn, ok := pr.cellFunction(&rows.entries[i], &cols.entries[j])
				if !ok {
					continue
				}
				g := aggs[i*nc+j]
				if g == nil {
					g = &pivotAgg{}
					aggs[i*nc+j] = g
				}
				d := pr.p.Data[m].Field
				if d < 0 || d >= len(pr.src.names) {
					continue
				}
				g.add(pr.src.recs[r][d], fn)
			}
		}
	}
	return aggs
}

// cellFunction returns the data field and the function of the result at
// row entry re and column entry ce, false when a row and a column
// subtotal force different functions (ScDPAggData::Update).
func (pr *pivotRun) cellFunction(re, ce *pivotEntry) (int, PivotFunction, bool) {
	m := 0
	switch {
	case re.measure >= 0:
		m = re.measure
	case ce.measure >= 0:
		m = ce.measure
	}
	if m >= len(pr.p.Data) {
		return 0, 0, false
	}
	fn := pr.p.Data[m].Function
	if re.force != PivotAuto && ce.force != PivotAuto && re.force != ce.force {
		return 0, 0, false
	}
	if re.force != PivotAuto {
		fn = re.force
	}
	if ce.force != PivotAuto {
		fn = ce.force
	}
	if fn == PivotAuto {
		fn = PivotSum
	}
	return m, fn, true
}
