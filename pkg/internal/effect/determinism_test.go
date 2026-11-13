package effect

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// pinnedHashes holds the SHA-256 of each primitive's output on the fixed
// inputs of [TestDeterminism_PinnedHashes]. Every architecture must give
// these bytes: a change here is a change of output, on purpose or not.
var pinnedHashes = map[string]string{
	"arithmetic":           "2f32aa169783b45176a62c941b44e8dbfe621cec19eb2f21395b6eef55ea015e",
	"blend":                "3692ed613dd13137233f8566c79da85e25249f2100184f71a4065fbd428acd3b",
	"blur box colour wrap": "ce5bbda63d758f009575ea3ac5ba104cf92b74bfdf28cd2e89b7332995079b71",
	"blur gaussian":        "6bca4d9ea57e6c879b2f34ffee072f1a4b863d3aecedb35b1955090e40f00939",
	"blur tent duplicate":  "24742cee0c5385f2add8b20b532c89b1d0e3a1108f1f42d2b99f1387e2281ad8",
	"color matrices":       "721b5dd0e279b8d47b0a11cebf6facb2718e5dc3ddd2dd247daa26b0df468b78",
	"composite":            "aceb8b17d587694466f0e6c64ccdda0e03aef90ed50516ec3406109d44f8a873",
	"dilate erode":         "9ab04f53d53c22c0052bd37be427d49c7688a1fbbc2ad53737ca726966bfe7e3",
	"fill":                 "658cc6b892ac43b705a39677f2d06679b48ac39aa829e2fd53099013e118d16f",
	"gaussian kernels":     "9542430123aede09660a25086c9d8c397d46082c10a3f809d35f5e8e47055868",
	"luminance to alpha":   "d6cb35da8705798e2a9a613ec3b58a80c61b0fc7efd12f394d9c28b059637c62",
	"offset":               "1676e9e044ee77a0104dbb6cde34a887c08dc145493cf8e4c74f56088373b59d",
	"plan":                 "5948c337b35dd18743191ce3d377e51422848ef24a0bb5347b8e75c7ad083075",
	"tile":                 "2efc0b3c7b09a8e28a595ed15f2df2351c445491e1c463842898fa8a293862ab",
	"tint flood ramp":      "da2af97f27556fede774ecacb58fe9070cddaa51d355c4b05fc44413d030eb7b",
	"transfer":             "e02a038b3112ba018cee1bc220b770c9987dbe54237b7eb32ccfb41e35052e10",
	"transform":            "cf413c4cb1ed7ba5535487a8fd4953698642e85ab979b77d9300de2815f95f14",
}

// detCoverage returns a 97 by 61 coverage surface holding filled,
// even-odd and stroked shapes at sub-pixel positions under a rotation.
func detCoverage(t *testing.T) *Surface {
	t.Helper()
	tr := typeset.Matrix{A: 0.93, B: 0.21, C: -0.17, D: 1.04, E: 3.3, F: -2.1}
	return filled(t, 97, 61, tr,
		Path{Ops: star(40.3, 30.7, 26.1)},
		Path{Ops: append(ellipse(70.2, 25.5, 16.3, 11.9), ellipse(70.2, 25.5, 7.1, 5.3)...), Rule: EvenOdd},
		Path{Ops: ellipse(28.4, 33.3, 19.7, 14.2), Width: 2.35},
		Path{Ops: []typeset.PathOp{{Kind: typeset.MoveTo, P: pt3(5.5, 50.2)}, {Kind: typeset.LineTo, P: pt3(48.1, 41.7)}, {Kind: typeset.LineTo, P: pt3(90.6, 55.9)}}, Width: 4.1},
	)
}

// detColor returns a 61 by 47 RGBA surface of premultiplied pixels from
// a fixed sequence.
func detColor(t *testing.T, seed int) *Surface {
	t.Helper()
	pix := pixels(61 * 47 * (seed + 1))
	return surfaceOf2D(t, pix[len(pix)-61*47*4:], 61, 47)
}

func surfaceOf2D(t *testing.T, pix []uint8, w, h int) *Surface {
	t.Helper()
	s := newSurface(t, RGBA, w, h)
	copy(s.Pix, pix)
	return s
}

// TestDeterminism_PinnedHashes runs every primitive on fixed inputs and
// compares the SHA-256 of its output with the hash pinned for it, the
// same on every architecture: the coverage, the kernels and the
// fixed-point arithmetic leave nothing to the machine.
func TestDeterminism_PinnedHashes(t *testing.T) {
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	cases := map[string]func() []byte{
		"fill": func() []byte { return detCoverage(t).Pix },
		"gaussian kernels": func() []byte {
			var out []byte
			for sigma := 0.05; sigma < 60; sigma *= 1.17 {
				for _, w := range GaussianKernel(sigma).W {
					out = binary.LittleEndian.AppendUint32(out, w)
				}
			}
			return out
		},
		"blur gaussian": func() []byte {
			s := detCoverage(t)
			must(Blur(ctx, s, GaussianKernel(2.71), GaussianKernel(1.37), EdgeNone))
			return s.Pix
		},
		"blur tent duplicate": func() []byte {
			s := detCoverage(t)
			must(Blur(ctx, s, TentKernel(3.3), TentKernel(5.05), EdgeDuplicate))
			return s.Pix
		},
		"blur box colour wrap": func() []byte {
			s := detColor(t, 0)
			must(Blur(ctx, s, BoxKernel(7, 3), BoxKernel(4, 3), EdgeWrap))
			return s.Pix
		},
		"dilate erode": func() []byte {
			s := detCoverage(t)
			must(Dilate(ctx, s, 3, 1))
			must(Erode(ctx, s, 2, 4))
			return s.Pix
		},
		"offset": func() []byte {
			s := detColor(t, 1)
			must(Offset(ctx, s, 1.37, -2.61))
			return s.Pix
		},
		"transform": func() []byte {
			src := detColor(t, 2)
			dst := newSurface(t, RGBA, 70, 52)
			sn, cs := detSincos(23.5)
			must(Transform(ctx, dst, src, typeset.Matrix{A: float64(1.1 * cs), B: float64(1.1 * sn), C: float64(-0.9 * sn), D: float64(0.9 * cs), E: 12.25, F: -3.5}))
			return dst.Pix
		},
		"tile": func() []byte {
			src := detColor(t, 3)
			dst := newSurface(t, RGBA, 80, 33)
			must(Tile(ctx, dst, src, 7, 5, 20, 16))
			return dst.Pix
		},
		"color matrices": func() []byte {
			s := detColor(t, 4)
			must(ColorMatrix(ctx, s, HueRotateMatrix(37.5)))
			must(ColorMatrix(ctx, s, SaturateMatrix(0.43)))
			must(ColorMatrix(ctx, s, SepiaMatrix(0.6)))
			must(ColorMatrix(ctx, s, GrayscaleMatrix(0.3)))
			return s.Pix
		},
		"luminance to alpha": func() []byte {
			s := detColor(t, 5)
			must(ColorMatrix(ctx, s, LuminanceToAlphaMatrix()))
			return s.Pix
		},
		"transfer": func() []byte {
			s := detColor(t, 6)
			r := TransferFunc{Kind: TransferGamma, Amplitude: 1.1, Exponent: 2.2, Offset: -0.05}.Table()
			g := TransferFunc{Kind: TransferTable, Values: []float64{0.1, 0.9, 0.3, 1}}.Table()
			b := TransferFunc{Kind: TransferDiscrete, Values: []float64{0.2, 0.5, 0.7}}.Table()
			a := ContrastTransfer(1.3).Table()
			must(Transfer(ctx, s, &r, &g, &b, &a))
			return s.Pix
		},
		"composite": func() []byte {
			var out []byte
			for op := Over; op <= Xor; op++ {
				dst := detColor(t, 7)
				must(Composite(ctx, dst, detColor(t, 8), op))
				out = append(out, dst.Pix...)
			}
			return out
		},
		"arithmetic": func() []byte {
			dst := detColor(t, 9)
			must(Arithmetic(ctx, dst, detColor(t, 10), 0.37, 0.61, -0.25, 0.07))
			return dst.Pix
		},
		"blend": func() []byte {
			var out []byte
			for mode := Normal; mode <= Luminosity; mode++ {
				dst := detColor(t, 11)
				must(Blend(ctx, dst, detColor(t, 12), mode))
				out = append(out, dst.Pix...)
			}
			return out
		},
		"tint flood ramp": func() []byte {
			cov := detCoverage(t)
			s, err := Tint(ctx, cov, Color{200, 30, 90, 180})
			must(err)
			must(AlphaRamp(ctx, s, 3.3, 60.2, 41.7, 2.9, 0.52, 0.03))
			f := newSurface(t, RGBA, 5, 5)
			must(Flood(ctx, f, Color{13, 250, 77, 99}))
			return append(s.Pix, f.Pix...)
		},
		"plan": func() []byte {
			var out []byte
			for sigma := 0.0; sigma < 40; sigma += 0.37 {
				r, _ := Plan(sigma, 123.4+float64(sigma*7), 77.7)
				out = binary.LittleEndian.AppendUint64(out, math.Float64bits(r.Scale))
				out = binary.LittleEndian.AppendUint32(out, uint32(r.W))
				out = binary.LittleEndian.AppendUint32(out, uint32(r.H))
			}
			return out
		},
	}
	for name, f := range cases {
		sum := sha256.Sum256(f())
		got := hex.EncodeToString(sum[:])
		if want := pinnedHashes[name]; got != want {
			t.Errorf("%q: sha256 %s, pinned %s", name, got, want)
		}
	}
}
