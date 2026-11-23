package ooxml

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/chart"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

func FuzzChartFromElement(f *testing.F) {
	full := chartXML(`<c:style val="18"/>`, `<c:title/><c:legend><c:legendPos val="b"/></c:legend>`,
		`<c:barChart><c:barDir val="col"/><c:grouping val="stacked"/>`+ser(0, "A", `<c:dLbls><c:showVal val="1"/></c:dLbls>`, "1", "-2", "3")+ser(1, "B", "", "4", "5")+`<c:axId val="1"/><c:axId val="2"/></c:barChart>`+
			`<c:lineChart>`+ser(2, "C", "", "1", "2")+`<c:axId val="1"/><c:axId val="2"/></c:lineChart>`+axes)
	f.Add([]byte(full))
	f.Add([]byte(full[:len(full)/2]))
	f.Add([]byte(chartXML("", "", `<c:pieChart>`+ser(0, "P", `<c:dLbls><c:showPercent val="1"/><c:dLblPos val="bestFit"/></c:dLbls>`, "1", "2", "3")+`</c:pieChart>`)))
	f.Add([]byte(bubbleXML))
	// A 3-D column chart with rows and a titled series axis, turned at
	// the ends of the view's ranges, and a 3-D pie seen edge on.
	f.Add([]byte(chartXML("", `<c:view3D><c:rotX val="-90"/><c:hPercent val="500"/><c:rotY val="360"/><c:depthPercent val="2000"/><c:rAngAx val="0"/><c:perspective val="240"/></c:view3D>`,
		`<c:bar3DChart><c:barDir val="col"/><c:grouping val="standard"/>`+ser(0, "A", "", "1", "-2", "3")+ser(1, "B", "", "4", "5")+
			`<c:gapDepth val="500"/><c:shape val="cone"/><c:axId val="1"/><c:axId val="2"/><c:axId val="3"/></c:bar3DChart>`+
			`<c:catAx><c:axId val="1"/><c:crossAx val="2"/></c:catAx><c:valAx><c:axId val="2"/><c:crossAx val="1"/></c:valAx>`+
			`<c:serAx><c:axId val="3"/><c:title><c:tx><c:rich><a:p><a:r><a:t>Rows</a:t></a:r></a:p></c:rich></c:tx></c:title><c:majorTickMark val="out"/><c:crossAx val="2"/></c:serAx>`)))
	f.Add([]byte(chartXML("", `<c:view3D><c:rotX val="90"/><c:hPercent val="5"/><c:rotY val="179"/><c:perspective val="0"/></c:view3D>`,
		`<c:pie3DChart>`+ser(0, "P", "", "1", "2", "3")+`</c:pie3DChart>`)))
	// A labelled pie of pie splitting at a huge percentage with a second
	// plot past its range, and a stock chart with high-low lines and
	// up-down bars of a huge gap width over values of extreme magnitude.
	f.Add([]byte(chartXML("", "", `<c:ofPieChart><c:ofPieType val="pie"/>`+ser(0, "P", `<c:dLbls><c:showPercent val="1"/></c:dLbls>`, "1e300", "-2", "3", "0")+
		`<c:splitType val="percent"/><c:splitPos val="1e300"/><c:secondPieSize val="100000"/><c:serLines/></c:ofPieChart>`)))
	f.Add([]byte(chartXML("", "", `<c:stockChart>`+ser(0, "O", "", "1e-300", "2")+ser(1, "H", "", "1e300", "9")+ser(2, "C", "", "-1e300", "1")+
		`<c:hiLowLines/><c:upDownBars><c:gapWidth val="1000000000"/><c:upBars/><c:downBars/></c:upDownBars><c:axId val="1"/><c:axId val="2"/></c:stockChart>`+axes)))
	fonts, err := text.NewFonts(context.Background(), nil, []string{filepath.Join("..", "layout", "testdata")})
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		root, err := Parse(context.Background(), data, nil)
		if err != nil {
			return
		}
		c, err := ChartFromElement(root, nil, ChartOptions{MSO2007: len(data)%2 == 0, Presentation: len(data)%3 == 0})
		if err != nil {
			return
		}
		start := time.Now()
		frags := chart.Render(context.Background(), fonts, c, typeset.Rect{X: 5, Y: 5, Width: 300, Height: 200})
		if d := time.Since(start); d > 2*time.Second {
			t.Fatalf("rendering took %v", d)
		}
		if len(frags) > 1 {
			t.Fatalf("got %d top fragments", len(frags))
		}
	})
}

func FuzzChartExFromElement(f *testing.F) {
	full := chartExXML(cxData("0", "Sheet1!$B$2:$B$4", []string{"a", "b", "c"}, "4", "5", "7")+cxData("1", "_xlchart.v1.0", nil),
		`<cx:title><cx:tx><cx:txData><cx:v>T</cx:v></cx:txData></cx:tx></cx:title><cx:plotArea><cx:plotAreaRegion>`+
			`<cx:series layoutId="funnel"><cx:dataId val="0"/></cx:series><cx:series layoutId="clusteredColumn"><cx:dataId val="1"/><cx:layoutPr><cx:binning><cx:binCount val="3"/></cx:binning></cx:layoutPr></cx:series>`+
			`</cx:plotAreaRegion>`+cxAxes+`</cx:plotArea><cx:legend pos="r"/>`)
	f.Add([]byte(full))
	f.Add([]byte(full[:len(full)/2]))
	f.Add([]byte(chartExXML(cxData("0", "", nil, "1", "2", "3"), `<cx:plotArea><cx:plotAreaRegion><cx:series layoutId="clusteredColumn"><cx:dataId val="0"/><cx:layoutPr><cx:binning><cx:binSize val="0.5"/></cx:binning></cx:layoutPr></cx:series></cx:plotAreaRegion>`+cxAxes+`</cx:plotArea>`)))
	fonts, err := text.NewFonts(context.Background(), nil, []string{filepath.Join("..", "layout", "testdata")})
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		root, err := Parse(context.Background(), data, nil)
		if err != nil {
			return
		}
		c, err := ChartExFromElement(root, nil, ChartOptions{Presentation: len(data)%2 == 0}, map[string]string{"_xlchart.v1.0": "Sheet1!$A$1:$A$3"})
		if err != nil {
			return
		}
		c.Resolver = func(string) []chart.Value {
			return []chart.Value{chart.Num(1), chart.Num(float64(len(data))), chart.Num(-3)}
		}
		start := time.Now()
		frags := chart.Render(context.Background(), fonts, c, typeset.Rect{X: 5, Y: 5, Width: 300, Height: 200})
		if d := time.Since(start); d > 2*time.Second {
			t.Fatalf("rendering took %v", d)
		}
		if len(frags) > 1 {
			t.Fatalf("got %d top fragments", len(frags))
		}
	})
}
