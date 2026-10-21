package truetype

import (
	"encoding/binary"
	"fmt"
)

// hheaNumberOfHMetrics is the offset of numberOfHMetrics in the hhea table,
// and hheaSize the length of the version 1.0 table (OpenType specification,
// hhea table).
const (
	hheaNumberOfHMetrics = 34
	hheaSize             = 36
)

// HMetrics is the horizontal metrics of an sfnt program, its hmtx table read
// through hhea's numberOfHMetrics. A CFF2 program states its advance widths
// here alone (OpenType specification, CFF2 table, 'hmtx' and HVAR tables).
//
// An HMetrics is immutable and safe for concurrent use.
type HMetrics struct {
	hmtx []byte
	n    int
}

// ReadHMetrics reads the hhea and hmtx tables of data. It fails on a program
// that carries neither, or an hhea table too short to state numberOfHMetrics.
func ReadHMetrics(data []byte) (HMetrics, error) {
	records, err := ParseTableDirectory(data)
	if err != nil {
		return HMetrics{}, err
	}
	hhea, err := TableData(data, records, "hhea")
	if err != nil {
		return HMetrics{}, err
	}
	if len(hhea) < hheaSize {
		return HMetrics{}, fmt.Errorf("%w: hhea table too short", ErrInvalidFont)
	}
	hmtx, err := TableData(data, records, "hmtx")
	if err != nil {
		return HMetrics{}, err
	}
	// A shorter hmtx holds fewer long metrics than hhea states.
	n := min(int(binary.BigEndian.Uint16(hhea[hheaNumberOfHMetrics:])), len(hmtx)/4)
	return HMetrics{hmtx: hmtx, n: n}, nil
}

// Advance returns the advance width of glyph gid in font units: a glyph past
// numberOfHMetrics takes the last long metric's advance (OpenType
// specification, hmtx table). Metrics that hold no long metric give 0.
func (m HMetrics) Advance(gid int) float64 {
	if m.n == 0 || gid < 0 {
		return 0
	}
	i := min(gid, m.n-1)
	return float64(binary.BigEndian.Uint16(m.hmtx[4*i:]))
}
