package jpx

import (
	"bytes"
	"encoding/binary"
	"errors"
	"slices"
	"testing"
)

// annexJ10 is the codestream of T.800, J.10: one 1x9 component of 8-bit
// samples, one tile, one level of the 5-3 transform. J.10.5 gives its samples
// as 101, 103, 104, 105, 96, 97, 96, 102, 109.
var annexJ10 = []byte{
	0xFF, 0x4F, 0xFF, 0x51, 0x00, 0x29, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x09,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x09,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x07, 0x01, 0x01, 0xFF, 0x5C, 0x00,
	0x07, 0x40, 0x40, 0x48, 0x48, 0x50, 0xFF, 0x52, 0x00, 0x0C, 0x00, 0x00, 0x00, 0x01, 0x00, 0x01,
	0x04, 0x04, 0x00, 0x01, 0xFF, 0x90, 0x00, 0x0A, 0x00, 0x00, 0x00, 0x00, 0x00, 0x1E, 0x00, 0x01,
	0xFF, 0x93, 0xC7, 0xD4, 0x0C, 0x01, 0x8F, 0x0D, 0xC8, 0x75, 0x5D, 0xC0, 0x7C, 0x21, 0x80, 0x0F,
	0xB1, 0x76, 0xFF, 0xD9,
}

// annexJ10Data is the bit-stream data of the J.10 tile-part, J.10.2.
var annexJ10Data = annexJ10[82:98]

// TestParseCodestream_AnnexJ10 reads the main and tile-part headers of the
// J.10 codestream as J.10.1 and J.10.2 decode them.
func TestParseCodestream_AnnexJ10(t *testing.T) {
	t.Parallel()

	cs, err := parseCodestream(annexJ10)
	if err != nil {
		t.Fatalf("parseCodestream: %v", err)
	}
	if cs.x0 != 0 || cs.y0 != 0 || cs.x1 != 1 || cs.y1 != 9 || cs.tileW != 1 || cs.tileH != 9 || len(cs.tiles) != 1 {
		t.Errorf("geometry = %+v", cs)
	}
	if want := []component{{precision: 8, dx: 1, dy: 1}}; !slices.Equal(cs.components, want) {
		t.Errorf("components = %+v, want %+v", cs.components, want)
	}
	// J.10.1: 2 guard bits, no quantization, exponents {8, 9, 9, 10}.
	q := cs.main.qcd
	if want := []step{{exponent: 8}, {exponent: 9}, {exponent: 9}, {exponent: 10}}; q.style != quantNone || q.guards != 2 || !slices.Equal(q.steps, want) {
		t.Errorf("QCD = %+v", q)
	}
	// J.10.1: LRCP, one layer, no transformation, one level, 64x64
	// code-blocks, 5-3 filter, maximal precincts.
	cod := cs.main.cod
	if cod.order != 0 || cod.layers != 1 || cod.transformed || cod.sop || cod.eph {
		t.Errorf("COD = %+v", cod)
	}
	s := cod.style
	if s.levels != 1 || s.cbw != 6 || s.cbh != 6 || s.cbStyle != 0 || !s.reversible || !slices.Equal(s.ppx, []int{15, 15}) || !slices.Equal(s.ppy, []int{15, 15}) {
		t.Errorf("coding style = %+v", s)
	}
	tl := cs.tiles[0]
	if tl == nil || tl.parts != 1 || !bytes.Equal(tl.data, annexJ10Data) || tl.packetHeaders != nil {
		t.Errorf("tile = %+v", tl)
	}
}

// seg builds a marker segment: marker, length, then body.
func seg(marker uint16, body ...byte) []byte {
	b := binary.BigEndian.AppendUint16(nil, marker)
	b = binary.BigEndian.AppendUint16(b, uint16(len(body)+2))
	return append(b, body...)
}

// sizBody builds SIZ parameters for 8-bit unsigned components of the given
// count on a width x height grid in tiles of tileW x tileH.
func sizBody(width, height, tileW, tileH uint32, components int) []byte {
	b := []byte{0, 0}
	for _, v := range []uint32{width, height, 0, 0, tileW, tileH, 0, 0} {
		b = binary.BigEndian.AppendUint32(b, v)
	}
	b = binary.BigEndian.AppendUint16(b, uint16(components))
	for range components {
		b = append(b, 7, 1, 1)
	}
	return b
}

// codBody is a COD for LRCP, one layer, no transformation, one level, 64x64
// code-blocks and the 5-3 filter.
var codBody = []byte{0, 0, 0, 1, 0, 1, 4, 4, 0, 1}

// qcdBody is a QCD for no quantization with four step sizes.
var qcdBody = []byte{0x40, 0x40, 0x48, 0x48, 0x50}

// tilePart builds a tile-part: SOT with an exact Psot, the header segments,
// SOD and data.
func tilePart(index uint16, part byte, headers []byte, data []byte) []byte {
	length := 12 + len(headers) + 2 + len(data)
	b := binary.BigEndian.AppendUint16(nil, markerSOT)
	b = binary.BigEndian.AppendUint16(b, 10)
	b = binary.BigEndian.AppendUint16(b, index)
	b = binary.BigEndian.AppendUint32(b, uint32(length))
	b = append(b, part, 0)
	b = append(b, headers...)
	b = binary.BigEndian.AppendUint16(b, markerSOD)
	return append(b, data...)
}

// codestreamOf joins SOC, a main header and tile-parts, then EOC.
func codestreamOf(main []byte, parts ...[]byte) []byte {
	b := append([]byte{0xFF, 0x4F}, main...)
	for _, p := range parts {
		b = append(b, p...)
	}
	return append(b, 0xFF, 0xD9)
}

// TestParseCodestream_TileParts interleaves the tile-parts of two tiles, as
// T.800, B.11 allows, and checks that each tile gathers its data, its PPT
// packet headers in Zppt order, and the POC volumes of all its headers.
func TestParseCodestream_TileParts(t *testing.T) {
	t.Parallel()

	main := slices.Concat(seg(markerSIZ, sizBody(8, 4, 4, 4, 1)...), seg(markerCOD, codBody...), seg(markerQCD, qcdBody...))
	poc1 := []byte{0, 0, 0, 1, 1, 1, 0}
	// CEpoc 0 stands for 256 (T.800, Table A.32).
	poc2 := []byte{1, 0, 0, 1, 2, 0, 1}
	b := codestreamOf(main,
		tilePart(1, 0, slices.Concat(seg(markerPPT, 1, 0xB1), seg(markerPPT, 0, 0xA1, 0xA2), seg(markerPOC, poc1...)), []byte{1, 2}),
		tilePart(0, 0, nil, []byte{9}),
		tilePart(1, 1, slices.Concat(seg(markerPPT, 0, 0xC1), seg(0xFF64, 0, 1, 'x'), seg(markerPOC, poc2...)), []byte{3}),
		tilePart(0, 1, []byte{0xFF, 0x30}, []byte{8, 7}),
	)
	cs, err := parseCodestream(b)
	if err != nil {
		t.Fatalf("parseCodestream: %v", err)
	}
	t0, t1 := cs.tiles[0], cs.tiles[1]
	if t0.parts != 2 || !bytes.Equal(t0.data, []byte{9, 8, 7}) || t0.packetHeaders != nil {
		t.Errorf("tile 0 = %+v", t0)
	}
	if t1.parts != 2 || !bytes.Equal(t1.data, []byte{1, 2, 3}) || !bytes.Equal(t1.packetHeaders, []byte{0xA1, 0xA2, 0xB1, 0xC1}) {
		t.Errorf("tile 1 = %+v", t1)
	}
	want := []progressionChange{
		{resStart: 0, compStart: 0, layerEnd: 1, resEnd: 1, compEnd: 1, order: 0},
		{resStart: 1, compStart: 0, layerEnd: 1, resEnd: 2, compEnd: 256, order: 1},
	}
	if !slices.Equal(t1.header.poc, want) {
		t.Errorf("tile 1 POC = %+v, want %+v", t1.header.poc, want)
	}
}

// TestParseCodestream_PPM spreads the packet headers of three tile-parts over
// two PPM segments given out of Zppm order, with one tile-part's headers split
// across the segment boundary, as T.800, A.7.4 allows.
func TestParseCodestream_PPM(t *testing.T) {
	t.Parallel()

	// In Zppm order the list is (2: A1 A2) (3: B1 B2 B3) (0: ).
	ppm0 := []byte{0, 0, 0, 0, 2, 0xA1, 0xA2, 0, 0, 0, 3, 0xB1}
	ppm1 := []byte{1, 0xB2, 0xB3, 0, 0, 0, 0}
	main := slices.Concat(seg(markerSIZ, sizBody(8, 4, 4, 4, 1)...), seg(markerPPM, ppm1...), seg(markerCOD, codBody...), seg(markerQCD, qcdBody...), seg(markerPPM, ppm0...))
	b := codestreamOf(main, tilePart(0, 0, nil, []byte{1}), tilePart(1, 0, nil, []byte{2}), tilePart(0, 1, nil, []byte{3}))
	cs, err := parseCodestream(b)
	if err != nil {
		t.Fatalf("parseCodestream: %v", err)
	}
	if got := cs.tiles[0].packetHeaders; !bytes.Equal(got, []byte{0xA1, 0xA2}) {
		t.Errorf("tile 0 packet headers = % X", got)
	}
	if got := cs.tiles[1].packetHeaders; !bytes.Equal(got, []byte{0xB1, 0xB2, 0xB3}) {
		t.Errorf("tile 1 packet headers = % X", got)
	}
}

// TestParseCodestream_ComponentSegments reads COC, QCC, RGN and user-defined
// precincts, and the two-byte component index used from 257 components on.
func TestParseCodestream_ComponentSegments(t *testing.T) {
	t.Parallel()

	for _, components := range []int{3, 300} {
		idx := func(c byte) []byte {
			if components < 257 {
				return []byte{c}
			}
			return []byte{0, c}
		}
		// COC for component 2: precincts 2^3 x 2^4 then 2^5 x 2^6, two
		// levels, 32x16 code-blocks with bypass and reset, 9-7 filter.
		coc := slices.Concat(idx(2), []byte{0x01, 1, 3, 2, 0x03, 0, 0x43, 0x65})
		// QCC for component 1: expounded, 1 guard bit.
		qcc := slices.Concat(idx(1), []byte{0x22, 0x88, 0x01, 0x90, 0x02})
		rgn := slices.Concat(idx(0), []byte{0, 7})
		main := slices.Concat(seg(markerSIZ, sizBody(8, 8, 8, 8, components)...), seg(markerCOD, codBody...), seg(markerQCD, qcdBody...),
			seg(markerCOC, coc...), seg(markerQCC, qcc...), seg(markerRGN, rgn...))
		cs, err := parseCodestream(codestreamOf(main, tilePart(0, 0, nil, nil)))
		if err != nil {
			t.Fatalf("%d components: parseCodestream: %v", components, err)
		}
		s := cs.main.coc[2]
		if s == nil || s.levels != 1 || s.cbw != 5 || s.cbh != 4 || s.cbStyle != 0x03 || s.reversible || !slices.Equal(s.ppx, []int{3, 5}) || !slices.Equal(s.ppy, []int{4, 6}) {
			t.Errorf("%d components: COC = %+v", components, s)
		}
		q := cs.main.qcc[1]
		if want := []step{{exponent: 17, mantissa: 1}, {exponent: 18, mantissa: 2}}; q == nil || q.style != quantExpounded || q.guards != 1 || !slices.Equal(q.steps, want) {
			t.Errorf("%d components: QCC = %+v", components, q)
		}
		if cs.main.roi[0] != 7 || cs.main.roi[1] != -1 {
			t.Errorf("%d components: RGN shifts = %v", components, cs.main.roi[:2])
		}
	}
}

// TestParseCodestream_Rejects checks that codestreams breaking a rule of T.800,
// Annex A or B are refused with the sentinel naming why.
func TestParseCodestream_Rejects(t *testing.T) {
	t.Parallel()

	siz := seg(markerSIZ, sizBody(8, 4, 4, 4, 1)...)
	cod := seg(markerCOD, codBody...)
	qcd := seg(markerQCD, qcdBody...)
	main := slices.Concat(siz, cod, qcd)
	part := tilePart(0, 0, nil, []byte{1})
	withSIZ := func(mutate func([]byte)) []byte {
		b := sizBody(8, 4, 4, 4, 1)
		mutate(b)
		return codestreamOf(slices.Concat(seg(markerSIZ, b...), cod, qcd), part)
	}
	withCOD := func(body ...byte) []byte {
		return codestreamOf(slices.Concat(siz, seg(markerCOD, body...), qcd), part)
	}
	truncated := codestreamOf(main, part)
	truncated = truncated[:len(truncated)-2]
	overlong := slices.Clone(part)
	binary.BigEndian.PutUint32(overlong[6:], 100)
	for _, tc := range []struct {
		name string
		data []byte
		want error
	}{
		{"no SOC", annexJ10[2:], ErrMalformed},
		{"Rsiz of ISO/IEC 15444-2", withSIZ(func(b []byte) { b[0] = 0x80 }), ErrUnsupported},
		{"Rsiz of extended capabilities", withSIZ(func(b []byte) { b[0] = 0x40 }), ErrUnsupported},
		{"CAP marker segment", codestreamOf(slices.Concat(siz, seg(markerCAP, 0, 0, 0, 0), cod, qcd), part), ErrUnsupported},
		{"empty image area", withSIZ(func(b []byte) { b[13] = 8 }), ErrMalformed},
		{"tile grid past the image origin", withSIZ(func(b []byte) { b[29] = 1 }), ErrMalformed},
		{"zero separation", withSIZ(func(b []byte) { b[37] = 0 }), ErrMalformed},
		{"39-bit component", withSIZ(func(b []byte) { b[36] = 38 }), ErrMalformed},
		{"more tiles than indices", withSIZ(func(b []byte) {
			binary.BigEndian.PutUint32(b[2:], 1<<20)
			binary.BigEndian.PutUint32(b[18:], 1)
		}), ErrMalformed},
		{"no COD", codestreamOf(slices.Concat(siz, qcd), part), ErrMalformed},
		{"no QCD", codestreamOf(slices.Concat(siz, cod), part), ErrMalformed},
		{"second COD", codestreamOf(slices.Concat(main, cod), part), ErrMalformed},
		{"33 levels", withCOD(0, 0, 0, 1, 0, 33, 4, 4, 0, 1), ErrMalformed},
		{"code-blocks over 2^12", withCOD(0, 0, 0, 1, 0, 1, 5, 5, 0, 1), ErrMalformed},
		{"zero layers", withCOD(0, 0, 0, 0, 0, 1, 4, 4, 0, 1), ErrMalformed},
		{"progression order 5", withCOD(0, 5, 0, 1, 0, 1, 4, 4, 0, 1), ErrMalformed},
		{"zero precinct exponent above level 0", withCOD(1, 0, 0, 1, 0, 1, 4, 4, 0, 1, 0x00, 0x10), ErrMalformed},
		{"precinct sizes for too few levels", withCOD(1, 0, 0, 1, 0, 1, 4, 4, 0, 1, 0x00), ErrMalformed},
		{"derived quantization with two steps", codestreamOf(slices.Concat(siz, cod, seg(markerQCD, 0x41, 0, 0, 0, 0)), part), ErrMalformed},
		{"COC for a missing component", codestreamOf(slices.Concat(main, seg(markerCOC, 1, 0, 1, 4, 4, 0, 1)), part), ErrMalformed},
		{"RGN of a reserved style", codestreamOf(slices.Concat(main, seg(markerRGN, 0, 1, 7)), part), ErrMalformed},
		{"tile index past the grid", codestreamOf(main, tilePart(2, 0, nil, nil)), ErrMalformed},
		{"COD in a later tile-part", codestreamOf(main, part, tilePart(0, 1, cod, nil)), ErrMalformed},
		{"PPT alongside PPM", codestreamOf(slices.Concat(main, seg(markerPPM, 0, 0, 0, 0, 0)), tilePart(0, 0, seg(markerPPT, 0), nil)), ErrMalformed},
		{"PPM for too few tile-parts", codestreamOf(slices.Concat(main, seg(markerPPM, 0)), part), ErrMalformed},
		{"PPM for too many tile-parts", codestreamOf(slices.Concat(main, seg(markerPPM, 0, 0, 0, 0, 0, 0, 0, 0, 0)), part), ErrMalformed},
		{"Psot past the end", codestreamOf(main, overlong), ErrMalformed},
		{"no EOC", truncated, ErrMalformed},
		{"segment longer than the data", codestreamOf(slices.Concat(main, []byte{0xFF, 0x64, 0xFF, 0xFF}), part), ErrMalformed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if _, err := parseCodestream(tc.data); !errors.Is(err, tc.want) {
				t.Errorf("parseCodestream = %v, want %v", err, tc.want)
			}
		})
	}
}

// TestParseCodestream_ZeroPsot reads a last tile-part whose Psot is 0, which
// T.800, A.4.2 says runs to the EOC marker.
func TestParseCodestream_ZeroPsot(t *testing.T) {
	t.Parallel()

	main := slices.Concat(seg(markerSIZ, sizBody(8, 4, 4, 4, 1)...), seg(markerCOD, codBody...), seg(markerQCD, qcdBody...))
	part := tilePart(0, 0, nil, []byte{5, 6, 7})
	binary.BigEndian.PutUint32(part[6:], 0)
	cs, err := parseCodestream(codestreamOf(main, part))
	if err != nil {
		t.Fatalf("parseCodestream: %v", err)
	}
	if got := cs.tiles[0].data; !bytes.Equal(got, []byte{5, 6, 7}) {
		t.Errorf("tile data = % X", got)
	}
}
