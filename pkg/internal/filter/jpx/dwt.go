package jpx

// Lifting parameters of the 9-7 irreversible filter, T.800 Table F.4.
const (
	liftAlpha = -1.586134342059924
	liftBeta  = -0.052980118572961
	liftGamma = 0.882911075530934
	liftDelta = 0.443506852043971
	liftK     = 1.230174104914001
)

// extension is the number of samples 1D_EXTR adds on each side: the largest
// minimum of T.800, Tables F.2 and F.3, which is 2 for the 5-3 filter and 4
// for the 9-7 one. Extending further changes nothing (F.3.7).
const extension = 4

// pseo is PSEO, the periodic symmetric extension of T.800, Equation (F-4): the
// index inside [i0, i1) whose value extends index i, for i1 - i0 >= 2.
func pseo(i, i0, i1 int) int {
	period := 2 * (i1 - i0 - 1)
	m := (i - i0) % period
	if m < 0 {
		m += period
	}
	return i0 + min(m, period-m)
}

// floorHalf returns floor(v / 2).
func floorHalf(v int) int {
	return v >> 1
}

// sr53 is 1D_SR with the 5-3 reversible filter (T.800, F.3.6 to F.3.8.1): it
// reconstructs the signal of indices [i0, i0 + len(s)) in place. ext is
// scratch space of at least len(s) + 2*extension values.
func sr53(s []int32, i0 int, ext []int32) {
	n := len(s)
	if n == 0 {
		return
	}
	if n == 1 {
		if i0&1 == 1 {
			s[0] /= 2
		}
		return
	}
	i1 := i0 + n
	// ext[j] holds index i0 - extension + j.
	e := ext[:n+2*extension]
	for j := range e {
		e[j] = s[pseo(i0-extension+j, i0, i1)-i0]
	}
	at := func(i int) *int32 { return &e[i-i0+extension] }
	// F-5 on the even indices, then F-6 on the odd ones.
	for k := floorHalf(i0); k < floorHalf(i1)+1; k++ {
		*at(2 * k) -= (*at(2*k - 1) + *at(2*k + 1) + 2) >> 2
	}
	for k := floorHalf(i0); k < floorHalf(i1); k++ {
		*at(2*k + 1) += (*at(2 * k) + *at(2*k + 2)) >> 1
	}
	copy(s, e[extension:extension+n])
}

// sr97 is 1D_SR with the 9-7 irreversible filter (T.800, F.3.6 to F.3.8.2):
// it reconstructs the signal of indices [i0, i0 + len(s)) in place. ext is
// scratch space of at least len(s) + 2*extension values.
func sr97(s []float32, i0 int, ext []float32) {
	n := len(s)
	if n == 0 {
		return
	}
	if n == 1 {
		if i0&1 == 1 {
			s[0] /= 2
		}
		return
	}
	i1 := i0 + n
	e := ext[:n+2*extension]
	for j := range e {
		e[j] = s[pseo(i0-extension+j, i0, i1)-i0]
	}
	at := func(i int) *float32 { return &e[i-i0+extension] }
	h0, h1 := floorHalf(i0), floorHalf(i1)
	// The six steps of Equation (F-7), each over the range F.3.8.2 gives it.
	for k := h0 - 1; k < h1+2; k++ {
		*at(2 * k) *= liftK
	}
	for k := h0 - 2; k < h1+2; k++ {
		*at(2*k + 1) *= 1 / liftK
	}
	for k := h0 - 1; k < h1+2; k++ {
		*at(2 * k) -= liftDelta * (*at(2*k - 1) + *at(2*k + 1))
	}
	for k := h0 - 1; k < h1+1; k++ {
		*at(2*k + 1) -= liftGamma * (*at(2 * k) + *at(2*k + 2))
	}
	for k := h0; k < h1+1; k++ {
		*at(2 * k) -= liftBeta * (*at(2*k - 1) + *at(2*k + 1))
	}
	for k := h0; k < h1; k++ {
		*at(2*k + 1) -= liftAlpha * (*at(2 * k) + *at(2*k + 2))
	}
	copy(s, e[extension:extension+n])
}

// plane holds the coefficients of one tile-component in the arrangement the
// inverse transformation works on in place: the NLLL sub-band at the top
// left, and each level's HL, LH and HH sub-bands beside, below and diagonal
// to the level below it. ints or floats hold the values, row by row, for the
// reversible and irreversible paths.
type plane struct {
	stride int
	ints   []int32
	floats []float32
}

// reconstructLevel is 2D_SR (T.800, F.3.2) for one resolution level whose
// rectangle is [x0, x1) x [y0, y1), held at the top left of p with the level
// below it, lw x lh coefficients, in the corner. 2D_INTERLEAVE (F.3.3) puts
// low-pass coefficients at even coordinates and high-pass ones at odd; here
// each row, then each column, is interleaved into scratch space, filtered by
// 1D_SR, and written back in order, which is HOR_SR (F.3.4) then VER_SR
// (F.3.5) on the interleaved array.
func reconstructLevel(p *plane, x0, y0, x1, y1, lw, lh int) {
	w, h := x1-x0, y1-y0
	n := max(w, h)
	if p.ints != nil {
		line := make([]int32, n)
		ext := make([]int32, n+2*extension)
		for y := range h {
			row := p.ints[y*p.stride : y*p.stride+w]
			spread(row, line[:w], x0, lw)
			sr53(line[:w], x0, ext)
			copy(row, line[:w])
		}
		col := make([]int32, h)
		for x := range w {
			for y := range h {
				col[y] = p.ints[y*p.stride+x]
			}
			spread(col, line[:h], y0, lh)
			sr53(line[:h], y0, ext)
			for y := range h {
				p.ints[y*p.stride+x] = line[y]
			}
		}
		return
	}
	line := make([]float32, n)
	ext := make([]float32, n+2*extension)
	for y := range h {
		row := p.floats[y*p.stride : y*p.stride+w]
		spread(row, line[:w], x0, lw)
		sr97(line[:w], x0, ext)
		copy(row, line[:w])
	}
	col := make([]float32, h)
	for x := range w {
		for y := range h {
			col[y] = p.floats[y*p.stride+x]
		}
		spread(col, line[:h], y0, lh)
		sr97(line[:h], y0, ext)
		for y := range h {
			p.floats[y*p.stride+x] = line[y]
		}
	}
}

// spread interleaves src, its nlow low-pass coefficients followed by its
// high-pass ones, into dst for a signal starting at index i0: even indices
// take low-pass coefficients in order, odd ones high-pass (F.3.3).
func spread[T int32 | float32](src, dst []T, i0, nlow int) {
	lo, hi := 0, nlow
	for j := range dst {
		if (i0+j)&1 == 0 {
			dst[j] = src[lo]
			lo++
		} else {
			dst[j] = src[hi]
			hi++
		}
	}
}
