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
