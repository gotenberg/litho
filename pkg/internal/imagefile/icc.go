package imagefile

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"

	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// iccHeaderBytes is the size of an ICC profile's header (ICC.1:2022, 7.2).
const iccHeaderBytes = 128

// profile is an ICC profile a file embeds, checked enough to be written as
// the stream of an ICCBased colour space (ISO 32000-1, 8.6.5.5).
type profile struct {
	data []byte
	// components is the count of the profile's data colour space: 1 for
	// GRAY, 3 for RGB, 4 for CMYK.
	components int
}

// readProfile returns the ICC profile data when its header reads as one
// that an ICCBased colour space takes: the size it states is its length,
// its signature is "acsp", its data colour space is GRAY, RGB or CMYK
// (ICC.1:2022, 7.2.2, 7.2.6 and 7.2.9; ISO 32000-1, 8.6.5.5 takes 1, 3 or
// 4 components), and its version is one the PDF version reads: a major
// version up to 4 from PDF 1.5, up to 2 before it (ISO 32000-1, Table
// 67). nil for anything else: the samples are then read in the device
// space of their count, and why lands in reason.
func readProfile(data []byte, pdf14 bool) (p *profile, reason string) {
	if len(data) == 0 {
		return nil, ""
	}
	if len(data) > typeset.MaxICCBytes {
		return nil, "the colour profile is larger than litho embeds"
	}
	if len(data) < iccHeaderBytes || int64(binary.BigEndian.Uint32(data)) != int64(len(data)) || !bytes.Equal(data[36:40], []byte("acsp")) {
		return nil, "the colour profile is malformed"
	}
	major := int(data[8])
	if major < 2 || major > 4 || pdf14 && major > 2 {
		return nil, "the colour profile's version is not one the PDF version reads"
	}
	var n int
	switch string(data[16:20]) {
	case "GRAY":
		n = 1
	case "RGB ":
		n = 3
	case "CMYK":
		n = 4
	default:
		return nil, "the colour profile is not of a gray, RGB or CMYK space"
	}
	return &profile{data: data, components: n}, ""
}

// forSpace returns the profile's bytes when it describes colours of n
// components, nil otherwise or for no profile.
func (p *profile) forSpace(n int) []byte {
	if p == nil || p.components != n {
		return nil
	}
	return p.data
}

// Profile returns the ICC profile the PNG or JPEG file data embeds, as
// the file stores it once inflated or joined: a PNG's iCCP chunk (PNG 3rd
// edition, 11.3.2.3), a JPEG's ICC_PROFILE APP2 segments (ICC.1:2022,
// Annex B.4); nil for none and for another format. Its size is bounded as
// [Read] bounds it.
func Profile(data []byte, f Format) []byte {
	switch f { //nolint:exhaustive // the other formats carry no profile this package reads
	case PNG:
		return readPNGMeta(data).icc
	case JPEG:
		return readJPEGMeta(data, len(data)).icc
	}
	return nil
}

// EncodeProfiled writes img losslessly as [Read] writes a decoded picture,
// its colours read through the ICC profile icc as an ICCBased colour space
// (ISO 32000-1, 8.6.5.5) when [Read] would keep it: a profile it refuses,
// one of a major version past 2 under pdf14 among them, or one of another
// colour space than img's, leaves the colours in the device space of their
// count, and reason says why. pdf14 also writes 16-bit samples at 8 bits.
// The caller owns img; EncodeProfiled does not release it.
func EncodeProfiled(ctx context.Context, img image.Image, icc []byte, pdf14 bool) (_ *typeset.ImageSource, reason string, err error) {
	p, reason := readProfile(icc, pdf14)
	src, err := encode(ctx, img, encodeOptions{no16: pdf14, icc: p})
	return src, reason, err
}
