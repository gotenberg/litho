package imagefile

import (
	"bytes"
	"encoding/binary"
)

// Format is the format of an image file, read from its first bytes.
type Format uint8

// The formats [Sniff] tells.
const (
	// Unknown is a file of none of the formats below.
	Unknown Format = iota
	PNG
	JPEG
	GIF
	WebP
	BMP
	TIFF
	SVG
	JPEG2000
	ICO
)

// String names the format.
func (f Format) String() string {
	switch f {
	case PNG:
		return "PNG"
	case JPEG:
		return "JPEG"
	case GIF:
		return "GIF"
	case WebP:
		return "WebP"
	case BMP:
		return "BMP"
	case TIFF:
		return "TIFF"
	case SVG:
		return "SVG"
	case JPEG2000:
		return "JPEG 2000"
	case ICO:
		return "ICO"
	case Unknown:
	}
	return "unknown"
}

// svgHeadBytes is how far into a file an <svg root is looked for, past a
// byte order mark, white space, an XML declaration, comments and a
// document type declaration: a sniff, not a parse.
const svgHeadBytes = 4096

// jp2Signature is the JPEG 2000 Signature box every JP2 file starts with
// (ITU-T T.800, I.5.1).
var jp2Signature = []byte{0, 0, 0, 0x0C, 'j', 'P', ' ', ' ', 0x0D, 0x0A, 0x87, 0x0A}

// j2kSignature starts a bare JPEG 2000 codestream: its SOC marker, then
// the SIZ marker (ITU-T T.800, A.4.1 and A.5.1).
var j2kSignature = []byte{0xFF, 0x4F, 0xFF, 0x51}

// Sniff returns the format of the image file whose first bytes are data,
// by the signature each format's specification puts first: PNG (ISO/IEC
// 15948, 5.2), JPEG (ITU-T T.81, B.1.1.3: SOI then a marker), GIF
// ("GIF87a" or "GIF89a"), WebP (a RIFF file of form WEBP, RFC 9649, 2.4),
// BMP ("BM"), TIFF (TIFF 6.0, section 2: "II*\0" or "MM\0*"), JPEG 2000
// (the JP2 Signature box, ITU-T T.800, I.5.1, or a bare codestream's SOC
// and SIZ markers, A.4.1), ICO (a reserved 0, type 1
// and a count, the Windows icon directory) and SVG (an <svg root in the
// first bytes of an XML text). Anything else is [Unknown].
func Sniff(data []byte) Format {
	switch {
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		return PNG
	case len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF:
		return JPEG
	case bytes.HasPrefix(data, []byte("GIF87a")), bytes.HasPrefix(data, []byte("GIF89a")):
		return GIF
	case len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return WebP
	case bytes.HasPrefix(data, []byte("BM")) && len(data) >= 14:
		return BMP
	case bytes.HasPrefix(data, []byte("II*\x00")), bytes.HasPrefix(data, []byte("MM\x00*")):
		return TIFF
	case bytes.HasPrefix(data, jp2Signature), bytes.HasPrefix(data, j2kSignature):
		return JPEG2000
	case len(data) >= 6 && data[0] == 0 && data[1] == 0 && data[2] == 1 && data[3] == 0 && binary.LittleEndian.Uint16(data[4:]) > 0:
		return ICO
	case isSVG(data):
		return SVG
	}
	return Unknown
}

// isSVG reports a text whose first element, past a byte order mark, an XML
// declaration, comments and a document type declaration, is svg.
func isSVG(data []byte) bool {
	head := data[:min(len(data), svgHeadBytes)]
	head = bytes.TrimPrefix(head, []byte("\xef\xbb\xbf"))
	for {
		head = bytes.TrimLeft(head, " \t\r\n")
		switch {
		case bytes.HasPrefix(head, []byte("<?")):
			end := bytes.Index(head, []byte("?>"))
			if end < 0 {
				return false
			}
			head = head[end+2:]
		case bytes.HasPrefix(head, []byte("<!--")):
			end := bytes.Index(head, []byte("-->"))
			if end < 0 {
				return false
			}
			head = head[end+3:]
		case bytes.HasPrefix(head, []byte("<!")):
			end := bytes.IndexByte(head, '>')
			if end < 0 {
				return false
			}
			head = head[end+1:]
		default:
			if !bytes.HasPrefix(head, []byte("<svg")) || len(head) < 5 {
				return false
			}
			switch head[4] {
			case ' ', '\t', '\r', '\n', '>', '/':
				return true
			}
			return false
		}
	}
}

// Unsupported names what the file whose first bytes are data is when it is
// a picture format litho does not read but can tell by its signature:
// HEIF and AVIF (an ISO base media file whose ftyp box names a brand of
// ISO/IEC 23008-12 or of AV1 Image File Format), JPEG XL (ISO/IEC 18181-2,
// a bare codestream or the container), BigTIFF ("II+\0" or "MM\0+"), a
// cursor (the icon directory of type 2) and a gzip-compressed SVG. ""
// for anything else.
func Unsupported(data []byte) string {
	switch {
	case len(data) >= 12 && string(data[4:8]) == "ftyp":
		switch string(data[8:12]) {
		case "avif", "avis":
			return "AVIF"
		case "heic", "heix", "hevc", "hevx", "heim", "heis", "mif1", "msf1":
			return "HEIF"
		}
	case bytes.HasPrefix(data, []byte{0xFF, 0x0A}),
		bytes.HasPrefix(data, []byte{0, 0, 0, 0x0C, 'J', 'X', 'L', ' ', 0x0D, 0x0A, 0x87, 0x0A}):
		return "JPEG XL"
	case bytes.HasPrefix(data, []byte("II+\x00")), bytes.HasPrefix(data, []byte("MM\x00+")):
		return "BigTIFF"
	case len(data) >= 6 && data[0] == 0 && data[1] == 0 && data[2] == 2 && data[3] == 0:
		return "a cursor"
	case bytes.HasPrefix(data, []byte{0x1F, 0x8B}):
		return "gzip-compressed data"
	}
	return ""
}
