package jbig2

import (
	"fmt"
	"io"
)

// Reader provides bit-level and byte-level reading from a byte slice.
// It replaces the Java SubInputStream concept with a simple, seekable reader.
type Reader struct {
	data   []byte
	pos    int64 // byte position
	bitPos int   // bit offset within current byte (0-7)
	length int64
}

// NewReader creates a Reader over the given data.
func NewReader(data []byte) *Reader {
	return &Reader{
		data:   data,
		length: int64(len(data)),
	}
}

// SubReader creates a Reader over a sub-range of the given data.
func SubReader(data []byte, offset, length int64) *Reader {
	end := min(offset+length, int64(len(data)))
	if offset < 0 {
		offset = 0
	}
	sub := data[offset:end]
	return &Reader{
		data:   sub,
		length: int64(len(sub)),
	}
}

// Length returns the total length of the data in bytes.
func (r *Reader) Length() int64 { return r.length }

// Position returns the current byte position.
func (r *Reader) Position() int64 { return r.pos }

// SeekTo sets the byte position. Resets bit offset to 0.
func (r *Reader) SeekTo(pos int64) {
	r.pos = pos
	r.bitPos = 0
}

// ReadByte reads a single byte and advances the position.
func (r *Reader) ReadByte() (byte, error) {
	r.bitPos = 0
	if r.pos >= r.length {
		return 0, io.EOF
	}
	b := r.data[r.pos]
	r.pos++
	return b, nil
}

// Read reads a single byte as an int (0-255), or -1 on EOF.
func (r *Reader) Read() int {
	r.bitPos = 0
	if r.pos >= r.length {
		return -1
	}
	b := int(r.data[r.pos]) & 0xff
	r.pos++
	return b
}

// ReadBit reads a single bit (0 or 1) from the stream.
func (r *Reader) ReadBit() (int, error) {
	if r.pos >= r.length {
		return 0, io.EOF
	}
	b := r.data[r.pos]
	bit := int((b >> (7 - r.bitPos)) & 1) //nolint:gosec // bitPos is always 0-7
	r.bitPos++
	if r.bitPos >= 8 {
		r.bitPos = 0
		r.pos++
	}
	return bit, nil
}

// ReadBits reads n bits (1-32) from the stream and returns them as an int64.
func (r *Reader) ReadBits(n int) (int64, error) {
	if n <= 0 || n > 32 {
		return 0, fmt.Errorf("jbig2: invalid bit count %d", n)
	}
	var result int64
	for range n {
		bit, err := r.ReadBit()
		if err != nil {
			return 0, err
		}
		result = (result << 1) | int64(bit)
	}
	return result, nil
}

// ReadSignedByte reads a signed byte from the stream.
func (r *Reader) ReadSignedByte() (int8, error) {
	r.bitPos = 0
	if r.pos >= r.length {
		return 0, io.EOF
	}
	b := int8(r.data[r.pos]) //nolint:gosec // intentional reinterpretation
	r.pos++
	return b, nil
}

// ReadUint32 reads a 32-bit unsigned integer (big-endian).
func (r *Reader) ReadUint32() (uint32, error) {
	v, err := r.ReadBits(32)
	if err != nil {
		return 0, err
	}
	return uint32(v & 0xffffffff), nil //nolint:gosec // masked to 32 bits
}

// ReadUint16 reads a 16-bit unsigned integer (big-endian).
func (r *Reader) ReadUint16() (uint16, error) {
	v, err := r.ReadBits(16)
	if err != nil {
		return 0, err
	}
	return uint16(v & 0xffff), nil //nolint:gosec // masked to 16 bits
}

// SkipBits skips any partial bits in the current byte, aligning to the next byte boundary.
func (r *Reader) SkipBits() {
	if r.bitPos != 0 {
		r.bitPos = 0
		r.pos++
	}
}

// Remaining returns the number of bytes remaining.
func (r *Reader) Remaining() int64 {
	return r.length - r.pos
}
