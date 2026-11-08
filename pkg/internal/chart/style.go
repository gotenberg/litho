package chart

import (
	"github.com/gotenberg/litho/pkg/internal/compose"
)

// defaultPalette is LibreOffice's own series colours, the automatic
// colours of a chart whose reader sets no palette (chart2's
// ChartColorPaletteHelper default palette).
var defaultPalette = []compose.Color{
	rgb(0x004586), rgb(0xff420e), rgb(0xffd320), rgb(0x579d1c), rgb(0x7e0021), rgb(0x83caff),
	rgb(0x314004), rgb(0xaecf00), rgb(0x4b1f6f), rgb(0xff950e), rgb(0xc5000b), rgb(0x0084d1),
}

// rgb returns the opaque colour 0xRRGGBB.
func rgb(v uint32) compose.Color {
	return compose.Color{R: float64(v>>16&0xff) / 255, G: float64(v>>8&0xff) / 255, B: float64(v&0xff) / 255, A: 1}
}

// AutoColor returns the automatic colour of item i of n items: the
// palette's colours in turn, and with PaletteCycleTint the colours of
// each cycle through the palette shaded or tinted as Office does: from a
// 70% shade before the first cycle to a 70% tint after the last, in equal
// steps (oox's ObjectFormatter, getPhColor).
func (c *Chart) AutoColor(i, n int) compose.Color {
	pal := c.Palette
	if len(pal) == 0 {
		pal = defaultPalette
	}
	if i < 0 {
		i = 0
	}
	col := pal[i%len(pal)]
	if !c.PaletteCycleTint || n <= 0 {
		return col
	}
	cycle, last := i/len(pal), (n-1)/len(pal)
	t := float64(cycle+1)/float64(last+2)*1.4 - 0.7
	switch {
	case t < -1e-9:
		f := 1 + t
		col.R, col.G, col.B = col.R*f, col.G*f, col.B*f
	case t > 1e-9:
		col.R, col.G, col.B = col.R+(1-col.R)*t, col.G+(1-col.G)*t, col.B+(1-col.B)*t
	}
	return col
}

// seriesCount returns the number of series of the chart, for the colour
// cycles.
func (c *Chart) seriesCount() int {
	n := 0
	for _, g := range c.Groups {
		n += len(g.Series)
	}
	return n
}

// lined reports a kind whose series draw as lines.
func lined(k Kind) bool { return k == Line || k == Scatter || k == Radar }

// fillOf returns the fill of point j of series s in group g, j -1 for the
// series itself.
func (rd *renderer) fillOf(g *Group, s *Series, j int) *Paint {
	if p := s.point(j); p != nil && p.Fill != nil {
		return p.Fill
	}
	// A pie varies its colours over the series' fill; another kind only
	// when the series has none (oox's SeriesConverter::createDataSeries).
	if g.VaryColors && j >= 0 && (g.Kind == Pie || g.Kind == Doughnut || len(g.Series) == 1 && s.Fill == nil) {
		return &Paint{Color: rd.c.AutoColor(j, s.Values.Len())}
	}
	if s.Fill != nil {
		return s.Fill
	}
	return &Paint{Color: rd.c.AutoColor(s.Index, rd.c.seriesCount())}
}

// borderOf returns the outline of point j of series s, nil for none. A
// line kind's border is its line, in the automatic colour by default.
func (rd *renderer) borderOf(g *Group, s *Series, j int) *Stroke {
	if p := s.point(j); p != nil && p.Border != nil {
		return p.Border
	}
	if s.Border != nil {
		return s.Border
	}
	if lined(g.Kind) && (g.Kind != Radar || !g.Filled) {
		return &Stroke{Color: rd.c.AutoColor(s.Index, rd.c.seriesCount()), Width: 1}
	}
	return nil
}

// point returns the override of point j, nil for none.
func (s *Series) point(j int) *Point {
	if j < 0 {
		return nil
	}
	for i := range s.Points {
		if s.Points[i].Index == j {
			return &s.Points[i]
		}
	}
	return nil
}

// markerOf returns the marker of point j of series s, its symbol resolved.
func (rd *renderer) markerOf(g *Group, s *Series, j int) Marker {
	m := s.Marker
	if p := s.point(j); p != nil && p.HasMarker {
		m = p.Marker
	}
	if m.Symbol == MarkerAuto {
		m.Symbol = MarkerSquare + MarkerSymbol(max(s.Index, 0)%int(MarkerVerticalBar-MarkerSquare+1)) //nolint:gosec // under 15 by the modulo
	}
	if !(m.Size > 0) {
		m.Size = 5
	}
	m.Size = min(m.Size, 100)
	if m.Fill == nil {
		if b := rd.borderOf(g, s, j); b != nil && !b.None {
			m.Fill = &Paint{Color: b.Color}
		} else {
			m.Fill = rd.fillOf(g, s, j)
		}
	}
	return m
}

// labelsOf returns the data labels of point j of series s.
func (s *Series) labelsOf(j int) Labels {
	if p := s.point(j); p != nil && p.HasLabels {
		return p.Labels
	}
	return s.Labels
}
