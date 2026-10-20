package raster

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/render/geom"
)

// pathFromBytes decodes a fuzz input into a path: each record is one op
// byte, then for each point two little-endian float32 coordinates. float32
// bit patterns reach NaN, infinities and the extremes directly.
func pathFromBytes(data []byte) geom.Path {
	var p geom.Path
	for len(data) > 0 {
		op := geom.Op(data[0] % 5)
		data = data[1:]
		n := map[geom.Op]int{geom.MoveTo: 1, geom.LineTo: 1, geom.CubicTo: 3}[op]
		p.Ops = append(p.Ops, op)
		for range n {
			if len(data) < 8 {
				return p
			}
			x := math.Float32frombits(binary.LittleEndian.Uint32(data))
			y := math.Float32frombits(binary.LittleEndian.Uint32(data[4:]))
			p.Pts = append(p.Pts, geom.Point{X: float64(x), Y: float64(y)})
			data = data[8:]
		}
	}
	return p
}

func FuzzFill(f *testing.F) {
	rec := func(op byte, xy ...float32) []byte {
		b := []byte{op}
		for _, v := range xy {
			b = binary.LittleEndian.AppendUint32(b, math.Float32bits(v))
		}
		return b
	}
	var star []byte
	star = append(star, rec(0, 10, 2)...)
	star = append(star, rec(1, 15, 18)...)
	star = append(star, rec(1, 2, 8)...)
	star = append(star, rec(1, 18, 8)...)
	star = append(star, rec(1, 5, 18)...)
	star = append(star, rec(3)...)
	f.Add(star, false)
	f.Add(star[:20], true)
	f.Add(rec(2, 0, 1e30, -1e30, 4, 30, 30), true)
	f.Fuzz(func(t *testing.T, data []byte, evenOdd bool) {
		rule := NonZero
		if evenOdd {
			rule = EvenOdd
		}
		c, err := Fill(pathFromBytes(data), rule, 20, 20, nil)
		if err != nil {
			return
		}
		if len(c.A) != c.Width*c.Height || c.X < 0 || c.Y < 0 || c.X+c.Width > 20 || c.Y+c.Height > 20 {
			t.Fatalf("coverage box %d,%d %dx%d (len %d) leaves the raster", c.X, c.Y, c.Width, c.Height, len(c.A))
		}
		for _, v := range c.A {
			if !(v >= 0 && v <= 1) {
				t.Fatalf("coverage %v outside [0, 1]", v)
			}
		}
	})
}
