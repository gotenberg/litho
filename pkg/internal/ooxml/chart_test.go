package ooxml

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/chart"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/shape"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// chartXML wraps a plot area's content and the chart's other children in
// a chart space.
func chartXML(style, chartKids, plotArea string) string {
	return `<c:chartSpace xmlns:c="http://schemas.openxmlformats.org/drawingml/2006/chart" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main">` +
		style + `<c:chart>` + chartKids + `<c:plotArea>` + plotArea + `</c:plotArea></c:chart></c:chartSpace>`
}

// ser returns a category series with a name, categories and values.
func ser(idx int, name, extra string, vals ...string) string {
	var pts strings.Builder
	for i, v := range vals {
		pts.WriteString(`<c:pt idx="` + string(rune('0'+i)) + `"><c:v>` + v + `</c:v></c:pt>`)
	}
	n := string(rune('0' + len(vals)))
	return `<c:ser><c:idx val="` + string(rune('0'+idx)) + `"/><c:order val="` + string(rune('0'+idx)) + `"/>` +
		`<c:tx><c:strRef><c:f>Sheet1!$B$1</c:f><c:strCache><c:ptCount val="1"/><c:pt idx="0"><c:v>` + name + `</c:v></c:pt></c:strCache></c:strRef></c:tx>` + extra +
		`<c:cat><c:strRef><c:f>Sheet1!$A$2:$A$3</c:f><c:strCache><c:ptCount val="` + n + `"/>` + strings.ReplaceAll(pts.String(), "<c:v>", "<c:v>c") + `</c:strCache></c:strRef></c:cat>` +
		`<c:val><c:numRef><c:f>Sheet1!$B$2:$B$3</c:f><c:numCache><c:formatCode>0.0</c:formatCode><c:ptCount val="` + n + `"/>` + pts.String() + `</c:numCache></c:numRef></c:val></c:ser>`
}

const axes = `<c:catAx><c:axId val="1"/><c:scaling><c:orientation val="maxMin"/></c:scaling><c:delete val="0"/><c:axPos val="b"/><c:majorTickMark val="out"/><c:tickLblPos val="nextTo"/><c:crossAx val="2"/><c:crosses val="autoZero"/></c:catAx>` +
	`<c:valAx><c:axId val="2"/><c:scaling><c:logBase val="10"/><c:orientation val="minMax"/><c:max val="100"/><c:min val="1"/></c:scaling><c:delete val="0"/><c:axPos val="l"/><c:majorGridlines/><c:numFmt formatCode="0%" sourceLinked="0"/><c:tickLblPos val="low"/><c:crossAx val="1"/><c:crosses val="max"/><c:crossBetween val="midCat"/><c:majorUnit val="5"/>` +
	`<c:title><c:tx><c:rich><a:bodyPr rot="-5400000"/><a:p><a:r><a:rPr sz="1200" b="0"/><a:t>Amount</a:t></a:r></a:p></c:rich></c:tx></c:title></c:valAx>`

// readChart parses xml and reads its chart, failing the test on an error.
func readChart(t *testing.T, xml string, mso2007 bool) *chart.Chart {
	t.Helper()
	root, err := Parse(context.Background(), []byte(xml), nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	c, err := ChartFromElement(root, nil, ChartOptions{MSO2007: mso2007})
	if err != nil {
		t.Fatalf("ChartFromElement: %v", err)
	}
	return c
}

func TestChartFromElement_Kinds(t *testing.T) {
	two := ser(0, "A", "", "1", "2") + ser(1, "B", "", "3", "4")
	ids := `<c:axId val="1"/><c:axId val="2"/>`
	tests := []struct {
		name, plot string
		check      func(t *testing.T, g *chart.Group)
	}{
		{"clustered column", `<c:barChart><c:barDir val="col"/><c:grouping val="clustered"/>` + two + `<c:gapWidth val="219"/><c:overlap val="-27"/>` + ids + `</c:barChart>` + axes, func(t *testing.T, g *chart.Group) {
			if g.Kind != chart.Bar || g.Horizontal || g.Grouping != chart.Clustered || g.GapWidth != 219 || g.Overlap != -27 || g.XAxis != 0 || g.YAxis != 1 {
				t.Errorf("group = %+v", g)
			}
		}},
		{"stacked bar", `<c:barChart><c:barDir val="bar"/><c:grouping val="stacked"/>` + two + ids + `</c:barChart>` + axes, func(t *testing.T, g *chart.Group) {
			if !g.Horizontal || g.Grouping != chart.Stacked || g.GapWidth != 150 {
				t.Errorf("group = %+v", g)
			}
		}},
		{"percent 3D column", `<c:bar3DChart><c:barDir val="col"/><c:grouping val="percentStacked"/>` + two + ids + `</c:bar3DChart>` + axes, func(t *testing.T, g *chart.Group) {
			if !g.ThreeD || g.Grouping != chart.PercentStacked {
				t.Errorf("group = %+v", g)
			}
		}},
		{"line with markers", `<c:lineChart><c:grouping val="standard"/>` + ser(0, "A", `<c:marker><c:symbol val="triangle"/><c:size val="7"/></c:marker>`, "1", "2") + `<c:marker val="1"/>` + ids + `</c:lineChart>` + axes, func(t *testing.T, g *chart.Group) {
			s := g.Series[0]
			if g.Kind != chart.Line || s.Marker.Symbol != chart.MarkerArrowUp || s.Marker.Size != 7 || s.Border == nil || math.Abs(s.Border.Width-2.25) > 1e-9 || !s.Smooth {
				t.Errorf("series = %+v marker %+v border %+v", s, s.Marker, s.Border)
			}
		}},
		{"area", `<c:areaChart><c:grouping val="standard"/>` + two + ids + `</c:areaChart>` + axes, func(t *testing.T, g *chart.Group) {
			if g.Kind != chart.Area || len(g.Series) != 2 || g.Series[1].Name != "B" {
				t.Errorf("group = %+v", g)
			}
		}},
		{"pie", `<c:pieChart><c:varyColors val="1"/>` + ser(0, "A", `<c:explosion val="12"/><c:dPt><c:idx val="1"/><c:spPr><a:solidFill><a:srgbClr val="FF0000"/></a:solidFill></c:spPr></c:dPt>`, "1", "2") + `<c:firstSliceAng val="30"/></c:pieChart>`, func(t *testing.T, g *chart.Group) {
			s := g.Series[0]
			if g.Kind != chart.Pie || !g.VaryColors || g.FirstSliceAngle != 30 || s.Explosion != 12 || len(s.Points) != 1 || s.Points[0].Fill.Color.R != 1 {
				t.Errorf("group = %+v series %+v", g, s)
			}
		}},
		{"doughnut", `<c:doughnutChart><c:varyColors val="1"/>` + two + `<c:holeSize val="60"/></c:doughnutChart>`, func(t *testing.T, g *chart.Group) {
			if g.Kind != chart.Doughnut || g.HoleSize != 60 {
				t.Errorf("group = %+v", g)
			}
		}},
		{"doughnut hole without val", `<c:doughnutChart><c:varyColors val="1"/>` + two + `<c:holeSize/></c:doughnutChart>`, func(t *testing.T, g *chart.Group) {
			// CT_HoleSize's val defaults to 10% (21.2.2.82, 21.2.3.18).
			if g.HoleSize != 10 {
				t.Errorf("hole %v, want 10", g.HoleSize)
			}
		}},
		{"doughnut without hole", `<c:doughnutChart><c:varyColors val="1"/>` + two + `</c:doughnutChart>`, func(t *testing.T, g *chart.Group) {
			if g.HoleSize != 50 {
				t.Errorf("hole %v, want 50", g.HoleSize)
			}
		}},
		{"percent forms", `<c:barChart><c:barDir val="col"/>` + two + `<c:gapWidth val="80%"/><c:overlap val="-20%"/>` + ids + `</c:barChart>` + axes, func(t *testing.T, g *chart.Group) {
			// ST_GapAmount and ST_Overlap take a percent form (21.2.3.16,
			// 21.2.3.31).
			if g.GapWidth != 80 || g.Overlap != -20 {
				t.Errorf("gap %v overlap %v, want 80 and -20", g.GapWidth, g.Overlap)
			}
		}},
		{"val-less gap, overlap and bubble scale", `<c:bubbleChart>` + two + `<c:gapWidth/><c:overlap/><c:bubbleScale/>` + ids + `</c:bubbleChart>` + axes, func(t *testing.T, g *chart.Group) {
			if g.GapWidth != 150 || g.Overlap != 0 || g.BubbleScale != 100 {
				t.Errorf("group = %+v", g)
			}
		}},
		{"bar of pie", `<c:ofPieChart><c:ofPieType val="bar"/>` + ser(0, "A", "", "1") + `</c:ofPieChart>`, func(t *testing.T, g *chart.Group) {
			if g.Kind != chart.Pie {
				t.Errorf("group = %+v", g)
			}
		}},
		{"scatter", `<c:scatterChart><c:scatterStyle val="marker"/><c:ser><c:idx val="0"/><c:order val="0"/><c:xVal><c:numLit><c:ptCount val="2"/><c:pt idx="0"><c:v>1.5</c:v></c:pt><c:pt idx="1"><c:v>2.5</c:v></c:pt></c:numLit></c:xVal><c:yVal><c:numRef><c:f>Sheet1!$B$2:$B$3</c:f></c:numRef></c:yVal></c:ser>` + ids + `</c:scatterChart>` + axes, func(t *testing.T, g *chart.Group) {
			s := g.Series[0]
			if g.Kind != chart.Scatter || s.Categories.Len() != 2 || s.Categories.Values[1].Number != 2.5 || s.Values.Cached || s.Values.Ref != "Sheet1!$B$2:$B$3" || s.Border == nil || !s.Border.None || s.Name != "Series1" {
				t.Errorf("series = %+v", s)
			}
		}},
		{"bubble", `<c:bubbleChart><c:ser><c:idx val="0"/><c:order val="0"/><c:yVal><c:numLit><c:ptCount val="1"/><c:pt idx="0"><c:v>2</c:v></c:pt></c:numLit></c:yVal><c:bubbleSize><c:numLit><c:ptCount val="1"/><c:pt idx="0"><c:v>9</c:v></c:pt></c:numLit></c:bubbleSize></c:ser><c:bubbleScale val="50"/>` + ids + `</c:bubbleChart>` + axes, func(t *testing.T, g *chart.Group) {
			if g.Kind != chart.Bubble || g.BubbleScale != 50 || g.Series[0].Sizes.Values[0].Number != 9 {
				t.Errorf("group = %+v", g)
			}
		}},
		{"filled radar", `<c:radarChart><c:radarStyle val="filled"/>` + two + ids + `</c:radarChart>` + axes, func(t *testing.T, g *chart.Group) {
			if g.Kind != chart.Radar || !g.Filled {
				t.Errorf("group = %+v", g)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := readChart(t, chartXML("", "", tt.plot), false)
			if len(c.Groups) != 1 {
				t.Fatalf("got %d groups", len(c.Groups))
			}
			tt.check(t, c.Groups[0])
		})
	}
}

func TestChartFromElement_SeriesData(t *testing.T) {
	c := readChart(t, chartXML("", "", `<c:barChart><c:barDir val="col"/>`+ser(0, "Sales", "", "1", "2.5")+`<c:axId val="1"/><c:axId val="2"/></c:barChart>`+axes), false)
	s := c.Groups[0].Series[0]
	if s.Name != "Sales" || s.Categories.Values[1].Text != "c2.5" || s.Values.Values[1].Number != 2.5 || s.Values.Format != "0.0" || s.Values.Ref != "Sheet1!$B$2:$B$3" {
		t.Errorf("series = %+v", s)
	}
	// Style 2's first automatic colour is the theme's first accent.
	if c.Palette[0].B < c.Palette[0].R || !c.PaletteCycleTint {
		t.Errorf("palette = %v", c.Palette)
	}
}

func TestChartFromElement_Axes(t *testing.T) {
	c := readChart(t, chartXML("", "", `<c:barChart><c:barDir val="col"/>`+ser(0, "A", "", "1")+`<c:axId val="1"/><c:axId val="2"/></c:barChart>`+axes), false)
	if len(c.Axes) != 2 {
		t.Fatalf("got %d axes", len(c.Axes))
	}
	cat, val := c.Axes[0], c.Axes[1]
	if cat.Kind != chart.CategoryAxis || !cat.Reverse || cat.MajorTicks != chart.TickOut || cat.BetweenCategories || cat.Line == nil {
		t.Errorf("category axis = %+v", cat)
	}
	if val.Kind != chart.ValueAxis || val.Log != 10 || val.Min != 1 || val.Max != 100 || val.MajorUnit != 5 || val.Format != "0%" ||
		val.Labels != chart.LabelLow || val.Crosses != chart.CrossMax || val.MajorGrid == nil || val.BetweenCategories || val.MajorTicks != chart.TickCross {
		t.Errorf("value axis = %+v", val)
	}
	if val.Title == nil || val.Title.Text != "Amount" || val.Title.Style.Size != 12 || val.Title.Style.Bold || val.Title.Style.Rotation != 90 {
		t.Errorf("axis title = %+v", val.Title)
	}
}

// TestChartFromElement_TickMarkDefaults locks the tick marks of an axis:
// cross for a tick mark element without val, CT_TickMark's default
// (ECMA-376 Part 1, 21.2.2.101, 21.2.2.110), and the reader's own when
// the element is absent.
func TestChartFromElement_TickMarkDefaults(t *testing.T) {
	ax := `<c:catAx><c:axId val="1"/><c:scaling/><c:axPos val="b"/><c:minorTickMark/><c:crossAx val="2"/></c:catAx>` +
		`<c:valAx><c:axId val="2"/><c:scaling/><c:axPos val="l"/><c:majorTickMark/><c:crossAx val="1"/></c:valAx>`
	c := readChart(t, chartXML("", "", `<c:barChart><c:barDir val="col"/>`+ser(0, "A", "", "1")+`<c:axId val="1"/><c:axId val="2"/></c:barChart>`+ax), false)
	if len(c.Axes) != 2 {
		t.Fatalf("got %d axes", len(c.Axes))
	}
	if cat := c.Axes[0]; cat.MinorTicks != chart.TickCross || cat.MajorTicks != chart.TickCross {
		t.Errorf("category axis ticks %v %v, want cross and cross", cat.MajorTicks, cat.MinorTicks)
	}
	if val := c.Axes[1]; val.MajorTicks != chart.TickCross || val.MinorTicks != chart.TickNone {
		t.Errorf("value axis ticks %v %v, want cross and none", val.MajorTicks, val.MinorTicks)
	}
}

func TestChartFromElement_TitleLegendAndDefaults(t *testing.T) {
	kids := `<c:title><c:tx><c:rich><a:p><a:r><a:t>Line one</a:t></a:r></a:p><a:p><a:r><a:t>Line two</a:t></a:r></a:p></c:rich></c:tx><c:overlay val="1"/></c:title>` +
		`<c:legend><c:legendPos val="t"/><c:legendEntry><c:idx val="1"/><c:delete val="1"/></c:legendEntry><c:txPr><a:p><a:pPr><a:defRPr sz="800" i="1"><a:solidFill><a:srgbClr val="00FF00"/></a:solidFill><a:latin typeface="Arial"/></a:defRPr></a:pPr></a:p></c:txPr></c:legend>`
	c := readChart(t, chartXML(`<c:style val="10"/><c:txPr><a:p><a:pPr><a:defRPr sz="1000"/></a:pPr></a:p></c:txPr>`, kids, `<c:pieChart>`+ser(0, "A", "", "1")+`</c:pieChart>`), false)
	if c.Title == nil || c.Title.Text != "Line one\nLine two" || !c.Title.Overlay || !c.Title.Style.Bold || c.Title.Style.Size != 12 {
		t.Errorf("title = %+v", c.Title)
	}
	l := c.Legend
	if l == nil || l.Position != chart.LegendTop || len(l.Hidden) != 1 || l.Hidden[0] != 1 || l.Style.Size != 8 || !l.Style.Italic || l.Style.Color.G != 1 || l.Style.Families[0] != "Arial" {
		t.Errorf("legend = %+v", l)
	}
	// Style 10 outlines filled series in the light colour.
	if b := c.Groups[0].Series[0].Border; b == nil || b.Color.R != 1 {
		t.Errorf("series border = %+v", b)
	}
	if c.Area.Fill == nil || c.Area.Border == nil {
		t.Errorf("chart area = %+v", c.Area)
	}
}

func TestChartFromElement_AutomaticTitle(t *testing.T) {
	plot := `<c:barChart><c:barDir val="col"/>` + ser(0, "Only", "", "1") + `</c:barChart>`
	if c := readChart(t, chartXML("", "", plot), true); c.Title == nil || c.Title.Text != "Only" {
		t.Errorf("Office 2007 chart without a title: title = %+v, want the series name", c.Title)
	}
	if c := readChart(t, chartXML("", "", plot), false); c.Title != nil {
		t.Errorf("chart without a title: title = %+v, want none", c.Title)
	}
	if c := readChart(t, chartXML("", "<c:title/>", plot), false); c.Title == nil || c.Title.Text != "Only" {
		t.Errorf("empty title element: title = %+v, want the series name", c.Title)
	}
}

func TestChartFromElement_DataLabelsInherit(t *testing.T) {
	plot := `<c:barChart><c:barDir val="col"/>` + ser(0, "A", `<c:dLbls><c:dLbl><c:idx val="1"/><c:showVal val="0"/><c:showPercent val="1"/></c:dLbl><c:dLblPos val="outEnd"/><c:showCatName val="0"/></c:dLbls>`, "1", "2") +
		`<c:dLbls><c:showVal val="1"/><c:separator>, </c:separator></c:dLbls></c:barChart>`
	s := readChart(t, chartXML("", "", plot), false).Groups[0].Series[0]
	if !s.Labels.ShowValue || s.Labels.Placement != chart.LabelOutsideEnd || s.Labels.Separator != ", " {
		t.Errorf("series labels = %+v", s.Labels)
	}
	if len(s.Points) != 1 || !s.Points[0].HasLabels || s.Points[0].Labels.ShowValue || !s.Points[0].Labels.ShowPercent {
		t.Errorf("point labels = %+v", s.Points)
	}
}

func TestChartFromElement_DataLabelLayout(t *testing.T) {
	lay := func(idx, body string) string {
		return `<c:dLbl><c:idx val="` + idx + `"/><c:layout><c:manualLayout>` + body + `</c:manualLayout></c:layout><c:showVal val="1"/></c:dLbl>`
	}
	plot := `<c:pieChart>` + ser(0, "A", `<c:dLbls>`+lay("0", `<c:xMode val="edge"/><c:yMode val="edge"/><c:x val="0.25"/><c:y val="0.5"/>`)+
		lay("1", `<c:x val="-0.1"/><c:y val="1e300"/>`)+lay("2", `<c:xMode val="edge"/><c:x val="0.3"/><c:y val="0.05"/>`)+
		`<c:showVal val="1"/><c:showLeaderLines val="1"/></c:dLbls>`, "1", "2", "3") + `</c:pieChart>`
	s := readChart(t, chartXML("", "", plot), false).Groups[0].Series[0]
	if !s.Labels.Leaders || s.Labels.Layout != nil {
		t.Errorf("series labels = %+v, want leader lines and no layout", s.Labels)
	}
	want := []chart.LabelLayout{
		{X: 0.25, Y: 0.5, EdgeX: true, EdgeY: true},
		{X: -0.1, Y: maxLabelShift},
		{X: 0.3, Y: 0.05, EdgeX: true},
	}
	if len(s.Points) != len(want) {
		t.Fatalf("points = %+v", s.Points)
	}
	for i, w := range want {
		if l := s.Points[i].Labels; l.Layout == nil || *l.Layout != w || !l.Leaders {
			t.Errorf("point %d labels = %+v, want layout %+v", i, l, w)
		}
	}
}

func TestChartFromElement_DataLabelWidth(t *testing.T) {
	c15 := func(w string) string {
		return `<c:extLst><c:ext uri="{CE6537A1-D6FC-4f65-9D91-7224C49458BB}" xmlns:c15="http://schemas.microsoft.com/office/drawing/2012/chart">` +
			`<c15:layout><c:manualLayout><c:w val="` + w + `"/><c:h val="0.08"/></c:manualLayout></c15:layout></c:ext></c:extLst>`
	}
	lbl := func(idx, ext string) string {
		return `<c:dLbl><c:idx val="` + idx + `"/><c:showVal val="1"/>` + ext + `</c:dLbl>`
	}
	plot := `<c:pieChart>` + ser(0, "A", `<c:dLbls>`+lbl("0", c15("0.38"))+lbl("1", c15("7"))+lbl("2", c15("-1"))+lbl("3", "")+
		`<c:showVal val="1"/></c:dLbls>`, "1", "2", "3", "4") + `</c:pieChart>`
	s := readChart(t, chartXML("", "", plot), false).Groups[0].Series[0]
	// A width past the chart's is held to it; a negative one or none is 0.
	want := []float64{0.38, 1, 0, 0}
	if len(s.Points) != len(want) {
		t.Fatalf("points = %+v", s.Points)
	}
	for i, w := range want {
		if l := s.Points[i].Labels; l.Width != w || l.Layout != nil {
			t.Errorf("point %d labels = %+v, want width %v and no layout", i, l, w)
		}
	}
}

func TestChartFromElement_View3D(t *testing.T) {
	plot := `<c:bar3DChart><c:barDir val="col"/><c:grouping val="standard"/>` + ser(0, "A", "", "1", "2") +
		`<c:gapDepth val="80"/><c:shape val="cylinder"/><c:axId val="1"/><c:axId val="2"/><c:axId val="3"/></c:bar3DChart>` +
		`<c:catAx><c:axId val="1"/></c:catAx><c:valAx><c:axId val="2"/></c:valAx><c:serAx><c:axId val="3"/></c:serAx>`
	c := readChart(t, chartXML("", `<c:view3D><c:rotY val="45"/><c:depthPercent val="60"/><c:rAngAx/></c:view3D>`, plot), false)
	want := chart.View3D{RotX: 15, RotY: 45, Depth: 60, Perspective: 30, RightAngle: true}
	if c.View3D == nil || *c.View3D != want {
		t.Errorf("view = %+v, want %+v", c.View3D, want)
	}
	g := c.Groups[0]
	if !g.ThreeD || !g.Rows || g.Shape != chart.ShapeCylinder || g.GapDepth != 80 || g.ZAxis != 2 {
		t.Errorf("group = %+v", g)
	}
	// Out of range values are held to the schema's ranges: hPercent 5
	// to 500, perspective 0 to 240, depthPercent 20 to 2000, rotX -90 to
	// 90, rotY 0 to 360.
	wild := readChart(t, chartXML("", `<c:view3D><c:rotX val="-200"/><c:rotY val="900"/><c:depthPercent val="9999"/><c:hPercent val="1"/><c:perspective val="255"/></c:view3D>`, plot), false)
	want = chart.View3D{RotX: -90, RotY: 360, Depth: 2000, Perspective: 240, Height: 5}
	if wild.View3D == nil || *wild.View3D != want {
		t.Errorf("view = %+v, want %+v", wild.View3D, want)
	}
	// A pie without rotY keeps its first slice where it is.
	pie := `<c:pie3DChart>` + ser(0, "A", "", "1", "2") + `</c:pie3DChart>`
	if v := readChart(t, chartXML("", "", pie), false).View3D; v == nil || v.RotX != 15 || v.RotY != 0 {
		t.Errorf("pie view = %+v, want rotX 15, rotY 0", v)
	}
	// A flat chart keeps no view.
	flat := `<c:barChart><c:barDir val="col"/>` + ser(0, "A", "", "1") + `</c:barChart>`
	if v := readChart(t, chartXML("", `<c:view3D><c:rotX val="30"/></c:view3D>`, flat), false).View3D; v != nil {
		t.Errorf("flat chart view = %+v, want none", v)
	}
}

func TestChartFromElement_Warnings(t *testing.T) {
	plot := `<c:line3DChart>` + ser(0, "A", `<c:trendline><c:dispRSqr val="1"/></c:trendline><c:errBars/>`, "1") + `</c:line3DChart><c:surfaceChart/>`
	c := readChart(t, chartXML("", "", plot), false)
	var msgs []string
	for _, w := range c.Warnings {
		msgs = append(msgs, w.Message)
	}
	got := strings.Join(msgs, "|")
	for _, want := range []string{"equation", "lighting"} {
		if !strings.Contains(got, want) {
			t.Errorf("warnings %q lack %q", got, want)
		}
	}
	// Office's perspective is drawn: no warning says otherwise.
	if strings.Contains(got, "perspective") {
		t.Errorf("warnings %q speak of the perspective, which is drawn", got)
	}
	if _, err := ChartFromElement(&Element{Space: NSChart, Local: "chart"}, nil, ChartOptions{}); !errors.Is(err, office.ErrMalformed) {
		t.Errorf("a part that is not a chart space: %v, want office.ErrMalformed", err)
	}
}

func TestParseChart_ThemeAndOffice2007(t *testing.T) {
	files := minimalPackage()
	files["word/_rels/document.xml.rels"] = `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
		`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/theme" Target="theme/theme1.xml"/></Relationships>`
	files["word/theme/theme1.xml"] = `<a:theme xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><a:themeElements><a:clrScheme name="x">` +
		`<a:dk1><a:srgbClr val="000000"/></a:dk1><a:lt1><a:srgbClr val="FFFFFF"/></a:lt1><a:accent1><a:srgbClr val="123456"/></a:accent1></a:clrScheme>` +
		`<a:fontScheme name="f"><a:majorFont><a:latin typeface="Major"/></a:majorFont><a:minorFont><a:latin typeface="Minor"/></a:minorFont></a:fontScheme></a:themeElements></a:theme>`
	files["_rels/.rels"] = strings.Replace(packageRels, "</Relationships>", `<Relationship Id="rId9" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/extended-properties" Target="docProps/app.xml"/></Relationships>`, 1)
	files["docProps/app.xml"] = `<Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties"><Application>Microsoft Excel</Application><AppVersion>12.0000</AppVersion></Properties>`
	files["word/charts/chart1.xml"] = chartXML("", "", `<c:pieChart>`+ser(0, "Pies", "", "1", "2")+`</c:pieChart>`)
	pkg, err := Open(t.Context(), buildZip(t, files))
	if err != nil {
		t.Fatal(err)
	}
	c, err := ParseChart(context.Background(), pkg, "word/charts/chart1.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Palette[0]; math.Abs(got.R-0x12/255.0) > 1e-9 {
		t.Errorf("first automatic colour = %v, want the theme's accent1", got)
	}
	if c.Title == nil || c.Title.Text != "Pies" || c.Title.Style.Families[0] != "Minor" {
		t.Errorf("title = %+v, want the automatic title in the theme's minor font", c.Title)
	}
	if c.Groups[0].VaryColors {
		t.Error("an Office 2007 pie without varyColors varies its colours")
	}
	if _, err := ParseChart(context.Background(), pkg, "word/charts/missing.xml", nil); !IsMissing(err) {
		t.Errorf("missing part: err = %v", err)
	}
}

func TestChartFromElement_AutomaticFrames(t *testing.T) {
	plot := `<c:barChart><c:barDir val="col"/>` + ser(0, "A", "", "1") + `</c:barChart>`
	tests := []struct {
		name               string
		style              string
		presentation       bool
		areaFill, areaLine bool
		plotFill           bool
		plotGrey           float64
	}{
		{name: "spreadsheet style 2", style: `<c:style val="2"/>`, areaFill: true, areaLine: true, plotFill: true, plotGrey: 1},
		{name: "presentation style 2", style: `<c:style val="2"/>`, presentation: true},
		{name: "presentation style 47", style: `<c:style val="47"/>`, presentation: true, areaFill: true, plotFill: true, plotGrey: 0.25},
		{name: "spreadsheet style 34", style: `<c:style val="34"/>`, areaFill: true, areaLine: true, plotFill: true, plotGrey: 0.89},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, err := Parse(context.Background(), []byte(chartXML(tt.style, "", plot)), nil)
			if err != nil {
				t.Fatal(err)
			}
			c, err := ChartFromElement(root, nil, ChartOptions{Presentation: tt.presentation})
			if err != nil {
				t.Fatal(err)
			}
			if (c.Area.Fill != nil) != tt.areaFill || (c.Area.Border != nil) != tt.areaLine {
				t.Errorf("chart area = %+v", c.Area)
			}
			// Office's automatic chart area line: 0.75 points of grey 0.525.
			if b := c.Area.Border; tt.areaLine && b != nil && (math.Abs(b.Color.R-0.525) > 0.002 || b.Width != 0.75) {
				t.Errorf("chart area line = grey %.3f, %.2f points; want 0.525, 0.75", b.Color.R, b.Width)
			}
			if (c.PlotArea.Fill != nil) != tt.plotFill {
				t.Fatalf("plot area = %+v", c.PlotArea)
			}
			if tt.plotFill && math.Abs(c.PlotArea.Fill.Color.R-tt.plotGrey) > 0.02 {
				t.Errorf("plot area grey = %.3f, want %.3f", c.PlotArea.Fill.Color.R, tt.plotGrey)
			}
		})
	}
}

func TestChartFromElement_PointTakesSeriesFill(t *testing.T) {
	plot := `<c:pieChart><c:varyColors val="1"/>` + ser(0, "A", `<c:spPr><a:solidFill><a:srgbClr val="FF0000"/></a:solidFill></c:spPr><c:dPt><c:idx val="0"/></c:dPt><c:dPt><c:idx val="1"/><c:spPr><a:solidFill><a:srgbClr val="0000FF"/></a:solidFill></c:spPr></c:dPt>`, "1", "2", "3") + `</c:pieChart>`
	s := readChart(t, chartXML("", "", plot), false).Groups[0].Series[0]
	if len(s.Points) != 2 || s.Points[0].Fill == nil || s.Points[0].Fill.Color.R != 1 || s.Points[1].Fill == nil || s.Points[1].Fill.Color.B != 1 {
		t.Errorf("points = %+v", s.Points)
	}
}

// TestChartFromElement_UnnamedSeries holds a series without c:tx to the
// name Office prints for it, after its index, whatever its range.
func TestChartFromElement_UnnamedSeries(t *testing.T) {
	plot := `<c:barChart><c:barDir val="col"/><c:ser><c:idx val="2"/><c:order val="0"/><c:val><c:numRef><c:f>Sheet1!$B$1:$B$4</c:f></c:numRef></c:val></c:ser></c:barChart>`
	s := readChart(t, chartXML("", "", plot), false).Groups[0].Series[0]
	if s.Name != "Series3" || !s.Unnamed {
		t.Errorf("name = %q, unnamed %v; want Series3, the application's", s.Name, s.Unnamed)
	}
	if named := readChart(t, chartXML("", "", `<c:barChart>`+ser(0, "North", "", "1")+`</c:barChart>`), false).Groups[0].Series[0]; named.Unnamed {
		t.Errorf("a series its c:tx names reads as unnamed")
	}
}

// TestChartFromElement_ApplicationTitles marks the titles Office supplies
// where the file states none (ECMA-376 Part 1, 21.2.2.210 title with no
// tx; autoTitleDeleted false) apart from the file's own, and reads the
// kinds the renderer draws as others: a stock chart, drawn as lines, and a
// surface chart, not drawn.
func TestChartFromElement_ApplicationTitles(t *testing.T) {
	one := `<c:lineChart>` + ser(0, "Sales", "", "1", "2") + `</c:lineChart>`
	auto := readChart(t, chartXML("", `<c:autoTitleDeleted val="0"/>`, one), false)
	if auto.Title == nil || auto.Title.Text != "Sales" || !auto.Title.Auto {
		t.Errorf("auto title = %+v, want the series name, the application's", auto.Title)
	}
	placeholder := readChart(t, chartXML("", `<c:title/>`, `<c:lineChart>`+ser(0, "A", "", "1")+ser(1, "B", "", "1")+`</c:lineChart>`), false)
	if placeholder.Title == nil || placeholder.Title.Text != "Chart Title" || !placeholder.Title.Auto {
		t.Errorf("placeholder title = %+v", placeholder.Title)
	}
	own := readChart(t, chartXML("", `<c:title><c:tx><c:rich><a:p><a:r><a:t>Revenue</a:t></a:r></a:p></c:rich></c:tx></c:title>`, one), false)
	if own.Title == nil || own.Title.Auto {
		t.Errorf("the file's own title = %+v, want it not marked", own.Title)
	}
	if ax := readChart(t, chartXML("", "", `<c:barChart>`+ser(0, "A", "", "1")+`<c:axId val="1"/><c:axId val="2"/></c:barChart>`+axes), false).Axes[1]; ax.Title == nil || ax.Title.Auto {
		t.Errorf("an axis title the file states = %+v", ax.Title)
	}
	if ax := readChart(t, chartXML("", "", `<c:barChart>`+ser(0, "A", "", "1")+`<c:axId val="1"/><c:axId val="2"/></c:barChart>`+strings.Replace(axes, `<c:tx><c:rich><a:bodyPr rot="-5400000"/><a:p><a:r><a:rPr sz="1200" b="0"/><a:t>Amount</a:t></a:r></a:p></c:rich></c:tx>`, "", 1)), false).Axes[1]; ax.Title == nil || ax.Title.Text != "Axis Title" || !ax.Title.Auto {
		t.Errorf("a placeholder axis title = %+v", ax.Title)
	}
	stock := readChart(t, chartXML("", "", `<c:stockChart>`+ser(0, "High", "", "1")+`</c:stockChart>`), false)
	if g := stock.Groups[0]; g.Kind != chart.Line || !g.Stock {
		t.Errorf("stock group = %+v", g)
	}
	if surface := readChart(t, chartXML("", "", `<c:surfaceChart>`+ser(0, "Z", "", "1")+`</c:surfaceChart>`), false); !surface.Surface || len(surface.Groups) != 1 || surface.Groups[0].Kind != chart.Surface {
		t.Errorf("surface chart: Surface %v, %d groups", surface.Surface, len(surface.Groups))
	}
}

// TestChartFromElement_Surface reads a surface group: its series in rows,
// its wireframe, its band formats by index, its series axis, and the view
// of a contour, seen from straight above when the file states none.
func TestChartFromElement_Surface(t *testing.T) {
	bands := `<c:bandFmts><c:bandFmt><c:idx val="1"/><c:spPr><a:solidFill><a:srgbClr val="00FF00"/></a:solidFill></c:spPr></c:bandFmt></c:bandFmts>`
	axes := `<c:axId val="1"/><c:axId val="2"/><c:axId val="3"/>`
	defs := `<c:catAx><c:axId val="1"/></c:catAx><c:valAx><c:axId val="2"/></c:valAx><c:serAx><c:axId val="3"/></c:serAx>`
	contour := readChart(t, chartXML("", "", `<c:surfaceChart><c:wireframe val="1"/>`+ser(0, "A", "", "1", "2")+ser(1, "B", "", "3", "4")+bands+axes+`</c:surfaceChart>`+defs), false)
	g := contour.Groups[0]
	if g.Kind != chart.Surface || g.ThreeD || !g.Rows || !g.Wireframe || g.ZAxis != 2 || len(g.Series) != 2 {
		t.Errorf("contour group = %+v", g)
	}
	if len(g.Bands) != 2 || g.Bands[0].Fill != nil || g.Bands[1].Fill == nil || g.Bands[1].Fill.Color != (typeset.Color{G: 1, A: 1}) {
		t.Errorf("bands = %+v, want band 1 green alone", g.Bands)
	}
	if v := contour.View3D; v == nil || v.RotX != 90 || v.RotY != 0 || v.Perspective != 0 {
		t.Errorf("contour view = %+v, want straight above", v)
	}
	solid := readChart(t, chartXML("", "", `<c:surface3DChart>`+ser(0, "A", "", "1", "2")+axes+`</c:surface3DChart>`+defs), false)
	if g := solid.Groups[0]; g.Kind != chart.Surface || !g.ThreeD || g.Wireframe {
		t.Errorf("3-D surface group = %+v", g)
	}
	if v := solid.View3D; v == nil || v.RotX != 15 || v.RotY != 20 {
		t.Errorf("3-D surface view = %+v, want Office's default", v)
	}
}

func TestChartFromElement_AxisLabelBreaks(t *testing.T) {
	tests := []struct {
		name             string
		axis             string
		noBreak, overlap bool
	}{
		{name: "automatic", axis: `<c:catAx><c:axId val="1"/><c:txPr><a:bodyPr rot="-60000000"/></c:txPr></c:catAx>`},
		{name: "upright", axis: `<c:catAx><c:axId val="1"/><c:txPr><a:bodyPr rot="0"/></c:txPr></c:catAx>`, overlap: true},
		{name: "turned", axis: `<c:catAx><c:axId val="1"/><c:txPr><a:bodyPr rot="-2700000"/></c:txPr></c:catAx>`, noBreak: true},
		{name: "quarter turn", axis: `<c:catAx><c:axId val="1"/><c:txPr><a:bodyPr rot="-5400000"/></c:txPr></c:catAx>`},
		{name: "date", axis: `<c:dateAx><c:axId val="1"/></c:dateAx>`, noBreak: true},
		{name: "quarter turn beside a placed plot area", axis: `<c:layout><c:manualLayout><c:layoutTarget val="inner"/><c:xMode val="edge"/><c:yMode val="edge"/><c:x val="0.1"/><c:y val="0.1"/><c:w val="0.5"/><c:h val="0.5"/></c:manualLayout></c:layout><c:catAx><c:axId val="1"/><c:txPr><a:bodyPr rot="-5400000"/></c:txPr></c:catAx>`, noBreak: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := readChart(t, chartXML("", "", tt.axis), false)
			if len(c.Axes) != 1 || c.Axes[0].NoBreak != tt.noBreak || c.Axes[0].Overlap != tt.overlap {
				t.Errorf("axes = %+v", c.Axes)
			}
		})
	}
}

func TestChartFromElement_GradientAndPatternFills(t *testing.T) {
	spPr := `<c:spPr><a:gradFill><a:gsLst><a:gs pos="0"><a:srgbClr val="FF0000"/></a:gs><a:gs pos="100000"><a:srgbClr val="0000FF"/></a:gs></a:gsLst><a:lin ang="5400000"/></a:gradFill></c:spPr>`
	plot := `<c:barChart><c:barDir val="col"/>` + ser(0, "A", spPr, "1") + `</c:barChart><c:spPr><a:pattFill prst="dkDnDiag"><a:fgClr><a:srgbClr val="00FF00"/></a:fgClr></a:pattFill></c:spPr>`
	c := readChart(t, chartXML("", "", plot), false)
	f := c.Groups[0].Series[0].Fill
	if f == nil || f.Shape == nil || f.Shape.Kind != shape.PaintGradient || len(f.Shape.Stops) < 3 || f.Shape.Angle != 90 || f.Color.B != 1 {
		t.Errorf("series fill = %+v", f)
	}
	pf := c.PlotArea.Fill
	if pf == nil || pf.Shape == nil || pf.Shape.Pattern != "dkDnDiag" || pf.Shape.Color.G != 1 || pf.Shape.Background.R != 1 {
		t.Errorf("plot area fill = %+v", pf)
	}
}

func TestChartFromElement_CategoryPositions(t *testing.T) {
	cat := `<c:catAx><c:axId val="1"/><c:crossAx val="2"/></c:catAx>`
	val := func(cb string) string {
		return `<c:valAx><c:axId val="2"/><c:crossAx val="1"/>` + cb + `</c:valAx>`
	}
	ids := `<c:axId val="1"/><c:axId val="2"/>`
	tests := []struct {
		name, plot string
		between    bool
	}{
		{"bar by default", `<c:barChart>` + ser(0, "A", "", "1") + ids + `</c:barChart>` + cat + val(""), true},
		{"area by default", `<c:areaChart>` + ser(0, "A", "", "1") + ids + `</c:areaChart>` + cat + val(""), false},
		{"value axis says between", `<c:areaChart>` + ser(0, "A", "", "1") + ids + `</c:areaChart>` + cat + val(`<c:crossBetween val="between"/>`), true},
		{"value axis says on ticks", `<c:barChart>` + ser(0, "A", "", "1") + ids + `</c:barChart>` + cat + val(`<c:crossBetween val="midCat"/>`), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := readChart(t, chartXML("", "", tt.plot), false)
			if c.Axes[0].BetweenCategories != tt.between {
				t.Errorf("between = %v, want %v", c.Axes[0].BetweenCategories, tt.between)
			}
		})
	}
}

func TestIsDateCode(t *testing.T) {
	for code, want := range map[string]bool{
		"m/d/yyyy": true, "General": false, "0.00": false, `"day" 0`: false, "[Red]0": false,
		`0\d`: false, "hh:mm": true, "0.00E+00": false, "[$-409]mmm-yy": true, "": false,
	} {
		if got := isDateCode(code); got != want {
			t.Errorf("isDateCode(%q) = %v, want %v", code, got, want)
		}
	}
}

func TestChartFromElement_Date1904(t *testing.T) {
	plot := `<c:lineChart><c:ser><c:idx val="0"/><c:order val="0"/><c:cat><c:numRef><c:f>S!$A$1:$A$1</c:f><c:numCache><c:formatCode>m/d/yyyy</c:formatCode><c:ptCount val="1"/><c:pt idx="0"><c:v>100</c:v></c:pt></c:numCache></c:numRef></c:cat>` +
		`<c:val><c:numRef><c:f>S!$B$1:$B$1</c:f><c:numCache><c:formatCode>General</c:formatCode><c:ptCount val="1"/><c:pt idx="0"><c:v>100</c:v></c:pt></c:numCache></c:numRef></c:val></c:ser></c:lineChart>`
	c := readChart(t, chartXML(`<c:date1904 val="1"/>`, "", plot), false)
	s := c.Groups[0].Series[0]
	if s.Categories.Values[0].Number != 1561 || s.Values.Values[0].Number != 100 {
		t.Errorf("categories %v, values %v", s.Categories.Values, s.Values.Values)
	}
}

func TestManualLayout(t *testing.T) {
	ml := func(kids string) *Element {
		root, err := Parse(context.Background(), []byte(`<c:plotArea xmlns:c="`+NSChart+`"><c:layout><c:manualLayout>`+kids+`</c:manualLayout></c:layout></c:plotArea>`), nil)
		if err != nil {
			t.Fatal(err)
		}
		return root
	}
	tests := []struct {
		name string
		kids string
		want *chart.ManualLayout
	}{
		{"edge corner, factor size", `<c:layoutTarget val="inner"/><c:xMode val="edge"/><c:yMode val="edge"/><c:x val="0.1"/><c:y val="0.2"/><c:w val="0.5"/><c:h val="0.6"/>`, &chart.ManualLayout{X: 0.1, Y: 0.2, Width: 0.5, Height: 0.6, Inner: true}},
		{"edge size", `<c:xMode val="edge"/><c:yMode val="edge"/><c:wMode val="edge"/><c:hMode val="edge"/><c:x val="0.1"/><c:y val="0.2"/><c:w val="0.5"/><c:h val="0.6"/>`, &chart.ManualLayout{X: 0.1, Y: 0.2, Width: 0.4, Height: 0.4}},
		{"corner only", `<c:xMode val="edge"/><c:yMode val="edge"/><c:x val="0.7"/><c:y val="0.1"/>`, &chart.ManualLayout{X: 0.7, Y: 0.1}},
		{"factor corner", `<c:x val="0.1"/><c:y val="0.2"/>`, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := manualLayout(ml(tt.kids))
			if (got == nil) != (tt.want == nil) || got != nil && math.Abs(got.X-tt.want.X)+math.Abs(got.Y-tt.want.Y)+math.Abs(got.Width-tt.want.Width)+math.Abs(got.Height-tt.want.Height) > 1e-9 || got != nil && got.Inner != tt.want.Inner {
				t.Errorf("manualLayout = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestChartFromElement_DataTable(t *testing.T) {
	plot := `<c:barChart><c:barDir val="col"/>` + ser(0, "A", "", "1") + `</c:barChart><c:dTable><c:showHorzBorder val="1"/><c:showKeys val="1"/><c:spPr><a:solidFill><a:srgbClr val="D9D9D9"/></a:solidFill><a:ln w="12700"><a:solidFill><a:srgbClr val="0000FF"/></a:solidFill></a:ln></c:spPr><c:txPr><a:p><a:pPr><a:defRPr sz="900"/></a:pPr></a:p></c:txPr></c:dTable>`
	dt := readChart(t, chartXML("", "", plot), false).DataTable
	if dt == nil || !dt.HBorder || dt.VBorder || !dt.Keys || dt.Style.Size != 9 || dt.Fill == nil || dt.Line == nil || dt.Line.Width != 1 {
		t.Errorf("data table = %+v", dt)
	}
}

func TestChartFromElement_CustomLabels(t *testing.T) {
	dl := `<c:dLbls><c:dLbl><c:idx val="0"/><c:tx><c:rich><a:p><a:r><a:t>Share </a:t></a:r><a:fld id="{1}" type="PERCENTAGE"><a:t>[PERCENTAGE]</a:t></a:fld></a:p><a:p><a:fld id="{2}" type="CELLREF"><a:t>[CELL]</a:t></a:fld></a:p></c:rich></c:tx><c:showVal val="1"/>` +
		`<c:extLst><c:ext uri="{CE6537A1-D6FC-4f65-9D91-7224C49458BB}" xmlns:c15="http://schemas.microsoft.com/office/drawing/2012/chart"><c15:dlblFieldTable><c15:dlblFTEntry><c15:txfldGUID>{2}</c15:txfldGUID><c15:f>S!$B$3</c15:f><c15:dlblFieldTableCache><c:ptCount val="1"/><c:pt idx="0"><c:v>cell text</c:v></c:pt></c15:dlblFieldTableCache></c15:dlblFTEntry></c15:dlblFieldTable></c:ext></c:extLst></c:dLbl><c:showVal val="1"/></c:dLbls>`
	s := readChart(t, chartXML("", "", `<c:pieChart>`+ser(0, "A", dl, "1", "3")+`</c:pieChart>`), false).Groups[0].Series[0]
	if len(s.Points) != 1 {
		t.Fatalf("points = %+v", s.Points)
	}
	want := []chart.LabelField{{Text: "Share "}, {Kind: chart.FieldPercent, Text: "[PERCENTAGE]"}, {Kind: chart.FieldBreak}, {Text: "cell text"}}
	got := s.Points[0].Labels.Custom
	if len(got) != len(want) {
		t.Fatalf("custom = %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("custom[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestChartFromElement_ChartTypeWithoutSeries(t *testing.T) {
	c := readChart(t, chartXML("", `<c:title/>`, `<c:barChart><c:barDir val="col"/><c:axId val="1"/><c:axId val="2"/></c:barChart>`+axes), false)
	if len(c.Groups) != 0 || hasWarning(c, warn.ContentUnsupported) {
		t.Errorf("groups %+v, warnings %+v: want no group and no warning", c.Groups, c.Warnings)
	}
	if c.Title != nil {
		t.Errorf("title = %+v, want none: Office titles no chart of no series", c.Title)
	}
}

func TestParseChart_ThemeOverride(t *testing.T) {
	files := minimalPackage()
	files["word/_rels/document.xml.rels"] = `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
		`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/theme" Target="theme/theme1.xml"/></Relationships>`
	files["word/theme/theme1.xml"] = `<a:theme xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><a:themeElements><a:clrScheme name="x">` +
		`<a:dk1><a:srgbClr val="000000"/></a:dk1><a:lt1><a:srgbClr val="FFFFFF"/></a:lt1><a:accent1><a:srgbClr val="4F81BD"/></a:accent1><a:accent2><a:srgbClr val="C0504D"/></a:accent2></a:clrScheme>` +
		`<a:fontScheme name="f"><a:majorFont><a:latin typeface="Major"/></a:majorFont><a:minorFont><a:latin typeface="Minor"/></a:minorFont></a:fontScheme></a:themeElements></a:theme>`
	files["word/charts/chart1.xml"] = chartXML("", "", `<c:barChart>`+ser(0, "A", "", "1")+`</c:barChart>`)
	files["word/charts/_rels/chart1.xml.rels"] = `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
		`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/themeOverride" Target="../theme/themeOverride1.xml"/></Relationships>`
	files["word/theme/themeOverride1.xml"] = `<a:themeOverride xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><a:clrScheme name="o"><a:accent1><a:srgbClr val="5B9BD5"/></a:accent1></a:clrScheme></a:themeOverride>`
	pkg, err := Open(t.Context(), buildZip(t, files))
	if err != nil {
		t.Fatal(err)
	}
	c, err := ParseChart(context.Background(), pkg, "word/charts/chart1.xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Palette[0]; math.Abs(got.R-0x5b/255.0) > 1e-9 {
		t.Errorf("first automatic colour = %v, want the override's accent1", got)
	}
	if got := c.Palette[1]; math.Abs(got.R-0xc0/255.0) > 1e-9 {
		t.Errorf("second automatic colour = %v, want the document theme's accent2", got)
	}
	if c.Title != nil && c.Title.Style.Families[0] != "Minor" {
		t.Errorf("title font %v, want the document theme's minor font", c.Title.Style.Families)
	}
}

func TestChartFromElement_SeriesColourByIdx(t *testing.T) {
	a := strings.Replace(ser(0, "A", "", "1"), `<c:idx val="0"/>`, `<c:idx val="2"/>`, 1)
	b := strings.Replace(strings.Replace(ser(1, "B", "", "2"), `<c:idx val="1"/>`, `<c:idx val="0"/>`, 1), `<c:order val="1"/>`, `<c:order val="5"/>`, 1)
	c := readChart(t, chartXML("", "", `<c:barChart>`+b+a+`</c:barChart>`), false)
	ss := c.Groups[0].Series
	if ss[0].Name != "A" || ss[0].Index != 2 || ss[1].Name != "B" || ss[1].Index != 0 {
		t.Errorf("series %q index %d, %q index %d: want A (idx 2) drawn before B (idx 0)", ss[0].Name, ss[0].Index, ss[1].Name, ss[1].Index)
	}
}

func TestChartFromElement_Trendlines(t *testing.T) {
	tl := `<c:trendline><c:name>Fit</c:name><c:spPr><a:ln w="19050"><a:solidFill><a:srgbClr val="FF0000"/></a:solidFill></a:ln></c:spPr><c:trendlineType val="poly"/><c:order val="3"/><c:forward val="1.5"/><c:intercept val="2"/><c:dispEq val="1"/></c:trendline>` +
		`<c:trendline><c:trendlineType val="movingAvg"/><c:period val="4"/></c:trendline><c:trendline><c:trendlineType val="bogus"/></c:trendline>`
	c := readChart(t, chartXML("", "", `<c:lineChart>`+ser(0, "A", tl, "1", "2")+`</c:lineChart>`), false)
	got := c.Groups[0].Series[0].Trendlines
	if len(got) != 2 {
		t.Fatalf("trendlines = %+v, want two", got)
	}
	if a := got[0]; a.Kind != chart.TrendPolynomial || a.Order != 3 || a.Forward != 1.5 || !a.HasIntercept || a.Intercept != 2 || a.Name != "Fit" || a.Line == nil || a.Line.Width != 1.5 {
		t.Errorf("first trendline = %+v", a)
	}
	if b := got[1]; b.Kind != chart.TrendMovingAverage || b.Period != 4 || b.Order != 2 || b.Line != nil {
		t.Errorf("second trendline = %+v", b)
	}
	if !hasWarning(c, warn.ContentUnsupported) {
		t.Error("an equation shown is not reported")
	}
}

func TestChartFromElement_ErrorBars(t *testing.T) {
	eb := `<c:errBars><c:errDir val="y"/><c:errBarType val="plus"/><c:errValType val="percentage"/><c:noEndCap val="1"/><c:val val="5"/></c:errBars>` +
		`<c:errBars><c:errDir val="x"/><c:errBarType val="both"/><c:errValType val="cust"/><c:plus><c:numLit><c:ptCount val="1"/><c:pt idx="0"><c:v>2</c:v></c:pt></c:numLit></c:plus></c:errBars>`
	c := readChart(t, chartXML("", "", `<c:barChart>`+ser(0, "A", eb, "1")+`</c:barChart>`), false)
	got := c.Groups[0].Series[0].ErrorBars
	if len(got) != 2 {
		t.Fatalf("error bars = %+v, want two", got)
	}
	if a := got[0]; a.Kind != chart.ErrorPercent || a.Plus != 5 || !a.Up || a.Down || !a.NoCap || a.X || a.Line == nil {
		t.Errorf("first = %+v", a)
	}
	if b := got[1]; b.Kind != chart.ErrorCustom || b.X || !b.Up || !b.Down || b.PlusData.Len() != 1 {
		t.Errorf("second = %+v, want a bar chart's custom bars along its values", b)
	}
}

// TestChartFromElement_AxisSourceAndUnits locks two value axis readings:
// a numFmt linked to its source leaves the format to the source data,
// whatever its formatCode (ECMA-376 Part 1, 21.2.2.121), and a millions
// display unit with an empty label divides the labels by a million and
// titles the axis "Millions" (21.2.2.47); see [chartReader.axis] and
// [chartReader.displayUnits].
func TestChartFromElement_AxisSourceAndUnits(t *testing.T) {
	ax := `<c:catAx><c:axId val="1"/><c:scaling/><c:axPos val="b"/><c:crossAx val="2"/></c:catAx>` +
		`<c:valAx><c:axId val="2"/><c:scaling/><c:axPos val="l"/><c:numFmt formatCode="#,##0" sourceLinked="1"/><c:crossAx val="1"/>` +
		`<c:dispUnits><c:builtInUnit val="millions"/><c:dispUnitsLbl/></c:dispUnits></c:valAx>`
	c := readChart(t, chartXML("", "", `<c:barChart><c:barDir val="col"/>`+ser(0, "A", "", "1")+`<c:axId val="1"/><c:axId val="2"/></c:barChart>`+ax), false)
	val := c.Axes[1]
	if !val.SourceLinked || val.Format != "" {
		t.Errorf("source-linked axis: SourceLinked %v, Format %q", val.SourceLinked, val.Format)
	}
	if val.DisplayUnit != 1e6 || val.Title == nil || val.Title.Text != "Millions" {
		t.Errorf("display unit %v, title %+v; want 1e6 and Millions", val.DisplayUnit, val.Title)
	}
}

func TestChartFromElement_UprightAxisTitle(t *testing.T) {
	tests := []struct {
		name    string
		bodyPr  string
		upright bool
	}{
		{"no rotation stated", `<a:bodyPr/>`, false},
		{"zero rotation", `<a:bodyPr rot="0"/>`, true},
		{"quarter turn", `<a:bodyPr rot="-5400000"/>`, false},
		{"automatic", `<a:bodyPr rot="-60000000"/>`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			axis := `<c:valAx><c:axId val="2"/><c:axPos val="l"/><c:title><c:tx><c:rich>` + tt.bodyPr +
				`<a:p><a:r><a:t>y</a:t></a:r></a:p></c:rich></c:tx></c:title></c:valAx>`
			c := readChart(t, chartXML("", "", axis), false)
			if len(c.Axes) != 1 || c.Axes[0].Title == nil {
				t.Fatalf("axes = %+v", c.Axes)
			}
			if got := c.Axes[0].Title.Upright; got != tt.upright {
				t.Errorf("Upright = %v, want %v", got, tt.upright)
			}
		})
	}
}

// TestChartFromElement_DateAxisTimeUnits reads a date axis's base and
// major time units, an absent one being automatic.
func TestChartFromElement_DateAxisTimeUnits(t *testing.T) {
	date := func(kids string) *chart.Axis {
		plot := `<c:lineChart><c:axId val="1"/><c:axId val="2"/></c:lineChart>` +
			`<c:dateAx><c:axId val="1"/><c:scaling><c:orientation val="minMax"/></c:scaling><c:delete val="0"/><c:axPos val="b"/><c:crossAx val="2"/>` + kids + `</c:dateAx>` +
			`<c:valAx><c:axId val="2"/><c:scaling><c:orientation val="minMax"/></c:scaling><c:delete val="0"/><c:axPos val="l"/><c:crossAx val="1"/></c:valAx>`
		c := readChart(t, chartXML("", "", plot), false)
		for _, a := range c.Axes {
			if a.Kind == chart.DateAxis {
				return a
			}
		}
		t.Fatal("no date axis")
		return nil
	}
	if a := date(`<c:baseTimeUnit val="days"/><c:majorUnit val="2"/><c:majorTimeUnit val="months"/>`); a.BaseTimeUnit != chart.TimeDays || a.MajorTimeUnit != chart.TimeMonths || a.MajorUnit != 2 {
		t.Errorf("base %v, major %v %v, want days, 2 months", a.BaseTimeUnit, a.MajorUnit, a.MajorTimeUnit)
	}
	if a := date(``); a.BaseTimeUnit != chart.TimeAuto || a.MajorTimeUnit != chart.TimeAuto {
		t.Errorf("base %v, major %v, want automatic", a.BaseTimeUnit, a.MajorTimeUnit)
	}
}

// bubbleXML is a bubble chart sized by width whose labels show the
// bubbles' sizes.
var bubbleXML = chartXML("", "", `<c:bubbleChart><c:ser><c:idx val="0"/><c:order val="0"/>`+
	`<c:dLbls><c:showVal val="0"/><c:showBubbleSize val="1"/></c:dLbls>`+
	`<c:xVal><c:numRef><c:f>Sheet1!$A$2:$A$3</c:f><c:numCache><c:ptCount val="2"/><c:pt idx="0"><c:v>1</c:v></c:pt><c:pt idx="1"><c:v>2</c:v></c:pt></c:numCache></c:numRef></c:xVal>`+
	`<c:yVal><c:numRef><c:f>Sheet1!$B$2:$B$3</c:f><c:numCache><c:ptCount val="2"/><c:pt idx="0"><c:v>3</c:v></c:pt><c:pt idx="1"><c:v>4</c:v></c:pt></c:numCache></c:numRef></c:yVal>`+
	`<c:bubbleSize><c:numRef><c:f>Sheet1!$C$2:$C$3</c:f><c:numCache><c:ptCount val="2"/><c:pt idx="0"><c:v>1e308</c:v></c:pt><c:pt idx="1"><c:v>-5</c:v></c:pt></c:numCache></c:numRef></c:bubbleSize>`+
	`</c:ser><c:bubbleScale val="300"/><c:sizeRepresents val="w"/><c:axId val="1"/><c:axId val="2"/></c:bubbleChart>`+
	`<c:valAx><c:axId val="1"/><c:scaling><c:orientation val="minMax"/></c:scaling><c:delete val="0"/><c:axPos val="b"/><c:crossAx val="2"/></c:valAx>`+
	`<c:valAx><c:axId val="2"/><c:scaling><c:orientation val="minMax"/></c:scaling><c:delete val="1"/><c:axPos val="l"/><c:crossAx val="1"/></c:valAx>`)

func TestChartFromElement_BubbleWidthAndSizeLabels(t *testing.T) {
	c := readChart(t, bubbleXML, false)
	if len(c.Groups) != 1 || !c.Groups[0].BubbleWidth {
		t.Fatalf("groups %+v, want one bubble group sized by width", c.Groups)
	}
	if s := c.Groups[0].Series; len(s) != 1 || !s[0].Labels.ShowSize || s[0].Labels.ShowValue {
		t.Errorf("labels %+v, want the sizes alone", c.Groups[0].Series[0].Labels)
	}
}

func TestChartFromElement_InvertIfNegative(t *testing.T) {
	inv := `<c:invertIfNegative val="1"/>`
	ext := inv + `<c:extLst><c:ext uri="{6F2FDCE9-48DA-4B69-8628-5D25D57E0C4C}" xmlns:c14="http://schemas.microsoft.com/office/drawing/2007/8/2/chart">` +
		`<c14:invertSolidFillFmt><c14:spPr><a:solidFill><a:srgbClr val="FF0000"/></a:solidFill></c14:spPr></c14:invertSolidFillFmt></c:ext></c:extLst>`
	for _, tc := range []struct {
		extra string
		want  *typeset.Color
	}{
		{"", &typeset.Color{R: 1, G: 1, B: 1, A: 1}},
		{`<c:invertIfNegative val="0"/>`, nil},
		{inv, &typeset.Color{R: 1, G: 1, B: 1, A: 1}},
		{ext, &typeset.Color{R: 1, A: 1}},
	} {
		c := readChart(t, chartXML("", "", `<c:barChart><c:barDir val="col"/>`+ser(0, "A", tc.extra, "1", "-2")+`<c:axId val="1"/><c:axId val="2"/></c:barChart>`+axes), false)
		got := c.Groups[0].Series[0].Invert
		switch {
		case tc.want == nil && got != nil, tc.want != nil && got == nil:
			t.Errorf("%q: invert %+v, want %v", tc.extra, got, tc.want)
		case tc.want != nil && got.Color != *tc.want:
			t.Errorf("%q: invert %+v, want %v", tc.extra, got.Color, *tc.want)
		}
	}
}

// TestChartFromElement_StockLines reads a stock chart's high-low lines and
// up-down bars (ECMA-376 Part 1, 21.2.2.80 hiLowLines, 21.2.2.218
// upDownBars) with Office's automatic formats, and warns of neither.
func TestChartFromElement_StockLines(t *testing.T) {
	two := ser(0, "Open", "", "1", "2") + ser(1, "Close", "", "3", "1")
	c := readChart(t, chartXML("", "", `<c:stockChart>`+two+
		`<c:hiLowLines><c:spPr><a:ln w="9525"><a:solidFill><a:srgbClr val="404040"/></a:solidFill></a:ln></c:spPr></c:hiLowLines>`+
		`<c:upDownBars><c:gapWidth val="80"/><c:upBars><c:spPr><a:solidFill><a:srgbClr val="70AD47"/></a:solidFill><a:ln><a:solidFill><a:srgbClr val="FF0000"/></a:solidFill></a:ln></c:spPr></c:upBars><c:downBars/></c:upDownBars>`+
		`</c:stockChart>`), false)
	g := c.Groups[0]
	if g.HighLow == nil || g.HighLow.Width != 0.75 || g.HighLow.None {
		t.Errorf("high-low lines = %+v", g.HighLow)
	}
	ud := g.UpDown
	if ud == nil || ud.GapWidth != 80 {
		t.Fatalf("up-down bars = %+v", ud)
	}
	if ud.Up.Fill == nil || ud.Up.Border == nil || ud.Up.Border.Width != upDownOutline || ud.Up.Border.Color.R != 1 {
		t.Errorf("up bars = %+v %+v, want the file's fill and red outline %v wide", ud.Up.Fill, ud.Up.Border, upDownOutline)
	}
	if ud.Down.Fill == nil || ud.Down.Fill.Color.R != 0 || ud.Down.Border == nil || ud.Down.Border.Width != upDownOutline {
		t.Errorf("down bars = %+v %+v, want black outlined in black", ud.Down.Fill, ud.Down.Border)
	}
	for _, w := range c.Warnings {
		t.Errorf("warning %q", w.Message)
	}
	if plain := readChart(t, chartXML("", "", `<c:lineChart>`+two+`</c:lineChart>`), false).Groups[0]; plain.HighLow != nil || plain.UpDown != nil {
		t.Errorf("a line chart without them = %+v %+v", plain.HighLow, plain.UpDown)
	}
}

// TestChartFromElement_OfPie reads a bar of pie's split and second plot
// (ECMA-376 Part 1, 21.2.2.126 ofPieChart), an automatic split as the
// last officeOfPieAuto points, and warns of nothing.
func TestChartFromElement_OfPie(t *testing.T) {
	one := ser(0, "A", "", "9", "8", "7", "6")
	c := readChart(t, chartXML("", "", `<c:ofPieChart><c:ofPieType val="bar"/>`+one+
		`<c:splitType val="cust"/><c:custSplit><c:secondPiePt val="1"/><c:secondPiePt val="3"/></c:custSplit><c:secondPieSize val="300"/><c:serLines/></c:ofPieChart>`), false)
	o := c.Groups[0].OfPie
	if o == nil || !o.Bar || o.Split != chart.SplitCustom || len(o.Custom) != 2 || o.Custom[1] != 3 || o.SecondSize != 200 || o.Lines == nil {
		t.Fatalf("of pie = %+v", o)
	}
	for _, w := range c.Warnings {
		t.Errorf("warning %q", w.Message)
	}
	auto := readChart(t, chartXML("", "", `<c:ofPieChart><c:ofPieType val="pie"/>`+one+`<c:splitPos val="1"/></c:ofPieChart>`), false).Groups[0].OfPie
	if auto == nil || auto.Bar || auto.Split != chart.SplitPosition || auto.SplitAt != officeOfPieAuto || auto.SecondSize != 75 || auto.Lines != nil {
		t.Errorf("automatic of pie = %+v", auto)
	}
	if pos := readChart(t, chartXML("", "", `<c:ofPieChart>`+one+`<c:splitType val="pos"/><c:splitPos val="2"/></c:ofPieChart>`), false).Groups[0].OfPie; pos.SplitAt != 2 {
		t.Errorf("split by position = %+v", pos)
	}
}

// TestChartFromElement_SeriesDeletesLabels locks that a series whose
// c:dLbls deletes its labels shows none under a group that shows values,
// while a series without its own c:dLbls shows the group's.
func TestChartFromElement_SeriesDeletesLabels(t *testing.T) {
	plot := `<c:barChart><c:barDir val="col"/>` + ser(0, "A", `<c:dLbls><c:delete val="1"/></c:dLbls>`, "1", "2") + ser(1, "B", "", "3", "4") +
		`<c:dLbls><c:showVal val="1"/></c:dLbls><c:axId val="1"/><c:axId val="2"/></c:barChart>` + axes
	g := readChart(t, chartXML("", "", plot), false).Groups[0]
	if l := g.Series[0].Labels; !l.Deleted || l.Any() {
		t.Errorf("series A labels = %+v, want deleted", l)
	}
	if l := g.Series[1].Labels; l.Deleted {
		t.Errorf("series B labels = %+v, want the group's", l)
	}
}

// TestChartFromElement_PictureFill locks that a picture fill paints the
// picture its relationship loads, tiled at its size by the tile's scale
// or stretched without a tile, and fills nothing when no picture loads.
func TestChartFromElement_PictureFill(t *testing.T) {
	src := &typeset.ImageSource{}
	pictures := func(id string) *layout.Picture {
		if id != "rId3" {
			return nil
		}
		return &layout.Picture{Width: 96, Height: 48, Source: src}
	}
	area := func(blip string) string {
		return `<c:chartSpace xmlns:c="http://schemas.openxmlformats.org/drawingml/2006/chart" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">` +
			`<c:chart><c:plotArea/></c:chart><c:spPr><a:blipFill>` + blip + `</a:blipFill></c:spPr></c:chartSpace>`
	}
	read := func(xml string, pics func(string) *layout.Picture) *chart.Paint {
		root, err := Parse(context.Background(), []byte(xml), nil)
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		c, err := ChartFromElement(root, nil, ChartOptions{Pictures: pics})
		if err != nil {
			t.Fatalf("ChartFromElement: %v", err)
		}
		return c.Area.Fill
	}
	tiled := area(`<a:blip r:embed="rId3"/><a:tile sx="50000" sy="200000"/>`)
	if f := read(tiled, pictures); f == nil || f.Shape == nil || f.Shape.Kind != shape.PaintPicture || f.Shape.Picture != src || f.Shape.TileWidth != 48 || f.Shape.TileHeight != 96 {
		t.Errorf("tiled picture fill = %+v, want the picture in tiles 48 by 96", f)
	}
	if f := read(area(`<a:blip r:embed="rId3"/><a:stretch/>`), pictures); f == nil || f.Shape == nil || f.Shape.TileWidth != 0 {
		t.Errorf("stretched picture fill = %+v, want the picture untiled", f)
	}
	for _, pics := range []func(string) *layout.Picture{nil, func(string) *layout.Picture { return nil }} {
		if f := read(tiled, pics); f != nil {
			t.Errorf("picture fill without a picture = %+v, want none", f)
		}
	}
}
