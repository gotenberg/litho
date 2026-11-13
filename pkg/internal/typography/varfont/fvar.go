package varfont

import (
	"encoding/binary"
	"fmt"
	"math"
)

// maxAxes caps the axes of one font: 64. Anti-DoS guard, not
// spec-mandated ('fvar' counts axes in a uint16): every tuple of 'gvar'
// and every region of an item variation store costs a loop over the axes,
// and a font defines a handful of them, the registered five and a few of
// its own. A font past it is not instanced.
const maxAxes = 64

// fvar header and record sizes (OpenType specification, 'fvar' table): the
// 16-byte header, the 20-byte VariationAxisRecord, and the InstanceRecord
// without its coordinates (subfamilyNameID and flags) and with its
// optional postScriptNameID.
const (
	fvarHeaderSize     = 16
	fvarAxisRecordSize = 20
	instanceFixedSize  = 4
)

// noPostScriptName is the postScriptNameID of an instance record that names
// none (OpenType specification, 'fvar' table, InstanceRecord).
const noPostScriptName = 0xFFFF

// Axis is one variation axis of a font (OpenType specification, 'fvar'
// table, VariationAxisRecord), its values in user coordinates.
type Axis struct {
	// Tag is the axis tag, such as "wght".
	Tag string
	// Min, Default and Max bound the axis, Min <= Default <= Max.
	Min, Default, Max float64
	// fixed holds Min, Default and Max as the 16.16 values the font stores.
	fixed [3]int32
}

// NamedInstance is one named instance of a font (OpenType specification,
// 'fvar' table, InstanceRecord).
type NamedInstance struct {
	// SubfamilyNameID names the instance's style in the 'name' table.
	SubfamilyNameID uint16
	// PostScriptNameID names its PostScript name, 0xFFFF for none.
	PostScriptNameID uint16
	// Coords are its user coordinates, one per axis.
	Coords []float64
}

// Axes is the design space of a variable font: its axes and named
// instances, from its 'fvar' table.
type Axes struct {
	Axes      []Axis
	Instances []NamedInstance
}

// ParseFvar reads an 'fvar' table (OpenType specification, 'fvar' table).
// It fails on a table whose version is not 1, whose records do not fit,
// whose axis records state min > default or default > max, or which
// defines more than [maxAxes] axes or none.
func ParseFvar(data []byte) (*Axes, error) {
	if len(data) < fvarHeaderSize {
		return nil, fmt.Errorf("%w: fvar header of %d bytes", ErrInvalid, len(data))
	}
	if major := binary.BigEndian.Uint16(data); major != 1 {
		return nil, fmt.Errorf("%w: fvar version %d", ErrInvalid, major)
	}
	axesAt := int(binary.BigEndian.Uint16(data[4:]))
	axisCount := int(binary.BigEndian.Uint16(data[8:]))
	axisSize := int(binary.BigEndian.Uint16(data[10:]))
	instanceCount := int(binary.BigEndian.Uint16(data[12:]))
	instanceSize := int(binary.BigEndian.Uint16(data[14:]))
	if axisCount == 0 || axisCount > maxAxes {
		return nil, fmt.Errorf("%w: %d axes", ErrInvalid, axisCount)
	}
	if axisSize < fvarAxisRecordSize {
		return nil, fmt.Errorf("%w: fvar axis records of %d bytes", ErrInvalid, axisSize)
	}
	// An instance record holds the subfamily name ID, its flags and one
	// Fixed coordinate per axis, then an optional PostScript name ID
	// (OpenType specification, 'fvar' table, InstanceRecord).
	coordsSize := 4 * axisCount
	if instanceCount > 0 && instanceSize < instanceFixedSize+coordsSize {
		return nil, fmt.Errorf("%w: fvar instance records of %d bytes for %d axes", ErrInvalid, instanceSize, axisCount)
	}
	instancesAt := axesAt + axisCount*axisSize
	if axesAt < fvarHeaderSize || instancesAt+instanceCount*instanceSize > len(data) {
		return nil, fmt.Errorf("%w: fvar records past the table", ErrInvalid)
	}
	a := &Axes{Axes: make([]Axis, axisCount)}
	for i := range a.Axes {
		r := data[axesAt+i*axisSize:]
		ax := Axis{Tag: string(r[:4])}
		for j := range ax.fixed {
			ax.fixed[j] = int32(binary.BigEndian.Uint32(r[4+4*j:])) //nolint:gosec // reinterprets the 32 bits of a signed Fixed
		}
		if ax.fixed[0] > ax.fixed[1] || ax.fixed[1] > ax.fixed[2] {
			return nil, fmt.Errorf("%w: axis %q from %d through %d to %d", ErrInvalid, ax.Tag, ax.fixed[0], ax.fixed[1], ax.fixed[2])
		}
		ax.Min, ax.Default, ax.Max = fixedToFloat(ax.fixed[0]), fixedToFloat(ax.fixed[1]), fixedToFloat(ax.fixed[2])
		a.Axes[i] = ax
	}
	if instanceCount > 0 {
		a.Instances = make([]NamedInstance, instanceCount)
	}
	for i := range a.Instances {
		r := data[instancesAt+i*instanceSize:]
		in := NamedInstance{
			SubfamilyNameID:  binary.BigEndian.Uint16(r),
			PostScriptNameID: noPostScriptName,
			Coords:           make([]float64, axisCount),
		}
		for j := range in.Coords {
			in.Coords[j] = fixedToFloat(int32(binary.BigEndian.Uint32(r[instanceFixedSize+4*j:]))) //nolint:gosec // reinterprets the 32 bits of a signed Fixed
		}
		if instanceSize >= instanceFixedSize+coordsSize+2 {
			in.PostScriptNameID = binary.BigEndian.Uint16(r[instanceFixedSize+coordsSize:])
		}
		a.Instances[i] = in
	}
	return a, nil
}

// Index returns the index of the axis tagged tag, -1 when the font has
// none.
func (a *Axes) Index(tag string) int {
	for i, ax := range a.Axes {
		if ax.Tag == tag {
			return i
		}
	}
	return -1
}

// Clamp returns v clamped to the range of axis i (CSS Fonts 4, 8.2: a
// value out of an axis's range is clamped to the closest value the font
// supports).
func (a *Axes) Clamp(i int, v float64) float64 {
	ax := a.Axes[i]
	if math.IsNaN(v) {
		return ax.Default
	}
	return min(max(v, ax.Min), ax.Max)
}

// Defaults returns the user coordinates of the default instance.
func (a *Axes) Defaults() []float64 {
	out := make([]float64, len(a.Axes))
	for i, ax := range a.Axes {
		out[i] = ax.Default
	}
	return out
}

// fixedToFloat converts a 16.16 value to a float.
func fixedToFloat(v int32) float64 { return float64(v) / 65536 }

// floatToFixed converts v to 16.16 as the OpenType Font Variations
// Overview, Coordinate scales and normalization, prescribes: the fraction
// times 65536 rounded half up. v is clamped to the 16.16 range first.
func floatToFixed(v float64) int32 {
	f := math.Floor(float64(v*65536) + 0.5)
	return int32(min(max(f, math.MinInt32), math.MaxInt32))
}
