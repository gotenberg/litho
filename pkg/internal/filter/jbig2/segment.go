package jbig2

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/intconv"
)

// Segment types, ITU-T T.88, 7.3.
const (
	typeSymbolDictionary         = 0
	typeIntermediateText         = 4
	typeImmediateText            = 6
	typeImmediateLosslessText    = 7
	typePatternDictionary        = 16
	typeIntermediateHalftone     = 20
	typeImmediateHalftone        = 22
	typeImmediateLosslessHalf    = 23
	typeIntermediateGeneric      = 36
	typeImmediateGeneric         = 38
	typeImmediateLosslessGeneric = 39
	typeIntermediateRefinement   = 40
	typeImmediateRefinement      = 42
	typeImmediateLosslessRefine  = 43
	typePageInformation          = 48
	typeEndOfPage                = 49
	typeEndOfStripe              = 50
	typeEndOfFile                = 51
	typeProfiles                 = 52
	typeTables                   = 53
	typeExtension                = 62
)

// unknownDataLength is the segment data length an immediate generic region
// may carry when its length was not known as the header was written (T.88,
// 7.2.7).
const unknownDataLength = 0xFFFFFFFF

// segment is one parsed JBIG2 segment: the header fields of ITU-T T.88, 7.2
// that decoding uses, and the segment's data part.
type segment struct {
	number   uint32
	kind     int
	page     uint32
	referred []uint32
	data     []byte
	// rows is the row count an immediate generic region of unknown length
	// ends with (7.4.6.4), and -1 for every other segment.
	rows int64
}

// cursor reads big-endian fields from a byte slice (T.88, 5.4.2) and
// remembers the first read that ran past the end.
type cursor struct {
	b   []byte
	off int
	err error
}

func (c *cursor) take(n int) []byte {
	if c.err != nil {
		return nil
	}
	if n < 0 || n > len(c.b)-c.off {
		c.err = fmt.Errorf("%w: field of %d bytes at offset %d runs past the %d available", ErrMalformed, n, c.off, len(c.b))
		return nil
	}
	v := c.b[c.off : c.off+n]
	c.off += n
	return v
}

func (c *cursor) u8() uint8 {
	if v := c.take(1); v != nil {
		return v[0]
	}
	return 0
}

// s8 reads a one-byte signed value, which T.88, 5.4.4 stores in two's
// complement.
func (c *cursor) s8() int {
	v := int(c.u8())
	if v >= 0x80 {
		v -= 0x100
	}
	return v
}

// s32 reads a four-byte signed value, in two's complement (T.88, 5.4.4).
func (c *cursor) s32() int64 {
	v := int64(c.u32())
	if v >= 1<<31 {
		v -= 1 << 32
	}
	return v
}

func (c *cursor) u16() uint16 {
	if v := c.take(2); v != nil {
		return binary.BigEndian.Uint16(v)
	}
	return 0
}

func (c *cursor) u32() uint32 {
	if v := c.take(4); v != nil {
		return binary.BigEndian.Uint32(v)
	}
	return 0
}

// parseSegments splits data into segments in the sequential or embedded
// organisation of T.88, Annex D, where each segment header is immediately
// followed by its data part.
//
// The loop is bounded by len(data): every header is at least 11 bytes and each
// iteration consumes one.
func parseSegments(data []byte) ([]segment, error) {
	var segs []segment
	for off := 0; off < len(data); {
		seg, n, err := parseSegment(data[off:])
		if err != nil {
			return nil, fmt.Errorf("segment at offset %d: %w", off, err)
		}
		segs = append(segs, seg)
		off += n
	}
	return segs, nil
}

// parseSegment reads one segment header and its data part from b and returns
// the segment and the number of bytes it occupies.
func parseSegment(b []byte) (segment, int, error) {
	c := &cursor{b: b}
	seg := segment{rows: -1}
	seg.number = c.u32()
	flags := c.u8()
	seg.kind = int(flags & 0x3F)
	longPage := flags&0x40 != 0

	// 7.2.4: the three high bits of the first byte give the referred-to
	// segment count, or 7 for the long form; 5 and 6 are forbidden.
	var count uint32
	if len(b) > c.off && b[c.off]>>5 == 7 {
		count = c.u32() & 0x1FFFFFFF
		// One retain bit for this segment and one per referred-to segment,
		// rounded up to whole bytes: 4 + (R + 1)/8 bytes in all.
		c.take(intconv.Clamp((uint64(count) + 8) / 8))
	} else {
		count = uint32(c.u8() >> 5)
		if count > 4 {
			return segment{}, 0, fmt.Errorf("%w: referred-to segment count field holds the reserved value %d", ErrMalformed, count)
		}
	}

	// 7.2.5: the width of each referred-to number depends on this segment's
	// own number.
	refSize := 4
	switch {
	case seg.number <= 256:
		refSize = 1
	case seg.number <= 65536:
		refSize = 2
	}
	if c.err == nil && int64(count)*int64(refSize) > int64(len(b)-c.off) {
		return segment{}, 0, fmt.Errorf("%w: segment %d refers to %d segments, more than its %d remaining bytes can hold",
			ErrMalformed, seg.number, count, len(b)-c.off)
	}
	if count > 0 && c.err == nil {
		seg.referred = make([]uint32, count)
		for i := range seg.referred {
			switch refSize {
			case 1:
				seg.referred[i] = uint32(c.u8())
			case 2:
				seg.referred[i] = uint32(c.u16())
			default:
				seg.referred[i] = c.u32()
			}
		}
	}

	if longPage {
		seg.page = c.u32()
	} else {
		seg.page = uint32(c.u8())
	}
	length := c.u32()
	if c.err != nil {
		return segment{}, 0, c.err
	}

	if length == unknownDataLength {
		return parseUnknownLength(seg, b, c.off)
	}
	if int64(length) > int64(len(b)-c.off) {
		return segment{}, 0, fmt.Errorf("%w: segment %d declares %d data bytes, %d remain",
			ErrMalformed, seg.number, length, len(b)-c.off)
	}
	seg.data = b[c.off : c.off+int(length)] // fits int: at most len(b)-c.off, checked above
	return seg, c.off + int(length), nil    // fits int: at most len(b)-c.off, checked above
}

// parseUnknownLength finds the end of an immediate generic region whose data
// length field is 0xFFFFFFFF, by the procedure of T.88, 7.2.7: the data ends
// with a four-byte row count, preceded by 0xFF 0xAC when the region is
// arithmetically coded and by 0x00 0x00 when it is MMR coded. Neither sequence
// can occur earlier in coded data of its kind (7.4.6.4, NOTE).
//
// The search starts after the generic region segment data header, so the
// signed AT offsets it holds cannot be mistaken for the end sequence.
func parseUnknownLength(seg segment, b []byte, start int) (segment, int, error) {
	if seg.kind != typeImmediateGeneric {
		return segment{}, 0, fmt.Errorf("%w: segment %d of type %d has unknown data length, which only an immediate generic region may",
			ErrMalformed, seg.number, seg.kind)
	}
	data := b[start:]
	if len(data) < regionInfoSize+1 {
		return segment{}, 0, fmt.Errorf("%w: generic region segment %d is shorter than its header", ErrMalformed, seg.number)
	}
	flags := data[regionInfoSize]
	mmr := flags&1 != 0
	header := regionInfoSize + 1
	end := []byte{0xFF, 0xAC}
	if mmr {
		end = []byte{0x00, 0x00}
	} else {
		header += genericATBytes(int(flags>>1) & 3)
	}
	if header > len(data) {
		return segment{}, 0, fmt.Errorf("%w: generic region segment %d is shorter than its header", ErrMalformed, seg.number)
	}
	i := bytes.Index(data[header:], end)
	if i < 0 || header+i+len(end)+4 > len(data) {
		return segment{}, 0, fmt.Errorf("%w: generic region segment %d of unknown length has no end sequence", ErrMalformed, seg.number)
	}
	rowsAt := header + i + len(end)
	seg.rows = int64(binary.BigEndian.Uint32(data[rowsAt:]))
	seg.data = data[:rowsAt+4]
	return seg, start + rowsAt + 4, nil
}
