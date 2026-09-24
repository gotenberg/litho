package filter

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"os"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/filter/jpx"
)

// annexJ10Samples are the samples of testdata/annex_j10.j2k, the codestream of
// ITU-T T.800, J.10: one 1x9 component of 8-bit samples (J.10.5).
var annexJ10Samples = []byte{101, 103, 104, 105, 96, 97, 96, 102, 109}

// readJPX reads a JPEG 2000 test file.
func readJPX(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// box builds a JP2 box (T.800, I.4).
func box(kind string, content ...[]byte) []byte {
	body := slices.Concat(content...)
	b := binary.BigEndian.AppendUint32(nil, uint32(8+len(body)))
	return append(append(b, kind...), body...)
}

// palettedJ10 wraps the J.10 codestream in a JP2 file that maps its single
// component through a 256-entry sRGB palette, entry j being (255 - j, j, j/2),
// with any extra header boxes given. No column maps an index to itself, so a
// palette lookup never passes for the index.
func palettedJ10(t *testing.T, extra ...[]byte) []byte {
	t.Helper()
	pclr := []byte{1, 0, 3, 7, 7, 7}
	for j := range 256 {
		pclr = append(pclr, byte(255-j), byte(j), byte(j/2))
	}
	header := box("jp2h",
		box("ihdr", []byte{0, 0, 0, 9, 0, 0, 0, 1, 0, 1, 7, 7, 0, 0}),
		box("colr", []byte{1, 0, 0, 0, 0, 0, 16}),
		box("pclr", pclr),
		box("cmap", []byte{0, 0, 1, 0, 0, 0, 1, 1, 0, 0, 1, 2}),
		slices.Concat(extra...),
	)
	return slices.Concat(box("jP  ", []byte{0x0D, 0x0A, 0x87, 0x0A}), box("ftyp", []byte("jp2 \x00\x00\x00\x00jp2 ")), header,
		box("jp2c", readJPX(t, "testdata/annex_j10.j2k")))
}

// TestDecodeJPX_Samples decodes the J.10 codestream to its 8-bit samples, one
// byte per pixel.
func TestDecodeJPX_Samples(t *testing.T) {
	t.Parallel()

	got, err := decodeJPX(context.Background(), readJPX(t, "testdata/annex_j10.j2k"), nil, jpx.DefaultMaxSamples)
	if err != nil || !bytes.Equal(got, annexJ10Samples) {
		t.Errorf("decodeJPX = %v, %v; want %v", got, err, annexJ10Samples)
	}
}

// TestDecodeJPX_Palette reads a palettized JP2 file two ways. Without a
// /ColorSpace the JPEG 2000 colour specification governs, so each sample is
// looked up in the palette and yields three colour channels. Under an /Indexed
// /ColorSpace the samples are indices into the PDF's own colour table and the
// JPEG 2000 palette is ignored (ISO 32000-1, 7.4.9).
func TestDecodeJPX_Palette(t *testing.T) {
	t.Parallel()

	data := palettedJ10(t)
	got, err := decodeJPX(context.Background(), data, nil, jpx.DefaultMaxSamples)
	if err != nil {
		t.Fatalf("decodeJPX: %v", err)
	}
	var want []byte
	for _, v := range annexJ10Samples {
		want = append(want, 255-v, v, v/2)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("without /ColorSpace: %v, want %v", got, want)
	}
	indexed := ast.PdfDict{"ColorSpace": ast.PdfArray{ast.PdfName("Indexed"), ast.PdfName("DeviceRGB"), ast.PdfInt(255), ast.PdfString("")}}
	got, err = decodeJPX(context.Background(), data, indexed, jpx.DefaultMaxSamples)
	if err != nil || !bytes.Equal(got, annexJ10Samples) {
		t.Errorf("under /Indexed: %v, %v; want %v", got, err, annexJ10Samples)
	}
}

// TestDecodeJPX_ColourOrder hands the colour channels on in the order of the
// colours they carry (T.800, I.5.3.6), whatever their order in the file: here
// the Channel Definition box gives the palette's columns as blue, green and
// red, so the samples come out as (j/2, j, 255 - j).
func TestDecodeJPX_ColourOrder(t *testing.T) {
	t.Parallel()

	cdef := box("cdef", []byte{0, 3, 0, 0, 0, 0, 0, 3, 0, 1, 0, 0, 0, 2, 0, 2, 0, 0, 0, 1})
	got, err := decodeJPX(context.Background(), palettedJ10(t, cdef), nil, jpx.DefaultMaxSamples)
	if err != nil {
		t.Fatalf("decodeJPX: %v", err)
	}
	var want []byte
	for _, v := range annexJ10Samples {
		want = append(want, v/2, v, 255-v)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("samples %v, want %v", got, want)
	}
}

// TestDecodeJPX_Opacity drops the opacity channel of files OpenJPEG coded from
// 9x7 RGBA and grey-and-alpha images, which declare it in a Channel Definition
// box (T.800, I.5.3.6): the colour channels alone match the colour space.
func TestDecodeJPX_Opacity(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		pixel func(x, y int) []byte
	}{
		{"testdata/rgba.jp2", func(x, y int) []byte { return []byte{byte(x * 28), byte(y * 36), byte(x * y * 7)} }},
		{"testdata/grey_alpha.jp2", func(x, y int) []byte { return []byte{byte(x*25 + y*10)} }},
	} {
		got, err := decodeJPX(context.Background(), readJPX(t, tc.name), nil, jpx.DefaultMaxSamples)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		var want []byte
		for y := range 7 {
			for x := range 9 {
				want = append(want, tc.pixel(x, y)...)
			}
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s: %d bytes, want %d: %v", tc.name, len(got), len(want), got)
		}
	}
}

// TestDecodeJPX_Scaling maps 12-bit signed samples onto 8 bits, the linear
// mapping of T.800, I.5.3.3 after moving them to the unsigned range.
func TestDecodeJPX_Scaling(t *testing.T) {
	t.Parallel()

	got, err := decodeJPX(context.Background(), readJPX(t, "jpx/testdata/signed12.j2k"), nil, jpx.DefaultMaxSamples)
	if err != nil {
		t.Fatal(err)
	}
	src := readJPX(t, "jpx/testdata/source_s12.raw")
	for i := range got {
		v := int64(int16(binary.BigEndian.Uint16(src[2*i:]))) + 2048
		if want := byte((v*255 + 2047) / 4095); got[i] != want {
			t.Fatalf("sample %d = %d, want %d", i, got[i], want)
		}
	}
	if len(got) != len(src)/2 {
		t.Errorf("%d samples, want %d", len(got), len(src)/2)
	}
}

// TestDecodeJPX_Subsampled resamples 2x2-subsampled components onto the
// full-resolution grid, so each pixel carries all three.
func TestDecodeJPX_Subsampled(t *testing.T) {
	t.Parallel()

	got, err := decodeJPX(context.Background(), readJPX(t, "jpx/testdata/cprl_sub.j2k"), nil, jpx.DefaultMaxSamples)
	if err != nil {
		t.Fatal(err)
	}
	src := readJPX(t, "jpx/testdata/source_sub.raw")
	if len(got) != 24*20*3 {
		t.Fatalf("%d bytes, want %d", len(got), 24*20*3)
	}
	for y := range 20 {
		for x := range 24 {
			p := got[(y*24+x)*3:]
			want := []byte{src[y*24+x], src[480+(y/2)*12+x/2], src[600+(y/2)*12+x/2]}
			if !bytes.Equal(p[:3], want) {
				t.Fatalf("pixel (%d,%d) = %v, want %v", x, y, p[:3], want)
			}
		}
	}
}

// TestDecodeJPX_Malformed refuses data that is not JPEG 2000 without claiming
// the filter is unsupported.
func TestDecodeJPX_Malformed(t *testing.T) {
	t.Parallel()

	for _, data := range [][]byte{{}, {0xFF, 0x4F, 0xFF, 0x51, 0x00}} {
		_, err := decodeJPX(context.Background(), data, nil, jpx.DefaultMaxSamples)
		if err == nil || errors.Is(err, ErrUnsupportedFilter) {
			t.Errorf("decodeJPX(% X, jpx.DefaultMaxSamples) = %v, want a malformed-data error", data, err)
		}
	}
}
