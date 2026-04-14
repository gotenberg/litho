package filter

import "io"

// lzwReader is a custom LZW decoder that supports both EarlyChange=1 (standard)
// and EarlyChange=0 (legacy PDF) modes. Go's compress/lzw only supports
// EarlyChange=1, which produces incorrect output for legacy PDF streams.
//
// PDF LZW uses MSB-first bit packing and a literal width of 8 (codes 0–255),
// with ClearTable=256, EOD=257, and the first user code at 258.
type lzwReader struct {
	src         io.Reader
	earlyChange int

	// Bit-reading state.
	bits  uint64 // accumulator
	nBits uint   // number of valid bits in the accumulator

	// Dictionary. Each entry stores the prefix code and suffix byte.
	// The full string is reconstructed by walking the prefix chain.
	prefix [4096]int
	suffix [4096]byte
	length [4096]int // cached decoded length for each entry

	nextCode  int // next code to assign
	codeWidth int // current code bit-width (9..12)

	// Decode state.
	prevCode int    // previous code (-1 means none)
	buf      []byte // pending output bytes not yet consumed by Read
	err      error
}

const (
	lzwClearCode = 256
	lzwEODCode   = 257
	lzwFirstCode = 258
	lzwMaxWidth  = 12
	lzwMaxCode   = 1<<lzwMaxWidth - 1
	lzwNoCode    = -1
)

// newLZWReader creates a custom LZW decoder. earlyChange must be 0 or 1.
func newLZWReader(src io.Reader, earlyChange int) *lzwReader {
	r := &lzwReader{
		src:         src,
		earlyChange: earlyChange,
	}
	r.reset()
	return r
}

// reset re-initialises the dictionary to the 258 base entries.
func (r *lzwReader) reset() {
	for i := range 256 {
		r.prefix[i] = lzwNoCode
		r.suffix[i] = byte(i)
		r.length[i] = 1
	}
	r.nextCode = lzwFirstCode
	r.codeWidth = 9
	r.prevCode = lzwNoCode
}

// readCode reads the next variable-width code from the MSB-first bit stream.
func (r *lzwReader) readCode() (int, error) {
	for r.nBits < uint(r.codeWidth) { //nolint:gosec // codeWidth is 9–12
		var tmp [1]byte
		_, err := r.src.Read(tmp[:])
		if err != nil {
			return 0, err
		}
		r.bits = r.bits<<8 | uint64(tmp[0])
		r.nBits += 8
	}

	r.nBits -= uint(r.codeWidth)                            //nolint:gosec // codeWidth is 9–12
	code := int(r.bits>>r.nBits) & ((1 << r.codeWidth) - 1) //nolint:gosec // masked to codeWidth bits
	return code, nil
}

// emit reconstructs the byte string for the given code and appends it to buf.
// Returns the first byte of the emitted string.
func (r *lzwReader) emit(code int) {
	n := r.length[code]
	// Grow buf to hold the new output.
	start := len(r.buf)
	r.buf = append(r.buf, make([]byte, n)...)
	out := r.buf[start:]

	c := code
	for i := n - 1; i >= 0; i-- {
		out[i] = r.suffix[c]
		c = r.prefix[c]
	}
}

// addEntry adds a new dictionary entry: the string for prevCode + the given suffix byte.
func (r *lzwReader) addEntry(prevCode int, suffixByte byte) {
	if r.nextCode > lzwMaxCode {
		return
	}
	r.prefix[r.nextCode] = prevCode
	r.suffix[r.nextCode] = suffixByte
	r.length[r.nextCode] = r.length[prevCode] + 1
	r.nextCode++
	r.maybeWiden()
}

// firstByte returns the first byte in the string represented by code.
func (r *lzwReader) firstByte(code int) byte {
	for r.prefix[code] != lzwNoCode {
		code = r.prefix[code]
	}
	return r.suffix[code]
}

// Read implements io.Reader.
func (r *lzwReader) Read(p []byte) (int, error) {
	// Drain buffered output first.
	if len(r.buf) > 0 {
		n := copy(p, r.buf)
		r.buf = r.buf[n:]
		return n, nil
	}

	if r.err != nil {
		return 0, r.err
	}

	for len(r.buf) == 0 {
		code, err := r.readCode()
		if err != nil {
			r.err = err
			return 0, err
		}

		switch {
		case code == lzwClearCode:
			r.reset()
			continue

		case code == lzwEODCode:
			r.err = io.EOF
			return 0, io.EOF

		case r.prevCode == lzwNoCode:
			// First code after init or ClearTable: must be a literal.
			if code >= lzwFirstCode {
				r.err = ErrLZWNonLiteralAfterClear
				return 0, r.err
			}
			r.emit(code)
			r.prevCode = code

		case code < r.nextCode:
			// Code already in the dictionary.
			firstByte := r.firstByte(code)
			r.addEntry(r.prevCode, firstByte)
			r.emit(code)
			r.prevCode = code

		case code == r.nextCode:
			// Special case: code not yet in dictionary.
			// The string is prevString + prevString[0].
			firstByte := r.firstByte(r.prevCode)
			r.addEntry(r.prevCode, firstByte)
			r.emit(code)
			r.prevCode = code

		default:
			r.err = ErrLZWCodeOutOfRange
			return 0, r.err
		}
	}

	n := copy(p, r.buf)
	r.buf = r.buf[n:]
	return n, nil
}

// maybeWiden increases the code width if the dictionary has reached the threshold.
//
// The decoder's dictionary lags the encoder's by one entry (the first code
// after ClearTable produces no dictionary entry), so the decoder must widen
// one entry earlier than the encoder would. The encoder widens at
// nextCode == (1<<codeWidth) - earlyChange; the decoder widens at
// nextCode == (1<<codeWidth) - earlyChange - 1.
//
//	EarlyChange=1 (standard): decoder widens at nextCode == (1<<codeWidth) - 2
//	EarlyChange=0 (legacy):   decoder widens at nextCode == (1<<codeWidth) - 1
func (r *lzwReader) maybeWiden() {
	if r.codeWidth < lzwMaxWidth && r.nextCode == (1<<r.codeWidth)-r.earlyChange-1 {
		r.codeWidth++
	}
}
