package icc

import "testing"

// TestSGrayProfileReadsAsDeviceGray pins that the generated grey profile
// converts each grey level to the sRGB colour of three equal components, the
// reading DeviceGray gets, and that it is a version 2 profile.
func TestSGrayProfileReadsAsDeviceGray(t *testing.T) {
	t.Parallel()

	p, err := Parse(SGrayProfile())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if p.Version != 2 || p.Space != "GRAY" {
		t.Fatalf("version %d, space %q", p.Version, p.Space)
	}
	tr, err := p.NewTransform(RelativeColorimetric)
	if err != nil {
		t.Fatalf("NewTransform: %v", err)
	}
	for v := range 256 {
		r, g, b := tr.Convert([]byte{byte(v)})
		if r != byte(v) || g != byte(v) || b != byte(v) {
			t.Fatalf("grey %d reads as %d,%d,%d", v, r, g, b)
		}
	}
	if !tr.Identity() {
		t.Error("the transform does not read as the identity, so colour management would convert DeviceGray through it")
	}
}
