package dct

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"math/bits"
)

// errNotOptimizable is what the walk of [OptimizeHuffman] fails with on data
// it does not re-code: anything but well-formed baseline or extended
// sequential Huffman coding of 8-bit samples.
var errNotOptimizable = errors.New("not re-codable")

// maxBaselineBlocks is the most blocks one MCU of a sequential frame holds
// (ITU-T T.81, B.2.3).
const maxBaselineBlocks = 10

// maxCodeLength is the longest Huffman code T.81 allows (C.2, B.2.4.2).
const maxCodeLength = 16

// maxDCCategory and maxACSize are the largest DC difference category and AC
// coefficient size of 8-bit samples (T.81, F.1.2.1 and F.1.2.2, Tables F.1
// and F.2). A larger one is data no conforming encoder writes, which the
// walk refuses rather than re-codes.
const (
	maxDCCategory = 11
	maxACSize     = 10
)

// OptimizeHuffman re-codes a sequential Huffman-coded JPEG (ITU-T T.81,
// SOF0 or SOF1 with 8-bit samples) with Huffman tables built from the
// statistics of its own scans, as T.81, Annex K.2 describes, and returns the
// result when it is smaller than data. It is lossless: every block codes the
// same quantized coefficients, and with them every decoder reconstructs the
// same samples. Only the DHT and DRI segments and the entropy-coded data
// change; every other marker segment is kept, in its order and byte for byte,
// and each scan is preceded by one DHT segment holding the tables it uses.
//
// Restart intervals are dropped: the DRI segments and the restart markers go,
// and each DC coefficient is coded again as its difference from the previous
// block of its component across what were interval boundaries (T.81,
// F.1.1.5.1 resets the prediction at the start of each interval). An interval of
// zero, or none, is the scan without restarts (B.2.4.4), so the coefficients
// are unchanged. When a difference so derived falls outside the categories of
// 8-bit samples (Table F.1), which only data no encoder writes reaches, the
// restart intervals are kept and the DC differences coded as data codes them.
//
// ok is false, with a nil error, when data is not re-coded: it is
// progressive, arithmetic-coded, lossless or hierarchical, its samples are
// not 8 bits, it has a DNL marker or an unexpected one, any of its scans
// does not decode exactly to the marker that ends it (damaged, truncated or
// padded data), a restart marker is out of sequence, or the re-coding is not
// smaller. The error is only ever the context's.
//
// The result is verified before it is returned: its own walk must yield the
// same marker segments, but for DRI, and the same quantized coefficients,
// block by block, as data's, compared by digest.
//
// Progressive data is left as it is, though T.81, Annex G, codes it with
// Huffman tables an encoder may optimize too: measured against jpegtran,
// neither re-coding it with optimized tables and its own scan script nor
// converting it to sequential coding saves a noticeable amount.
//
// # Corpus evidence
//
//   - All corpora: 5,713 of the 5,892 images whose only filter is DCTDecode
//     are re-coded, saving 19.3 MB, and libjpeg-turbo's djpeg and [Decode]
//     read every one to the samples they read from its source.
//   - govdocs: the re-coding saves 100.2% of what jpegtran -optimize saves,
//     and 95.2% while it kept restart intervals.
//   - All corpora: 865 of 6,349 DCTDecode images have restart intervals,
//     56.4 MB. Dropping them saves 0.73 MB more than keeping them, re-codes
//     8 images keeping them did not shrink, and no derived DC difference
//     leaves Table F.1. djpeg reads every changed re-coding to the samples it
//     reads from its source.
//   - All corpora: 133 images are progressive, 6.04 MB. jpegtran -optimize
//     re-codes them with their own scan scripts 632 bytes smaller in all (5
//     images smaller), and sequentially 4.0% larger (8 images smaller, by
//     762 bytes): libjpeg optimizes the tables of every progressive JPEG it
//     writes.
func OptimizeHuffman(ctx context.Context, data []byte) ([]byte, bool, error) {
	out, ok, restarts, err := recode(ctx, data, false)
	if err != nil || ok || !restarts {
		return out, ok, err
	}
	out, ok, _, err = recode(ctx, data, true)
	return out, ok, err
}

// recode is [OptimizeHuffman], dropping data's restart intervals or keeping
// them. restarts reports whether data's walk met a restart marker, so that a
// re-coding that dropped them and was not kept can be tried again keeping
// them.
func recode(ctx context.Context, data []byte, keepRestarts bool) ([]byte, bool, bool, error) {
	stats := statsVisitor{coder: dcCoder{keepRestarts: keepRestarts}}
	origSum, err := walkSequential(ctx, data, &stats)
	if err != nil {
		return nil, false, false, notOptimizable(err)
	}
	if stats.coder.overflow {
		return nil, false, stats.restarts, nil
	}
	tables := make([]scanTables, len(stats.scans))
	for i, freq := range stats.scans {
		if tables[i], err = buildScanTables(freq); err != nil {
			return nil, false, stats.restarts, nil //nolint:nilerr // A table that cannot be built leaves the image as it is.
		}
	}
	enc := &encodeVisitor{tables: tables, coder: dcCoder{keepRestarts: keepRestarts}, out: make([]byte, 0, len(data))}
	if _, err := walkSequential(ctx, data, enc); err != nil {
		return nil, false, false, notOptimizable(err)
	}
	out := enc.out
	if len(out) >= len(data) {
		return nil, false, stats.restarts, nil
	}
	newSum, err := walkSequential(ctx, out, nopVisitor{})
	if err != nil {
		return nil, false, stats.restarts, notOptimizable(err)
	}
	if newSum != origSum {
		return nil, false, stats.restarts, nil
	}
	return out, true, stats.restarts, nil
}

// notOptimizable returns err when it is the context's, and nil for every
// other failure of the walk, which leaves the data as it is.
func notOptimizable(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return nil
}

// visitor receives what [walkSequential] reads. segment is every marker
// segment but DHT, marker and length included, and the SOI and EOI markers;
// dht is called with each DHT segment instead. scan is called with each
// scan's ordinal before its first block, dc with each block's quantized DC
// coefficient, the block's component given by its position in the scan
// header, ac with each coded AC symbol and its additional bits, restart with
// each restart marker, and endScan after a scan's last block.
type visitor interface {
	segment(seg []byte)
	dht()
	scan(index int, header []byte)
	dc(comp, slot, value int)
	ac(slot int, sym byte, extra uint16, n int)
	restart(marker byte)
	endScan()
	trailer(rest []byte)
}

// dcCoder codes the DC coefficients of a scan as the differences T.81,
// F.1.1.5.1 codes: from the previous block of the same component, from zero
// at the start of the scan and, when keepRestarts is set, of each restart
// interval. overflow is set when a difference falls outside the categories
// of 8-bit samples (Table F.1).
type dcCoder struct {
	keepRestarts bool
	pred         [maxComponents]int
	overflow     bool
}

// startScan resets the predictions at the start of a scan.
func (c *dcCoder) startScan() {
	c.pred = [maxComponents]int{}
}

// restart resets the predictions at a restart marker the coding keeps.
func (c *dcCoder) restart() {
	if c.keepRestarts {
		c.startScan()
	}
}

// code returns the category and additional bits of the difference that
// codes value for the scan's component comp (T.81, F.1.2.1.1 and Table
// F.1).
func (c *dcCoder) code(comp, value int) (byte, uint16) {
	diff := value - c.pred[comp]
	c.pred[comp] = value
	mag := diff
	if mag < 0 {
		mag = -mag
	}
	t := bits.Len(uint(mag))
	if t > maxDCCategory {
		c.overflow = true
		return 0, 0
	}
	// A negative difference is coded as its ones' complement in t bits.
	if diff < 0 {
		diff += 1<<t - 1
	}
	return byte(t), uint16(diff) //nolint:gosec // t is at most maxDCCategory and 0 <= diff < 1<<t.
}

// extendDC returns the DC difference a category t and its additional bits
// code (T.81, F.2.2.1, EXTEND).
func extendDC(t int, extra uint16) int {
	if t == 0 {
		return 0
	}
	v := int(extra)
	if v < 1<<(t-1) {
		v -= 1<<t - 1
	}
	return v
}

// seqFrame is the frame a sequential walk lays its scans out on.
type seqFrame struct {
	width, height int
	hmax, vmax    int
	mcusX, mcusY  int
	extended      bool
	comps         []seqComp
}

// seqComp is one component of a sequential frame.
type seqComp struct {
	id            byte
	h, v          int
	width, height int
}

// walkSequential walks data as a sequential Huffman-coded JPEG, strictly,
// telling v what it reads, and returns a digest of everything but its
// Huffman tables and restart intervals: the other marker segments, and for
// each scan the quantized DC coefficient and the AC symbols and additional
// bits of each block. Two JPEGs with the same digest decode to the same
// coefficients.
func walkSequential(ctx context.Context, data []byte, v visitor) ([32]byte, error) {
	var sum [32]byte
	h := sha256.New()
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return sum, fmt.Errorf("%w: no start-of-image marker", errNotOptimizable)
	}
	v.segment(data[:2])
	hashBytes(h, 'S', data[:2])
	var (
		frame    *seqFrame
		dc, ac   [4]*huffman
		interval int
		scans    int
	)
	p := 2
	for {
		// Fill bytes may precede any marker (T.81, B.1.1.2).
		for p+1 < len(data) && data[p] == 0xFF && data[p+1] == 0xFF {
			p++
		}
		if p+1 >= len(data) || data[p] != 0xFF {
			return sum, fmt.Errorf("%w: no marker at %d", errNotOptimizable, p)
		}
		m := data[p+1]
		if m == 0xD9 {
			if frame == nil || scans == 0 {
				return sum, fmt.Errorf("%w: no scan", errNotOptimizable)
			}
			v.segment(data[p : p+2])
			hashBytes(h, 'S', data[p:p+2])
			v.trailer(data[p+2:])
			hashBytes(h, 'T', data[p+2:])
			copy(sum[:], h.Sum(nil))
			return sum, nil
		}
		if p+4 > len(data) {
			return sum, fmt.Errorf("%w: segment past the data", errNotOptimizable)
		}
		length := int(binary.BigEndian.Uint16(data[p+2:]))
		if length < 2 || p+2+length > len(data) {
			return sum, fmt.Errorf("%w: segment 0x%02X of %d bytes past the data", errNotOptimizable, m, length)
		}
		seg, body := data[p:p+2+length], data[p+4:p+2+length]
		switch {
		case m == 0xC0 || m == 0xC1:
			if frame != nil {
				return sum, fmt.Errorf("%w: a second frame", errNotOptimizable)
			}
			f, err := parseSeqFrame(body, m == 0xC1)
			if err != nil {
				return sum, err
			}
			frame = f
		case m == 0xC4:
			if err := readTables(body, frame, &dc, &ac); err != nil {
				return sum, err
			}
			v.dht()
			p += 2 + length
			continue
		case m == 0xDD:
			if len(body) != 2 {
				return sum, fmt.Errorf("%w: DRI segment of %d bytes", errNotOptimizable, len(body))
			}
			interval = int(binary.BigEndian.Uint16(body))
			// Restart intervals change how the coefficients are coded,
			// not what they are, so the digest leaves them out.
			v.segment(seg)
			p += 2 + length
			continue
		case m == 0xDB, m >= 0xE0 && m <= 0xEF, m == 0xFE:
			// Quantization tables, application data and comments are kept.
		case m == 0xDA:
			if frame == nil {
				return sum, fmt.Errorf("%w: scan before frame", errNotOptimizable)
			}
			hashBytes(h, 'S', seg)
			v.segment(seg)
			v.scan(scans, seg)
			end, err := walkScan(ctx, data, p+2+length, frame, body, interval, &dc, &ac, v, h)
			if err != nil {
				return sum, err
			}
			v.endScan()
			scans++
			p = end
			continue
		default:
			// Any other frame, DAC, DNL, a restart marker outside a scan,
			// a reserved marker: nothing this re-coder vouches for.
			return sum, fmt.Errorf("%w: marker 0x%02X", errNotOptimizable, m)
		}
		hashBytes(h, 'S', seg)
		v.segment(seg)
		p += 2 + length
	}
}

// hashBytes adds a tagged, length-prefixed byte string to h.
func hashBytes(h hash.Hash, tag byte, b []byte) {
	var hdr [9]byte
	hdr[0] = tag
	binary.BigEndian.PutUint64(hdr[1:], uint64(len(b)))
	h.Write(hdr[:])
	h.Write(b)
}

// parseSeqFrame reads a sequential start-of-frame segment (T.81, B.2.2).
func parseSeqFrame(b []byte, extended bool) (*seqFrame, error) {
	if len(b) < 6 || b[0] != 8 {
		return nil, fmt.Errorf("%w: frame header", errNotOptimizable)
	}
	f := &seqFrame{
		height:   int(binary.BigEndian.Uint16(b[1:])),
		width:    int(binary.BigEndian.Uint16(b[3:])),
		extended: extended,
		hmax:     1,
		vmax:     1,
	}
	n := int(b[5])
	// A height of zero is given later by a DNL marker (T.81, B.2.5), which
	// the walk does not follow.
	if f.width == 0 || f.height == 0 || n < 1 || n > maxComponents || len(b) != 6+3*n {
		return nil, fmt.Errorf("%w: frame of %dx%d with %d components", errNotOptimizable, f.width, f.height, n)
	}
	for i := range n {
		c := seqComp{id: b[6+3*i], h: int(b[7+3*i] >> 4), v: int(b[7+3*i] & 15)}
		if c.h < 1 || c.h > 4 || c.v < 1 || c.v > 4 || b[8+3*i] > 3 {
			return nil, fmt.Errorf("%w: component sampling", errNotOptimizable)
		}
		for _, o := range f.comps {
			if o.id == c.id {
				return nil, fmt.Errorf("%w: component %d repeated", errNotOptimizable, c.id)
			}
		}
		f.hmax, f.vmax = max(f.hmax, c.h), max(f.vmax, c.v)
		f.comps = append(f.comps, c)
	}
	f.mcusX = ceilDiv(f.width, 8*f.hmax)
	f.mcusY = ceilDiv(f.height, 8*f.vmax)
	for i := range f.comps {
		c := &f.comps[i]
		c.width = ceilDiv(f.width*c.h, f.hmax)
		c.height = ceilDiv(f.height*c.v, f.vmax)
	}
	return f, nil
}

// readTables reads a DHT segment (T.81, B.2.4.2) into dc and ac. A baseline
// frame has two tables of each class (B.2.4.2, Table B.5).
func readTables(b []byte, frame *seqFrame, dc, ac *[4]*huffman) error {
	slots := 4
	if frame != nil && !frame.extended {
		slots = 2
	}
	if len(b) == 0 {
		return fmt.Errorf("%w: empty DHT segment", errNotOptimizable)
	}
	for len(b) > 0 {
		if len(b) < 17 {
			return fmt.Errorf("%w: DHT segment", errNotOptimizable)
		}
		tc, th := b[0]>>4, int(b[0]&15)
		var counts [16]byte
		copy(counts[:], b[1:17])
		total := 0
		for _, c := range counts {
			total += int(c)
		}
		if tc > 1 || th >= slots || len(b) < 17+total {
			return fmt.Errorf("%w: Huffman table %d/%d", errNotOptimizable, tc, th)
		}
		t, err := newHuffman(counts, b[17:17+total])
		if err != nil {
			return fmt.Errorf("%w: %w", errNotOptimizable, err)
		}
		if tc == 0 {
			dc[th] = t
		} else {
			ac[th] = t
		}
		b = b[17+total:]
	}
	return nil
}

// seqScanComp is one component of a sequential scan with its table slots.
type seqScanComp struct {
	c      *seqComp
	dc, ac int
}

// walkScan walks the entropy-coded data of one sequential scan, whose header
// body is hdr and whose data starts at start, and returns the position of the
// marker that ends it. Every unit must decode within the data, each restart
// interval must end, but for the bits that pad its last byte, at the restart
// marker the sequence expects (T.81, B.2.1 and F.1.2.3), and the scan at a
// marker that is not one.
func walkScan(
	ctx context.Context,
	data []byte,
	start int,
	frame *seqFrame,
	hdr []byte,
	interval int,
	dc, ac *[4]*huffman,
	v visitor,
	h hash.Hash,
) (int, error) {
	if len(hdr) < 1 {
		return 0, fmt.Errorf("%w: scan header", errNotOptimizable)
	}
	ns := int(hdr[0])
	if ns < 1 || ns > len(frame.comps) || len(hdr) != 1+2*ns+3 {
		return 0, fmt.Errorf("%w: scan header", errNotOptimizable)
	}
	slots := 4
	if !frame.extended {
		slots = 2
	}
	var (
		sc     []seqScanComp
		blocks int
	)
	for i := range ns {
		id, t := hdr[1+2*i], hdr[2+2*i]
		k := -1
		for j := range frame.comps {
			if frame.comps[j].id == id {
				k = j
			}
		}
		for _, o := range sc {
			if k >= 0 && o.c == &frame.comps[k] {
				k = -1
			}
		}
		td, ta := int(t>>4), int(t&15)
		if k < 0 || td >= slots || ta >= slots || dc[td] == nil || ac[ta] == nil {
			return 0, fmt.Errorf("%w: scan component %d", errNotOptimizable, id)
		}
		c := &frame.comps[k]
		sc = append(sc, seqScanComp{c: c, dc: td, ac: ta})
		blocks += c.h * c.v
	}
	// A sequential scan codes every coefficient at full precision (T.81,
	// B.2.3).
	if hdr[1+2*ns] != 0 || hdr[2+2*ns] != 63 || hdr[3+2*ns] != 0 {
		return 0, fmt.Errorf("%w: spectral selection", errNotOptimizable)
	}
	units, perUnit := frame.mcusX*frame.mcusY, blocks
	if ns == 1 {
		c := sc[0].c
		units, perUnit = ceilDiv(c.width, 8)*ceilDiv(c.height, 8), 1
	} else if blocks > maxBaselineBlocks {
		return 0, fmt.Errorf("%w: MCU of %d blocks", errNotOptimizable, blocks)
	}

	r := &strictBits{data: data, pos: start}
	preds := make([]int, ns)
	sinceCtx := 0
	for u := range units {
		if interval > 0 && u > 0 && u%interval == 0 {
			marker := byte(0xD0 + (u/interval-1)%8)
			if err := r.restart(marker); err != nil {
				return 0, err
			}
			v.restart(marker)
			for i := range preds {
				preds[i] = 0
			}
		}
		if sinceCtx += perUnit; sinceCtx >= ctxBlocks {
			sinceCtx = 0
			if err := ctx.Err(); err != nil {
				return 0, err
			}
		}
		for i, s := range sc {
			n := s.c.h * s.c.v
			if ns == 1 {
				n = 1
			}
			for range n {
				if err := walkBlock(r, dc[s.dc], ac[s.ac], s, i, &preds[i], v, h); err != nil {
					return 0, err
				}
			}
		}
	}
	return r.end()
}

// walkBlock walks the symbols of one block of a sequential scan (T.81,
// F.2.2.1 and F.2.2.2), the block of the scan's component comp, whose DC
// prediction is pred (F.2.1.3.1).
func walkBlock(r *strictBits, dcT, acT *huffman, s seqScanComp, comp int, pred *int, v visitor, h hash.Hash) error {
	t, err := dcT.decodeStrict(r)
	if err != nil {
		return err
	}
	if t > maxDCCategory {
		return fmt.Errorf("%w: DC category %d", errNotOptimizable, t)
	}
	extra, err := r.bits(int(t))
	if err != nil {
		return err
	}
	*pred += extendDC(int(t), extra)
	v.dc(comp, s.dc, *pred)
	var dcHash [9]byte
	dcHash[0] = 'D'
	binary.BigEndian.PutUint64(dcHash[1:], uint64(*pred)) //nolint:gosec // Two's complement bits, hashed rather than read as a size.
	h.Write(dcHash[:])
	acSlot := s.ac
	for k := 1; k < 64; {
		rs, err := acT.decodeStrict(r)
		if err != nil {
			return err
		}
		run, size := int(rs>>4), int(rs&15)
		switch {
		case size == 0 && run == 0:
			v.ac(acSlot, rs, 0, 0)
			hashSymbol(h, 'A', rs, 0)
			return nil
		case size == 0 && run == 15:
			k += 16
			if k > 64 {
				return fmt.Errorf("%w: run past the block", errNotOptimizable)
			}
			v.ac(acSlot, rs, 0, 0)
			hashSymbol(h, 'A', rs, 0)
			continue
		case size == 0 || size > maxACSize:
			return fmt.Errorf("%w: AC symbol 0x%02X", errNotOptimizable, rs)
		}
		k += run
		if k > 63 {
			return fmt.Errorf("%w: coefficient past the block", errNotOptimizable)
		}
		extra, err := r.bits(size)
		if err != nil {
			return err
		}
		v.ac(acSlot, rs, extra, size)
		hashSymbol(h, 'A', rs, extra)
		k++
	}
	return nil
}

// hashSymbol adds one symbol and its additional bits to h.
func hashSymbol(h hash.Hash, tag, sym byte, extra uint16) {
	h.Write(binary.BigEndian.AppendUint16([]byte{tag, sym}, extra))
}

// strictBits reads entropy-coded data (T.81, F.1.2.3) and fails, rather
// than reading zeros, past the marker that ends it.
type strictBits struct {
	data []byte
	pos  int
	cur  byte
	left int
}

// bit reads one bit.
func (r *strictBits) bit() (int, error) {
	if r.left == 0 {
		if r.pos >= len(r.data) {
			return 0, fmt.Errorf("%w: scan data ends early", errNotOptimizable)
		}
		b := r.data[r.pos]
		if b == 0xFF {
			if r.pos+1 >= len(r.data) || r.data[r.pos+1] != 0x00 {
				return 0, fmt.Errorf("%w: scan data ends early", errNotOptimizable)
			}
			r.pos += 2
		} else {
			r.pos++
		}
		r.cur, r.left = b, 8
	}
	r.left--
	return int(r.cur>>r.left) & 1, nil
}

// bits reads n bits, at most 16, most significant first.
func (r *strictBits) bits(n int) (uint16, error) {
	var v uint16
	for range n {
		b, err := r.bit()
		if err != nil {
			return 0, err
		}
		v = v<<1 | uint16(b) //nolint:gosec // b is 0 or 1.
	}
	return v, nil
}

// atMarker drops the bits that pad the current byte and the fill bytes that
// may precede a marker, and returns the marker's position, failing when a
// byte of data comes first.
func (r *strictBits) atMarker() (int, error) {
	r.left = 0
	p := r.pos
	for p+1 < len(r.data) && r.data[p] == 0xFF && r.data[p+1] == 0xFF {
		p++
	}
	if p+1 >= len(r.data) || r.data[p] != 0xFF || r.data[p+1] == 0x00 {
		return 0, fmt.Errorf("%w: data after the last unit", errNotOptimizable)
	}
	return p, nil
}

// restart moves past the restart marker that ends an interval, which must
// be want.
func (r *strictBits) restart(want byte) error {
	p, err := r.atMarker()
	if err != nil {
		return err
	}
	if r.data[p+1] != want {
		return fmt.Errorf("%w: restart marker 0x%02X where 0x%02X was due", errNotOptimizable, r.data[p+1], want)
	}
	r.pos = p + 2
	return nil
}

// end returns the position of the marker that ends the scan, which must not
// be a restart marker.
func (r *strictBits) end() (int, error) {
	p, err := r.atMarker()
	if err != nil {
		return 0, err
	}
	if m := r.data[p+1]; m >= 0xD0 && m <= 0xD7 {
		return 0, fmt.Errorf("%w: restart marker after the last unit", errNotOptimizable)
	}
	return p, nil
}

// decodeStrict reads one value (T.81, F.2.2.3, DECODE) from a strict reader.
func (h *huffman) decodeStrict(r *strictBits) (byte, error) {
	code := 0
	for l := 1; l <= maxCodeLength; l++ {
		b, err := r.bit()
		if err != nil {
			return 0, err
		}
		code = code<<1 | b
		if h.maxCode[l] >= 0 && code <= h.maxCode[l] {
			return h.values[h.valPtr[l]+code-h.minCode[l]], nil
		}
	}
	return 0, fmt.Errorf("%w: invalid Huffman code", errNotOptimizable)
}

// scanFreqs counts the symbols one scan codes under each table slot.
type scanFreqs struct {
	dc, ac         [4][256]int64
	usedDC, usedAC [4]bool
}

// statsVisitor counts the symbols every scan codes, its DC differences
// derived by coder, and records whether a restart marker was met.
type statsVisitor struct {
	scans    []*scanFreqs
	coder    dcCoder
	restarts bool
}

func (*statsVisitor) segment([]byte) {}
func (*statsVisitor) dht()           {}
func (s *statsVisitor) scan(int, []byte) {
	s.scans = append(s.scans, &scanFreqs{})
	s.coder.startScan()
}

func (s *statsVisitor) dc(comp, slot, value int) {
	sym, _ := s.coder.code(comp, value)
	f := s.scans[len(s.scans)-1]
	f.dc[slot][sym]++
	f.usedDC[slot] = true
}

func (s *statsVisitor) ac(slot int, sym byte, _ uint16, _ int) {
	f := s.scans[len(s.scans)-1]
	f.ac[slot][sym]++
	f.usedAC[slot] = true
}

func (s *statsVisitor) restart(byte) {
	s.restarts = true
	s.coder.restart()
}

func (*statsVisitor) endScan() {}

func (*statsVisitor) trailer([]byte) {}

// nopVisitor is the visitor of a walk run for its digest alone.
type nopVisitor struct{}

func (nopVisitor) segment([]byte) {}

func (nopVisitor) dht() {}

func (nopVisitor) scan(int, []byte) {}

func (nopVisitor) dc(int, int, int) {}

func (nopVisitor) ac(int, byte, uint16, int) {}

func (nopVisitor) restart(byte) {}

func (nopVisitor) endScan() {}

func (nopVisitor) trailer([]byte) {}

// codeTable is one optimized Huffman table: the counts and values its DHT
// entry carries, and the code and length of each value.
type codeTable struct {
	counts [16]byte
	values []byte
	code   [256]uint16
	size   [256]int
}

// scanTables are the tables one scan is re-coded with, by slot, nil for a
// slot the scan does not use.
type scanTables struct {
	dc, ac [4]*codeTable
}

// buildScanTables builds the optimal tables of one scan's symbol counts.
func buildScanTables(f *scanFreqs) (scanTables, error) {
	var t scanTables
	for slot := range 4 {
		var err error
		if f.usedDC[slot] {
			if t.dc[slot], err = optimalTable(&f.dc[slot]); err != nil {
				return t, err
			}
		}
		if f.usedAC[slot] {
			if t.ac[slot], err = optimalTable(&f.ac[slot]); err != nil {
				return t, err
			}
		}
	}
	return t, nil
}

// maxTreeDepth bounds the code lengths [optimalTable] counts before T.81,
// K.3 limits them to 16 bits, as libjpeg's MAX_CLEN does. A deeper tree
// takes symbol counts that grow like the Fibonacci numbers; the counts are
// then halved, which costs a little compression and keeps the table valid.
const maxTreeDepth = 32

// optimalTable builds a Huffman table for the symbol counts freq by the
// procedure of T.81, Annex K.2: code sizes (Figure K.1), their counts (K.2),
// limited to 16 bits (K.3), and the values sorted by code size (K.4). A
// reserved symbol of count one keeps the all-ones code out of the table, as
// T.81 requires (C.2).
func optimalTable(freq *[256]int64) (*codeTable, error) {
	var counts [257]int64
	copy(counts[:256], freq[:])
	counts[256] = 1
	for range 64 {
		sizes, ok := codeSizes(counts)
		if ok {
			return tableFromSizes(sizes)
		}
		for i, c := range counts {
			if c > 1 {
				counts[i] = (c + 1) / 2
			}
		}
	}
	return nil, fmt.Errorf("%w: Huffman code sizes do not converge", errNotOptimizable)
}

// codeSizes runs T.81, Figure K.1 over freq, and reports false when a size
// passes maxTreeDepth.
func codeSizes(freq [257]int64) ([257]int, bool) {
	var (
		size   [257]int
		others [257]int
	)
	for i := range others {
		others[i] = -1
	}
	for {
		v1, f1, ok := leastCount(&freq, -1)
		if !ok {
			break
		}
		v2, f2, ok := leastCount(&freq, v1)
		if !ok {
			break
		}
		freq[v1], freq[v2] = f1+f2, 0
		for size[v1]++; others[v1] >= 0; size[v1]++ {
			v1 = others[v1]
		}
		others[v1] = v2
		for size[v2]++; others[v2] >= 0; size[v2]++ {
			v2 = others[v2]
		}
	}
	for _, s := range size {
		if s > maxTreeDepth {
			return size, false
		}
	}
	return size, true
}

// leastCount returns the symbol of freq, other than skip, with the least
// count above zero, the larger symbol on a tie (T.81, Figure K.1), and that
// count, or false when there is none.
func leastCount(freq *[257]int64, skip int) (int, int64, bool) {
	sym, least := -1, int64(0)
	for i, f := range freq {
		if i != skip && f > 0 && (sym < 0 || f <= least) {
			sym, least = i, f
		}
	}
	return sym, least, sym >= 0
}

// tableFromSizes turns code sizes into a table: counts per size limited to
// 16 bits (T.81, Figure K.3), the reserved symbol removed, the values sorted
// by size (Figure K.4), and the codes generated (C.1, C.2).
func tableFromSizes(size [257]int) (*codeTable, error) {
	var bits [maxTreeDepth + 1]int
	for _, s := range size {
		if s > 0 {
			bits[s]++
		}
	}
	for i := maxTreeDepth; i > maxCodeLength; i-- {
		for bits[i] > 0 {
			j := i - 2
			for j > 0 && bits[j] == 0 {
				j--
			}
			if j <= 0 {
				return nil, fmt.Errorf("%w: Huffman code sizes", errNotOptimizable)
			}
			bits[i] -= 2
			bits[i-1]++
			bits[j+1] += 2
			bits[j]--
		}
	}
	// The reserved symbol holds one of the longest codes; drop one of them.
	i := maxCodeLength
	for i > 0 && bits[i] == 0 {
		i--
	}
	bits[i]--

	t := &codeTable{}
	for l := 1; l <= maxCodeLength; l++ {
		t.counts[l-1] = byte(bits[l]) //nolint:gosec // At most 256 codes of one length, 255 once the reserved one is gone.
	}
	for l := 1; l <= maxTreeDepth; l++ {
		for v := range 256 {
			if size[v] == l {
				t.values = append(t.values, byte(v)) //nolint:gosec // v is below 256.
			}
		}
	}
	total := 0
	for _, c := range t.counts {
		total += int(c)
	}
	if total != len(t.values) {
		return nil, fmt.Errorf("%w: %d codes for %d values", errNotOptimizable, total, len(t.values))
	}
	t.assignCodes()
	return t, nil
}

// assignCodes generates the code of each value from the counts per code
// length (T.81, C.1 and C.2): the codes of one length are consecutive, and
// those of the next length continue one bit longer.
func (t *codeTable) assignCodes() {
	code, k := 0, 0
	for l := 1; l <= maxCodeLength; l++ {
		for range int(t.counts[l-1]) {
			v := t.values[k]
			t.code[v], t.size[v] = uint16(code), l //nolint:gosec // code fits l <= 16 bits.
			code++
			k++
		}
		code <<= 1
	}
}

// dhtSegment returns the DHT segment carrying the tables of t.
func (t scanTables) dhtSegment() []byte {
	body := []byte{}
	add := func(class byte, slot int, ct *codeTable) {
		if ct == nil {
			return
		}
		body = append(body, class<<4|byte(slot)) //nolint:gosec // slot is below 4.
		body = append(body, ct.counts[:]...)
		body = append(body, ct.values...)
	}
	for slot := range 4 {
		add(0, slot, t.dc[slot])
	}
	for slot := range 4 {
		add(1, slot, t.ac[slot])
	}
	seg := make([]byte, 4, 4+len(body))
	seg[0], seg[1] = 0xFF, 0xC4
	binary.BigEndian.PutUint16(seg[2:], uint16(2+len(body))) //nolint:gosec // At most 8 tables of 17+256 bytes.
	return append(seg, body...)
}

// encodeVisitor writes the walked JPEG back with the scans' optimized
// tables, its DC differences derived by coder, and without its restart
// intervals unless coder keeps them.
type encodeVisitor struct {
	tables []scanTables
	cur    scanTables
	coder  dcCoder
	bitWriter
}

func (e *encodeVisitor) segment(seg []byte) {
	if len(seg) >= 2 && seg[1] == 0xDA {
		// The scan's tables go right before its header, which scan
		// receives next; the header itself is written there.
		return
	}
	if len(seg) >= 2 && seg[1] == 0xDD && !e.coder.keepRestarts {
		return
	}
	e.out = append(e.out, seg...)
}

func (*encodeVisitor) dht() {}

func (e *encodeVisitor) scan(index int, header []byte) {
	e.cur = e.tables[index]
	e.coder.startScan()
	e.out = append(e.out, e.cur.dhtSegment()...)
	e.out = append(e.out, header...)
}

func (e *encodeVisitor) dc(comp, slot, value int) {
	sym, extra := e.coder.code(comp, value)
	t := e.cur.dc[slot]
	e.put(uint32(t.code[sym]), t.size[sym])
	if sym > 0 {
		e.put(uint32(extra), int(sym))
	}
}

func (e *encodeVisitor) ac(slot int, sym byte, extra uint16, n int) {
	t := e.cur.ac[slot]
	e.put(uint32(t.code[sym]), t.size[sym])
	if n > 0 {
		e.put(uint32(extra), n)
	}
}

func (e *encodeVisitor) restart(marker byte) {
	if !e.coder.keepRestarts {
		return
	}
	e.coder.restart()
	e.flush()
	e.out = append(e.out, 0xFF, marker)
}

func (e *encodeVisitor) endScan() { e.flush() }

func (e *encodeVisitor) trailer(rest []byte) { e.out = append(e.out, rest...) }

// bitWriter writes entropy-coded data (T.81, F.1.2.3): bits most
// significant first, a zero byte stuffed after each 0xFF.
type bitWriter struct {
	out []byte
	acc uint32
	n   int
}

// put writes the n low bits of v, n at most 16, most significant first,
// stuffing a zero byte after each 0xFF (T.81, F.1.2.3).
func (e *bitWriter) put(v uint32, n int) {
	e.acc = e.acc<<n | v&(1<<n-1)
	e.n += n
	for e.n >= 8 {
		b := byte(e.acc >> (e.n - 8)) //nolint:gosec // The top byte of the bits held.
		e.out = append(e.out, b)
		if b == 0xFF {
			e.out = append(e.out, 0x00)
		}
		e.n -= 8
	}
	e.acc &= 1<<e.n - 1
}

// flush pads the last byte with one bits (T.81, F.1.2.3).
func (e *bitWriter) flush() {
	if e.n > 0 {
		e.put(1<<(8-e.n)-1, 8-e.n)
	}
}
