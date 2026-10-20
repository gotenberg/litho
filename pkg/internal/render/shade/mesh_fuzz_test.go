package shade

import (
	"context"
	"testing"
)

// FuzzParseMesh feeds arbitrary vertex data to the mesh readers of types 4
// to 7 under every permitted bit depth. The data and the layout both come
// from the file. The readers must never panic, and every triangle and patch
// they return must name vertices and colour values that exist, which
// painting indexes without a check.
func FuzzParseMesh(f *testing.F) {
	for _, fx := range []fixture{fixtureGouraud, fixtureCoons, fixtureCoonsShared, fixtureTensor, fixtureLattice, fixtureStrip} {
		data := streamData(fx.shading)
		f.Add([]byte(data), uint8(0), uint8(3), uint8(3), uint8(3))
		f.Add([]byte(data[:len(data)/2]), uint8(1), uint8(3), uint8(3), uint8(1))
	}
	f.Add([]byte{0xFF, 0x00, 0xAA}, uint8(3), uint8(0), uint8(0), uint8(1))

	coordDepths := []int{1, 2, 4, 8, 12, 16, 24, 32}
	compDepths := []int{1, 2, 4, 8, 12, 16}
	flagDepths := []int{2, 4, 8}
	f.Fuzz(func(t *testing.T, data []byte, kind, coord, comp, nvals uint8) {
		if len(data) > 1<<16 {
			return
		}
		p := meshParams{
			kind:      4 + int(kind%4),
			coordBits: coordDepths[int(coord)%len(coordDepths)],
			compBits:  compDepths[int(comp)%len(compDepths)],
			flagBits:  flagDepths[int(coord+comp)%len(flagDepths)],
			perRow:    2 + int(nvals%5),
			nvals:     1 + int(nvals%4),
		}
		for range 2 + p.nvals {
			p.decode = append(p.decode, 0, 1)
		}
		m, err := parseMesh(context.Background(), data, p)
		if err != nil {
			return
		}
		for _, tri := range m.tris {
			for _, v := range tri {
				if v < 0 || int(v) >= len(m.points) || (int(v)+1)*p.nvals > len(m.colors) {
					t.Fatalf("triangle %v names a vertex past %d points", tri, len(m.points))
				}
			}
		}
		for _, pt := range m.patches {
			for _, c := range pt.c {
				if c < 0 || int(c)+p.nvals > len(m.colors) {
					t.Fatalf("patch colour offset %d past %d values", c, len(m.colors))
				}
			}
		}
	})
}

// FuzzBitReader checks that the bit reader never reads past its data and
// never returns a value wider than it was asked for.
func FuzzBitReader(f *testing.F) {
	f.Add([]byte{0xB2, 0x4F, 0x00}, uint8(5), uint8(3))
	f.Add([]byte{}, uint8(8), uint8(1))
	f.Fuzz(func(t *testing.T, data []byte, width, steps uint8) {
		r := &bitReader{data: data}
		n := int(width%33) + 1
		for range int(steps) {
			v, ok := r.read(n)
			if !ok {
				return
			}
			if n <= 32 && v > uint64(1)<<uint(n)-1 {
				t.Fatalf("read(%d) = %#x", n, v)
			}
			if r.pos > 8*len(data) {
				t.Fatalf("position %d past %d bits", r.pos, 8*len(data))
			}
			r.align()
		}
	})
}

// streamData returns the stream data of a fixture's stream object.
func streamData(obj string) string {
	const open, closing = "stream\n", "\nendstream"
	start := len(obj)
	for i := range len(obj) - len(open) + 1 {
		if obj[i:i+len(open)] == open {
			start = i + len(open)
			break
		}
	}
	end := len(obj) - len(closing)
	if end < start {
		return ""
	}
	return obj[start:end]
}
