package jpx

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// bitString is a bitSource over a string of '0' and '1' that fails once
// exhausted, so a test sees exactly how many bits a decoder asked for.
type bitString struct {
	bits string
	pos  int
}

func (b *bitString) bit() (int, error) {
	if b.pos >= len(b.bits) {
		return 0, errTruncated
	}
	b.pos++
	return int(b.bits[b.pos-1] - '0'), nil
}

// packBits packs a string of '0' and '1' into packet header bytes, most
// significant bit first, stuffing a 0 bit after every 0xFF byte (T.800,
// B.10.1) and padding the last byte with zeros.
func packBits(s string) []byte {
	var out []byte
	var cur byte
	n, limit := 0, 8
	for _, c := range strings.ReplaceAll(s, " ", "") {
		cur <<= 1
		if c == '1' {
			cur |= 1
		}
		n++
		if n == limit {
			out = append(out, cur)
			limit = 8
			if cur == 0xFF {
				limit = 7
			}
			cur, n = 0, 0
		}
	}
	if n > 0 {
		out = append(out, cur<<(limit-n))
	}
	return out
}

// TestTagTree_AnnexB12Note decodes the three leaves the NOTE of T.800,
// B.10.2 walks through, in a tree of four levels over a 6x3 array: 01111
// codes q3(0,0) = 1, then 001 codes q3(1,0) = 3 above the known path, then
// 101 codes q2(1,0) = 1 and q3(2,0) = 2.
func TestTagTree_AnnexB12Note(t *testing.T) {
	t.Parallel()

	tree := newTagTree(6, 3)
	if len(tree.levels) != 4 {
		t.Fatalf("a 6x3 tag tree has %d levels, want 4", len(tree.levels))
	}
	for _, tc := range []struct {
		x, bits string
		want    int
	}{
		{"0", "01111", 1},
		{"1", "001", 3},
		{"2", "101", 2},
	} {
		src := &bitString{bits: tc.bits}
		x := int(tc.x[0] - '0')
		v, ok, err := tree.decode(src, x, 0, 1<<30)
		if err != nil || !ok || v != tc.want || src.pos != len(tc.bits) {
			t.Errorf("leaf (%d,0) = %d, %v, %v after %d of %d bits, want %d", x, v, ok, err, src.pos, len(tc.bits), tc.want)
		}
	}
}

// TestTagTree_Thresholds decodes one leaf of value 2 a question at a time, as
// code-block inclusion does over successive layers (T.800, B.10.4): below 1?
// no. Below 2? no. Below 3? yes, and it is 2. Each answer costs only the bits
// the previous ones did not settle.
func TestTagTree_Thresholds(t *testing.T) {
	t.Parallel()

	tree := newTagTree(1, 1)
	src := &bitString{bits: "001"}
	for threshold, want := range []bool{false, false, false, true} {
		if threshold == 0 {
			continue
		}
		v, ok, err := tree.decode(src, 0, 0, threshold)
		if err != nil || ok != want || (ok && v != 2) {
			t.Errorf("threshold %d: %d, %v, %v; want %v", threshold, v, ok, err, want)
		}
		if src.pos != threshold {
			t.Errorf("threshold %d read %d bits in all, want %d", threshold, src.pos, threshold)
		}
	}
}

// passCodeword codes a number of coding passes as in T.800, Table B.4.
func passCodeword(n int) string {
	switch {
	case n == 1:
		return "0"
	case n == 2:
		return "10"
	case n <= 5:
		return fmt.Sprintf("11%02b", n-3)
	case n <= 36:
		return fmt.Sprintf("1111%05b", n-6)
	default:
		return fmt.Sprintf("111111111%07b", n-37)
	}
}

// TestPassCount reads every codeword of T.800, Table B.4, 1 to 164 passes.
func TestPassCount(t *testing.T) {
	t.Parallel()

	for n := 1; n <= 164; n++ {
		code := passCodeword(n)
		r := &headerReader{data: packBits(code + "0")}
		got, err := r.passCount()
		if err != nil || got != n {
			t.Errorf("codeword %s = %d, %v; want %d", code, got, err, n)
		}
	}
}

// TestReadContribution_LengthExample reads the four contributions of the
// example in T.800, B.10.7.1 NOTE 1: 6, 31, 44 and 134 bytes over 1, 9, 2 and
// 5 passes, with Lblock rising from 3 to 5 then 6 as the lengths outgrow it.
func TestReadContribution_LengthExample(t *testing.T) {
	t.Parallel()

	cb := &codeBlock{lblock: 3}
	for _, tc := range []struct {
		passes     int
		signalling string
		length     int
		lblock     int
	}{
		{1, "0 110", 6, 3},
		{9, "0 011111", 31, 3},
		{2, "110 101100", 44, 5},
		{5, "10 10000110", 134, 6},
	} {
		r := &headerReader{data: packBits(passCodeword(tc.passes) + tc.signalling)}
		c, err := readContribution(r, cb, 0)
		if err != nil {
			t.Fatalf("%d passes: %v", tc.passes, err)
		}
		if want := [][2]int{{tc.length, tc.passes}}; !slices.Equal(c.pieces, want) || cb.lblock != tc.lblock {
			t.Errorf("%d passes: pieces %v, Lblock %d; want %v, %d", tc.passes, c.pieces, cb.lblock, want, tc.lblock)
		}
		cb.passes += tc.passes
	}
}

// TestReadContribution_SegmentExample reads the example of T.800, B.10.7.2
// NOTE: with selective arithmetic coding bypass, a packet carrying the
// cleanup pass of bit-plane 4 through the significance propagation pass of
// bit-plane 6 signals four lengths, one per terminated pass plus the last,
// over 1, 2, 1 and 1 passes.
func TestReadContribution_SegmentExample(t *testing.T) {
	t.Parallel()

	cb := &codeBlock{lblock: 3, passes: 9}
	bits := passCodeword(5) + "111110" + "00000110" + "001001011" + "10000110" + "11000000"
	c, err := readContribution(&headerReader{data: packBits(bits)}, cb, styleBypass)
	if err != nil {
		t.Fatalf("readContribution: %v", err)
	}
	if want := [][2]int{{6, 1}, {75, 2}, {134, 1}, {192, 1}}; !slices.Equal(c.pieces, want) || cb.lblock != 8 {
		t.Errorf("pieces %v, Lblock %d; want %v, 8", c.pieces, cb.lblock, want)
	}
	// The first nine passes arrived earlier in one open segment. Pass 9 ends
	// it, passes 10 and 11 form a raw segment, pass 12 an arithmetic one, and
	// pass 13 opens a raw segment later packets extend.
	cb.segments = []codewordSegment{{first: 0, passes: 9, data: []byte{1}}}
	for _, piece := range c.pieces {
		addPiece(cb, []byte{byte(piece[0])}, piece[1], styleBypass)
	}
	want := []codewordSegment{
		{first: 0, passes: 10, data: []byte{1, 6}, closed: true},
		{first: 10, passes: 2, data: []byte{75}, closed: true},
		{first: 12, passes: 1, data: []byte{134}, closed: true},
		{first: 13, passes: 1, data: []byte{192}},
	}
	if !slices.EqualFunc(cb.segments, want, func(a, b codewordSegment) bool {
		return a.first == b.first && a.passes == b.passes && a.closed == b.closed && bytes.Equal(a.data, b.data)
	}) || cb.passes != 14 {
		t.Errorf("segments %+v after %d passes, want %+v after 14", cb.segments, cb.passes, want)
	}
}

// TestTerminates checks the termination patterns of T.800, Tables D.8 and D.9
// for the first sixteen passes.
func TestTerminates(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		style uint8
		want  string
	}{
		{0, "0000000000000000"},
		{styleTerminateAll, "1111111111111111"},
		// Passes 0 to 9 are arithmetic coded and end at the fourth cleanup
		// pass; from pass 10 each raw significance propagation pass runs on
		// into its terminated refinement pass, then a terminated cleanup.
		{styleBypass, "0000000001011011"},
		{styleBypass | styleTerminateAll, "1111111111111111"},
	} {
		var got strings.Builder
		for p := range 16 {
			if terminates(p, tc.style) {
				got.WriteByte('1')
			} else {
				got.WriteByte('0')
			}
		}
		if got.String() != tc.want {
			t.Errorf("style 0x%02X: %s, want %s", tc.style, got.String(), tc.want)
		}
	}
}

// TestHeaderReader_Stuffing reads across a 0xFF byte, after which only seven
// bits count (T.800, B.10.1), and aligns after a header whose last byte is
// 0xFF, which then owns the byte holding the stuffed bit.
func TestHeaderReader_Stuffing(t *testing.T) {
	t.Parallel()

	r := &headerReader{data: []byte{0xFF, 0x55, 0xAA}}
	v, err := r.bits(15)
	if err != nil || v != 0x7FD5 {
		t.Errorf("15 bits across 0xFF = 0x%X, %v; want 0x7FD5", v, err)
	}
	r = &headerReader{data: []byte{0xFF, 0x00, 0x42}}
	if _, err := r.bits(3); err != nil {
		t.Fatal(err)
	}
	if err := r.align(); err != nil || r.pos != 2 {
		t.Errorf("align after 0xFF left pos %d, %v; want 2", r.pos, err)
	}
}

// decodeTiers runs tier-2 decoding over every tile of a codestream and returns
// the tile decoders.
func decodeTiers(t *testing.T, data []byte) []*tileDecoder {
	t.Helper()
	stream, _, err := readFile(data)
	if err != nil {
		t.Fatalf("readFile: %v", err)
	}
	cs, err := parseCodestream(stream)
	if err != nil {
		t.Fatalf("parseCodestream: %v", err)
	}
	budget := &blockBudget{}
	var out []*tileDecoder
	for _, tl := range cs.tiles {
		if tl == nil {
			continue
		}
		d, err := cs.newTileDecoder(tl, budget)
		if err != nil {
			t.Fatalf("tile %d: %v", tl.index, err)
		}
		if err := d.decodePackets(context.Background(), cs); err != nil {
			t.Fatalf("tile %d: %v", tl.index, err)
		}
		out = append(out, d)
	}
	return out
}

// TestDecodePackets_AnnexJ10 reads the two packets of the J.10 codestream as
// Tables J.20 and J.21 decode them: the NLLL code-block with 3 zero
// bit-planes and 16 passes in 6 bytes, then the LH code-block, the only one
// of resolution level 1, with 7 zero bit-planes and 7 passes in 3 bytes.
func TestDecodePackets_AnnexJ10(t *testing.T) {
	t.Parallel()

	d := decodeTiers(t, annexJ10)[0]
	res := d.comps[0].resolutions
	ll := res[0].precincts[0].bands[0].blocks
	if len(ll) != 1 {
		t.Fatalf("%d NLLL code-blocks, want 1", len(ll))
	}
	checkBlock(t, "NLLL", &ll[0], 3, 16, annexJ10Data[3:9])
	bands := res[1].precincts[0].bands
	if len(bands[0].blocks) != 0 || len(bands[2].blocks) != 0 || len(bands[1].blocks) != 1 {
		t.Fatalf("resolution level 1 has %d, %d and %d code-blocks, want 0, 1 and 0", len(bands[0].blocks), len(bands[1].blocks), len(bands[2].blocks))
	}
	checkBlock(t, "LH", &bands[1].blocks[0], 7, 7, annexJ10Data[13:16])
	if d.truncated || d.pos != len(annexJ10Data) {
		t.Errorf("read %d of %d bytes, truncated %v", d.pos, len(annexJ10Data), d.truncated)
	}
}

// checkBlock checks a code-block that holds one codeword segment.
func checkBlock(t *testing.T, name string, cb *codeBlock, zeroPlanes, passes int, data []byte) {
	t.Helper()
	if cb.zeroPlanes != zeroPlanes || cb.passes != passes || len(cb.segments) != 1 || !bytes.Equal(cb.segments[0].data, data) || cb.segments[0].passes != passes {
		t.Errorf("%s code-block: %d zero bit-planes, %d passes, segments %+v; want %d, %d, % X", name, cb.zeroPlanes, cb.passes, cb.segments, zeroPlanes, passes, data)
	}
}

// TestDecodePackets_Truncated cuts the J.10 tile two bytes into the body of
// its last packet. T.800, B.11 allows that, so the tile decodes and the LH
// code-block keeps the byte that arrived.
func TestDecodePackets_Truncated(t *testing.T) {
	t.Parallel()

	b := slices.Clone(annexJ10[:96])
	binary.BigEndian.PutUint32(b[74:], 28)
	b = append(b, 0xFF, 0xD9)
	d := decodeTiers(t, b)[0]
	if !d.truncated {
		t.Errorf("tile not marked truncated")
	}
	lh := &d.comps[0].resolutions[1].precincts[0].bands[1].blocks[0]
	checkBlock(t, "LH", lh, 7, 7, annexJ10Data[13:14])
}

// TestDecodePackets_Consumes decodes the packets of codestreams made by
// another encoder, OpenJPEG 2.5.4's opj_compress, and checks that each tile
// reads every packet its progression names and exactly the bytes it holds. A
// misread header field desynchronises everything after it, so exact
// consumption is a strong check. The recipes, all from a 23x19 RGB image or
// three components subsampled 1x1, 2x2 and 2x2 (24x20 for rpcl_sub and
// cprl_sub, 23x19 for the offset streams):
//
//	tiles_sop_eph   -SOP -EPH -TP R -t 12,10 -r 8,2,1 -n 3
//	rpcl_sub        -p RPCL -c [8,8],[4,4] -n 3 -r 3,1 -mct 0
//	pcrl_offsets    -d 7,5 -T 3,2 -t 12,10 -p PCRL -c [8,8],[4,4] -n 3 -r 3,1
//	cprl_sub        -p CPRL -c [4,4] -n 2 -mct 0
//	modes63         -M 63 -n 3 -r 5,1
//	bypass          -M 1 -n 2 -r 4,1
//	pcrl_sub_offsets  -d 11,2 -T 1,0 -t 16,12 -p PCRL -c [8,8] -n 3 -r 3,1 -mct 0
//	rpcl_sub_offsets  -d 11,2 -T 1,0 -t 16,12 -p RPCL -c [8,8] -n 3 -r 3,1 -mct 0
//	poc_rlcp_cprl   -t 12,10 -r 16,4,1 -n 4 -POC T0=0,0,3,2,3,RLCP/T0=2,0,3,4,3,CPRL/...
//	poc_tiles       the same POC given for tiles 1 and 2, which opj_compress
//	                writes for tiles 0 and 1
//
// The two offset streams have tiles whose first precincts start before the
// tile's edge at resolution levels whose precincts span different distances
// on the reference grid, so the position orders depend on where the loops of
// B.12.1 first reach those precincts.
//
// tiles_sop_eph_ppt and tiles_sop_eph_ppm carry the packet headers of
// tiles_sop_eph in PPT and PPM marker segments, given out of index order and
// split between packets; opj_decompress decodes all three to the same pixels.
func TestDecodePackets_Consumes(t *testing.T) {
	t.Parallel()

	names, err := filepath.Glob("testdata/*.j2k")
	if err != nil || len(names) < 10 {
		t.Fatalf("found %d codestreams, %v", len(names), err)
	}
	for _, name := range names {
		t.Run(filepath.Base(name), func(t *testing.T) {
			t.Parallel()

			data, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			for _, d := range decodeTiers(t, data) {
				read, slots := 0, 0
				for _, c := range d.comps {
					for _, r := range c.resolutions {
						for _, p := range r.precincts {
							read += p.layers
							slots += d.layers
						}
					}
				}
				if d.truncated || read != slots || d.pos != len(d.t.data) || (d.t.packetHeaders != nil && d.headerPos != len(d.t.packetHeaders)) {
					t.Errorf("tile %d: read %d of %d packets, %d of %d bytes, %d of %d header bytes, truncated %v",
						d.t.index, read, slots, d.pos, len(d.t.data), d.headerPos, len(d.t.packetHeaders), d.truncated)
				}
			}
		})
	}
}

// TestDecodePackets_PacketHeadersApart checks that packet headers carried in
// PPT or PPM marker segments deliver the same code-block data as the same
// headers in the bit stream (T.800, A.7.4 and A.7.5).
func TestDecodePackets_PacketHeadersApart(t *testing.T) {
	t.Parallel()

	summary := func(name string) string {
		data, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		var b strings.Builder
		for _, d := range decodeTiers(t, data) {
			for _, c := range d.comps {
				for _, r := range c.resolutions {
					for _, p := range r.precincts {
						for _, pb := range p.bands {
							for _, cb := range pb.blocks {
								fmt.Fprintf(&b, "%d %d %d", d.t.index, cb.zeroPlanes, cb.passes)
								for _, s := range cb.segments {
									fmt.Fprintf(&b, " %d:%x", s.passes, s.data)
								}
								b.WriteByte('\n')
							}
						}
					}
				}
			}
		}
		return b.String()
	}
	want := summary("tiles_sop_eph.j2k")
	for _, name := range []string{"tiles_sop_eph_ppt.j2k", "tiles_sop_eph_ppm.j2k"} {
		if got := summary(name); got != want {
			t.Errorf("%s delivers different code-block data from tiles_sop_eph.j2k", name)
		}
	}
}

// TestProgression_Volumes walks the example of T.800, B.12.2 NOTE and Figure
// B.14 on one component of three resolution levels and three layers: a first
// volume in resolution-layer order over levels 0 and 1 and layer 0, then a
// second in layer-resolution order over everything, which reads only the
// packets the first did not.
func TestProgression_Volumes(t *testing.T) {
	t.Parallel()

	comps := []tileComponent{{resolutions: make([]resolution, 3)}}
	for r := range comps[0].resolutions {
		comps[0].resolutions[r].precincts = make([]precinct, 1)
	}
	var got []string
	w := &progression{
		ctx: context.Background(), comps: comps, dx: []int{1}, dy: []int{1},
		visit: func(s packetSlot) error {
			got = append(got, fmt.Sprintf("l%dr%d", s.layer, s.r))
			return nil
		},
	}
	for _, v := range []progressionChange{
		{resEnd: 2, compEnd: 1, layerEnd: 1, order: orderRLCP},
		{resEnd: 3, compEnd: 1, layerEnd: 3, order: orderLRCP},
	} {
		if err := w.run(v); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"l0r0", "l0r1", "l0r2", "l1r0", "l1r1", "l1r2", "l2r0", "l2r1", "l2r2"}
	if !slices.Equal(got, want) {
		t.Errorf("packets %v, want %v", got, want)
	}
}

// TestDecodePackets_StepLimit gives one precinct 65 535 empty packets, then a
// POC of thousands of volumes that each walk all of them again. Every
// repeated slot is skipped at no cost in data, so only maxPacketSteps stops
// the walk.
func TestDecodePackets_StepLimit(t *testing.T) {
	t.Parallel()

	cod := []byte{0, 0, 0xFF, 0xFF, 0, 0, 4, 4, 0, 1}
	var poc []byte
	for range 9000 {
		poc = append(poc, 0, 0, 0xFF, 0xFF, 1, 1, 0)
	}
	main := slices.Concat(seg(markerSIZ, sizBody(1, 1, 1, 1, 1)...), seg(markerCOD, cod...), seg(markerQCD, 0x40, 0x40), seg(markerPOC, poc...))
	b := codestreamOf(main, tilePart(0, 0, nil, make([]byte, 0xFFFF)))
	if _, err := Decode(context.Background(), b, DefaultMaxSamples); !errors.Is(err, ErrLimit) {
		t.Errorf("Decode = %v, want %v", err, ErrLimit)
	}
}

// TestDecodePackets_PositionStepLimit gives each of 8192 one-sample precincts
// one packet, then thousands of position-order volumes that walk every
// precinct again with nothing left to read. The walk costs no data, so only
// maxPacketSteps stops it.
func TestDecodePackets_PositionStepLimit(t *testing.T) {
	t.Parallel()

	// One layer, no levels, maximal code-blocks, precincts of 2^0 samples.
	cod := []byte{0x01, 0, 0, 1, 0, 0, 4, 4, 0, 1, 0x00}
	var poc []byte
	for range 9000 {
		poc = append(poc, 0, 0, 0, 1, 1, 1, orderPCRL)
	}
	main := slices.Concat(seg(markerSIZ, sizBody(8192, 1, 8192, 1, 1)...), seg(markerCOD, cod...), seg(markerQCD, 0x40, 0x40), seg(markerPOC, poc...))
	b := codestreamOf(main, tilePart(0, 0, nil, make([]byte, 8192)))
	if _, err := Decode(context.Background(), b, DefaultMaxSamples); !errors.Is(err, ErrLimit) {
		t.Errorf("Decode = %v, want %v", err, ErrLimit)
	}
}

// TestDecodePackets_MissingEPH refuses a packet header without the EPH marker
// its COD requires (T.800, A.8.2).
func TestDecodePackets_MissingEPH(t *testing.T) {
	t.Parallel()

	b := slices.Clone(annexJ10)
	b[58] |= 0x04
	if _, err := Decode(context.Background(), b, DefaultMaxSamples); !errors.Is(err, ErrMalformed) {
		t.Errorf("Decode = %v, want %v", err, ErrMalformed)
	}
}

// TestNewTileDecoder_Precedence checks the precedence of T.800, A.6: a
// tile-part COC over a tile-part COD over a main COC over the main COD, the
// same order for QCC and QCD, a tile-part RGN over the main one, and a
// tile-part POC over the main one. A tile-part COD overrides even the main
// COC of component 1.
func TestNewTileDecoder_Precedence(t *testing.T) {
	t.Parallel()

	codWith := func(levels byte) []byte { return []byte{0, 0, 0, 1, 0, levels, 4, 4, 0, 1} }
	cocWith := func(c, levels byte) []byte { return []byte{c, 0, levels, 4, 4, 0, 1} }
	// No quantization, 13 step sizes (enough for four levels), told apart by
	// the first exponent.
	quantWith := func(first byte) []byte { return append([]byte{0x40, first << 3}, make([]byte, 12)...) }
	main := slices.Concat(
		seg(markerSIZ, sizBody(16, 16, 16, 16, 3)...),
		seg(markerCOD, codWith(1)...), seg(markerCOC, cocWith(1, 2)...),
		seg(markerQCD, quantWith(1)...), seg(markerQCC, append([]byte{1}, quantWith(2)...)...),
		seg(markerRGN, 0, 0, 5),
		seg(markerPOC, 0, 0, 0, 1, 33, 3, orderLRCP),
	)
	withTile := slices.Concat(
		seg(markerCOD, 0, orderRLCP, 0, 2, 0, 3, 4, 4, 0, 1), seg(markerCOC, cocWith(2, 4)...),
		seg(markerQCD, quantWith(3)...), seg(markerQCC, append([]byte{2}, quantWith(4)...)...),
		seg(markerRGN, 1, 0, 6),
		seg(markerPOC, 0, 0, 0, 9, 33, 0, orderRPCL),
	)
	for _, tc := range []struct {
		name     string
		headers  []byte
		levels   [3]int
		exponent [3]int
		roi      [3]int
		volume   progressionChange
	}{
		{
			"main header only", nil,
			[3]int{1, 2, 1},
			[3]int{1, 2, 1},
			[3]int{5, 0, 0},
			progressionChange{layerEnd: 1, resEnd: 3, compEnd: 3, order: orderLRCP},
		},
		{
			"tile-part header", withTile,
			[3]int{3, 3, 4},
			[3]int{3, 3, 4},
			[3]int{5, 6, 0},
			progressionChange{layerEnd: 2, resEnd: 5, compEnd: 3, order: orderRPCL},
		},
	} {
		cs, err := parseCodestream(codestreamOf(main, tilePart(0, 0, tc.headers, nil)))
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		d, err := cs.newTileDecoder(cs.tiles[0], &blockBudget{})
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		for c := range 3 {
			tcomp := &d.comps[c]
			if tcomp.style.levels != tc.levels[c] || tcomp.quant.steps[0].exponent != tc.exponent[c] || tcomp.roi != tc.roi[c] {
				t.Errorf("%s: component %d has %d levels, first exponent %d, ROI shift %d; want %d, %d, %d",
					tc.name, c, tcomp.style.levels, tcomp.quant.steps[0].exponent, tcomp.roi, tc.levels[c], tc.exponent[c], tc.roi[c])
			}
		}
		if len(d.volumes) != 1 || d.volumes[0] != tc.volume {
			t.Errorf("%s: volumes %+v, want [%+v]", tc.name, d.volumes, tc.volume)
		}
	}
}
