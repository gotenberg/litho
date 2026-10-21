package icc

import (
	"encoding/binary"
	"math"
	"sort"
)

// buildProfile assembles an ICC profile: a 128-byte header naming the data
// colour space, connection space and version, then the tag table and the
// tags, each padded to four bytes.
func buildProfile(space, pcs string, version byte, tags map[string][]byte) []byte {
	class := "mntr"
	if space == "CMYK" {
		class = "prtr"
	}
	return buildClassProfile(class, space, pcs, version, tags)
}

// buildClassProfile is buildProfile with the device class named.
func buildClassProfile(class, space, pcs string, version byte, tags map[string][]byte) []byte {
	sigs := make([]string, 0, len(tags))
	for sig := range tags {
		sigs = append(sigs, sig)
	}
	sort.Strings(sigs)
	header := make([]byte, 128)
	header[8] = version
	copy(header[12:], class)
	copy(header[16:], space)
	copy(header[20:], pcs)
	copy(header[36:], "acsp")
	put := func(b []byte, v float64) { binary.BigEndian.PutUint32(b, uint32(int32(math.Round(v*65536)))) }
	put(header[68:], 0.9642)
	put(header[72:], 1)
	put(header[76:], 0.8249)
	table := binary.BigEndian.AppendUint32(nil, uint32(len(sigs)))
	offset := 128 + 4 + 12*len(sigs)
	var body []byte
	// Tags of the same data share it, as ICC.1:2010, 7.3.1 allows.
	placed := make(map[string]int)
	for _, sig := range sigs {
		data := tags[sig]
		table = append(table, sig...)
		if at, ok := placed[string(data)]; ok {
			table = binary.BigEndian.AppendUint32(table, uint32(at))
			table = binary.BigEndian.AppendUint32(table, uint32(len(data)))
			continue
		}
		placed[string(data)] = offset + len(body)
		table = binary.BigEndian.AppendUint32(table, uint32(offset+len(body)))
		table = binary.BigEndian.AppendUint32(table, uint32(len(data)))
		body = append(body, data...)
		for len(body)%4 != 0 {
			body = append(body, 0)
		}
	}
	out := append(append(header, table...), body...)
	binary.BigEndian.PutUint32(out, uint32(len(out)))
	return out
}

// s15Bytes encodes s15Fixed16Numbers.
func s15Bytes(vs ...float64) []byte {
	var b []byte
	for _, v := range vs {
		b = binary.BigEndian.AppendUint32(b, uint32(int32(math.Round(v*65536))))
	}
	return b
}

// xyzType builds an XYZType tag.
func xyzType(x, y, z float64) []byte {
	return append([]byte("XYZ \x00\x00\x00\x00"), s15Bytes(x, y, z)...)
}

// gammaCurve builds a curveType of one exponent.
func gammaCurve(g float64) []byte {
	b := []byte("curv\x00\x00\x00\x00\x00\x00\x00\x01")
	return binary.BigEndian.AppendUint16(b, uint16(math.Round(g*256)))
}

// tableCurve builds a curveType of sampled 16-bit values.
func tableCurve(vs ...uint16) []byte {
	b := binary.BigEndian.AppendUint32([]byte("curv\x00\x00\x00\x00"), uint32(len(vs)))
	for _, v := range vs {
		b = binary.BigEndian.AppendUint16(b, v)
	}
	return b
}

// paraCurve builds a parametricCurveType.
func paraCurve(kind uint16, params ...float64) []byte {
	b := binary.BigEndian.AppendUint16([]byte("para\x00\x00\x00\x00"), kind)
	return append(append(b, 0, 0), s15Bytes(params...)...)
}

// rgbProfile is a matrix and tone curve RGB profile: sRGB's colorants adapted
// to D50, a 2.2 gamma on red, a sampled curve on green and a parametric sRGB
// curve on blue.
func rgbProfile() []byte {
	return buildProfile("RGB ", "XYZ ", 2, map[string][]byte{
		"rXYZ": xyzType(0.4361, 0.2225, 0.0139),
		"gXYZ": xyzType(0.3851, 0.7169, 0.0971),
		"bXYZ": xyzType(0.1431, 0.0606, 0.7141),
		"rTRC": gammaCurve(2.2),
		"gTRC": tableCurve(0, 4000, 16000, 40000, 65535),
		"bTRC": paraCurve(3, 2.4, 1/1.055, 0.055/1.055, 1/12.92, 0.04045),
		"wtpt": xyzType(0.9642, 1, 0.8249),
	})
}

// greyProfile is a grey profile whose tone curve is a parametric type 1.
func greyProfile() []byte {
	return buildProfile("GRAY", "XYZ ", 4, map[string][]byte{
		"kTRC": paraCurve(1, 1.8, 0.9, 0.1),
		"wtpt": xyzType(0.9642, 1, 0.8249),
	})
}

// cmykProfile is a CMYK profile with a lut16Type AToB1 table into Lab: a
// three-point grid whose Lab values fall with ink, cyan pulling a* and b*
// down, magenta a* up, yellow b* up.
func cmykProfile() []byte {
	b := []byte("mft2\x00\x00\x00\x00")
	b = append(b, 4, 3, 3, 0)
	b = append(b, s15Bytes(1, 0, 0, 0, 1, 0, 0, 0, 1)...)
	b = binary.BigEndian.AppendUint16(b, 2)
	b = binary.BigEndian.AppendUint16(b, 2)
	for range 4 {
		b = binary.BigEndian.AppendUint16(b, 0)
		b = binary.BigEndian.AppendUint16(b, 65535)
	}
	for c := range 3 {
		for m := range 3 {
			for y := range 3 {
				for k := range 3 {
					ink := float64(c+m+y) / 6
					l := 100 * (1 - 0.8*ink) * (1 - 0.45*float64(k))
					a := -40*float64(c)/2 + 60*float64(m)/2
					bb := -45*float64(c)/2 + 70*float64(y)/2
					// Legacy 16-bit Lab: L* 100 is 0xFF00, a* and b* 0 is 0x8000.
					b = binary.BigEndian.AppendUint16(b, uint16(math.Round(l/100*65280)))
					b = binary.BigEndian.AppendUint16(b, uint16(math.Round((a+128)*256)))
					b = binary.BigEndian.AppendUint16(b, uint16(math.Round((bb+128)*256)))
				}
			}
		}
	}
	for range 3 {
		b = binary.BigEndian.AppendUint16(b, 0)
		b = binary.BigEndian.AppendUint16(b, 65535)
	}
	return buildProfile("CMYK", "Lab ", 2, map[string][]byte{
		"A2B1": b,
		"wtpt": xyzType(0.9642, 1, 0.8249),
	})
}

// extendedProfile is a version 4 RGB profile whose AToB1 table is M curves, a
// matrix with negative offsets and identity B curves into XYZ, the shape of
// an extended-range encoding: its intermediate values leave 0 to 1.
func extendedProfile() []byte {
	b := []byte("mAB \x00\x00\x00\x00")
	b = append(b, 3, 3, 0, 0)
	offB := 32
	identity := []byte("curv\x00\x00\x00\x00\x00\x00\x00\x00")
	bCurves := append(append(append([]byte(nil), identity...), identity...), identity...)
	offMatrix := offB + len(bCurves)
	matrix := s15Bytes(0.4754, 0.4198, 0.156, 0.2425, 0.7816, 0.0661, 0.0152, 0.1058, 0.7783, -0.2449, -0.254, -0.2095)
	offM := offMatrix + len(matrix)
	m := gammaCurve(1.8)
	m = append(m, 0, 0)
	mCurves := append(append(append([]byte(nil), m...), m...), m...)
	for _, v := range []int{offB, offMatrix, offM, 0, 0} {
		b = binary.BigEndian.AppendUint32(b, uint32(v))
	}
	b = append(append(append(b, bCurves...), matrix...), mCurves...)
	return buildProfile("RGB ", "XYZ ", 4, map[string][]byte{
		"A2B1": b,
		"wtpt": xyzType(0.9642, 1, 0.8249),
	})
}
