package imagetest

import (
	"encoding/binary"
	"image"
	"image/color"
)

// boolEncoder is the boolean entropy encoder of VP8 (RFC 6386, 7.3).
type boolEncoder struct {
	out      []byte
	rng      uint32
	bottom   uint32
	bitCount int
}

func newBoolEncoder() *boolEncoder {
	return &boolEncoder{rng: 255, bitCount: 24}
}

// addOne propagates a carry into the bytes written.
func (e *boolEncoder) addOne() {
	i := len(e.out) - 1
	for i >= 0 && e.out[i] == 255 {
		e.out[i] = 0
		i--
	}
	if i >= 0 {
		e.out[i]++
	}
}

// put writes the bit v with probability prob/256 of being 0.
func (e *boolEncoder) put(prob uint8, v bool) {
	split := 1 + (e.rng-1)*uint32(prob)>>8
	if v {
		e.bottom += split
		e.rng -= split
	} else {
		e.rng = split
	}
	for e.rng < 128 {
		e.rng <<= 1
		if e.bottom&(1<<31) != 0 {
			e.addOne()
		}
		e.bottom <<= 1
		e.bitCount--
		if e.bitCount == 0 {
			e.out = append(e.out, byte(e.bottom>>24))
			e.bottom &= 1<<24 - 1
			e.bitCount = 8
		}
	}
}

// zeros writes an n-bit literal of value 0, each bit at probability 128
// (RFC 6386, 8.2, L(n)).
func (e *boolEncoder) zeros(n int) {
	for range n {
		e.put(128, false)
	}
}

// flush writes out what the encoder holds (RFC 6386, 7.3).
func (e *boolEncoder) flush() []byte {
	c := e.bitCount
	v := e.bottom
	if v&(1<<uint(32-c)) != 0 {
		e.addOne()
	}
	v <<= uint(c & 7)
	c >>= 3
	for c--; c >= 0; c-- {
		v <<= 8
	}
	for range 4 {
		e.out = append(e.out, byte(v>>24))
		v <<= 8
	}
	return e.out
}

// vp8Bands maps a coefficient's position to its band (RFC 6386, 13.3).
var vp8Bands = [17]int{0, 1, 2, 3, 6, 4, 5, 6, 6, 6, 6, 6, 6, 6, 6, 7, 0}

// The extra bits of the token categories and their probabilities (RFC
// 6386, 13.2).
var vp8CatProbs = [6][]uint8{
	{159},
	{165, 145},
	{173, 148, 140},
	{176, 155, 140, 135},
	{180, 157, 141, 134, 130},
	{254, 254, 243, 230, 196, 177, 153, 140, 133, 130, 129},
}

// vp8CatBase is the smallest value of each token category.
var vp8CatBase = [6]int{5, 7, 11, 19, 35, 67}

// The planes of the token probabilities (RFC 6386, 13.3).
const (
	vp8PlaneY  = 0 // Y after Y2
	vp8PlaneY2 = 1
	vp8PlaneUV = 2
)

// putDCBlock writes a block whose only coefficient is its first, v, with
// the token probabilities of plane in context ctx (RFC 6386, 13.2), and
// returns whether the block holds a token other than its end of block.
func putDCBlock(e *boolEncoder, plane, ctx, v int) int {
	p := vp8DefaultProbs[plane][vp8Bands[0]][ctx]
	if v == 0 {
		e.put(p[0], false)
		return 0
	}
	e.put(p[0], true)
	e.put(p[1], true)
	a := v
	if a < 0 {
		a = -a
	}
	next := 2
	switch {
	case a == 1:
		e.put(p[2], false)
		next = 1
	case a <= 4:
		e.put(p[2], true)
		e.put(p[3], false)
		if a == 2 {
			e.put(p[4], false)
		} else {
			e.put(p[4], true)
			e.put(p[5], a == 4)
		}
	default:
		e.put(p[2], true)
		e.put(p[3], true)
		cat := 0
		for cat < 5 && a >= vp8CatBase[cat+1] {
			cat++
		}
		switch cat {
		case 0, 1:
			e.put(p[6], false)
			e.put(p[7], cat == 1)
		case 2, 3:
			e.put(p[6], true)
			e.put(p[8], false)
			e.put(p[9], cat == 3)
		default:
			e.put(p[6], true)
			e.put(p[8], true)
			e.put(p[10], cat == 5)
		}
		extra := a - vp8CatBase[cat]
		probs := vp8CatProbs[cat]
		for i, pr := range probs {
			e.put(pr, extra>>uint(len(probs)-1-i)&1 != 0)
		}
	}
	e.put(128, v < 0)
	// The end of block at the second position.
	e.put(vp8DefaultProbs[plane][vp8Bands[1]][next][0], false)
	return 1
}

// VP8 returns img as a lossy VP8 key frame (RFC 6386) in its simplest
// coding: every macroblock predicted DC_PRED, its luma one level a
// macroblock and its chroma one level a 4 by 4 block, at the finest
// quantizer, with no loop filter. The picture is blocky, its colours
// close to the source's: a lossy file whose decoding is exact by the
// decoder's own arithmetic.
func VP8(img image.Image) []byte {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	mw, mh := (w+15)/16, (h+15)/16
	// The source planes at macroblock size, edges repeated.
	yw, cw := mw*16, mw*8
	srcY := make([]int, yw*mh*16)
	srcU := make([]int, cw*mh*8)
	srcV := make([]int, cw*mh*8)
	for y := range mh * 16 {
		for x := range yw {
			c := color.NRGBAModel.Convert(img.At(b.Min.X+min(x, w-1), b.Min.Y+min(y, h-1))).(color.NRGBA) //nolint:forcetypeassert // NRGBAModel returns color.NRGBA
			yy, cb, cr := color.RGBToYCbCr(c.R, c.G, c.B)
			srcY[y*yw+x] = int(yy)
			srcU[(y/2)*cw+x/2] += int(cb)
			srcV[(y/2)*cw+x/2] += int(cr)
		}
	}
	for i := range srcU {
		srcU[i] = (srcU[i] + 2) / 4
		srcV[i] = (srcV[i] + 2) / 4
	}
	recY := make([]int, len(srcY))
	recU := make([]int, len(srcU))
	recV := make([]int, len(srcV))

	first := newBoolEncoder()
	first.zeros(1) // colour space
	first.zeros(1) // clamping type
	first.zeros(1) // no segmentation
	first.zeros(1) // filter type
	first.zeros(6) // loop filter level 0
	first.zeros(3) // sharpness
	first.zeros(1) // no loop filter deltas
	first.zeros(2) // one token partition
	first.zeros(7) // y_ac_qi 0
	first.zeros(5) // no quantizer delta
	first.zeros(1) // refresh_entropy_probs
	for i := range vp8UpdateProbs {
		for j := range vp8UpdateProbs[i] {
			for k := range vp8UpdateProbs[i][j] {
				for l := range vp8UpdateProbs[i][j][k] {
					first.put(vp8UpdateProbs[i][j][k][l], false)
				}
			}
		}
	}
	first.zeros(1) // mb_no_coeff_skip: every macroblock codes its tokens

	tokens := newBoolEncoder()
	upY2 := make([]int, mw)
	upU := make([]int, mw*2)
	upV := make([]int, mw*2)
	for my := range mh {
		leftY2 := 0
		leftU := [2]int{}
		leftV := [2]int{}
		for mx := range mw {
			// DC_PRED for luma (key frame tree, RFC 6386, 11.2) and
			// chroma.
			first.put(145, true)
			first.put(156, false)
			first.put(163, false)
			first.put(142, false)

			pred := predDC(recY, yw, mx*16, my*16, 16)
			var sum int
			for y := range 16 {
				for x := range 16 {
					sum += srcY[(my*16+y)*yw+mx*16+x]
				}
			}
			r := (sum+128)/256 - pred
			for y := range 16 {
				for x := range 16 {
					recY[(my*16+y)*yw+mx*16+x] = clamp255(pred + r)
				}
			}
			// Y2's DC: dequantized by 8, then the inverse WHT and the
			// inverse DCT each divide by 8 (RFC 6386, 14.3 and 14.4).
			nz := putDCBlock(tokens, vp8PlaneY2, leftY2+upY2[mx], 8*r)
			leftY2, upY2[mx] = nz, nz
			for range 16 {
				// A Y block after Y2 starts at its second coefficient,
				// none of them coded.
				tokens.put(vp8DefaultProbs[vp8PlaneY][vp8Bands[1]][0][0], false)
			}
			for plane, pl := range [2]struct {
				src, rec []int
				up       []int
				left     *[2]int
			}{{srcU, recU, upU, &leftU}, {srcV, recV, upV, &leftV}} {
				_ = plane
				cpred := predDC(pl.rec, cw, mx*8, my*8, 8)
				for by := range 2 {
					for bx := range 2 {
						var s int
						for y := range 4 {
							for x := range 4 {
								s += pl.src[(my*8+by*4+y)*cw+mx*8+bx*4+x]
							}
						}
						cr := (s+8)/16 - cpred
						for y := range 4 {
							for x := range 4 {
								pl.rec[(my*8+by*4+y)*cw+mx*8+bx*4+x] = clamp255(cpred + cr)
							}
						}
						// Chroma's DC is dequantized by 4, the inverse DCT
						// divides by 8.
						n := putDCBlock(tokens, vp8PlaneUV, pl.left[by]+pl.up[mx*2+bx], 2*cr)
						pl.left[by], pl.up[mx*2+bx] = n, n
					}
				}
			}
		}
	}
	part1 := first.flush()
	part2 := tokens.flush()
	tag := uint32(0) | 0<<1 | 1<<4 | uint32(len(part1))<<5                      //nolint:gosec // G115: a test frame's partition
	out := []byte{byte(tag), byte(tag >> 8), byte(tag >> 16), 0x9d, 0x01, 0x2a} //nolint:gosec // G115: the three bytes of a 24-bit frame tag
	out = binary.LittleEndian.AppendUint16(out, uint16(w))                      //nolint:gosec // G115: a 14-bit width
	out = binary.LittleEndian.AppendUint16(out, uint16(h))                      //nolint:gosec // G115: a 14-bit height
	out = append(out, part1...)
	return append(out, part2...)
}

// predDC returns the DC_PRED prediction of the n by n block of the plane
// rec (stride w) at x, y (RFC 6386, 12.2): the mean of the row above and
// the column left of it, of one of them at the picture's edge, else 128.
func predDC(rec []int, w, x, y, n int) int {
	sum, count := 0, 0
	if y > 0 {
		for i := range n {
			sum += rec[(y-1)*w+x+i]
		}
		count += n
	}
	if x > 0 {
		for i := range n {
			sum += rec[(y+i)*w+x-1]
		}
		count += n
	}
	if count == 0 {
		return 128
	}
	return (sum + count/2) / count
}

func clamp255(v int) int {
	return min(max(v, 0), 255)
}
