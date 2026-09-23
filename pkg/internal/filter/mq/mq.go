package mq

// state is one row of the probability estimation table: the LPS probability
// estimate Qe, the state that follows an MPS renormalisation (NMPS) or an LPS
// (NLPS), and whether an LPS in this state swaps the sense of the MPS.
type state struct {
	qe        uint32
	nmps      uint8
	nlps      uint8
	switchMPS bool
}

// qeTable mirrors ITU-T T.88, Table E.1 (Qe values and probability estimation
// process), row for row. Every NMPS and NLPS entry is itself an index into this
// table, so a [Context] can never reach an index outside it.
//
//nolint:gochecknoglobals // static spec table, ITU-T T.88 Table E.1
var qeTable = [47]state{
	{0x5601, 1, 1, true},
	{0x3401, 2, 6, false},
	{0x1801, 3, 9, false},
	{0x0AC1, 4, 12, false},
	{0x0521, 5, 29, false},
	{0x0221, 38, 33, false},
	{0x5601, 7, 6, true},
	{0x5401, 8, 14, false},
	{0x4801, 9, 14, false},
	{0x3801, 10, 14, false},
	{0x3001, 11, 17, false},
	{0x2401, 12, 18, false},
	{0x1C01, 13, 20, false},
	{0x1601, 29, 21, false},
	{0x5601, 15, 14, true},
	{0x5401, 16, 14, false},
	{0x5101, 17, 15, false},
	{0x4801, 18, 16, false},
	{0x3801, 19, 17, false},
	{0x3401, 20, 18, false},
	{0x3001, 21, 19, false},
	{0x2801, 22, 19, false},
	{0x2401, 23, 20, false},
	{0x2201, 24, 21, false},
	{0x1C01, 25, 22, false},
	{0x1801, 26, 23, false},
	{0x1601, 27, 24, false},
	{0x1401, 28, 25, false},
	{0x1201, 29, 26, false},
	{0x1101, 30, 27, false},
	{0x0AC1, 31, 28, false},
	{0x09C1, 32, 29, false},
	{0x08A1, 33, 30, false},
	{0x0521, 34, 31, false},
	{0x0441, 35, 32, false},
	{0x02A1, 36, 33, false},
	{0x0221, 37, 34, false},
	{0x0141, 38, 35, false},
	{0x0111, 39, 36, false},
	{0x0085, 40, 37, false},
	{0x0049, 41, 38, false},
	{0x0025, 42, 39, false},
	{0x0015, 43, 40, false},
	{0x0009, 44, 41, false},
	{0x0005, 45, 42, false},
	{0x0001, 45, 43, false},
	{0x5601, 46, 46, false},
}

// Context is the adaptive state of one coding context CX: the index I(CX) into
// the probability estimation table and the sense MPS(CX) of the more probable
// symbol. The zero value is the reset state of ITU-T T.88, E.3.7, index 0 with
// MPS 0, so a slice of contexts from make is ready to use.
type Context struct {
	index uint8
	mps   uint8
}

// Decoder decodes binary decisions from one arithmetically coded segment.
//
// A Decoder is not safe for concurrent use. The contexts it adapts belong to
// the caller, which decides when they are reset or carried across segments.
type Decoder struct {
	data []byte
	// bp indexes the byte most recently read into c: BP in ITU-T T.88, E.3.
	bp int
	// c holds Chigh in its upper 16 bits and Clow in its lower 16 bits.
	c  uint32
	a  uint32
	ct int
}

// NewDecoder starts decoding data, following INITDEC in ITU-T T.88, Figure
// E.20.
//
// Data that ends before the decisions a caller asks for does not fail: past
// the end the decoder reads 0xFF bytes, which BYTEIN treats as a terminating
// marker and answers by feeding 1-bits (E.3.4). A truncated segment therefore
// decodes to a definite result, and no read falls outside data.
func NewDecoder(data []byte) *Decoder {
	d := &Decoder{data: data}
	d.c = d.byteAt(0) << 16
	d.byteIn()
	d.c <<= 7
	d.ct -= 7
	d.a = 0x8000
	return d
}

// Decode returns the next decision, 0 or 1, coded in cx, and adapts cx. It
// follows DECODE in ITU-T T.88, Figure E.15, with the conditional exchanges of
// Figures E.16 and E.17.
//
// The loop in renormalisation always ends: A depends only on table entries,
// never on the data, and every Qe is at least 1, so at most 15 shifts restore
// A to 0x8000 or above.
func (d *Decoder) Decode(cx *Context) int {
	s := &qeTable[cx.index]
	d.a -= s.qe
	if d.c>>16 < s.qe {
		bit := d.lpsExchange(cx, s)
		d.renormalize()
		return bit
	}
	d.c -= s.qe << 16
	if d.a&0x8000 != 0 {
		return int(cx.mps)
	}
	bit := d.mpsExchange(cx, s)
	d.renormalize()
	return bit
}

// mpsExchange is MPS_EXCHANGE, ITU-T T.88 Figure E.16. The MPS sub-interval
// fell below the LPS one, so the MPS path decodes an LPS when A < Qe.
func (d *Decoder) mpsExchange(cx *Context, s *state) int {
	if d.a < s.qe {
		bit := int(1 - cx.mps)
		if s.switchMPS {
			cx.mps = 1 - cx.mps
		}
		cx.index = s.nlps
		return bit
	}
	bit := int(cx.mps)
	cx.index = s.nmps
	return bit
}

// lpsExchange is LPS_EXCHANGE, ITU-T T.88 Figure E.17. Both branches shrink A
// to Qe; when A < Qe the exchange happened and the LPS path decodes an MPS.
func (d *Decoder) lpsExchange(cx *Context, s *state) int {
	if d.a < s.qe {
		d.a = s.qe
		bit := int(cx.mps)
		cx.index = s.nmps
		return bit
	}
	d.a = s.qe
	bit := int(1 - cx.mps)
	if s.switchMPS {
		cx.mps = 1 - cx.mps
	}
	cx.index = s.nlps
	return bit
}

// renormalize is RENORMD, ITU-T T.88 Figure E.18.
func (d *Decoder) renormalize() {
	for {
		if d.ct == 0 {
			d.byteIn()
		}
		d.a <<= 1
		d.c <<= 1
		d.ct--
		if d.a&0x8000 != 0 {
			return
		}
	}
}

// byteIn is BYTEIN, ITU-T T.88 Figure E.19. A 0xFF followed by a byte above
// 0x8F is a marker: the pointer stays on it and 1-bits are fed. A 0xFF followed
// by anything else is a stuffed byte, whose leading bit carries into Chigh.
func (d *Decoder) byteIn() {
	if d.byteAt(d.bp) == 0xFF {
		if d.byteAt(d.bp+1) > 0x8F {
			d.c += 0xFF00
			d.ct = 8
			return
		}
		d.bp++
		d.c += d.byteAt(d.bp) << 9
		d.ct = 7
		return
	}
	d.bp++
	d.c += d.byteAt(d.bp) << 8
	d.ct = 8
}

// byteAt returns the byte at i, or 0xFF past the end of data. See
// [NewDecoder] for why 0xFF is the right answer there.
func (d *Decoder) byteAt(i int) uint32 {
	if i < len(d.data) {
		return uint32(d.data[i])
	}
	return 0xFF
}
