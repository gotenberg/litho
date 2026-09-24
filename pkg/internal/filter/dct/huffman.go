package dct

import "fmt"

// huffman is a decoding table built as ITU-T T.81, Annex C and F.2.2.3
// describe: for each code length l, the codes of that length run from
// minCode[l] to maxCode[l], and the value of code minCode[l]+k is
// values[valPtr[l]+k].
type huffman struct {
	minCode, maxCode [17]int
	valPtr           [17]int
	values           []byte
}

// newHuffman builds a table from the BITS counts and HUFFVAL list of a DHT
// segment (T.81, B.2.4.2 and C.2).
func newHuffman(counts [16]byte, values []byte) (*huffman, error) {
	total := 0
	for _, c := range counts {
		total += int(c)
	}
	if total > 256 || total != len(values) {
		return nil, fmt.Errorf("%w: Huffman table of %d codes and %d values", ErrMalformed, total, len(values))
	}
	h := &huffman{values: values}
	code, k := 0, 0
	for l := 1; l <= 16; l++ {
		n := int(counts[l-1])
		if n == 0 {
			h.maxCode[l] = -1
		} else {
			h.valPtr[l] = k
			h.minCode[l] = code
			code += n
			k += n
			h.maxCode[l] = code - 1
		}
		// Codes of the next length continue from these, one bit longer.
		if code > 1<<l {
			return nil, fmt.Errorf("%w: Huffman table overflows %d-bit codes", ErrMalformed, l)
		}
		code <<= 1
	}
	return h, nil
}

// decode reads one value (T.81, F.2.2.3, DECODE).
func (h *huffman) decode(r *bitReader) (byte, error) {
	code := 0
	for l := 1; l <= 16; l++ {
		code = code<<1 | r.bit()
		if h.maxCode[l] >= 0 && code <= h.maxCode[l] {
			return h.values[h.valPtr[l]+code-h.minCode[l]], nil
		}
	}
	return 0, fmt.Errorf("%w: invalid Huffman code", ErrMalformed)
}

// receiveExtend reads an s-bit magnitude and extends it to a signed value
// (T.81, F.2.2.1, RECEIVE and EXTEND).
func receiveExtend(r *bitReader, s int) int {
	if s == 0 {
		return 0
	}
	v := r.bits(s)
	if v < 1<<(s-1) {
		v += -1<<s + 1
	}
	return v
}
