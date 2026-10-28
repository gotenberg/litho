package icc

import (
	"encoding/binary"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/intconv"
)

// maxTags bounds the tag table of a profile. It is an anti-DoS guard, not
// spec-mandated: the count is a 32-bit field of the file, and real profiles
// carry a few dozen tags.
const maxTags = 1024

// Profile is a parsed ICC profile (ICC.1:2010, 7): the header fields a colour
// transform needs and the data of each tag.
type Profile struct {
	// Class is the profile/device class signature (ICC.1:2010, 7.2.5, Table
	// 18), such as "mntr", "prtr", "scnr" or "link".
	Class string
	// Space is the data colour space signature (Table 19), such as "RGB ",
	// "CMYK" or "GRAY", and PCS the profile connection space, "XYZ " or "Lab ".
	// A DeviceLink profile names its output colour space in PCS (7.2.7).
	Space, PCS string
	// Version is the major version, 2 or 4. [Parse] refuses 5 and later.
	Version byte
	tags    map[string][]byte
}

// iccMAXVersion is the first major version that is not ICC.1: version 5 is
// ICC.2 (iccMAX, ISO 20677-1), a different profile format whose processing
// elements and tag types an ICC.1 engine does not read.
const iccMAXVersion = 5

// Parse reads an ICC profile's header and tag table (ICC.1:2010, 7.2 and
// 7.3). The tag data is read only when a transform is built, and it aliases
// data, which the caller must not modify while the profile is in use.
//
// A profile of major version 5 or later fails with [ErrUnsupported]. ISO
// 32000-1, 8.6.5.5, Table 67 lists the ICC.1 editions an ICCBased space
// reads, up to version 4.2 (ICC.1:2004-10), and ISO 32000-2, 8.6.5.5 adds
// ICC.1:2010 (ISO 15076-1, version 4.3): an ICC.2 profile is none of them,
// so it is a profile the reader does not support, which Table 66 has fall
// back to another space, and an output intent's profile of that version
// characterises nothing litho can convert through. A version below 2, or 3,
// which no ICC.1 edition carries, is read as ICC.1, as LittleCMS reads it.
//
// The readers disagree on version 5, so the specification decides it:
// LittleCMS 2.16, which poppler converts through, refuses a version past
// 5.0.0 but reads 5.0.0 itself, qcms, which PDF.js converts through, checks
// no version, and veraPDF 1.30.2 fails a version 5 profile in an output
// intent or an ICCBased space at every PDF/A part (ISO 19005-2, 6.2.3 and
// 6.2.4.2).
//
// # Corpus evidence
//
//   - All corpora: 5 of the 3,916 ICC profile streams state version 5.0, in
//     litho iccbadcmyk_1b.pdf, iccbadgray_1b.pdf and iccbased_4.pdf and the
//     veraPDF 6-2-3-t01-fail-d pair; none is embedded in a JPEG 2000 image.
//   - litho iccbadcmyk_1b.pdf: DeviceCMYK read through the version 5 output
//     intent painted a neutral grey; read through the built-in profile, the
//     page matches poppler's.
func Parse(data []byte) (*Profile, error) {
	p, err := parse(data)
	if err != nil {
		return nil, err
	}
	if p.Version >= iccMAXVersion {
		return nil, fmt.Errorf("%w: version %d.%d profile, which is not ICC.1", ErrUnsupported, data[8], data[9]>>4)
	}
	return p, nil
}

// DataComponents returns the number of components of the profile data's
// colour space, as [Profile.Components] counts them, whatever the profile's
// version: which space a profile characterises does not depend on whether
// litho converts colours through it. It fails as [Parse] fails on a
// malformed profile.
func DataComponents(data []byte) (int, error) {
	p, err := parse(data)
	if err != nil {
		return 0, err
	}
	return p.Components(), nil
}

// parse is [Parse] without its version check.
func parse(data []byte) (*Profile, error) {
	if len(data) < 132 {
		return nil, fmt.Errorf("%w: %d bytes", ErrMalformed, len(data))
	}
	if string(data[36:40]) != "acsp" {
		return nil, fmt.Errorf("%w: no profile file signature", ErrMalformed)
	}
	p := &Profile{
		Class:   string(data[12:16]),
		Space:   string(data[16:20]),
		PCS:     string(data[20:24]),
		Version: data[8],
		tags:    make(map[string][]byte),
	}
	count := binary.BigEndian.Uint32(data[128:])
	if count > maxTags || 132+12*intconv.Clamp(count) > len(data) {
		return nil, fmt.Errorf("%w: tag table of %d entries", ErrMalformed, count)
	}
	for i := range intconv.Clamp(count) {
		e := data[132+12*i:]
		sig := string(e[:4])
		off, size := binary.BigEndian.Uint32(e[4:]), binary.BigEndian.Uint32(e[8:])
		if uint64(off)+uint64(size) > uint64(len(data)) || size < 8 {
			return nil, fmt.Errorf("%w: tag %q past the profile", ErrMalformed, sig)
		}
		p.tags[sig] = data[off : off+size]
	}
	return p, nil
}

// Components returns the number of colour components of the profile's data
// colour space, or 0 for a space this package does not transform.
func (p *Profile) Components() int {
	switch p.Space {
	case "GRAY":
		return 1
	case "RGB ":
		return 3
	case "CMYK":
		return 4
	}
	return 0
}

// ColorSpace returns the data colour space signature (ICC.1:2010, Table 19).
func (p *Profile) ColorSpace() string {
	return p.Space
}

// Channels returns the number of channels of the profile's data colour
// space, or 0 for a signature ICC.1:2010, Table 19 does not list.
func (p *Profile) Channels() int {
	return spaceChannels(p.Space)
}

// spaceChannels returns the channel count of a colour space signature
// (ICC.1:2010, Table 19). The n-colour spaces '2CLR' to 'FCLR' name their
// channel count, 2 to 15, in a hexadecimal digit.
func spaceChannels(sig string) int {
	switch sig {
	case "GRAY":
		return 1
	case "XYZ ", "Lab ", "Luv ", "YCbr", "Yxy ", "RGB ", "HSV ", "HLS ", "CMY ":
		return 3
	case "CMYK":
		return 4
	}
	if len(sig) == 4 && sig[1:] == "CLR" {
		switch c := sig[0]; {
		case c >= '2' && c <= '9':
			return int(c - '0')
		case c >= 'A' && c <= 'F':
			return int(c-'A') + 10
		}
	}
	return 0
}

// s15 reads an s15Fixed16Number (ICC.1:2010, 4.6): a two's complement
// 32-bit value in units of 1/65536.
func s15(b []byte) float64 {
	v := float64(binary.BigEndian.Uint32(b))
	if v >= 1<<31 {
		v -= 1 << 32
	}
	return v / 65536
}

// xyzTag reads the XYZ number an XYZType tag holds (10.31).
func (p *Profile) xyzTag(sig string) ([3]float64, bool) {
	b, ok := p.tags[sig]
	if !ok || len(b) < 20 || string(b[:4]) != "XYZ " {
		return [3]float64{}, false
	}
	return [3]float64{s15(b[8:]), s15(b[12:]), s15(b[16:])}, true
}
