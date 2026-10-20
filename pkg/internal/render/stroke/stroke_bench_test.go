package stroke

import (
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/render/geom"
)

// pageCTM maps PDF points to a 150 dpi device with the y axis flipped, the
// matrix a renderer of a US Letter page uses.
var pageCTM = geom.Matrix{150.0 / 72, 0, 0, -150.0 / 72, 0, 792 * 150.0 / 72}

// zigzag is a 1000-segment polyline across a page, every vertex a corner.
func zigzag() geom.Path {
	var p geom.Path
	p.MoveTo(pt(36, 36))
	for i := 1; i <= 1000; i++ {
		p.LineTo(pt(36+float64(i)*0.54, 36+float64(i%2)*700))
	}
	return p
}

// circles is 100 circles of radius 20, each four cubics.
func circles() geom.Path {
	var p geom.Path
	const k = 0.5522847498
	for i := range 100 {
		cx, cy, r := 50+float64(i%10)*50, 100+float64(i/10)*60, 20.0
		p.MoveTo(pt(cx+r, cy))
		for q := range 4 {
			a := float64(q) * math.Pi / 2
			c0, s0 := math.Cos(a), math.Sin(a)
			c1, s1 := math.Cos(a+math.Pi/2), math.Sin(a+math.Pi/2)
			p.CubicTo(
				pt(cx+r*(c0-k*s0), cy+r*(s0+k*c0)),
				pt(cx+r*(c1+k*s1), cy+r*(s1-k*c1)),
				pt(cx+r*c1, cy+r*s1),
			)
		}
		p.Close()
	}
	return p
}

func benchOutline(b *testing.B, p geom.Path, s Style) {
	b.Helper()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Outline(p, s, pageCTM, DefaultTolerance); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkZigzagMiter(b *testing.B) {
	benchOutline(b, zigzag(), Style{Width: 2, MiterLimit: DefaultMiterLimit})
}

func BenchmarkZigzagRound(b *testing.B) {
	benchOutline(b, zigzag(), Style{Width: 2, Join: RoundJoin, Cap: RoundCap})
}

func BenchmarkCircles(b *testing.B) {
	benchOutline(b, circles(), Style{Width: 3})
}

func BenchmarkCirclesDashed(b *testing.B) {
	benchOutline(b, circles(), Style{Width: 3, Cap: RoundCap, Dash: []float64{4, 2}})
}

func BenchmarkHairline(b *testing.B) {
	benchOutline(b, zigzag(), Style{})
}
