package effect

import "context"

// Dilate raises every channel of every pixel of s to its largest value
// within rx pixels along the row and ry along the column, in place: the
// covered area grown by a rectangle of 2rx+1 by 2ry+1 pixels (Filter
// Effects 1, 9.17 feMorphology, operator dilate). Pixels past the edge
// read as transparent black: the specification leaves them unsaid, and
// transparent black is what an edgeMode of none reads (9.14). A radius
// below 1 leaves its axis alone; one past [MaxKernelRadius] is cut there.
func Dilate(ctx context.Context, s *Surface, rx, ry int) error {
	return morph(ctx, s, rx, ry, true)
}

// Erode lowers every channel of every pixel of s to its smallest value
// within rx pixels along the row and ry along the column, in place: the
// covered area shrunk by a rectangle of 2rx+1 by 2ry+1 pixels (Filter
// Effects 1, 9.17 feMorphology, operator erode). Pixels past the edge
// read as transparent black, so coverage that reaches the edge erodes
// from it. A radius below 1 leaves its axis alone; one past
// [MaxKernelRadius] is cut there.
func Erode(ctx context.Context, s *Surface, rx, ry int) error {
	return morph(ctx, s, rx, ry, false)
}

func morph(ctx context.Context, s *Surface, rx, ry int, grow bool) error {
	rx, ry = min(max(rx, 0), MaxKernelRadius), min(max(ry, 0), MaxKernelRadius)
	if rx == 0 && ry == 0 {
		return nil
	}
	ch := int(s.F)
	n := max(s.W, s.H)
	var sc scratch
	defer sc.release()
	if err := sc.take(ctx, int64(n)*2+int64(n+2*max(rx, ry)+1)*8); err != nil {
		return err
	}
	line := make([]uint8, n)
	res := make([]uint8, n)
	q := make([]int, 0, n+2*max(rx, ry)+1)
	stride := s.stride()
	if rx > 0 {
		for y := range s.H {
			if y%64 == 0 {
				if err := ctx.Err(); err != nil {
					return err
				}
			}
			row := s.Pix[y*stride : (y+1)*stride]
			for c := range ch {
				for x := range s.W {
					line[x] = row[x*ch+c]
				}
				extreme(line[:s.W], res[:s.W], rx, grow, q)
				for x := range s.W {
					row[x*ch+c] = res[x]
				}
			}
		}
	}
	if ry > 0 {
		for x := range s.W {
			if x%64 == 0 {
				if err := ctx.Err(); err != nil {
					return err
				}
			}
			for c := range ch {
				for y := range s.H {
					line[y] = s.Pix[y*stride+x*ch+c]
				}
				extreme(line[:s.H], res[:s.H], ry, grow, q)
				for y := range s.H {
					s.Pix[y*stride+x*ch+c] = res[y]
				}
			}
		}
	}
	return nil
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
	for i := range n {
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

// DilateDisc raises every channel of every pixel of s to its largest value
// within the disc of radius r pixels about it, in place: the covered area
// grown by r in every direction, its corners rounded, where [Dilate]
// grows them square. A pixel (dx, dy) away belongs to the disc when
// dx^2 + dy^2 <= r^2. Pixels past the edge read as transparent black, as
// for [Dilate]. The radius is rounded to 1/256 pixel; one below 1 leaves
// s alone, one past [MaxKernelRadius] is cut there.
func DilateDisc(ctx context.Context, s *Surface, r float64) error {
	return morphDisc(ctx, s, r, true)
}

// ErodeDisc lowers every channel of every pixel of s to its smallest
// value within the disc of radius r pixels about it, in place: the
// covered area shrunk by r in every direction, as [DilateDisc] grows it.
// Pixels past the edge read as transparent black, so coverage that
// reaches the edge erodes from it. The radius is rounded to 1/256 pixel;
// one below 1 leaves s alone, one past [MaxKernelRadius] is cut there.
func ErodeDisc(ctx context.Context, s *Surface, r float64) error {
	return morphDisc(ctx, s, r, false)
}

// discWidths returns, for each row 0 to n away from the centre of the
// disc of radius q/256 pixels, how many pixels the disc reaches either
// side along that row: the largest w with (256w)^2 + (256dy)^2 <= q^2.
func discWidths(q int64) []int {
	n := q / sigmaUnit
	out := make([]int, n+1)
	for dy := range out {
		d := int64(dy) * sigmaUnit
		out[dy] = int(isqrt(q*q-d*d) / sigmaUnit) // fits int: at most MaxKernelRadius
	}
	return out
}

// morphDisc runs a disc's morphology as one running extreme along the
// rows a distinct row width of the disc, each row's result then folded
// into the rows its width serves: (r+1) passes of linear work.
func morphDisc(ctx context.Context, s *Surface, r float64, grow bool) error {
	q := min(quantize(r), MaxKernelRadius*sigmaUnit)
	if q < sigmaUnit {
		return nil
	}
	widths := discWidths(q)
	ch := int(s.F)
	n := max(s.W, s.H)
	plane := s.W * s.H
	var sc scratch
	defer sc.release()
	if err := sc.take(ctx, 3*int64(plane)+int64(n)*2+int64(n+2*len(widths)+1)*8); err != nil {
		return err
	}
	src := make([]uint8, plane)
	run := make([]uint8, plane)
	out := make([]uint8, plane)
	line := make([]uint8, n)
	res := make([]uint8, n)
	q2 := make([]int, 0, n+2*len(widths)+1)
	stride := s.stride()
	for c := range ch {
		for y := range s.H {
			for x := range s.W {
				src[y*s.W+x] = s.Pix[y*stride+x*ch+c]
			}
		}
		start := uint8(255)
		if grow {
			start = 0
		}
		for i := range out {
			out[i] = start
		}
		for dy, w := range widths {
			if err := ctx.Err(); err != nil {
				return err
			}
			// A width the row before shares is in run already.
			if dy == 0 || w != widths[dy-1] {
				for y := range s.H {
					row := src[y*s.W : (y+1)*s.W]
					copy(line, row)
					extreme(line[:s.W], res[:s.W], w, grow, q2)
					copy(run[y*s.W:(y+1)*s.W], res[:s.W])
				}
			}
			for y := range s.H {
				dst := out[y*s.W : (y+1)*s.W]
				for _, from := range [2]int{y - dy, y + dy} {
					if from < 0 || from >= s.H {
						if !grow {
							clear(dst)
						}
						continue
					}
					fold(dst, run[from*s.W:(from+1)*s.W], grow)
					if dy == 0 {
						break
					}
				}
			}
		}
		for y := range s.H {
			for x := range s.W {
				s.Pix[y*stride+x*ch+c] = out[y*s.W+x]
			}
		}
	}
	return nil
}

// fold sets each value of dst to the larger (grow) or the smaller of it
// and the value of src at its position.
func fold(dst, src []uint8, grow bool) {
	for i, v := range src {
		if grow {
			dst[i] = max(dst[i], v)
		} else {
			dst[i] = min(dst[i], v)
		}
	}
}
