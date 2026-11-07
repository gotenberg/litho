package html

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/draw"
)

// maxEXIFScan caps the bytes of a JPEG [jpegOrientation] walks to find
// its EXIF segment: 256 KiB. Anti-DoS guard, not spec-mandated: the APP1
// segment sits right after the SOI marker in every camera's output (EXIF
// 2.32, 4.5.4), after at most an APP0 and an ICC profile, so the scan
// never needs to cross the picture's scans.
const maxEXIFScan = 256 << 10

// tagOrientation is the EXIF orientation tag of IFD0 (EXIF 2.32, 4.6.4,
// Table 4, tag 274).
const tagOrientation = 0x0112

// jpegOrientation returns the EXIF orientation of the JPEG data, 1 to 8,
// 1 when it carries none or a malformed one. CSS Images 3, 5.3: Chromium
// applies it by default, image-orientation: from-image. The value means,
// for the stored rows and columns (EXIF 2.32, 4.6.4, Orientation):
// 1 as stored, 2 mirrored left to right, 3 turned a half, 4 mirrored top
// to bottom, 5 transposed, 6 turned a quarter clockwise, 7 transversed,
// 8 turned a quarter counter-clockwise.
func jpegOrientation(data []byte) int {
	data = data[:min(len(data), maxEXIFScan)]
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 1
	}
	i := 2
	for i+4 <= len(data) {
		if data[i] != 0xFF {
			return 1
		}
		marker := data[i+1]
		if marker == 0xFF {
			// A fill byte before a marker (ITU T.81, B.1.1.2).
			i++
			continue
		}
		if marker == 0xDA || marker == 0xD9 {
			// Start of scan or end of image: no EXIF before them.
			return 1
		}
		n := int(binary.BigEndian.Uint16(data[i+2:]))
		if n < 2 || i+2+n > len(data) {
			return 1
		}
		seg := data[i+4 : i+2+n]
		if marker == 0xE1 && bytes.HasPrefix(seg, []byte("Exif\x00\x00")) {
			return tiffOrientation(seg[6:])
		}
		i += 2 + n
	}
	return 1
}

// tiffOrientation reads the orientation tag of IFD0 of the TIFF structure
// an EXIF segment holds (EXIF 2.32, 4.5.2; TIFF 6.0, section 2).
func tiffOrientation(t []byte) int {
	if len(t) < 8 {
		return 1
	}
	var bo binary.ByteOrder
	switch string(t[:4]) {
	case "II*\x00":
		bo = binary.LittleEndian
	case "MM\x00*":
		bo = binary.BigEndian
	default:
		return 1
	}
	off := int64(bo.Uint32(t[4:]))
	if off < 8 || off+2 > int64(len(t)) {
		return 1
	}
	count := int64(bo.Uint16(t[off:]))
	for e := range count {
		at := off + 2 + 12*e
		if at+12 > int64(len(t)) {
			return 1
		}
		entry := t[at : at+12]
		if bo.Uint16(entry) != tagOrientation {
			continue
		}
		// A SHORT (type 3) of count 1, held in the entry's value field.
		if bo.Uint16(entry[2:]) != 3 || bo.Uint32(entry[4:]) != 1 {
			return 1
		}
		if v := int(bo.Uint16(entry[8:])); v >= 1 && v <= 8 {
			return v
		}
		return 1
	}
	return 1
}

// orientImage returns img with the EXIF orientation o applied, img itself
// for 1 or a value out of range. The pixels are copied into a grey image
// for a grey source and an RGBA one otherwise.
func orientImage(img image.Image, o int) image.Image {
	if o < 2 || o > 8 {
		return img
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	dw, dh := w, h
	if o >= 5 {
		dw, dh = h, w
	}
	var dst draw.Image
	if _, ok := img.(*image.Gray); ok {
		dst = image.NewGray(image.Rect(0, 0, dw, dh))
	} else {
		dst = image.NewRGBA(image.Rect(0, 0, dw, dh))
	}
	for y := range h {
		for x := range w {
			var dx, dy int
			switch o {
			case 2:
				dx, dy = w-1-x, y
			case 3:
				dx, dy = w-1-x, h-1-y
			case 4:
				dx, dy = x, h-1-y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = h-1-y, x
			case 7:
				dx, dy = h-1-y, w-1-x
			default: // 8
				dx, dy = y, w-1-x
			}
			dst.Set(dx, dy, img.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

// unsupportedFormat names the format of a picture Chromium draws and no
// decoder registered with image.Decode reads, by its signature or its
// response's media type: AVIF (ISO/IEC 23008-12 ftyp brands avif and
// avis), JPEG XL (ISO/IEC 18181-2, a bare codestream or the container),
// ICO and CUR (the Windows icon directory). "" for any other: what no
// decoder reads then fails as a malformed image.
func unsupportedFormat(data []byte, mediaType string) string {
	switch {
	case len(data) >= 12 && string(data[4:8]) == "ftyp" && (string(data[8:12]) == "avif" || string(data[8:12]) == "avis"),
		mediaType == "image/avif":
		return "AVIF"
	case bytes.HasPrefix(data, []byte{0xFF, 0x0A}),
		bytes.HasPrefix(data, []byte{0, 0, 0, 0x0C, 'J', 'X', 'L', ' ', 0x0D, 0x0A, 0x87, 0x0A}),
		mediaType == "image/jxl":
		return "JPEG XL"
	case len(data) >= 6 && data[0] == 0 && data[1] == 0 && (data[2] == 1 || data[2] == 2) && data[3] == 0 && (data[4] != 0 || data[5] != 0),
		mediaType == "image/x-icon", mediaType == "image/vnd.microsoft.icon":
		return "ICO"
	}
	return ""
}
