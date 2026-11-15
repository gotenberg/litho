package effect

import (
	"context"
	"math"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typeset"
)

func TestGaussianKernel_Weights(t *testing.T) {
	for _, tc := range []struct {
		sigma float64
		want  []uint32
	}{
		// e^(-i^2/2) times 65,536: 1, 0.6065, 0.1353, 0.0111.
		{1, []uint32{65536, 39750, 8869, 728}},
		{0.5, []uint32{65536, 8869, 22}},
		{0, nil},
		{math.NaN(), nil},
		{-3, nil},
		{1.0 / 512, nil},
	} {
		if got := GaussianKernel(tc.sigma).W; !slices.Equal(got, tc.want) {
			t.Errorf("GaussianKernel(%v) = %v, want %v", tc.sigma, got, tc.want)
		}
	}
	if n := len(GaussianKernel(1e9).W); n != MaxKernelRadius+1 {
		t.Errorf("GaussianKernel(1e9) has %d weights a side, want %d", n, MaxKernelRadius+1)
	}
}

func TestTentBoxKernel_Weights(t *testing.T) {
	for _, tc := range []struct {
		name string
		k    Kernel
		want []uint32
	}{
		{"tent 2", TentKernel(2), []uint32{768, 512, 256}},
		{"tent 1.5", TentKernel(1.5), []uint32{640, 384, 128}},
		{"tent 0", TentKernel(0), nil},
		{"box 3 once", BoxKernel(3, 1), []uint32{1, 1}},
		{"box 3 twice", BoxKernel(3, 2), []uint32{3, 2, 1}},
		{"box 3 thrice", BoxKernel(3, 3), []uint32{7, 6, 3, 1}},
		// Even widths: d, d and d+1 for three passes, d and d for two.
		{"box 2 thrice", BoxKernel(2, 3), []uint32{4, 3, 1}},
		{"box 2 twice", BoxKernel(2, 2), []uint32{2, 1}},
		{"box 2 once", BoxKernel(2, 1), []uint32{1, 1}},
		{"box 1", BoxKernel(1, 3), nil},
	} {
		if !slices.Equal(tc.k.W, tc.want) {
			t.Errorf("%s = %v, want %v", tc.name, tc.k.W, tc.want)
		}
	}
}

// TestBlur_StepIsErrorFunction blurs a sharp vertical edge and compares
// each pixel with two answers: the convolution of the same pixels with
// the sampled Gaussian in floating point, within 0.6 levels (the integer
// weights and the two roundings), and, from a deviation of 2 pixels, the
// Gaussian's integral 255 Phi((x + 1/2 - e) / sigma), Phi the normal
// distribution, within one level. Below 2 pixels the sampled kernel and
// the integral part by up to 4 levels at 0.8: the kernel samples the
// Gaussian at pixel centres, as Filter Effects 1 defines it.
func TestBlur_StepIsErrorFunction(t *testing.T) {
	for _, sigma := range []float64{0.8, 1.5, 2, 3, 7.25, 20} {
		const w = 200
		edge := 100.0
		s := filled(t, w, 3, typeset.Identity, Path{Ops: rect(edge, -1, 1e4, 10)})
		src := slices.Clone(s.Pix[w : 2*w])
		if err := Blur(context.Background(), s, GaussianKernel(sigma), Kernel{}, EdgeDuplicate); err != nil {
			t.Fatal(err)
		}
		n := int(math.Ceil(3 * sigma))
		sampled, integral := 0.0, 0.0
		for x := range w {
			num, den := 0.0, 0.0
			for i := -n; i <= n; i++ {
				k := math.Exp(-float64(i*i) / (2 * sigma * sigma))
				num += k * float64(src[min(max(x+i, 0), w-1)])
				den += k
			}
			got := float64(s.Pix[w+x])
			sampled = max(sampled, math.Abs(got-num/den))
			integral = max(integral, math.Abs(got-255*0.5*(1+math.Erf((float64(x)+0.5-edge)/(sigma*math.Sqrt2)))))
		}
		if sampled > 0.6 {
			t.Errorf("sigma %v: blurred step off the sampled Gaussian by %.2f levels, want at most 0.6", sigma, sampled)
		}
		if sigma >= 2 && integral > 1 {
			t.Errorf("sigma %v: blurred step off the error function by %.2f levels, want at most 1", sigma, integral)
		}
	}
}

func TestBlur_PreservesMass(t *testing.T) {
	s := filled(t, 64, 64, typeset.Identity, Path{Ops: ellipse(32, 32, 10, 7)})
	before := 0
	for _, v := range s.Pix {
		before += int(v)
	}
	if err := Blur(context.Background(), s, GaussianKernel(3), GaussianKernel(3), EdgeNone); err != nil {
		t.Fatal(err)
	}
	after := 0
	for _, v := range s.Pix {
		after += int(v)
	}
	if d := math.Abs(float64(after-before)) / 255; d > 3 {
		t.Errorf("blur moved %.1f pixels of coverage, want at most 3", d)
	}
}

func TestBlur_Edges(t *testing.T) {
	row := func(edge Edge) []uint8 {
		s := newSurface(t, Coverage, 4, 1)
		copy(s.Pix, []uint8{255, 0, 0, 255})
		if err := Blur(context.Background(), s, BoxKernel(3, 1), Kernel{}, edge); err != nil {
			t.Fatal(err)
		}
		return s.Pix
	}
	for _, tc := range []struct {
		edge Edge
		want []uint8
	}{
		{EdgeNone, []uint8{85, 85, 85, 85}},
		{EdgeDuplicate, []uint8{170, 85, 85, 170}},
		{EdgeWrap, []uint8{170, 85, 85, 170}},
		{EdgeMirror, []uint8{170, 85, 85, 170}},
	} {
		if got := row(tc.edge); !slices.Equal(got, tc.want) {
			t.Errorf("edge %d: %v, want %v", tc.edge, got, tc.want)
		}
	}
}

// TestBlur_EdgeMirror locks the mirror edge of Filter Effects 1, 9.14: a
// box of 7 at the start of the row 0, 0, 255, 255, 0 reads 255, 0, 0
// past the edge, the row's first three pixels mirrored, where duplicate
// reads 0, 0, 0 and wrap 255, 255, 0, its last three.
func TestBlur_EdgeMirror(t *testing.T) {
	for _, tc := range []struct {
		edge Edge
		want uint8
	}{
		{EdgeMirror, 109},
		{EdgeDuplicate, 73},
		{EdgeWrap, 146},
	} {
		s := newSurface(t, Coverage, 5, 1)
		copy(s.Pix, []uint8{0, 0, 255, 255, 0})
		if err := Blur(context.Background(), s, BoxKernel(7, 1), Kernel{}, tc.edge); err != nil {
			t.Fatal(err)
		}
		if s.Pix[0] != tc.want {
			t.Errorf("edge %d: first pixel %d, want %d", tc.edge, s.Pix[0], tc.want)
		}
	}
}

func TestBlur_ColorChannelsApart(t *testing.T) {
	s := newSurface(t, RGBA, 3, 1)
	copy(s.Pix, []uint8{0, 0, 0, 0, 255, 51, 0, 255, 0, 0, 0, 0})
	if err := Blur(context.Background(), s, BoxKernel(3, 1), Kernel{}, EdgeNone); err != nil {
		t.Fatal(err)
	}
	want := []uint8{85, 17, 0, 85, 85, 17, 0, 85, 85, 17, 0, 85}
	if !slices.Equal(s.Pix, want) {
		t.Errorf("blurred RGBA = %v, want %v", s.Pix, want)
	}
}

// TestBlur_ReproducesEngineFalloffs blurs a sharp edge by each engine's
// rule and compares the row with the soft mask that engine printed,
// the edge's position fitted to 1/16 pixel, the deviation fixed by the
// rule: Microsoft Office's Gaussian at a third of a shadow's, blur's or
// soft edge's radius and a sixth of a glow's, LibreOffice's tent of the
// radius, Chromium's three boxes of Filter Effects 1's width. Each lands
// within 0.75 levels, root mean square, of the printed row; Chromium's
// on it exactly.
//
// # Corpus evidence
//
//   - litho/pptx effects-shadow: the 4 pt blurred shadow at 100 pixels an
//     inch, rms 0.32, and the 20 pt one at 50, 0.32.
//   - litho/pptx effects-glow-softedge-reflection: the 8 pt glow of a
//     rectangle, 0.50; the 5, 15 and 25 pt soft edges, 0.35, 0.35 and
//     0.49; all at 100 pixels an inch.
//   - litho/docx shapes-effects-3d: Word's 11 pt glow, 5 pt soft edge and
//     6 pt shadow at 288 pixels an inch, 0.29, 0.53 and 0.31.
//   - libreoffice/odp sd/shadow-blur.odp: Impress's 0.388 cm shadow blur
//     at 2.932 pixels a point, an inverted mask, 0.71 for the tent.
//   - probe, html: Chromium's filter: blur(5px) at 300 pixels an inch,
//     three boxes of 29 pixels: every pixel of the row equal.
func TestBlur_ReproducesEngineFalloffs(t *testing.T) {
	ppt, word := 100.0/72, 288.0/72
	for _, tc := range []struct {
		name string
		row  []uint8
		k    Kernel
	}{
		{"powerpoint shadow 4 pt", []uint8{0, 0, 0, 3, 9, 21, 40, 62, 81, 93, 99, 102, 102, 102, 102}, GaussianKernel(4.0 / 3 * ppt)},
		{"powerpoint shadow 20 pt at 50 ppi", []uint8{0, 0, 1, 1, 2, 4, 7, 11, 16, 23, 31, 41, 53, 65, 79, 92, 104, 115, 125, 133, 139, 144, 147, 149, 151, 152, 152, 153, 153, 153, 153, 153}, GaussianKernel(20.0 / 3 * 50 / 72)},
		{"powerpoint glow 8 pt", []uint8{0, 0, 0, 1, 7, 22, 52, 100, 155, 203, 233, 248, 254, 255, 255, 255}, GaussianKernel(8.0 / 6 * ppt)},
		{"powerpoint soft edge 5 pt", []uint8{0, 2, 6, 16, 35, 65, 105, 150, 190, 220, 239, 249, 253, 255, 255, 255}, GaussianKernel(5.0 / 3 * ppt)},
		{"powerpoint soft edge 15 pt", []uint8{0, 1, 2, 3, 4, 6, 9, 12, 16, 21, 28, 35, 44, 54, 66, 78, 91, 106, 120, 135, 149, 164, 177, 189, 201, 211, 220, 227, 234, 239, 243, 246, 249, 251, 252, 253, 254, 254, 255, 255, 255, 255}, GaussianKernel(15.0 / 3 * ppt)},
		{"powerpoint soft edge 25 pt", []uint8{0, 1, 1, 1, 1, 2, 3, 4, 5, 6, 7, 8, 10, 11, 14, 17, 20, 23, 27, 31, 36, 41, 46, 52, 59, 66, 73, 81, 89, 97, 106, 115, 123, 132, 141, 150, 158, 166, 174, 182, 190, 197, 203, 209, 215, 220, 224, 228, 232, 235, 239, 242, 244, 245, 247, 248, 250, 251, 252, 253, 253, 254, 254, 254, 254, 255, 255, 255, 255, 255}, GaussianKernel(25.0 / 3 * ppt)},
		{"word glow 11 pt", []uint8{0, 0, 0, 0, 0, 0, 0, 1, 1, 2, 2, 3, 4, 6, 8, 10, 12, 16, 19, 23, 28, 32, 37, 43, 48, 54, 59, 65, 70, 74, 79, 83, 86, 90, 92, 94, 96, 98, 99, 100, 100, 101, 101, 102, 102, 102, 102, 102, 102, 102}, GaussianKernel(11.0 / 6 * word)},
		{"word soft edge 5 pt", []uint8{0, 1, 1, 2, 3, 5, 7, 10, 14, 19, 26, 33, 42, 52, 64, 76, 90, 105, 120, 135, 150, 165, 179, 191, 203, 213, 222, 229, 236, 241, 245, 248, 250, 252, 253, 254, 254, 255, 255, 255, 255, 255}, GaussianKernel(5.0 / 3 * word)},
		{"word shadow 6 pt", []uint8{0, 0, 0, 1, 1, 1, 1, 2, 3, 3, 5, 6, 8, 10, 12, 15, 18, 22, 27, 32, 37, 42, 48, 54, 61, 67, 74, 80, 86, 91, 96, 101, 106, 110, 113, 116, 118, 120, 122, 123, 125, 125, 126, 127, 127, 127, 127, 128, 128, 128, 128, 128, 128}, GaussianKernel(6.0 / 3 * word)},
		{"impress shadow 0.388 cm", []uint8{0, 1, 1, 2, 3, 4, 5, 7, 9, 11, 13, 15, 18, 21, 24, 28, 32, 36, 40, 44, 49, 54, 59, 64, 70, 76, 82, 88, 94, 101, 108, 115, 123, 131, 138, 145, 153, 159, 166, 172, 179, 184, 190, 195, 201, 206, 211, 215, 219, 223, 227, 231, 234, 237, 240, 243, 245, 247, 249, 251, 252, 253, 254, 255, 255, 255}, TentKernel(0.388 / 2.54 * 72 * 504 / 171.921)},
		{"chromium blur 5px", []uint8{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 2, 3, 3, 4, 5, 6, 8, 9, 11, 13, 15, 17, 20, 23, 26, 29, 32, 36, 40, 45, 49, 54, 60, 65, 71, 77, 83, 89, 95, 101, 108, 114, 121, 128, 134, 141, 147, 154, 160, 166, 172, 178, 184, 190, 195, 201, 206, 210, 215, 219, 223, 226, 229, 232, 235, 238, 240, 242, 244, 246, 247, 249, 250, 251, 252, 252, 253, 254, 254, 254, 255, 255, 255, 255}, BoxKernel(29, 3)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			best, at := fitEdge(t, tc.row, tc.k)
			t.Logf("rms %.2f levels, edge at %.4f pixels", best, at)
			if best > 0.75 {
				t.Errorf("blurred edge off the engine's row by %.2f levels rms, want at most 0.75", best)
			}
		})
	}
}

// fitEdge blurs by k a sharp edge rising at x0 to the row's plateau, for
// every x0 across the row in steps of 1/16 pixel, and returns the least
// root mean square difference with row and its x0.
func fitEdge(t *testing.T, row []uint8, k Kernel) (float64, float64) {
	t.Helper()
	top := float64(slices.Max(row))
	best, at := math.Inf(1), 0.0
	for i := range 16 * len(row) {
		x0 := float64(i) / 16
		s := filled(t, len(row), 1, typeset.Identity, Path{Ops: rect(x0, -1, 1e4, 2)})
		if err := Blur(context.Background(), s, k, Kernel{}, EdgeDuplicate); err != nil {
			t.Fatal(err)
		}
		e := 0.0
		for x, v := range row {
			d := float64(s.Pix[x])*top/255 - float64(v)
			e += d * d
		}
		if e = math.Sqrt(e / float64(len(row))); e < best {
			best, at = e, x0
		}
		s.Release()
	}
	return best, at
}

// TestBlur_BoxesSumAsTheirKernel blurs surfaces of random pixels with box
// kernels, by the running sums of their boxes, and with the same weights
// convolved directly: every byte is equal, at every width, pass count,
// channel count and edge.
func TestBlur_BoxesSumAsTheirKernel(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for _, f := range []Format{Coverage, RGBA} {
		for _, edge := range []Edge{EdgeNone, EdgeDuplicate, EdgeWrap, EdgeMirror} {
			for _, tc := range []struct{ wx, px, wy, py int }{
				{2, 1, 2, 1}, {2, 3, 3, 3}, {3, 2, 0, 0}, {0, 0, 4, 3},
				{7, 3, 4, 2}, {29, 3, 29, 3}, {64, 3, 5, 1},
			} {
				s := newSurface(t, f, 37, 23)
				for i := range s.Pix {
					s.Pix[i] = uint8(rng.UintN(256))
				}
				want := newSurface(t, f, s.W, s.H)
				copy(want.Pix, s.Pix)
				kx, ky := BoxKernel(tc.wx, tc.px), BoxKernel(tc.wy, tc.py)
				if err := Blur(context.Background(), s, kx, ky, edge); err != nil {
					t.Fatal(err)
				}
				if err := Blur(context.Background(), want, Kernel{W: kx.W}, Kernel{W: ky.W}, edge); err != nil {
					t.Fatal(err)
				}
				if !slices.Equal(s.Pix, want.Pix) {
					t.Errorf("format %d, edge %d, boxes %+v: the running sums differ from the convolution", f, edge, tc)
				}
			}
		}
	}
}
