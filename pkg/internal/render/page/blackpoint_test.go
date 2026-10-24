package page

import (
	"encoding/binary"
	"testing"
)

// liftedGreyProfile builds a version 2.1 grey display profile whose tone
// curve starts at a luminance of 1/8, so its black reads L* 42 and black
// point compensation (ISO 18619) moves it onto sRGB's black.
func liftedGreyProfile() []byte {
	be32 := binary.BigEndian.AppendUint32
	s15 := func(b []byte, v float64) []byte { return be32(b, uint32(int32(v*65536+0.5))) }
	wtpt := []byte("XYZ \x00\x00\x00\x00")
	for _, v := range []float64{0.9642, 1, 0.8249} {
		wtpt = s15(wtpt, v)
	}
	trc := []byte("curv\x00\x00\x00\x00")
	trc = be32(trc, 2)
	trc = binary.BigEndian.AppendUint16(trc, 0x2000)
	trc = binary.BigEndian.AppendUint16(trc, 0xFFFF)
	tags := []struct {
		sig  string
		data []byte
	}{{"wtpt", wtpt}, {"kTRC", trc}}
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
	copy(header[12:], "mntrGRAYXYZ ")
	copy(header[36:], "acsp")
	ill := s15(nil, 0.9642)
	ill = s15(ill, 1)
	ill = s15(ill, 0.8249)
	copy(header[68:], ill)
	return append(append(header, table...), body...)
}

// TestUseBlackPtCompReachesConversions checks ISO 32000-2, Table 57: under
// /UseBlackPtComp /OFF an ICCBased image and an ICCBased fill keep their
// profile's lifted black, while /ON and the initial /Default compensate it to
// sRGB's black. An image and a fill painted after Q return to /Default.
func TestUseBlackPtCompReachesConversions(t *testing.T) {
	t.Parallel()
	profile := stream("/N 1", string(liftedGreyProfile()))
	draw1 := func(t *testing.T, gs string) (image, fill, after rgb) {
		t.Helper()
		img, _ := draw(t, fixture{
			page: "/Resources << /ColorSpace << /CS [/ICCBased 5 0 R] >> /XObject << /Im 6 0 R >> " +
				"/ExtGState << /Off << /UseBlackPtComp /OFF >> /On << /UseBlackPtComp /ON >> >> >>",
			content: "q " + gs + " q 30 0 0 30 10 60 cm /Im Do Q /CS cs 0 sc 60 60 30 30 re f Q " +
				"q 30 0 0 30 10 10 cm /Im Do Q",
			objs: []string{profile, stream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /BitsPerComponent 8 "+
				"/ColorSpace [/ICCBased 5 0 R]", "\x00")},
		})
		return at(t, img, 25, 25), at(t, img, 75, 25), at(t, img, 25, 75)
	}
	defImage, defFill, _ := draw1(t, "")
	onImage, onFill, _ := draw1(t, "/On gs")
	offImage, offFill, offAfter := draw1(t, "/Off gs")
	for name, c := range map[string]rgb{"default image": defImage, "default fill": defFill, "ON image": onImage, "ON fill": onFill, "image after Q": offAfter} {
		if !close8(c, black, 1) {
			t.Errorf("%s = %v, want compensated black", name, c)
		}
	}
	// L* 42 is about sRGB 99; the bar only has to tell it from black.
	for name, c := range map[string]rgb{"OFF image": offImage, "OFF fill": offFill} {
		if c.r < 80 || c.r != c.g || c.g != c.b {
			t.Errorf("%s = %v, want the profile's own dark grey", name, c)
		}
	}
	if offImage != offFill {
		t.Errorf("OFF image %v and fill %v differ", offImage, offFill)
	}
}
