package writer

import (
	"bytes"
	"compress/zlib"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/limits"
)

// paletteColor is colour c of [paletteRGB], distinct for every c below 256.
func paletteColor(c int) [3]byte {
	return [3]byte{byte(c), byte(255 - c), byte(c * 7)}
}

// paletteRGB returns w x h RGB samples scattered over n colours, each used at
// least once when there are n pixels: noise an Indexed space stores in a
// fraction of the bytes, where bands would compress as well either way.
func paletteRGB(w, h, n int) []byte {
	out := make([]byte, 0, w*h*3)
	x := uint32(2463534242)
	for i := range w * h {
		x ^= x << 13
		x ^= x >> 17
		x ^= x << 5
		c := int(x % uint32(n))
		if i < n {
			c = i
		}
		col := paletteColor(c)
		out = append(out, col[:]...)
	}
	return out
}

// expandIndexed maps an Indexed image's decoded indices back to RGB samples
// through its own /ColorSpace, as a viewer does, failing when the dictionary
// is not an Indexed image over three components.
func expandIndexed(t *testing.T, dict ast.PdfDict, indices []byte, w, h int) []byte {
	t.Helper()
	cs, ok := dict["ColorSpace"].(ast.PdfArray)
	if !ok || len(cs) != 4 || cs[0] != ast.PdfName("Indexed") {
		t.Fatalf("/ColorSpace = %v, want an Indexed space", dict["ColorSpace"])
	}
	hival, _ := cs[2].(ast.PdfInt)
	lookup, _ := cs[3].(ast.PdfString)
	if len(lookup) != 3*(int(hival)+1) {
		t.Fatalf("lookup holds %d bytes for hival %d", len(lookup), hival)
	}
	bits, _ := dict["BitsPerComponent"].(ast.PdfInt)
	p := indexedPalette{lookup: []byte(lookup), bits: int(bits), width: w}
	return p.expand(indices, h)
}

// TestRecompressImagesConvertsFewColoursToIndexed is the contract: an RGB
// image of few colours becomes an Indexed image whose pixels are the
// source's, with the narrowest indices that number its colours.
func TestRecompressImagesConvertsFewColoursToIndexed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cases := []struct {
		colors, bits int
	}{{2, 1}, {3, 2}, {4, 2}, {16, 4}, {17, 8}, {256, 8}}
	for _, tc := range cases {
		t.Run("", func(t *testing.T) {
			t.Parallel()
			const w, h = 300, 40
			samples := paletteRGB(w, h, tc.colors)
			alloc := NewObjAllocator(0)
			ref := alloc.Alloc(ast.NewStream(imageDict(w, h, nil), samples))
			stats, err := RecompressImages(ctx, alloc, ImageRecompression{})
			if err != nil {
				t.Fatal(err)
			}
			if stats.Images != 1 {
				t.Fatalf("stats = %+v", stats)
			}
			dict := streamAt(t, alloc, ref.ObjectNum).Dict
			if dict["BitsPerComponent"] != ast.PdfInt(tc.bits) {
				t.Fatalf("/BitsPerComponent = %v, want %d", dict["BitsPerComponent"], tc.bits)
			}
			cs := dict["ColorSpace"].(ast.PdfArray)
			if cs[1] != ast.PdfName("DeviceRGB") || cs[2] != ast.PdfInt(tc.colors-1) {
				t.Fatalf("/ColorSpace = %v", cs)
			}
			if got := expandIndexed(t, dict, decodedSamples(t, alloc, ref.ObjectNum), w, h); !bytes.Equal(got, samples) {
				t.Fatal("the Indexed image's pixels differ from the source's")
			}
		})
	}
}

// TestRecompressImagesIndexedNeverLosesToSamples checks that weighing an
// Indexed form never makes an image larger than its own samples re-encoded
// would be. Bands of few colours compress about as well either way, and
// zlib's default level once ranked the Indexed form first where its best
// level did not.
func TestRecompressImagesIndexedNeverLosesToSamples(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, n := range []int{3, 17, 60} {
		const w, h = 300, 40
		samples := make([]byte, 0, w*h*3)
		for range h {
			for x := range w {
				col := paletteColor(x * n / w)
				samples = append(samples, col[:]...)
			}
		}
		dict := imageDict(w, h, nil)
		plain := int64(-1)
		for _, l := range []*predictorLayout{nil, {colors: 3, bpc: 8, columns: w}} {
			enc, d, ok := encodeCandidate(samples, dict, l, zlib.BestCompression)
			if !ok {
				t.Fatal("encoding the samples failed")
			}
			size, err := serializedSize(ctx, ast.NewStream(d, enc))
			if err != nil {
				t.Fatal(err)
			}
			if plain < 0 || size < plain {
				plain = size
			}
		}
		alloc := NewObjAllocator(0)
		ref := alloc.Alloc(ast.NewStream(dict, samples))
		if _, err := RecompressImages(ctx, alloc, ImageRecompression{}); err != nil {
			t.Fatal(err)
		}
		got, err := serializedSize(ctx, streamAt(t, alloc, ref.ObjectNum))
		if err != nil {
			t.Fatal(err)
		}
		if got > plain {
			t.Fatalf("%d colours: %d bytes, where the samples re-encoded take %d", n, got, plain)
		}
	}
}

// TestRecompressImagesIndexedKeepsICCBase checks an ICCBased RGB image keeps
// its colour space, by reference, as the Indexed base, and an identity
// /Decode goes.
func TestRecompressImagesIndexedKeepsICCBase(t *testing.T) {
	t.Parallel()
	alloc := NewObjAllocator(0)
	icc := alloc.Alloc(ast.NewStream(ast.PdfDict{"N": ast.PdfInt(3)}, []byte("profile")))
	cs := alloc.Alloc(ast.PdfArray{ast.PdfName("ICCBased"), icc})
	samples := paletteRGB(200, 30, 5)
	ref := alloc.Alloc(ast.NewStream(imageDict(200, 30, ast.PdfDict{
		"ColorSpace": cs,
		"Decode":     ast.PdfArray{ast.PdfInt(0), ast.PdfReal(1), ast.PdfInt(0), ast.PdfInt(1), ast.PdfInt(0), ast.PdfInt(1)},
		"SMask":      alloc.Alloc(ast.NewStream(ast.PdfDict{"Subtype": ast.PdfName("Image")}, nil)),
	}), samples))
	if _, err := RecompressImages(context.Background(), alloc, ImageRecompression{}); err != nil {
		t.Fatal(err)
	}
	dict := streamAt(t, alloc, ref.ObjectNum).Dict
	got, ok := dict["ColorSpace"].(ast.PdfArray)
	if !ok || got[1] != cs {
		t.Fatalf("/ColorSpace = %v, want an Indexed space over %v", dict["ColorSpace"], cs)
	}
	if _, present := dict["Decode"]; present {
		t.Fatal("the identity /Decode was kept, which an Indexed image reads as index ranges")
	}
	if alloc.Pending()[cs.ObjectNum].(ast.PdfArray)[0] != ast.PdfName("ICCBased") {
		t.Fatal("the shared colour space changed")
	}
	if !bytes.Equal(expandIndexed(t, dict, decodedSamples(t, alloc, ref.ObjectNum), 200, 30), samples) {
		t.Fatal("the Indexed image's pixels differ from the source's")
	}
}

// TestRecompressImagesIndexedTranslatesColorKey checks a colour-key /Mask is
// rewritten as the index range of the colours it masked, and dropped when it
// masked none.
func TestRecompressImagesIndexedTranslatesColorKey(t *testing.T) {
	t.Parallel()
	const w, h = 240, 20
	samples := paletteRGB(w, h, 6)
	// Colours 2 and 3 are (2,253,14) and (3,252,21): a key on red 2..5 and
	// blue 0..21 masks both and nothing else, since colours 4 and 5 have a
	// bluer blue and 0 and 1 a lower red.
	cases := []struct {
		name string
		key  ast.PdfArray
		want ast.PdfObject
	}{
		{"masks two colours", ast.PdfArray{ast.PdfInt(2), ast.PdfInt(5), ast.PdfInt(0), ast.PdfInt(255), ast.PdfInt(0), ast.PdfInt(21)},
			ast.PdfArray{ast.PdfInt(0), ast.PdfInt(1)}},
		{"masks nothing", ast.PdfArray{ast.PdfInt(100), ast.PdfInt(200), ast.PdfInt(0), ast.PdfInt(255), ast.PdfInt(0), ast.PdfInt(255)}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			alloc := NewObjAllocator(0)
			ref := alloc.Alloc(ast.NewStream(imageDict(w, h, ast.PdfDict{"Mask": tc.key}), samples))
			if _, err := RecompressImages(context.Background(), alloc, ImageRecompression{}); err != nil {
				t.Fatal(err)
			}
			dict := streamAt(t, alloc, ref.ObjectNum).Dict
			indices := decodedSamples(t, alloc, ref.ObjectNum)
			got := expandIndexed(t, dict, indices, w, h)
			if !bytes.Equal(got, samples) {
				t.Fatal("the Indexed image's pixels differ from the source's")
			}
			mask, present := dict["Mask"]
			if tc.want == nil {
				if present {
					t.Fatalf("/Mask = %v, want it gone", mask)
				}
				return
			}
			if arr, ok := mask.(ast.PdfArray); !ok || len(arr) != 2 || arr[0] != ast.PdfInt(0) || arr[1] != ast.PdfInt(1) {
				t.Fatalf("/Mask = %v, want %v", mask, tc.want)
			}
			// The indices the key now names are exactly the masked colours.
			lookup := []byte(dict["ColorSpace"].(ast.PdfArray)[3].(ast.PdfString))
			for i := 0; i < len(lookup); i += 3 {
				inKey := lookup[i] >= 2 && lookup[i] <= 5 && lookup[i+2] <= 21
				if inKey != (i/3 <= 1) {
					t.Fatalf("index %d (%v) masked=%v", i/3, lookup[i:i+3], i/3 <= 1)
				}
			}
		})
	}
}

// TestRecompressImagesIndexedLeavesAlone covers the images that keep their
// colour space: each would read differently, or no better, as Indexed.
func TestRecompressImagesIndexedLeavesAlone(t *testing.T) {
	t.Parallel()
	const w, h = 200, 30
	few := paletteRGB(w, h, 4)
	alloc := func() *ObjAllocator { return NewObjAllocator(0) }
	cases := []struct {
		name string
		dict func(a *ObjAllocator) ast.PdfDict
		data []byte
	}{
		{"many colours", func(*ObjAllocator) ast.PdfDict { return imageDict(64, 48, nil) }, gradientRGB(64, 48)},
		{"inverted decode", func(*ObjAllocator) ast.PdfDict {
			return imageDict(w, h, ast.PdfDict{"Decode": ast.PdfArray{ast.PdfInt(1), ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(1), ast.PdfInt(0), ast.PdfInt(1)}})
		}, few},
		{"interpolated", func(*ObjAllocator) ast.PdfDict { return imageDict(w, h, ast.PdfDict{"Interpolate": ast.PdfBool(true)}) }, few},
		{"matte", func(a *ObjAllocator) ast.PdfDict {
			sm := a.Alloc(ast.NewStream(ast.PdfDict{"Matte": ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(0)}}, nil))
			return imageDict(w, h, ast.PdfDict{"SMask": sm})
		}, few},
		{"gray", func(*ObjAllocator) ast.PdfDict {
			return imageDict(w*3, h, ast.PdfDict{"ColorSpace": ast.PdfName("DeviceGray")})
		}, few},
		{"16-bit", func(*ObjAllocator) ast.PdfDict {
			return imageDict(w/2, h, ast.PdfDict{"BitsPerComponent": ast.PdfInt(16)})
		}, few[:w/2*h*6]},
		{"lab", func(*ObjAllocator) ast.PdfDict {
			return imageDict(w, h, ast.PdfDict{"ColorSpace": ast.PdfArray{ast.PdfName("Lab"), ast.PdfDict{"WhitePoint": ast.PdfArray{ast.PdfInt(1), ast.PdfInt(1), ast.PdfInt(1)}}}})
		}, few},
		{"icc with a range", func(a *ObjAllocator) ast.PdfDict {
			icc := a.Alloc(ast.NewStream(ast.PdfDict{"N": ast.PdfInt(3), "Range": ast.PdfArray{ast.PdfInt(0), ast.PdfInt(2), ast.PdfInt(0), ast.PdfInt(1), ast.PdfInt(0), ast.PdfInt(1)}}, nil))
			return imageDict(w, h, ast.PdfDict{"ColorSpace": ast.PdfArray{ast.PdfName("ICCBased"), icc}})
		}, few},
		{"trailing data", func(*ObjAllocator) ast.PdfDict { return imageDict(w, h-1, nil) }, few},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := alloc()
			ref := a.Alloc(ast.NewStream(tc.dict(a), tc.data))
			if _, err := RecompressImages(context.Background(), a, ImageRecompression{}); err != nil {
				t.Fatal(err)
			}
			if cs, ok := streamAt(t, a, ref.ObjectNum).Dict["ColorSpace"].(ast.PdfArray); ok && cs[0] == ast.PdfName("Indexed") {
				t.Fatalf("converted to %v", cs)
			}
		})
	}
}

// TestRecompressImagesLazyIndexedMatchesRetained checks an Indexed
// re-encoding redone at write time serializes to the bytes it would have had
// in memory.
func TestRecompressImagesLazyIndexedMatchesRetained(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	samples := paletteRGB(300, 40, 9)
	build := func() (*ObjAllocator, int) {
		alloc := NewObjAllocator(0)
		ref := alloc.Alloc(ast.NewStream(imageDict(300, 40, nil), samples))
		return alloc, ref.ObjectNum
	}
	retained, num := build()
	if _, err := RecompressImages(ctx, retained, ImageRecompression{}); err != nil {
		t.Fatal(err)
	}
	lazy, _ := build()
	if _, err := RecompressImages(ctx, lazy, ImageRecompression{Limits: limits.Limits{MaxStreamBytes: 1}}); err != nil {
		t.Fatal(err)
	}
	var a, b bytes.Buffer
	if _, err := Serialize(ctx, &a, streamAt(t, retained, num)); err != nil {
		t.Fatal(err)
	}
	if _, err := Serialize(ctx, &b, streamAt(t, lazy, num)); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(a.Bytes(), []byte("/Indexed")) || !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Fatal("the lazy Indexed re-encoding differs from the retained one")
	}
}

// TestIndexedPaletteRoundTrip checks indices and expand invert each other at
// every index width, rows padded to a whole byte.
func TestIndexedPaletteRoundTrip(t *testing.T) {
	t.Parallel()
	for _, n := range []int{1, 2, 4, 16, 200} {
		const w, h = 13, 3
		samples := paletteRGB(w, h, n)
		colors := distinctColors(samples)
		lookup := make([]byte, 0, len(colors)*3)
		for _, c := range colors {
			lookup = append(lookup, c[:]...)
		}
		p := indexedPalette{lookup: lookup, bits: indexBits(len(colors)), width: w}
		idx, ok := p.indices(samples)
		if !ok || len(idx) != h*((w*p.bits+7)/8) {
			t.Fatalf("n=%d: indices failed or have the wrong length", n)
		}
		if !bytes.Equal(p.expand(idx, h), samples) {
			t.Fatalf("n=%d: expand does not invert indices", n)
		}
	}
	if distinctColors(gradientRGB(64, 48)) != nil {
		t.Fatal("distinctColors counted past 256")
	}
}
