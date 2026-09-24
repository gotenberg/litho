package jpx

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// TestDecode_AnnexJ10 decodes the J.10 codestream to the samples J.10.5
// gives after the inverse 5-3 transformation and the DC level shift.
func TestDecode_AnnexJ10(t *testing.T) {
	t.Parallel()

	img, err := Decode(context.Background(), annexJ10, DefaultMaxSamples)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(img.Components) != 1 {
		t.Fatalf("%d components, want 1", len(img.Components))
	}
	c := img.Components[0]
	want := []int32{101, 103, 104, 105, 96, 97, 96, 102, 109}
	if c.Width != 1 || c.Height != 9 || c.Precision != 8 || c.Signed || !slices.Equal(c.Samples, want) {
		t.Errorf("component %dx%d, %d bits, signed %v, samples %v; want 1x9, 8 bits, unsigned, %v", c.Width, c.Height, c.Precision, c.Signed, c.Samples, want)
	}
}

// readPPM reads a binary PPM of 8-bit samples, skipping comment lines, and
// returns its three colour planes.
func readPPM(t *testing.T, path string) (int, int, [3][]int32) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fields []string
	for len(fields) < 4 {
		nl := bytes.IndexByte(b, '\n')
		line := string(b[:nl])
		b = b[nl+1:]
		if !strings.HasPrefix(line, "#") {
			fields = append(fields, strings.Fields(line)...)
		}
	}
	w, _ := strconv.Atoi(fields[1])
	h, _ := strconv.Atoi(fields[2])
	var planes [3][]int32
	for i := range w * h {
		for c := range 3 {
			planes[c] = append(planes[c], int32(b[3*i+c]))
		}
	}
	return w, h, planes
}

// TestDecode_Lossless decodes codestreams OpenJPEG coded losslessly, with the
// 5-3 filter, from source images committed beside them, and checks the
// samples equal the source exactly. The 23x19 RGB source reaches the
// reversible component transformation, every progression order, tiles and
// tile-parts, PPM and PPT, SOP and EPH, POC, every code-block style (modes63
// sets all six flags) and Maxshift ROI (roi, coded with -ROI c=0,U=4); the
// 24x20 three-component source subsampled 1x1, 2x2 and 2x2 reaches components
// of different sizes; the 12-bit signed source reaches signed samples, which
// take no DC level shift.
func TestDecode_Lossless(t *testing.T) {
	t.Parallel()

	_, _, rgb := readPPM(t, "testdata/source_rgb.ppm")
	sub, err := os.ReadFile("testdata/source_sub.raw")
	if err != nil {
		t.Fatal(err)
	}
	s12, err := os.ReadFile("testdata/source_s12.raw")
	if err != nil {
		t.Fatal(err)
	}
	bytesTo := func(b []byte) []int32 {
		out := make([]int32, len(b))
		for i, v := range b {
			out[i] = int32(v)
		}
		return out
	}
	var signed []int32
	for i := 0; i < len(s12); i += 2 {
		signed = append(signed, int32(int16(binary.BigEndian.Uint16(s12[i:]))))
	}
	type want struct {
		w, h    int
		samples []int32
	}
	rgbWant := []want{{23, 19, rgb[0]}, {23, 19, rgb[1]}, {23, 19, rgb[2]}}
	subWant := []want{{24, 20, bytesTo(sub[:480])}, {12, 10, bytesTo(sub[480:600])}, {12, 10, bytesTo(sub[600:])}}
	for _, tc := range []struct {
		name string
		want []want
	}{
		{"tiles_sop_eph", rgbWant},
		{"tiles_sop_eph_ppt", rgbWant},
		{"tiles_sop_eph_ppm", rgbWant},
		{"pcrl_offsets", rgbWant},
		{"poc_rlcp_cprl", rgbWant},
		{"poc_tiles", rgbWant},
		{"modes63", rgbWant},
		{"bypass", rgbWant},
		{"segsym", rgbWant},
		{"bypass_segsym", rgbWant},
		{"causal_segsym", rgbWant},
		{"roi", rgbWant},
		{"rpcl_sub", subWant},
		{"cprl_sub", subWant},
		{"signed12", []want{{23, 19, signed}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			data, err := os.ReadFile("testdata/" + tc.name + ".j2k")
			if err != nil {
				t.Fatal(err)
			}
			img, err := Decode(context.Background(), data, DefaultMaxSamples)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if len(img.Components) != len(tc.want) {
				t.Fatalf("%d components, want %d", len(img.Components), len(tc.want))
			}
			for c, w := range tc.want {
				got := img.Components[c]
				if got.Width != w.w || got.Height != w.h {
					t.Errorf("component %d is %dx%d, want %dx%d", c, got.Width, got.Height, w.w, w.h)
					continue
				}
				if n := differing(got.Samples, w.samples); n != 0 {
					t.Errorf("component %d differs from the source in %d of %d samples", c, n, len(w.samples))
				}
			}
		})
	}
}

// differing counts the positions at which a and b differ.
func differing(a, b []int32) int {
	n := 0
	for i := range a {
		if a[i] != b[i] {
			n++
		}
	}
	return n
}

// TestDecode_Irreversible decodes a codestream OpenJPEG coded with the 9-7
// filter and the irreversible component transformation at 2.5:1, and compares
// it with OpenJPEG's own decoding. The reconstruction parameter r of T.800,
// E-6 is the decoder's choice and floating-point rounding differs between
// implementations, so a sample may differ by one; a wrong filter, step size
// or transformation would differ by far more, far more often.
func TestDecode_Irreversible(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("testdata/irrev97.j2k")
	if err != nil {
		t.Fatal(err)
	}
	img, err := Decode(context.Background(), data, DefaultMaxSamples)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	_, _, ref := readPPM(t, "testdata/irrev97_openjpeg.ppm")
	for c := range 3 {
		got := img.Components[c].Samples
		off := 0
		for i := range got {
			d := got[i] - ref[c][i]
			if d < -1 || d > 1 {
				t.Fatalf("component %d sample %d = %d, OpenJPEG decodes %d", c, i, got[i], ref[c][i])
			}
			if d != 0 {
				off++
			}
		}
		if off > len(got)/100 {
			t.Errorf("component %d: %d of %d samples differ from OpenJPEG by one", c, off, len(got))
		}
	}
}

// TestDecode_ReversibleLossy decodes a codestream OpenJPEG coded with the 5-3
// filter at 12:1, so code-blocks stop short of their last bit-planes and each
// coefficient is reconstructed at the middle of the interval its decoded bits
// leave open (T.800, E-8 with r = 1/2). The integer path leaves no rounding to
// choose, and the result equals OpenJPEG's.
func TestDecode_ReversibleLossy(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("testdata/rev53_lossy.j2k")
	if err != nil {
		t.Fatal(err)
	}
	img, err := Decode(context.Background(), data, DefaultMaxSamples)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	_, _, ref := readPPM(t, "testdata/rev53_lossy_openjpeg.ppm")
	_, _, src := readPPM(t, "testdata/source_rgb.ppm")
	for c := range 3 {
		if n := differing(img.Components[c].Samples, ref[c]); n != 0 {
			t.Errorf("component %d differs from OpenJPEG in %d samples", c, n)
		}
		if differing(img.Components[c].Samples, src[c]) == 0 {
			t.Errorf("component %d decodes losslessly, so the test reaches no truncated code-block", c)
		}
	}
}

// TestDescribe_CapScales pins that the cap Describe is given bounds the image
// and its rendered grid: the J.10 codestream describes under a cap of exactly
// its samples and is refused under one sample less.
func TestDescribe_CapScales(t *testing.T) {
	t.Parallel()

	img, err := Describe(annexJ10, DefaultMaxSamples)
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	var samples int64
	for _, c := range img.Components {
		samples += int64(c.Width) * int64(c.Height)
	}
	exact, err := Describe(annexJ10, samples)
	if err != nil {
		t.Fatalf("Describe under a cap of its %d samples: %v", samples, err)
	}
	chs, err := exact.Channels()
	if err != nil {
		t.Fatalf("Channels: %v", err)
	}
	if _, err := exact.Render(context.Background(), chs[:1], func(v int32, _ int, _ bool) byte { return byte(v) }); err != nil {
		t.Errorf("Render one channel under a cap of its %d samples: %v", samples, err)
	}
	if _, err := Describe(annexJ10, samples-1); !errors.Is(err, ErrLimit) {
		t.Errorf("Describe under a cap one sample short: err = %v, want ErrLimit", err)
	}
}
