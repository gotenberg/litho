package shade

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/intconv"
	"github.com/gotenberg/litho/pkg/internal/render/geom"
)

// maxMeshValues bounds the numbers one mesh shading stores: two coordinates
// per vertex or control point, plus its colour values. The vertex data comes
// from a stream the file controls, and the stream budget alone allows a
// quarter of a gigabyte of it, which unpacks to billions of values: 8 bytes
// each. Anti-DoS, not spec-mandated: ISO 32000-1, 8.7.4.5.5 to 8.7.4.5.8 put
// no bound on a mesh. 1<<23 values is 64 MiB of float64, room for a million
// Gouraud vertices in an RGB space or a hundred thousand Coons patches.
//
// # Corpus evidence
//
//   - All corpora: 5,142 shadings, 2,697 of them meshes (2,669 type 4, 1
//     type 5, 2 type 6, 25 type 7). The largest stores 69,840 values (1,455
//     tensor patches), under 1% of the bound; the most triangles in one mesh
//     is 162.
const maxMeshValues = 1 << 23

// maxLatticeRow bounds /VerticesPerRow of a lattice-form mesh. Anti-DoS, not
// spec-mandated: Table 83 only requires at least 2. Rows are read whole
// before triangles are formed, so the bound keeps one row's buffer small
// while leaving room for any lattice a producer samples at screen
// resolution.
const maxLatticeRow = 1 << 16

// meshParams are the stream-layout entries of Tables 82 to 84.
type meshParams struct {
	kind      int
	coordBits int
	compBits  int
	flagBits  int // 0 for type 5, which carries no flags
	perRow    int // type 5 only
	nvals     int
	decode    []float64 // xmin xmax ymin ymax, then one pair per value
}

// mesh is the geometry [parseMesh] reads.
type mesh struct {
	points  []geom.Point
	colors  []float64
	tris    [][3]int32
	patches []patch
}

// initMesh reads the layout entries of a type 4 to 7 shading and its vertex
// data.
func (s *Shading) initMesh(ctx context.Context, doc *document.Document, dict ast.PdfDict, stream ast.PdfStream) error {
	p := meshParams{kind: s.kind}
	var err error
	if p.coordBits, err = intEntry(ctx, doc, dict, "BitsPerCoordinate"); err != nil {
		return err
	}
	// Tables 82 to 84 list the permitted depths.
	switch p.coordBits {
	case 1, 2, 4, 8, 12, 16, 24, 32:
	default:
		return fmt.Errorf("%w: /BitsPerCoordinate %d", ErrMalformed, p.coordBits)
	}
	if p.compBits, err = intEntry(ctx, doc, dict, "BitsPerComponent"); err != nil {
		return err
	}
	switch p.compBits {
	case 1, 2, 4, 8, 12, 16:
	default:
		return fmt.Errorf("%w: /BitsPerComponent %d", ErrMalformed, p.compBits)
	}
	if s.kind == 5 {
		if p.perRow, err = intEntry(ctx, doc, dict, "VerticesPerRow"); err != nil {
			return err
		}
		if p.perRow < 2 || p.perRow > maxLatticeRow {
			return fmt.Errorf("%w: /VerticesPerRow %d", ErrMalformed, p.perRow)
		}
	} else {
		if p.flagBits, err = intEntry(ctx, doc, dict, "BitsPerFlag"); err != nil {
			return err
		}
		switch p.flagBits {
		case 2, 4, 8:
		default:
			return fmt.Errorf("%w: /BitsPerFlag %d", ErrMalformed, p.flagBits)
		}
	}
	// Under a /Function each vertex carries one parametric value t in place
	// of the colour components (8.7.4.5.5).
	p.nvals = s.ncomp
	if s.funcs != nil {
		p.nvals = 1
	}
	if p.decode, err = numbers(ctx, doc, dict["Decode"]); err != nil || len(p.decode) < 4+2*p.nvals {
		return fmt.Errorf("%w: /Decode holds %d numbers, want %d", ErrMalformed, len(p.decode), 4+2*p.nvals)
	}
	p.decode = p.decode[:4+2*p.nvals]

	data, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		return fmt.Errorf("decoding mesh data: %w", err)
	}
	m, err := parseMesh(ctx, data, p)
	if err != nil {
		return err
	}
	s.nvals = p.nvals
	s.points, s.colors, s.tris, s.patches = m.points, m.colors, m.tris, m.patches
	if s.funcs != nil {
		return s.buildRamp(p.decode[4], p.decode[5])
	}
	return nil
}

// parseMesh reads the vertex data of a mesh shading.
//
// A truncated record at the end of the data ends the mesh and keeps what
// came before, as poppler and pdf.js do: ISO 32000-1, 8.7.4.5.5 to 8.7.4.5.8
// define no partial record, and a stream cut short mid-record is the common
// damage. A flag outside the type's range, or a patch that continues a
// previous one when there is none, ends the mesh for the same reason.
func parseMesh(ctx context.Context, data []byte, p meshParams) (*mesh, error) {
	m := &mesh{}
	r := &bitReader{data: data}
	switch p.kind {
	case 4:
		return m, m.readFreeForm(ctx, r, p)
	case 5:
		return m, m.readLattice(ctx, r, p)
	}
	return m, m.readPatches(ctx, r, p)
}

// checkCtxEvery is how many mesh records are read between context checks.
const checkCtxEvery = 256

// readVertex reads one vertex's coordinates and colour values, decoded
// through /Decode, and appends them. It reports false when the data ends
// first, leaving the mesh unchanged.
func (m *mesh) readVertex(r *bitReader, p meshParams) (int32, bool, error) {
	if len(m.colors)+2*len(m.points)+2+p.nvals > maxMeshValues {
		return 0, false, fmt.Errorf("%w: more than %d mesh values", ErrTooLarge, maxMeshValues)
	}
	pt, ok := readPoint(r, p)
	if !ok {
		return 0, false, nil
	}
	n := len(m.colors)
	for i := range p.nvals {
		raw, ok := r.read(p.compBits)
		if !ok {
			m.colors = m.colors[:n]
			return 0, false, nil
		}
		m.colors = append(m.colors, decodeValue(raw, p.compBits, p.decode[4+2*i], p.decode[5+2*i]))
	}
	m.points = append(m.points, pt)
	return int32(len(m.points) - 1), true, nil //nolint:gosec // len(points) <= maxMeshValues/2, which fits int32
}

// readPoint reads one coordinate pair, decoded through /Decode.
func readPoint(r *bitReader, p meshParams) (geom.Point, bool) {
	x, okX := r.read(p.coordBits)
	y, okY := r.read(p.coordBits)
	if !okX || !okY {
		return geom.Point{}, false
	}
	return geom.Point{
		X: decodeValue(x, p.coordBits, p.decode[0], p.decode[1]),
		Y: decodeValue(y, p.coordBits, p.decode[2], p.decode[3]),
	}, true
}

// readFreeForm reads a type 4 mesh (8.7.4.5.5). Each vertex carries an edge
// flag: 0 starts a new triangle, whose next two vertices' flags are ignored;
// 1 forms a triangle with the previous triangle's second and third vertices
// (vb, vc); 2 with its first and third (va, vc). Each vertex starts on a byte
// boundary, which is how poppler and pdf.js read the stream.
func (m *mesh) readFreeForm(ctx context.Context, r *bitReader, p meshParams) error {
	var cur []int32
	for n := 0; ; n++ {
		if n%checkCtxEvery == 0 {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("reading free-form mesh: %w", err)
			}
		}
		flag, ok := r.read(p.flagBits)
		if !ok {
			return nil
		}
		v, ok, err := m.readVertex(r, p)
		if err != nil || !ok {
			return err
		}
		r.align()
		switch {
		case len(cur) < 3:
			cur = append(cur, v)
		case flag == 0:
			cur = append(cur[:0], v)
		case flag == 1:
			cur = []int32{cur[1], cur[2], v}
		case flag == 2:
			cur = []int32{cur[0], cur[2], v}
		default:
			return nil
		}
		if len(cur) == 3 {
			m.tris = append(m.tris, [3]int32{cur[0], cur[1], cur[2]})
		}
	}
}

// readLattice reads a type 5 mesh (8.7.4.5.6): rows of /VerticesPerRow
// vertices, each pair of adjacent rows split into two triangles per cell.
// Each vertex starts on a byte boundary, as poppler reads the stream.
func (m *mesh) readLattice(ctx context.Context, r *bitReader, p meshParams) error {
	prev := make([]int32, 0, p.perRow)
	row := make([]int32, 0, p.perRow)
	for n := 0; ; n++ {
		if n%checkCtxEvery == 0 {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("reading lattice mesh: %w", err)
			}
		}
		v, ok, err := m.readVertex(r, p)
		if err != nil || !ok {
			return err
		}
		r.align()
		row = append(row, v)
		if len(row) < p.perRow {
			continue
		}
		if len(prev) == p.perRow {
			for i := range p.perRow - 1 {
				m.tris = append(m.tris,
					[3]int32{prev[i], prev[i+1], row[i]},
					[3]int32{prev[i+1], row[i+1], row[i]})
			}
		}
		prev, row = row, prev[:0]
	}
}

// boundary lists, in stream order, the indices i*4+j of the twelve boundary
// control points p_ij of a patch: p00 p01 p02 p03 p13 p23 p33 p32 p31 p30 p20
// p10 (ISO 32000-1, 8.7.4.5.7, Figure 98, and Table 85). The corners sit at
// positions 0, 3, 6 and 9, in the order the colours c00 c03 c33 c30 come.
var boundary = [12]int{0, 1, 2, 3, 7, 11, 15, 14, 13, 12, 8, 4}

// interior lists, in stream order, the four interior points of a
// tensor-product patch: p11 p12 p22 p21 (8.7.4.5.8, Table 86).
var interior = [4]int{5, 6, 10, 9}

// readPatches reads a type 6 or 7 mesh. Flag 0 starts a patch with all its
// points and four colours. Flags 1, 2 and 3 continue the previous patch: its
// edge D2, D3 or D4 (boundary positions 3-6, 6-9, 9-0) becomes the new
// patch's first edge, with that edge's two corner colours, and the stream
// supplies the rest. Each patch starts on a byte boundary, as poppler and
// pdf.js read the stream.
func (m *mesh) readPatches(ctx context.Context, r *bitReader, p meshParams) error {
	for n := 0; ; n++ {
		if n%checkCtxEvery == 0 {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("reading patch mesh: %w", err)
			}
		}
		flag, ok := r.read(p.flagBits)
		if !ok {
			return nil
		}
		if flag > 3 || (flag != 0 && len(m.patches) == 0) {
			return nil
		}
		var pt patch
		first := 0 // the boundary position the stream starts at
		firstColor := 0
		if flag != 0 {
			prev := m.patches[len(m.patches)-1]
			f := intconv.Clamp(flag)
			for k := range 4 {
				pt.p[boundary[k]] = prev.p[boundary[(3*f+k)%12]]
			}
			pt.c[0] = prev.c[f]
			pt.c[1] = prev.c[(f+1)%4]
			first, firstColor = 4, 2
		}
		need := 2*(12-first) + (4-firstColor)*p.nvals
		if p.kind == 7 {
			need += 8
		}
		if len(m.colors)+2*len(m.points)+2*16+need > maxMeshValues {
			return fmt.Errorf("%w: more than %d mesh values", ErrTooLarge, maxMeshValues)
		}
		complete := true
		for k := first; k < 12 && complete; k++ {
			pt.p[boundary[k]], complete = readPoint(r, p)
		}
		if p.kind == 7 {
			for k := 0; k < 4 && complete; k++ {
				pt.p[interior[k]], complete = readPoint(r, p)
			}
		}
		n0 := len(m.colors)
		for k := firstColor; k < 4 && complete; k++ {
			pt.c[k] = int32(len(m.colors)) //nolint:gosec // len(colors) <= maxMeshValues, which fits int32
			for i := 0; i < p.nvals && complete; i++ {
				var raw uint64
				raw, complete = r.read(p.compBits)
				m.colors = append(m.colors, decodeValue(raw, p.compBits, p.decode[4+2*i], p.decode[5+2*i]))
			}
		}
		if !complete {
			m.colors = m.colors[:n0]
			return nil
		}
		r.align()
		if p.kind == 6 {
			coonsInterior(&pt)
		}
		m.patches = append(m.patches, pt)
	}
}

// coonsInterior sets the four interior points of a Coons patch so that the
// tensor-product surface they define is the Coons surface: the equations of
// ISO 32000-1, 8.7.4.5.8, which express p11, p12, p21 and p22 in terms of
// the boundary points.
func coonsInterior(pt *patch) {
	// q is the weighted sum of the points p_ij named by the terms, over 9.
	type term struct {
		w    float64
		i, j int
	}
	q := func(terms ...term) geom.Point {
		var out geom.Point
		for _, t := range terms {
			out.X += t.w * pt.p[4*t.i+t.j].X
			out.Y += t.w * pt.p[4*t.i+t.j].Y
		}
		return geom.Point{X: out.X / 9, Y: out.Y / 9}
	}
	pt.p[5] = q(term{-4, 0, 0}, term{6, 0, 1}, term{6, 1, 0}, term{-2, 0, 3}, term{-2, 3, 0},
		term{3, 3, 1}, term{3, 1, 3}, term{-1, 3, 3})
	pt.p[6] = q(term{-4, 0, 3}, term{6, 0, 2}, term{6, 1, 3}, term{-2, 0, 0}, term{-2, 3, 3},
		term{3, 3, 2}, term{3, 1, 0}, term{-1, 3, 0})
	pt.p[9] = q(term{-4, 3, 0}, term{6, 3, 1}, term{6, 2, 0}, term{-2, 3, 3}, term{-2, 0, 0},
		term{3, 0, 1}, term{3, 2, 3}, term{-1, 0, 3})
	pt.p[10] = q(term{-4, 3, 3}, term{6, 3, 2}, term{6, 2, 3}, term{-2, 3, 0}, term{-2, 0, 3},
		term{3, 0, 2}, term{3, 2, 0}, term{-1, 0, 0})
}

// decodeValue maps a raw sample of the given width onto [dmin, dmax], the
// Decode mapping of 8.7.4.5.5.
func decodeValue(raw uint64, bits int, dmin, dmax float64) float64 {
	maxVal := float64(uint64(1)<<uint(bits) - 1) //nolint:gosec // bits <= 32, validated by initMesh
	return dmin + float64(raw)*(dmax-dmin)/maxVal
}

// bitReader reads big-endian bit fields from a byte slice, most significant
// bit first.
type bitReader struct {
	data []byte
	pos  int // in bits
}

// read returns the next n bits, n at most 32, and false when fewer remain.
func (r *bitReader) read(n int) (uint64, bool) {
	if n <= 0 || n > 32 || r.pos+n > 8*len(r.data) {
		return 0, false
	}
	var v uint64
	for range n {
		b := r.data[r.pos>>3] >> (7 - uint(r.pos&7)) & 1
		v = v<<1 | uint64(b)
		r.pos++
	}
	return v, true
}

// align skips to the next byte boundary.
func (r *bitReader) align() {
	r.pos = (r.pos + 7) &^ 7
}

// intEntry resolves the integer entry key of dict.
func intEntry(ctx context.Context, doc *document.Document, dict ast.PdfDict, key string) (int, error) {
	v, err := document.ResolveInt(ctx, doc, dict[ast.PdfName(key)])
	if err != nil {
		return 0, fmt.Errorf("%w: /%s: %w", ErrMalformed, key, err)
	}
	return intconv.Clamp(v), nil
}
