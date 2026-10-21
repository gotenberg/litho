package icc

import (
	"encoding/binary"
	"fmt"
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
	// Version is the major version, 2 or 4.
	Version byte
	tags    map[string][]byte
}

// Parse reads an ICC profile's header and tag table (ICC.1:2010, 7.2 and
// 7.3). The tag data is read only when a transform is built, and it aliases
// data, which the caller must not modify while the profile is in use.
func Parse(data []byte) (*Profile, error) {
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
	if count > maxTags || 132+12*int(count) > len(data) {
		return nil, fmt.Errorf("%w: tag table of %d entries", ErrMalformed, count)
	}
	for i := range int(count) {
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
