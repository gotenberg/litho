package chart

// live returns c with the data of its series read from the cells their
// ranges name, through [Chart.Cells], in place of the file's caches: the
// values, categories, bubble sizes and the custom errors of their error
// bars. A range the cells do not give keeps its cache. A pivot chart
// keeps its caches, its data being the pivot table's. It returns c itself
// when it reads no cells, so c stays read-only.
//
// A numeric point read where the cache held a string cache, which states
// no number format, is written in the format of the range's first cell
// ([Chart.FormatResolver]), as the cache's text was.
//
// # Corpus evidence
//
//   - libreoffice/xlsx: chart2/hidden_cells.xlsx caches two points of the
//     four rows its ranges name, two rows hidden, under plotVisOnly 0;
//     Excel plots the four rows' values, 1 to 4 and 2 to 5.
//   - libreoffice/xlsx: chart2/barchart_totalsrow.xlsx's first chart
//     caches 9, 1, 3, 4 and 1; Excel draws the cells' 2, 1, 9, 1, 3, 4 and
//     1.
func live(c *Chart) *Chart {
	if c.Cells == nil || c.Pivot {
		return c
	}
	cc := *c
	cc.Groups = make([]*Group, len(c.Groups))
	for i, g := range c.Groups {
		gg := *g
		gg.Series = make([]*Series, len(g.Series))
		for j, s := range g.Series {
			ss := *s
			ss.Categories = c.fromCells(s.Categories)
			ss.Values = c.fromCells(s.Values)
			ss.Sizes = c.fromCells(s.Sizes)
			if len(s.ErrorBars) > 0 {
				ss.ErrorBars = make([]ErrorBars, len(s.ErrorBars))
				for k, e := range s.ErrorBars {
					e.PlusData, e.MinusData = c.fromCells(e.PlusData), c.fromCells(e.MinusData)
					ss.ErrorBars[k] = e
				}
			}
			gg.Series[j] = &ss
		}
		cc.Groups[i] = &gg
	}
	return &cc
}

// fromCells returns d with its values read from the cells its range
// names, d itself when it names none or [Chart.Cells] does not read it.
func (c *Chart) fromCells(d Data) Data {
	if d.Ref == "" {
		return d
	}
	vals := c.Cells(d.Ref)
	if vals == nil {
		return d
	}
	if len(vals) > MaxPoints {
		vals = vals[:MaxPoints]
	}
	d.Values, d.Cached = vals, true
	if d.Format == "" && c.FormatResolver != nil {
		for _, v := range vals {
			if v.Kind == Number {
				d.Format, _ = c.FormatResolver(d.Ref)
				break
			}
		}
	}
	return d
}
