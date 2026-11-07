package odf

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/chart"
	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/text"
)

func FuzzChartFromDocument(f *testing.F) {
	full := chartContent(chartStyles, "bar", `<chart:title><text:p>T</text:p></chart:title><chart:legend chart:legend-position="end"/>`,
		`<chart:axis chart:dimension="x" chart:name="primary-x"/><chart:axis chart:dimension="y" chart:name="primary-y" chart:style-name="ax"><chart:grid/></chart:axis>`+series)
	f.Add([]byte(full))
	f.Add([]byte(full[:len(full)/2]))
	f.Add([]byte(chartContent(chartStyles, "circle", "", series)))
	fonts, err := text.NewFonts(context.Background(), nil, []string{filepath.Join("..", "layout", "testdata")})
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		root, err := ParseXML(context.Background(), data)
		if err != nil {
			return
		}
		c, err := ChartFromDocument(root, root)
		if err != nil {
			return
		}
		start := time.Now()
		frags := chart.Render(context.Background(), fonts, c, compose.Rect{X: 5, Y: 5, Width: 300, Height: 200})
		if d := time.Since(start); d > 2*time.Second {
			t.Fatalf("rendering took %v", d)
		}
		if len(frags) > 1 {
			t.Fatalf("got %d top fragments", len(frags))
		}
	})
}
