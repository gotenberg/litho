package icc

import (
	"math"
	"testing"
)

// TestPCSLinkMatchesLabLink converts XYZ through NewPCSLink and the same
// colour, as Lab, through a link from LabProfile: both reach the destination
// through its BToA side, so they agree within the gamut.
func TestPCSLinkMatchesLabLink(t *testing.T) {
	t.Parallel()

	for _, dst := range [][]byte{SRGBProfile(), CMYKPrinterProfile(), SGrayProfile()} {
		p := mustParse(t, dst)
		for _, intent := range []Intent{Perceptual, RelativeColorimetric, AbsoluteColorimetric} {
			opts := TransformOptions{Intent: intent}
			pcs, err := NewPCSLink(p, opts)
			if err != nil {
				t.Fatalf("NewPCSLink(%q, %d): %v", p.Space, intent, err)
			}
			lab := mustLink(t, LabProfile(), p, opts)
			if pcs.InputChannels() != 3 || pcs.OutputChannels() != lab.OutputChannels() {
				t.Fatalf("channels %d to %d, want 3 to %d", pcs.InputChannels(), pcs.OutputChannels(), lab.OutputChannels())
			}
			got, want := make([]float64, pcs.OutputChannels()), make([]float64, pcs.OutputChannels())
			for _, c := range [][3]float64{{50, 0, 0}, {70, 20, -30}, {30, -10, 40}, {95, 2, 2}} {
				xyz := labToXYZ(c[0], c[1], c[2])
				pcs.Convert(xyz[:], got)
				lab.Convert([]float64{c[0] / 100, (c[1] + 128) / 255, (c[2] + 128) / 255}, want)
				for i := range got {
					if math.Abs(got[i]-want[i]) > 1.0/255 {
						t.Errorf("%q intent %d Lab %v: PCS link %v, Lab link %v", p.Space, intent, c, got, want)
						break
					}
				}
			}
		}
	}
}

// TestPCSLinkTakesUnclippedXYZ converts the PCS white, which is the
// destination's white, and a colour outside the visible gamut, X negative,
// which reaches the destination and is clipped there.
func TestPCSLinkTakesUnclippedXYZ(t *testing.T) {
	t.Parallel()

	l, err := NewPCSLink(mustParse(t, SRGBProfile()), TransformOptions{Intent: RelativeColorimetric})
	if err != nil {
		t.Fatal(err)
	}
	out := make([]float64, 3)
	l.Convert(d50[:], out)
	for _, v := range out {
		if math.Abs(v-1) > 1.0/255 {
			t.Fatalf("PCS white = %v, want white", out)
		}
	}
	// A green past the sRGB primary: green stays the dominant channel.
	l.Convert([]float64{-0.1, 0.6, 0.05}, out)
	if !(out[1] > out[0] && out[1] > out[2]) {
		t.Fatalf("an out-of-gamut green = %v, want green dominant", out)
	}
	for _, v := range out {
		if v < 0 || v > 1 || math.IsNaN(v) {
			t.Fatalf("out-of-gamut colour = %v, want components in [0, 1]", out)
		}
	}
}

// TestPCSLinkRefusesNoDestination refuses a missing destination and a
// DeviceLink, which has no PCS side.
func TestPCSLinkRefusesNoDestination(t *testing.T) {
	t.Parallel()

	if _, err := NewPCSLink(nil, TransformOptions{}); err == nil {
		t.Fatal("NewPCSLink(nil) succeeded")
	}
	if _, err := NewPCSLink(&Profile{Class: "link", Space: "RGB ", PCS: "CMYK"}, TransformOptions{}); err == nil {
		t.Fatal("NewPCSLink(DeviceLink) succeeded")
	}
}
