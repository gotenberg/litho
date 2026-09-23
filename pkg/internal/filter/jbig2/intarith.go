package jbig2

import (
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/filter/mq"
)

// intRanges transcribes the magnitude ranges of ITU-T T.88 Figure A.1 and
// Table A.1, in prefix order: how many bits follow the prefix, and the
// smallest magnitude the range codes.
//
//nolint:gochecknoglobals // static spec table, ITU-T T.88 Figure A.1
var intRanges = [...]struct {
	bits   int
	offset int64
}{{2, 0}, {4, 4}, {6, 20}, {8, 84}, {12, 340}, {32, 4436}}

// intDecoder is one of the arithmetic integer decoding procedures of ITU-T
// T.88, Annex A.2 (IADH, IADW, IAEX and the rest). Each has its own 512
// contexts, addressed by a leading 1 bit followed by up to eight of the bits
// decoded so far in the current value.
type intDecoder struct {
	cx [512]mq.Context
}

// decode returns the next value, or ok false for the out-of-band value OOB,
// following Figure A.1 and Table A.1: a sign bit, a unary prefix choosing one
// of six ranges, and that range's fixed number of magnitude bits. A sign of 1
// on a magnitude of 0 is OOB.
func (d *intDecoder) decode(dec *mq.Decoder) (value int64, ok bool) {
	prev := 1
	bit := func() int64 {
		b := dec.Decode(&d.cx[prev])
		if prev < 256 {
			prev = prev<<1 | b
		} else {
			prev = (prev<<1|b)&511 | 256
		}
		return int64(b)
	}
	sign := bit()
	// A 0 bit ends the unary prefix at a range; five 1 bits select the last.
	r := intRanges[len(intRanges)-1]
	for _, candidate := range intRanges[:len(intRanges)-1] {
		if bit() == 0 {
			r = candidate
			break
		}
	}
	var v int64
	for range r.bits {
		v = v<<1 | bit()
	}
	v += r.offset
	if sign == 1 {
		if v == 0 {
			return 0, false
		}
		v = -v
	}
	return v, true
}

// value decodes a value that must not be OOB. what names it for the error.
func (d *intDecoder) value(dec *mq.Decoder, what string) (int64, error) {
	v, ok := d.decode(dec)
	if !ok {
		return 0, fmt.Errorf("%w: %s decoded as the out-of-band value", ErrMalformed, what)
	}
	return v, nil
}

// iaidDecoder is the IAID procedure of ITU-T T.88, A.3: a fixed-length symbol
// ID of codeLen bits, each decoded in a context holding every bit before it.
type iaidDecoder struct {
	codeLen int
	cx      []mq.Context
}

// newIAID returns an IAID decoder for codeLen-bit IDs. codeLen is bounded by
// the caller: symbolCodeLen never exceeds the width of maxSymbols.
func newIAID(codeLen int) *iaidDecoder {
	return &iaidDecoder{codeLen: codeLen, cx: make([]mq.Context, 1<<(codeLen+1))}
}

func (d *iaidDecoder) decode(dec *mq.Decoder) int64 {
	prev := 1
	for range d.codeLen {
		prev = prev<<1 | dec.Decode(&d.cx[prev])
	}
	return int64(prev - 1<<d.codeLen)
}

// symbolCodeLen is the number of bits needed to tell n symbols apart: the
// base-2 logarithm of n rounded up, and 0 for a single symbol (T.88, Table 31
// and 6.5.8.2.3).
func symbolCodeLen(n int) int {
	codeLen := 0
	for 1<<codeLen < n {
		codeLen++
	}
	return codeLen
}

// arithState holds everything one arithmetically coded segment decodes with:
// the decoder over its data and the contexts of every procedure it may call.
// A symbol dictionary shares one state between its own fields and the text
// region and refinement procedures its aggregations invoke (T.88, 6.5.8.2).
type arithState struct {
	dec *mq.Decoder

	iadh, iadw, iaex, iaai       intDecoder
	iadt, iafs, iads, iait, iari intDecoder
	iardw, iardh, iardx, iardy   intDecoder
	iaid                         *iaidDecoder
	gb                           []mq.Context
	gr                           []mq.Context
	budget                       *budget
}
