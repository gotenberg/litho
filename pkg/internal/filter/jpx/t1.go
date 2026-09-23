package jpx

import (
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/filter/mq"
)

// maxPlanes bounds the magnitude bit-planes of a code-block, Mb plus the ROI
// shift (T.800, E-2 and H-3), to what an int64 magnitude holds. It is an
// implementation limit, not spec-mandated: T.800 allows 37 bit-planes and an
// ROI shift of 255, but a 16-bit image needs about 19.
const maxPlanes = 63

// Code-block style flags of T.800, Table A.19 that tier-1 decoding reads, in
// addition to styleBypass and styleTerminateAll.
const (
	styleReset        = 0x02
	styleCausal       = 0x08
	styleSegmentation = 0x20
)

// Context labels of T.800, Annex D: 0 to 8 for significance (Table D.1), 9 to
// 13 for signs (Table D.3), 14 to 16 for magnitude refinement (Table D.4),
// then the run-length and UNIFORM contexts.
const (
	ctxRunLength = 17
	ctxUniform   = 18
	numContexts  = 19
)

// Coefficient state flags. The low byte holds the significance of the eight
// neighbours, the next four bits the sign of the four direct neighbours, and
// the rest the coefficient's own state.
const (
	sigN = 1 << iota
	sigS
	sigW
	sigE
	sigNW
	sigNE
	sigSW
	sigSE
	negN
	negS
	negW
	negE
	// flagSig marks a significant coefficient and flagNeg a negative one.
	flagSig
	flagNeg
	// flagVisited marks a coefficient coded in this bit-plane's significance
	// propagation pass, which the refinement and cleanup passes then skip.
	flagVisited
	// flagRefined marks a coefficient that has had a refinement bit.
	flagRefined

	neighbours = sigN | sigS | sigW | sigE | sigNW | sigNE | sigSW | sigSE
	// below holds the neighbours of the next stripe, which vertically causal
	// context formation treats as insignificant (T.800, D.7).
	below = sigS | sigSW | sigSE | negS
)

// sigTable returns the index into sigContexts for a sub-band orientation.
func sigTable(orient int) int {
	switch orient {
	case bandHL:
		return 1
	case bandHH:
		return 2
	}
	return 0
}

// sigContexts maps the neighbour significance byte to a context label for LL
// and LH, HL, and HH sub-bands, per T.800, Table D.1.
var sigContexts = func() [3][256]uint8 {
	var t [3][256]uint8
	for f := range 256 {
		h := f>>2&1 + f>>3&1
		v := f&1 + f>>1&1
		d := f>>4&1 + f>>5&1 + f>>6&1 + f>>7&1
		t[0][f] = sigLabel(h, v, d)
		// HL is high-pass across: Table D.1 swaps the roles of H and V.
		t[1][f] = sigLabel(v, h, d)
		t[2][f] = diagonalLabel(h+v, d)
	}
	return t
}()

// sigLabel is the LL and LH column of T.800, Table D.1.
func sigLabel(h, v, d int) uint8 {
	switch {
	case h == 2:
		return 8
	case h == 1 && v >= 1:
		return 7
	case h == 1 && d >= 1:
		return 6
	case h == 1:
		return 5
	case v == 2:
		return 4
	case v == 1:
		return 3
	case d >= 2:
		return 2
	case d == 1:
		return 1
	}
	return 0
}

// diagonalLabel is the HH column of T.800, Table D.1.
func diagonalLabel(hv, d int) uint8 {
	switch {
	case d >= 3:
		return 8
	case d == 2 && hv >= 1:
		return 7
	case d == 2:
		return 6
	case d == 1 && hv >= 2:
		return 5
	case d == 1 && hv == 1:
		return 4
	case d == 1:
		return 3
	case hv >= 2:
		return 2
	case hv == 1:
		return 1
	}
	return 0
}

// signContexts maps the significance and sign bits of the four direct
// neighbours to a sign context label and the bit its decision is XORed with,
// per T.800, Tables D.2 and D.3.
var signContexts = func() [256][2]uint8 {
	var t [256][2]uint8
	// contribution is one line of Table D.2 for two neighbours.
	contribution := func(sig0, neg0, sig1, neg1 int) int {
		c := 0
		if sig0 == 1 {
			c += 1 - 2*neg0
		}
		if sig1 == 1 {
			c += 1 - 2*neg1
		}
		return max(-1, min(1, c))
	}
	for f := range 256 {
		// Bits 0 to 3 are sigN, sigS, sigW, sigE; bits 4 to 7 negN, negS,
		// negW, negE, as in the flag layout shifted down by 4 for the signs.
		v := contribution(f&1, f>>4&1, f>>1&1, f>>5&1)
		h := contribution(f>>2&1, f>>6&1, f>>3&1, f>>7&1)
		label, xor := 0, 0
		switch {
		case h == 1:
			label = 12 + v
		case h == 0 && v == 0:
			label = 9
		case h == 0:
			label = 10
			if v < 0 {
				xor = 1
			}
		default:
			label, xor = 12-v, 1
		}
		t[f] = [2]uint8{uint8(label), uint8(xor)}
	}
	return t
}()

// signIndex packs the sign-relevant bits of a flag word into the index of
// signContexts.
func signIndex(f uint32) uint32 {
	return f&(sigN|sigS|sigW|sigE) | (f>>8&0xF)<<4
}

// t1 decodes code-blocks (T.800, Annex D). It keeps its buffers between
// code-blocks, so one t1 serves many blocks in turn.
type t1 struct {
	w, h   int
	stride int
	// flags is the state of each coefficient on a grid padded by one on every
	// side, so neighbours of edge coefficients need no bounds checks.
	flags []uint32
	// mag holds each coefficient's magnitude bits decoded so far, bit p for
	// bit-plane p, and nbits how many bit-planes each has had decoded.
	mag   []int64
	nbits []int
	cx    [numContexts]mq.Context
	// segmentSymbols counts the segmentation symbols (T.800, D.5) decoded, and
	// segmentErrors those that were not 1010, which reveals corrupt data.
	segmentSymbols, segmentErrors int
}

// resetContexts puts every context in its initial state (T.800, Table D.7).
func (t *t1) resetContexts() {
	for i := range t.cx {
		t.cx[i] = mq.Context{}
	}
	t.cx[0] = mq.NewContext(4)
	t.cx[ctxRunLength] = mq.NewContext(3)
	t.cx[ctxUniform] = mq.NewContext(46)
}

// symbols supplies the decisions of one codeword segment: from the MQ
// decoder, or raw bits in the passes selective arithmetic coding bypass
// leaves uncoded (T.800, D.6).
type symbols struct {
	mq  *mq.Decoder
	raw *rawBits
}

func (s *symbols) decide(cx *mq.Context) int {
	if s.raw != nil {
		return s.raw.bit()
	}
	return s.mq.Decode(cx)
}

// rawBits reads the bits of a raw segment: most significant first, and only
// seven from a byte that follows 0xFF (T.800, D.6). Past the end of the
// segment it reads 0, so a truncated raw pass leaves the remaining
// coefficients as they were.
type rawBits struct {
	data   []byte
	pos    int
	cur    byte
	left   int
	lastFF bool
}

func (r *rawBits) bit() int {
	if r.left == 0 {
		if r.pos >= len(r.data) {
			return 0
		}
		r.cur = r.data[r.pos]
		r.pos++
		r.left = 8
		if r.lastFF {
			r.left = 7
		}
		r.lastFF = r.cur == 0xFF
	}
	r.left--
	return int(r.cur>>r.left) & 1
}

// Coding pass kinds, T.800 D.3.
const (
	passSignificance = iota
	passRefinement
	passCleanup
)

// passKind returns the kind of coding pass p: the first is a cleanup pass,
// then significance propagation, refinement and cleanup repeat (T.800, D.3).
func passKind(p int) int {
	if p == 0 {
		return passCleanup
	}
	return (p - 1) % 3
}

// decode decodes the coding passes of cb, coded with planes magnitude
// bit-planes, of which the first cb.zeroPlanes are zero (T.800, B.10.5 and
// D.3). orient selects the significance contexts of Table D.1.
func (t *t1) decode(cb *codeBlock, orient, planes int, cbStyle uint8) error {
	t.w, t.h = cb.x1-cb.x0, cb.y1-cb.y0
	t.stride = t.w + 2
	n := t.w * t.h
	t.flags = resize(t.flags, t.stride*(t.h+2))
	t.mag = resize(t.mag, n)
	t.nbits = resize(t.nbits, n)
	if cb.passes == 0 {
		return nil
	}
	if planes > maxPlanes {
		return fmt.Errorf("%w: code-block of %d magnitude bit-planes", ErrUnsupported, planes)
	}
	coded := planes - cb.zeroPlanes
	// D.3: one cleanup pass for the first coded bit-plane, three for each
	// after it.
	if coded < 1 || cb.passes > 3*coded-2 {
		return fmt.Errorf("%w: %d coding passes for %d coded bit-planes", ErrMalformed, cb.passes, coded)
	}
	t.resetContexts()
	p := 0
	for _, seg := range cb.segments {
		s := &symbols{}
		if cbStyle&styleBypass != 0 && seg.first >= 10 && passKind(seg.first) != passCleanup {
			s.raw = &rawBits{data: seg.data}
		} else {
			s.mq = mq.NewDecoder(seg.data)
		}
		for range seg.passes {
			plane := coded - 1 - (p+2)/3
			nbits := coded - plane
			switch passKind(p) {
			case passSignificance:
				t.significance(s, orient, plane, nbits, cbStyle)
			case passRefinement:
				t.refinement(s, plane, nbits, cbStyle)
			default:
				t.cleanup(s, orient, plane, nbits, cbStyle)
				if cbStyle&styleSegmentation != 0 {
					v := 0
					for range 4 {
						v = v<<1 | s.decide(&t.cx[ctxUniform])
					}
					t.segmentSymbols++
					if v != 0xA {
						t.segmentErrors++
					}
				}
			}
			if cbStyle&styleReset != 0 {
				t.resetContexts()
			}
			p++
		}
	}
	return nil
}

// resize returns buf with length n, zeroed.
func resize[T any](buf []T, n int) []T {
	if cap(buf) < n {
		return make([]T, n)
	}
	buf = buf[:n]
	clear(buf)
	return buf
}

// neighbourFlags returns the flag word of the coefficient at padded index i in
// row y, with the next stripe's neighbours cleared under vertically causal
// context formation (T.800, D.7).
func (t *t1) neighbourFlags(i, y int, cbStyle uint8) uint32 {
	f := t.flags[i]
	if cbStyle&styleCausal != 0 && y%4 == 3 {
		f &^= below
	}
	return f
}

// becomeSignificant marks the coefficient at padded index i significant with
// the given sign and records it in its neighbours' flags.
func (t *t1) becomeSignificant(i int, negative bool) {
	s := t.stride
	f := t.flags
	f[i] |= flagSig
	if negative {
		f[i] |= flagNeg
		f[i-s] |= negS
		f[i+s] |= negN
		f[i-1] |= negE
		f[i+1] |= negW
	}
	f[i-s-1] |= sigSE
	f[i-s] |= sigS
	f[i-s+1] |= sigSW
	f[i-1] |= sigE
	f[i+1] |= sigW
	f[i+s-1] |= sigNE
	f[i+s] |= sigN
	f[i+s+1] |= sigNW
}

// decodeSign decodes the sign of a newly significant coefficient (T.800,
// D.3.2), or reads it raw in a bypassed pass (D-2).
func (t *t1) decodeSign(s *symbols, f uint32) bool {
	if s.raw != nil {
		return s.raw.bit() == 1
	}
	c := signContexts[signIndex(f)]
	return s.mq.Decode(&t.cx[c[0]])^int(c[1]) == 1
}

// significance is the significance propagation pass over bit-plane plane
// (T.800, D.3.1).
func (t *t1) significance(s *symbols, orient, plane, nbits int, cbStyle uint8) {
	ctx := &sigContexts[sigTable(orient)]
	for y0 := 0; y0 < t.h; y0 += 4 {
		for x := range t.w {
			for y := y0; y < min(y0+4, t.h); y++ {
				i := (y+1)*t.stride + x + 1
				f := t.neighbourFlags(i, y, cbStyle)
				if f&flagSig != 0 || f&neighbours == 0 {
					continue
				}
				t.flags[i] |= flagVisited
				j := y*t.w + x
				t.nbits[j] = nbits
				if s.decide(&t.cx[ctx[f&neighbours]]) == 1 {
					t.mag[j] |= 1 << plane
					t.becomeSignificant(i, t.decodeSign(s, f))
				}
			}
		}
	}
}

// refinement is the magnitude refinement pass over bit-plane plane (T.800,
// D.3.3).
func (t *t1) refinement(s *symbols, plane, nbits int, cbStyle uint8) {
	for y0 := 0; y0 < t.h; y0 += 4 {
		for x := range t.w {
			for y := y0; y < min(y0+4, t.h); y++ {
				i := (y+1)*t.stride + x + 1
				f := t.neighbourFlags(i, y, cbStyle)
				if f&flagSig == 0 || f&flagVisited != 0 {
					continue
				}
				label := 16
				if f&flagRefined == 0 {
					label = 14
					if f&neighbours != 0 {
						label = 15
					}
				}
				j := y*t.w + x
				t.nbits[j] = nbits
				t.mag[j] |= int64(s.decide(&t.cx[label])) << plane
				t.flags[i] |= flagRefined
			}
		}
	}
}

// cleanup is the cleanup pass over bit-plane plane (T.800, D.3.4), after which
// no coefficient counts as visited.
func (t *t1) cleanup(s *symbols, orient, plane, nbits int, cbStyle uint8) {
	ctx := &sigContexts[sigTable(orient)]
	for y0 := 0; y0 < t.h; y0 += 4 {
		for x := range t.w {
			start := y0
			// Table D.5: a full column of four coefficients that are all left
			// to this pass with context 0 is run-length coded.
			if y0+4 <= t.h && t.runnable(x, y0, cbStyle) {
				if s.decide(&t.cx[ctxRunLength]) == 0 {
					for y := y0; y < y0+4; y++ {
						t.nbits[y*t.w+x] = nbits
					}
					continue
				}
				r := s.decide(&t.cx[ctxUniform]) << 1
				r |= s.decide(&t.cx[ctxUniform])
				for y := y0; y < y0+r; y++ {
					t.nbits[y*t.w+x] = nbits
				}
				y := y0 + r
				i := (y+1)*t.stride + x + 1
				j := y*t.w + x
				t.nbits[j] = nbits
				t.mag[j] |= 1 << plane
				t.becomeSignificant(i, t.decodeSign(s, t.neighbourFlags(i, y, cbStyle)))
				start = y + 1
			}
			for y := start; y < min(y0+4, t.h); y++ {
				i := (y+1)*t.stride + x + 1
				f := t.neighbourFlags(i, y, cbStyle)
				if f&(flagSig|flagVisited) != 0 {
					continue
				}
				j := y*t.w + x
				t.nbits[j] = nbits
				if s.decide(&t.cx[ctx[f&neighbours]]) == 1 {
					t.mag[j] |= 1 << plane
					t.becomeSignificant(i, t.decodeSign(s, f))
				}
			}
		}
	}
	for i := range t.flags {
		t.flags[i] &^= flagVisited
	}
}

// runnable reports whether the four coefficients of column x from row y0 are
// all insignificant, uncoded in this bit-plane, and without significant
// neighbours.
func (t *t1) runnable(x, y0 int, cbStyle uint8) bool {
	for y := y0; y < y0+4; y++ {
		i := (y+1)*t.stride + x + 1
		if t.neighbourFlags(i, y, cbStyle)&(flagSig|flagVisited|neighbours) != 0 {
			return false
		}
	}
	return true
}

// coefficient returns the quantized value q of coefficient j (T.800, E-1) and
// the number of its magnitude bits decoded, Nb (D.2.1), with the Maxshift ROI
// of H.1 undone. mb is Mb (E-2), roi the RGN shift s, and zeroPlanes P.
func (t *t1) coefficient(j, mb, roi, zeroPlanes int) (int64, int) {
	mag := t.mag[j]
	nb := zeroPlanes + t.nbits[j]
	if roi > 0 {
		switch {
		case nb < mb || mag>>roi != 0:
			// H.1 steps 2 and 3: a coefficient with a bit in its first Mb
			// most significant bit-planes belongs to the ROI and those planes
			// are its value.
			mag >>= roi
			nb = min(nb, mb)
		default:
			// H.1 step 4: a background coefficient is its low planes.
			nb = max(0, nb-roi)
		}
	}
	i := (j/t.w+1)*t.stride + j%t.w + 1
	if t.flags[i]&flagNeg != 0 {
		mag = -mag
	}
	return mag, nb
}
