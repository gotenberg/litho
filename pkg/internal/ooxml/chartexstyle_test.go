package ooxml

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/chart"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// cxStyleXML returns a cs:chartStyle whose entries are entries, each a
// name and its content.
func cxStyleXML(entries ...string) string {
	s := `<cs:chartStyle xmlns:cs="http://schemas.microsoft.com/office/drawing/2012/chartStyle" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" id="410">`
	for i := 0; i+1 < len(entries); i += 2 {
		s += `<cs:` + entries[i] + `>` + entries[i+1] + `</cs:` + entries[i] + `>`
	}
	return s + `</cs:chartStyle>`
}

// blankEntry is a style entry with no theme style and the text colour.
const blankEntry = `<cs:lnRef idx="0"/><cs:fillRef idx="0"/><cs:effectRef idx="0"/><cs:fontRef idx="minor"><a:schemeClr val="tx1"/></cs:fontRef>`

const cxColorStyle = `<cs:colorStyle xmlns:cs="http://schemas.microsoft.com/office/drawing/2012/chartStyle" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" meth="cycle" id="10">` +
	`<a:srgbClr val="FF0000"/><a:srgbClr val="0000FF"/><cs:variation/><cs:variation><a:lumMod val="50000"/></cs:variation></cs:colorStyle>`

// readStyledChartEx reads xml with the chart style style and the colour
// style colors.
func readStyledChartEx(t *testing.T, xml, style, colors string) *chart.Chart {
	t.Helper()
	parse := func(s string) *Element {
		if s == "" {
			return nil
		}
		e, err := Parse(context.Background(), []byte(s), nil)
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		return e
	}
	root := parse(xml)
	c, err := ChartExFromElement(root, nil, ChartOptions{ChartStyle: parse(style), ColorStyle: parse(colors)}, nil)
	if err != nil {
		t.Fatalf("ChartExFromElement: %v", err)
	}
	return c
}

const cxTitleLegend = `<cx:title><cx:tx><cx:txData><cx:v>T</cx:v></cx:txData></cx:tx></cx:title>`

func TestChartExFromElement_BlankChartStyle(t *testing.T) {
	xml := chartExXML(cxData("0", "Sheet1!$B$2:$B$4", []string{"a", "b", "c"}, "4", "5", "7"),
		cxTitleLegend+`<cx:plotArea><cx:plotAreaRegion><cx:series layoutId="waterfall"><cx:dataId val="0"/></cx:series></cx:plotAreaRegion>`+cxAxes+`</cx:plotArea><cx:legend pos="t"/>`)
	var entries []string
	for _, name := range []string{"axisTitle", "categoryAxis", "chartArea", "dataLabel", "dataPoint", "gridlineMajor", "gridlineMinor", "legend", "plotArea", "title", "valueAxis"} {
		entries = append(entries, name, blankEntry)
	}
	c := readStyledChartEx(t, xml, cxStyleXML(entries...), cxColorStyle)
	if c.Rules != chart.OfficeRules || !c.Office2016 {
		t.Errorf("rules %v, Office2016 %v: want Office's rules for an Office 2016 chart", c.Rules, c.Office2016)
	}
	if c.Area.Fill != nil || c.Area.Border != nil || c.PlotArea.Fill != nil || c.PlotArea.Border != nil {
		t.Errorf("chart area %+v, plot area %+v: want neither filled nor framed", c.Area, c.PlotArea)
	}
	s := c.Groups[0].Series[0]
	if s.Fill == nil || !s.Fill.None {
		t.Errorf("series fill %+v, want none", s.Fill)
	}
	x, y := c.Axes[0], c.Axes[1]
	if x.Line != nil || y.Line != nil || y.MajorGrid != nil {
		t.Errorf("axis lines %+v %+v, gridlines %+v: want none", x.Line, y.Line, y.MajorGrid)
	}
	if c.Title.Style.Size != 14 || c.Title.Style.Bold || c.Legend.Style.Size != 9 || x.Style.Size != 8 || y.Style.Size != 8 {
		t.Errorf("text sizes %v %v %v %v, bold title %v: want 14, 9, 8 and 8, regular", c.Title.Style.Size, c.Legend.Style.Size, x.Style.Size, y.Style.Size, c.Title.Style.Bold)
	}
	if c.Title.Style.Color != typeset.Black {
		t.Errorf("title colour %v, want the theme's tx1", c.Title.Style.Color)
	}
	// The colour style's colours, then their variation: red, blue, half
	// red, half blue.
	if len(c.Palette) != 4 || c.Palette[0] != (typeset.Color{R: 1, A: 1}) || c.Palette[3].B >= 0.6 || c.Palette[3].B <= 0.4 || c.PaletteCycleTint {
		t.Errorf("palette %v, want the colour style's four colours", c.Palette)
	}
}

func TestChartExFromElement_PlaceholderChartStyle(t *testing.T) {
	xml := chartExXML(cxData("0", "Sheet1!$B$2:$B$4", nil, "4", "5", "7"),
		`<cx:plotArea><cx:plotAreaRegion><cx:series layoutId="clusteredColumn"><cx:dataId val="0"/><cx:layoutPr><cx:binning/></cx:layoutPr></cx:series></cx:plotAreaRegion>`+cxAxes+`</cx:plotArea>`)
	ph := `<cs:lnRef idx="0"><cs:styleClr val="auto"/></cs:lnRef><cs:fillRef idx="0"><cs:styleClr val="auto"/></cs:fillRef><cs:effectRef idx="0"/><cs:fontRef idx="minor"><a:schemeClr val="tx1"/></cs:fontRef>` +
		`<cs:spPr><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></cs:spPr>`
	grid := blankEntry + `<cs:spPr><a:ln w="9525"><a:solidFill><a:srgbClr val="00FF00"/></a:solidFill></a:ln></cs:spPr>`
	text := blankEntry + `<cs:defRPr sz="900" b="1"/>`
	c := readStyledChartEx(t, xml, cxStyleXML("dataPoint", ph, "gridlineMajor", grid, "valueAxis", text), cxColorStyle)
	if s := c.Groups[0].Series[0]; s.Fill != nil {
		t.Errorf("series fill %+v, want the automatic colour of the colour style", s.Fill)
	}
	if g := c.Axes[1].MajorGrid; g == nil || g.Color != (typeset.Color{G: 1, A: 1}) || g.Width != 0.75 {
		t.Errorf("gridlines %+v, want the style's 0.75 pt green", g)
	}
	if st := c.Axes[1].Style; st.Size != 9 || !st.Bold {
		t.Errorf("value labels %+v, want the style's 9 pt bold", st)
	}
}
