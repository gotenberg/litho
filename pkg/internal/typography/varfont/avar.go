package varfont

import (
	"encoding/binary"
	"fmt"
)

// avarHeaderSize is the size of an 'avar' version 1 header: majorVersion,
// minorVersion, a reserved field and axisCount (OpenType specification,
// 'avar' table).
const avarHeaderSize = 8

// one is 1.0 in 16.16.
const one = 1 << 16

// axisValueMap is one AxisValueMap record, its coordinates in 16.16.
type axisValueMap struct {
	from, to int32
}

// Avar is a font's 'avar' table: one segment map per axis, nil for an axis
// whose map is the identity.
type Avar struct {
	maps [][]axisValueMap
}

// ParseAvar reads an 'avar' table of version 1 for a font of axisCount
// axes (OpenType specification, 'avar' table). It fails with
// [ErrUnsupported] on version 2, whose variation store remaps coordinates
// in ways version 1 has no form for, and with [ErrInvalid] on a table that
// does not hold one segment map per axis, or whose map lists its
// fromCoordinate values out of increasing order.
func ParseAvar(data []byte, axisCount int) (*Avar, error) {
	if len(data) < avarHeaderSize {
		return nil, fmt.Errorf("%w: avar header of %d bytes", ErrInvalid, len(data))
	}
	switch major := binary.BigEndian.Uint16(data); major {
	case 1:
	case 2:
		return nil, fmt.Errorf("%w: avar version 2", ErrUnsupported)
	default:
		return nil, fmt.Errorf("%w: avar version %d", ErrInvalid, major)
	}
	if n := int(binary.BigEndian.Uint16(data[6:])); n != axisCount {
		return nil, fmt.Errorf("%w: avar maps %d axes of %d", ErrInvalid, n, axisCount)
	}
	a := &Avar{maps: make([][]axisValueMap, axisCount)}
	at := avarHeaderSize
	for i := range axisCount {
		if at+2 > len(data) {
			return nil, fmt.Errorf("%w: avar segment map %d past the table", ErrInvalid, i)
		}
		n := int(binary.BigEndian.Uint16(data[at:]))
		at += 2
		if at+4*n > len(data) {
			return nil, fmt.Errorf("%w: avar segment map %d past the table", ErrInvalid, i)
		}
		m := make([]axisValueMap, n)
		for j := range m {
			m[j] = axisValueMap{from: f2dot14At(data, at+4*j) << 2, to: f2dot14At(data, at+4*j+2) << 2}
			if j > 0 && m[j].from <= m[j-1].from {
				return nil, fmt.Errorf("%w: avar segment map %d out of order", ErrInvalid, i)
			}
		}
		at += 4 * n
		if n > 0 {
			a.maps[i] = m
		}
	}
	return a, nil
}

// f2dot14At reads the F2DOT14 at data[at:] as an integer of 1/16384 units.
func f2dot14At(data []byte, at int) int32 {
	return int32(int16(binary.BigEndian.Uint16(data[at:]))) //nolint:gosec // reinterprets the 16 bits of a signed F2DOT14
}

// Normalize returns the normalized coordinates, in F2DOT14 units of
// 1/16384, of the user coordinates user, one per axis of axes, through avar
// when it is not nil (OpenType Font Variations Overview, Coordinate scales
// and normalization). The arithmetic is the specification's: each value is
// converted to 16.16, clamped to its axis, normalized against the axis's
// default and clamped to [-1, 1] in 16.16, mapped through the axis's
// segment map, clamped again, and converted to 2.14 by adding 2 and
// shifting right by 2. A missing user value is the axis's default.
func Normalize(axes *Axes, avar *Avar, user []float64) []int16 {
	out := make([]int16, len(axes.Axes))
	for i, ax := range axes.Axes {
		v := ax.fixed[1]
		if i < len(user) {
			v = floatToFixed(user[i])
		}
		n := defaultNormalized(v, ax.fixed)
		if avar != nil && i < len(avar.maps) && avar.maps[i] != nil {
			n = clampUnit(segmentMap(n, avar.maps[i]))
		}
		out[i] = int16((n + 2) >> 2) //nolint:gosec // n lies in [-65536, 65536], so the shift lies in [-16384, 16384]
	}
	return out
}

// defaultNormalized maps v, 16.16, to [-1, 1] in 16.16 against the axis
// values lim, its minimum, default and maximum (OpenType Font Variations
// Overview, Coordinate scales and normalization), rounding the quotient to
// the nearest 16.16 value.
func defaultNormalized(v int32, lim [3]int32) int32 {
	v = min(max(v, lim[0]), lim[2])
	switch {
	case v < lim[1]:
		return clampUnit(divRound(int64(v-lim[1])*one, int64(lim[1]-lim[0])))
	case v > lim[1]:
		return clampUnit(divRound(int64(v-lim[1])*one, int64(lim[2]-lim[1])))
	}
	return 0
}

// segmentMap maps n, 16.16, through the segment map m (OpenType
// specification, 'avar' table): the value of the segment whose ends hold
// n, interpolated linearly, or n shifted as the nearest end shifts when no
// segment holds it.
func segmentMap(n int32, m []axisValueMap) int32 {
	if n <= m[0].from {
		return n + m[0].to - m[0].from
	}
	last := m[len(m)-1]
	if n >= last.from {
		return n + last.to - last.from
	}
	for j := 1; j < len(m); j++ {
		end := m[j]
		if end.from < n {
			continue
		}
		if end.from == n {
			return end.to
		}
		start := m[j-1]
		return start.to + int32(divRound(int64(n-start.from)*int64(end.to-start.to), int64(end.from-start.from))) //nolint:gosec // the quotient lies between start.to and end.to
	}
	return n
}

// clampUnit clamps v, 16.16, to [-1, 1].
func clampUnit[T int32 | int64](v T) int32 {
	return int32(min(max(v, -one), one)) //nolint:gosec // clamped to [-65536, 65536] first
}

// divRound returns a/b rounded to the nearest integer, halves away from
// zero; b is not 0.
func divRound(a, b int64) int64 {
	if b < 0 {
		a, b = -a, -b
	}
	if a >= 0 {
		return (a + b/2) / b
	}
	return -((-a + b/2) / b)
}
