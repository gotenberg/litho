package raster

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/render/geom"
	"github.com/gotenberg/litho/pkg/internal/render/surface"
)

// A US Letter page at 150 dpi.
const benchW, benchH = 1275, 1650

func BenchmarkFillPageRect(b *testing.B) {
	p := rect(10.5, 10.5, benchW-10.5, benchH-10.5)
	for b.Loop() {
		if _, err := Fill(p, NonZero, benchW, benchH, nil); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFillPageCircle(b *testing.B) {
	p := circle(benchW/2, benchH/2, 600)
	for b.Loop() {
		if _, err := Fill(p, EvenOdd, benchW, benchH, nil); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkFillGlyphs fills a page of 3000 glyph-sized stars, each its own
// call, as text rendering does.
func BenchmarkFillGlyphs(b *testing.B) {
	var glyphs []geom.Path
	for i := range 3000 {
		glyphs = append(glyphs, star(20+float64(i%60)*20.3, 20+float64(i/60)*30.7, 6))
	}
	c, err := surface.NewCanvas(benchW, benchH, benchW*benchH)
	if err != nil {
		b.Fatal(err)
	}
	for b.Loop() {
		for _, g := range glyphs {
			cov, err := Fill(g, NonZero, benchW, benchH, nil)
			if err != nil {
				b.Fatal(err)
			}
			PaintSolid(c, cov, 0, 0, 0, 1)
		}
	}
}

func BenchmarkClipPage(b *testing.B) {
	p := circle(benchW/2, benchH/2, 600)
	for b.Loop() {
		if _, err := Clip(p, NonZero, nil, benchW, benchH, benchW*benchH); err != nil {
			b.Fatal(err)
		}
	}
}
