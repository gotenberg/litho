package jbig2

import "fmt"

// qeEntry is a single entry in the QE probability estimation table.
// Fields: qeValue, nextMPS index, nextLPS index, switchFlag.
type qeEntry struct {
	qe      int
	nextMPS int
	nextLPS int
	sw      int
}

// qeTable is the 47-entry QE table from ISO 14492 Annex E.3.
var qeTable = [47]qeEntry{
	{0x5601, 1, 1, 1},
	{0x3401, 2, 6, 0},
	{0x1801, 3, 9, 0},
	{0x0AC1, 4, 12, 0},
	{0x0521, 5, 29, 0},
	{0x0221, 38, 33, 0},
	{0x5601, 7, 6, 1},
	{0x5401, 8, 14, 0},
	{0x4801, 9, 14, 0},
	{0x3801, 10, 14, 0},
	{0x3001, 11, 17, 0},
	{0x2401, 12, 18, 0},
	{0x1C01, 13, 20, 0},
	{0x1601, 29, 21, 0},
	{0x5601, 15, 14, 1},
	{0x5401, 16, 14, 0},
	{0x5101, 17, 15, 0},
	{0x4801, 18, 16, 0},
	{0x3801, 19, 17, 0},
	{0x3401, 20, 18, 0},
	{0x3001, 21, 19, 0},
	{0x2801, 22, 19, 0},
	{0x2401, 23, 20, 0},
	{0x2201, 24, 21, 0},
	{0x1C01, 25, 22, 0},
	{0x1801, 26, 23, 0},
	{0x1601, 27, 24, 0},
	{0x1401, 28, 25, 0},
	{0x1201, 29, 26, 0},
	{0x1101, 30, 27, 0},
	{0x0AC1, 31, 28, 0},
	{0x09C1, 32, 29, 0},
	{0x08A1, 33, 30, 0},
	{0x0521, 34, 31, 0},
	{0x0441, 35, 32, 0},
	{0x02A1, 36, 33, 0},
	{0x0221, 37, 34, 0},
	{0x0141, 38, 35, 0},
	{0x0111, 39, 36, 0},
	{0x0085, 40, 37, 0},
	{0x0049, 41, 38, 0},
	{0x0025, 42, 39, 0},
	{0x0015, 43, 40, 0},
	{0x0009, 44, 41, 0},
	{0x0005, 45, 42, 0},
	{0x0001, 45, 43, 0},
	{0x5601, 46, 46, 0},
}

// CX holds the context state for arithmetic decoding.
// Each context has a QE table index (cx) and a most-probable-symbol bit (mps).
type CX struct {
	cx    []byte // QE table index per context
	mps   []byte // MPS value per context (0 or 1)
	index int    // current context index
}

// NewCX creates a new context with the given number of entries.
func NewCX(size, startIndex int) *CX {
	return &CX{
		cx:    make([]byte, size),
		mps:   make([]byte, size),
		index: startIndex,
	}
}

// SetIndex sets the active context index.
func (c *CX) SetIndex(idx int) { c.index = idx }

// Index returns the current context index.
func (c *CX) Index() int { return c.index }

func (c *CX) cxVal() int  { return int(c.cx[c.index]) & 0x7f }
func (c *CX) mpsVal() int { return int(c.mps[c.index]) }
func (c *CX) setCx(v int) { c.cx[c.index] = byte(v & 0x7f) }
func (c *CX) toggleMps()  { c.mps[c.index] ^= 1 }

// ArithmeticDecoder implements the QM arithmetic decoder (ISO 14492 Annex E.3).
type ArithmeticDecoder struct {
	r          *Reader
	a          int   // interval register (16-bit effective)
	c          int64 // code register (32-bit effective, stored in int64)
	ct         int   // bit counter
	b          int   // current byte
	streamPos0 int64 // initial stream position
}

// NewArithmeticDecoder creates and initializes an arithmetic decoder from the given reader.
func NewArithmeticDecoder(r *Reader) (*ArithmeticDecoder, error) {
	ad := &ArithmeticDecoder{r: r}
	if err := ad.init(); err != nil {
		return nil, fmt.Errorf("jbig2: arithmetic decoder init: %w", err)
	}
	return ad, nil
}

func (ad *ArithmeticDecoder) init() error {
	ad.streamPos0 = ad.r.Position()
	ad.b = max(ad.r.Read(), 0)

	ad.c = int64(ad.b) << 16

	if err := ad.byteIn(); err != nil {
		return err
	}

	ad.c <<= 7
	ad.ct -= 7
	ad.a = 0x8000
	return nil
}

// Decode decodes a single bit using the given context.
func (ad *ArithmeticDecoder) Decode(cx *CX) (int, error) {
	qe := qeTable[cx.cxVal()]
	qeVal := qe.qe
	icx := cx.cxVal()

	ad.a -= qeVal

	if (ad.c >> 16) < int64(qeVal) {
		d := ad.lpsExchange(cx, icx, qeVal)
		if err := ad.renormalize(); err != nil {
			return 0, err
		}
		return d, nil
	}

	ad.c -= int64(qeVal) << 16
	if ad.a&0x8000 == 0 {
		d := ad.mpsExchange(cx, icx)
		if err := ad.renormalize(); err != nil {
			return 0, err
		}
		return d, nil
	}

	return cx.mpsVal(), nil
}

func (ad *ArithmeticDecoder) byteIn() error {
	if ad.r.Position() > ad.streamPos0 {
		ad.r.SeekTo(ad.r.Position() - 1)
	}

	ad.b = ad.r.Read()
	if ad.b < 0 {
		ad.b = 0xff
	}

	if ad.b == 0xFF {
		b1 := ad.r.Read()
		if b1 < 0 {
			b1 = 0xff
		}
		if b1 > 0x8f {
			ad.c += 0xff00
			ad.ct = 8
			if ad.r.Position() >= 2 {
				ad.r.SeekTo(ad.r.Position() - 2)
			}
		} else {
			ad.c += int64(b1) << 9
			ad.ct = 7
		}
	} else {
		ad.b = max(ad.r.Read(), 0)
		ad.c += int64(ad.b) << 8
		ad.ct = 8
	}

	ad.c &= 0xffffffff
	return nil
}

func (ad *ArithmeticDecoder) renormalize() error {
	for {
		if ad.ct == 0 {
			if err := ad.byteIn(); err != nil {
				return err
			}
		}

		ad.a <<= 1
		ad.c <<= 1
		ad.ct--

		if ad.a&0x8000 != 0 {
			break
		}
	}

	ad.c &= 0xffffffff
	return nil
}

func (ad *ArithmeticDecoder) mpsExchange(cx *CX, icx int) int {
	mps := cx.mpsVal()
	if ad.a < qeTable[icx].qe {
		if qeTable[icx].sw == 1 {
			cx.toggleMps()
		}
		cx.setCx(qeTable[icx].nextLPS)
		return 1 - mps
	}
	cx.setCx(qeTable[icx].nextMPS)
	return mps
}

func (ad *ArithmeticDecoder) lpsExchange(cx *CX, icx, qeVal int) int {
	mps := cx.mpsVal()
	if ad.a < qeVal {
		cx.setCx(qeTable[icx].nextMPS)
		ad.a = qeVal
		return mps
	}
	if qeTable[icx].sw == 1 {
		cx.toggleMps()
	}
	cx.setCx(qeTable[icx].nextLPS)
	ad.a = qeVal
	return 1 - mps
}

// ArithmeticIntegerDecoder decodes integers using arithmetic coding (Annex A.2).
type ArithmeticIntegerDecoder struct {
	decoder *ArithmeticDecoder
	prev    int
}

// NewArithmeticIntegerDecoder creates an integer decoder wrapping the given arithmetic decoder.
func NewArithmeticIntegerDecoder(decoder *ArithmeticDecoder) *ArithmeticIntegerDecoder {
	return &ArithmeticIntegerDecoder{decoder: decoder}
}

// Decode decodes a signed integer using the given context (Annex A.2).
// Returns the decoded value and whether the result is out-of-band (OOB).
func (aid *ArithmeticIntegerDecoder) Decode(cx *CX) (int64, bool, error) {
	if cx == nil {
		cx = NewCX(512, 1)
	}

	aid.prev = 1

	cx.SetIndex(aid.prev)
	s, err := aid.decoder.Decode(cx)
	if err != nil {
		return 0, false, err
	}
	aid.setPrev(s)

	cx.SetIndex(aid.prev)
	d, err := aid.decoder.Decode(cx)
	if err != nil {
		return 0, false, err
	}
	aid.setPrev(d)

	var bitsToRead int
	var offset int

	if d == 1 {
		cx.SetIndex(aid.prev)
		d, err = aid.decoder.Decode(cx)
		if err != nil {
			return 0, false, err
		}
		aid.setPrev(d)

		if d == 1 {
			cx.SetIndex(aid.prev)
			d, err = aid.decoder.Decode(cx)
			if err != nil {
				return 0, false, err
			}
			aid.setPrev(d)

			if d == 1 {
				cx.SetIndex(aid.prev)
				d, err = aid.decoder.Decode(cx)
				if err != nil {
					return 0, false, err
				}
				aid.setPrev(d)

				if d == 1 {
					cx.SetIndex(aid.prev)
					d, err = aid.decoder.Decode(cx)
					if err != nil {
						return 0, false, err
					}
					aid.setPrev(d)

					if d == 1 {
						bitsToRead = 32
						offset = 4436
					} else {
						bitsToRead = 12
						offset = 340
					}
				} else {
					bitsToRead = 8
					offset = 84
				}
			} else {
				bitsToRead = 6
				offset = 20
			}
		} else {
			bitsToRead = 4
			offset = 4
		}
	} else {
		bitsToRead = 2
		offset = 0
	}

	v := 0
	for i := 0; i < bitsToRead; i++ {
		cx.SetIndex(aid.prev)
		d, err = aid.decoder.Decode(cx)
		if err != nil {
			return 0, false, err
		}
		aid.setPrev(d)
		v = (v << 1) | d
	}

	v += offset

	if s == 0 {
		return int64(v), false, nil
	} else if s == 1 && v > 0 {
		return int64(-v), false, nil
	}

	// OOB (out-of-band)
	return 0, true, nil
}

// DecodeIAID decodes a symbol ID (Annex A.3).
func (aid *ArithmeticIntegerDecoder) DecodeIAID(cx *CX, symCodeLen int) (int, error) {
	aid.prev = 1

	for range symCodeLen {
		cx.SetIndex(aid.prev)
		bit, err := aid.decoder.Decode(cx)
		if err != nil {
			return 0, err
		}
		aid.prev = (aid.prev << 1) | bit
	}

	return aid.prev - (1 << symCodeLen), nil //nolint:gosec // symCodeLen is small
}

func (aid *ArithmeticIntegerDecoder) setPrev(bit int) {
	if aid.prev < 256 {
		aid.prev = ((aid.prev << 1) | bit) & 0x1ff
	} else {
		aid.prev = ((((aid.prev << 1) | bit) & 511) | 256) & 0x1ff
	}
}
