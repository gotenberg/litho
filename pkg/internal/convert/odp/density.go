package odp

import "encoding/binary"

// maxDensityScan bounds the bytes [density] reads past a picture's
// signature looking for its resolution: 64 KiB. Anti-DoS guard, not
// spec-mandated: a PNG's pHYs comes before its image data and a JPEG's
// JFIF segment first, both within a few hundred bytes of the start.
const maxDensityScan = 64 << 10

// The resolutions density accepts, in pixels per inch: a stated
// resolution outside them is a writer's mistake, and the picture takes
// the default resolution.
const (
	minDensity = 10.0
	maxDensity = 10_000.0
)

// density returns the resolution a PNG or JPEG picture states, in pixels
// per inch across and down, false for none: a PNG's pHYs chunk in pixels
// per metre (PNG 1.2, 4.2.4.2, unit specifier 1), a JPEG's JFIF APP0
// density in dots per inch or per centimetre (JFIF 1.02, units 1 and
// 2). Impress sizes a picture by it, which places a crop given in
// lengths (ODF 1.3, Part 3, 20.172 fo:clip); a picture without one
// takes 96 pixels per inch.
func density(data []byte) (float64, float64, bool) {
	if len(data) > maxDensityScan {
		data = data[:maxDensityScan]
	}
	ok := func(x, y float64) (float64, float64, bool) {
		if x < minDensity || y < minDensity || x > maxDensity || y > maxDensity {
			return 0, 0, false
		}
		return x, y, true
	}
	const pngSig = "\x89PNG\r\n\x1a\n"
	if len(data) >= 8 && string(data[:8]) == pngSig {
		for i := 8; i+8 <= len(data); {
			n := binary.BigEndian.Uint32(data[i:])
			typ := string(data[i+4 : i+8])
			if n > maxDensityScan {
				return 0, 0, false
			}
			end := i + 8 + int(n) // fits int: n is at most maxDensityScan, checked above
			if end+4 > len(data) {
				return 0, 0, false
			}
			switch typ {
			case "pHYs":
				if n != 9 || data[i+16] != 1 {
					return 0, 0, false
				}
				x := float64(binary.BigEndian.Uint32(data[i+8:])) * 0.0254
				y := float64(binary.BigEndian.Uint32(data[i+12:])) * 0.0254
				return ok(x, y)
			case "IDAT", "IEND":
				return 0, 0, false
			}
			i = end + 4
		}
		return 0, 0, false
	}
	if len(data) >= 4 && data[0] == 0xFF && data[1] == 0xD8 {
		for i := 2; i+4 <= len(data) && data[i] == 0xFF; {
			marker := data[i+1]
			n := int(binary.BigEndian.Uint16(data[i+2:]))
			if marker == 0xDA || n < 2 || i+2+n > len(data) {
				return 0, 0, false
			}
			seg := data[i+4 : i+2+n]
			if marker == 0xE0 && len(seg) >= 12 && string(seg[:5]) == "JFIF\x00" {
				x, y := float64(binary.BigEndian.Uint16(seg[8:])), float64(binary.BigEndian.Uint16(seg[10:]))
				switch seg[7] {
				case 1:
					return ok(x, y)
				case 2:
					return ok(x*2.54, y*2.54)
				}
				return 0, 0, false
			}
			i += 2 + n
		}
	}
	return 0, 0, false
}
