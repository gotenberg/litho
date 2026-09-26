package pdf

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// cmykPDF builds a one-page document showing /Im0, a 1x1 DeviceCMYK image of
// full cyan.
func cmykPDF() []byte {
	cyan := []byte{255, 0, 0, 0}
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /XObject << /Im0 4 0 R >> >> >>",
		fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceCMYK /BitsPerComponent 8 /Length %d >>\nstream\n%s\nendstream",
			len(cyan), cyan),
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

// whiteProfile returns the built-in CMYK profile with the output tables of
// its lut16Type AToB0 table flattened to L* 100, a* and b* 0: every colour
// reads as white, which the built-in profile gives no ink.
func whiteProfile(t *testing.T) []byte {
	t.Helper()
	profile, err := os.ReadFile(filepath.Join("..", "internal", "graphics", "icc", "profiles", "CGATS001Compat-v2-micro.icc"))
	if err != nil {
		t.Fatal(err)
	}
	for i := range int(binary.BigEndian.Uint32(profile[128:])) {
		entry := profile[132+12*i:]
		if string(entry[:4]) != "A2B0" {
			continue
		}
		tag := profile[binary.BigEndian.Uint32(entry[4:]):]
		in, out, grid := int(tag[8]), int(tag[9]), int(tag[10])
		inEntries, outEntries := int(binary.BigEndian.Uint16(tag[48:])), int(binary.BigEndian.Uint16(tag[50:]))
		points := 1
		for range in {
			points *= grid
		}
		pos := 52 + 2*in*inEntries + 2*points*out
		for c := range out {
			v := uint16(0x8000)
			if c == 0 {
				v = 0xFF00
			}
			for range outEntries {
				binary.BigEndian.PutUint16(tag[pos:], v)
				pos += 2
			}
		}
		return profile
	}
	t.Fatal("no AToB0 table")
	return nil
}

// TestWithDefaultCMYKProfile_ReadsDeviceCMYK pins that the caller's profile
// replaces the built-in one for DeviceCMYK, and that the option copies it.
func TestWithDefaultCMYKProfile_ReadsDeviceCMYK(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		profile []byte
		want    [3]uint32
	}{
		{"the built-in profile", nil, [3]uint32{0, 174, 239}},
		{"the caller's profile", whiteProfile(t), [3]uint32{255, 255, 255}},
	} {
		data := cmykPDF()
		var opts []OpenOption
		if tc.profile != nil {
			opts = append(opts, WithDefaultCMYKProfile(tc.profile))
		}
		r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)), opts...)
		if err != nil {
			t.Fatalf("%s: Open: %v", tc.name, err)
		}
		clear(tc.profile)
		img, err := r.DecodeImage(context.Background(), 1, "Im0")
		if err != nil {
			t.Fatalf("%s: DecodeImage: %v", tc.name, err)
		}
		if got := levels(img)[0]; got != tc.want {
			t.Errorf("%s: cyan = %v, want %v", tc.name, got, tc.want)
		}
		if err := r.Close(); err != nil {
			t.Errorf("%s: Close: %v", tc.name, err)
		}
	}
}

// TestWithDefaultCMYKProfile_Refuses pins that Open names a profile litho
// cannot read CMYK colours through with ErrInvalidCMYKProfile.
func TestWithDefaultCMYKProfile_Refuses(t *testing.T) {
	t.Parallel()

	header := func(space string) []byte {
		b := make([]byte, 132)
		copy(b[16:], space)
		copy(b[20:], "Lab ")
		copy(b[36:], "acsp")
		return b
	}
	data := cmykPDF()
	for name, profile := range map[string][]byte{
		"not a profile":                []byte("not a profile"),
		"an RGB profile":               header("RGB "),
		"a CMYK profile with no table": header("CMYK"),
	} {
		_, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)), WithDefaultCMYKProfile(profile))
		if !errors.Is(err, ErrInvalidCMYKProfile) {
			t.Errorf("%s: Open = %v, want %v", name, err, ErrInvalidCMYKProfile)
		}
	}
}
