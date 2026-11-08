package ooxml

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/chart"
)

// chartExXML wraps data and a chart's content in a chartex chart space.
func chartExXML(data, chartKids string) string {
	return `<cx:chartSpace xmlns:cx="http://schemas.microsoft.com/office/drawing/2014/chartex" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main">` +
		`<cx:chartData>` + data + `</cx:chartData><cx:chart>` + chartKids + `</cx:chart></cx:chartSpace>`
}

// cxData returns a cx:data of id with categories and cached values.
func cxData(id, f string, cats []string, vals ...string) string {
	var b strings.Builder
	b.WriteString(`<cx:data id="` + id + `">`)
	if cats != nil {
		b.WriteString(`<cx:strDim type="cat"><cx:f>Sheet1!$A$2:$A$9</cx:f><cx:lvl ptCount="` + string(rune('0'+len(cats))) + `">`)
		for i, c := range cats {
			b.WriteString(`<cx:pt idx="` + string(rune('0'+i)) + `">` + c + `</cx:pt>`)
		}
		b.WriteString(`</cx:lvl></cx:strDim>`)
	}
	b.WriteString(`<cx:numDim type="val"><cx:f>` + f + `</cx:f>`)
	if vals != nil {
		b.WriteString(`<cx:lvl ptCount="` + string(rune('0'+len(vals))) + `" formatCode="General">`)
		for i, v := range vals {
			b.WriteString(`<cx:pt idx="` + string(rune('0'+i)) + `">` + v + `</cx:pt>`)
		}
		b.WriteString(`</cx:lvl>`)
	}
	b.WriteString(`</cx:numDim></cx:data>`)
	return b.String()
}

const cxAxes = `<cx:axis id="0"><cx:catScaling gapWidth="0.5"/><cx:majorTickMarks type="in"/><cx:tickLabels/></cx:axis>` +
	`<cx:axis id="1"><cx:valScaling max="10" min="auto"/><cx:majorGridlines/><cx:tickLabels/><cx:numFmt formatCode="0.0" sourceLinked="0"/></cx:axis>`

// readChartEx parses xml and reads its chart, failing the test on an error.
func readChartEx(t *testing.T, xml string, names map[string]string) *chart.Chart {
	t.Helper()
	root, err := Parse(context.Background(), []byte(xml), nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	c, err := ChartExFromElement(root, nil, ChartOptions{}, names)
	if err != nil {
		t.Fatalf("ChartExFromElement: %v", err)
	}
	return c
}

func TestChartExFromElement_Unsupported(t *testing.T) {
	title := `<cx:title pos="t" align="ctr" overlay="0"><cx:txPr><a:bodyPr/><a:p><a:pPr><a:defRPr/></a:pPr><a:r><a:rPr sz="1400" b="0"/><a:t>Funnel!</a:t></a:r></a:p></cx:txPr></cx:title>`
	xml := chartExXML(cxData("0", "Sheet1!$B$2:$B$4", []string{"a", "b", "c"}, "4", "5", "7")+cxData("1", "Sheet1!$C$2:$C$4", nil, "1", "2", "3"),
		title+`<cx:plotArea><cx:plotAreaRegion>`+
			`<cx:series layoutId="funnel"><cx:tx><cx:txData><cx:f>Sheet1!$B$1</cx:f><cx:v>Stages</cx:v></cx:txData></cx:tx><cx:dataId val="0"/></cx:series>`+
			`<cx:series layoutId="funnel" hidden="1"><cx:dataId val="1"/></cx:series>`+
			`</cx:plotAreaRegion>`+cxAxes+`</cx:plotArea><cx:legend pos="t" align="ctr" overlay="0"/>`)
	c := readChartEx(t, xml, nil)
	if len(c.Groups) != 1 || c.Groups[0].Kind != chart.Unsupported || len(c.Groups[0].Series) != 2 {
		t.Fatalf("groups = %+v, want one unsupported group of two series", c.Groups)
	}
	g := c.Groups[0]
	if s := g.Series[0]; s.Name != "Stages" || s.Values.Len() != 3 || s.Categories.Len() != 3 || s.Categories.Values[1].Text != "b" {
		t.Errorf("first series = %+v", s)
	}
	if s := g.Series[1]; s.Name != "Column C" {
		t.Errorf("second series named %q, want its range's name", s.Name)
	}
	if c.Notice == nil || c.Notice.Text != NoticeUnsupported || c.Notice.Style.Families[0] != "Liberation Serif" || c.Notice.Style.Size != 10 {
		t.Errorf("notice = %+v", c.Notice)
	}
	if c.Title == nil || c.Title.Text != "Funnel!" || c.Title.Style.Size != 14 || c.Title.Style.Bold {
		t.Errorf("title = %+v, want the text of its txPr in 14 points", c.Title)
	}
	if c.Legend == nil || c.Legend.Position != chart.LegendTop {
		t.Errorf("legend = %+v", c.Legend)
	}
	x, y := c.Axes[g.XAxis], c.Axes[g.YAxis]
	if x.Deleted || x.BetweenCategories || x.MajorTicks != chart.TickIn || x.MinorTicks != chart.TickCross {
		t.Errorf("category axis = %+v", x)
	}
	if y.Deleted || y.Max != 10 || !math.IsNaN(y.Min) || y.Format != "0.0" || y.MajorGrid != nil || y.Position != chart.AxisLeft {
		t.Errorf("value axis = %+v, want no gridlines for a chart of two series", y)
	}
	if math.Abs(y.Line.Width-0.51) > 0.01 {
		t.Errorf("axis line %v points wide, want 0.51", y.Line.Width)
	}
	if g.GapWidth != 50 {
		t.Errorf("gap width = %v, want 50", g.GapWidth)
	}
}

func TestChartExFromElement_Histogram(t *testing.T) {
	xml := chartExXML(cxData("0", "_xlchart.v1.0", nil),
		`<cx:title><cx:tx><cx:txData><cx:v>Chart Title</cx:v></cx:txData></cx:tx></cx:title><cx:plotArea><cx:plotAreaRegion>`+
			`<cx:series layoutId="clusteredColumn"><cx:dataId val="0"/><cx:layoutPr><cx:binning intervalClosed="r"><cx:binSize val="2.5"/></cx:binning></cx:layoutPr></cx:series>`+
			`</cx:plotAreaRegion><cx:axis id="0"><cx:catScaling gapWidth="0"/><cx:tickLabels/></cx:axis><cx:axis id="1"><cx:valScaling/><cx:majorGridlines/></cx:axis></cx:plotArea>`)
	c := readChartEx(t, xml, map[string]string{"_xlchart.v1.0": "Sheet1!$A$1:$A$5"})
	g := c.Groups[0]
	if g.Kind != chart.Bar || g.GapWidth != 0 || c.Notice != nil {
		t.Errorf("group = %+v, notice %v: want bars without a notice", g, c.Notice)
	}
	s := g.Series[0]
	if s.Bins == nil || s.Bins.Width != 2.5 || s.Values.Ref != "Sheet1!$A$1:$A$5" || s.Name != "Column A" {
		t.Errorf("series = %+v, want bins 2.5 wide over the defined name's range", s)
	}
	if !c.Axes[0].BetweenCategories || c.Axes[1].MajorGrid == nil {
		t.Errorf("axes = %+v %+v", c.Axes[0], c.Axes[1])
	}
	if !hasWarning(c, CodeChartNoCache) {
		t.Errorf("warnings %+v lack %s", c.Warnings, CodeChartNoCache)
	}
}

func TestChartExFromElement_ParetoAndBareClusteredColumn(t *testing.T) {
	for _, region := range []string{
		`<cx:series layoutId="clusteredColumn"><cx:dataId val="0"/><cx:layoutPr><cx:binning/></cx:layoutPr></cx:series><cx:series layoutId="paretoLine" ownerIdx="0"/>`,
		`<cx:series layoutId="clusteredColumn"><cx:dataId val="0"/></cx:series>`,
	} {
		c := readChartEx(t, chartExXML(cxData("0", "", nil, "1", "2"), `<cx:plotArea><cx:plotAreaRegion>`+region+`</cx:plotAreaRegion>`+cxAxes+`</cx:plotArea>`), nil)
		if c.Groups[0].Kind != chart.Unsupported || c.Notice == nil {
			t.Errorf("%s: kind %v, notice %v, want the notice", region, c.Groups[0].Kind, c.Notice)
		}
	}
}

func TestChartExFromElement_MissingAxesAndNotChartEx(t *testing.T) {
	c := readChartEx(t, chartExXML(cxData("0", "", nil, "1"), `<cx:plotArea><cx:plotAreaRegion><cx:series layoutId="treemap"><cx:dataId val="0"/></cx:series></cx:plotAreaRegion></cx:plotArea>`), nil)
	if !c.Axes[0].Deleted || !c.Axes[1].Deleted {
		t.Errorf("axes = %+v %+v, want both missing", c.Axes[0], c.Axes[1])
	}
	root, _ := Parse(context.Background(), []byte(chartXML("", "", "")), nil)
	if _, err := ChartExFromElement(root, nil, ChartOptions{}, nil); err == nil {
		t.Error("a c:chartSpace reads as a chartex chart")
	}
}

func TestParseChartEx_DefinedNamesAndTheme(t *testing.T) {
	files := map[string]string{
		"[Content_Types].xml": contentTypes,
		"_rels/.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`,
		"xl/workbook.xml": `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><definedNames>` +
			`<definedName name="_xlchart.v1.0" hidden="1">Sheet1!$C$4:$C$15</definedName><definedName name="_xlchart.v1.0" localSheetId="0">Sheet1!$Z$1</definedName></definedNames></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/theme" Target="theme/theme1.xml"/></Relationships>`,
		"xl/theme/theme1.xml": `<a:theme xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><a:themeElements><a:clrScheme name="x">` +
			`<a:dk1><a:srgbClr val="000000"/></a:dk1><a:lt1><a:srgbClr val="FFFFFF"/></a:lt1><a:accent1><a:srgbClr val="4472C4"/></a:accent1></a:clrScheme>` +
			`<a:fontScheme name="f"><a:majorFont><a:latin typeface="Calibri Light"/></a:majorFont><a:minorFont><a:latin typeface="Calibri"/></a:minorFont></a:fontScheme></a:themeElements></a:theme>`,
		"xl/charts/chartEx1.xml": chartExXML(cxData("0", "_xlchart.v1.0", nil), `<cx:plotArea><cx:plotAreaRegion><cx:series layoutId="boxWhisker"><cx:dataId val="0"/></cx:series></cx:plotAreaRegion>`+cxAxes+`</cx:plotArea>`),
	}
	pkg, err := Open(buildZip(t, files))
	if err != nil {
		t.Fatal(err)
	}
	c, err := ParseChartEx(context.Background(), pkg, "xl/charts/chartEx1.xml")
	if err != nil {
		t.Fatal(err)
	}
	if ref := c.Groups[0].Series[0].Values.Ref; ref != "Sheet1!$C$4:$C$15" {
		t.Errorf("values' range = %q, want the workbook-wide defined name's", ref)
	}
	if got := c.Palette[0]; math.Abs(got.B-0xc4/255.0) > 1e-9 {
		t.Errorf("first automatic colour = %v, want the theme's accent1", got)
	}
	if c.Area.Border == nil {
		t.Error("a workbook's chart area has no border")
	}
	if _, err := ParseChartEx(context.Background(), pkg, "xl/charts/missing.xml"); !IsMissing(err) {
		t.Errorf("missing part: err = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ParseChartEx(ctx, pkg, "xl/charts/chartEx1.xml"); err == nil {
		t.Error("a cancelled context reads the chart")
	}
}

// hasWarning reports a warning of code.
func hasWarning(c *chart.Chart, code string) bool {
	for _, w := range c.Warnings {
		if w.Code == code {
			return true
		}
	}
	return false
}
