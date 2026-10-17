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
func (s *bcmapStream) readNumber() (int, error) {
	var n int
	for {
		b, err := s.readByte()
		if err != nil {
			return 0, err
		}
		n = (n << 7) | int(b&0x7F)
		if b&0x80 == 0 {
			return n, nil
		}
	}
}

// readSigned reads a zig-zag encoded signed integer built on top of readNumber.
func (s *bcmapStream) readSigned() (int, error) {
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
func (s *bcmapStream) readHex(dataSize int) (int, error) {
	n := dataSize + 1
	if s.pos+n > len(s.data) {
		return 0, io.ErrUnexpectedEOF
	}
	var val int
	for range n {
		val = (val << 8) | int(s.data[s.pos])
		s.pos++
	}
	return val, nil
}

// skipString reads and discards a VLQ-length-prefixed string (used for
// metadata records such as comments and usecmap references).
func (s *bcmapStream) skipString() error {
	length, err := s.readNumber()
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

// parseBCMapCIDToUnicode parses a binary CMap (.bcmap) file containing
// Unicode→CID mappings (e.g., UniJIS-UCS2-H) and returns the inverted
// CID→Unicode map. When multiple Unicode values map to the same CID, the
// lowest Unicode value wins.
func parseBCMapCIDToUnicode(data []byte) (map[int]rune, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty bcmap data")
	}

	s := &bcmapStream{data: data, pos: 1} // skip header byte (type<<1 | wmode)
	uni2cid := make(map[int]int, 16384)   //nolint:mnd // typical CMap size

	for s.pos < len(s.data) {
		tag, err := s.readByte()
		if err != nil {
			break
		}

		recType := int(tag >> 5)

		// Metadata records (type 7): comment (0xE0) or usecmap (0xE1).
		if recType == 7 {
			if err := s.skipString(); err != nil {
				return nil, fmt.Errorf("bcmap metadata: %w", err)
			}
			continue
		}

		seq := tag&0x10 != 0
		dataSize := int(tag & 0x0F)

		count, err := s.readNumber()
		if err != nil {
			return nil, fmt.Errorf("bcmap record count: %w", err)
		}

		switch recType {
		case 0:
			err = s.advanceCodespace(count, dataSize)
		case 1:
			err = s.advanceNotdef(count, dataSize)
		case 2:
			err = s.collectCIDChar(count, dataSize, seq, uni2cid)
		case 3:
			err = s.collectCIDRange(count, dataSize, seq, uni2cid)
		case 4:
			err = s.advanceBFChar(count, dataSize, seq)
		case 5:
			err = s.advanceBFRange(count, dataSize, seq)
		default:
			err = fmt.Errorf("unknown record type %d", recType)
		}
		if err != nil {
			return nil, fmt.Errorf("bcmap record type %d: %w", recType, err)
		}
	}

	// Invert to CID→Unicode. Lowest Unicode value wins.
	cid2uni := make(map[int]rune, len(uni2cid))
	for uni, cid := range uni2cid {
		r := rune(uni) //nolint:gosec // UCS-2 values fit in int32.
		if existing, ok := cid2uni[cid]; !ok || r < existing {
			cid2uni[cid] = r
		}
	}

	return cid2uni, nil
}

// parseBCMapCharToCID parses a binary CMap (.bcmap) encoding file
// (e.g., GBK-EUC-H) and returns the character-code→CID map. Codespace
// ranges are also collected to determine valid byte lengths. The notdef
// mappings and the usecmap base, which [parseBCMapCIDProgram] also reads, are
// left out.
func parseBCMapCharToCID(data []byte) (charToCID map[int]int, codespaces []CodeSpaceRange, err error) {
	prog, err := parseBCMapCIDProgram(data)
	if err != nil {
		return nil, nil, err
	}
	return prog.charToCID, prog.codespaces, nil
}

// collectCodespace parses codespace range entries (type 0) and returns them.
func (s *bcmapStream) collectCodespace(count, dataSize int) ([]CodeSpaceRange, error) {
	var ranges []CodeSpaceRange
	var end int
	byteLen := dataSize + 1
	for i := range count {
		var start int
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
func intToBytes(val, length int) []byte {
	b := make([]byte, length)
	for i := length - 1; i >= 0; i-- {
		b[i] = byte(val & 0xFF) //nolint:gosec // intentional truncation
		val >>= 8
	}
	return b
}

// advanceCodespace advances past codespace range entries (type 0).
func (s *bcmapStream) advanceCodespace(count, dataSize int) error {
	var end int
	for i := range count {
		var start int
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
	var end int
	for i := range count {
		var start int
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

// collectCIDChar parses cidchar entries (type 2) into Unicode→CID mappings.
// Each entry maps a single character code to a CID. Entries are
// delta-encoded: subsequent char codes and CIDs are offsets from the previous.
func (s *bcmapStream) collectCIDChar(count, dataSize int, seq bool, dst map[int]int) error {
	var char, code int
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
		dst[char] = code
	}
	return nil
}

// collectCIDRange parses cidrange entries (type 3) into Unicode→CID mappings.
// Each entry maps a contiguous range [start, end] to CIDs starting at code.
func (s *bcmapStream) collectCIDRange(count, dataSize int, seq bool, dst map[int]int) error {
	var prevEnd int
	for i := range count {
		var start int
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
			dst[j] = code + (j - start)
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
