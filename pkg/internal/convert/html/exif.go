package html

import (
	"bytes"
)

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
