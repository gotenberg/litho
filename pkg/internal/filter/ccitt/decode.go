package ccitt

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/limits"
)

// maxColumns bounds the width of a row. It is an anti-DoS guard, not
// spec-mandated: Columns comes from the file, and every row costs Columns/8
// bytes of output and a reference line of up to Columns changes. A 1200 dpi
// scan of an A0 sheet is under 40,000 pixels wide.
const maxColumns = 1 << 17

// defaultMaxOutput bounds the bytes one Decode call returns when [Params]
// leaves MaxOutput zero. It is an anti-DoS guard, not spec-mandated: a few
// bytes of two-dimensional coding describe a whole white row, so a small
// stream can ask for an enormous image. It is the default stream budget,
// [limits.DefaultMaxStreamBytes], which package filter hands on as MaxOutput
// from the document's own budget.
const defaultMaxOutput = limits.DefaultMaxStreamBytes

// ctxRows is how many rows are decoded between checks of the context.
const ctxRows = 256

// Params are the CCITTFaxDecode parameters of ISO 32000-1, Table 11.
type Params struct {
	// K selects the coding: negative for pure two-dimensional (ITU-T T.6,
	// Group 4), zero for pure one-dimensional (T.4, Group 3), positive for
	// mixed coding, where a tag bit before each row names its coding.
	K int
	// EndOfLine reports that every row is preceded by an end-of-line code.
	// The decoder accepts one wherever a row may start either way.
	EndOfLine bool
	// EncodedByteAlign reports that every row begins on a byte boundary.
	EncodedByteAlign bool
	// Columns is the width in pixels, and Rows the height, or 0 when the
	// data alone says where it ends.
	Columns, Rows int
	// EndOfBlock reports that the data ends with an end-of-block pattern.
	// The decoder stops at one whether it is announced or not.
	EndOfBlock bool
	// BlackIs1 makes 1 bits black; by default 0 bits are.
	BlackIs1 bool
	// MaxOutput bounds the bytes the call returns, or 0 for
	// defaultMaxOutput. It is not a Table 11 parameter: the caller sets it
	// from its resource budget.
	MaxOutput int64
}

// DefaultParams returns the parameters an empty parameter dictionary
// describes (ISO 32000-1, Table 11).
func DefaultParams() Params {
	return Params{Columns: 1728, EndOfBlock: true}
}

// Decode decodes CCITT facsimile data into rows of 1-bit pixels, each row
// padded to a whole byte, most significant bit first.
//
// Rows are decoded until Rows rows are done, an end-of-block pattern appears,
// or the data runs out. A row the data breaks off in, or garbles, ends the
// decoding: the rows before it are returned, and an error only when there are
// none.
//
// Decode is safe for concurrent use: it keeps no state between calls.
func Decode(ctx context.Context, data []byte, p Params) ([]byte, error) {
	out, _, err := decode(ctx, data, p, false)
	return out, err
}

// DecodeBlock decodes exactly Rows rows as [Decode] does, then, for
// two-dimensional coding (K negative), the end-of-block pattern if one
// follows them, and reports how many whole bytes the rows and the pattern
// occupy. It serves a block embedded in a format whose data goes on after
// it, as JBIG2 embeds T.6 bitplanes (ITU-T T.88, 6.2.6). Data that holds
// fewer rows, or garbles one, is an error.
//
// DecodeBlock is safe for concurrent use: it keeps no state between calls.
func DecodeBlock(ctx context.Context, data []byte, p Params) ([]byte, int, error) {
	if p.Rows < 1 {
		return nil, 0, fmt.Errorf("%w: a block of %d rows", ErrMalformed, p.Rows)
	}
	out, used, err := decode(ctx, data, p, true)
	if err != nil {
		return nil, 0, err
	}
	if want := p.Rows * ((p.Columns + 7) / 8); len(out) < want {
		return nil, 0, fmt.Errorf("%w: data ends after %d of %d rows", ErrMalformed, len(out)/((p.Columns+7)/8), p.Rows)
	}
	return out, used, nil
}

// decode decodes rows as [Decode] and [DecodeBlock] describe, and reports
// the whole bytes consumed. When block is set a garbled row is an error
// whatever precedes it, and the end-of-block pattern after the last row is
// consumed.
func decode(ctx context.Context, data []byte, p Params, block bool) ([]byte, int, error) {
	if p.Columns < 1 || p.Columns > maxColumns {
		return nil, 0, fmt.Errorf("%w: %d columns", ErrLimit, p.Columns)
	}
	if p.Rows < 0 {
		return nil, 0, fmt.Errorf("%w: %d rows", ErrMalformed, p.Rows)
	}
	maxOutput := p.MaxOutput
	if maxOutput <= 0 {
		maxOutput = defaultMaxOutput
	}
	rowBytes := (p.Columns + 7) / 8
	if int64(p.Rows) > maxOutput/int64(rowBytes) {
		return nil, 0, fmt.Errorf("%w: %d rows of %d columns", ErrLimit, p.Rows, p.Columns)
	}
	d := &decoder{r: bitReader{data: data}, p: p}
	var out []byte
	for row := 0; p.Rows == 0 || row < p.Rows; row++ {
		if row%ctxRows == 0 {
			if err := ctx.Err(); err != nil {
				return nil, 0, err
			}
		}
		if int64(len(out)+rowBytes) > maxOutput {
			return nil, 0, fmt.Errorf("%w: more than %d bytes of rows", ErrLimit, maxOutput)
		}
		ok, err := d.row()
		if err != nil {
			if row > 0 && !block {
				break
			}
			return nil, 0, err
		}
		if !ok {
			break
		}
		out = d.render(out)
	}
	if block && p.K < 0 {
		// EOFB is two end-of-line codes (T.6, 2.4.3).
		for range 2 {
			if eol, err := d.r.endOfLine(); err != nil || !eol {
				break
			}
		}
	}
	return out, min((d.r.pos+7)/8, len(data)), nil
}

// decoder holds the state that runs from one row to the next.
type decoder struct {
	r bitReader
	p Params
	// ref and cur are the changing elements of the reference row and the row
	// being coded: the columns where the colour changes, from white at the
	// left edge, in increasing order.
	ref, cur []int
}

// row decodes the next row into d.cur, and reports false at the end of the
// data.
func (d *decoder) row() (bool, error) {
	if d.p.EncodedByteAlign {
		d.r.align()
	}
	eol, err := d.r.endOfLine()
	if err != nil {
		return false, err
	}
	twoD := d.p.K < 0
	if d.p.K > 0 {
		tag, ok := d.r.bit()
		if !ok {
			return false, nil
		}
		twoD = tag == 0
	}
	// A second end-of-line where a row should begin is the end-of-block
	// pattern: RTC (T.4, 4.1.4) or EOFB (T.6, 2.4.3).
	if eol || d.p.K < 0 {
		if end, err := d.r.endOfLine(); err != nil || end {
			return false, err
		}
	}
	if !d.r.more() {
		return false, nil
	}
	d.ref, d.cur = d.cur, d.ref[:0]
	if twoD {
		err = d.row2D()
	} else {
		err = d.row1D()
	}
	if err != nil {
		return false, err
	}
	d.cur = cancelPairs(d.cur, d.p.Columns)
	return true, nil
}

// row1D decodes a row coded one-dimensionally (T.4, 4.1): runs of alternating
// colour from white, each a sum of make-up codes and a terminating code.
func (d *decoder) row1D() error {
	pos, black := 0, false
	for pos < d.p.Columns {
		run, err := d.r.run(black)
		if err != nil {
			return err
		}
		pos = min(pos+run, d.p.Columns)
		black = !black
		d.cur = append(d.cur, pos)
	}
	return nil
}

// row2D decodes a row coded two-dimensionally against d.ref (T.4, 4.2 and
// T.6, 2.2).
func (d *decoder) row2D() error {
	ref := append(d.ref, d.p.Columns, d.p.Columns) //nolint:gocritic // the sentinels are needed on a copy only while coding this row.
	a0, black := -1, false
	for a0 < d.p.Columns {
		mode, err := d.r.decode(modeTree)
		if err != nil {
			return err
		}
		b1, b2 := changes(ref, a0, black)
		switch mode {
		case modePass:
			a0 = b2
		case modeHorizontal:
			first, err := d.r.run(black)
			if err != nil {
				return err
			}
			second, err := d.r.run(!black)
			if err != nil {
				return err
			}
			a1 := min(max(a0, 0)+first, d.p.Columns)
			a2 := min(a1+second, d.p.Columns)
			d.cur = append(d.cur, a1, a2)
			a0 = a2
		case modeExtension:
			return fmt.Errorf("%w: uncompressed mode", ErrUnsupported)
		default:
			a1 := b1 + verticalOffset[mode]
			if a1 < a0 || a1 < 0 || a1 > d.p.Columns {
				return fmt.Errorf("%w: vertical mode reaches column %d from %d", ErrMalformed, a1, a0)
			}
			d.cur = append(d.cur, a1)
			a0, black = a1, !black
		}
	}
	return nil
}

// verticalOffset is a1 - b1 for each vertical mode.
var verticalOffset = map[int]int{
	modeV0: 0, modeVR1: 1, modeVR2: 2, modeVR3: 3,
	modeVL1: -1, modeVL2: -2, modeVL3: -3,
}

// changes returns b1, the first changing element of ref right of a0 whose
// colour is the opposite of a0's, and b2, the next one (T.4, 4.2.1.3.1). ref
// holds the changes to black at even indices and to white at odd ones, and
// ends with two sentinels at the row's width.
func changes(ref []int, a0 int, black bool) (int, int) {
	i := 0
	for i < len(ref)-2 && ref[i] <= a0 {
		i++
	}
	// A change to black is the opposite of white, at an even index.
	if (i%2 == 1) != black {
		i++
	}
	i = min(i, len(ref)-2)
	return ref[i], ref[i+1]
}

// cancelPairs removes changes that coincide in pairs, since a run of no
// pixels changes nothing and the next row's changing elements must not see it,
// and the changes at the right edge, which are no pixel's.
func cancelPairs(cs []int, columns int) []int {
	out := cs[:0]
	for _, c := range cs {
		if n := len(out); n > 0 && out[n-1] == c {
			out = out[:n-1]
			continue
		}
		out = append(out, c)
	}
	for len(out) > 0 && out[len(out)-1] >= columns {
		out = out[:len(out)-1]
	}
	return out
}

// render appends the pixels of d.cur to out as one packed row. The bits past
// the last column are 0.
func (d *decoder) render(out []byte) []byte {
	rowBytes := (d.p.Columns + 7) / 8
	start := len(out)
	white := byte(0xFF)
	if d.p.BlackIs1 {
		white = 0
	}
	for range rowBytes {
		out = append(out, white)
	}
	row := out[start:]
	for i := 0; i < len(d.cur); i += 2 {
		to := d.p.Columns
		if i+1 < len(d.cur) {
			to = d.cur[i+1]
		}
		for x := d.cur[i]; x < to; x++ {
			row[x>>3] ^= 0x80 >> (x & 7)
		}
	}
	if pad := d.p.Columns % 8; pad != 0 {
		row[rowBytes-1] &= 0xFF << (8 - pad)
	}
	return out
}
