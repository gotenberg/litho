package jbig2

import (
	"fmt"
	"math"
)

// huffCode represents a single Huffman code entry.
type huffCode struct {
	prefixLength int
	rangeLength  int
	rangeLow     int
	isLowerRange bool
	code         int
}

// huffNode is the interface for Huffman tree nodes.
type huffNode interface {
	decode(r *Reader) (int64, error)
}

// huffInternalNode is a branch node in the Huffman tree.
type huffInternalNode struct {
	depth int
	zero  huffNode
	one   huffNode
}

func (n *huffInternalNode) decode(r *Reader) (int64, error) {
	bit, err := r.ReadBit()
	if err != nil {
		return 0, err
	}
	var child huffNode
	if bit == 0 {
		child = n.zero
	} else {
		child = n.one
	}
	if child == nil {
		return 0, fmt.Errorf("%w: invalid Huffman code", ErrCorruptedData)
	}
	return child.decode(r)
}

func (n *huffInternalNode) append(c *huffCode) {
	if c.prefixLength == 0 {
		return
	}

	shift := c.prefixLength - 1 - n.depth
	if shift < 0 {
		return
	}

	bit := (c.code >> uint(shift)) & 1
	if shift == 0 {
		if c.rangeLength == -1 {
			// OOB node
			node := &huffOOBNode{}
			if bit == 1 {
				n.one = node
			} else {
				n.zero = node
			}
		} else {
			// Value node
			node := &huffValueNode{
				rangeLen:     c.rangeLength,
				rangeLow:     c.rangeLow,
				isLowerRange: c.isLowerRange,
			}
			if bit == 1 {
				n.one = node
			} else {
				n.zero = node
			}
		}
	} else {
		if bit == 1 {
			if n.one == nil {
				n.one = &huffInternalNode{depth: n.depth + 1}
			}
			n.one.(*huffInternalNode).append(c)
		} else {
			if n.zero == nil {
				n.zero = &huffInternalNode{depth: n.depth + 1}
			}
			n.zero.(*huffInternalNode).append(c)
		}
	}
}

// huffValueNode is a leaf node containing a decoded value range.
type huffValueNode struct {
	rangeLen     int
	rangeLow     int
	isLowerRange bool
}

func (n *huffValueNode) decode(r *Reader) (int64, error) {
	if n.rangeLen == 0 {
		return int64(n.rangeLow), nil
	}
	bits, err := r.ReadBits(n.rangeLen)
	if err != nil {
		return 0, err
	}
	if n.isLowerRange {
		return int64(n.rangeLow) - bits, nil
	}
	return int64(n.rangeLow) + bits, nil
}

// huffOOBNode is a leaf node indicating out-of-band.
type huffOOBNode struct{}

func (n *huffOOBNode) decode(_ *Reader) (int64, error) {
	return math.MaxInt64, nil
}

// HuffmanTable is a Huffman decoding table built from a code list.
type HuffmanTable struct {
	root *huffInternalNode
}

// NewHuffmanTable builds a Huffman table from the given code entries.
func NewHuffmanTable(codes []*huffCode) *HuffmanTable {
	preprocessCodes(codes)
	root := &huffInternalNode{}
	for _, c := range codes {
		root.append(c)
	}
	return &HuffmanTable{root: root}
}

// Decode reads and decodes a Huffman-coded value from the reader.
// Returns (value, isOOB, error).
func (ht *HuffmanTable) Decode(r *Reader) (int64, bool, error) {
	v, err := ht.root.decode(r)
	if err != nil {
		return 0, false, err
	}
	if v == math.MaxInt64 {
		return 0, true, nil
	}
	return v, false, nil
}

// preprocessCodes assigns canonical Huffman codes to the code entries (Annex B.3).
func preprocessCodes(codes []*huffCode) {
	maxPrefixLen := 0
	for _, c := range codes {
		if c.prefixLength > maxPrefixLen {
			maxPrefixLen = c.prefixLength
		}
	}

	lenCount := make([]int, maxPrefixLen+1)
	for _, c := range codes {
		lenCount[c.prefixLength]++
	}

	firstCode := make([]int, maxPrefixLen+2)
	lenCount[0] = 0

	for curLen := 1; curLen <= maxPrefixLen; curLen++ {
		firstCode[curLen] = (firstCode[curLen-1] + lenCount[curLen-1]) << 1
		curCode := firstCode[curLen]
		for _, c := range codes {
			if c.prefixLength == curLen {
				c.code = curCode
				curCode++
			}
		}
	}
}

// Standard Huffman tables B.1 through B.15 (ISO 14492 Annex B).
// Each entry is [prefixLength, rangeLength, rangeLow]. A 4th element flags isLowerRange.
var standardTableDefs = [15][][]int{
	// B1
	{{1, 4, 0}, {2, 8, 16}, {3, 16, 272}, {3, 32, 65808}},
	// B2
	{{1, 0, 0}, {2, 0, 1}, {3, 0, 2}, {4, 3, 3}, {5, 6, 11}, {6, 32, 75}, {6, -1, 0}},
	// B3
	{
		{8, 8, -256},
		{1, 0, 0},
		{2, 0, 1},
		{3, 0, 2},
		{4, 3, 3},
		{5, 6, 11},
		{8, 32, -257, 1},
		{7, 32, 75},
		{6, -1, 0},
	},
	// B4
	{{1, 0, 1}, {2, 0, 2}, {3, 0, 3}, {4, 3, 4}, {5, 6, 12}, {5, 32, 76}},
	// B5
	{
		{7, 8, -255},
		{1, 0, 1},
		{2, 0, 2},
		{3, 0, 3},
		{4, 3, 4},
		{5, 6, 12},
		{7, 32, -256, 1},
		{6, 32, 76},
	},
	// B6
	{
		{5, 10, -2048},
		{4, 9, -1024},
		{4, 8, -512},
		{4, 7, -256},
		{5, 6, -128},
		{5, 5, -64},
		{4, 5, -32},
		{2, 7, 0},
		{3, 7, 128},
		{3, 8, 256},
		{4, 9, 512},
		{4, 10, 1024},
		{6, 32, -2049, 1},
		{6, 32, 2048},
	},
	// B7
	{
		{4, 9, -1024},
		{3, 8, -512},
		{4, 7, -256},
		{5, 6, -128},
		{5, 5, -64},
		{4, 5, -32},
		{4, 5, 0},
		{5, 5, 32},
		{5, 6, 64},
		{4, 7, 128},
		{3, 8, 256},
		{3, 9, 512},
		{3, 10, 1024},
		{5, 32, -1025, 1},
		{5, 32, 2048},
	},
	// B8
	{
		{8, 3, -15},
		{9, 1, -7},
		{8, 1, -5},
		{9, 0, -3},
		{7, 0, -2},
		{4, 0, -1},
		{2, 1, 0},
		{5, 0, 2},
		{6, 0, 3},
		{3, 4, 4},
		{6, 1, 20},
		{4, 4, 22},
		{4, 5, 38},
		{5, 6, 70},
		{5, 7, 134},
		{6, 7, 262},
		{7, 8, 390},
		{6, 10, 646},
		{9, 32, -16, 1},
		{9, 32, 1670},
		{2, -1, 0},
	},
	// B9
	{
		{8, 4, -31},
		{9, 2, -15},
		{8, 2, -11},
		{9, 1, -7},
		{7, 1, -5},
		{4, 1, -3},
		{3, 1, -1},
		{3, 1, 1},
		{5, 1, 3},
		{6, 1, 5},
		{3, 5, 7},
		{6, 2, 39},
		{4, 5, 43},
		{4, 6, 75},
		{5, 7, 139},
		{5, 8, 267},
		{6, 8, 523},
		{7, 9, 779},
		{6, 11, 1291},
		{9, 32, -32, 1},
		{9, 32, 3339},
		{2, -1, 0},
	},
	// B10
	{
		{7, 4, -21},
		{8, 0, -5},
		{7, 0, -4},
		{5, 0, -3},
		{2, 2, -2},
		{5, 0, 2},
		{6, 0, 3},
		{7, 0, 4},
		{8, 0, 5},
		{2, 6, 6},
		{5, 5, 70},
		{6, 5, 102},
		{6, 6, 134},
		{6, 7, 198},
		{6, 8, 326},
		{6, 9, 582},
		{6, 10, 1094},
		{7, 11, 2118},
		{8, 32, -22, 1},
		{8, 32, 4166},
		{2, -1, 0},
	},
	// B11
	{
		{1, 0, 1},
		{2, 1, 2},
		{4, 0, 4},
		{4, 1, 5},
		{5, 1, 7},
		{5, 2, 9},
		{6, 2, 13},
		{7, 2, 17},
		{7, 3, 21},
		{7, 4, 29},
		{7, 5, 45},
		{7, 6, 77},
		{7, 32, 141},
	},
	// B12
	{
		{1, 0, 1},
		{2, 0, 2},
		{3, 1, 3},
		{5, 0, 5},
		{5, 1, 6},
		{6, 1, 8},
		{7, 0, 10},
		{7, 1, 11},
		{7, 2, 13},
		{7, 3, 17},
		{7, 4, 25},
		{8, 5, 41},
		{8, 32, 73},
	},
	// B13
	{
		{1, 0, 1},
		{3, 0, 2},
		{4, 0, 3},
		{5, 0, 4},
		{4, 1, 5},
		{3, 3, 7},
		{6, 1, 15},
		{6, 2, 17},
		{6, 3, 21},
		{6, 4, 29},
		{6, 5, 45},
		{7, 6, 77},
		{7, 32, 141},
	},
	// B14
	{{3, 0, -2}, {3, 0, -1}, {1, 0, 0}, {3, 0, 1}, {3, 0, 2}},
	// B15
	{
		{7, 4, -24},
		{6, 2, -8},
		{5, 1, -4},
		{4, 0, -2},
		{3, 0, -1},
		{1, 0, 0},
		{3, 0, 1},
		{4, 0, 2},
		{5, 1, 3},
		{6, 2, 5},
		{7, 4, 9},
		{7, 32, -25, 1},
		{7, 32, 25},
	},
}

var standardTables [15]*HuffmanTable

// GetStandardTable returns the standard Huffman table with the given number (1-15).
func GetStandardTable(number int) *HuffmanTable {
	idx := number - 1
	if standardTables[idx] == nil {
		def := standardTableDefs[idx]
		codes := make([]*huffCode, len(def))
		for i, entry := range def {
			isLower := len(entry) > 3
			codes[i] = &huffCode{
				prefixLength: entry[0],
				rangeLength:  entry[1],
				rangeLow:     entry[2],
				isLowerRange: isLower,
			}
		}
		standardTables[idx] = NewHuffmanTable(codes)
	}
	return standardTables[idx]
}

// NewEncodedTable builds a Huffman table from a Table segment's data.
func NewEncodedTable(r *Reader, htOOB, htPS, htRS, htLow, htHigh int) (*HuffmanTable, error) {
	var codes []*huffCode
	curRangeLow := htLow

	for curRangeLow < htHigh {
		prefLen, err := r.ReadBits(htPS)
		if err != nil {
			return nil, err
		}
		rangeLen, err := r.ReadBits(htRS)
		if err != nil {
			return nil, err
		}
		codes = append(codes, &huffCode{
			prefixLength: int(prefLen),
			rangeLength:  int(rangeLen),
			rangeLow:     curRangeLow,
		})
		curRangeLow += 1 << rangeLen //nolint:gosec // rangeLen is small
	}

	// Lower range line
	prefLen, err := r.ReadBits(htPS)
	if err != nil {
		return nil, err
	}
	codes = append(codes, &huffCode{
		prefixLength: int(prefLen),
		rangeLength:  32,
		rangeLow:     htLow - 1,
		isLowerRange: true,
	})

	// Upper range line
	prefLen, err = r.ReadBits(htPS)
	if err != nil {
		return nil, err
	}
	codes = append(codes, &huffCode{
		prefixLength: int(prefLen),
		rangeLength:  32,
		rangeLow:     htHigh,
	})

	// OOB line
	if htOOB == 1 {
		prefLen, err = r.ReadBits(htPS)
		if err != nil {
			return nil, err
		}
		codes = append(codes, &huffCode{
			prefixLength: int(prefLen),
			rangeLength:  -1,
			rangeLow:     -1,
		})
	}

	return NewHuffmanTable(codes), nil
}
