package odp

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/png"
	"math"
	"testing"
)

// pngWith returns a 2 by 2 PNG with a pHYs chunk of ppm pixels per metre
// in unit, inserted after the header; none for ppm 0.
func pngWith(t testing.TB, ppm uint32, unit byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	if ppm == 0 {
		return data
	}
	chunk := make([]byte, 0, 21)
	chunk = binary.BigEndian.AppendUint32(chunk, 9)
	body := append([]byte("pHYs"), 0, 0, 0, 0, 0, 0, 0, 0, unit)
	binary.BigEndian.PutUint32(body[4:], ppm)
	binary.BigEndian.PutUint32(body[8:], ppm)
	chunk = append(chunk, body...)
	chunk = binary.BigEndian.AppendUint32(chunk, crc32.ChecksumIEEE(body))
	header := 8 + 25
	return append(append(append([]byte{}, data[:header]...), chunk...), data[header:]...)
}

func TestDensity(t *testing.T) {
	t.Parallel()
	jfif := func(unit byte, x, y uint16) []byte {
		seg := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 16, 'J', 'F', 'I', 'F', 0, 1, 2, unit, byte(x >> 8), byte(x), byte(y >> 8), byte(y), 0, 0, 0xFF, 0xDA}
		return seg
	}
	tests := []struct {
		name string
		data []byte
		x, y float64
		ok   bool
	}{
		{"png at 300 dpi", pngWith(t, 11811, 1), 300, 300, true},
		{"png without pHYs", pngWith(t, 0, 0), 0, 0, false},
		{"png of an unknown unit", pngWith(t, 11811, 0), 0, 0, false},
		{"jpeg in dots per inch", jfif(1, 150, 200), 150, 200, true},
		{"jpeg in dots per centimetre", jfif(2, 100, 100), 254, 254, true},
		{"jpeg of an aspect ratio alone", jfif(0, 1, 1), 0, 0, false},
		{"jpeg of an absurd density", jfif(1, 1, 1), 0, 0, false},
		{"truncated png", pngWith(t, 11811, 1)[:40], 0, 0, false},
		{"not a picture", []byte("GIF89a"), 0, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			x, y, ok := density(tt.data)
			if ok != tt.ok || math.Abs(x-tt.x) > 0.01 || math.Abs(y-tt.y) > 0.01 {
				t.Errorf("density = %v, %v, %v, want %v, %v, %v", x, y, ok, tt.x, tt.y, tt.ok)
			}
		})
	}
}

func FuzzDensity(f *testing.F) {
	f.Add(pngWith(f, 11811, 1))
	f.Add(pngWith(f, 11811, 1)[:40])
	f.Add([]byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 16, 'J', 'F', 'I', 'F', 0, 1, 2, 1, 0, 72, 0, 72, 0, 0})
	f.Fuzz(func(t *testing.T, data []byte) {
		x, y, ok := density(data)
		if ok && (x < minDensity || y < minDensity || x > maxDensity || y > maxDensity) {
			t.Fatalf("density %v, %v out of range", x, y)
		}
	})
}
