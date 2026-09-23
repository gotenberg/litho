package jpx

import (
	"cmp"
	"encoding/binary"
	"fmt"
	"slices"
)

// Marker codes, ITU-T T.800, Table A.2.
const (
	markerCAP = 0xFF50
	markerSIZ = 0xFF51
	markerCOD = 0xFF52
	markerCOC = 0xFF53
	markerQCD = 0xFF5C
	markerQCC = 0xFF5D
	markerRGN = 0xFF5E
	markerPOC = 0xFF5F
	markerPPM = 0xFF60
	markerPPT = 0xFF61
	markerSOT = 0xFF90
	markerSOD = 0xFF93
	markerEOC = 0xFFD9
)

// maxTiles is the number of tiles a codestream can address: Isot runs from 0
// to 65 534 (T.800, Table A.5).
const maxTiles = 65535

// maxComponents is the largest Csiz, 16 384 (T.800, Table A.9).
const maxComponents = 16384

// maxPrecision is the deepest component T.800, Table A.11 allows, 38 bits.
const maxPrecision = 38

// maxLevels is the most decomposition levels a COD or COC may state (T.800,
// Table A.15).
const maxLevels = 32

// orderCPRL is the last progression order of T.800, Table A.16,
// component-position-resolution level-layer.
const orderCPRL = 4

// Quantization styles, T.800 Table A.28.
const (
	quantNone      = 0
	quantDerived   = 1
	quantExpounded = 2
)

// component is what the SIZ marker segment says of one component (T.800,
// A.5.1).
type component struct {
	precision int
	signed    bool
	// dx and dy are XRsiz and YRsiz, the component's sample separation on the
	// reference grid.
	dx, dy int
}

// codingStyle is the part of a COD or COC marker segment that may differ per
// component: SPcod or SPcoc and the precinct flag of Scod or Scoc (T.800,
// A.6.1 and A.6.2).
type codingStyle struct {
	levels int
	// cbw and cbh are the code-block width and height exponents xcb and ycb.
	cbw, cbh int
	// cbStyle holds the code-block style flags.
	cbStyle    uint8
	reversible bool
	// ppx and ppy hold the precinct size exponents of each resolution level,
	// lowest first.
	ppx, ppy []int
}

// codingDefault is a COD marker segment: the per-component coding style and
// the parameters that apply to the whole tile or image (T.800, A.6.1).
type codingDefault struct {
	style       codingStyle
	sop, eph    bool
	order       int
	layers      int
	transformed bool
}

// step is one quantization step size: an exponent and, for irreversible
// quantization, an 11-bit mantissa (T.800, Tables A.29 and A.30).
type step struct {
	exponent int
	mantissa int
}

// quantization is a QCD or QCC marker segment (T.800, A.6.4 and A.6.5).
type quantization struct {
	style  int
	guards int
	steps  []step
}

// progressionChange is one progression order volume of a POC marker segment
// (T.800, A.6.6 and B.12.2): layers below layerEnd, resolution levels from
// resStart below resEnd, components from compStart below compEnd.
type progressionChange struct {
	resStart, compStart int
	layerEnd            int
	resEnd, compEnd     int
	order               int
}

// header holds the functional marker segments of the main header or of one
// tile's header (T.800, A.6), each nil or empty when absent.
type header struct {
	cod *codingDefault
	coc []*codingStyle
	qcd *quantization
	qcc []*quantization
	// roi holds each component's RGN shift, or -1 without an RGN.
	roi []int
	poc []progressionChange
}

func newHeader(components int) header {
	h := header{
		coc: make([]*codingStyle, components),
		qcc: make([]*quantization, components),
		roi: make([]int, components),
	}
	for i := range h.roi {
		h.roi[i] = -1
	}
	return h
}

// codestream is a parsed codestream (T.800, Annex A): the image and tile
// geometry of SIZ, the main header, and the tiles its tile-parts carry.
type codestream struct {
	// x1 and y1 are Xsiz and Ysiz, x0 and y0 are XOsiz and YOsiz: the image
	// area on the reference grid (T.800, B.2).
	x0, y0, x1, y1 int
	// tileW and tileH are XTsiz and YTsiz, tileX0 and tileY0 are XTOsiz and
	// YTOsiz (T.800, B.3).
	tileW, tileH   int
	tileX0, tileY0 int
	tilesX, tilesY int
	components     []component
	main           header
	// tiles holds one entry per tile index, nil for a tile no tile-part
	// carries.
	tiles []*tile
}

// tile is one tile's coded data: the header of its first tile-part, POC and
// PPT segments from all of them, and the bit-stream data of all its
// tile-parts in order.
type tile struct {
	index  int
	header header
	data   []byte
	// packetHeaders holds the packet headers carried by PPM or PPT marker
	// segments (T.800, A.7.4 and A.7.5), or nil when they are in data.
	packetHeaders []byte
	parts         int
}

// marker reads the marker at off. Every marker is 0xFF followed by a byte from
// 0x01 to 0xFE (T.800, A.1).
func marker(b []byte, off int) (int, error) {
	if len(b)-off < 2 {
		return 0, fmt.Errorf("%w: codestream ends at offset %d where a marker was expected", ErrMalformed, off)
	}
	if b[off] != 0xFF {
		return 0, fmt.Errorf("%w: byte 0x%02X at offset %d where a marker was expected", ErrMalformed, b[off], off)
	}
	return int(binary.BigEndian.Uint16(b[off:])), nil
}

// segmentBody returns the parameters of the marker segment at off, after its
// marker and length, and the offset after it.
func segmentBody(b []byte, off int) ([]byte, int, error) {
	if len(b)-off < 4 {
		return nil, 0, fmt.Errorf("%w: marker segment at offset %d has no length", ErrMalformed, off)
	}
	length := int(binary.BigEndian.Uint16(b[off+2:]))
	if length < 2 || length > len(b)-off-2 {
		return nil, 0, fmt.Errorf("%w: marker segment 0x%04X at offset %d declares %d bytes, %d remain", ErrMalformed, binary.BigEndian.Uint16(b[off:]), off, length, len(b)-off-2)
	}
	return b[off+4 : off+2+length], off + 2 + length, nil
}

// parseCodestream parses a codestream: its main header, then each tile-part
// until the EOC marker (T.800, A.3).
func parseCodestream(b []byte) (*codestream, error) {
	if len(b) < 4 || binary.BigEndian.Uint16(b) != 0xFF4F || binary.BigEndian.Uint16(b[2:]) != markerSIZ {
		return nil, fmt.Errorf("%w: codestream does not start with SOC and SIZ", ErrMalformed)
	}
	siz, off, err := segmentBody(b, 2)
	if err != nil {
		return nil, err
	}
	cs, err := parseSIZ(siz)
	if err != nil {
		return nil, err
	}
	cs.main = newHeader(len(cs.components))
	var ppm []ppmSegment
	for {
		m, err := marker(b, off)
		if err != nil {
			return nil, fmt.Errorf("main header: %w", err)
		}
		if m == markerSOT {
			break
		}
		if isMarkerOnly(m) {
			off += 2
			continue
		}
		body, next, err := segmentBody(b, off)
		if err != nil {
			return nil, fmt.Errorf("main header: %w", err)
		}
		switch m {
		case markerCAP:
			// T.800, A.5.2: extended capabilities, such as the block coder of
			// ISO/IEC 15444-15, change how the codestream decodes.
			return nil, fmt.Errorf("%w: codestream signals extended capabilities (CAP)", ErrUnsupported)
		case markerPPM:
			if len(body) < 1 {
				return nil, fmt.Errorf("%w: empty PPM marker segment", ErrMalformed)
			}
			ppm = append(ppm, ppmSegment{z: int(body[0]), data: body[1:]})
		case markerSIZ, markerPPT, markerSOD, markerEOC:
			return nil, fmt.Errorf("%w: marker 0x%04X in the main header", ErrMalformed, m)
		default:
			if err := cs.main.apply(m, body, len(cs.components)); err != nil {
				return nil, fmt.Errorf("main header: %w", err)
			}
		}
		off = next
	}
	// T.800, Table A.2: COD and QCD are required in the main header.
	if cs.main.cod == nil || cs.main.qcd == nil {
		return nil, fmt.Errorf("%w: main header lacks a COD or QCD marker segment", ErrMalformed)
	}
	if err := cs.parseTileParts(b, off, ppm); err != nil {
		return nil, err
	}
	return cs, nil
}

// isMarkerOnly reports whether m is one of the markers without parameters
// that T.800, A.1.3 says a decoder skips.
func isMarkerOnly(m int) bool {
	return m >= 0xFF30 && m <= 0xFF3F
}

// ppmSegment is one PPM marker segment: its index Zppm and the packet header
// data after it (T.800, A.7.4).
type ppmSegment struct {
	z    int
	data []byte
}

// parseSIZ reads the SIZ marker segment (T.800, A.5.1) and checks the
// geometry constraints of B.2 and B.3.
func parseSIZ(body []byte) (*codestream, error) {
	if len(body) < 36 {
		return nil, fmt.Errorf("%w: SIZ marker segment of %d bytes", ErrMalformed, len(body))
	}
	rsiz := binary.BigEndian.Uint16(body)
	// T.800, Table A.10 NOTE 1: the two most significant bits of Rsiz signal
	// capabilities of ISO/IEC 15444-2 or extended capabilities, which change
	// decoding in ways this package does not implement.
	if rsiz&0xC000 != 0 {
		return nil, fmt.Errorf("%w: Rsiz 0x%04X requires capabilities beyond ITU-T T.800", ErrUnsupported, rsiz)
	}
	u := func(i int) int { return int(binary.BigEndian.Uint32(body[2+4*i:])) }
	cs := &codestream{
		x1: u(0), y1: u(1), x0: u(2), y0: u(3),
		tileW: u(4), tileH: u(5), tileX0: u(6), tileY0: u(7),
	}
	n := int(binary.BigEndian.Uint16(body[34:]))
	if n < 1 || n > maxComponents || len(body) != 36+3*n {
		return nil, fmt.Errorf("%w: SIZ marker segment of %d bytes for %d components", ErrMalformed, len(body), n)
	}
	// T.800, B.2 and B.3: a non-empty image area, tiles of at least one grid
	// point, a tile grid that starts no later than the image area (B-3) and a
	// first tile that reaches into it (B-4).
	switch {
	case cs.x1 <= cs.x0 || cs.y1 <= cs.y0:
		return nil, fmt.Errorf("%w: empty image area from (%d,%d) to (%d,%d)", ErrMalformed, cs.x0, cs.y0, cs.x1, cs.y1)
	case cs.tileW < 1 || cs.tileH < 1:
		return nil, fmt.Errorf("%w: tiles of %dx%d", ErrMalformed, cs.tileW, cs.tileH)
	case cs.tileX0 > cs.x0 || cs.tileY0 > cs.y0 || cs.tileX0+cs.tileW <= cs.x0 || cs.tileY0+cs.tileH <= cs.y0:
		return nil, fmt.Errorf("%w: tile grid at (%d,%d) does not cover the image area origin (%d,%d)", ErrMalformed, cs.tileX0, cs.tileY0, cs.x0, cs.y0)
	}
	cs.tilesX = ceilDiv(cs.x1-cs.tileX0, cs.tileW)
	cs.tilesY = ceilDiv(cs.y1-cs.tileY0, cs.tileH)
	if cs.tilesX*cs.tilesY > maxTiles {
		return nil, fmt.Errorf("%w: %dx%d tiles, more than tile indices can address", ErrMalformed, cs.tilesX, cs.tilesY)
	}
	cs.components = make([]component, n)
	for i := range cs.components {
		c := body[36+3*i:]
		cs.components[i] = component{
			precision: int(c[0]&0x7F) + 1,
			signed:    c[0]&0x80 != 0,
			dx:        int(c[1]),
			dy:        int(c[2]),
		}
		if cs.components[i].precision > maxPrecision || c[1] == 0 || c[2] == 0 {
			return nil, fmt.Errorf("%w: component %d has Ssiz 0x%02X and separation %dx%d", ErrMalformed, i, c[0], c[1], c[2])
		}
	}
	cs.tiles = make([]*tile, cs.tilesX*cs.tilesY)
	return cs, nil
}

// ceilDiv returns a / b rounded up, for a >= 0 and b > 0.
func ceilDiv(a, b int) int {
	return (a + b - 1) / b
}

// componentIndex reads the component index that starts COC, QCC, RGN and POC
// parameters: one byte when there are fewer than 257 components, two
// otherwise (T.800, Tables A.22, A.24, A.31 and A.32).
func componentIndex(body []byte, components int) (int, []byte, error) {
	if components < 257 {
		if len(body) < 1 {
			return 0, nil, fmt.Errorf("%w: marker segment ends before its component index", ErrMalformed)
		}
		return int(body[0]), body[1:], nil
	}
	if len(body) < 2 {
		return 0, nil, fmt.Errorf("%w: marker segment ends before its component index", ErrMalformed)
	}
	return int(binary.BigEndian.Uint16(body)), body[2:], nil
}

// apply records one functional marker segment in h. Marker segments this
// package does not interpret, such as TLM, PLM, PLT, CRG and COM, are
// discarded, as T.800, A.1 requires of any it does not specify.
func (h *header) apply(m int, body []byte, components int) error {
	switch m {
	case markerCOD:
		if h.cod != nil {
			return fmt.Errorf("%w: second COD marker segment", ErrMalformed)
		}
		cod, err := parseCOD(body)
		if err != nil {
			return err
		}
		h.cod = cod
	case markerCOC:
		c, rest, err := componentIndex(body, components)
		if err != nil {
			return err
		}
		if c >= components || h.coc[c] != nil {
			return fmt.Errorf("%w: COC for component %d of %d, or a second one", ErrMalformed, c, components)
		}
		if len(rest) < 1 {
			return fmt.Errorf("%w: COC marker segment ends before Scoc", ErrMalformed)
		}
		style, err := parseStyle(rest[1:], rest[0]&0x01 != 0)
		if err != nil {
			return fmt.Errorf("COC for component %d: %w", c, err)
		}
		h.coc[c] = style
	case markerQCD:
		if h.qcd != nil {
			return fmt.Errorf("%w: second QCD marker segment", ErrMalformed)
		}
		q, err := parseQuantization(body)
		if err != nil {
			return err
		}
		h.qcd = q
	case markerQCC:
		c, rest, err := componentIndex(body, components)
		if err != nil {
			return err
		}
		if c >= components || h.qcc[c] != nil {
			return fmt.Errorf("%w: QCC for component %d of %d, or a second one", ErrMalformed, c, components)
		}
		q, err := parseQuantization(rest)
		if err != nil {
			return fmt.Errorf("QCC for component %d: %w", c, err)
		}
		h.qcc[c] = q
	case markerRGN:
		c, rest, err := componentIndex(body, components)
		if err != nil {
			return err
		}
		// T.800, Tables A.24 to A.26: Srgn 0, implicit ROI, is the only style.
		if c >= components || h.roi[c] >= 0 || len(rest) != 2 || rest[0] != 0 {
			return fmt.Errorf("%w: RGN marker segment for component %d", ErrMalformed, c)
		}
		h.roi[c] = int(rest[1])
	case markerPOC:
		if h.poc != nil {
			return fmt.Errorf("%w: second POC marker segment in one header", ErrMalformed)
		}
		poc, err := parsePOC(body, components)
		if err != nil {
			return err
		}
		h.poc = poc
	}
	return nil
}

// parseCOD reads a COD marker segment (T.800, A.6.1).
func parseCOD(body []byte) (*codingDefault, error) {
	if len(body) < 5 {
		return nil, fmt.Errorf("%w: COD marker segment of %d bytes", ErrMalformed, len(body))
	}
	// T.800, Table A.13: decoders may ignore the five high bits of Scod.
	scod := body[0]
	cod := &codingDefault{
		sop:         scod&0x02 != 0,
		eph:         scod&0x04 != 0,
		order:       int(body[1]),
		layers:      int(binary.BigEndian.Uint16(body[2:])),
		transformed: body[4] == 1,
	}
	if cod.order > orderCPRL || cod.layers < 1 || body[4] > 1 {
		return nil, fmt.Errorf("%w: COD with progression order %d, %d layers and multiple component transformation %d", ErrMalformed, cod.order, cod.layers, body[4])
	}
	style, err := parseStyle(body[5:], scod&0x01 != 0)
	if err != nil {
		return nil, fmt.Errorf("COD: %w", err)
	}
	cod.style = *style
	return cod, nil
}

// parseStyle reads SPcod or SPcoc (T.800, Table A.15). precincts is the
// precinct bit of Scod or Scoc: without it every precinct is 2^15 wide and
// high.
func parseStyle(body []byte, precincts bool) (*codingStyle, error) {
	if len(body) < 5 {
		return nil, fmt.Errorf("%w: coding style parameters of %d bytes", ErrMalformed, len(body))
	}
	s := &codingStyle{
		levels: int(body[0]),
		cbw:    int(body[1]) + 2,
		cbh:    int(body[2]) + 2,
		// T.800, Table A.19: decoders may ignore the two high bits.
		cbStyle:    body[3] & 0x3F,
		reversible: body[4] == 1,
	}
	// T.800, Tables A.15, A.18 and A.20: at most 32 levels, code-blocks of
	// 2^2 to 2^10 with xcb + ycb <= 12, and one of the two filters.
	if s.levels > maxLevels || body[1] > 8 || body[2] > 8 || s.cbw+s.cbh > 12 || body[4] > 1 {
		return nil, fmt.Errorf("%w: %d levels, code-blocks 2^%d x 2^%d, transformation %d", ErrMalformed, s.levels, s.cbw, s.cbh, body[4])
	}
	s.ppx = make([]int, s.levels+1)
	s.ppy = make([]int, s.levels+1)
	if !precincts {
		for r := range s.ppx {
			s.ppx[r], s.ppy[r] = 15, 15
		}
		return s, nil
	}
	if len(body) != 5+s.levels+1 {
		return nil, fmt.Errorf("%w: %d bytes of precinct sizes for %d resolution levels", ErrMalformed, len(body)-5, s.levels+1)
	}
	for r := range s.ppx {
		v := body[5+r]
		s.ppx[r], s.ppy[r] = int(v&0x0F), int(v>>4)
		// T.800, Table A.21: only the lowest resolution level may have a zero
		// exponent.
		if r > 0 && (s.ppx[r] == 0 || s.ppy[r] == 0) {
			return nil, fmt.Errorf("%w: precinct exponents %d x %d at resolution level %d", ErrMalformed, s.ppx[r], s.ppy[r], r)
		}
	}
	return s, nil
}

// parseQuantization reads Sqcd and SPqcd, or Sqcc and SPqcc (T.800, Tables
// A.28 to A.30).
func parseQuantization(body []byte) (*quantization, error) {
	if len(body) < 1 {
		return nil, fmt.Errorf("%w: empty quantization marker segment", ErrMalformed)
	}
	q := &quantization{style: int(body[0] & 0x1F), guards: int(body[0] >> 5)}
	values := body[1:]
	switch q.style {
	case quantNone:
		q.steps = make([]step, len(values))
		for i, v := range values {
			q.steps[i] = step{exponent: int(v >> 3)}
		}
	case quantDerived, quantExpounded:
		if len(values)%2 != 0 {
			return nil, fmt.Errorf("%w: %d bytes of 16-bit quantization step sizes", ErrMalformed, len(values))
		}
		q.steps = make([]step, len(values)/2)
		for i := range q.steps {
			v := binary.BigEndian.Uint16(values[2*i:])
			q.steps[i] = step{exponent: int(v >> 11), mantissa: int(v & 0x7FF)}
		}
	default:
		return nil, fmt.Errorf("%w: quantization style %d", ErrMalformed, q.style)
	}
	// T.800, Equation (A-4): derived quantization signals the NLLL step only.
	if len(q.steps) == 0 || (q.style == quantDerived && len(q.steps) != 1) {
		return nil, fmt.Errorf("%w: %d step sizes for quantization style %d", ErrMalformed, len(q.steps), q.style)
	}
	return q, nil
}

// parsePOC reads a POC marker segment (T.800, A.6.6).
func parsePOC(body []byte, components int) ([]progressionChange, error) {
	size := 7
	if components >= 257 {
		size = 9
	}
	if len(body) == 0 || len(body)%size != 0 {
		return nil, fmt.Errorf("%w: POC marker segment of %d bytes", ErrMalformed, len(body))
	}
	changes := make([]progressionChange, len(body)/size)
	for i := range changes {
		e := body[i*size:]
		var ch progressionChange
		ch.resStart = int(e[0])
		if size == 7 {
			ch.compStart = int(e[1])
			ch.layerEnd = int(binary.BigEndian.Uint16(e[2:]))
			ch.resEnd = int(e[4])
			ch.compEnd = int(e[5])
			ch.order = int(e[6])
			// T.800, Table A.32: CEpoc 0 is interpreted as 256.
			if ch.compEnd == 0 {
				ch.compEnd = 256
			}
		} else {
			ch.compStart = int(binary.BigEndian.Uint16(e[1:]))
			ch.layerEnd = int(binary.BigEndian.Uint16(e[3:]))
			ch.resEnd = int(e[5])
			ch.compEnd = int(binary.BigEndian.Uint16(e[6:]))
			ch.order = int(e[8])
			if ch.compEnd == 0 {
				ch.compEnd = maxComponents
			}
		}
		if ch.order > orderCPRL || ch.resEnd <= ch.resStart || ch.compEnd <= ch.compStart || ch.layerEnd < 1 {
			return nil, fmt.Errorf("%w: progression order change %d: %+v", ErrMalformed, i, ch)
		}
		changes[i] = ch
	}
	return changes, nil
}

// parseTileParts reads the tile-parts from off up to the EOC marker (T.800,
// A.4) and gathers each tile's header, data and packet headers.
//
// The loop is bounded by len(b): each tile-part is at least the 14 bytes of
// its SOT and SOD markers.
func (cs *codestream) parseTileParts(b []byte, off int, ppm []ppmSegment) error {
	// T.800, A.7.4: PPM segments concatenate in Zppm order into one list of
	// (Nppm, Ippm) pairs, one pair per tile-part in codestream order.
	var ppmData []byte
	if len(ppm) > 0 {
		slices.SortStableFunc(ppm, func(a, b ppmSegment) int { return cmp.Compare(a.z, b.z) })
		for _, s := range ppm {
			ppmData = append(ppmData, s.data...)
		}
	}
	for {
		m, err := marker(b, off)
		if err != nil {
			return err
		}
		if m == markerEOC {
			break
		}
		if m != markerSOT {
			return fmt.Errorf("%w: marker 0x%04X at offset %d where SOT or EOC was expected", ErrMalformed, m, off)
		}
		next, err := cs.parseTilePart(b, off, &ppmData, ppm != nil)
		if err != nil {
			return err
		}
		off = next
	}
	if len(ppmData) != 0 {
		return fmt.Errorf("%w: %d bytes of PPM packet headers left over", ErrMalformed, len(ppmData))
	}
	return nil
}

// parseTilePart reads the tile-part whose SOT marker is at off and returns
// the offset after it.
func (cs *codestream) parseTilePart(b []byte, off int, ppmData *[]byte, havePPM bool) (int, error) {
	sot, pos, err := segmentBody(b, off)
	if err != nil {
		return 0, err
	}
	if len(sot) != 8 {
		return 0, fmt.Errorf("%w: SOT marker segment of %d bytes", ErrMalformed, len(sot))
	}
	index := int(binary.BigEndian.Uint16(sot))
	length := int(binary.BigEndian.Uint32(sot[2:]))
	if index >= len(cs.tiles) {
		return 0, fmt.Errorf("%w: tile-part for tile %d of %d", ErrMalformed, index, len(cs.tiles))
	}
	// T.800, A.4.2: Psot counts from the SOT marker to the end of the
	// tile-part's data; 0 means the data runs to the EOC marker.
	end := off + length
	if length == 0 {
		end = len(b) - 2
		if end < pos || binary.BigEndian.Uint16(b[end:]) != markerEOC {
			return 0, fmt.Errorf("%w: last tile-part has no EOC marker after it", ErrMalformed)
		}
	} else if length < 14 || length > len(b)-off {
		return 0, fmt.Errorf("%w: tile-part at offset %d declares %d bytes, %d remain", ErrMalformed, off, length, len(b)-off)
	}
	t := cs.tiles[index]
	first := t == nil
	if first {
		t = &tile{index: index, header: newHeader(len(cs.components))}
		cs.tiles[index] = t
	}
	t.parts++
	var ppt []ppmSegment
	for {
		m, err := marker(b[:end], pos)
		if err != nil {
			return 0, fmt.Errorf("tile %d header: %w", index, err)
		}
		if m == markerSOD {
			pos += 2
			break
		}
		if isMarkerOnly(m) {
			pos += 2
			continue
		}
		body, next, err := segmentBody(b[:end], pos)
		if err != nil {
			return 0, fmt.Errorf("tile %d header: %w", index, err)
		}
		switch m {
		case markerPPT:
			if havePPM || len(body) < 1 {
				return 0, fmt.Errorf("%w: PPT marker segment in tile %d alongside PPM, or empty", ErrMalformed, index)
			}
			ppt = append(ppt, ppmSegment{z: int(body[0]), data: body[1:]})
		case markerPOC:
			// T.800, A.6.6: progression order volumes may be described in any
			// tile-part header before their packets, so they accumulate.
			poc, err := parsePOC(body, len(cs.components))
			if err != nil {
				return 0, fmt.Errorf("tile %d header: %w", index, err)
			}
			t.header.poc = append(t.header.poc, poc...)
		case markerCOD, markerCOC, markerQCD, markerQCC, markerRGN:
			// T.800, A.6.1 to A.6.5: these belong in the first tile-part only.
			if !first {
				return 0, fmt.Errorf("%w: marker 0x%04X in tile-part %d of tile %d", ErrMalformed, m, t.parts-1, index)
			}
			if err := t.header.apply(m, body, len(cs.components)); err != nil {
				return 0, fmt.Errorf("tile %d header: %w", index, err)
			}
		case markerSIZ, markerCAP, markerPPM, markerSOT, markerEOC:
			return 0, fmt.Errorf("%w: marker 0x%04X in the header of tile %d", ErrMalformed, m, index)
		}
		pos = next
	}
	if len(ppt) > 0 {
		slices.SortStableFunc(ppt, func(a, b ppmSegment) int { return cmp.Compare(a.z, b.z) })
		for _, s := range ppt {
			t.packetHeaders = append(t.packetHeaders, s.data...)
		}
		if t.packetHeaders == nil {
			t.packetHeaders = []byte{}
		}
	}
	if havePPM {
		if len(*ppmData) < 4 {
			return 0, fmt.Errorf("%w: PPM holds no packet headers for tile-part %d of tile %d", ErrMalformed, t.parts-1, index)
		}
		n := int(binary.BigEndian.Uint32(*ppmData))
		if n > len(*ppmData)-4 {
			return 0, fmt.Errorf("%w: PPM declares %d bytes of packet headers, %d remain", ErrMalformed, n, len(*ppmData)-4)
		}
		t.packetHeaders = append(t.packetHeaders, (*ppmData)[4:4+n]...)
		if t.packetHeaders == nil {
			t.packetHeaders = []byte{}
		}
		*ppmData = (*ppmData)[4+n:]
	}
	t.data = append(t.data, b[pos:end]...)
	return end, nil
}
