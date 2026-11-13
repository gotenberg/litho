package varfont

import (
	"encoding/binary"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/intconv"
)

// Item variation store layout (OpenType Font Variations Common Table
// Formats, Item variation store): the header, the region list header, a
// region axis record (start, peak, end), and the ItemVariationData header.
const (
	itemStoreHeaderSize = 8
	regionListHeader    = 4
	regionAxisSize      = 6
	itemDataHeaderSize  = 6
)

// ItemVariationData wordDeltaCount bits (OpenType Font Variations Common
// Table Formats, ItemVariationData): deltas are int32 and int16 rather
// than int16 and int8, and the count of long deltas.
const (
	longWords      = 0x8000
	wordCountMask  = 0x7FFF
	noVariationIdx = 0xFFFF
)

// itemData is one ItemVariationData subtable.
type itemData struct {
	itemCount int
	wordCount int
	long      bool
	regions   []uint16
	rowSize   int
	rows      []byte
}

// itemStore is an item variation store with the scalar of each of its
// regions at one instance.
type itemStore struct {
	data    []itemData
	scalars []float64
}

// parseItemStore reads the item variation store at data and computes the
// scalar of each region at coords, the normalized coordinates of the
// instance (OpenType Font Variations Overview, Algorithm for interpolation
// of instance values). The store must define its regions over exactly
// len(coords) axes; nil coords stand for the default instance of a store
// over any number of axes, every axis at 0. The rows stay in data and are
// read on lookup.
func parseItemStore(data []byte, coords []int16) (*itemStore, error) {
	if len(data) < itemStoreHeaderSize || binary.BigEndian.Uint16(data) != 1 {
		return nil, fmt.Errorf("%w: item variation store header", ErrInvalid)
	}
	regionsAt := binary.BigEndian.Uint32(data[2:])
	count := int(binary.BigEndian.Uint16(data[6:]))
	if itemStoreHeaderSize+4*count > len(data) || uint64(regionsAt)+regionListHeader > uint64(len(data)) {
		return nil, fmt.Errorf("%w: item variation store past its table", ErrInvalid)
	}
	rl := data[regionsAt:]
	axisCount := int(binary.BigEndian.Uint16(rl))
	regionCount := int(binary.BigEndian.Uint16(rl[2:]))
	if coords != nil && axisCount != len(coords) {
		return nil, fmt.Errorf("%w: item variation regions over %d axes of %d", ErrInvalid, axisCount, len(coords))
	}
	if regionListHeader+regionCount*axisCount*regionAxisSize > len(rl) {
		return nil, fmt.Errorf("%w: variation region list past its table", ErrInvalid)
	}
	s := &itemStore{scalars: make([]float64, regionCount), data: make([]itemData, count)}
	for r := range regionCount {
		rec := rl[regionListHeader+r*axisCount*regionAxisSize:]
		s.scalars[r] = 1
		for a := range axisCount {
			at := a * regionAxisSize
			var v int32
			if coords != nil {
				v = int32(coords[a])
			}
			s.scalars[r] *= axisScalar(v, f2dot14At(rec, at), f2dot14At(rec, at+2), f2dot14At(rec, at+4))
		}
	}
	for i := range s.data {
		at := binary.BigEndian.Uint32(data[itemStoreHeaderSize+4*i:])
		d, err := parseItemData(data, at, regionCount)
		if err != nil {
			return nil, fmt.Errorf("item variation data %d: %w", i, err)
		}
		s.data[i] = d
	}
	return s, nil
}

// parseItemData reads the ItemVariationData at data[at:], whose region
// indices must name regions of a list of regionCount.
func parseItemData(data []byte, at uint32, regionCount int) (itemData, error) {
	if at == 0 {
		// A null offset is an empty subtable: every lookup into it is 0.
		return itemData{}, nil
	}
	if uint64(at)+itemDataHeaderSize > uint64(len(data)) {
		return itemData{}, fmt.Errorf("%w: header past its table", ErrInvalid)
	}
	b := data[at:]
	d := itemData{itemCount: int(binary.BigEndian.Uint16(b))}
	wc := binary.BigEndian.Uint16(b[2:])
	d.long = wc&longWords != 0
	d.wordCount = int(wc & wordCountMask)
	n := int(binary.BigEndian.Uint16(b[4:]))
	if d.wordCount > n || itemDataHeaderSize+2*n > len(b) {
		return itemData{}, fmt.Errorf("%w: %d word deltas of %d regions", ErrInvalid, d.wordCount, n)
	}
	d.regions = make([]uint16, n)
	for i := range d.regions {
		d.regions[i] = binary.BigEndian.Uint16(b[itemDataHeaderSize+2*i:])
		if int(d.regions[i]) >= regionCount {
			return itemData{}, fmt.Errorf("%w: region %d of %d", ErrInvalid, d.regions[i], regionCount)
		}
	}
	short := 1
	if d.long {
		short = 2
	}
	d.rowSize = d.wordCount*2*short + (n-d.wordCount)*short
	rowsAt := itemDataHeaderSize + 2*n
	if rowsAt+d.itemCount*d.rowSize > len(b) {
		return itemData{}, fmt.Errorf("%w: %d delta sets past the table", ErrInvalid, d.itemCount)
	}
	d.rows = b[rowsAt : rowsAt+d.itemCount*d.rowSize]
	return d, nil
}

// delta returns the interpolated delta of the item (outer, inner), the
// sum of each region's delta times its scalar (OpenType Font Variations
// Overview, Algorithm for interpolation of instance values), unrounded. An
// index past the store, and the index 0xFFFF/0xFFFF, give 0.
func (s *itemStore) delta(outer, inner int) float64 {
	if s == nil || outer >= len(s.data) {
		return 0
	}
	d := &s.data[outer]
	if inner >= d.itemCount {
		return 0
	}
	row := d.rows[inner*d.rowSize:]
	var sum float64
	at := 0
	for i, r := range d.regions {
		var v int32
		switch {
		case d.long && i < d.wordCount:
			v = int32(binary.BigEndian.Uint32(row[at:])) //nolint:gosec // reinterprets a signed int32 delta
			at += 4
		case d.long || i < d.wordCount:
			v = int32(int16(binary.BigEndian.Uint16(row[at:]))) //nolint:gosec // reinterprets a signed int16 delta
			at += 2
		default:
			v = int32(int8(row[at])) //nolint:gosec // reinterprets a signed int8 delta
			at++
		}
		if sc := s.scalars[r]; sc != 0 {
			sum += float64(sc * float64(v))
		}
	}
	return sum
}

// axisScalar returns the per-axis scalar of a region whose start, peak and
// end on the axis are start, peak and end at the instance coordinate v,
// all F2DOT14 (OpenType Font Variations Overview, Algorithm for
// interpolation of instance values).
func axisScalar(v, start, peak, end int32) float64 {
	switch {
	case start > peak || peak > end:
		return 1
	case start < 0 && end > 0 && peak != 0:
		return 1
	case peak == 0:
		return 1
	case v < start || v > end:
		return 0
	case v == peak:
		return 1
	case v < peak:
		return float64(v-start) / float64(peak-start)
	}
	return float64(end-v) / float64(end-peak)
}

// deltaSetIndexMap is a DeltaSetIndexMap (OpenType Font Variations Common
// Table Formats): the item each glyph's delta set is.
type deltaSetIndexMap struct {
	count     int
	entrySize int
	innerBits uint
	data      []byte
}

// parseDeltaSetIndexMap reads the DeltaSetIndexMap at data[at:].
func parseDeltaSetIndexMap(data []byte, at uint32) (*deltaSetIndexMap, error) {
	if uint64(at)+4 > uint64(len(data)) {
		return nil, fmt.Errorf("%w: delta-set index map past its table", ErrInvalid)
	}
	b := data[at:]
	m := &deltaSetIndexMap{}
	format, entryFormat := b[0], b[1]
	head := 4
	switch format {
	case 0:
		m.count = int(binary.BigEndian.Uint16(b[2:]))
	case 1:
		if len(b) < 6 {
			return nil, fmt.Errorf("%w: delta-set index map past its table", ErrInvalid)
		}
		m.count = intconv.Clamp(binary.BigEndian.Uint32(b[2:]))
		head = 6
	default:
		return nil, fmt.Errorf("%w: delta-set index map format %d", ErrInvalid, format)
	}
	// entryFormat packs the entry size less one in bits 4 and 5 and the
	// inner index's bit count less one in bits 0 to 3 (OpenType Font
	// Variations Common Table Formats, Associating target items to
	// variation data).
	m.entrySize = int(entryFormat&0x30)>>4 + 1
	m.innerBits = uint(entryFormat&0x0F) + 1
	if m.count > len(b) || head+m.count*m.entrySize > len(b) {
		return nil, fmt.Errorf("%w: %d delta-set index entries past the table", ErrInvalid, m.count)
	}
	m.data = b[head : head+m.count*m.entrySize]
	return m, nil
}

// lookup returns the outer and inner index of item i: the last entry for
// an i past the map, and (0, i) for a nil map (OpenType specification,
// 'HVAR' table).
func (m *deltaSetIndexMap) lookup(i int) (outer, inner int) {
	if m == nil {
		return 0, i
	}
	if m.count == 0 {
		return noVariationIdx, noVariationIdx
	}
	i = min(i, m.count-1)
	var e uint32
	for _, c := range m.data[i*m.entrySize : (i+1)*m.entrySize] {
		e = e<<8 | uint32(c)
	}
	outer = int(e >> m.innerBits)         // fits int: innerBits >= 1 leaves at most 31 bits
	inner = int(e & (1<<m.innerBits - 1)) // fits int: at most 16 bits
	return outer, inner
}
