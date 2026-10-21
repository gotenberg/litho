package cff

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/gotenberg/litho/pkg/internal/render/geom"
)

// CFF2 header fields (OpenType specification, CFF2 table, Header): the major
// version, and the five bytes a version 2.0 header holds.
const (
	cff2MajorVersion = 2
	cff2HeaderSize   = 5
)

// DICT operators of a CFF2 Top DICT and Private DICT that CFF does not have,
// or encodes differently (OpenType specification, CFF2 table, TopDICT and
// PrivateDICT operator summaries).
const (
	opCFF2VStore  = 24
	opCFF2VSIndex = 22
	opCFF2Blend   = 23
)

// CFF2 FDSelect formats (OpenType specification, CFF2 table, FontDICTSelect):
// one Font DICT index per glyph, and ranges of 16-bit or 32-bit glyph IDs.
const (
	fdSelectFormat0 = 0
	fdSelectFormat3 = 3
	fdSelectFormat4 = 4
)

// noVariationStore marks the region count of a font without a VariationStore,
// whose charstrings may use neither blend nor vsindex (OpenType specification,
// CFF2 table, VariationStore).
const noVariationStore = -1

// ParseCFF2 reads a 'CFF2' table (OpenType specification, CFF2 table): the
// header, the Top DICT, the Global Subr INDEX, the CharStrings INDEX, the
// VariationStore, and the Private DICT and local subroutines of each Font DICT
// of the FDArray with its FDSelect. An OpenType program carries the table
// beside the 'hmtx' that holds its advance widths, which truetype.CFF2Table
// returns.
//
// The returned [Font] draws the default instance: a blend keeps its default
// values, every region scalar being 0 there (OpenType Font Variations
// Overview, algorithm for interpolation of instance values). It is neither
// name-keyed nor CID-keyed, CFF2 having no charset; its glyphs advance by 0,
// a CFF2 charstring stating no width.
//
// It fails on data that is not a CFF2 program, on a program without glyphs or
// without an FDArray, on a Private DICT whose vsindex names no ItemVariationData,
// and on a structure an offset points outside the data.
func ParseCFF2(data []byte) (*Font, error) {
	if len(data) < cff2HeaderSize || data[0] != cff2MajorVersion {
		return nil, fmt.Errorf("%w: not a CFF2 header", ErrInvalidCFF)
	}
	topStart := int(data[2])
	topEnd := topStart + int(binary.BigEndian.Uint16(data[3:5]))
	if topStart < cff2HeaderSize || topEnd > len(data) {
		return nil, fmt.Errorf("%w: Top DICT out of bounds", ErrInvalidCFF)
	}
	// The Global Subr INDEX follows the Top DICT.
	_, gsubrs, err := parseCFF2INDEX(data, topEnd)
	if err != nil {
		return nil, fmt.Errorf("reading the Global Subr INDEX: %w", err)
	}
	if hasNilEntry(gsubrs) {
		return nil, fmt.Errorf("%w: a global subroutine out of bounds", ErrInvalidCFF)
	}
	top := readCFF2Top(data[topStart:topEnd])
	if top.charStrings <= 0 || top.charStrings >= len(data) {
		return nil, fmt.Errorf("%w: no CharStrings INDEX", ErrInvalidCFF)
	}
	_, charStrings, err := parseCFF2INDEX(data, top.charStrings)
	if err != nil {
		return nil, fmt.Errorf("reading the CharStrings INDEX: %w", err)
	}
	if len(charStrings) == 0 {
		return nil, fmt.Errorf("%w: empty CharStrings INDEX", ErrInvalidCFF)
	}
	f := &Font{charStrings: charStrings, globalSubrs: gsubrs, cff2: true}
	if top.vstore > 0 {
		if f.regionCounts, err = readRegionCounts(data, top.vstore); err != nil {
			return nil, fmt.Errorf("reading the VariationStore: %w", err)
		}
	}
	matrix := defaultFontMatrix
	if top.hasMatrix {
		matrix = top.matrix
	}
	if err := f.readCFF2FDArray(data, top, matrix); err != nil {
		return nil, err
	}
	return f, nil
}

// parseCFF2INDEX reads a CFF2 INDEX, whose count is a Card32 (OpenType
// specification, CFF2 table, INDEX data).
func parseCFF2INDEX(data []byte, pos int) (endPos int, entries [][]byte, err error) {
	if pos < 0 || pos+4 > len(data) {
		return 0, nil, ErrTruncated
	}
	count := binary.BigEndian.Uint32(data[pos : pos+4])
	if uint64(count) > uint64(len(data)) {
		return 0, nil, ErrTruncated
	}
	return parseINDEXBody(data, pos+4, int(count))
}

// cff2Top is what the outline reader takes from a CFF2 Top DICT. Offsets it
// does not state are 0, which no structure of the table can start at.
type cff2Top struct {
	charStrings, fdArray, fdSelect, vstore int
	matrix                                 geom.Matrix
	hasMatrix                              bool
}

// readCFF2Top reads the Top DICT operators an outline depends on (OpenType
// specification, CFF2 table, TopDICT operator summary). A Top DICT holds no
// blend, so its operands are read as they stand.
func readCFF2Top(dict []byte) cff2Top {
	var top cff2Top
	scanCFF2Dict(dict, func() int { return 0 }, func(op int, operands []float64) {
		last := 0
		if n := len(operands); n > 0 && operands[n-1] > 0 && operands[n-1] <= math.MaxInt32 {
			last = int(operands[n-1])
		}
		switch op {
		case opCharStrings:
			top.charStrings = last
		case opFDArray:
			top.fdArray = last
		case opFDSelect:
			top.fdSelect = last
		case opCFF2VStore:
			top.vstore = last
		case opFontMatrix:
			if m, ok := dictMatrix(operands); ok {
				top.matrix, top.hasMatrix = m, true
			}
		}
	})
	return top
}

// scanCFF2Dict calls fn with each operator of a CFF2 DICT and its operands,
// as [scanDict] does for CFF. CFF2 adds the operators 22 to 24 (vsindex, blend
// and VariationStoreOffset), and a blend is resolved here: it leaves on the
// stack the n default values of the n + n*k + 1 operands it takes, k being
// the region count regions returns, the current vsindex's (OpenType
// specification, CFF2 table, PrivateDICT variation operators). The operands
// stay within the CFF2 stack limit, the 513 of [maxCharstringStack].
func scanCFF2Dict(dict []byte, regions func() int, fn func(op int, operands []float64)) {
	operands := make([]float64, 0, maxDictOperands)
	for i := 0; i < len(dict); {
		b := dict[i]
		if len(operands) >= maxCharstringStack && b > opCFF2VStore {
			operands = operands[:0]
		}
		switch {
		case b == opCFF2Blend:
			operands = blendDefaults(operands, regions())
			i++
		case b <= opCFF2VStore:
			op := int(b)
			if b == 12 && i+1 < len(dict) {
				i++
				op = 1200 + int(dict[i])
			}
			fn(op, operands)
			operands = operands[:0]
			i++
		case b >= 32 && b <= 246:
			operands = append(operands, float64(int32(b)-139))
			i++
		case b >= 247 && b <= 250:
			if i+1 >= len(dict) {
				return
			}
			operands = append(operands, float64((int32(b)-247)*256+int32(dict[i+1])+108))
			i += 2
		case b >= 251 && b <= 254:
			if i+1 >= len(dict) {
				return
			}
			operands = append(operands, float64(-(int32(b)-251)*256-int32(dict[i+1])-108))
			i += 2
		case b == 28:
			if i+2 >= len(dict) {
				return
			}
			operands = append(operands, float64(int16(binary.BigEndian.Uint16(dict[i+1:i+3])))) //nolint:gosec // G115: a two-byte DICT operand is a two's-complement int16.
			i += 3
		case b == 29:
			if i+4 >= len(dict) {
				return
			}
			operands = append(operands, float64(int32(binary.BigEndian.Uint32(dict[i+1:i+5])))) //nolint:gosec // G115: a four-byte DICT operand is a two's-complement int32.
			i += 5
		case b == 30:
			val, end := parseRealNumber(dict, i+1)
			operands = append(operands, val)
			i = end
		default:
			i++
		}
	}
}

// blendDefaults applies a blend to stack, whose top is n: it removes n and
// the n*k deltas and keeps the n default values below them, the default
// instance's blend. A blend whose operands the stack does not hold, or with
// no VariationStore to give k, clears the stack.
func blendDefaults(stack []float64, k int) []float64 {
	defaults, ok := blendSplit(stack, k)
	if !ok {
		return stack[:0]
	}
	return stack[:defaults]
}

// blendSplit returns the length the stack keeps after a blend of the n on its
// top: the operands below the blend's, then its n default values. ok is false
// when the operands are not there or k is unknown.
func blendSplit(stack []float64, k int) (int, bool) {
	if k < 0 || len(stack) == 0 {
		return 0, false
	}
	nv := stack[len(stack)-1]
	rest := len(stack) - 1
	if nv < 0 || nv != math.Trunc(nv) || nv > float64(rest) {
		return 0, false
	}
	n := int(nv)
	// rest bounds n, and k counts the regions of a uint16-sized
	// regionIndexes array, so the product stays well inside an int.
	need := n * (k + 1)
	if need > rest {
		return 0, false
	}
	return rest - need + n, true
}

// readRegionCounts reads the VariationStore at pos: a uint16 length, then an
// ItemVariationStore (OpenType specification, CFF2 table, VariationStore, and
// OpenType Font Variations Common Table Formats, Item Variation Store). It
// returns the regionIndexCount of each ItemVariationData, the k a blend
// takes under the vsindex selecting it; the default instance needs no region
// itself.
func readRegionCounts(data []byte, pos int) ([]int, error) {
	if pos+2 > len(data) {
		return nil, ErrTruncated
	}
	length := int(binary.BigEndian.Uint16(data[pos:]))
	store := data[pos+2:]
	if length > len(store) {
		return nil, ErrTruncated
	}
	store = store[:length]
	// format, variationRegionListOffset, itemVariationDataCount.
	const ivsHeader = 8
	if len(store) < ivsHeader {
		return nil, ErrTruncated
	}
	count := int(binary.BigEndian.Uint16(store[6:8]))
	if ivsHeader+4*count > len(store) {
		return nil, ErrTruncated
	}
	counts := make([]int, count)
	for i := range count {
		off := binary.BigEndian.Uint32(store[ivsHeader+4*i:])
		// itemCount, wordDeltaCount, regionIndexCount.
		if uint64(off)+6 > uint64(len(store)) {
			return nil, fmt.Errorf("%w: ItemVariationData %d out of bounds", ErrInvalidCFF, i)
		}
		counts[i] = int(binary.BigEndian.Uint16(store[off+4:]))
	}
	return counts, nil
}

// readCFF2FDArray reads the Font DICTs of the FDArray, their Private DICTs and
// the FDSelect (OpenType specification, CFF2 table, FontDICTINDEX and
// FontDICTSelect). A CFF2 Font DICT holds no FontMatrix: every glyph draws
// through the Top DICT's.
func (f *Font) readCFF2FDArray(data []byte, top cff2Top, matrix geom.Matrix) error {
	if top.fdArray <= 0 || top.fdArray >= len(data) {
		return fmt.Errorf("%w: a CFF2 program without FDArray", ErrInvalidCFF)
	}
	_, fds, err := parseCFF2INDEX(data, top.fdArray)
	if err != nil {
		return fmt.Errorf("reading the FDArray: %w", err)
	}
	if len(fds) == 0 {
		return fmt.Errorf("%w: empty FDArray", ErrInvalidCFF)
	}
	for i, fd := range fds {
		if fd == nil {
			return fmt.Errorf("%w: Font DICT %d out of bounds", ErrInvalidCFF, i)
		}
		size, offset := -1, -1
		scanCFF2Dict(fd, func() int { return 0 }, func(op int, operands []float64) {
			if n := len(operands); op == opPrivate && n >= 2 && operands[n-2] >= 0 && operands[n-1] >= 0 &&
				operands[n-2] <= math.MaxInt32 && operands[n-1] <= math.MaxInt32 {
				size, offset = int(operands[n-2]), int(operands[n-1])
			}
		})
		private, err := f.readCFF2Private(data, size, offset)
		if err != nil {
			return fmt.Errorf("reading the Private DICT of Font DICT %d: %w", i, err)
		}
		private.matrix = matrix
		f.privates = append(f.privates, private)
	}
	if top.fdSelect > 0 {
		f.fdForGID = parseCFF2FDSelect(data, top.fdSelect, len(f.charStrings))
	}
	return nil
}

// readCFF2Private reads the local subroutines and the vsindex of the Private
// DICT of size bytes at offset (OpenType specification, CFF2 table,
// PrivateDICT operator summary). A Font DICT that names no Private DICT takes
// no local subroutine and vsindex 0.
func (f *Font) readCFF2Private(data []byte, size, offset int) (outlinePrivate, error) {
	p := outlinePrivate{}
	if size < 0 || offset < 0 {
		p.regions = f.regionCount(0)
		return p, nil
	}
	if offset > len(data) || size > len(data)-offset {
		return outlinePrivate{}, fmt.Errorf("%w: Private DICT out of bounds", ErrInvalidCFF)
	}
	vsindex, subrs := 0, 0
	scanCFF2Dict(data[offset:offset+size], func() int { return f.regionCount(vsindex) }, func(op int, operands []float64) {
		n := len(operands)
		if n == 0 {
			return
		}
		v := operands[n-1]
		switch op {
		case opCFF2VSIndex:
			if v >= 0 && v <= math.MaxUint16 && v == math.Trunc(v) {
				vsindex = int(v)
			} else {
				vsindex = -1
			}
		case opSubrs:
			if v > 0 && v <= math.MaxInt32 {
				subrs = int(v)
			}
		}
	})
	if vsindex < 0 || vsindex > 0 && f.regionCount(vsindex) == noVariationStore {
		return outlinePrivate{}, fmt.Errorf("%w: vsindex names no ItemVariationData", ErrInvalidCFF)
	}
	p.regions = f.regionCount(vsindex)
	if subrs > 0 {
		at := offset + subrs
		if at >= len(data) {
			return outlinePrivate{}, fmt.Errorf("%w: Subrs offset out of bounds", ErrInvalidCFF)
		}
		var err error
		if _, p.subrs, err = parseCFF2INDEX(data, at); err != nil {
			return outlinePrivate{}, fmt.Errorf("reading a local Subrs INDEX: %w", err)
		}
		if hasNilEntry(p.subrs) {
			return outlinePrivate{}, fmt.Errorf("%w: a local subroutine out of bounds", ErrInvalidCFF)
		}
	}
	return p, nil
}

// regionCount returns k, the regions of ItemVariationData ivd, and
// [noVariationStore] when the font holds no such data.
func (f *Font) regionCount(ivd int) int {
	if ivd < 0 || ivd >= len(f.regionCounts) {
		return noVariationStore
	}
	return f.regionCounts[ivd]
}

// parseCFF2FDSelect reads a CFF2 FDSelect of format 0, 3 or 4 and returns the
// Font DICT of each glyph (OpenType specification, CFF2 table,
// FontDICTSelect). A range that does not start past the one before ends the
// read, the format requiring them sorted; the glyphs it leaves, and those of
// an unknown format, take Font DICT 0.
func parseCFF2FDSelect(data []byte, offset, numGlyphs int) []uint16 {
	out := make([]uint16, numGlyphs)
	if offset >= len(data) {
		return out
	}
	switch data[offset] {
	case fdSelectFormat0, fdSelectFormat3:
		for gid, fd := range parseFDSelect(data, offset, numGlyphs) {
			out[gid] = uint16(fd)
		}
	case fdSelectFormat4:
		// uint32 numRanges, then {uint32 first, uint16 fd} per range and a
		// uint32 sentinel.
		const rangeSize = 6
		if offset+5 > len(data) {
			return out
		}
		nRanges := binary.BigEndian.Uint32(data[offset+1:])
		pos := offset + 5
		prev := 0
		for r := uint32(0); r < nRanges && pos+rangeSize+4 <= len(data); r++ {
			first := int(binary.BigEndian.Uint32(data[pos:]))
			fd := binary.BigEndian.Uint16(data[pos+4:])
			next := int(binary.BigEndian.Uint32(data[pos+rangeSize:]))
			if first < prev || next < first {
				break
			}
			for gid := first; gid < next && gid < numGlyphs; gid++ {
				out[gid] = fd
			}
			prev = next
			pos += rangeSize
		}
	}
	return out
}
