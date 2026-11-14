package xlsx

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/chart"
	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// maxSparklines caps the sparklines read from one sheet. Anti-DoS guard,
// not spec-mandated: a dashboard holds a sparkline a row of a few
// hundred rows, and each one is a drawing of its own.
const maxSparklines = 4096

// sparklines reads the sparkline groups of the worksheet's extLst
// (MS-XLSX, 2.6.90 CT_SparklineGroup, 2.6.88 CT_Sparkline): each
// sparkline becomes a drawing of its cell (xm:sqref) that draws the
// values of its range (xm:f), resolved when the page is drawn, so that a
// range on a later sheet is read.
func (w *sheetReader) sparklines(root *ooxml.Element) {
	r := w.r
	n := 0
	for _, x := range root.Child(nsMain, "extLst").Kids() {
		for _, gs := range x.Kids() {
			if !gs.Is(nsX14, "sparklineGroups") {
				continue
			}
			for _, ge := range gs.Kids() {
				if !ge.Is(nsX14, "sparklineGroup") {
					continue
				}
				g := r.sparkGroup(ge)
				var refs []string
				for _, sp := range ge.Child(nsX14, "sparklines").Kids() {
					if !sp.Is(nsX14, "sparkline") {
						continue
					}
					row, col, ok := cellRef(strings.TrimSpace(textOf(sp.Child(nsXM, "sqref"))))
					if !ok {
						continue
					}
					if n >= maxSparklines || len(w.sh.Drawings) >= maxDrawings {
						r.warn(warn.LimitReached, warn.Omitted, fmt.Sprintf("sheet %q holds more than %d sparklines: the rest are left out", w.sh.Name, maxSparklines))
						return
					}
					n++
					to := sheet.Anchor{Row: row + 1, Column: col + 1}
					w.sh.Drawings = append(w.sh.Drawings, sheet.Drawing{
						From: sheet.Anchor{Row: row, Column: col}, To: &to, Shape: true,
						Name: "sparkline", Picture: g.Picture(len(refs)),
					})
					refs = append(refs, strings.TrimSpace(textOf(sp.Child(nsXM, "f"))))
				}
				g.Count = len(refs)
				g.Values = func(i int) []chart.Value { return r.rangeValues(refs[i]) }
			}
		}
	}
}

// sparkGroup reads the look of a sparkline group (MS-XLSX, 2.6.90): its
// type, colours, the points it marks, its value axis and how it shows
// empty cells; Excel's defaults where the group says nothing.
func (r *reader) sparkGroup(e *ooxml.Element) *sheet.SparkGroup {
	g := &sheet.SparkGroup{LineWeight: 0.75}
	switch attr(e, "type") {
	case "column":
		g.Kind = sheet.SparkColumn
	case "stacked":
		g.Kind = sheet.SparkWinLoss
	}
	switch attr(e, "displayEmptyCellsAs") {
	case "zero":
		g.Empty = sheet.SparkZero
	case "span":
		g.Empty = sheet.SparkSpan
	}
	if v, err := strconv.ParseFloat(attr(e, "lineWeight"), 64); err == nil && v > 0 {
		g.LineWeight = min(v, 1584)
	}
	col := func(local string, def typeset.Color) typeset.Color {
		if c, ok := r.color(e.Child(nsX14, local)); ok {
			return typesetColor(c)
		}
		return def
	}
	black := typeset.Color{A: 1}
	series := typeset.Color{R: 0x37 / 255.0, G: 0x60 / 255.0, B: 0x92 / 255.0, A: 1}
	g.Series = col("colorSeries", series)
	g.Negative = col("colorNegative", typeset.Color{R: 0xD0 / 255.0, A: 1})
	g.Axis = col("colorAxis", black)
	g.Markers = col("colorMarkers", series)
	g.First = col("colorFirst", series)
	g.Last = col("colorLast", series)
	g.High = col("colorHigh", series)
	g.Low = col("colorLow", series)
	g.ShowNegative = boolAttr(e, "negative")
	g.ShowMarkers = boolAttr(e, "markers")
	g.ShowFirst = boolAttr(e, "first")
	g.ShowLast = boolAttr(e, "last")
	g.ShowHigh = boolAttr(e, "high")
	g.ShowLow = boolAttr(e, "low")
	g.ShowAxis = boolAttr(e, "displayXAxis")
	g.RightToLeft = boolAttr(e, "rightToLeft")
	axis := func(name string) sheet.SparkAxis {
		switch attr(e, name) {
		case "group":
			return sheet.SparkGroupAxis
		case "custom":
			return sheet.SparkCustom
		}
		return sheet.SparkIndividual
	}
	g.MinAxis, g.MaxAxis = axis("minAxisType"), axis("maxAxisType")
	for _, m := range []struct {
		name string
		v    *float64
	}{{"manualMin", &g.ManualMin}, {"manualMax", &g.ManualMax}} {
		if v, err := strconv.ParseFloat(attr(e, m.name), 64); err == nil && numeric.Finite(v) {
			*m.v = v
		}
	}
	return g
}
