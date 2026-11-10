package odf

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/chart"
)

// chartNS declares the namespaces of a chart object's documents.
const chartNS = `xmlns:office="urn:oasis:names:tc:opendocument:xmlns:office:1.0" xmlns:style="urn:oasis:names:tc:opendocument:xmlns:style:1.0" ` +
	`xmlns:text="urn:oasis:names:tc:opendocument:xmlns:text:1.0" xmlns:table="urn:oasis:names:tc:opendocument:xmlns:table:1.0" ` +
	`xmlns:draw="urn:oasis:names:tc:opendocument:xmlns:drawing:1.0" xmlns:fo="urn:oasis:names:tc:opendocument:xmlns:xsl-fo-compatible:1.0" ` +
	`xmlns:svg="urn:oasis:names:tc:opendocument:xmlns:svg-compatible:1.0" xmlns:chart="urn:oasis:names:tc:opendocument:xmlns:chart:1.0" ` +
	`xmlns:number="urn:oasis:names:tc:opendocument:xmlns:datastyle:1.0" xmlns:loext="urn:org:documentfoundation:names:experimental:office:xmlns:loext:1.0"`

// tableXML is LibreOffice's embedded table of two series over two
// categories, the series' columns marked with their ranges.
const tableXML = `<table:table table:name="local-table"><table:table-header-columns><table:table-column/></table:table-header-columns>` +
	`<table:table-columns><table:table-column table:number-columns-repeated="2"/></table:table-columns>` +
	`<table:table-header-rows><table:table-row><table:table-cell><text:p/></table:table-cell>` +
	`<table:table-cell office:value-type="string"><text:p>North</text:p><draw:g><svg:desc>Sheet1.B1:Sheet1.B1</svg:desc></draw:g></table:table-cell>` +
	`<table:table-cell office:value-type="string"><text:p>South</text:p><draw:g><svg:desc>Sheet1.C1:Sheet1.C1</svg:desc></draw:g></table:table-cell></table:table-row></table:table-header-rows>` +
	`<table:table-rows><table:table-row><table:table-cell office:value-type="string"><text:p>Q1</text:p></table:table-cell>` +
	`<table:table-cell office:value-type="float" office:value="10"><text:p>10</text:p><draw:g><svg:desc>Sheet1.C2:Sheet1.C3</svg:desc></draw:g></table:table-cell>` +
	`<table:table-cell office:value-type="float" office:value="20"><text:p>20</text:p><draw:g><svg:desc>Sheet1.B2:Sheet1.B3</svg:desc></draw:g></table:table-cell></table:table-row>` +
	`<table:table-row><table:table-cell office:value-type="string"><text:p>Q2</text:p></table:table-cell>` +
	`<table:table-cell office:value-type="float" office:value="30"><text:p>30</text:p></table:table-cell><table:table-cell office:value-type="float" office:value="40"><text:p>40</text:p></table:table-cell></table:table-row></table:table-rows></table:table>`

// chartContent returns a chart object's content document.
func chartContent(styles, class, chartKids, plotKids string) string {
	return `<office:document-content ` + chartNS + `><office:automatic-styles>` + styles + `</office:automatic-styles><office:body><office:chart>` +
		`<chart:chart svg:width="16cm" svg:height="9cm" chart:class="chart:` + class + `" chart:style-name="ch1">` + chartKids +
		`<chart:plot-area chart:style-name="pa">` + plotKids + `</chart:plot-area>` + tableXML + `</chart:chart></office:chart></office:body></office:document-content>`
}

// series returns two series over the table's columns, B and C.
const series = `<chart:series chart:style-name="s1" chart:values-cell-range-address="Sheet1.B2:Sheet1.B3" chart:label-cell-address="Sheet1.B1:Sheet1.B1"><chart:data-point chart:repeated="1"/><chart:data-point chart:style-name="dp"/></chart:series>` +
	`<chart:series chart:style-name="s2" chart:values-cell-range-address="Sheet1.C2:Sheet1.C3" chart:label-cell-address="Sheet1.C1:Sheet1.C1"/>`

const chartStyles = `<number:number-style style:name="N1"><number:number number:decimal-places="2" number:min-integer-digits="1" number:grouping="true"/></number:number-style>` +
	`<number:percentage-style style:name="N2"><number:number number:decimal-places="0" number:min-integer-digits="1"/><number:text>%</number:text></number:percentage-style>` +
	`<style:style style:name="ch1" style:family="chart"><style:graphic-properties draw:stroke="none" draw:fill-color="#ffffff"/><style:text-properties fo:font-size="9pt" fo:font-family="DejaVu Sans"/></style:style>` +
	`<style:style style:name="pa" style:family="chart"><style:chart-properties chart:stacked="true" chart:vertical="true" chart:gap-width="80" chart:overlap="20" chart:angle-offset="45"/></style:style>` +
	`<style:style style:name="s1" style:family="chart"><style:chart-properties chart:data-label-number="value" chart:label-position="outside" chart:symbol-type="named-symbol" chart:symbol-name="diamond" chart:symbol-width="0.2cm" chart:interpolation="cubic-spline"/><style:graphic-properties draw:fill-color="#ff0000" svg:stroke-color="#00ff00" svg:stroke-width="0.05cm"/></style:style>` +
	`<style:style style:name="s2" style:family="chart"><style:graphic-properties draw:fill="none" draw:stroke="none"/></style:style>` +
	`<style:style style:name="dp" style:family="chart" style:parent-style-name="s1"><style:graphic-properties draw:fill-color="#0000ff"/><style:chart-properties chart:pie-offset="25"/></style:style>` +
	`<style:style style:name="ax" style:family="chart" style:data-style-name="N1"><style:chart-properties chart:display-label="true" chart:logarithmic="true" chart:minimum="1" chart:maximum="1000" chart:reverse-direction="true" chart:axis-position="end" chart:tick-marks-major-inner="true"/><style:graphic-properties svg:stroke-color="#123456"/><style:text-properties fo:font-size="8pt" fo:font-weight="bold"/></style:style>` +
	`<style:style style:name="lg" style:family="chart"><style:text-properties fo:font-size="11pt" fo:color="#00ff00"/></style:style>`

// readODFChart reads a chart from its content document.
func readODFChart(t *testing.T, content string) *chart.Chart {
	t.Helper()
	root, err := ParseXML(context.Background(), []byte(content))
	if err != nil {
		t.Fatalf("ParseXML: %v", err)
	}
	c, err := ChartFromDocument(root, nil)
	if err != nil {
		t.Fatalf("ChartFromDocument: %v", err)
	}
	return c
}

func TestChartFromDocument_Kinds(t *testing.T) {
	tests := []struct {
		class string
		kind  chart.Kind
	}{
		{"bar", chart.Bar},
		{"line", chart.Line},
		{"area", chart.Area},
		{"circle", chart.Pie},
		{"ring", chart.Doughnut},
		{"scatter", chart.Scatter},
		{"bubble", chart.Bubble},
		{"radar", chart.Radar},
		{"filled-radar", chart.Radar},
		{"stock", chart.Line},
	}
	for _, tt := range tests {
		t.Run(tt.class, func(t *testing.T) {
			c := readODFChart(t, chartContent(chartStyles, tt.class, "", series))
			if len(c.Groups) != 1 || c.Groups[0].Kind != tt.kind {
				t.Fatalf("groups = %+v", c.Groups)
			}
			if tt.class == "filled-radar" && !c.Groups[0].Filled {
				t.Error("a filled radar is not filled")
			}
		})
	}
}

func TestChartFromDocument_GroupAndSeries(t *testing.T) {
	c := readODFChart(t, chartContent(chartStyles, "bar", "", series))
	g := c.Groups[0]
	if !g.Horizontal || g.Grouping != chart.Stacked || g.GapWidth != 80 || g.Overlap != 20 || g.FirstSliceAngle != 45 {
		t.Errorf("group = %+v", g)
	}
	if len(g.Series) != 2 {
		t.Fatalf("got %d series", len(g.Series))
	}
	a, b := g.Series[0], g.Series[1]
	// The table marks column 2 as B's values and column 1 as C's.
	if a.Name != "North" || a.Values.Ref != "Sheet1.B2:Sheet1.B3" || a.Values.Values[0].Number != 20 || a.Values.Values[1].Number != 40 {
		t.Errorf("first series = %+v", a)
	}
	if b.Name != "South" || b.Values.Values[0].Number != 10 {
		t.Errorf("second series = %+v", b)
	}
	if a.Categories.Values[1].Text != "Q2" {
		t.Errorf("categories = %+v", a.Categories)
	}
	if a.Fill == nil || a.Fill.Color.R != 1 || a.Border == nil || a.Border.Color.G != 1 || !b.Fill.None || b.Border != nil {
		t.Errorf("fills %+v %+v borders %+v %+v", a.Fill, b.Fill, a.Border, b.Border)
	}
	if !a.Labels.ShowValue || a.Labels.Placement != chart.LabelOutsideEnd {
		t.Errorf("labels = %+v", a.Labels)
	}
	if len(a.Points) != 1 || a.Points[0].Index != 1 || a.Points[0].Fill.Color.B != 1 || a.Points[0].Explosion != 25 {
		t.Errorf("points = %+v", a.Points)
	}
}

func TestChartFromDocument_LineSeries(t *testing.T) {
	s := readODFChart(t, chartContent(chartStyles, "line", "", series)).Groups[0].Series[0]
	if s.Marker.Symbol != chart.MarkerDiamond || s.Marker.Size < 5.6 || s.Marker.Size > 5.7 || !s.Smooth || s.Border == nil || s.Border.Width < 1.4 {
		t.Errorf("line series = %+v marker %+v", s, s.Marker)
	}
}

func TestChartFromDocument_AxesTitlesLegend(t *testing.T) {
	kids := `<chart:title chart:style-name="ch1"><text:p>Main</text:p></chart:title><chart:subtitle><text:p>Sub</text:p></chart:subtitle>` +
		`<chart:legend chart:legend-position="bottom" loext:overlay="true" chart:style-name="lg"/>`
	plot := `<chart:axis chart:dimension="x" chart:name="primary-x"><chart:categories table:cell-range-address="Sheet1.A2:Sheet1.A3"/></chart:axis>` +
		`<chart:axis chart:dimension="y" chart:name="primary-y" chart:style-name="ax"><chart:grid chart:class="major"/><chart:title><text:p>Units</text:p></chart:title></chart:axis>` + series +
		`<chart:wall chart:style-name="s1"/>`
	c := readODFChart(t, chartContent(chartStyles, "line", kids, plot))
	if c.Title == nil || c.Title.Text != "Main" || c.Title.Style.Size != 9 || c.Subtitle == nil || c.Subtitle.Style.Size != 11 {
		t.Errorf("titles = %+v %+v", c.Title, c.Subtitle)
	}
	if c.Legend == nil || c.Legend.Position != chart.LegendBottom || !c.Legend.Overlay || c.Legend.Style.Size != 11 || c.Legend.Style.Color.G != 1 {
		t.Errorf("legend = %+v", c.Legend)
	}
	if len(c.Axes) != 2 {
		t.Fatalf("got %d axes", len(c.Axes))
	}
	x, y := c.Axes[0], c.Axes[1]
	if x.Kind != chart.CategoryAxis || x.BetweenCategories {
		t.Errorf("x axis = %+v", x)
	}
	if y.Log != 10 || y.Min != 1 || y.Max != 1000 || !y.Reverse || y.Crosses != chart.CrossMax || y.MajorTicks != chart.TickCross ||
		y.Format != "#,##0.00" || !y.Style.Bold || y.Style.Size != 8 || y.Line.Color.B != 0x56/255.0 || y.MajorGrid == nil {
		t.Errorf("y axis = %+v", y)
	}
	if y.Title == nil || y.Title.Text != "Units" || y.Title.Style.Rotation != 90 {
		t.Errorf("axis title = %+v", y.Title)
	}
	if c.PlotArea.Fill == nil || c.PlotArea.Fill.Color.R != 1 {
		t.Errorf("wall = %+v", c.PlotArea)
	}
	if c.Area.Fill == nil || c.Area.Border != nil {
		t.Errorf("chart area = %+v", c.Area)
	}
}

func TestChartFromDocument_PieDirection(t *testing.T) {
	plot := `<chart:axis chart:dimension="x" chart:name="primary-x" chart:style-name="x"/>` + series
	st := chartStyles + `<style:style style:name="x" style:family="chart"><style:chart-properties chart:reverse-direction="true"/></style:style>`
	if g := readODFChart(t, chartContent(st, "circle", "", plot)).Groups[0]; !g.CounterClockwise {
		t.Error("a pie with a reversed angle axis runs clockwise")
	}
	st = chartStyles + `<style:style style:name="x" style:family="chart"/>`
	if g := readODFChart(t, chartContent(st, "circle", "", plot)).Groups[0]; g.CounterClockwise {
		t.Error("a pie with its angle axis as is runs counter-clockwise")
	}
}

func TestChartFromDocument_NumberFormats(t *testing.T) {
	st := `<number:number-style style:name="P"><number:currency-symbol>$</number:currency-symbol><number:number number:decimal-places="0" number:min-integer-digits="1"/></number:number-style>` +
		`<number:number-style style:name="M"><number:text>-</number:text><number:currency-symbol>$</number:currency-symbol><number:number number:decimal-places="0" number:min-integer-digits="1"/><style:map style:condition="value()&gt;=0" style:apply-style-name="P"/></number:number-style>` +
		`<number:number-style style:name="E"><number:scientific-number number:decimal-places="2" number:min-integer-digits="1" number:min-exponent-digits="3"/></number:number-style>` +
		`<number:date-style style:name="D"><number:year number:style="long"/><number:text>-</number:text><number:month number:style="long"/><number:text>-</number:text><number:day/></number:date-style>` +
		`<number:number-style style:name="G"><number:number number:min-integer-digits="1"/></number:number-style>` +
		`<number:percentage-style style:name="H" number:language="hu" number:country="HU"><number:number number:decimal-places="2" number:min-integer-digits="1"/><number:text>%</number:text></number:percentage-style>` +
		`<number:number-style style:name="HM" number:language="de" number:country="DE"><number:number number:decimal-places="1" number:min-integer-digits="1"/><style:map style:condition="value()&gt;=0" style:apply-style-name="G"/></number:number-style>` +
		`<number:number-style style:name="EN" number:language="en" number:country="US"><number:number number:decimal-places="1" number:min-integer-digits="1"/></number:number-style>`
	root, err := ParseXML(context.Background(), []byte(chartContent(st, "bar", "", series)))
	if err != nil {
		t.Fatal(err)
	}
	r := &chartReader{formats: DataStylesOf(root, nil)}
	tests := map[string]string{"M": `[$$]0;\-[$$]0`, "E": "0.00E+000", "D": `yyyy\-mm\-d`, "G": "", "missing": "", "H": "[$-40E]0.00%", "EN": "0.0"}
	for name, want := range tests {
		if got := r.numberFormat(name); got != want {
			t.Errorf("numberFormat(%s) = %q, want %q", name, got, want)
		}
	}
}

func TestParseChart_Package(t *testing.T) {
	manifest := `<manifest:manifest xmlns:manifest="urn:oasis:names:tc:opendocument:xmlns:manifest:1.0"><manifest:file-entry manifest:full-path="/" manifest:media-type="application/vnd.oasis.opendocument.spreadsheet"/>` +
		`<manifest:file-entry manifest:full-path="Object 1/" manifest:media-type="application/vnd.oasis.opendocument.chart"/></manifest:manifest>`
	content := chartContent(chartStyles, "bar", "", series)
	styles := `<office:document-styles ` + chartNS + `><office:styles><style:style style:name="s2" style:family="chart"><style:graphic-properties draw:fill-color="#00ff00"/></style:style></office:styles></office:document-styles>`
	data := zipOf(t, "mimetype", "application/vnd.oasis.opendocument.spreadsheet", "META-INF/manifest.xml", manifest, "content.xml", minimalContent,
		"Object 1/content.xml", content, "Object 1/styles.xml", styles)
	pkg, err := Open(context.Background(), data, Options{})
	if err != nil {
		t.Fatal(err)
	}
	c, err := ParseChart(context.Background(), pkg, "./Object 1")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Groups) != 1 || len(c.Groups[0].Series) != 2 {
		t.Fatalf("groups = %+v", c.Groups)
	}
	if _, err := ParseChart(context.Background(), pkg, "Object 2"); !IsNotExist(err) {
		t.Errorf("missing object: err = %v", err)
	}
	if _, err := ParseChart(context.Background(), pkg, "."); err == nil || !strings.Contains(err.Error(), "no chart") {
		t.Errorf("object without a chart: err = %v", err)
	}
}

func TestChartFromDocument_Warnings(t *testing.T) {
	plot := `<chart:series chart:values-cell-range-address="Sheet9.A1:Sheet9.A2"><chart:regression-curve><chart:equation chart:display-equation="true"/></chart:regression-curve><chart:error-indicator/></chart:series>`
	content := strings.Replace(chartContent(chartStyles, "bar", "", plot), tableXML, "", 1)
	c := readODFChart(t, content)
	var msgs []string
	for _, w := range c.Warnings {
		msgs = append(msgs, w.Message)
	}
	got := strings.Join(msgs, "|")
	for _, want := range []string{"equation", "no values"} {
		if !strings.Contains(got, want) {
			t.Errorf("warnings %q lack %q", got, want)
		}
	}
	if s := c.Groups[0].Series[0]; s.Values.Ref != "Sheet9.A1:Sheet9.A2" || s.Values.Cached {
		t.Errorf("uncached series = %+v", s.Values)
	}
}

func TestChartFromDocument_LinkedAxisFormat(t *testing.T) {
	st := chartStyles + `<number:percentage-style style:name="P"><number:number number:decimal-places="0" number:min-integer-digits="1"/><number:text>%</number:text></number:percentage-style>` +
		`<style:style style:name="lx" style:family="chart" style:data-style-name="P"><style:chart-properties chart:link-data-style-to-source="true"/></style:style>`
	c := readODFChart(t, chartContent(st, "bar", "", `<chart:axis chart:dimension="y" chart:name="primary-y" chart:style-name="lx"/>`+series))
	if len(c.Axes) == 0 || c.Axes[0].Format != "0%" {
		t.Errorf("linked axis format = %+v", c.Axes)
	}
}

func TestChartFromDocument_AxisLabelBreaks(t *testing.T) {
	st := chartStyles + `<style:style style:name="b1" style:family="chart"><style:chart-properties text:line-break="true"/></style:style>` +
		`<style:style style:name="b2" style:family="chart"><style:chart-properties text:line-break="false" chart:text-overlap="true"/></style:style>`
	c := readODFChart(t, chartContent(st, "bar", "", `<chart:axis chart:dimension="x" chart:style-name="b1"/><chart:axis chart:dimension="y" chart:style-name="b2"/>`+series))
	if len(c.Axes) != 2 || c.Axes[0].NoBreak || c.Axes[0].Overlap || !c.Axes[1].NoBreak || !c.Axes[1].Overlap {
		t.Errorf("axes = %+v %+v", c.Axes[0], c.Axes[1])
	}
}

func TestChartFromDocument_PointNumberFormat(t *testing.T) {
	st := chartStyles + `<number:number-style style:name="E"><number:scientific-number number:decimal-places="2" number:min-integer-digits="1" number:min-exponent-digits="2"/></number:number-style>` +
		`<style:style style:name="pe" style:family="chart" style:data-style-name="E"/>`
	ser := `<chart:series chart:style-name="s1" chart:values-cell-range-address="Sheet1.B2:Sheet1.B3" chart:label-cell-address="Sheet1.B1:Sheet1.B1"><chart:data-point chart:style-name="pe"/></chart:series>`
	c := readODFChart(t, chartContent(st, "bar", "", ser))
	pts := c.Groups[0].Series[0].Points
	if len(pts) == 0 || !pts[0].HasLabels || pts[0].Labels.Format != "0.00E+00" || !pts[0].Labels.ShowValue {
		t.Errorf("points = %+v", pts)
	}
}

func TestChartFromDocument_MultiLevelCategoriesBreak(t *testing.T) {
	st := chartStyles + `<style:style style:name="nb" style:family="chart"><style:chart-properties text:line-break="false"/></style:style>`
	axes := `<chart:axis chart:dimension="x" chart:name="primary-x" chart:style-name="nb"><chart:categories table:cell-range-address="Sheet1.A2:Sheet1.B3"/></chart:axis><chart:axis chart:dimension="y" chart:name="primary-y" chart:style-name="nb"/>`
	c := readODFChart(t, chartContent(st, "bar", "", axes+series))
	if len(c.Axes) != 2 || c.Axes[0].NoBreak || !c.Axes[1].NoBreak {
		t.Errorf("axes = %+v %+v", c.Axes[0], c.Axes[1])
	}
}

func TestFormatSections(t *testing.T) {
	tests := []struct {
		code string
		want []string
	}{
		{"0.0", []string{"0.0"}},
		{`0;\-0;"a;b"`, []string{"0", `\-0`, `"a;b"`}},
		{`[>=0]0;\;0`, []string{"[>=0]0", `\;0`}},
	}
	for _, tt := range tests {
		if got := formatSections(tt.code); !slices.Equal(got, tt.want) {
			t.Errorf("formatSections(%q) = %q, want %q", tt.code, got, tt.want)
		}
	}
}

func TestChartFromDocument_LabelFrameAndAxisGap(t *testing.T) {
	st := chartStyles + `<style:style style:name="dl" style:family="chart"><style:graphic-properties draw:fill="solid" draw:fill-color="#00ff00" draw:stroke="solid" svg:stroke-color="#0000ff" svg:stroke-width="0.03in"/></style:style>` +
		`<style:style style:name="ay" style:family="chart"><style:chart-properties chart:gap-width="219" chart:overlap="-27"/></style:style>`
	sers := `<chart:series chart:style-name="s1" chart:values-cell-range-address="Sheet1.B2:Sheet1.B3"><chart:data-label chart:style-name="dl"/></chart:series>`
	plot := `<chart:axis chart:dimension="x" chart:name="primary-x"/><chart:axis chart:dimension="y" chart:name="primary-y" chart:style-name="ay"/>` + sers
	c := readODFChart(t, chartContent(strings.Replace(st, `chart:gap-width="80" chart:overlap="20" `, "", 1), "bar", "", plot))
	g := c.Groups[0]
	if g.GapWidth != 219 || g.Overlap != -27 {
		t.Errorf("gap %v overlap %v, want the value axis style's 219 and -27", g.GapWidth, g.Overlap)
	}
	f := g.Series[0].Labels.Frame
	if f.Fill == nil || f.Fill.Color.G != 1 || f.Border == nil || f.Border.Color.B != 1 {
		t.Errorf("label frame = %+v, want the data label style's green fill and blue border", f)
	}
}

func TestChartFromDocument_RegressionCurves(t *testing.T) {
	st := chartStyles + `<style:style style:name="rc" style:family="chart"><style:chart-properties chart:regression-type="moving-average" chart:regression-moving-type="averaged-abscissa" chart:regression-period="5" chart:regression-name="Avg" loext:regression-extrapolate-forward="2"/><style:graphic-properties svg:stroke-color="#8b0000" svg:stroke-width="0.04cm"/></style:style>` +
		`<style:style style:name="rl" style:family="chart"><style:chart-properties chart:regression-type="linear" chart:regression-force-intercept="true" chart:regression-intercept-value="3"/></style:style>` +
		`<style:style style:name="rn" style:family="chart"><style:chart-properties chart:regression-type="none"/></style:style>`
	sers := `<chart:series chart:style-name="s1" chart:values-cell-range-address="Sheet1.B2:Sheet1.B3"><chart:regression-curve chart:style-name="rc"/><chart:regression-curve chart:style-name="rl"/><chart:regression-curve chart:style-name="rn"/></chart:series>`
	c := readODFChart(t, chartContent(st, "line", "", sers))
	got := c.Groups[0].Series[0].Trendlines
	if len(got) != 2 {
		t.Fatalf("trendlines = %+v, want two", got)
	}
	if a := got[0]; a.Kind != chart.TrendMovingAverage || a.Moving != chart.MovingAveragedAbscissa || a.Period != 5 || a.Name != "Avg" || a.Forward != 2 || a.Line == nil || a.Line.Color.R < 0.5 {
		t.Errorf("first curve = %+v", a)
	}
	if b := got[1]; b.Kind != chart.TrendLinear || !b.HasIntercept || b.Intercept != 3 || b.Line != nil {
		t.Errorf("second curve = %+v", b)
	}
}

func TestChartFromDocument_ErrorIndicators(t *testing.T) {
	st := chartStyles + `<style:style style:name="ep" style:family="chart"><style:chart-properties chart:error-upper-indicator="true" chart:error-lower-indicator="true" chart:error-category="percentage" chart:error-percentage="10"/><style:graphic-properties svg:stroke-width="0cm" svg:stroke-color="#000000"/></style:style>` +
		`<style:style style:name="ec" style:family="chart"><style:chart-properties chart:error-upper-indicator="true" chart:error-category="cell-range" chart:error-upper-range="Sheet1.B2:Sheet1.B3"/></style:style>` +
		`<style:style style:name="en" style:family="chart"><style:chart-properties chart:error-category="none"/></style:style>`
	sers := `<chart:series chart:style-name="s1" chart:values-cell-range-address="Sheet1.B2:Sheet1.B3"><chart:error-indicator chart:style-name="ep" chart:dimension="y"/><chart:error-indicator chart:style-name="ec" chart:dimension="x"/><chart:error-indicator chart:style-name="en"/></chart:series>`
	c := readODFChart(t, chartContent(st, "scatter", "", sers))
	got := c.Groups[0].Series[0].ErrorBars
	if len(got) != 2 {
		t.Fatalf("error bars = %+v, want two", got)
	}
	if a := got[0]; a.Kind != chart.ErrorPercent || a.Plus != 10 || !a.Up || !a.Down || a.X || a.Line == nil {
		t.Errorf("first = %+v", a)
	}
	if b := got[1]; b.Kind != chart.ErrorCustom || !b.X || !b.Up || b.Down || b.PlusData.Len() != 2 {
		t.Errorf("second = %+v", b)
	}
}
