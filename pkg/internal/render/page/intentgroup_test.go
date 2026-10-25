package page

import (
	"encoding/binary"
	"testing"
)

// splitCMYKProfile builds a version 2.1 CMYK output profile over Lab whose
// two intents read black ink otherwise: its colorimetric table (A2B1) takes
// full black to L* 0 and its perceptual one (A2B0) to L* 50, so K 0.5 reads
// L* 50 relative colorimetric and L* 75 perceptual. Both tables back from
// Lab (B2A0, B2A1) invert the colorimetric one on the black axis, so a
// colour converted from sRGB into the space reads back as itself under
// relative colorimetric.
func splitCMYKProfile() []byte {
	be32 := binary.BigEndian.AppendUint32
	identity := []float64{1, 0, 0, 0, 1, 0, 0, 0, 1}
	lut8 := func(in, out int, clut []byte) []byte {
		b := append([]byte("mft1\x00\x00\x00\x00"), byte(in), byte(out), 2, 0)
		for _, v := range identity {
			b = be32(b, uint32(int32(v*65536)))
		}
		ramp := make([]byte, 256)
		for i := range ramp {
			ramp[i] = byte(i)
		}
		for range in {
			b = append(b, ramp...)
		}
		b = append(b, clut...)
		for range out {
			b = append(b, ramp...)
		}
		return b
	}
	toLab := func(kDark int) []byte {
		var clut []byte
		for c := range 2 {
			for m := range 2 {
				for y := range 2 {
					for k := range 2 {
						clut = append(clut, byte(max(0, 255-kDark*k-64*(c+m+y))), 128, 128)
					}
				}
			}
		}
		return lut8(4, 3, clut)
	}
	var fromLab []byte
	for l := range 2 {
		for range 4 {
			fromLab = append(fromLab, 0, 0, 0, byte(255*(1-l)))
		}
	}
	wtpt := []byte("XYZ \x00\x00\x00\x00")
	for _, v := range []float64{0.9642, 1, 0.8249} {
		wtpt = be32(wtpt, uint32(int32(v*65536+0.5)))
	}
	tags := []struct {
		sig  string
		data []byte
	}{{"wtpt", wtpt}, {"A2B0", toLab(128)}, {"A2B1", toLab(255)}, {"B2A0", lut8(3, 4, fromLab)}, {"B2A1", lut8(3, 4, fromLab)}}
	table := be32(nil, uint32(len(tags)))
	var body []byte
	offset := 128 + 4 + 12*len(tags)
	for _, tag := range tags {
		table = append(table, tag.sig...)
		table = be32(table, uint32(offset+len(body)))
		table = be32(table, uint32(len(tag.data)))
		body = append(body, tag.data...)
		for len(body)%4 != 0 {
			body = append(body, 0)
		}
	}
	header := make([]byte, 128)
	binary.BigEndian.PutUint32(header[0:], uint32(128+len(table)+len(body)))
	binary.BigEndian.PutUint32(header[8:], 0x02100000)
	copy(header[12:], "prtrCMYKLab ")
	copy(header[36:], "acsp")
	copy(header[68:], wtpt[8:])
	return append(append(header, table...), body...)
}

// TestGroupColourKeepsItsIntent checks that a colour in a page group's own
// blending colour space, painted under a rendering intent its profile reads
// otherwise than relative colorimetric, draws as it draws outside the group
// (ISO 32000-1, 8.6.5.8 and 11.3.4), where its components passed into the
// group would read back under the group's relative colorimetric. Under the
// initial intent it keeps its components.
func TestGroupColourKeepsItsIntent(t *testing.T) {
	t.Parallel()
	profile := stream("/N 4", string(splitCMYKProfile()))
	image := stream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /BitsPerComponent 8 /ColorSpace [/ICCBased 5 0 R]", "\x00\x00\x00\x80")
	// The Multiply square makes the page composite in its group's space.
	const res = "/Resources << /XObject << /Im 6 0 R >> /ColorSpace << /S [/ICCBased 5 0 R] >> " +
		"/ExtGState << /M << /BM /Multiply /ca 0.5 >> >> >>"
	content := "q /M gs 0 g 95 95 5 5 re f Q " +
		"q 30 0 0 30 10 60 cm /Im Do Q " +
		"q /Perceptual ri 30 0 0 30 60 60 cm /Im Do Q " +
		"q /S cs 0 0 0 0.5 sc 10 10 30 30 re f Q " +
		"q /Perceptual ri /S cs 0 0 0 0.5 sc 60 10 30 30 re f Q"
	read := func(group string) [4]rgb {
		img, _ := draw(t, fixture{page: res + group, content: content, objs: []string{profile, image}})
		return [4]rgb{at(t, img, 25, 25), at(t, img, 75, 25), at(t, img, 25, 75), at(t, img, 75, 75)}
	}
	plain := read("")
	grouped := read(" /Group << /S /Transparency /CS [/ICCBased 5 0 R] >>")
	if plain[1].r < plain[0].r+40 || plain[3].r < plain[2].r+40 {
		t.Fatalf("without a group: %v; want the perceptual image and fill lighter than the relative ones", plain)
	}
	for i, name := range []string{"image", "image under perceptual", "fill", "fill under perceptual"} {
		if !close8(grouped[i], plain[i], 3) {
			t.Errorf("%s in the group = %v, want %v as outside it", name, grouped[i], plain[i])
		}
	}
}

// TestGroupColourAlikeUnderIntents checks that a colour whose space reads
// alike under its intent and relative colorimetric keeps its components in
// the group's space, where a trip through sRGB would move a saturated ink.
func TestGroupColourAlikeUnderIntents(t *testing.T) {
	t.Parallel()
	const res = "/Resources << /ExtGState << /M << /BM /Multiply /ca 0.5 >> >> >>"
	content := "q /M gs 0 g 95 95 5 5 re f Q " +
		"q 1 0 0 0 k 10 10 30 30 re f Q " +
		"q /Perceptual ri 1 0 0 0 k 60 10 30 30 re f Q"
	img, _ := draw(t, fixture{page: res + " /Group << /S /Transparency /CS /DeviceCMYK >>", content: content})
	if relative, perceptual := at(t, img, 25, 75), at(t, img, 75, 75); relative != perceptual {
		t.Errorf("process cyan under perceptual = %v, want %v as under the initial intent", perceptual, relative)
	}
}
