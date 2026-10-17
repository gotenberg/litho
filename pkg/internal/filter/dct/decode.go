package dct

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"math"
)

// DefaultMaxSamples bounds the samples of one image, over all components,
// under the default image budget. It is an anti-DoS guard, not spec-mandated:
// a JPEG header of a few bytes can declare 65535 x 65535 pixels, and every
// sample costs two bytes of coefficients, a byte of reconstructed plane and a
// byte of output. 2^27 is the bound package jpx sets for the same reason, and
// admits the largest JPEG real documents carry. [Decode] takes the cap scaled
// with the document's image budget (limits.Limits.ScaleImageCap).
//
// # Corpus evidence
//
//   - All corpora: the largest JPEG is 10248 x 3712 in three components.
const DefaultMaxSamples = 1 << 27

// maxCategory is the largest DC difference category a Huffman value may
// name. T.81, Table F.1 goes to 11 for 8-bit samples; the value is a byte of
// the file, and a category past 16 would read more bits than one read holds.
const maxCategory = 16

// ctxBlocks is how many blocks are decoded between checks of the context.
const ctxBlocks = 4096

// zigzag maps the position of a coefficient in the zig-zag sequence of
// ITU-T T.81, Figure A.6 to its row-major position in the block.
var zigzag = [64]int{
	0, 1, 8, 16, 9, 2, 3, 10,
	17, 24, 32, 25, 18, 11, 4, 5,
	12, 19, 26, 33, 40, 48, 41, 34,
	27, 20, 13, 6, 7, 14, 21, 28,
	35, 42, 49, 56, 57, 50, 43, 36,
	29, 22, 15, 23, 30, 37, 44, 51,
	58, 59, 52, 45, 38, 31, 39, 46,
	53, 60, 61, 54, 47, 55, 62, 63,
}

// component is one image component of the frame (T.81, B.2.2).
type component struct {
	id     byte
	h, v   int
	tq     int
	width  int // samples a row, ceil(X * h / Hmax)
	height int
	// bw and bh count the blocks a row and a column, padded to whole MCUs,
	// and coeffs holds bw*bh blocks of 64 coefficients in row-major order.
	bw, bh int
	coeffs []int16
	pred   int
}

// decoder holds the state of one Decode call.
type decoder struct {
	data              []byte
	width, height     int
	progressive       bool
	comps             []*component
	hmax, vmax        int
	mcusX, mcusY      int
	quant             [4][64]int32
	dc, ac            [4]*huffman
	restartInterval   int
	eobrun            int
	adobe             bool
	adobeTransform    byte
	statedHeight      int
	maxSamples        int64
	blocksSinceCtx    int
	sawFrame, sawScan bool
	// checkOnly stops the walk of the marker segments at the first scan,
	// whose coded data starts at firstScanData, and allocates no plane: see
	// [Check].
	checkOnly     bool
	firstScanData int
}

// Decode decodes a JPEG, baseline or progressive Huffman-coded (ITU-T T.81),
// into one byte per component per pixel, in rows, the way the DCTDecode
// filter hands its samples on (ISO 32000-1, 7.4.8): three components are
// converted from YCbCr to RGB and four from YCCK to CMYK when the colour
// transform says so, and CMYK is not inverted. colorTransform is the
// filter's /ColorTransform, or -1 when absent; an Adobe APP14 marker in the
// data overrides it. statedHeight is the /Height of the image the data is
// for, or 0 when unknown. maxSamples caps the samples of the image over all
// its components, [DefaultMaxSamples] under the default image budget; a frame
// past it fails with [ErrLimit] before anything is allocated.
//
// Decode tolerates the damage other readers render through: a scan whose
// data ends early keeps the blocks decoded before the end, a component
// identifier repeated in the frame is matched by position, a height of zero
// is taken from a DNL marker, and a frame that declares more lines than the
// image states, a placeholder a streaming encoder never corrected, is decoded
// to the stated height.
func Decode(ctx context.Context, data []byte, colorTransform, statedHeight int, maxSamples int64) ([]byte, int, int, int, error) {
	d := &decoder{data: data, statedHeight: statedHeight, maxSamples: maxSamples}
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return nil, 0, 0, 0, fmt.Errorf("%w: no start-of-image marker", ErrMalformed)
	}
	if err := d.decodeSegments(ctx); err != nil {
		return nil, 0, 0, 0, err
	}
	if !d.sawScan {
		return nil, 0, 0, 0, fmt.Errorf("%w: no scan", ErrMalformed)
	}
	out, err := d.pixels(colorTransform)
	if err != nil {
		return nil, 0, 0, 0, err
	}
	return out, len(d.comps), d.width, d.height, nil
}

// Check reports, from the marker segments before the first scan alone,
// whether [Decode] refuses data with [ErrUnsupported], and decodes no scan to
// find out. err is the first error Decode meets before the first scan's coded
// data: a coding Decode refuses as unsupported, lossless, arithmetic or
// hierarchical coding, samples other than 8 bits or a count of components it
// lacks, or a malformed or over-budget header it fails on first. statedHeight
// and maxSamples are what Decode is given; the colour transform never refuses
// data, so Check takes none.
//
// settled is false when Decode may refuse the data past the first scan,
// where Check does not look: a frame marker after it starts a second frame,
// or one of a coding Decode lacks. Check finds such a marker by its two bytes
// anywhere past the scan, which the coded data escapes (T.81, B.1.1.5), so
// settled is true only when Decode cannot meet one.
//
// Check does not read the coded data, so data damaged there fails Decode
// where Check reports nothing.
func Check(ctx context.Context, data []byte, statedHeight int, maxSamples int64) (settled bool, err error) {
	d := &decoder{data: data, statedHeight: statedHeight, maxSamples: maxSamples, checkOnly: true}
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return true, fmt.Errorf("%w: no start-of-image marker", ErrMalformed)
	}
	if err := d.decodeSegments(ctx); err != nil {
		return true, err
	}
	if !d.sawScan {
		return true, fmt.Errorf("%w: no scan", ErrMalformed)
	}
	if err := ctx.Err(); err != nil {
		return true, err
	}
	return !hasFrameMarker(data[d.firstScanData:]), nil
}

// isFrameMarker reports whether m is a start-of-frame marker (T.81, Table
// B.1): SOF0 to SOF15 but for DHT, JPG and DAC, which share the range.
func isFrameMarker(m byte) bool {
	return m >= 0xC0 && m <= 0xCF && m != 0xC4 && m != 0xC8 && m != 0xCC
}

// hasFrameMarker reports whether data holds a start-of-frame marker's two
// bytes anywhere.
func hasFrameMarker(data []byte) bool {
	for {
		i := bytes.IndexByte(data, 0xFF)
		if i < 0 || i+1 >= len(data) {
			return false
		}
		if isFrameMarker(data[i+1]) {
			return true
		}
		data = data[i+1:]
	}
}

// decodeSegments walks the marker segments and decodes each scan.
func (d *decoder) decodeSegments(ctx context.Context) error {
	p := 2
	for p+1 < len(d.data) {
		if d.data[p] != 0xFF {
			// Garbage between segments: look for the next marker.
			p++
			continue
		}
		m := d.data[p+1]
		switch {
		case m == 0xFF || m == 0x00:
			p++
			continue
		case m == 0xD8 || (m >= 0xD0 && m <= 0xD7) || m == 0x01:
			p += 2
			continue
		case m == 0xD9:
			return nil
		}
		if p+4 > len(d.data) {
			return nil
		}
		length := int(binary.BigEndian.Uint16(d.data[p+2:]))
		if length < 2 || p+2+length > len(d.data) {
			return fmt.Errorf("%w: segment 0x%02X of %d bytes past the data", ErrMalformed, m, length)
		}
		body := d.data[p+4 : p+2+length]
		var err error
		switch {
		case m == 0xC0 || m == 0xC1 || m == 0xC2:
			err = d.frame(body, m == 0xC2)
		case isFrameMarker(m):
			err = fmt.Errorf("%w: frame 0x%02X (lossless or arithmetic coding)", ErrUnsupported, m)
		case m == 0xC4:
			err = d.huffmanTables(body)
		case m == 0xDB:
			err = d.quantTables(body)
		case m == 0xDD:
			if len(body) >= 2 {
				d.restartInterval = int(binary.BigEndian.Uint16(body))
			}
		case m == 0xEE:
			if len(body) >= 12 && string(body[:5]) == "Adobe" {
				d.adobe, d.adobeTransform = true, body[11]
			}
		case m == 0xDA && d.checkOnly:
			if _, err := d.scanHeader(body); err != nil {
				return err
			}
			d.sawScan, d.firstScanData = true, p+2+length
			return nil
		case m == 0xDA:
			var next int
			next, err = d.scan(ctx, body, p+2+length)
			if err != nil {
				return err
			}
			p = next
			continue
		}
		if err != nil {
			return err
		}
		p += 2 + length
	}
	return nil
}

// frame reads a start-of-frame segment (T.81, B.2.2) and lays out the
// component planes.
//
// # Corpus evidence
//
//   - pdfjs issue8614.pdf: declares 65535 lines with a DNL marker giving
//     3477, and PDF.js draws it at the stated size.
//   - pdfjs issue10880.pdf: declares 65280 lines of which its data holds
//     the 3712 the image states, and PDF.js draws it at the stated size.
func (d *decoder) frame(b []byte, progressive bool) error {
	if d.sawFrame {
		return fmt.Errorf("%w: a second frame", ErrUnsupported)
	}
	if len(b) < 6 {
		return fmt.Errorf("%w: frame header of %d bytes", ErrMalformed, len(b))
	}
	if b[0] != 8 {
		return fmt.Errorf("%w: %d-bit samples", ErrUnsupported, b[0])
	}
	d.sawFrame, d.progressive = true, progressive
	d.height = int(binary.BigEndian.Uint16(b[1:]))
	d.width = int(binary.BigEndian.Uint16(b[3:]))
	n := int(b[5])
	if n != 1 && n != 3 && n != 4 {
		return fmt.Errorf("%w: %d components", ErrUnsupported, n)
	}
	if len(b) < 6+3*n {
		return fmt.Errorf("%w: frame header of %d bytes for %d components", ErrMalformed, len(b), n)
	}
	if d.height == 0 {
		d.height = d.lines()
	}
	// A frame can declare more lines than the image states, a placeholder a
	// streaming encoder never corrected, and PDF.js draws such an image at
	// the stated size.
	if d.statedHeight > 0 && d.height > d.statedHeight {
		d.height = d.statedHeight
	}
	if d.width == 0 || d.height == 0 {
		return fmt.Errorf("%w: %dx%d image", ErrMalformed, d.width, d.height)
	}
	if int64(d.width)*int64(d.height) > d.maxSamples/int64(n) {
		return fmt.Errorf("%w: %dx%d image of %d components", ErrLimit, d.width, d.height, n)
	}
	d.hmax, d.vmax = 1, 1
	for i := range n {
		c := &component{id: b[6+3*i], h: int(b[7+3*i] >> 4), v: int(b[7+3*i] & 15), tq: int(b[8+3*i])}
		if c.h < 1 || c.h > 4 || c.v < 1 || c.v > 4 || c.tq > 3 {
			return fmt.Errorf("%w: component %d samples %dx%d with table %d", ErrMalformed, i, c.h, c.v, c.tq)
		}
		d.hmax, d.vmax = max(d.hmax, c.h), max(d.vmax, c.v)
		d.comps = append(d.comps, c)
	}
	d.mcusX = ceilDiv(d.width, 8*d.hmax)
	d.mcusY = ceilDiv(d.height, 8*d.vmax)
	for _, c := range d.comps {
		c.width = ceilDiv(d.width*c.h, d.hmax)
		c.height = ceilDiv(d.height*c.v, d.vmax)
		c.bw, c.bh = d.mcusX*c.h, d.mcusY*c.v
		if !d.checkOnly {
			c.coeffs = make([]int16, c.bw*c.bh*64)
		}
	}
	return nil
}

// lines returns the height a DNL marker gives (T.81, B.2.5), or 0.
func (d *decoder) lines() int {
	for p := 2; p+5 < len(d.data); p++ {
		if d.data[p] == 0xFF && d.data[p+1] == 0xDC && binary.BigEndian.Uint16(d.data[p+2:]) == 4 {
			return int(binary.BigEndian.Uint16(d.data[p+4:]))
		}
	}
	return 0
}

// quantTables reads a DQT segment (T.81, B.2.4.1), storing each table in
// row-major order.
func (d *decoder) quantTables(b []byte) error {
	for len(b) > 0 {
		pq, tq := b[0]>>4, int(b[0]&15)
		size := 64 * (1 + int(pq))
		if tq > 3 || pq > 1 || len(b) < 1+size {
			return fmt.Errorf("%w: quantization table %d", ErrMalformed, tq)
		}
		for k := range 64 {
			if pq == 0 {
				d.quant[tq][zigzag[k]] = int32(b[1+k])
			} else {
				d.quant[tq][zigzag[k]] = int32(binary.BigEndian.Uint16(b[1+2*k:]))
			}
		}
		b = b[1+size:]
	}
	return nil
}

// huffmanTables reads a DHT segment (T.81, B.2.4.2).
func (d *decoder) huffmanTables(b []byte) error {
	for len(b) >= 17 {
		tc, th := b[0]>>4, int(b[0]&15)
		var counts [16]byte
		copy(counts[:], b[1:17])
		total := 0
		for _, c := range counts {
			total += int(c)
		}
		if tc > 1 || th > 3 || len(b) < 17+total {
			return fmt.Errorf("%w: Huffman table %d/%d", ErrMalformed, tc, th)
		}
		h, err := newHuffman(counts, b[17:17+total])
		if err != nil {
			return err
		}
		if tc == 0 {
			d.dc[th] = h
		} else {
			d.ac[th] = h
		}
		b = b[17+total:]
	}
	return nil
}

// scanComp is one component of a scan with its tables.
type scanComp struct {
	c      *component
	dc, ac *huffman
}

// scanParams is what a start-of-scan header (T.81, B.2.3) says of its scan:
// its components, the band of coefficients it codes, ss to se, and the
// successive approximation bits ah and al.
type scanParams struct {
	comps          []scanComp
	ss, se, ah, al int
}

// scanHeader reads a start-of-scan header (T.81, B.2.3), checking it against
// the frame and the tables read so far.
func (d *decoder) scanHeader(b []byte) (scanParams, error) {
	if !d.sawFrame {
		return scanParams{}, fmt.Errorf("%w: scan before frame", ErrMalformed)
	}
	if len(b) < 1 || len(b) < 1+2*int(b[0])+3 || b[0] < 1 || int(b[0]) > len(d.comps) {
		return scanParams{}, fmt.Errorf("%w: scan header", ErrMalformed)
	}
	ns := int(b[0])
	var sc []scanComp
	used := make([]bool, len(d.comps))
	for i := range ns {
		id, tables := b[1+2*i], b[2+2*i]
		// A component is the first of that identifier not already in the
		// scan, which matches repeated identifiers by position.
		k := -1
		for j, c := range d.comps {
			if c.id == id && !used[j] {
				k = j
				break
			}
		}
		if k < 0 {
			return scanParams{}, fmt.Errorf("%w: scan names component %d the frame lacks", ErrMalformed, id)
		}
		used[k] = true
		sc = append(sc, scanComp{c: d.comps[k], dc: d.dc[tables>>4&3], ac: d.ac[tables&3]})
	}
	ss, se := int(b[1+2*ns]), int(b[2+2*ns])
	ah, al := int(b[3+2*ns]>>4), int(b[3+2*ns]&15)
	if !d.progressive {
		ss, se, ah, al = 0, 63, 0, 0
	}
	if ss > se || se > 63 || al > 13 || (ss == 0 && se != 0 && d.progressive) {
		return scanParams{}, fmt.Errorf("%w: spectral selection %d..%d", ErrMalformed, ss, se)
	}
	for _, s := range sc {
		if (ss == 0 && s.dc == nil && ah == 0) || (se > 0 && s.ac == nil) {
			return scanParams{}, fmt.Errorf("%w: scan uses a Huffman table the data lacks", ErrMalformed)
		}
	}
	return scanParams{comps: sc, ss: ss, se: se, ah: ah, al: al}, nil
}

// scan reads a start-of-scan segment (T.81, B.2.3), decodes the scan whose
// data starts at dataStart, and returns the position of the marker after
// it.
func (d *decoder) scan(ctx context.Context, b []byte, dataStart int) (int, error) {
	h, err := d.scanHeader(b)
	if err != nil {
		return 0, err
	}
	sc, ns := h.comps, len(h.comps)
	ss, se, ah, al := h.ss, h.se, h.ah, h.al
	d.sawScan = true
	r := &bitReader{data: d.data, start: dataStart, pos: dataStart}
	for _, c := range d.comps {
		c.pred = 0
	}
	d.eobrun = 0
	block := func(s scanComp, bx, by int) error {
		coef := s.c.coeffs[(by*s.c.bw+bx)*64 : (by*s.c.bw+bx+1)*64]
		switch {
		case !d.progressive:
			return d.baseline(r, s, coef)
		case ss == 0 && ah == 0:
			return d.dcFirst(r, s, coef, al)
		case ss == 0:
			coef[0] |= int16(r.bit() << al) //nolint:gosec // al is at most 13.
			return nil
		case ah == 0:
			return d.acFirst(r, s, coef, ss, se, al)
		default:
			return d.acRefine(r, s, coef, ss, se, al)
		}
	}
	// A scan of one component codes its blocks in raster order over the
	// component's own blocks (T.81, A.2.2); an interleaved scan codes MCUs.
	units, perRow := d.mcusX*d.mcusY, d.mcusX
	if ns == 1 {
		c := sc[0].c
		perRow = ceilDiv(c.width, 8)
		units = perRow * ceilDiv(c.height, 8)
	}
	for u := 0; u < units; u++ {
		if d.restartInterval > 0 && u > 0 && u%d.restartInterval == 0 {
			r.restart()
			for _, c := range d.comps {
				c.pred = 0
			}
			d.eobrun = 0
		}
		if d.blocksSinceCtx++; d.blocksSinceCtx >= ctxBlocks {
			d.blocksSinceCtx = 0
			if err := ctx.Err(); err != nil {
				return 0, err
			}
		}
		ux, uy := u%perRow, u/perRow
		var err error
		if ns == 1 {
			err = block(sc[0], ux, uy)
		} else {
		mcu:
			for _, s := range sc {
				for v := range s.c.v {
					for h := range s.c.h {
						if err = block(s, ux*s.c.h+h, uy*s.c.v+v); err != nil {
							break mcu
						}
					}
				}
			}
		}
		// Damaged data, or data that ends before the scan does, ends the
		// scan: the blocks decoded so far stand, the rest keep what earlier
		// scans gave them.
		if err != nil || r.exhausted() {
			break
		}
	}
	return r.nextMarker(), nil //nolint:nilerr // Damaged scan data ends the scan, keeping the blocks decoded (see above).
}

// baseline decodes one block of a sequential scan (T.81, F.2.2).
func (d *decoder) baseline(r *bitReader, s scanComp, coef []int16) error {
	t, err := s.dc.decode(r)
	if err != nil {
		return err
	}
	if t > maxCategory {
		return fmt.Errorf("%w: DC difference category %d", ErrMalformed, t)
	}
	s.c.pred += receiveExtend(r, int(t))
	coef[0] = clamp16(s.c.pred)
	for k := 1; k < 64; {
		rs, err := s.ac.decode(r)
		if err != nil {
			return err
		}
		run, size := int(rs>>4), int(rs&15)
		if size == 0 {
			if run != 15 {
				return nil
			}
			k += 16
			continue
		}
		k += run
		if k > 63 {
			return fmt.Errorf("%w: coefficient past the block", ErrMalformed)
		}
		coef[zigzag[k]] = clamp16(receiveExtend(r, size))
		k++
	}
	return nil
}

// dcFirst decodes the first DC scan of a progressive block (T.81, G.1.2.1).
func (d *decoder) dcFirst(r *bitReader, s scanComp, coef []int16, al int) error {
	t, err := s.dc.decode(r)
	if err != nil {
		return err
	}
	if t > maxCategory {
		return fmt.Errorf("%w: DC difference category %d", ErrMalformed, t)
	}
	s.c.pred += receiveExtend(r, int(t))
	coef[0] = clamp16(s.c.pred << al)
	return nil
}

// acFirst decodes a first AC scan of a progressive block (T.81, G.1.2.2).
func (d *decoder) acFirst(r *bitReader, s scanComp, coef []int16, ss, se, al int) error {
	if d.eobrun > 0 {
		d.eobrun--
		return nil
	}
	for k := ss; k <= se; {
		rs, err := s.ac.decode(r)
		if err != nil {
			return err
		}
		run, size := int(rs>>4), int(rs&15)
		if size == 0 {
			if run < 15 {
				d.eobrun = 1<<run - 1
				if run > 0 {
					d.eobrun += r.bits(run)
				}
				return nil
			}
			k += 16
			continue
		}
		k += run
		if k > se {
			return fmt.Errorf("%w: coefficient past the band", ErrMalformed)
		}
		coef[zigzag[k]] = clamp16(receiveExtend(r, size) << al)
		k++
	}
	return nil
}

// acRefine decodes an AC refinement scan of a progressive block (T.81,
// G.1.2.3): one correction bit for each coefficient already non-zero in the
// band, and new coefficients of magnitude 1 << al.
func (d *decoder) acRefine(r *bitReader, s scanComp, coef []int16, ss, se, al int) error {
	p1, m1 := int16(1)<<al, int16(-1)<<al
	correct := func(i int) {
		if r.bit() == 1 && coef[i]&p1 == 0 {
			if coef[i] >= 0 {
				coef[i] += p1
			} else {
				coef[i] += m1
			}
		}
	}
	k := ss
	if d.eobrun == 0 {
		for ; k <= se; k++ {
			rs, err := s.ac.decode(r)
			if err != nil {
				return err
			}
			run, size := int(rs>>4), int(rs&15)
			var value int16
			if size != 0 {
				value = m1
				if r.bit() == 1 {
					value = p1
				}
			} else if run != 15 {
				d.eobrun = 1 << run
				if run > 0 {
					d.eobrun += r.bits(run)
				}
				break
			}
			// Pass over the non-zero coefficients, correcting each, and run
			// zero ones: the new coefficient lands on the next zero.
			for ; k <= se; k++ {
				i := zigzag[k]
				if coef[i] != 0 {
					correct(i)
					continue
				}
				if run == 0 {
					break
				}
				run--
			}
			if value != 0 && k <= se {
				coef[zigzag[k]] = value
			}
		}
	}
	if d.eobrun > 0 {
		for ; k <= se; k++ {
			if i := zigzag[k]; coef[i] != 0 {
				correct(i)
			}
		}
		d.eobrun--
	}
	return nil
}

// clamp16 narrows a coefficient to the 16 bits its storage holds. Valid data
// never needs it: an 8-bit coefficient is at most 11 bits of magnitude.
func clamp16(v int) int16 {
	return int16(min(max(v, -32768), 32767)) //nolint:gosec // clamped to the int16 range.
}

// ceilDiv returns ceil(a / b) for positive b.
func ceilDiv(a, b int) int {
	return (a + b - 1) / b
}

// idctCos[x][u] is C(u) cos((2x + 1) u pi / 16) / 2, the factor of
// T.81, A.3.3 split between the row and column passes.
var idctCos = func() [8][8]float64 {
	var t [8][8]float64
	for x := range 8 {
		for u := range 8 {
			c := 1.0
			if u == 0 {
				c = 1 / math.Sqrt2
			}
			t[x][u] = c * math.Cos(float64(2*x+1)*float64(u)*math.Pi/16) / 2
		}
	}
	return t
}()

// idct dequantizes one block and returns its 64 samples, level shifted
// back and clamped (T.81, A.3.3 and A.3.1).
func idct(coef []int16, q *[64]int32, out *[64]byte) {
	var f, tmp [64]float64
	for i := range 64 {
		f[i] = float64(int32(coef[i]) * q[i])
	}
	for y := range 8 {
		for x := range 8 {
			s := 0.0
			for u := range 8 {
				s += idctCos[x][u] * f[y*8+u]
			}
			tmp[y*8+x] = s
		}
	}
	for x := range 8 {
		for y := range 8 {
			s := 0.0
			for v := range 8 {
				s += idctCos[y][v] * tmp[v*8+x]
			}
			out[y*8+x] = byte(min(max(math.Round(s+128), 0), 255))
		}
	}
}

// pixels reconstructs the samples of every component and assembles the
// output rows.
func (d *decoder) pixels(colorTransform int) ([]byte, error) {
	planes := make([][]byte, len(d.comps))
	var block [64]byte
	for ci, c := range d.comps {
		plane := make([]byte, c.bw*8*c.bh*8)
		stride := c.bw * 8
		for by := range c.bh {
			for bx := range c.bw {
				idct(c.coeffs[(by*c.bw+bx)*64:(by*c.bw+bx+1)*64], &d.quant[c.tq], &block)
				for y := range 8 {
					copy(plane[(by*8+y)*stride+bx*8:], block[y*8:y*8+8])
				}
			}
		}
		planes[ci] = plane
	}
	n := len(d.comps)
	transform := d.transform(colorTransform)
	out := make([]byte, d.width*d.height*n)
	sample := make([]byte, n)
	for y := range d.height {
		for x := range d.width {
			for ci, c := range d.comps {
				// The sample that covers the pixel, as a replicating
				// upsampler reads it.
				sx, sy := x*c.h/d.hmax, y*c.v/d.vmax
				sample[ci] = planes[ci][sy*c.bw*8+sx]
			}
			o := (y*d.width + x) * n
			switch {
			case n >= 3 && transform:
				rr, gg, bb := ycc(sample[0], sample[1], sample[2])
				if n == 3 {
					out[o], out[o+1], out[o+2] = rr, gg, bb
				} else {
					// YCCK: CMY are the complements of the RGB the YCC
					// components give, and K passes through (Adobe
					// Technical Note #5116, which ISO 32000-1, 7.4.8 follows).
					out[o], out[o+1], out[o+2], out[o+3] = 255-rr, 255-gg, 255-bb, sample[3]
				}
			default:
				copy(out[o:o+n], sample)
			}
		}
	}
	return out, nil
}

// transform reports whether the colour components are YCbCr or YCCK: as an
// Adobe marker says, else as /ColorTransform says, else by the default of
// ISO 32000-1, Table 13, 1 for three components and 0 otherwise. Three
// components named R, G and B are RGB whatever the default.
func (d *decoder) transform(colorTransform int) bool {
	n := len(d.comps)
	switch {
	case n < 3:
		return false
	case d.adobe:
		return d.adobeTransform != 0
	case colorTransform >= 0:
		return colorTransform != 0
	case n == 3:
		return d.comps[0].id != 'R' || d.comps[1].id != 'G' || d.comps[2].id != 'B'
	}
	return false
}

// ycc converts one YCbCr sample to RGB (JFIF, ITU-T T.871, 7).
func ycc(y, cb, cr byte) (byte, byte, byte) {
	fy, fcb, fcr := float64(y), float64(cb)-128, float64(cr)-128
	clamp := func(v float64) byte { return byte(min(max(math.Round(v), 0), 255)) }
	return clamp(fy + 1.402*fcr), clamp(fy - 0.344136*fcb - 0.714136*fcr), clamp(fy + 1.772*fcb)
}
