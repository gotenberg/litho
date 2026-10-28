package cmap

import (
	"fmt"
	"io"
)

// ucs2DataSize is the fixed data size (bytes minus 1) for UCS-2 keys in
// bfchar/bfrange records. A value of 1 means 2-byte (UCS-2) character codes.
const ucs2DataSize = 1

// bcmapStream provides sequential reading of a binary CMap (.bcmap)
// file. The format uses VLQ-encoded integers and delta-compressed entries.
type bcmapStream struct {
	data []byte
	pos  int
}

func (s *bcmapStream) readByte() (byte, error) {
	if s.pos >= len(s.data) {
		return 0, io.ErrUnexpectedEOF
	}
	b := s.data[s.pos]
	s.pos++
	return b, nil
}

// readNumber reads a VLQ-encoded unsigned integer. Each byte contributes 7
// payload bits (mask 0x7F); bit 7 (0x80) is the continuation flag.
func (s *bcmapStream) readNumber() (int64, error) {
	var n int64
	for {
		b, err := s.readByte()
		if err != nil {
			return 0, err
		}
		n = (n << 7) | int64(b&0x7F)
		if b&0x80 == 0 {
			return n, nil
		}
	}
}

// readCount reads a VLQ record or string length. Every counted item takes at
// least one byte, so a count past the bytes left names data the stream does
// not hold; bounding it there also keeps it within int on a 32-bit platform.
func (s *bcmapStream) readCount() (int, error) {
	n, err := s.readNumber()
	if err != nil {
		return 0, err
	}
	if n > int64(len(s.data)-s.pos) {
		return 0, fmt.Errorf("a count of %d past the %d bytes left: %w", n, len(s.data)-s.pos, errUnexpectedBCMapEnd)
	}
	return int(n), nil // fits int: at most len(s.data), checked above
}

// readSigned reads a zig-zag encoded signed integer built on top of readNumber.
func (s *bcmapStream) readSigned() (int64, error) {
	raw, err := s.readNumber()
	if err != nil {
		return 0, err
	}
	if raw&1 != 0 {
		return -(raw >> 1) - 1, nil
	}
	return raw >> 1, nil
}

// readHex reads dataSize+1 raw bytes as a big-endian unsigned integer.
func (s *bcmapStream) readHex(dataSize int) (int64, error) {
	n := dataSize + 1
	if s.pos+n > len(s.data) {
		return 0, io.ErrUnexpectedEOF
	}
	var val int64
	for range n {
		val = (val << 8) | int64(s.data[s.pos])
		s.pos++
	}
	return val, nil
}

// skipString reads and discards a VLQ-length-prefixed string (used for
// metadata records such as comments and usecmap references).
func (s *bcmapStream) skipString() error {
	length, err := s.readCount()
	if err != nil {
		return err
	}
	for range length {
		if _, err := s.readNumber(); err != nil {
			return err
		}
	}
	return nil
}

// collectCodespace parses codespace range entries (type 0) and returns them.
func (s *bcmapStream) collectCodespace(count, dataSize int) ([]CodeSpaceRange, error) {
	var ranges []CodeSpaceRange
	var end int64
	byteLen := dataSize + 1
	for i := range count {
		var start int64
		if i == 0 {
			v, err := s.readHex(dataSize)
			if err != nil {
				return nil, err
			}
			start = v
		} else {
			delta, err := s.readNumber()
			if err != nil {
				return nil, err
			}
			start = end + 1 + delta
		}
		delta, err := s.readNumber()
		if err != nil {
			return nil, err
		}
		end = start + delta
		low := intToBytes(start, byteLen)
		high := intToBytes(end, byteLen)
		ranges = append(ranges, CodeSpaceRange{Low: low, High: high})
	}
	return ranges, nil
}

// intToBytes converts an integer to a big-endian byte slice of the given length.
func intToBytes(val int64, length int) []byte {
	b := make([]byte, length)
	for i := length - 1; i >= 0; i-- {
		b[i] = byte(val & 0xFF) //nolint:gosec // intentional truncation
		val >>= 8
	}
	return b
}

// advanceCodespace advances past codespace range entries (type 0).
func (s *bcmapStream) advanceCodespace(count, dataSize int) error {
	var end int64
	for i := range count {
		var start int64
		if i == 0 {
			v, err := s.readHex(dataSize)
			if err != nil {
				return err
			}
			start = v
		} else {
			delta, err := s.readNumber()
			if err != nil {
				return err
			}
			start = end + 1 + delta
		}
		delta, err := s.readNumber()
		if err != nil {
			return err
		}
		end = start + delta
	}
	return nil
}

// advanceNotdef advances past notdef range entries (type 1).
func (s *bcmapStream) advanceNotdef(count, dataSize int) error {
	var end int64
	for i := range count {
		var start int64
		if i == 0 {
			v, err := s.readHex(dataSize)
			if err != nil {
				return err
			}
			start = v
		} else {
			delta, err := s.readNumber()
			if err != nil {
				return err
			}
			start = end + 1 + delta
		}
		delta, err := s.readNumber()
		if err != nil {
			return err
		}
		end = start + delta
		if _, err := s.readNumber(); err != nil {
			return err
		}
	}
	return nil
}

// collectCIDChar parses cidchar entries (type 2) into code→CID mappings.
// Each entry maps a single character code to a CID. Entries are
// delta-encoded: subsequent char codes and CIDs are offsets from the previous.
func (s *bcmapStream) collectCIDChar(count, dataSize int, seq bool, dst map[int64]int) error {
	var char, code int64
	for i := range count {
		if i == 0 {
			v, err := s.readHex(dataSize)
			if err != nil {
				return err
			}
			char = v
			c, err := s.readNumber()
			if err != nil {
				return err
			}
			code = c
		} else {
			if seq {
				char++
			} else {
				delta, err := s.readNumber()
				if err != nil {
					return err
				}
				char += 1 + delta
			}
			delta, err := s.readSigned()
			if err != nil {
				return err
			}
			code += 1 + delta
		}
		dst[char] = int(code) //nolint:gosec // fits int: bundled CMaps hold CIDs below 65536 (ISO 32000-1, Annex C).
	}
	return nil
}

// collectCIDRange parses cidrange entries (type 3) into code→CID mappings.
// Each entry maps a contiguous range [start, end] to CIDs starting at code.
func (s *bcmapStream) collectCIDRange(count, dataSize int, seq bool, dst map[int64]int) error {
	var prevEnd int64
	for i := range count {
		var start int64
		if i == 0 {
			v, err := s.readHex(dataSize)
			if err != nil {
				return err
			}
			start = v
		} else {
			if seq {
				start = prevEnd + 1
			} else {
				delta, err := s.readNumber()
				if err != nil {
					return err
				}
				start = prevEnd + 1 + delta
			}
		}
		delta, err := s.readNumber()
		if err != nil {
			return err
		}
		end := start + delta
		code, err := s.readNumber()
		if err != nil {
			return err
		}
		for j := start; j <= end; j++ {
			dst[j] = int(code + (j - start)) //nolint:gosec // fits int: bundled CMaps hold CIDs below 65536 (ISO 32000-1, Annex C).
		}
		prevEnd = end
	}
	return nil
}

// advanceBFChar advances past bfchar entries (type 4) without collecting data.
func (s *bcmapStream) advanceBFChar(count, dataSize int, seq bool) error {
	for i := range count {
		if i == 0 {
			if _, err := s.readHex(ucs2DataSize); err != nil {
				return err
			}
			if _, err := s.readHex(dataSize); err != nil {
				return err
			}
		} else {
			if !seq {
				if _, err := s.readNumber(); err != nil {
					return err
				}
			}
			if _, err := s.readSigned(); err != nil {
				return err
			}
		}
	}
	return nil
}

// advanceBFRange advances past bfrange entries (type 5) without collecting data.
func (s *bcmapStream) advanceBFRange(count, dataSize int, seq bool) error {
	for i := range count {
		if i == 0 {
			if _, err := s.readHex(ucs2DataSize); err != nil {
				return err
			}
			if _, err := s.readNumber(); err != nil {
				return err
			}
			if _, err := s.readHex(dataSize); err != nil {
				return err
			}
		} else {
			if !seq {
				if _, err := s.readNumber(); err != nil {
					return err
				}
			}
			if _, err := s.readNumber(); err != nil {
				return err
			}
			if _, err := s.readHex(dataSize); err != nil {
				return err
			}
		}
	}
	return nil
}
