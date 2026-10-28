package jbig2

import (
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/intconv"
)

// maxPrefixLen bounds the prefix code length a Huffman table line may have.
// A code table's flags allow up to 255 bits (B.2.1: HTPS is at most 8), but
// the longest prefix in the standard tables of B.5 is 9 bits, and 32 keeps
// every code within an int. Longer prefixes are refused.
const maxPrefixLen = 32

// maxFieldBits bounds the width of a value read directly from the bitstream.
// No field T.88 reads directly is wider than a range line's 32 bits.
const maxFieldBits = 32

// maxTableLines bounds the lines a code table segment may define. It is an
// anti-DoS guard, not spec-mandated: each line costs only a few bits, so a
// megabyte table could otherwise define millions of them. The standard tables
// have at most 21 lines, and a code table needs no more lines than it has
// distinct prefix codes worth sending.
const maxTableLines = 4096

// bitReader reads bits most significant first, as T.88 packs them (5.4.1).
type bitReader struct {
	data []byte
	pos  int
	bit  uint
}

func (r *bitReader) readBit() (int, error) {
	if r.pos >= len(r.data) {
		return 0, fmt.Errorf("%w: Huffman-coded data ends early", ErrMalformed)
	}
	b := int(r.data[r.pos]>>(7-r.bit)) & 1
	if r.bit++; r.bit == 8 {
		r.bit = 0
		r.pos++
	}
	return b, nil
}

// readBits reads n bits, n at most maxFieldBits, as an unsigned value.
func (r *bitReader) readBits(n int) (int, error) {
	if n < 0 || n > maxFieldBits {
		return 0, fmt.Errorf("%w: field of %d bits", ErrMalformed, n)
	}
	var v int
	for range n {
		b, err := r.readBit()
		if err != nil {
			return 0, err
		}
		v = v<<1 | b
	}
	return v, nil
}

// align skips the bits left in the current byte (T.88 uses it wherever a
// field must start on a byte boundary).
func (r *bitReader) align() {
	if r.bit != 0 {
		r.bit = 0
		r.pos++
	}
}

// huffLine is one table line of a Huffman table, ITU-T T.88 B.2.
type huffLine struct {
	prefLen  int
	rangeLen int
	rangeLow int64
	// lower marks the lower range line, whose values count down from
	// rangeLow; oob marks the out-of-band line, which has no range.
	lower bool
	oob   bool
}

// huffTable is a Huffman table with its prefix codes assigned (B.3). codes
// maps each prefix, per length, to its line: the same value at two lengths is
// two different codes.
type huffTable struct {
	lines []huffLine
	codes [maxPrefixLen + 1]map[int]int
}

// newHuffTable assigns prefix codes to lines by the algorithm of ITU-T T.88,
// B.3: codes are handed out in line order within each length, each length's
// first code following on from the previous length's last.
func newHuffTable(lines []huffLine) (*huffTable, error) {
	maxLen := 0
	var count [maxPrefixLen + 1]int
	for _, l := range lines {
		if l.prefLen < 0 || l.prefLen > maxPrefixLen {
			return nil, fmt.Errorf("%w: Huffman prefix length %d", ErrMalformed, l.prefLen)
		}
		if l.rangeLen < 0 || l.rangeLen > 32 {
			return nil, fmt.Errorf("%w: Huffman range length %d", ErrMalformed, l.rangeLen)
		}
		count[l.prefLen]++
		maxLen = max(maxLen, l.prefLen)
	}
	t := &huffTable{lines: lines}
	count[0] = 0
	first := 0
	for length := 1; length <= maxLen; length++ {
		first = (first + count[length-1]) << 1
		code := first
		t.codes[length] = make(map[int]int, count[length])
		for i, l := range lines {
			if l.prefLen != length {
				continue
			}
			if code>>length != 0 {
				return nil, fmt.Errorf("%w: Huffman code lengths oversubscribe %d-bit codes", ErrMalformed, length)
			}
			t.codes[length][code] = i
			code++
		}
	}
	return t, nil
}

// decode reads one value with the table, ITU-T T.88 B.4, reporting OOB as ok
// false.
func (t *huffTable) decode(r *bitReader) (value int64, ok bool, err error) {
	code := 0
	for length := 1; length <= maxPrefixLen; length++ {
		b, err := r.readBit()
		if err != nil {
			return 0, false, err
		}
		code = code<<1 | b
		i, found := t.codes[length][code]
		if !found {
			continue
		}
		l := t.lines[i]
		if l.oob {
			return 0, false, nil
		}
		off, err := r.readBits(l.rangeLen)
		if err != nil {
			return 0, false, err
		}
		if l.lower {
			return l.rangeLow - int64(off), true, nil
		}
		return l.rangeLow + int64(off), true, nil
	}
	return 0, false, fmt.Errorf("%w: bits match no Huffman code", ErrMalformed)
}

// value reads a value that must not be OOB.
func (t *huffTable) value(r *bitReader, what string) (int64, error) {
	v, ok, err := t.decode(r)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, fmt.Errorf("%w: %s decoded as the out-of-band value", ErrMalformed, what)
	}
	return v, nil
}

// stdTable describes a standard table in the form ITU-T T.88 B.5 prints it:
// its normal lines as (lowest value, prefix length, range length), then the
// prefix lengths of the lower range, upper range and OOB lines, 0 when the
// table has no such line.
type stdTable struct {
	lines                    [][3]int64
	lowerLen, upperLen, oobL int
}

// build turns the description into table lines in the order B.2 lists them:
// normal lines, lower range, upper range, out-of-band.
func (s stdTable) build() []huffLine {
	lines := make([]huffLine, 0, len(s.lines)+3)
	for _, l := range s.lines {
		lines = append(lines, huffLine{prefLen: intconv.Clamp(l[1]), rangeLen: intconv.Clamp(l[2]), rangeLow: l[0]})
	}
	low := s.lines[0][0]
	last := s.lines[len(s.lines)-1]
	high := last[0] + int64(1)<<last[2]
	if s.lowerLen > 0 {
		lines = append(lines, huffLine{prefLen: s.lowerLen, rangeLen: 32, rangeLow: low - 1, lower: true})
	}
	if s.upperLen > 0 {
		lines = append(lines, huffLine{prefLen: s.upperLen, rangeLen: 32, rangeLow: high})
	}
	if s.oobL > 0 {
		lines = append(lines, huffLine{prefLen: s.oobL, oob: true})
	}
	return lines
}

// standardTables transcribes the standard Huffman tables of ITU-T T.88, B.5,
// Tables B.1 to B.15, indexed from 1.
//
//nolint:gochecknoglobals // static spec table, ITU-T T.88 Tables B.1 to B.15
var standardTables = [16]stdTable{
	1: {lines: [][3]int64{{0, 1, 4}, {16, 2, 8}, {272, 3, 16}}, upperLen: 3},
	2: {lines: [][3]int64{{0, 1, 0}, {1, 2, 0}, {2, 3, 0}, {3, 4, 3}, {11, 5, 6}}, upperLen: 6, oobL: 6},
	3: {lines: [][3]int64{{-256, 8, 8}, {0, 1, 0}, {1, 2, 0}, {2, 3, 0}, {3, 4, 3}, {11, 5, 6}}, lowerLen: 8, upperLen: 7, oobL: 6},
	4: {lines: [][3]int64{{1, 1, 0}, {2, 2, 0}, {3, 3, 0}, {4, 4, 3}, {12, 5, 6}}, upperLen: 5},
	5: {lines: [][3]int64{{-255, 7, 8}, {1, 1, 0}, {2, 2, 0}, {3, 3, 0}, {4, 4, 3}, {12, 5, 6}}, lowerLen: 7, upperLen: 6},
	6: {lines: [][3]int64{
		{-2048, 5, 10},
		{-1024, 4, 9},
		{-512, 4, 8},
		{-256, 4, 7},
		{-128, 5, 6},
		{-64, 5, 5},
		{-32, 4, 5},
		{0, 2, 7},
		{128, 3, 7},
		{256, 3, 8},
		{512, 4, 9},
		{1024, 4, 10},
	}, lowerLen: 6, upperLen: 6},
	7: {lines: [][3]int64{
		{-1024, 4, 9},
		{-512, 3, 8},
		{-256, 4, 7},
		{-128, 5, 6},
		{-64, 5, 5},
		{-32, 4, 5},
		{0, 4, 5},
		{32, 5, 5},
		{64, 5, 6},
		{128, 4, 7},
		{256, 3, 8},
		{512, 3, 9},
		{1024, 3, 10},
	}, lowerLen: 5, upperLen: 5},
	8: {lines: [][3]int64{
		{-15, 8, 3},
		{-7, 9, 1},
		{-5, 8, 1},
		{-3, 9, 0},
		{-2, 7, 0},
		{-1, 4, 0},
		{0, 2, 1},
		{2, 5, 0},
		{3, 6, 0},
		{4, 3, 4},
		{20, 6, 1},
		{22, 4, 4},
		{38, 4, 5},
		{70, 5, 6},
		{134, 5, 7},
		{262, 6, 7},
		{390, 7, 8},
		{646, 6, 10},
	}, lowerLen: 9, upperLen: 9, oobL: 2},
	9: {lines: [][3]int64{
		{-31, 8, 4},
		{-15, 9, 2},
		{-11, 8, 2},
		{-7, 9, 1},
		{-5, 7, 1},
		{-3, 4, 1},
		{-1, 3, 1},
		{1, 3, 1},
		{3, 5, 1},
		{5, 6, 1},
		{7, 3, 5},
		{39, 6, 2},
		{43, 4, 5},
		{75, 4, 6},
		{139, 5, 7},
		{267, 5, 8},
		{523, 6, 8},
		{779, 7, 9},
		{1291, 6, 11},
	}, lowerLen: 9, upperLen: 9, oobL: 2},
	10: {lines: [][3]int64{
		{-21, 7, 4},
		{-5, 8, 0},
		{-4, 7, 0},
		{-3, 5, 0},
		{-2, 2, 2},
		{2, 5, 0},
		{3, 6, 0},
		{4, 7, 0},
		{5, 8, 0},
		{6, 2, 6},
		{70, 5, 5},
		{102, 6, 5},
		{134, 6, 6},
		{198, 6, 7},
		{326, 6, 8},
		{582, 6, 9},
		{1094, 6, 10},
		{2118, 7, 11},
	}, lowerLen: 8, upperLen: 8, oobL: 2},
	11: {lines: [][3]int64{
		{1, 1, 0},
		{2, 2, 1},
		{4, 4, 0},
		{5, 4, 1},
		{7, 5, 1},
		{9, 5, 2},
		{13, 6, 2},
		{17, 7, 2},
		{21, 7, 3},
		{29, 7, 4},
		{45, 7, 5},
		{77, 7, 6},
	}, upperLen: 7},
	12: {lines: [][3]int64{
		{1, 1, 0},
		{2, 2, 0},
		{3, 3, 1},
		{5, 5, 0},
		{6, 5, 1},
		{8, 6, 1},
		{10, 7, 0},
		{11, 7, 1},
		{13, 7, 2},
		{17, 7, 3},
		{25, 7, 4},
		{41, 8, 5},
	}, upperLen: 8},
	13: {lines: [][3]int64{
		{1, 1, 0},
		{2, 3, 0},
		{3, 4, 0},
		{4, 5, 0},
		{5, 4, 1},
		{7, 3, 3},
		{15, 6, 1},
		{17, 6, 2},
		{21, 6, 3},
		{29, 6, 4},
		{45, 6, 5},
		{77, 7, 6},
	}, upperLen: 7},
	14: {lines: [][3]int64{{-2, 3, 0}, {-1, 3, 0}, {0, 1, 0}, {1, 3, 0}, {2, 3, 0}}},
	15: {lines: [][3]int64{
		{-24, 7, 4},
		{-8, 6, 2},
		{-4, 5, 1},
		{-2, 4, 0},
		{-1, 3, 0},
		{0, 1, 0},
		{1, 3, 0},
		{2, 4, 0},
		{3, 5, 1},
		{5, 6, 2},
		{9, 7, 4},
	}, lowerLen: 7, upperLen: 7},
}

// standardTable returns standard Table B.n with its codes assigned.
func standardTable(n int) (*huffTable, error) {
	if n < 1 || n >= len(standardTables) {
		return nil, fmt.Errorf("%w: standard Huffman table B.%d", ErrMalformed, n)
	}
	t, err := newHuffTable(standardTables[n].build())
	if err != nil {
		return nil, fmt.Errorf("standard Huffman table B.%d: %w", n, err)
	}
	return t, nil
}

// parseCodeTable decodes a code table segment's data, ITU-T T.88 B.2.
func parseCodeTable(data []byte) (*huffTable, error) {
	c := &cursor{b: data}
	flags := c.u8()
	low := c.s32()
	high := c.s32()
	if c.err != nil {
		return nil, c.err
	}
	hasOOB := flags&1 != 0
	prefixBits := int(flags>>1&7) + 1
	rangeBits := int(flags>>4&7) + 1
	if high <= low {
		return nil, fmt.Errorf("%w: code table range %d to %d", ErrMalformed, low, high)
	}

	r := &bitReader{data: data[c.off:]}
	var lines []huffLine
	// Step 5: lines cover the range from HTLOW upward until they reach
	// HTHIGH. Each line reads at least two bits, so the data length bounds
	// the loop.
	for cur := low; cur < high; {
		prefLen, err := r.readBits(prefixBits)
		if err != nil {
			return nil, err
		}
		rangeLen, err := r.readBits(rangeBits)
		if err != nil {
			return nil, err
		}
		if rangeLen > 32 {
			return nil, fmt.Errorf("%w: code table line range length %d", ErrMalformed, rangeLen)
		}
		if len(lines) == maxTableLines {
			return nil, fmt.Errorf("%w: code table defines more than %d lines", ErrLimit, maxTableLines)
		}
		lines = append(lines, huffLine{prefLen: prefLen, rangeLen: rangeLen, rangeLow: cur})
		cur += 1 << rangeLen
	}
	lowerLen, err := r.readBits(prefixBits)
	if err != nil {
		return nil, err
	}
	upperLen, err := r.readBits(prefixBits)
	if err != nil {
		return nil, err
	}
	lines = append(lines,
		huffLine{prefLen: lowerLen, rangeLen: 32, rangeLow: low - 1, lower: true},
		huffLine{prefLen: upperLen, rangeLen: 32, rangeLow: high})
	if hasOOB {
		oobLen, err := r.readBits(prefixBits)
		if err != nil {
			return nil, err
		}
		lines = append(lines, huffLine{prefLen: oobLen, oob: true})
	}
	return newHuffTable(lines)
}

// runCodes is the number of run codes of ITU-T T.88 Table 29.
const runCodes = 35

// decodeSymbolIDTable decodes the symbol ID Huffman table of a text region,
// ITU-T T.88 7.4.3.1.7: run code lengths, run-length coded symbol code
// lengths, then codes assigned by B.3. Each symbol becomes a table line whose
// value is its ID.
func decodeSymbolIDTable(r *bitReader, numSyms int) (*huffTable, error) {
	runLines := make([]huffLine, runCodes)
	for i := range runLines {
		n, err := r.readBits(4)
		if err != nil {
			return nil, err
		}
		runLines[i] = huffLine{prefLen: n, rangeLow: int64(i)}
	}
	runTable, err := newHuffTable(runLines)
	if err != nil {
		return nil, err
	}

	lengths := make([]huffLine, 0, numSyms)
	for len(lengths) < numSyms {
		code, err := runTable.value(r, "symbol ID run code")
		if err != nil {
			return nil, err
		}
		length, repeat := 0, 1
		switch code {
		case 32:
			// Table 29: repeat the previous length 3 to 6 times.
			if len(lengths) == 0 {
				return nil, fmt.Errorf("%w: symbol ID run code 32 has no previous length to repeat", ErrMalformed)
			}
			n, err := r.readBits(2)
			if err != nil {
				return nil, err
			}
			length, repeat = lengths[len(lengths)-1].prefLen, n+3
		case 33:
			n, err := r.readBits(3)
			if err != nil {
				return nil, err
			}
			repeat = n + 3
		case 34:
			n, err := r.readBits(7)
			if err != nil {
				return nil, err
			}
			repeat = n + 11
		default:
			length = intconv.Clamp(code)
		}
		if len(lengths)+repeat > numSyms {
			return nil, fmt.Errorf("%w: symbol ID code lengths run past the %d symbols", ErrMalformed, numSyms)
		}
		for range repeat {
			lengths = append(lengths, huffLine{prefLen: length, rangeLow: int64(len(lengths))})
		}
	}
	r.align()
	return newHuffTable(lengths)
}
