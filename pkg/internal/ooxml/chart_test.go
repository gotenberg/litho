package ooxml

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/chart"
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
	c, err := ChartFromElement(root, nil, mso2007)
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
	if cat.Kind != chart.CategoryAxis || !cat.Reverse || cat.MajorTicks != chart.TickOut || !cat.BetweenCategories || cat.Line == nil {
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
	if _, err := ChartFromElement(&Element{Space: NSChart, Local: "chart"}, nil, false); err == nil {
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
