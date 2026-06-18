package compliance

import "testing"

// FuzzParseJP2Boxes asserts the JP2 box walker never panics on crafted input.
func FuzzParseJP2Boxes(f *testing.F) {
	f.Add([]byte("\x00\x00\x00\x0cjP  \r\n\x87\n")) // JP2 signature box.
	f.Add([]byte("\x00\x00"))                       // Truncated box header.
	f.Fuzz(func(_ *testing.T, data []byte) {
		_ = parseJP2Boxes(data)
	})
}

// FuzzParseCOLR asserts the colr-box parser never panics on crafted input.
func FuzzParseCOLR(f *testing.F) {
	f.Add([]byte{0x01, 0x00, 0x01, 0x00, 0x00, 0x00, 0x10}) // METH 1, APPROX 1, enumCS.
	f.Add([]byte{0x01})                                     // Truncated.
	f.Fuzz(func(_ *testing.T, data []byte) {
		_, _ = parseCOLR(data)
	})
}

// FuzzParseJ2KSIZMarker asserts the raw J2K SIZ marker parser never panics.
func FuzzParseJ2KSIZMarker(f *testing.F) {
	siz := make([]byte, 45)
	siz[0], siz[1], siz[2], siz[3] = 0xFF, 0x4F, 0xFF, 0x51 // SOC + SIZ markers.
	siz[41] = 0x01                                          // Csiz = 1 component.
	f.Add(siz)
	f.Add([]byte{0xFF, 0x4F, 0xFF, 0x51}) // Truncated before Csiz.
	f.Fuzz(func(_ *testing.T, data []byte) {
		_, _, _ = parseJ2KSIZMarker(data)
	})
}
