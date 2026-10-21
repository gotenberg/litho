package dct

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"math"
	"testing"
)

// testPhoto returns width x height pixels of comps components that look
// like a photograph to a JPEG encoder: smooth gradients, a sharp edge and a
// little deterministic noise.
func testPhoto(width, height, comps int) []byte {
	out := make([]byte, width*height*comps)
	seed := uint32(1)
	for y := range height {
		for x := range width {
			for c := range comps {
				seed = seed*1664525 + 1013904223
				noise := float64(seed>>24)/255*12 - 6
				v := 128 + 90*math.Sin(float64(x)/17+float64(c))*math.Cos(float64(y)/23-float64(c))
				if x > width/2 && y > height/3 {
					v = 255 - v
				}
				out[(y*width+x)*comps+c] = byte(min(max(math.Round(v+noise), 0), 255))
			}
		}
	}
	return out
}

// psnr returns the peak signal-to-noise ratio of b against a, in decibels.
func psnr(a, b []byte) float64 {
	sum := 0.0
	for i := range a {
		d := float64(a[i]) - float64(b[i])
		sum += d * d
	}
	if sum == 0 {
		return math.Inf(1)
	}
	return 10 * math.Log10(255*255/(sum/float64(len(a))))
}

// minPSNR is the least PSNR [TestEncodeRoundTrip] accepts of the test photo
// at each quality, a few decibels under what the encoder measured, so a
// regression in the transform, the quantization or the colour conversion
// fails it while a change of rounding does not.
var minPSNR = map[int]float64{50: 30, 75: 32, 85: 34, 90: 35, 95: 37, 100: 40}

// minPSNR420 is the least PSNR of the test photo under 4:2:0 subsampling, at
// any quality: the encoder measured 28.1 dB at quality 50 and 30.1 dB at 100.
const minPSNR420 = 27

// stdlibSamples decodes data with the standard library's decoder, an
// implementation independent of this package, into samples laid out as
// [Decode] lays them out. Go reads a four-component Adobe JPEG as inverted
// CMYK, so its samples are inverted back.
func stdlibSamples(t *testing.T, data []byte, comps int) []byte {
	t.Helper()
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("image/jpeg: %v", err)
	}
	b := img.Bounds()
	out := make([]byte, 0, b.Dx()*b.Dy()*comps)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			switch comps {
			case 1:
				out = append(out, img.(*image.Gray).GrayAt(x, y).Y)
			case 3:
				r, g, bb, _ := img.At(x, y).RGBA()
				out = append(out, byte(r>>8), byte(g>>8), byte(bb>>8))
			case 4:
				c := img.(*image.CMYK).CMYKAt(x, y)
				out = append(out, 255-c.C, 255-c.M, 255-c.Y, 255-c.K)
			}
		}
	}
	return out
}

func TestEncodeRoundTrip(t *testing.T) {
	ctx := context.Background()
	const width, height = 61, 37
	for _, comps := range []int{1, 3, 4} {
		src := testPhoto(width, height, comps)
		for _, sub := range []Subsampling{Subsample444, Subsample420} {
			if sub == Subsample420 && comps != 3 {
				continue
			}
			for _, q := range []int{50, 75, 85, 90, 95, 100} {
				for _, optimize := range []bool{false, true} {
					name := fmt.Sprintf("comps%d/sub%d/q%d/opt%v", comps, sub, q, optimize)
					t.Run(name, func(t *testing.T) {
						data, err := Encode(ctx, src, width, height, comps, EncodeOptions{Quality: q, Subsampling: sub, OptimizeHuffman: optimize})
						if err != nil {
							t.Fatal(err)
						}
						out, n, w, h, err := Decode(ctx, data, -1, 0, DefaultMaxSamples)
						if err != nil {
							t.Fatalf("decoding: %v", err)
						}
						if n != comps || w != width || h != height {
							t.Fatalf("decoded %dx%d of %d components", w, h, n)
						}
						bound := minPSNR[q]
						if sub == Subsample420 {
							// Half the chroma resolution bounds the
							// error whatever the quality: the photo's
							// channels vary apart, at the noise's scale.
							bound = minPSNR420
						}
						if p := psnr(src, out); p < bound {
							t.Errorf("PSNR %.2f dB, want at least %.0f", p, bound)
						}
						if p := psnr(src, stdlibSamples(t, data, comps)); p < bound-1 {
							t.Errorf("image/jpeg: PSNR %.2f dB, want at least %.0f", p, bound-1)
						}
					})
				}
			}
		}
	}
}

func TestEncodeEdgeSizes(t *testing.T) {
	ctx := context.Background()
	for _, size := range [][2]int{{1, 1}, {1, 17}, {17, 1}, {8, 8}, {16, 16}, {9, 15}} {
		for _, comps := range []int{1, 3, 4} {
			src := testPhoto(size[0], size[1], comps)
			data, err := Encode(ctx, src, size[0], size[1], comps, EncodeOptions{Quality: 90, Subsampling: Subsample420})
			if err != nil {
				t.Fatalf("%v x%d: %v", size, comps, err)
			}
			_, n, w, h, err := Decode(ctx, data, -1, 0, DefaultMaxSamples)
			if err != nil || n != comps || w != size[0] || h != size[1] {
				t.Fatalf("%v x%d: decoded %dx%d of %d: %v", size, comps, w, h, n, err)
			}
			stdlibSamples(t, data, comps)
		}
	}
}

func TestEncodeRestartInterval(t *testing.T) {
	ctx := context.Background()
	src := testPhoto(70, 50, 3)
	plain, err := Encode(ctx, src, 70, 50, 3, EncodeOptions{Quality: 85, Subsampling: Subsample420})
	if err != nil {
		t.Fatal(err)
	}
	want, _, _, _, err := Decode(ctx, plain, -1, 0, DefaultMaxSamples)
	if err != nil {
		t.Fatal(err)
	}
	for _, ri := range []int{1, 3, 7} {
		for _, optimize := range []bool{false, true} {
			data, err := Encode(ctx, src, 70, 50, 3, EncodeOptions{Quality: 85, Subsampling: Subsample420, RestartInterval: ri, OptimizeHuffman: optimize})
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(data, []byte{0xFF, 0xD0}) || !bytes.Contains(data, []byte{0xFF, 0xDD}) {
				t.Fatalf("interval %d: no restart marker or DRI segment", ri)
			}
			got, _, _, _, err := Decode(ctx, data, -1, 0, DefaultMaxSamples)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("interval %d: samples differ from the image without restarts", ri)
			}
			if !bytes.Equal(got, stdlibSamples(t, data, 3)) && psnr(got, stdlibSamples(t, data, 3)) < 40 {
				t.Errorf("interval %d: image/jpeg reads another image", ri)
			}
		}
	}
}

// TestEncodeOptimizedTables pins that tables built from the image's own
// counts code the same coefficients in no more bytes, and that the
// optimized output is what OptimizeHuffman makes of the standard one.
func TestEncodeOptimizedTables(t *testing.T) {
	ctx := context.Background()
	src := testPhoto(120, 80, 3)
	opts := EncodeOptions{Quality: 85}
	std, err := Encode(ctx, src, 120, 80, 3, opts)
	if err != nil {
		t.Fatal(err)
	}
	opts.OptimizeHuffman = true
	opt, err := Encode(ctx, src, 120, 80, 3, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(opt) >= len(std) {
		t.Errorf("optimized %d bytes, standard %d", len(opt), len(std))
	}
	a, _, _, _, err := Decode(ctx, std, -1, 0, DefaultMaxSamples)
	if err != nil {
		t.Fatal(err)
	}
	b, _, _, _, err := Decode(ctx, opt, -1, 0, DefaultMaxSamples)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Error("optimized tables decode to other samples")
	}
	if _, ok, err := OptimizeHuffman(ctx, std); err != nil || !ok {
		t.Errorf("OptimizeHuffman of the encoder's output: ok %v, %v", ok, err)
	}
}

func TestEncodeDeterministic(t *testing.T) {
	ctx := context.Background()
	src := testPhoto(33, 21, 4)
	opts := EncodeOptions{Quality: 77, OptimizeHuffman: true, RestartInterval: 2}
	a, err := Encode(ctx, src, 33, 21, 4, opts)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Encode(ctx, src, 33, 21, 4, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Error("two encodings of one image differ")
	}
}

// TestEncodeMarkers pins the colour signalling a PDF reader goes by (ISO
// 32000-1, 7.4.8): an Adobe APP14 marker of transform 1 for YCbCr, of
// transform 0 for CMYK, and none for grey; and the baseline frame marker.
func TestEncodeMarkers(t *testing.T) {
	ctx := context.Background()
	for comps, want := range map[int][]byte{
		1: nil,
		3: append([]byte("Adobe"), 0x00, 0x64, 0, 0, 0, 0, 1),
		4: append([]byte("Adobe"), 0x00, 0x64, 0, 0, 0, 0, 0),
	} {
		data, err := Encode(ctx, testPhoto(9, 9, comps), 9, 9, comps, EncodeOptions{Quality: 50})
		if err != nil {
			t.Fatal(err)
		}
		app14 := bytes.Contains(data, []byte{0xFF, 0xEE})
		if want == nil && app14 || want != nil && !bytes.Contains(data, want) {
			t.Errorf("%d components: APP14 present %v, want %q", comps, app14, want)
		}
		if !bytes.Contains(data, []byte{0xFF, 0xC0, 0x00, byte(8 + 3*comps), 8}) {
			t.Errorf("%d components: no baseline SOF0 header", comps)
		}
	}
}

// TestEncodeQualityScaling pins the libjpeg scaling of the Annex K tables:
// quality 50 is Table K.1 itself, 100 all ones, and 1 the table times 50
// held to 255.
func TestEncodeQualityScaling(t *testing.T) {
	if got := scaledQuant(&lumaQuant, 50); got != lumaQuant {
		t.Errorf("quality 50: %v", got)
	}
	for i, v := range scaledQuant(&chromaQuant, 100) {
		if v != 1 {
			t.Fatalf("quality 100: entry %d is %d", i, v)
		}
	}
	if got := scaledQuant(&lumaQuant, 1); got[0] != 255 || got[2] != 255 {
		t.Errorf("quality 1: %v", got[:4])
	}
	if got := scaledQuant(&lumaQuant, 75)[0]; got != 8 {
		t.Errorf("quality 75: DC quantizer %d, want 8", got)
	}
}

// TestStandardTables checks the Annex K Huffman tables as a decoder builds
// them: 12 DC values and 162 AC values, each listed once, and counts that
// make a valid prefix code.
func TestStandardTables(t *testing.T) {
	for name, spec := range map[string]huffSpec{"luma DC": stdLumaDC, "chroma DC": stdChromaDC, "luma AC": stdLumaAC, "chroma AC": stdChromaAC} {
		want := 162
		if len(name) > 3 && name[len(name)-2:] == "DC" {
			want = 12
		}
		if len(spec.values) != want {
			t.Errorf("%s: %d values, want %d", name, len(spec.values), want)
		}
		seen := map[byte]bool{}
		for _, v := range spec.values {
			if seen[v] {
				t.Errorf("%s: value 0x%02x twice", name, v)
			}
			seen[v] = true
		}
		if _, err := newHuffman(spec.counts, spec.values); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestEncodeRefusesInvalidInput(t *testing.T) {
	ctx := context.Background()
	ok := EncodeOptions{Quality: 75}
	for name, tc := range map[string]struct {
		samples []byte
		w, h, c int
		opts    EncodeOptions
	}{
		"two components":      {make([]byte, 8), 2, 2, 2, ok},
		"zero width":          {nil, 0, 2, 1, ok},
		"too wide":            {make([]byte, MaxDimension+1), MaxDimension + 1, 1, 1, ok},
		"short samples":       {make([]byte, 3), 2, 2, 1, ok},
		"quality 0":           {make([]byte, 4), 2, 2, 1, EncodeOptions{}},
		"quality 101":         {make([]byte, 4), 2, 2, 1, EncodeOptions{Quality: 101}},
		"negative interval":   {make([]byte, 4), 2, 2, 1, EncodeOptions{Quality: 75, RestartInterval: -1}},
		"unknown subsampling": {make([]byte, 12), 2, 2, 3, EncodeOptions{Quality: 75, Subsampling: 7}},
	} {
		if _, err := Encode(ctx, tc.samples, tc.w, tc.h, tc.c, tc.opts); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("%s: %v, want ErrInvalidInput", name, err)
		}
	}
}

func TestEncodeHonoursContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Encode(ctx, testPhoto(256, 256, 3), 256, 256, 3, EncodeOptions{Quality: 75}); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: %v", err)
	}
}

// TestFDCTMatchesDefinition checks the factorised transform against the
// sums of T.81, A.3.3, unquantized and through the quantizers of every
// quality's tables.
func TestFDCTMatchesDefinition(t *testing.T) {
	seed := uint32(3)
	one := [64]int{}
	for i := range one {
		one[i] = 1
	}
	unit := fdctDivisors(&one)
	tables := []*[64]int{&one}
	for _, q := range []int{10, 50, 85, 95} {
		luma, chroma := scaledQuant(&lumaQuant, q), scaledQuant(&chromaQuant, q)
		tables = append(tables, &luma, &chroma)
	}
	differ, total := 0, 0
	for range 2000 {
		var block [64]float64
		for i := range block {
			seed = seed*1664525 + 1013904223
			block[i] = float64(seed>>24) - 128
		}
		var want [64]float64
		for v := range 8 {
			for u := range 8 {
				s := 0.0
				for y := range 8 {
					for x := range 8 {
						s += idctCos[x][u] * idctCos[y][v] * block[y*8+x]
					}
				}
				want[v*8+u] = s
			}
		}
		got := block
		for r := range 8 {
			aan8(got[r*8:r*8+8], 1)
		}
		for c := range 8 {
			aan8(got[c:], 8)
		}
		for i := range got {
			if d := math.Abs(got[i]/unit[i] - want[i]); d > 1e-9 {
				t.Fatalf("coefficient %d: %v, want %v", i, got[i]/unit[i], want[i])
			}
		}
		for _, q := range tables {
			div := fdctDivisors(q)
			in := block
			out := make([]int16, 64)
			fdct(&in, &div, out)
			for k, c := range out {
				i := zigzag[k]
				ref := math.Round(want[i] / float64(q[i]))
				ref = min(max(ref, -maxAC), maxAC)
				total++
				if float64(c) != ref {
					// Only a quotient within rounding error of a half may
					// round the other way.
					if math.Abs(float64(c)-ref) > 1 || math.Abs(math.Abs(want[i]/float64(q[i])-math.Trunc(want[i]/float64(q[i])))-0.5) > 1e-9 {
						t.Fatalf("quantized coefficient %d: %d, want %v", i, c, ref)
					}
					differ++
				}
			}
		}
	}
	t.Logf("%d of %d quantized coefficients round the other way", differ, total)
}

func BenchmarkEncode(b *testing.B) {
	ctx := context.Background()
	src := testPhoto(512, 512, 3)
	for _, tc := range []struct {
		name string
		opts EncodeOptions
	}{
		{"q85_420", EncodeOptions{Quality: 85, Subsampling: Subsample420}},
		{"q95_444", EncodeOptions{Quality: 95}},
		{"q85_420_optimized", EncodeOptions{Quality: 85, Subsampling: Subsample420, OptimizeHuffman: true}},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.SetBytes(int64(len(src)))
			b.ReportAllocs()
			for b.Loop() {
				if _, err := Encode(ctx, src, 512, 512, 3, tc.opts); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
