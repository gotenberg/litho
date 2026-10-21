package icc

import (
	"encoding/binary"
	"fmt"
)

// maxChannels bounds the input channels of a lookup table: the 15 channels of
// 'FCLR', the widest data colour space of ICC.1:2010, Table 19, so that an
// n-colour profile reads. It is an anti-DoS guard, not spec-mandated: a CLUT
// holds grid^channels entries, which maxCLUTEntries bounds, and one colour
// interpolates over at most 2^(15-3) = 4096 tetrahedra.
const maxChannels = maxOutputs

// maxCLUTEntries bounds the values of a CLUT, each held as a float64. It is an
// anti-DoS guard, not spec-mandated: the grid is a byte of the file raised to
// the channel count. A 33-point grid over four channels into three outputs,
// the densest in common use, holds 3.6 million values.
const maxCLUTEntries = 1 << 22

// PCS encodings a pipeline's output is read in.
const (
	pcsXYZ    = iota // u1Fixed15Number: 1.0 is 0x8000 (ICC.1:2010, 6.3.4.3)
	pcsLab16         // legacy 16-bit Lab of lut16Type: L* 100 is 0xFF00 (10.9)
	pcsLab8          // 8-bit Lab of lut8Type (10.8)
	pcsLabV4         // Lab of lutAtoBType: L* 100 is 0xFFFF (6.3.4.2)
	pcsDevice        // device values of a DeviceLink profile (7.2.7)
)

// pipeline is a lookup-table transform (ICC.1:2010, 10.8, 10.9 and 10.10):
// input curves, a CLUT, then output curves, with the M curves and matrix a
// lutAtoBType may add between the CLUT and the output curves.
//
// A lutBtoAType table (10.12) runs the other way, and btoa marks it: its B
// curves, matrix and M curves on the connection space side come first, then
// the CLUT, then its A curves on the device side. A lut8Type or lut16Type
// table keeps its order in both directions, and its matrix applies only to
// XYZ input (10.8), so only a BToA table into a device space from the XYZ
// connection space keeps it, in inMatrix.
type pipeline struct {
	in, out  int
	a        []curve
	clut     *clut
	m        []curve
	matrix   *[12]float64
	b        []curve
	inMatrix *[9]float64
	btoa     bool
	// encoding is the connection space encoding of the table's output, or of
	// its input for a BToA table.
	encoding int
}

// clut is a colour lookup table: grid[i] points along input i, the first
// input varying slowest, out values a point, normalized to 0 to 1.
type clut struct {
	grid []int
	out  int
	data []float64
	// trilinear interpolates linearly along every input, for a table over
	// Lab. ICC.1:2010, 10.10.3 leaves the interpolation to the colour
	// engine; tetrahedral interpolation splits each cell along its diagonal,
	// which for Lab mixes L* with a* and b*, so a Lab table's neutrals, which
	// run along the L* input, would take a tint. littleCMS reads Lab tables
	// trilinearly too, so the two engines agree on them.
	trilinear bool
}

// readPipeline parses a lut8Type, lut16Type or lutAtoBType tag whose output
// is the PCS pcs, or, for an empty pcs, the device space of a DeviceLink
// profile.
func readPipeline(b []byte, pcs string) (*pipeline, error) {
	if len(b) < 12 {
		return nil, fmt.Errorf("%w: lookup table of %d bytes", ErrMalformed, len(b))
	}
	switch string(b[:4]) {
	case "mft1", "mft2":
		return readLut(b, pcs, false)
	case "mAB ":
		return readAtoB(b, pcs)
	}
	return nil, fmt.Errorf("%w: lookup table of type %q", ErrUnsupported, b[:4])
}

// readBPipeline parses a lut8Type, lut16Type or lutBtoAType tag whose input
// is the PCS pcs: a BToA table (ICC.1:2010, 9.2.5 to 9.2.7).
func readBPipeline(b []byte, pcs string) (*pipeline, error) {
	if len(b) < 12 {
		return nil, fmt.Errorf("%w: lookup table of %d bytes", ErrMalformed, len(b))
	}
	switch string(b[:4]) {
	case "mft1", "mft2":
		return readLut(b, pcs, true)
	case "mBA ":
		return readBtoA(b, pcs)
	}
	return nil, fmt.Errorf("%w: lookup table of type %q", ErrUnsupported, b[:4])
}

// readLut parses a lut8Type or lut16Type tag (ICC.1:2010, 10.8 and 10.9). The
// matrix applies only to XYZ input (10.8), which only a BToA table, fromPCS,
// takes from the XYZ connection space: an AToB table's input is a device
// space, and a DeviceLink's matrix is the identity the clause requires.
func readLut(b []byte, pcs string, fromPCS bool) (*pipeline, error) {
	wide := string(b[:4]) == "mft2"
	if len(b) < 48 {
		return nil, fmt.Errorf("%w: lookup table header", ErrMalformed)
	}
	p := &pipeline{in: int(b[8]), out: int(b[9])}
	grid := int(b[10])
	inEntries, outEntries, pos, width := 256, 256, 48, 1
	if wide {
		if len(b) < 52 {
			return nil, fmt.Errorf("%w: lookup table header", ErrMalformed)
		}
		inEntries, outEntries = int(binary.BigEndian.Uint16(b[48:])), int(binary.BigEndian.Uint16(b[50:]))
		pos, width = 52, 2
	}
	if err := checkShape(p.in, p.out, grid); err != nil {
		return nil, err
	}
	if inEntries < 2 || outEntries < 2 {
		return nil, fmt.Errorf("%w: lookup table curves of %d and %d entries", ErrMalformed, inEntries, outEntries)
	}
	read := func(n int) ([]float64, error) {
		if pos+n*width > len(b) {
			return nil, fmt.Errorf("%w: lookup table past its tag", ErrMalformed)
		}
		v := make([]float64, n)
		for i := range v {
			if wide {
				v[i] = float64(binary.BigEndian.Uint16(b[pos+2*i:])) / 65535
			} else {
				v[i] = float64(b[pos+i]) / 255
			}
		}
		pos += n * width
		return v, nil
	}
	for range p.in {
		s, err := read(inEntries)
		if err != nil {
			return nil, err
		}
		p.a = append(p.a, sampled(s))
	}
	points := pow(grid, p.in)
	data, err := read(points * p.out)
	if err != nil {
		return nil, err
	}
	p.clut = &clut{grid: repeat(grid, p.in), out: p.out, data: data}
	for range p.out {
		s, err := read(outEntries)
		if err != nil {
			return nil, err
		}
		p.b = append(p.b, sampled(s))
	}
	if fromPCS && pcs == "XYZ " {
		var m [9]float64
		identity := true
		for i := range m {
			m[i] = s15(b[12+4*i:])
			if want := float64(btoi(i%4 == 0)); m[i] != want {
				identity = false
			}
		}
		if !identity {
			p.inMatrix = &m
		}
	}
	switch {
	case pcs == "":
		p.encoding = pcsDevice
	case pcs == "XYZ " && wide:
		p.encoding = pcsXYZ
	case pcs == "Lab " && wide:
		p.encoding = pcsLab16
	case pcs == "Lab ":
		p.encoding = pcsLab8
	default:
		return nil, fmt.Errorf("%w: 8-bit table into the XYZ connection space", ErrUnsupported)
	}
	return p, nil
}

// btoi returns 1 for true and 0 for false.
func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

// lutAB holds the five offsets of a lutAtoBType or lutBtoAType header
// (ICC.1:2010, 10.10 and 10.12), which share one layout.
type lutAB struct {
	b, matrix, m, clut, a int
}

// readLutABHeader reads the channel counts and offsets of a lutAtoBType or
// lutBtoAType tag, whose connection space encoding follows pcs.
func readLutABHeader(b []byte, pcs, kind string) (*pipeline, lutAB, error) {
	if len(b) < 32 {
		return nil, lutAB{}, fmt.Errorf("%w: %s header", ErrMalformed, kind)
	}
	p := &pipeline{in: int(b[8]), out: int(b[9]), encoding: pcsLabV4}
	switch pcs {
	case "XYZ ":
		p.encoding = pcsXYZ
	case "":
		p.encoding = pcsDevice
	}
	if p.in < 1 || p.in > maxChannels || p.out < 1 || p.out > maxOutputs {
		return nil, lutAB{}, fmt.Errorf("%w: %s of %d inputs and %d outputs", ErrMalformed, kind, p.in, p.out)
	}
	u := func(at int) int { return int(binary.BigEndian.Uint32(b[at:])) }
	return p, lutAB{b: u(12), matrix: u(16), m: u(20), clut: u(24), a: u(28)}, nil
}

// readCurves reads n curves stored one after the other from off, each padded
// to four bytes (ICC.1:2010, 10.10).
func readCurves(b []byte, off, n int, kind string) ([]curve, error) {
	var cs []curve
	for range n {
		if off <= 0 || off >= len(b) {
			return nil, fmt.Errorf("%w: %s curve past its tag", ErrMalformed, kind)
		}
		c, size, err := readCurve(b[off:])
		if err != nil {
			return nil, err
		}
		cs = append(cs, c)
		off += (size + 3) &^ 3
	}
	return cs, nil
}

// readABMatrix reads the 3 by 3 matrix and offsets of a lutAtoBType or
// lutBtoAType tag.
func readABMatrix(b []byte, off int, kind string) (*[12]float64, error) {
	if off+48 > len(b) {
		return nil, fmt.Errorf("%w: %s matrix", ErrMalformed, kind)
	}
	var m [12]float64
	for i := range m {
		m[i] = s15(b[off+4*i:])
	}
	return &m, nil
}

// readABCLUT reads the CLUT of a lutAtoBType or lutBtoAType tag: in grid
// counts, a precision byte, then the points of out values each.
func readABCLUT(b []byte, off, in, out int) (*clut, error) {
	if off+20 > len(b) {
		return nil, fmt.Errorf("%w: CLUT header", ErrMalformed)
	}
	c := &clut{out: out}
	points := 1
	for i := range in {
		g := int(b[off+i])
		if g < 2 {
			return nil, fmt.Errorf("%w: CLUT of %d points along input %d", ErrMalformed, g, i)
		}
		c.grid = append(c.grid, g)
		points *= g
		if points > maxCLUTEntries {
			return nil, fmt.Errorf("%w: CLUT of more than %d points", ErrLimit, maxCLUTEntries)
		}
	}
	precision := int(b[off+16])
	if precision != 1 && precision != 2 {
		return nil, fmt.Errorf("%w: CLUT precision %d", ErrMalformed, precision)
	}
	n := points * out
	if n > maxCLUTEntries || off+20+n*precision > len(b) {
		return nil, fmt.Errorf("%w: CLUT past its tag", ErrMalformed)
	}
	c.data = make([]float64, n)
	for i := range c.data {
		if precision == 2 {
			c.data[i] = float64(binary.BigEndian.Uint16(b[off+20+2*i:])) / 65535
		} else {
			c.data[i] = float64(b[off+20+i]) / 255
		}
	}
	return c, nil
}

// readAtoB parses a lutAtoBType tag (ICC.1:2010, 10.10).
func readAtoB(b []byte, pcs string) (*pipeline, error) {
	const kind = "lutAtoB"
	p, off, err := readLutABHeader(b, pcs, kind)
	if err != nil {
		return nil, err
	}
	if off.b == 0 {
		return nil, fmt.Errorf("%w: lutAtoB without B curves", ErrMalformed)
	}
	if p.b, err = readCurves(b, off.b, p.out, kind); err != nil {
		return nil, err
	}
	if off.a != 0 {
		if p.a, err = readCurves(b, off.a, p.in, kind); err != nil {
			return nil, err
		}
	}
	if off.m != 0 {
		if p.m, err = readCurves(b, off.m, p.out, kind); err != nil {
			return nil, err
		}
	}
	if off.matrix != 0 {
		if p.out != 3 {
			return nil, fmt.Errorf("%w: lutAtoB matrix", ErrMalformed)
		}
		if p.matrix, err = readABMatrix(b, off.matrix, kind); err != nil {
			return nil, err
		}
	}
	switch {
	case off.clut != 0:
		if p.clut, err = readABCLUT(b, off.clut, p.in, p.out); err != nil {
			return nil, err
		}
		if p.a == nil {
			return nil, fmt.Errorf("%w: lutAtoB CLUT without A curves", ErrMalformed)
		}
	case p.in != p.out:
		return nil, fmt.Errorf("%w: lutAtoB of %d inputs and %d outputs without a CLUT", ErrMalformed, p.in, p.out)
	}
	return p, nil
}

// readBtoA parses a lutBtoAType tag (ICC.1:2010, 10.12): B curves, an
// optional matrix and M curves on its input, the connection space side, then
// an optional CLUT and the A curves it requires, on the device side.
func readBtoA(b []byte, pcs string) (*pipeline, error) {
	const kind = "lutBtoA"
	p, off, err := readLutABHeader(b, pcs, kind)
	if err != nil {
		return nil, err
	}
	p.btoa = true
	if off.b == 0 {
		return nil, fmt.Errorf("%w: lutBtoA without B curves", ErrMalformed)
	}
	if p.b, err = readCurves(b, off.b, p.in, kind); err != nil {
		return nil, err
	}
	if off.m != 0 {
		if p.m, err = readCurves(b, off.m, p.in, kind); err != nil {
			return nil, err
		}
	}
	if off.matrix != 0 {
		// The matrix maps the three B curve outputs to the M curves (10.12.4).
		if p.in != 3 || p.m == nil {
			return nil, fmt.Errorf("%w: lutBtoA matrix", ErrMalformed)
		}
		if p.matrix, err = readABMatrix(b, off.matrix, kind); err != nil {
			return nil, err
		}
	}
	if off.a != 0 {
		if p.a, err = readCurves(b, off.a, p.out, kind); err != nil {
			return nil, err
		}
	}
	switch {
	case off.clut != 0:
		if p.clut, err = readABCLUT(b, off.clut, p.in, p.out); err != nil {
			return nil, err
		}
		if p.a == nil {
			return nil, fmt.Errorf("%w: lutBtoA CLUT without A curves", ErrMalformed)
		}
	case p.in != p.out:
		return nil, fmt.Errorf("%w: lutBtoA of %d inputs and %d outputs without a CLUT", ErrMalformed, p.in, p.out)
	}
	return p, nil
}

// checkShape validates the channel counts and grid of a lut8 or lut16 table.
func checkShape(in, out, grid int) error {
	if in < 1 || in > maxChannels || out < 1 || out > 15 || grid < 2 {
		return fmt.Errorf("%w: lookup table of %d inputs, %d outputs and %d grid points", ErrMalformed, in, out, grid)
	}
	points := 1
	for range in {
		points *= grid
		if points > maxCLUTEntries {
			return fmt.Errorf("%w: CLUT of more than %d points", ErrLimit, maxCLUTEntries)
		}
	}
	if points*out > maxCLUTEntries {
		return fmt.Errorf("%w: CLUT of more than %d values", ErrLimit, maxCLUTEntries)
	}
	return nil
}

// maxOutputs is the most outputs a pipeline stage has: 15 colour channels
// (ICC.1:2010, Table 19).
const maxOutputs = 15

// eval runs the pipeline past its input curves on v, which holds their
// normalized outputs, and leaves its normalized outputs there. A [Transform]
// tabulates the input curves, which only ever see a byte.
func (p *pipeline) eval(v *[maxOutputs]float64) {
	if p.clut != nil {
		p.clut.eval(v)
	}
	for i, c := range p.m {
		v[i] = c.eval(v[i])
	}
	// Values are not clipped between stages: an extended-range profile such
	// as e-sRGB moves them past 0 to 1 on purpose, and only the final encoding
	// clips.
	if m := p.matrix; m != nil {
		x, y, z := v[0], v[1], v[2]
		for i := range 3 {
			v[i] = m[3*i]*x + m[3*i+1]*y + m[3*i+2]*z + m[9+i]
		}
	}
	for i, c := range p.b {
		v[i] = c.eval(v[i])
	}
}

// eval replaces the normalized inputs in v by the table's outputs,
// interpolated tetrahedrally over three inputs, and for more linearly along
// the first between two evaluations over the rest, as LittleCMS does;
// linearly over one or two.
func (c *clut) eval(v *[maxOutputs]float64) {
	var in, out [maxOutputs]float64
	in = *v
	c.interp(in[:len(c.grid)], 0, 0, 1, &out)
	*v = out
}

// interp adds weight times the interpolated value over the inputs from dim
// on, at base offset, to out.
func (c *clut) interp(in []float64, dim, base int, weight float64, out *[maxOutputs]float64) {
	remaining := len(c.grid) - dim
	if remaining == 3 && !c.trilinear {
		c.tetrahedral(in[dim:], base, weight, out)
		return
	}
	if remaining == 0 {
		for k := range c.out {
			out[k] += weight * c.data[base+k]
		}
		return
	}
	i, f := cell(in[dim], c.grid[dim])
	stride := c.stride(dim)
	c.interp(in, dim+1, base+i*stride, weight*(1-f), out)
	if f > 0 {
		c.interp(in, dim+1, base+(i+1)*stride, weight*f, out)
	}
}

// stride returns the offset between neighbouring points along dim.
func (c *clut) stride(dim int) int {
	s := c.out
	for d := dim + 1; d < len(c.grid); d++ {
		s *= c.grid[d]
	}
	return s
}

// tetrahedral interpolates over the last three inputs from base.
func (c *clut) tetrahedral(in []float64, base int, weight float64, out *[maxOutputs]float64) {
	n := len(c.grid)
	x, rx := cell(in[0], c.grid[n-3])
	y, ry := cell(in[1], c.grid[n-2])
	z, rz := cell(in[2], c.grid[n-1])
	sx, sy, sz := c.stride(n-3), c.stride(n-2), c.stride(n-1)
	o := base + x*sx + y*sy + z*sz
	at := func(dx, dy, dz, k int) float64 { return c.data[o+dx*sx+dy*sy+dz*sz+k] }
	for k := range c.out {
		c000, c111 := at(0, 0, 0, k), at(1, 1, 1, k)
		var v float64
		switch {
		case rx >= ry && ry >= rz:
			v = c000 + rx*(at(1, 0, 0, k)-c000) + ry*(at(1, 1, 0, k)-at(1, 0, 0, k)) + rz*(c111-at(1, 1, 0, k))
		case rx >= rz && rz >= ry:
			v = c000 + rx*(at(1, 0, 0, k)-c000) + rz*(at(1, 0, 1, k)-at(1, 0, 0, k)) + ry*(c111-at(1, 0, 1, k))
		case rz >= rx && rx >= ry:
			v = c000 + rz*(at(0, 0, 1, k)-c000) + rx*(at(1, 0, 1, k)-at(0, 0, 1, k)) + ry*(c111-at(1, 0, 1, k))
		case ry >= rx && rx >= rz:
			v = c000 + ry*(at(0, 1, 0, k)-c000) + rx*(at(1, 1, 0, k)-at(0, 1, 0, k)) + rz*(c111-at(1, 1, 0, k))
		case ry >= rz && rz >= rx:
			v = c000 + ry*(at(0, 1, 0, k)-c000) + rz*(at(0, 1, 1, k)-at(0, 1, 0, k)) + rx*(c111-at(0, 1, 1, k))
		default:
			v = c000 + rz*(at(0, 0, 1, k)-c000) + ry*(at(0, 1, 1, k)-at(0, 0, 1, k)) + rx*(c111-at(0, 1, 1, k))
		}
		out[k] += weight * v
	}
}

// cell returns the grid cell a normalized input falls in and its fraction
// across the cell, keeping the cell's far corner inside a grid of g points.
func cell(v float64, g int) (int, float64) {
	x := clamp01(v) * float64(g-1)
	i := min(int(x), g-2)
	return i, x - float64(i)
}

func pow(b, e int) int {
	r := 1
	for range e {
		r *= b
	}
	return r
}

func repeat(v, n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = v
	}
	return out
}
