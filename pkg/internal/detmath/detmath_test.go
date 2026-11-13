package detmath

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"math"
	"testing"
)

// functions names each function of the package, as one of two arguments.
var functions = []struct {
	name string
	det  func(x, y float64) float64
	std  func(x, y float64) float64
	// ulps bounds the distance to the math package on any machine: the
	// math package itself differs by up to this much between machines.
	ulps int64
	dom  func(x, y float64) (float64, float64)
}{
	{"Sin", func(x, _ float64) float64 { return Sin(x) }, func(x, _ float64) float64 { return math.Sin(x) }, 2, nil},
	{"Cos", func(x, _ float64) float64 { return Cos(x) }, func(x, _ float64) float64 { return math.Cos(x) }, 2, nil},
	{"Sincos sine", func(x, _ float64) float64 { s, _ := Sincos(x); return s }, func(x, _ float64) float64 { s, _ := math.Sincos(x); return s }, 2, nil},
	{"Sincos cosine", func(x, _ float64) float64 { _, c := Sincos(x); return c }, func(x, _ float64) float64 { _, c := math.Sincos(x); return c }, 2, nil},
	{"Tan", func(x, _ float64) float64 { return Tan(x) }, func(x, _ float64) float64 { return math.Tan(x) }, 4, nil},
	{"Atan", func(x, _ float64) float64 { return Atan(x) }, func(x, _ float64) float64 { return math.Atan(x) }, 2, nil},
	{"Atan2", Atan2, math.Atan2, 2, nil},
	{"Asin", func(x, _ float64) float64 { return Asin(x) }, func(x, _ float64) float64 { return math.Asin(x) }, 64, unit},
	{"Acos", func(x, _ float64) float64 { return Acos(x) }, func(x, _ float64) float64 { return math.Acos(x) }, 1 << 14, unit},
	{"Hypot", Hypot, math.Hypot, 2, nil},
	{"Exp", func(x, _ float64) float64 { return Exp(x) }, func(x, _ float64) float64 { return math.Exp(x) }, 2, func(x, y float64) (float64, float64) { return math.Mod(x, 50), y }},
	{"Log", func(x, _ float64) float64 { return Log(x) }, func(x, _ float64) float64 { return math.Log(x) }, 2, positive},
	{"Log10", func(x, _ float64) float64 { return Log10(x) }, func(x, _ float64) float64 { return math.Log10(x) }, 2, positive},
	{"Pow", Pow, math.Pow, 8, func(x, y float64) (float64, float64) { return math.Abs(x), math.Mod(y, 3) }},
	{"Cbrt", func(x, _ float64) float64 { return Cbrt(x) }, func(x, _ float64) float64 { return math.Cbrt(x) }, 1, nil},
	{"Erf", func(x, _ float64) float64 { return Erf(x) }, func(x, _ float64) float64 { return math.Erf(x) }, 2, func(x, y float64) (float64, float64) { return x / 32, y }},
}

func unit(x, y float64) (float64, float64)     { return math.Mod(x, 1), y }
func positive(x, y float64) (float64, float64) { return math.Abs(x) + 1e-9, y }

// inputs calls f with n pseudo-random arguments: a quarter of them within
// a few units, the rest up to about 2^19.
func inputs(n int, f func(x, y float64)) {
	var s uint64 = 88172645463325252
	next := func() int64 {
		s ^= s << 13
		s ^= s >> 7
		s ^= s << 17
		return int64(s>>11) - (1 << 52)
	}
	for i := range n {
		x := float64(next()) / (1 << 44)
		y := float64(next()) / (1 << 46)
		if i%4 == 0 {
			x, y = x/256, y/64
		}
		f(x, y)
	}
}

// pinnedHashes holds the SHA-256 of each function's results over the
// inputs of [TestFunctions_SameBitsOnEveryMachine]. Every machine must give
// these bits: they are the math package's on amd64 without AVX.
var pinnedHashes = map[string]string{
	"Sin":           "1986e1db1605bf96410f084566f2058851bc16d5cb34094165093a67a8f60bb7",
	"Cos":           "fb087c52a953a7a73c9270d58cb79a9c17942b3cb55d87bf0582fc6334597c5d",
	"Sincos sine":   "1986e1db1605bf96410f084566f2058851bc16d5cb34094165093a67a8f60bb7",
	"Sincos cosine": "fb087c52a953a7a73c9270d58cb79a9c17942b3cb55d87bf0582fc6334597c5d",
	"Tan":           "34b895ee4e4e81afb9916f46e7c50ea3f648b09cc325ac77531e0687df6073e7",
	"Atan":          "8968e159ba36275587fbb9e010f51655b512fdc0555e6e71c80bb4eb3d73f772",
	"Atan2":         "d67cdeb40b2b0a35ba19f67d7d1831f467e8b8bdc9868e8fbd9437176214ddd5",
	"Asin":          "0997d3527c7fe3ae3d78f3c4eef7daf9f24b78dd26fae22ce792bcd669d4f9d9",
	"Acos":          "4962981dc54dbd142901e9a57669b7b4a29679143511e81f45e064feda831fb2",
	"Hypot":         "bb7029096dfd9d5348f46cb0a0a3a365994592966c025adbb30b68884502a8b1",
	"Exp":           "5c6ae417f8317b44a2498e49233f082ba7dfae55c04cf3359b6a24f733222b01",
	"Log":           "14c14eae95332f35dd10e3f906f9d839b0b2b555d275224dee17d7383bb5cd22",
	"Log10":         "101c228f8f14936b9e246b9739c8a391fa999c465a94631300d734924737e9b8",
	"Pow":           "cb6840618b7e00b6c46e8e5c8042572894175dc295857943be7faca59f2b5a1f",
	"Cbrt":          "791a0be67f1c1976cc59cd24e7a0d886c729a3e7e73cf8150f03176c597c1202",
	"Erf":           "de25079a33a8b57a788ab1b2271e42b253fc05be44f69b7ad211345da4bb6988",
}

// TestFunctions_SameBitsOnEveryMachine hashes each function's results over
// 100,000 inputs and compares the hash with the one pinned for it.
func TestFunctions_SameBitsOnEveryMachine(t *testing.T) {
	t.Parallel()
	for _, f := range functions {
		h := sha256.New()
		var b [8]byte
		inputs(100000, func(x, y float64) {
			if f.dom != nil {
				x, y = f.dom(x, y)
			}
			binary.LittleEndian.PutUint64(b[:], math.Float64bits(f.det(x, y)))
			h.Write(b[:])
		})
		if got := hex.EncodeToString(h.Sum(nil)); got != pinnedHashes[f.name] {
			t.Errorf("%s: results hash to %s, pinned %s", f.name, got, pinnedHashes[f.name])
		}
	}
}

// TestFunctions_CloseToMath bounds each function's distance to the math
// package's, which differs from machine to machine by about as much.
func TestFunctions_CloseToMath(t *testing.T) {
	t.Parallel()
	for _, f := range functions {
		worst := int64(0)
		inputs(100000, func(x, y float64) {
			if f.dom != nil {
				x, y = f.dom(x, y)
			}
			d, s := f.det(x, y), f.std(x, y)
			if math.IsNaN(d) != math.IsNaN(s) {
				t.Fatalf("%s(%g, %g) = %g, math gives %g", f.name, x, y, d, s)
			}
			if math.IsNaN(d) {
				return
			}
			u := int64(math.Float64bits(d)) - int64(math.Float64bits(s)) //nolint:gosec // reinterpreting the bits as an ordered integer
			worst = max(worst, max(u, -u))
		})
		if worst > f.ulps {
			t.Errorf("%s: %d ulps from math at worst, more than %d", f.name, worst, f.ulps)
		}
	}
}

// TestFunctions_SpecialCases checks the special values against math.
func TestFunctions_SpecialCases(t *testing.T) {
	t.Parallel()
	inf, nan := math.Inf(1), math.NaN()
	specials := []float64{0, math.Copysign(0, -1), 1, -1, 0.5, -0.5, 2, inf, -inf, nan, math.SmallestNonzeroFloat64, math.MaxFloat64, 1e300, -1e300, 1 << 30, 710, -746}
	same := func(a, b float64) bool {
		return math.Float64bits(a) == math.Float64bits(b) || math.IsNaN(a) && math.IsNaN(b)
	}
	for _, f := range functions {
		for _, x := range specials {
			for _, y := range specials {
				d, s := f.det(x, y), f.std(x, y)
				if same(d, s) {
					continue
				}
				// Away from the special values, the results may differ by
				// an ulp as the machines' do.
				if math.IsInf(d, 0) || math.IsInf(s, 0) || math.IsNaN(d) || math.IsNaN(s) || d == 0 || s == 0 {
					t.Errorf("%s(%g, %g) = %g, math gives %g", f.name, x, y, d, s)
				}
			}
		}
	}
}

// TestRoundInt32 checks the conversion Exp's reduction makes as the
// CVTSD2SL instruction makes it.
func TestRoundInt32(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		in   float64
		want int32
	}{
		{0.5, 0}, {1.5, 2}, {2.5, 2}, {-0.5, 0}, {-1.5, -2}, {2147483647.4, math.MaxInt32},
		{2147483647.5, math.MinInt32}, {-2147483648.5, -2147483648}, {-2147483649, math.MinInt32}, {math.NaN(), math.MinInt32},
	} {
		if got := roundInt32(tc.in); got != tc.want {
			t.Errorf("roundInt32(%g) = %d, want %d", tc.in, got, tc.want)
		}
	}
}
