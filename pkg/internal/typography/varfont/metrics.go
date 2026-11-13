package varfont

import (
	"encoding/binary"
	"fmt"
)

// Header sizes of the metrics variation tables (OpenType specification,
// 'HVAR', 'VVAR' and 'MVAR' tables).
const (
	hvarHeaderSize = 20
	vvarHeaderSize = 24
	mvarHeaderSize = 12
	mvarRecordSize = 8
)

// advanceVariations are the advance deltas of an 'HVAR' or 'VVAR' table at
// one instance.
type advanceVariations struct {
	store   *itemStore
	advance *deltaSetIndexMap
}

// parseAdvanceVariations reads an 'HVAR' table, or a 'VVAR' one when
// vertical, at the normalized coordinates coords (OpenType specification,
// 'HVAR' and 'VVAR' tables). Only the advance deltas are read: an
// instance's side bearings come from its outlines.
func parseAdvanceVariations(data []byte, coords []int16, vertical bool) (*advanceVariations, error) {
	size := hvarHeaderSize
	if vertical {
		size = vvarHeaderSize
	}
	if len(data) < size || binary.BigEndian.Uint16(data) != 1 {
		return nil, fmt.Errorf("%w: metrics variations header", ErrInvalid)
	}
	storeAt := binary.BigEndian.Uint32(data[4:])
	if uint64(storeAt) >= uint64(len(data)) {
		return nil, fmt.Errorf("%w: item variation store past the table", ErrInvalid)
	}
	store, err := parseItemStore(data[storeAt:], coords)
	if err != nil {
		return nil, err
	}
	v := &advanceVariations{store: store}
	if at := binary.BigEndian.Uint32(data[8:]); at != 0 {
		if v.advance, err = parseDeltaSetIndexMap(data, at); err != nil {
			return nil, err
		}
	}
	return v, nil
}

// delta returns the unrounded advance delta of glyph gid.
func (v *advanceVariations) delta(gid int) float64 {
	outer, inner := v.advance.lookup(gid)
	return v.store.delta(outer, inner)
}

// metricsVariations are the deltas of an 'MVAR' table at one instance, by
// value tag.
type metricsVariations map[string]float64

// parseMVAR reads an 'MVAR' table at the normalized coordinates coords
// (OpenType specification, 'MVAR' table) and returns the unrounded delta
// of each value tag it lists.
func parseMVAR(data []byte, coords []int16) (metricsVariations, error) {
	if len(data) < mvarHeaderSize || binary.BigEndian.Uint16(data) != 1 {
		return nil, fmt.Errorf("%w: MVAR header", ErrInvalid)
	}
	recSize := int(binary.BigEndian.Uint16(data[6:]))
	count := int(binary.BigEndian.Uint16(data[8:]))
	storeAt := int(binary.BigEndian.Uint16(data[10:]))
	if count == 0 {
		return nil, nil
	}
	if recSize < mvarRecordSize || mvarHeaderSize+count*recSize > len(data) || storeAt == 0 || storeAt >= len(data) {
		return nil, fmt.Errorf("%w: MVAR records past the table", ErrInvalid)
	}
	store, err := parseItemStore(data[storeAt:], coords)
	if err != nil {
		return nil, err
	}
	m := make(metricsVariations, count)
	for i := range count {
		r := data[mvarHeaderSize+i*recSize:]
		m[string(r[:4])] = store.delta(int(binary.BigEndian.Uint16(r[4:])), int(binary.BigEndian.Uint16(r[6:])))
	}
	return m, nil
}
