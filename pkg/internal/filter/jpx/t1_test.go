package jpx

import (
	"errors"
	"os"
	"slices"
	"testing"
)

// blockValues decodes code-block cb of sub-band b of tc and returns its
// quantized coefficients and their decoded bit counts.
func blockValues(t *testing.T, tc *tileComponent, b *band, cb *codeBlock) ([]int64, []int, *t1) {
	t.Helper()
	dec := &t1{}
	mb := tc.magnitudeBits(b)
	if err := dec.decode(cb, b.orient, mb+tc.roi, tc.style.cbStyle); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var q []int64
	var nb []int
	for j := range (cb.x1 - cb.x0) * (cb.y1 - cb.y0) {
		v, n := dec.coefficient(j, mb, tc.roi, cb.zeroPlanes)
		q, nb = append(q, v), append(nb, n)
	}
	return q, nb, dec
}

// TestT1_AnnexJ10 decodes the two code-blocks of the J.10 codestream to the
// coefficients J.10.4 gives: -26, -22, -30, -32 and -19 in the NLLL sub-band,
// with all 9 bit-planes of Mb decoded, and 1, 5, 1 and 0 in the LH sub-band,
// with all 10.
func TestT1_AnnexJ10(t *testing.T) {
	t.Parallel()

	d := decodeTiers(t, annexJ10)[0]
	tc := &d.comps[0]
	for _, tc2 := range []struct {
		r, band int
		mb      int
		want    []int64
	}{
		{0, 0, 9, []int64{-26, -22, -30, -32, -19}},
		{1, 1, 10, []int64{1, 5, 1, 0}},
	} {
		res := &tc.resolutions[tc2.r]
		b := &res.bands[tc2.band]
		if mb := tc.magnitudeBits(b); mb != tc2.mb {
			t.Errorf("level %d band %d: Mb = %d, want %d", tc2.r, tc2.band, mb, tc2.mb)
		}
		q, nb, _ := blockValues(t, tc, b, &res.precincts[0].bands[tc2.band].blocks[0])
		if !slices.Equal(q, tc2.want) {
			t.Errorf("level %d band %d: coefficients %v, want %v", tc2.r, tc2.band, q, tc2.want)
		}
		for j, n := range nb {
			if n != tc2.mb {
				t.Errorf("level %d band %d: coefficient %d has %d bits decoded, want %d", tc2.r, tc2.band, j, n, tc2.mb)
			}
		}
	}
}

// TestT1_SegmentationSymbols decodes every code-block of codestreams OpenJPEG
// coded with segmentation symbols: modes63 with all six code-block style
// flags, and segsym, bypass_segsym and causal_segsym with symbols alone, with
// selective arithmetic coding bypass, and with vertically causal contexts,
// neither resetting contexts nor terminating each pass. Each cleanup pass ends
// with a symbol that decodes to 1010 only when every decision before it in the
// codeword segment used the right context and state (T.800, D.5), so the
// symbols check Annex D on this data. Without resets and terminations an
// error anywhere reaches every later symbol of the code-block.
func TestT1_SegmentationSymbols(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		style uint8
	}{
		{"modes63", 0x3F},
		{"segsym", styleSegmentation},
		{"bypass_segsym", styleSegmentation | styleBypass},
		{"causal_segsym", styleSegmentation | styleCausal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			data, err := os.ReadFile("testdata/" + tc.name + ".j2k")
			if err != nil {
				t.Fatal(err)
			}
			dec := &t1{}
			for _, d := range decodeTiers(t, data) {
				for c := range d.comps {
					tcomp := &d.comps[c]
					if tcomp.style.cbStyle != tc.style {
						t.Fatalf("component %d has code-block style 0x%02X, want 0x%02X", c, tcomp.style.cbStyle, tc.style)
					}
					for r := range tcomp.resolutions {
						res := &tcomp.resolutions[r]
						for k := range res.precincts {
							for i := range res.precincts[k].bands {
								b := &res.bands[i]
								for j := range res.precincts[k].bands[i].blocks {
									cb := &res.precincts[k].bands[i].blocks[j]
									if err := dec.decode(cb, b.orient, tcomp.magnitudeBits(b), tcomp.style.cbStyle); err != nil {
										t.Fatal(err)
									}
								}
							}
						}
					}
				}
			}
			if dec.segmentSymbols < 100 || dec.segmentErrors != 0 {
				t.Errorf("%d of %d segmentation symbols decoded wrong", dec.segmentErrors, dec.segmentSymbols)
			}
		})
	}
}

// TestMagnitudeBits checks Mb (T.800, E-2) under expounded quantization, one
// exponent per sub-band, and derived quantization, where a sub-band nb levels
// down from the tile-component has the exponent of NLLL less NL - nb (E-5).
func TestMagnitudeBits(t *testing.T) {
	t.Parallel()

	style := &codingStyle{levels: 3}
	expounded := &quantization{style: quantExpounded, guards: 2}
	for i := range 10 {
		expounded.steps = append(expounded.steps, step{exponent: 8 + i})
	}
	derived := &quantization{style: quantDerived, guards: 1, steps: []step{{exponent: 12, mantissa: 5}}}
	for _, tc := range []struct {
		quant *quantization
		b     band
		want  int
	}{
		{expounded, band{orient: bandLL, level: 3}, 2 + 8 - 1},
		{expounded, band{orient: bandHH, level: 1, step: 9}, 2 + 17 - 1},
		{derived, band{orient: bandLL, level: 3}, 1 + 12 - 1},
		{derived, band{orient: bandHL, level: 3, step: 1}, 1 + 12 - 1},
		{derived, band{orient: bandHH, level: 1, step: 9}, 1 + 10 - 1},
	} {
		tc2 := &tileComponent{style: style, quant: tc.quant}
		if got := tc2.magnitudeBits(&tc.b); got != tc.want {
			t.Errorf("quantization style %d, band %+v: Mb = %d, want %d", tc.quant.style, tc.b, got, tc.want)
		}
	}
}

// TestSigContexts checks one neighbourhood per row of T.800, Table D.1 in each
// of its three columns.
func TestSigContexts(t *testing.T) {
	t.Parallel()

	// flags builds a neighbourhood from counts of significant horizontal,
	// vertical and diagonal neighbours.
	flags := func(h, v, d int) int {
		f := 0
		for i, bit := range []int{sigW, sigE} {
			if i < h {
				f |= bit
			}
		}
		for i, bit := range []int{sigN, sigS} {
			if i < v {
				f |= bit
			}
		}
		for i, bit := range []int{sigNW, sigNE, sigSW, sigSE} {
			if i < d {
				f |= bit
			}
		}
		return f
	}
	for _, tc := range []struct {
		h, v, d int
		// want holds the label for LL and LH, HL, and HH.
		want [3]uint8
	}{
		{2, 0, 0, [3]uint8{8, 4, 2}},
		{1, 1, 0, [3]uint8{7, 7, 2}},
		{1, 0, 1, [3]uint8{6, 3, 4}},
		{1, 0, 0, [3]uint8{5, 3, 1}},
		{0, 2, 0, [3]uint8{4, 8, 2}},
		{0, 1, 0, [3]uint8{3, 5, 1}},
		{0, 0, 2, [3]uint8{2, 2, 6}},
		{0, 0, 1, [3]uint8{1, 1, 3}},
		{0, 0, 0, [3]uint8{0, 0, 0}},
		{0, 1, 3, [3]uint8{3, 6, 8}},
		{1, 1, 1, [3]uint8{7, 7, 5}},
	} {
		f := flags(tc.h, tc.v, tc.d)
		got := [3]uint8{sigContexts[sigTable(bandLL)][f], sigContexts[sigTable(bandHL)][f], sigContexts[sigTable(bandHH)][f]}
		if lh := sigContexts[sigTable(bandLH)][f]; got != tc.want || lh != got[0] {
			t.Errorf("H=%d V=%d D=%d: LL %d, LH %d, HL %d, HH %d; want %v", tc.h, tc.v, tc.d, got[0], lh, got[1], got[2], tc.want)
		}
	}
}

// TestSignContexts checks all nine rows of T.800, Table D.3, reaching each
// contribution of Table D.2 through two neighbours.
func TestSignContexts(t *testing.T) {
	t.Parallel()

	// pair builds the flags of two neighbours whose Table D.2 contribution is
	// c: both positive, one negative with one insignificant, or opposite.
	pair := func(c int, sig0, sig1, neg0, neg1 uint32) uint32 {
		switch c {
		case 1:
			return sig0 | sig1
		case -1:
			return sig0 | neg0
		}
		return sig0 | neg0 | sig1
	}
	for _, tc := range []struct {
		h, v        int
		label, xorb uint8
	}{
		{1, 1, 13, 0},
		{1, 0, 12, 0},
		{1, -1, 11, 0},
		{0, 1, 10, 0},
		{0, 0, 9, 0},
		{0, -1, 10, 1},
		{-1, 1, 11, 1},
		{-1, 0, 12, 1},
		{-1, -1, 13, 1},
	} {
		f := pair(tc.h, sigW, sigE, negW, negE) | pair(tc.v, sigN, sigS, negN, negS)
		if got := signContexts[signIndex(f)]; got != [2]uint8{tc.label, tc.xorb} {
			t.Errorf("H=%d V=%d: label %d XOR %d, want %d XOR %d", tc.h, tc.v, got[0], got[1], tc.label, tc.xorb)
		}
	}
}

// TestRawBits reads a raw segment across a 0xFF byte, after which the first
// bit is a stuffed 0 (T.800, D.6), then past its end, which reads as 0.
func TestRawBits(t *testing.T) {
	t.Parallel()

	r := &rawBits{data: []byte{0xFF, 0x7F, 0x80}}
	var got []int
	for range 26 {
		got = append(got, r.bit())
	}
	want := []int{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	if !slices.Equal(got, want) {
		t.Errorf("bits %v, want %v", got, want)
	}
}

// TestT1_Coefficient maps decoded magnitudes to quantized values under the
// Maxshift ROI of T.800, H.1, with Mb = 4 and s = 3 over 7 decoded planes.
func TestT1_Coefficient(t *testing.T) {
	t.Parallel()

	dec := &t1{w: 5, h: 1, stride: 7}
	dec.flags = make([]uint32, 7*3)
	dec.mag = []int64{0b1011000, 0b0000101, 0b0000101, 0b1000000, 0}
	dec.nbits = []int{7, 7, 3, 5, 7}
	dec.flags[1*7+1+1] |= flagNeg
	for j, want := range []struct {
		q  int64
		nb int
	}{
		// A bit in the first Mb planes: an ROI coefficient, shifted down.
		{0b1011, 4},
		// All zero there: a background coefficient keeps its low planes.
		{-0b101, 4},
		// Fewer than Mb planes decoded: no change but the scale.
		{0, 3},
		{0b1000, 4},
		{0, 4},
	} {
		q, nb := dec.coefficient(j, 4, 3, 0)
		if q != want.q || nb != want.nb {
			t.Errorf("coefficient %d = %d with %d bits, want %d with %d", j, q, nb, want.q, want.nb)
		}
	}
	// Without an ROI the value is the magnitude and Nb counts the zero
	// bit-planes too.
	if q, nb := dec.coefficient(0, 7, 0, 2); q != 0b1011000 || nb != 9 {
		t.Errorf("no ROI: %d with %d bits, want %d with 9", q, nb, 0b1011000)
	}
}

// TestT1_TooManyPasses refuses more coding passes than the coded bit-planes
// allow (T.800, D.3): one cleanup pass, then three per bit-plane.
func TestT1_TooManyPasses(t *testing.T) {
	t.Parallel()

	cb := &codeBlock{x1: 4, y1: 4, zeroPlanes: 1, passes: 5, segments: []codewordSegment{{passes: 5, data: []byte{0}}}}
	if err := (&t1{}).decode(cb, bandLL, 3, 0); !errors.Is(err, ErrMalformed) {
		t.Errorf("decode = %v, want %v", err, ErrMalformed)
	}
	cb.passes, cb.segments[0].passes = 4, 4
	if err := (&t1{}).decode(cb, bandLL, 3, 0); err != nil {
		t.Errorf("decode of 4 passes over 2 coded bit-planes = %v", err)
	}
}
