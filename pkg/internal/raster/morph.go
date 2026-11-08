package raster

import "image"

// Dilate returns m with every pixel raised to the largest coverage within
// r pixels along each axis: the covered area grown by a square of side
// 2r+1. Pixels past the edge are uncovered. A radius below 1 returns a
// copy; one above [MaxKernelRadius] is clipped to it.
func Dilate(m *image.Alpha, r int) *image.Alpha {
	return morph(m, r, true)
}

// Erode returns m with every pixel lowered to the smallest coverage within
// r pixels along each axis: the covered area shrunk by a square of side
// 2r+1. Pixels past the edge are uncovered, so coverage that reaches the
// edge erodes from it. A radius below 1 returns a copy; one above
// [MaxKernelRadius] is clipped to it.
func Erode(m *image.Alpha, r int) *image.Alpha {
	return morph(m, r, false)
}

func morph(m *image.Alpha, r int, grow bool) *image.Alpha {
	out := clone(m)
	if r < 1 {
		return out
	}
	r = min(r, MaxKernelRadius)
	w, h := out.Bounds().Dx(), out.Bounds().Dy()
	line := make([]uint8, max(w, h))
	res := make([]uint8, max(w, h))
	q := make([]int, 0, max(w, h)+2*r+1)
	for y := 0; y < h; y++ {
		row := out.Pix[y*out.Stride : y*out.Stride+w]
		copy(line, row)
		extreme(line[:w], res[:w], r, grow, q)
		copy(row, res[:w])
	}
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			line[y] = out.Pix[y*out.Stride+x]
		}
		extreme(line[:h], res[:h], r, grow, q)
		for y := 0; y < h; y++ {
			out.Pix[y*out.Stride+x] = res[y]
		}
	}
	return out
}

// extreme writes into dst the running maximum (grow) or minimum of src
// over the window [i-r, i+r], positions past either end reading 0, by a
// monotonic queue of positions in linear time.
func extreme(src, dst []uint8, r int, grow bool, q []int) {
	n := len(src)
	at := func(i int) uint8 {
		if i < 0 || i >= n {
			return 0
		}
		return src[i]
	}
	better := func(a, b uint8) bool {
		if grow {
			return a >= b
		}
		return a <= b
	}
	q = q[:0]
	head := 0
	next := -r
	for i := 0; i < n; i++ {
		for ; next <= i+r; next++ {
			v := at(next)
			for len(q) > head && better(v, at(q[len(q)-1])) {
				q = q[:len(q)-1]
			}
			q = append(q, next)
		}
		for q[head] < i-r {
			head++
		}
		dst[i] = at(q[head])
	}
}

// Threshold sets every covered pixel of m, however faintly, to fully
// covered, in place: the silhouette of an antialiased coverage.
func Threshold(m *image.Alpha) {
	for i, v := range m.Pix {
		if v != 0 {
			m.Pix[i] = 255
		}
	}
}

// Multiply scales the coverage of m by that of n, pixel by pixel, in
// place: the intersection of two coverages. The masks share a size;
// pixels of m outside n are left as they are.
func Multiply(m, n *image.Alpha) {
	b := m.Bounds().Intersect(n.Bounds())
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			i, j := m.PixOffset(x, y), n.PixOffset(x, y)
			m.Pix[i] = uint8((uint32(m.Pix[i])*uint32(n.Pix[j]) + 127) / 255) //nolint:gosec // a product of two bytes over 255 is a byte
		}
	}
}
