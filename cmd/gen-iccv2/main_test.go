package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"os"
	"testing"
)

// TestGeneratedProfileIsCommitted fails when the committed profile is not the
// generator's output, so neither drifts from the other.
func TestGeneratedProfileIsCommitted(t *testing.T) {
	t.Parallel()

	committed, err := os.ReadFile("../../pkg/internal/graphics/icc/profiles/" + outName)
	if err != nil {
		t.Fatalf("reading the committed profile: %v", err)
	}
	if !bytes.Equal(generate(), committed) {
		t.Errorf("profiles/%s differs from the generator's output: run go generate ./pkg/internal/graphics/icc/...", outName)
	}
}

func TestGenerateIsVersion2GreyDisplayProfile(t *testing.T) {
	t.Parallel()

	p := generate()
	if int(binary.BigEndian.Uint32(p)) != len(p) || len(p)%4 != 0 {
		t.Fatalf("size field %d for %d bytes", binary.BigEndian.Uint32(p), len(p))
	}
	if p[8] != 2 || string(p[12:24]) != "mntrGRAYXYZ " || string(p[36:40]) != "acsp" {
		t.Errorf("header = %x", p[:40])
	}
	count := binary.BigEndian.Uint32(p[128:])
	sigs := map[string][]byte{}
	for i := range count {
		e := p[132+12*i:]
		off, size := binary.BigEndian.Uint32(e[4:]), binary.BigEndian.Uint32(e[8:])
		sigs[string(e[:4])] = p[off : off+size]
	}
	for sig, typ := range map[string]string{"desc": "desc", "kTRC": "curv", "wtpt": "XYZ ", "cprt": "text"} {
		if b, ok := sigs[sig]; !ok || string(b[:4]) != typ {
			t.Errorf("%s tag missing or not of type %q", sig, typ)
		}
	}
	// The profile states its licence where a profile reader looks for one,
	// its copyrightTag (ICC.1:2001-04, 6.4), as the CC0 profile beside it
	// does.
	if cprt := sigs["cprt"]; !bytes.Contains(cprt, []byte("CC0 1.0")) || cprt[len(cprt)-1] != 0 {
		t.Errorf("cprt = %q, want a NUL-terminated CC0 1.0 dedication", cprt)
	}
	trc := sigs["kTRC"]
	if n := binary.BigEndian.Uint32(trc[8:]); n != curveEntries {
		t.Fatalf("kTRC holds %d entries", n)
	}
	last := float64(binary.BigEndian.Uint16(trc[12+2*(curveEntries-1):])) / 65535
	mid := float64(binary.BigEndian.Uint16(trc[12+2*512:])) / 65535
	if last != 1 || math.Abs(mid-srgbToLinear(512.0/1023)) > 1.0/65535 {
		t.Errorf("kTRC ends at %g, holds %g at 512", last, mid)
	}
}

func TestSRGBToLinear(t *testing.T) {
	t.Parallel()

	// IEC 61966-2-1, 5.2: the linear segment below 0.04045 meets the power
	// segment there, and both ends are fixed.
	for _, tc := range []struct{ in, want float64 }{{0, 0}, {0.04045, 0.04045 / 12.92}, {0.5, 0.21404}, {1, 1}} {
		if got := srgbToLinear(tc.in); math.Abs(got-tc.want) > 1e-5 {
			t.Errorf("srgbToLinear(%g) = %g, want %g", tc.in, got, tc.want)
		}
	}
}

// cmykSource reads the CC0 characterisation the CMYK profile is built from.
func cmykSource(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("../../pkg/internal/graphics/icc/profiles/" + sourceName)
	if err != nil {
		t.Fatalf("reading the source profile: %v", err)
	}
	return b
}

// TestGeneratedCMYKProfileIsCommitted fails when the committed CMYK profile
// is not the generator's output, so neither drifts from the other.
func TestGeneratedCMYKProfileIsCommitted(t *testing.T) {
	t.Parallel()

	committed, err := os.ReadFile("../../pkg/internal/graphics/icc/profiles/" + cmykName)
	if err != nil {
		t.Fatalf("reading the committed profile: %v", err)
	}
	generated, err := generateCMYK(cmykSource(t))
	if err != nil {
		t.Fatalf("generateCMYK: %v", err)
	}
	if !bytes.Equal(generated, committed) {
		t.Errorf("profiles/%s differs from the generator's output: run go generate ./pkg/internal/graphics/icc/...", cmykName)
	}
}

// TestGenerateCMYKIsVersion2PrinterProfile pins the header and the tags an
// ICC.1:2001-04 output profile requires (6.3.1.3), the AToB table carried
// over from the source, the CC0 dedication, and a second run's bytes.
func TestGenerateCMYKIsVersion2PrinterProfile(t *testing.T) {
	t.Parallel()

	source := cmykSource(t)
	p, err := generateCMYK(source)
	if err != nil {
		t.Fatalf("generateCMYK: %v", err)
	}
	if int(binary.BigEndian.Uint32(p)) != len(p) || len(p)%4 != 0 {
		t.Fatalf("size field %d for %d bytes", binary.BigEndian.Uint32(p), len(p))
	}
	if p[8] != 2 || string(p[12:24]) != "prtrCMYKLab " || string(p[36:40]) != "acsp" {
		t.Errorf("header = %x", p[:40])
	}
	for sig, typ := range map[string]string{
		"desc": "desc", "cprt": "text", "wtpt": "XYZ ", "gamt": "mft2",
		"A2B0": "mft2", "A2B1": "mft2", "A2B2": "mft2", "B2A0": "mft2", "B2A1": "mft2", "B2A2": "mft2",
	} {
		b, err := findTag(p, sig)
		if err != nil || string(b[:4]) != typ {
			t.Errorf("%s tag missing or not of type %q: %v", sig, typ, err)
		}
	}
	got, err := findTag(p, "A2B0")
	if err != nil {
		t.Fatalf("A2B0: %v", err)
	}
	want, err := findTag(source, "A2B0")
	if err != nil {
		t.Fatalf("source A2B0: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Error("A2B0 differs from the source's, so the profile no longer reads CMYK as litho's built-in DeviceCMYK reading")
	}
	if cprt, err := findTag(p, "cprt"); err != nil || !bytes.Contains(cprt, []byte("CC0 1.0")) || cprt[len(cprt)-1] != 0 {
		t.Errorf("cprt = %q, want a NUL-terminated CC0 1.0 dedication: %v", cprt, err)
	}
	again, err := generateCMYK(source)
	if err != nil || !bytes.Equal(p, again) {
		t.Errorf("a second run differs: %v", err)
	}
}

// TestGenerateCMYKSeparationRoundTrips pins that the BToA table inverts the
// AToB table: a printable CMYK colour, read to Lab, separated and read again,
// comes back close to its first reading; the paper separates to no ink; and a
// neutral shadow prints mostly in black ink.
func TestGenerateCMYKSeparationRoundTrips(t *testing.T) {
	t.Parallel()

	p, err := generateCMYK(cmykSource(t))
	if err != nil {
		t.Fatalf("generateCMYK: %v", err)
	}
	table := func(sig string) *lut16 {
		b, err := findTag(p, sig)
		if err != nil {
			t.Fatalf("%s: %v", sig, err)
		}
		l, err := parseLut16(b)
		if err != nil {
			t.Fatalf("%s: %v", sig, err)
		}
		return l
	}
	a2b, b2a := table("A2B0"), table("B2A0")
	separate := func(lab [3]float64) [4]float64 {
		return b2a.eval([]float64{lab[0] / lScale, (lab[1] + 128) / abScale, (lab[2] + 128) / abScale})
	}
	levels := []float64{0, 0.25, 0.5, 0.75, 1}
	var sum, worst float64
	var n int
	for i := range 625 {
		x := [4]float64{levels[i/125], levels[i/25%5], levels[i/5%5], levels[i%5]}
		if x[0]+x[1]+x[2]+x[3] > totalInk {
			continue
		}
		first := toLab(a2b.eval(x[:]))
		y := separate(first)
		again := toLab(a2b.eval(y[:]))
		d := math.Sqrt(math.Pow(first[0]-again[0], 2) + math.Pow(first[1]-again[1], 2) + math.Pow(first[2]-again[2], 2))
		sum += d
		worst = math.Max(worst, d)
		n++
	}
	if mean := sum / float64(n); mean > 2 || worst > 10 {
		t.Errorf("round trip ΔE*ab mean %.2f, worst %.2f over %d colours", mean, worst, n)
	}
	if paper := separate([3]float64{100, 0, 0}); paper[0]+paper[1]+paper[2]+paper[3] > 0.01 {
		t.Errorf("the paper white separates to %v, want no ink", paper)
	}
	if shadow := separate([3]float64{20, 0, 0}); shadow[3] < 0.5 {
		t.Errorf("a neutral shadow separates to %v, want mostly black ink", shadow)
	}
}

func TestGenerateCMYKRejectsOtherSources(t *testing.T) {
	t.Parallel()

	for name, src := range map[string][]byte{
		"empty": nil,
		"grey":  generate(),
	} {
		if _, err := generateCMYK(src); !errors.Is(err, errSource) {
			t.Errorf("%s: err = %v, want errSource", name, err)
		}
	}
}
