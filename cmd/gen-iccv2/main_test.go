package main

import (
	"bytes"
	"encoding/binary"
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
