package ooxml

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/chart"
	"github.com/gotenberg/litho/pkg/internal/shape"
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
		{"bar of pie", `<c:ofPieChart><c:ofPieType val="bar"/>` + ser(0, "A", "", "1") + `</c:ofPieChart>`, func(t *testing.T, g *chart.Group) {
			if g.Kind != chart.Pie {
				t.Errorf("group = %+v", g)
			}
		}},
		{"scatter", `<c:scatterChart><c:scatterStyle val="marker"/><c:ser><c:idx val="0"/><c:order val="0"/><c:xVal><c:numLit><c:ptCount val="2"/><c:pt idx="0"><c:v>1.5</c:v></c:pt><c:pt idx="1"><c:v>2.5</c:v></c:pt></c:numLit></c:xVal><c:yVal><c:numRef><c:f>Sheet1!$B$2:$B$3</c:f></c:numRef></c:yVal></c:ser>` + ids + `</c:scatterChart>` + axes, func(t *testing.T, g *chart.Group) {
			s := g.Series[0]
			if g.Kind != chart.Scatter || s.Categories.Len() != 2 || s.Categories.Values[1].Number != 2.5 || s.Values.Cached || s.Values.Ref != "Sheet1!$B$2:$B$3" || s.Border == nil || !s.Border.None || s.Name != "Column B" {
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

func TestChartFromElement_Warnings(t *testing.T) {
	plot := `<c:line3DChart>` + ser(0, "A", `<c:trendline/><c:errBars/>`, "1") + `</c:line3DChart><c:surfaceChart/>`
	c := readChart(t, chartXML("", "", plot), false)
	var msgs []string
	for _, w := range c.Warnings {
		msgs = append(msgs, w.Message)
	}
	got := strings.Join(msgs, "|")
	for _, want := range []string{"3D line", "trendlines", "error bars", "surface"} {
		if !strings.Contains(got, want) {
			t.Errorf("warnings %q lack %q", got, want)
		}
	}
	if _, err := ChartFromElement(&Element{Space: NSChart, Local: "chart"}, nil, ChartOptions{}); err == nil {
		t.Error("read a part that is not a chart space")
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
	pkg, err := Open(buildZip(t, files))
	if err != nil {
		t.Fatal(err)
	}
	c, err := ParseChart(context.Background(), pkg, "word/charts/chart1.xml")
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
	if _, err := ParseChart(context.Background(), pkg, "word/charts/missing.xml"); !IsMissing(err) {
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

func TestRangeName(t *testing.T) {
	for ref, want := range map[string]string{
		"Sheet1!$B$6:$B$16":      "Column B",
		"'My sheet'!$D$7:$I$7":   "Row 7",
		"Sheet1!$F$7":            "Column F",
		"Sheet1!$AA$2:$AB$9":     "Column AA",
		"":                       "",
		"Sheet1!$B$2:$B$x":       "",
		"[1]Sheet1!$C$1:$E$1":    "Row 1",
		"(Sheet1!$A$1,Sheet1!B)": "",
	} {
		if got := rangeName(ref); got != want {
			t.Errorf("rangeName(%q) = %q, want %q", ref, got, want)
		}
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
	if f == nil || f.Shape == nil || f.Shape.Kind != shape.PaintGradient || len(f.Shape.Stops) != 2 || f.Shape.Angle != 90 {
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
	if len(c.Groups) != 0 || hasWarning(c, CodeChartUnsupported) {
		t.Errorf("groups %+v, warnings %+v: want no group and no warning", c.Groups, c.Warnings)
	}
	if c.Title == nil || c.Title.Text != "Chart Title" {
		t.Errorf("title = %+v, want the automatic title", c.Title)
	}
}
