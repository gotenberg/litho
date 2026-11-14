package ods

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/chart"
	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/odf"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// maxSparklines caps the sparklines one document places. Anti-DoS guard,
// not spec-mandated: each is a drawing of its own, and a dashboard holds
// a few hundred.
const maxSparklines = 4096

// sparklines reads the calcext:sparkline-groups element el of the table
// sh, the sparklines Calc writes (calcext:sparkline-group with its
// calcext:sparkline children, the form of MS-XLSX, 2.6.90 that LibreOffice
// keeps): each sparkline is a drawing of its cell (calcext:cell-address)
// drawing the values of its range (calcext:data-range), resolved when the
// page is drawn. The rows of the sparklines' cells keep their own
// heights, as an empty row of a height of its own does when content
// follows it.
func (r *reader) sparklines(ts *tableState, el *odf.Element) {
	sh := ts.sh
	last := -1
	defer func() { r.flushPending(ts, last+1) }()
	for _, ge := range el.Children {
		if !ge.Is(nsCalcExt, "sparkline-group") {
			continue
		}
		g := sparkGroup(ge)
		var refs []string
		for _, sp := range ge.Child(nsCalcExt, "sparklines").Children {
			if !sp.Is(nsCalcExt, "sparkline") {
				continue
			}
			at, ok := cellRef(sp.A(nsCalcExt, "cell-address"))
			if !ok || at.whole {
				continue
			}
			if r.sparks >= maxSparklines {
				r.warn("too-many-sparklines", warn.LimitReached, warn.Omitted, fmt.Sprintf("sparklines past %d are not drawn", maxSparklines))
				return
			}
			r.sparks++
			last = max(last, at.row)
			to := sheet.Anchor{Row: at.row + 1, Column: at.col + 1}
			sh.Drawings = append(sh.Drawings, sheet.Drawing{
				From: sheet.Anchor{Row: at.row, Column: at.col}, To: &to, Shape: true,
				Name: "sparkline", Picture: g.Picture(len(refs)),
			})
			refs = append(refs, strings.TrimSpace(sp.A(nsCalcExt, "data-range")))
		}
		g.Count = len(refs)
		g.Values = func(i int) []chart.Value { return r.rangeValues(refs[i]) }
	}
}

// sparkGroup reads the look of a calcext:sparkline-group: its type,
// colours (the sRGB attributes, which LibreOffice writes beside the theme
// colours they resolve to), the points it marks, its value axis and how it
// shows empty cells.
func sparkGroup(e *odf.Element) *sheet.SparkGroup {
	g := &sheet.SparkGroup{LineWeight: 0.75}
	a := func(name string) string { return e.A(nsCalcExt, name) }
	switch a("type") {
	case "column":
		g.Kind = sheet.SparkColumn
	case "stacked":
		g.Kind = sheet.SparkWinLoss
	}
	switch a("display-empty-cells-as") {
	case "zero":
		g.Empty = sheet.SparkZero
	case "span":
		g.Empty = sheet.SparkSpan
	}
	if v, ok := odf.Length(a("line-width")); ok && v > 0 {
		g.LineWeight = min(v, 1584)
	}
	col := func(name string, def typeset.Color) typeset.Color {
		c, ok := odf.ParseColor(a(name))
		if !ok {
			return def
		}
		return typeset.Color{R: float64(c.R) / 255, G: float64(c.G) / 255, B: float64(c.B) / 255, A: 1}
	}
	series := typeset.Color{R: 0x37 / 255.0, G: 0x60 / 255.0, B: 0x92 / 255.0, A: 1}
	g.Series = col("color-series", series)
	g.Negative = col("color-negative", typeset.Color{R: 0xD0 / 255.0, A: 1})
	g.Axis = col("color-axis", typeset.Color{A: 1})
	g.Markers = col("color-markers", series)
	g.First = col("color-first", series)
	g.Last = col("color-last", series)
	g.High = col("color-high", series)
	g.Low = col("color-low", series)
	on := func(name string) bool { return a(name) == "true" }
	g.ShowNegative, g.ShowMarkers = on("negative"), on("markers")
	g.ShowFirst, g.ShowLast, g.ShowHigh, g.ShowLow = on("first"), on("last"), on("high"), on("low")
	g.ShowAxis, g.RightToLeft = on("display-x-axis"), on("right-to-left")
	axis := func(name string) sheet.SparkAxis {
		switch a(name) {
		case "group":
			return sheet.SparkGroupAxis
		case "custom":
			return sheet.SparkCustom
		}
		return sheet.SparkIndividual
	}
	g.MinAxis, g.MaxAxis = axis("min-axis-type"), axis("max-axis-type")
	for _, m := range []struct {
		name string
		v    *float64
	}{{"manual-min", &g.ManualMin}, {"manual-max", &g.ManualMax}} {
		if v, err := strconv.ParseFloat(a(m.name), 64); err == nil && numeric.Finite(v) {
			*m.v = v
		}
	}
	return g
}
