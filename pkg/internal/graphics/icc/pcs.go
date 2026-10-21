package icc

import "math"

// xyzToLab converts XYZ relative to D50 to CIELAB (ICC.1:2010, 6.3.4.2 and
// Annex A, with the PCS illuminant of 7.2.16 as the reference white).
func xyzToLab(xyz [3]float64) (l, a, b float64) {
	f := func(t float64) float64 {
		const e = 6.0 / 29
		if t > e*e*e {
			return math.Cbrt(t)
		}
		return t/(3*e*e) + 4.0/29
	}
	fx, fy, fz := f(xyz[0]/d50[0]), f(xyz[1]/d50[1]), f(xyz[2]/d50[2])
	return 116*fy - 16, 500 * (fx - fy), 200 * (fy - fz)
}

// fromXYZ writes XYZ relative to D50 in the table's connection space input
// encoding, normalized to 0 to 1, the inverse of [pipeline.pcs].
func (p *pipeline) fromXYZ(xyz [3]float64, v *[maxOutputs]float64) {
	if p.encoding == pcsXYZ {
		// u1Fixed15Number: 1.0 is 0x8000 (ICC.1:2010, 6.3.4.3).
		s := 32768.0 / 65535
		v[0], v[1], v[2] = xyz[0]*s, xyz[1]*s, xyz[2]*s
		return
	}
	l, a, b := xyzToLab(xyz)
	labToEncoding(l, a, b, p.encoding, v)
}

// labToEncoding writes CIELAB in a connection space encoding, normalized to 0
// to 1: L* 100 is 0xFF00 in the legacy 16-bit encoding of lut16Type
// (ICC.1:2010, 10.8) and 0xFFFF in the version 4 encoding and the 8-bit one
// (6.3.4.2), a* and b* 0 is 0x8000 and 0x80 alike.
func labToEncoding(l, a, b float64, encoding int, v *[maxOutputs]float64) {
	if encoding == pcsLab16 {
		v[0], v[1], v[2] = l/100*65280/65535, (a+128)*256/65535, (b+128)*256/65535
		return
	}
	v[0], v[1], v[2] = l/100, (a+128)/255, (b+128)/255
}

// labFromV4 reads CIELAB in the version 4 encoding normalized to 0 to 1, the
// encoding a [Link] takes and gives Lab data in.
func labFromV4(v []float64) (l, a, b float64) {
	return v[0] * 100, v[1]*255 - 128, v[2]*255 - 128
}

// v4ToLegacy rescales Lab data from the version 4 encoding to the legacy
// 16-bit encoding a lut16Type table over Lab data takes (ICC.1:2010, 10.8),
// and legacyToV4 back.
func v4ToLegacy(v *[maxOutputs]float64) {
	v[0] *= 65280.0 / 65535
	v[1] *= 65280.0 / 65535
	v[2] *= 65280.0 / 65535
}

func legacyToV4(v *[maxOutputs]float64) {
	v[0] *= 65535.0 / 65280
	v[1] *= 65535.0 / 65280
	v[2] *= 65535.0 / 65280
}
