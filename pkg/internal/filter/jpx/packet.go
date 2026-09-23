package jpx

import (
	"errors"
	"fmt"
	"math/bits"
)

// Markers found in the bit stream, T.800 A.8.
const (
	markerSOP = 0xFF91
	markerEPH = 0xFF92
)

// errTruncated reports that a tile's data ended inside a packet. T.800, B.11
// lets a tile's representation be truncated anywhere, so it ends packet
// decoding for the tile rather than failing it.
var errTruncated = errors.New("tile data ends inside a packet")

// maxLengthBits bounds the width of a codeword segment length (T.800,
// B.10.7.1). It is an anti-DoS guard, not spec-mandated: Lblock only grows,
// so a header can ask for arbitrarily wide lengths, but no segment can be
// longer than the 2^32 bytes a tile-part can hold.
const maxLengthBits = 32

// headerReader reads packet header bits (T.800, B.10.1): most significant bit
// first, and only seven bits from a byte that follows 0xFF, whose top bit is a
// stuffed 0.
type headerReader struct {
	data []byte
	pos  int
	cur  byte
	// left counts the bits of cur not yet read.
	left int
	// lastFF records that the byte loaded last was 0xFF.
	lastFF bool
}

func (r *headerReader) bit() (int, error) {
	if r.left == 0 {
		if r.pos >= len(r.data) {
			return 0, errTruncated
		}
		r.cur = r.data[r.pos]
		r.pos++
		r.left = 8
		if r.lastFF {
			r.left = 7
		}
		r.lastFF = r.cur == 0xFF
	}
	r.left--
	return int(r.cur>>r.left) & 1, nil
}

// bits reads n bits as an unsigned number, most significant first.
func (r *headerReader) bits(n int) (int, error) {
	v := 0
	for range n {
		b, err := r.bit()
		if err != nil {
			return 0, err
		}
		v = v<<1 | b
	}
	return v, nil
}

// align ends the header at a byte boundary. A header whose last byte is 0xFF
// also owns the byte after it, which holds the stuffed bit (B.10.1).
func (r *headerReader) align() error {
	r.left = 0
	if r.lastFF {
		if r.pos >= len(r.data) {
			return errTruncated
		}
		r.pos++
		r.lastFF = false
	}
	return nil
}

// passCount reads the number of coding passes a packet adds to a code-block,
// coded as in T.800, Table B.4.
func (r *headerReader) passCount() (int, error) {
	if b, err := r.bit(); err != nil || b == 0 {
		return 1, err
	}
	if b, err := r.bit(); err != nil || b == 0 {
		return 2, err
	}
	v, err := r.bits(2)
	if err != nil || v < 3 {
		return 3 + v, err
	}
	if v, err = r.bits(5); err != nil || v < 31 {
		return 6 + v, err
	}
	v, err = r.bits(7)
	return 37 + v, err
}

// terminates reports whether coding pass p (counted from 0, the first cleanup
// pass) ends with a termination under code-block style cbStyle (T.800, Tables
// D.8 and D.9).
func terminates(p int, cbStyle uint8) bool {
	switch {
	case cbStyle&styleTerminateAll != 0:
		return true
	case cbStyle&styleBypass == 0:
		return false
	case p < 9:
		return false
	case p == 9:
		// The fourth cleanup pass ends the arithmetic-coded prefix.
		return true
	default:
		// From the fifth bit-plane, significance propagation passes are raw
		// and run on into the magnitude refinement pass, which terminates, as
		// does every cleanup pass.
		return (p-1)%3 != 0
	}
}

// Code-block style flags, T.800 Table A.19.
const (
	styleBypass       = 0x01
	styleTerminateAll = 0x04
)

// contribution is what one packet adds to one code-block: the lengths of its
// codeword segment pieces, each with the number of passes it codes.
type contribution struct {
	block *codeBlock
	// pieces holds a length and a pass count per piece, in order.
	pieces [][2]int
}

// readPacketHeader reads the header of the packet of layer from precinct p
// (T.800, B.10) and returns what it contributes to each code-block, in the
// order the packet body holds their data.
func readPacketHeader(r *headerReader, p *precinct, layer int, cbStyle uint8) ([]contribution, error) {
	nonEmpty, err := r.bit()
	if err != nil || nonEmpty == 0 {
		return nil, err
	}
	var out []contribution
	for i := range p.bands {
		pb := &p.bands[i]
		for j := range pb.blocks {
			cb := &pb.blocks[j]
			included, err := blockIncluded(r, pb, cb, j, layer)
			if err != nil {
				return nil, err
			}
			if !included {
				continue
			}
			c, err := readContribution(r, cb, cbStyle)
			if err != nil {
				return nil, err
			}
			out = append(out, c)
		}
	}
	return out, nil
}

// blockIncluded reads whether code-block j contributes to this packet (T.800,
// B.10.4) and, on its first inclusion, its zero bit-planes (B.10.5).
func blockIncluded(r *headerReader, pb *precinctBand, cb *codeBlock, j, layer int) (bool, error) {
	if cb.included {
		b, err := r.bit()
		return b == 1, err
	}
	x, y := j%pb.w, j/pb.w
	// The inclusion tag tree holds the layer of first inclusion, so the block
	// is in this packet if that layer is below layer + 1.
	_, in, err := pb.inclusion.decode(r, x, y, layer+1)
	if err != nil || !in {
		return false, err
	}
	// Tag tree values are bounded by the header bits available to code them,
	// one bit per increment, so an unbounded threshold ends when they do.
	p, _, err := pb.zeroPlanes.decode(r, x, y, int(^uint(0)>>1))
	if err != nil {
		return false, err
	}
	cb.included = true
	cb.zeroPlanes = p
	return true, nil
}

// readContribution reads the number of passes a packet adds to cb, its Lblock
// increment, and the length of each codeword segment piece (T.800, B.10.6 and
// B.10.7).
func readContribution(r *headerReader, cb *codeBlock, cbStyle uint8) (contribution, error) {
	n, err := r.passCount()
	if err != nil {
		return contribution{}, err
	}
	for {
		b, err := r.bit()
		if err != nil {
			return contribution{}, err
		}
		if b == 0 {
			break
		}
		cb.lblock++
	}
	c := contribution{block: cb}
	// B.10.7.2: one length per terminated pass among those added, and one for
	// the last pass added if it is not terminated.
	for p, end := cb.passes, cb.passes+n; p < end; {
		last := p
		for last < end-1 && !terminates(last, cbStyle) {
			last++
		}
		count := last - p + 1
		width := cb.lblock + bits.Len(uint(count)) - 1
		if width > maxLengthBits {
			return contribution{}, fmt.Errorf("%w: codeword segment length of %d bits", ErrMalformed, width)
		}
		length, err := r.bits(width)
		if err != nil {
			return contribution{}, err
		}
		c.pieces = append(c.pieces, [2]int{length, count})
		p = last + 1
	}
	return c, nil
}

// addPiece appends length bytes coding count passes to cb, extending its open
// codeword segment or starting a new one (T.800, B.10.7.2).
func addPiece(cb *codeBlock, data []byte, count int, cbStyle uint8) {
	if n := len(cb.segments); n == 0 || cb.segments[n-1].closed {
		cb.segments = append(cb.segments, codewordSegment{first: cb.passes})
	}
	s := &cb.segments[len(cb.segments)-1]
	s.data = append(s.data, data...)
	s.passes += count
	cb.passes += count
	s.closed = terminates(cb.passes-1, cbStyle)
}
