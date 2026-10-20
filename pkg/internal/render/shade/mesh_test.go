package shade

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/render/geom"
)

// gray8 is the layout of the 8-bit DeviceGray test meshes: coordinates
// decoded onto [0, 255], one component onto [0, 1].
func gray8(kind int) meshParams {
	p := meshParams{kind: kind, coordBits: 8, compBits: 8, nvals: 1, decode: []float64{0, 255, 0, 255, 0, 1}}
	if kind == 5 {
		p.perRow = 2
	} else {
		p.flagBits = 8
	}
	return p
}

// TestFreeFormFlags checks the edge flags of 8.7.4.5.5: flag 1 reuses the
// previous triangle's vb and vc, flag 2 its va and vc.
func TestFreeFormFlags(t *testing.T) {
	t.Parallel()
	data := packBits(0, 8, 10, 8, 10, 8, 0, 8,
		7, 8, 50, 8, 10, 8, 128, 8, // the flags of a new triangle's second and third vertices are ignored
		3, 8, 10, 8, 50, 8, 64, 8,
		1, 8, 50, 8, 50, 8, 255, 8,
		2, 8, 90, 8, 90, 8, 0, 8,
		0, 8, 1, 8, 1, 8, 0, 8) // a new triangle that never completes
	m, err := parseMesh(context.Background(), []byte(data), gray8(4))
	if err != nil {
		t.Fatal(err)
	}
	want := [][3]int32{{0, 1, 2}, {1, 2, 3}, {1, 3, 4}}
	if len(m.tris) != len(want) {
		t.Fatalf("got %d triangles %v, want %v", len(m.tris), m.tris, want)
	}
	for i := range want {
		if m.tris[i] != want[i] {
			t.Errorf("triangle %d = %v, want %v", i, m.tris[i], want[i])
		}
	}
	if m.points[3] != (geom.Point{X: 50, Y: 50}) || m.colors[3] != 1 {
		t.Errorf("vertex 3 = %v, %v, want (50,50), 1", m.points[3], m.colors[3])
	}
}

// TestFreeFormBadFlag checks that a flag outside 0 to 2 ends the mesh and
// keeps the triangles before it.
func TestFreeFormBadFlag(t *testing.T) {
	t.Parallel()
	data := packBits(0, 8, 0, 8, 0, 8, 0, 8, 0, 8, 9, 8, 0, 8, 0, 8, 0, 8, 0, 8, 9, 8, 0, 8,
		3, 8, 5, 8, 5, 8, 0, 8,
		1, 8, 6, 8, 6, 8, 0, 8)
	m, err := parseMesh(context.Background(), []byte(data), gray8(4))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.tris) != 1 {
		t.Errorf("got %d triangles, want 1", len(m.tris))
	}
}

// TestLatticeAlignment checks that each lattice vertex starts on a byte
// boundary, as poppler reads it: the 20-bit vertices of fixtureLattice land
// where they were written only under that reading.
func TestLatticeAlignment(t *testing.T) {
	t.Parallel()
	p := gray8(5)
	p.compBits = 4
	data := packBits(10, 8, 10, 8, 0, 4, 0, 0,
		90, 8, 10, 8, 15, 4, 0, 0,
		10, 8, 90, 8, 5, 4, 0, 0,
		90, 8, 90, 8, 10, 4, 0, 0)
	m, err := parseMesh(context.Background(), []byte(data), p)
	if err != nil {
		t.Fatal(err)
	}
	wantPts := []geom.Point{{X: 10, Y: 10}, {X: 90, Y: 10}, {X: 10, Y: 90}, {X: 90, Y: 90}}
	wantVals := []float64{0, 1, 5.0 / 15, 10.0 / 15}
	if len(m.points) != 4 {
		t.Fatalf("got %d vertices, want 4", len(m.points))
	}
	for i := range wantPts {
		if m.points[i] != wantPts[i] || m.colors[i] != wantVals[i] {
			t.Errorf("vertex %d = %v %v, want %v %v", i, m.points[i], m.colors[i], wantPts[i], wantVals[i])
		}
	}
	want := [][3]int32{{0, 1, 2}, {1, 3, 2}}
	if len(m.tris) != 2 || m.tris[0] != want[0] || m.tris[1] != want[1] {
		t.Errorf("triangles = %v, want %v", m.tris, want)
	}
}

// TestTruncatedMesh checks that a record cut short ends the mesh without an
// error and without a partial vertex or patch.
func TestTruncatedMesh(t *testing.T) {
	t.Parallel()
	full := fixtureGouraud.shading
	start := strings.Index(full, "stream\n") + len("stream\n")
	data := []byte(full[start : start+3*6])
	p := meshParams{kind: 4, coordBits: 8, compBits: 8, flagBits: 8, nvals: 3, decode: []float64{0, 255, 0, 255, 0, 1, 0, 1, 0, 1}}
	for cut := range len(data) {
		m, err := parseMesh(context.Background(), data[:cut], p)
		if err != nil {
			t.Fatalf("cut at %d: %v", cut, err)
		}
		if len(m.points) != cut/6 || len(m.colors) != 3*len(m.points) || len(m.tris) != 0 {
			t.Errorf("cut at %d: %d points, %d values, %d triangles", cut, len(m.points), len(m.colors), len(m.tris))
		}
	}

	patch := gray8(6)
	one := packBits(0, 8, 1, 8, 2, 8, 3, 8, 4, 8, 5, 8, 6, 8, 7, 8, 8, 8, 9, 8, 10, 8, 11, 8, 12, 8,
		13, 8, 14, 8, 15, 8, 16, 8, 17, 8, 18, 8, 19, 8, 20, 8, 21, 8, 22, 8, 23, 8, 24, 8, 1, 8, 2, 8, 3, 8, 4, 8)
	m, err := parseMesh(context.Background(), []byte(one[:len(one)-1]), patch)
	if err != nil || len(m.patches) != 0 || len(m.colors) != 0 {
		t.Errorf("truncated patch: %v, %d patches, %d values", err, len(m.patches), len(m.colors))
	}
	m, err = parseMesh(context.Background(), []byte(one), patch)
	if err != nil || len(m.patches) != 1 {
		t.Fatalf("one patch: %v, %d patches", err, len(m.patches))
	}
	if got := m.patches[0].p[boundary[11]]; got != (geom.Point{X: 23, Y: 24}) {
		t.Errorf("p10 = %v, want (23,24)", got)
	}
}

// TestPatchContinuation checks the shared edge of flags 1 to 3 against
// Table 85: the new patch's p00 to p03 are the previous patch's D2, D3 or D4
// edge, and its c00 and c03 that edge's corner colours.
func TestPatchContinuation(t *testing.T) {
	t.Parallel()
	first := []uint64{0, 8}
	for k := range 12 {
		first = append(first, uint64(10*k), 8, uint64(10*k+1), 8)
	}
	first = append(first, 10, 8, 20, 8, 30, 8, 40, 8)
	for flag := uint64(1); flag <= 3; flag++ {
		fields := append(append([]uint64(nil), first...), flag, 8)
		for range 8 {
			fields = append(fields, 200, 8, 201, 8)
		}
		fields = append(fields, 50, 8, 60, 8)
		m, err := parseMesh(context.Background(), []byte(packBits(fields...)), gray8(6))
		if err != nil || len(m.patches) != 2 {
			t.Fatalf("flag %d: %v, %d patches", flag, err, len(m.patches))
		}
		prev, next := m.patches[0], m.patches[1]
		f := int(flag)
		for k := range 4 {
			if got, want := next.p[boundary[k]], prev.p[boundary[(3*f+k)%12]]; got != want {
				t.Errorf("flag %d: shared point %d = %v, want %v", flag, k, got, want)
			}
		}
		corner := func(pt patch, k int) float64 { return m.colors[pt.c[k]] }
		if corner(next, 0) != corner(prev, f) || corner(next, 1) != corner(prev, (f+1)%4) {
			t.Errorf("flag %d: shared colours %v %v, want %v %v", flag,
				corner(next, 0), corner(next, 1), corner(prev, f), corner(prev, (f+1)%4))
		}
		if next.p[boundary[4]] != (geom.Point{X: 200, Y: 201}) || corner(next, 2) != 50.0/255 {
			t.Errorf("flag %d: the stream's points and colours did not follow the shared edge", flag)
		}
	}

	// A continuation with no patch before it describes nothing.
	m, err := parseMesh(context.Background(), []byte(packBits(1, 8, 0, 8, 0, 8)), gray8(6))
	if err != nil || len(m.patches) != 0 {
		t.Errorf("leading continuation: %v, %d patches", err, len(m.patches))
	}
}

// TestMeshTooLarge checks the maxMeshValues bound: each 1-bit lattice vertex
// fills one byte and stores three values.
func TestMeshTooLarge(t *testing.T) {
	t.Parallel()
	p := meshParams{kind: 5, coordBits: 1, compBits: 1, perRow: 2, nvals: 1, decode: []float64{0, 1, 0, 1, 0, 1}}
	data := make([]byte, maxMeshValues/3+1)
	if _, err := parseMesh(context.Background(), data, p); !errors.Is(err, ErrTooLarge) {
		t.Errorf("parseMesh = %v, want ErrTooLarge", err)
	}
}

// TestCoonsInterior checks the interior points of a Coons patch on a
// boundary that is a bilinear square: they land at the thirds, the tensor
// points of the same surface.
func TestCoonsInterior(t *testing.T) {
	t.Parallel()
	var pt patch
	for i := range 4 {
		for j := range 4 {
			pt.p[4*i+j] = geom.Point{X: float64(3 * i), Y: float64(3 * j)}
		}
	}
	want := pt
	for _, k := range interior {
		pt.p[k] = geom.Point{}
	}
	coonsInterior(&pt)
	for _, k := range interior {
		dx, dy := pt.p[k].X-want.p[k].X, pt.p[k].Y-want.p[k].Y
		if dx*dx+dy*dy > 1e-18 {
			t.Errorf("interior point %d = %v, want %v", k, pt.p[k], want.p[k])
		}
	}
}
