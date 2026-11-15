package imagefile

import (
	"bytes"
	"context"
	"encoding/binary"
	"image/color"
	"image/jpeg"

	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// maxEXIFScan caps the bytes of a JPEG [JPEGOrientation] walks to find
// its EXIF segment: 256 KiB. Anti-DoS guard, not spec-mandated: the APP1
// segment sits right after the SOI marker in every camera's output (EXIF
// 2.32, 4.5.4), after at most an APP0 and an ICC profile, so the scan
// never needs to cross the picture's scans.
const maxEXIFScan = 256 << 10

// maxICCChunks caps the APP2 segments a JPEG's ICC profile is split into:
// the sequence number is a byte (ICC.1:2022, B.4), so 255 at most.
const maxICCChunks = 255

// jpegMeta is what the segments of a JPEG before its first scan state.
type jpegMeta struct {
	exif EXIF
	// jfifDPI is the density of a JFIF APP0 segment in pixels an inch,
	// 0, 0 for none or for one whose unit is 0, an aspect ratio alone
	// (JFIF 1.02, ITU-T T.871, 10.1).
	jfifX, jfifY float64
	// icc is the profile the APP2 segments carry, reassembled in their
	// sequence order (ICC.1:2022, B.4), nil for none or for chunks that
	// do not make one profile.
	icc []byte
	// adobe marks an Adobe APP14 segment (Adobe Technical Note 5116,
	// 18): the writer of a CMYK or YCCK frame stores its samples
	// inverted.
	adobe bool
}

// readJPEGMeta walks the marker segments of the JPEG data up to its first
// scan (ITU-T T.81, B.1.1), reading at most limit bytes of it. A segment
// that does not lie in the data ends the walk with what was read.
func readJPEGMeta(data []byte, limit int) jpegMeta {
	m := jpegMeta{exif: EXIF{Orientation: 1}}
	data = data[:min(len(data), limit)]
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return m
	}
	var chunks [][]byte
	total := 0
	exifSeen := false
	for i := 2; i+4 <= len(data); {
		if data[i] != 0xFF {
			break
		}
		marker := data[i+1]
		if marker == 0xFF {
			// A fill byte before a marker (ITU T.81, B.1.1.2).
			i++
			continue
		}
		if marker == 0xDA || marker == 0xD9 {
			// Start of scan or end of image.
			break
		}
		n := int(binary.BigEndian.Uint16(data[i+2:]))
		if n < 2 || i+2+n > len(data) {
			break
		}
		seg := data[i+4 : i+2+n]
		switch {
		case marker == 0xE0 && bytes.HasPrefix(seg, []byte("JFIF\x00")) && len(seg) >= 12:
			// Version (2 bytes), units, Xdensity, Ydensity.
			unit := seg[7]
			x, y := float64(binary.BigEndian.Uint16(seg[8:])), float64(binary.BigEndian.Uint16(seg[10:]))
			switch unit {
			case 1:
				m.jfifX, m.jfifY = Resolution(x, y)
			case 2:
				m.jfifX, m.jfifY = Resolution(x*2.54, y*2.54)
			}
		case marker == 0xE1 && !exifSeen && bytes.HasPrefix(seg, []byte("Exif\x00\x00")):
			exifSeen = true
			m.exif = ReadEXIF(seg[6:])
		case marker == 0xE2 && bytes.HasPrefix(seg, []byte("ICC_PROFILE\x00")) && len(seg) >= 14:
			if total+len(seg)-14 <= typeset.MaxICCBytes && len(chunks) < maxICCChunks {
				chunks = append(chunks, seg[12:])
				total += len(seg) - 14
			} else {
				chunks, total = nil, typeset.MaxICCBytes+1
			}
		case marker == 0xEE && bytes.HasPrefix(seg, []byte("Adobe")):
			m.adobe = true
		}
		i += 2 + n
	}
	if total <= typeset.MaxICCBytes {
		m.icc = joinICC(chunks)
	}
	return m
}

// joinICC reassembles the profile of APP2 chunks, each a sequence number
// from 1, the chunk count and the data (ICC.1:2022, B.4), nil when they
// are not numbered 1 to their count each once.
func joinICC(chunks [][]byte) []byte {
	if len(chunks) == 0 {
		return nil
	}
	ordered := make([][]byte, len(chunks))
	for _, c := range chunks {
		seq, count := int(c[0]), int(c[1])
		if count != len(chunks) || seq < 1 || seq > count || ordered[seq-1] != nil {
			return nil
		}
		ordered[seq-1] = c[2:]
	}
	return bytes.Join(ordered, nil)
}

// JPEGOrientation returns the EXIF orientation of the JPEG data, 1 to 8,
// 1 when it carries none or a malformed one, from its first 256 KiB. CSS
// Images 3, 5.3: Chromium applies it by default, image-orientation:
// from-image.
func JPEGOrientation(data []byte) int {
	return readJPEGMeta(data, maxEXIFScan).exif.Orientation
}

// PNGOrientation returns the orientation of the EXIF data of the PNG
// data's eXIf chunk, 1 to 8, 1 when it carries none or a malformed one
// (PNG 3rd edition, 11.3.3.7: the chunk holds the TIFF structure of an
// EXIF segment, and comes before the first IDAT), from its first 256 KiB.
// CSS Images 3, 5.3: Chromium applies it as it applies a JPEG's.
//
// # Corpus evidence
//
//   - probe, html: Chromium prints a 40 by 20 PNG whose eXIf chunk holds
//     orientation 6 turned a quarter clockwise, 20 by 40, as it prints
//     the same JPEG.
func PNGOrientation(data []byte) int {
	data = data[:min(len(data), maxEXIFScan)]
	if !bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")) {
		return 1
	}
	for i := 8; i+8 <= len(data); {
		n := int64(binary.BigEndian.Uint32(data[i:]))
		typ := string(data[i+4 : i+8])
		end := int64(i) + 12 + n
		if typ == "IDAT" || end > int64(len(data)) {
			return 1
		}
		if typ == "eXIf" {
			return ReadEXIF(data[i+8 : i+8+int(n)]).Orientation // fits int: i+8+n < end, within len(data)
		}
		i = int(end) // fits int: at most len(data)
	}
	return 1
}

// readJPEGFile reads a JPEG file, which is written as it is: its frame
// header alone is read (ITU-T T.81, B.2.2), never its scans, whatever its
// process, baseline, extended or progressive. Its colours are read in
// the ICC profile its APP2 segments carry when that profile describes
// them, else as the device space of its component count: gray, YCbCr or
// RGB read as RGB, CMYK; an Adobe CMYK file's inverted samples are
// inverted back (Adobe Technical Note 5116, 18). Its resolution is its
// JFIF density when that states a unit, else its EXIF resolution.
func readJPEGFile(ctx context.Context, data []byte, o *Options, fn func(*Picture) error) (Info, error) {
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return Info{}, malformed("JPEG header", err)
	}
	if err := checkSize(ctx, cfg.Width, cfg.Height); err != nil {
		return Info{}, err
	}
	m := readJPEGMeta(data, len(data))
	n := 3
	switch cfg.ColorModel {
	case color.GrayModel:
		n = 1
	case color.CMYKModel:
		n = 4
	}
	icc, reason := readProfile(m.icc, o.PDF14)
	o.warn(reason)
	l := typeset.ImageLayout{Components: n, ICC: icc.forSpace(n), Invert: n == 4 && m.adobe}
	if icc != nil && l.ICC == nil {
		o.warn("the colour profile does not describe the JPEG's components")
	}
	src, err := typeset.JPEGLayout(ctx, data, l)
	if err != nil {
		return Info{}, malformed("JPEG", err)
	}
	pic := &Picture{Source: src, Width: cfg.Width, Height: cfg.Height, XDPI: m.jfifX, YDPI: m.jfifY, Orientation: m.exif.Orientation}
	if pic.XDPI == 0 {
		pic.XDPI, pic.YDPI = m.exif.XDPI, m.exif.YDPI
	}
	return Info{Title: m.exif.Title}, fn(pic)
}
