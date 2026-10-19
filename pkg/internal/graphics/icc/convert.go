package icc

import (
	"encoding/binary"
	"fmt"
	"math"
	"slices"
	"unicode/utf16"
)

// v2Version is the header version ToV2 writes: 2.4.0, the version of
// ICC.1:2001-04, the last edition of the version 2 specification. Its tag
// types are the ones v2TagTypes lists.
const v2Version = 0x02400000

// curveSampleCounts are the sample counts ToV2 tries, in order, for the
// curveType that replaces a parametric curve. It keeps the first whose linear
// interpolation (ICC.1:2001-04, 6.5.3) gives every 16-bit input within
// [curveTolerance] of the parametric value. The last count samples every
// 16-bit input, so it always qualifies: a curve then differs from its
// parametric form only by the rounding of each entry to 16 bits, the
// precision of every curveType.
//
//nolint:gochecknoglobals // static table.
var curveSampleCounts = []int{1024, 4096, 1 << 16}

// curveTolerance is the largest difference ToV2 admits between a parametric
// curve and its sampled form, at any 16-bit input: one 16-bit code value, the
// step of the curveType that holds it.
const curveTolerance = 1.0 / 65535

// v2TagTypes are the tag types ICC.1:2001-04, 6.5 defines, which a version 2
// colour engine reads. ToV2 copies a tag of one of these types as it is.
//
// profileSequenceDescType ('pseq') is left out: the version 4 type nests
// multiLocalizedUnicodeType descriptions where the version 2 one nests
// textDescriptionType, so the same signature carries another layout.
//
//nolint:gochecknoglobals // static spec table.
var v2TagTypes = map[string]bool{
	"chrm": true, "crdi": true, "curv": true, "data": true, "devs": true,
	"dtim": true, "mft1": true, "mft2": true, "meas": true, "ncl2": true,
	"rcs2": true, "sf32": true, "scrn": true, "sig ": true, "desc": true,
	"text": true, "uf32": true, "bfd ": true, "ui16": true, "ui32": true,
	"ui64": true, "ui08": true, "view": true, "XYZ ": true,
}

// v2DescriptionTags are the tags ICC.1:2001-04, 6.4 types as
// textDescriptionType, which version 4 types as multiLocalizedUnicodeType
// (ICC.1:2010, 9.2): profileDescription, deviceMfgDesc, deviceModelDesc and
// viewingCondDesc. copyright is a textType in version 2 (6.4.13).
//
//nolint:gochecknoglobals // static spec table.
var v2DescriptionTags = map[string]bool{"desc": true, "dmnd": true, "dmdd": true, "vued": true}

// ToV2 re-encodes a version 4 profile as a version 2.4 profile with the same
// colour transforms, for a format that admits only version 2, such as the
// output intent of PDF/A-1 (ISO 19005-1, 6.2.2, which takes the profile of
// PDF Reference 1.4, 9.10.4, an ICC.1:1998-09 profile).
//
// The tags a colour engine reads keep their values: a tag of a type version 2
// defines is copied, and a parametric curve (ICC.1:2010, 10.18), which version
// 2 lacks, becomes a curveType sampled at every 16-bit input. Text tags change
// type alone: a multiLocalizedUnicodeType becomes the textDescriptionType or
// textType version 2 gives that tag, keeping its English record, or else its
// first. A display profile whose media white point is the PCS illuminant, as
// version 4 requires of one (ICC.1:2010, 9.2.36), gets back the display's own
// white, the illuminant mapped back through its chromatic adaptation
// (9.2.15), which is what version 2 records there (ICC.1:2001-04, 6.4.25).
// The profile ID, a version 4 header field, is cleared: version 2 reserves
// those bytes (ICC.1:2001-04, 6.1).
//
// A tag of any other version 4 type, a lutAtoBType table among them, would
// have to be resampled into another type, so ToV2 refuses the profile with
// [ErrUnsupported] rather than write one that converts colours differently.
// A version 2 profile comes back as it is.
func ToV2(data []byte) ([]byte, error) {
	p, err := Parse(data)
	if err != nil {
		return nil, err
	}
	switch p.Version {
	case 2:
		return slices.Clone(data), nil
	case 4:
	default:
		return nil, fmt.Errorf("%w: version %d profile", ErrUnsupported, p.Version)
	}

	sigs := tagOrder(data)
	tags := make(map[string][]byte, len(sigs))
	for _, sig := range sigs {
		b := p.tags[sig]
		typ := string(b[:4])
		switch {
		case typ == "mluc" && v2DescriptionTags[sig]:
			text, err := mlucText(b)
			if err != nil {
				return nil, err
			}
			tags[sig] = textDescription(text)
		case typ == "mluc" && sig == "cprt":
			text, err := mlucText(b)
			if err != nil {
				return nil, err
			}
			tags[sig] = textType(text)
		case typ == "para":
			c, _, err := readCurve(b)
			if err != nil {
				return nil, err
			}
			tags[sig] = sampledCurve(c)
		case v2TagTypes[typ]:
			tags[sig] = b
		default:
			return nil, fmt.Errorf("%w: tag %q of version 4 type %q", ErrUnsupported, sig, typ)
		}
	}

	if string(data[12:16]) == "mntr" {
		if wtpt, ok := displayWhite(p); ok {
			tags["wtpt"] = xyzTag(wtpt)
		}
	}

	header := slices.Clone(data[:128])
	binary.BigEndian.PutUint32(header[8:], v2Version)
	clear(header[84:100])
	return assemble(header, sigs, tags), nil
}

// tagOrder returns the tag signatures in the order of the profile's tag
// table, so the converted profile lists them as the source does. A signature
// the table repeats is kept once, as [Parse] keeps one.
func tagOrder(data []byte) []string {
	count := int(binary.BigEndian.Uint32(data[128:]))
	sigs := make([]string, 0, count)
	for i := range count {
		sig := string(data[132+12*i : 136+12*i])
		if !slices.Contains(sigs, sig) {
			sigs = append(sigs, sig)
		}
	}
	return sigs
}

// assemble writes header, a tag table of sigs and the tags, each padded to
// four bytes (ICC.1:2001-04, 6.3), and sets the profile size field. Tags of
// identical data share one copy, as the tag table admits (6.3).
func assemble(header []byte, sigs []string, tags map[string][]byte) []byte {
	table := binary.BigEndian.AppendUint32(nil, uint32(len(sigs))) //nolint:gosec // bounded by maxTags.
	base := 128 + 4 + 12*len(sigs)
	var body []byte
	written := make(map[string]int, len(sigs))
	for _, sig := range sigs {
		tag := tags[sig]
		at, ok := written[string(tag)]
		if !ok {
			at = base + len(body)
			written[string(tag)] = at
			body = append(body, tag...)
			for len(body)%4 != 0 {
				body = append(body, 0)
			}
		}
		table = append(table, sig...)
		table = binary.BigEndian.AppendUint32(table, uint32(at))       //nolint:gosec // a sum of tag sizes bounded by the source length and the converted curves.
		table = binary.BigEndian.AppendUint32(table, uint32(len(tag))) //nolint:gosec // a slice of the source or a converted tag of at most 128 KiB.
	}
	out := append(append(header, table...), body...)
	binary.BigEndian.PutUint32(out, uint32(len(out))) //nolint:gosec // the source length plus the converted curves.
	return out
}

// mlucText returns the English record of a multiLocalizedUnicodeType
// (ICC.1:2010, 10.15), or its first record when it holds no English one.
func mlucText(b []byte) (string, error) {
	if len(b) < 16 {
		return "", fmt.Errorf("%w: multiLocalizedUnicode of %d bytes", ErrMalformed, len(b))
	}
	count := binary.BigEndian.Uint32(b[8:])
	size := binary.BigEndian.Uint32(b[12:])
	if size < 12 || uint64(count)*uint64(size)+16 > uint64(len(b)) {
		return "", fmt.Errorf("%w: multiLocalizedUnicode of %d records", ErrMalformed, count)
	}
	if count == 0 {
		return "", nil
	}
	pick := 0
	for i := range int(count) {
		if string(b[16+i*int(size):][:2]) == "en" {
			pick = i
			break
		}
	}
	rec := b[16+pick*int(size):]
	length, offset := binary.BigEndian.Uint32(rec[4:]), binary.BigEndian.Uint32(rec[8:])
	if uint64(offset)+uint64(length) > uint64(len(b)) || length%2 != 0 {
		return "", fmt.Errorf("%w: multiLocalizedUnicode record past its tag", ErrMalformed)
	}
	units := make([]uint16, length/2)
	for i := range units {
		units[i] = binary.BigEndian.Uint16(b[int(offset)+2*i:])
	}
	return string(utf16.Decode(units)), nil
}

// asciiText returns s with every character outside 7-bit ASCII replaced by a
// question mark, the invariant description ICC.1:2001-04, 6.5.17 and 6.5.18
// require.
func asciiText(s string) []byte {
	out := make([]byte, 0, len(s))
	for _, r := range s {
		if r == 0 {
			break
		}
		if r > 0x7E || r < 0x20 {
			r = '?'
		}
		out = append(out, byte(r))
	}
	return out
}

// textDescription builds a textDescriptionType (ICC.1:2001-04, 6.5.17): the
// ASCII description, the same text as Unicode, and an empty ScriptCode
// description of the fixed 67-byte field.
func textDescription(s string) []byte {
	ascii := asciiText(s)
	b := []byte("desc\x00\x00\x00\x00")
	b = binary.BigEndian.AppendUint32(b, uint32(len(ascii)+1)) //nolint:gosec // a text tag of a parsed profile.
	b = append(append(b, ascii...), 0)
	units := utf16.Encode([]rune(s))
	b = binary.BigEndian.AppendUint32(b, 0)
	b = binary.BigEndian.AppendUint32(b, uint32(len(units)+1)) //nolint:gosec // a text tag of a parsed profile.
	for _, u := range units {
		b = binary.BigEndian.AppendUint16(b, u)
	}
	b = append(b, 0, 0)
	b = append(b, 0, 0, 0)
	return append(b, make([]byte, 67)...)
}

// textType builds a textType (ICC.1:2001-04, 6.5.18): 7-bit ASCII ending in a
// NUL.
func textType(s string) []byte {
	return append(append([]byte("text\x00\x00\x00\x00"), asciiText(s)...), 0)
}

// xyzTag builds an XYZType of one value (ICC.1:2001-04, 6.5.26).
func xyzTag(v [3]float64) []byte {
	b := []byte("XYZ \x00\x00\x00\x00")
	for _, c := range v {
		b = binary.BigEndian.AppendUint32(b, uint32(int32(math.Round(c*65536)))) //nolint:gosec // an s15Fixed16Number, two's complement by definition.
	}
	return b
}

// sampledCurve builds the curveType (ICC.1:2001-04, 6.5.3) of the fewest
// [curveSampleCounts] samples of c, each rounded to 16 bits, that stays within
// [curveTolerance] of c at every 16-bit input.
func sampledCurve(c curve) []byte {
	exact := make([]float64, 1<<16)
	for i := range exact {
		exact[i] = c.eval(float64(i) / 65535)
	}
	var s sampled
	for _, n := range curveSampleCounts {
		s = make(sampled, n)
		for i := range s {
			s[i] = math.Round(clamp01(c.eval(float64(i)/float64(n-1)))*65535) / 65535
		}
		if curveWithin(s, exact) {
			break
		}
	}
	b := binary.BigEndian.AppendUint32([]byte("curv\x00\x00\x00\x00"), uint32(len(s))) //nolint:gosec // a count of curveSampleCounts.
	for _, v := range s {
		b = binary.BigEndian.AppendUint16(b, uint16(math.Round(v*65535)))
	}
	return b
}

// curveWithin reports whether s gives every 16-bit input within
// [curveTolerance] of exact, the curve's value at that input.
func curveWithin(s sampled, exact []float64) bool {
	for i, want := range exact {
		if math.Abs(s.eval(float64(i)/65535)-want) > curveTolerance {
			return false
		}
	}
	return true
}

// displayWhite returns the white of a version 4 display profile whose media
// white point is the PCS illuminant: the illuminant through the inverse of
// the profile's chromatic adaptation matrix (ICC.1:2010, 9.2.15 and Annex
// E.3). It reports false when the profile records another white point, or no
// invertible adaptation, so that the white point it records stays.
func displayWhite(p *Profile) ([3]float64, bool) {
	wtpt, ok := p.xyzTag("wtpt")
	if !ok || !nearD50(wtpt) {
		return [3]float64{}, false
	}
	b, ok := p.tags["chad"]
	if !ok || len(b) < 44 || string(b[:4]) != "sf32" {
		return [3]float64{}, false
	}
	var m [9]float64
	for i := range m {
		m[i] = s15(b[8+4*i:])
	}
	inv, ok := invert3(m)
	if !ok {
		return [3]float64{}, false
	}
	return [3]float64{
		inv[0]*d50[0] + inv[1]*d50[1] + inv[2]*d50[2],
		inv[3]*d50[0] + inv[4]*d50[1] + inv[5]*d50[2],
		inv[6]*d50[0] + inv[7]*d50[1] + inv[8]*d50[2],
	}, true
}

// nearD50 reports whether v is the PCS illuminant to within the rounding of
// an s15Fixed16Number, which writers apply differently (0.9642 is written as
// 0xF6D5 by some and 0xF6D6 by others).
func nearD50(v [3]float64) bool {
	const tolerance = 2.0 / 65536
	for i := range v {
		if math.Abs(v[i]-d50[i]) > tolerance {
			return false
		}
	}
	return true
}

// invert3 inverts a 3x3 matrix in row-major order, reporting false when it
// is singular.
func invert3(m [9]float64) ([9]float64, bool) {
	det := m[0]*(m[4]*m[8]-m[5]*m[7]) - m[1]*(m[3]*m[8]-m[5]*m[6]) + m[2]*(m[3]*m[7]-m[4]*m[6])
	if math.Abs(det) < 1e-12 {
		return [9]float64{}, false
	}
	return [9]float64{
		(m[4]*m[8] - m[5]*m[7]) / det, (m[2]*m[7] - m[1]*m[8]) / det, (m[1]*m[5] - m[2]*m[4]) / det,
		(m[5]*m[6] - m[3]*m[8]) / det, (m[0]*m[8] - m[2]*m[6]) / det, (m[2]*m[3] - m[0]*m[5]) / det,
		(m[3]*m[7] - m[4]*m[6]) / det, (m[1]*m[6] - m[0]*m[7]) / det, (m[0]*m[4] - m[1]*m[3]) / det,
	}, true
}
