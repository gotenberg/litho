package dct

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"testing"
)

// reference decodes data with image/jpeg into the samples Decode returns:
// grey, RGB, or CMYK as stored, which is 255 minus what image/jpeg returns.
func reference(t *testing.T, data []byte) ([]byte, int, int, int) {
	t.Helper()
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("image/jpeg: %v", err)
	}
	b := img.Bounds()
	var out []byte
	n := 3
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			switch m := img.(type) {
			case *image.Gray:
				n = 1
				out = append(out, m.GrayAt(x, y).Y)
			case *image.CMYK:
				n = 4
				c := m.CMYKAt(x, y)
				out = append(out, 255-c.C, 255-c.M, 255-c.Y, 255-c.K)
			default:
				r, g, bb, _ := img.At(x, y).RGBA()
				out = append(out, byte(r>>8), byte(g>>8), byte(bb>>8))
			}
		}
	}
	return out, n, b.Dx(), b.Dy()
}

// near reports whether two sample runs differ by at most tol everywhere: the
// inverse DCT and colour conversion are each the decoder's own rounding.
func near(a, b []byte, tol int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if d := int(a[i]) - int(b[i]); d < -tol || d > tol {
			return false
		}
	}
	return true
}

// encoded returns a baseline JPEG of a gradient, grey or colour, from the
// standard library's encoder, which subsamples colour 4:2:0.
func encoded(t *testing.T, w, h int, grey bool) []byte {
	t.Helper()
	var img image.Image
	if grey {
		g := image.NewGray(image.Rect(0, 0, w, h))
		for y := range h {
			for x := range w {
				g.SetGray(x, y, color.Gray{Y: uint8((x*11 + y*5) % 256)})
			}
		}
		img = g
	} else {
		c := image.NewRGBA(image.Rect(0, 0, w, h))
		for y := range h {
			for x := range w {
				c.SetRGBA(x, y, color.RGBA{R: uint8(x * 6), G: uint8(y * 8), B: uint8((x + y) * 3), A: 255})
			}
		}
		img = c
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// segment returns the position of the first marker segment m.
func segment(t *testing.T, data []byte, m byte) int {
	t.Helper()
	for p := 2; p+3 < len(data); {
		if data[p] == 0xFF && data[p+1] == m {
			return p
		}
		p += 2 + int(binary.BigEndian.Uint16(data[p+2:]))
	}
	t.Fatalf("no marker 0x%02X", m)
	return 0
}

func decode(t *testing.T, data []byte, stated int) ([]byte, int, int, int) {
	t.Helper()
	out, n, w, h, err := Decode(context.Background(), data, -1, stated, DefaultMaxSamples)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	return out, n, w, h
}

// TestDecode_MatchesStandardLibrary decodes what image/jpeg decodes, to its
// rounding: baseline grey and 4:2:0 colour, progressive, restart intervals,
// and an Adobe CMYK file.
func TestDecode_MatchesStandardLibrary(t *testing.T) {
	t.Parallel()

	files := map[string][]byte{"grey": encoded(t, 23, 17, true), "colour": encoded(t, 37, 29, false)}
	for _, name := range []string{"progressive", "restart", "cmyk_adobe"} {
		data, err := os.ReadFile("testdata/" + name + ".jpg")
		if err != nil {
			t.Fatal(err)
		}
		files[name] = data
	}
	for name, data := range files {
		want, wn, ww, wh := reference(t, data)
		got, n, w, h := decode(t, data, 0)
		if n != wn || w != ww || h != wh || !near(got, want, 4) {
			t.Errorf("%s: %dx%dx%d, want %dx%dx%d within 4 levels", name, w, h, n, ww, wh, wn)
		}
	}
}

// TestDecode_Truncated keeps the rows a scan cut short still holds, where
// image/jpeg refuses the image.
func TestDecode_Truncated(t *testing.T) {
	t.Parallel()

	data := encoded(t, 64, 64, false)
	want, _, _, _ := reference(t, data)
	cut := data[:len(data)*2/3]
	if _, err := jpeg.Decode(bytes.NewReader(cut)); err == nil {
		t.Fatal("image/jpeg decoded the truncated data")
	}
	got, _, w, h := decode(t, cut, 0)
	if w != 64 || h != 64 {
		t.Fatalf("%dx%d, want 64x64", w, h)
	}
	// The first row of MCUs, 16 rows, arrived whole.
	if !near(got[:64*16*3], want[:64*16*3], 4) {
		t.Error("the rows before the cut differ from the whole image's")
	}
}

// TestDecode_RepeatedIdentifier matches the components of a frame that
// repeats an identifier by their position, where image/jpeg refuses it.
func TestDecode_RepeatedIdentifier(t *testing.T) {
	t.Parallel()

	data := encoded(t, 37, 29, false)
	want, _, _, _ := reference(t, data)
	bad := bytes.Clone(data)
	sof, sos := segment(t, bad, 0xC0), segment(t, bad, 0xDA)
	bad[sof+10+2*3] = bad[sof+10] // the third component takes the first's id
	bad[sos+5+2*2] = bad[sos+5]
	if _, err := jpeg.Decode(bytes.NewReader(bad)); err == nil {
		t.Fatal("image/jpeg decoded the repeated identifier")
	}
	if got, _, _, _ := decode(t, bad, 0); !near(got, want, 4) {
		t.Error("samples differ from the file with distinct identifiers")
	}
}

// TestDecode_Height takes a height of zero from a DNL marker, and a frame
// declaring more lines than the image states is decoded to the stated
// height.
func TestDecode_Height(t *testing.T) {
	t.Parallel()

	data := encoded(t, 23, 17, true)
	want, _, _, _ := reference(t, data)
	sof := segment(t, data, 0xC0)

	dnl := bytes.Clone(data[:len(data)-2])
	binary.BigEndian.PutUint16(dnl[sof+5:], 0)
	dnl = append(dnl, 0xFF, 0xDC, 0, 4, 0, 17, 0xFF, 0xD9)
	if got, _, _, h := decode(t, dnl, 0); h != 17 || !near(got, want, 4) {
		t.Errorf("DNL: height %d, want 17 with the same samples", h)
	}

	placeholder := bytes.Clone(data)
	binary.BigEndian.PutUint16(placeholder[sof+5:], 0xFFFF)
	if got, _, _, h := decode(t, placeholder, 17); h != 17 || !near(got, want, 4) {
		t.Errorf("placeholder: height %d, want 17 with the same samples", h)
	}
}

// TestDecode_Refuses names what it cannot decode with its sentinel.
func TestDecode_Refuses(t *testing.T) {
	t.Parallel()

	arithmetic := bytes.Clone(encoded(t, 8, 8, true))
	arithmetic[segment(t, arithmetic, 0xC0)+1] = 0xC9
	huge := bytes.Clone(encoded(t, 8, 8, true))
	sof := segment(t, huge, 0xC0)
	binary.BigEndian.PutUint16(huge[sof+5:], 0xFFFF)
	binary.BigEndian.PutUint16(huge[sof+7:], 0xFFFF)
	for name, tc := range map[string]struct {
		data []byte
		want error
	}{
		"not a JPEG":         {[]byte("GIF89a"), ErrMalformed},
		"arithmetic coding":  {arithmetic, ErrUnsupported},
		"65535 x 65535 grey": {huge, ErrLimit},
	} {
		if _, _, _, _, err := Decode(context.Background(), tc.data, -1, 0, DefaultMaxSamples); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", name, err, tc.want)
		}
	}
}

// TestDecode_CapScales pins that the cap Decode is given bounds the frame: a
// 23x17 grey JPEG decodes under a cap of exactly its samples and is refused
// under one sample less.
func TestDecode_CapScales(t *testing.T) {
	t.Parallel()

	data := encoded(t, 23, 17, true)
	if _, _, _, _, err := Decode(context.Background(), data, -1, 0, 23*17); err != nil {
		t.Fatalf("Decode under a cap of its samples: %v", err)
	}
	if _, _, _, _, err := Decode(context.Background(), data, -1, 0, 23*17-1); !errors.Is(err, ErrLimit) {
		t.Errorf("Decode under a cap one sample short: err = %v, want ErrLimit", err)
	}
}
