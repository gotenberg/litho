package writer

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/hex"
	"image"
	"image/color"
	"image/jpeg"
	"maps"
	"os"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/filter"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/plugins"
)

// gradientRGB returns w x h RGB samples that compress far better under a PNG
// predictor than without one.
func gradientRGB(w, h int) []byte {
	out := make([]byte, 0, w*h*3)
	for y := range h {
		for x := range w {
			out = append(out, byte(x*3+y), byte(x*5), byte(y*7))
		}
	}
	return out
}

func zlibAt(t *testing.T, level int, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw, err := zlib.NewWriterLevel(&buf, level)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := zw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func imageDict(w, h int, extra ast.PdfDict) ast.PdfDict {
	d := ast.PdfDict{
		"Type":             ast.PdfName("XObject"),
		"Subtype":          ast.PdfName("Image"),
		"Width":            ast.PdfInt(w),
		"Height":           ast.PdfInt(h),
		"ColorSpace":       ast.PdfName("DeviceRGB"),
		"BitsPerComponent": ast.PdfInt(8),
	}
	maps.Copy(d, extra)
	return d
}

// decodedSamples decodes the stream at num, failing on any incomplete decode.
func decodedSamples(t *testing.T, alloc *ObjAllocator, num int) []byte {
	t.Helper()
	s, ok := alloc.Pending()[num].(ast.PdfStream)
	if !ok {
		t.Fatalf("object %d is not a stream", num)
	}
	data, report, err := filter.NewDecoder(nil, limits.Default()).RefuseSalvaged().DecodeStreamReport(context.Background(), s)
	if err != nil || report.Incomplete() {
		t.Fatalf("decoding object %d: %v (%+v)", num, err, report)
	}
	return data
}

func streamAt(t *testing.T, alloc *ObjAllocator, num int) ast.PdfStream {
	t.Helper()
	s, ok := alloc.Pending()[num].(ast.PdfStream)
	if !ok {
		t.Fatalf("object %d is not a stream", num)
	}
	return s
}

func TestRecompressImagesKeepsSamplesAndShrinks(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	samples := gradientRGB(64, 48)
	smask := ast.PdfIndirectRef{ObjectNum: 99}

	cases := []struct {
		name   string
		filter ast.PdfObject
		data   []byte
	}{
		{"uncompressed", nil, samples},
		{"fast flate", ast.PdfName("FlateDecode"), zlibAt(t, zlib.BestSpeed, samples)},
		{"ascii hex", ast.PdfName("ASCIIHexDecode"), []byte(hex.EncodeToString(samples) + ">")},
		{
			"hex over flate",
			ast.PdfArray{ast.PdfName("ASCIIHexDecode"), ast.PdfName("FlateDecode")},
			[]byte(hex.EncodeToString(zlibAt(t, zlib.BestSpeed, samples)) + ">"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			alloc := NewObjAllocator(0)
			extra := ast.PdfDict{
				"Decode": ast.PdfArray{ast.PdfInt(1), ast.PdfInt(0), ast.PdfInt(1), ast.PdfInt(0), ast.PdfInt(1), ast.PdfInt(0)},
				"SMask":  smask,
			}
			if tc.filter != nil {
				extra["Filter"] = tc.filter
			}
			ref := alloc.Alloc(ast.NewStream(imageDict(64, 48, extra), tc.data))
			before, err := serializedSize(ctx, streamAt(t, alloc, ref.ObjectNum))
			if err != nil {
				t.Fatal(err)
			}

			stats, err := RecompressImages(ctx, alloc, ImageRecompression{})
			if err != nil {
				t.Fatalf("RecompressImages: %v", err)
			}
			if stats.Images != 1 || stats.SavedBytes <= 0 {
				t.Fatalf("stats = %+v, want one image recompressed with a saving", stats)
			}
			after, err := serializedSize(ctx, streamAt(t, alloc, ref.ObjectNum))
			if err != nil {
				t.Fatal(err)
			}
			if before-after != stats.SavedBytes {
				t.Fatalf("saved %d bytes, stats say %d", before-after, stats.SavedBytes)
			}
			if !bytes.Equal(decodedSamples(t, alloc, ref.ObjectNum), samples) {
				t.Fatal("recompression changed the decoded samples")
			}
			dict := streamAt(t, alloc, ref.ObjectNum).Dict
			if dict["Filter"] != ast.PdfName("FlateDecode") {
				t.Fatalf("/Filter = %v", dict["Filter"])
			}
			if dict["SMask"] != smask || len(dict["Decode"].(ast.PdfArray)) != 6 || dict["ColorSpace"] != ast.PdfName("DeviceRGB") {
				t.Fatalf("an entry other than the encoding changed: %v", dict)
			}
		})
	}
}

func TestRecompressImagesUsesPredictorWhenSmaller(t *testing.T) {
	t.Parallel()
	alloc := NewObjAllocator(0)
	ref := alloc.Alloc(ast.NewStream(imageDict(64, 48, nil), gradientRGB(64, 48)))
	if _, err := RecompressImages(context.Background(), alloc, ImageRecompression{}); err != nil {
		t.Fatal(err)
	}
	parms, ok := streamAt(t, alloc, ref.ObjectNum).Dict["DecodeParms"].(ast.PdfDict)
	if !ok || parms["Predictor"] != ast.PdfInt(15) || parms["Colors"] != ast.PdfInt(3) || parms["Columns"] != ast.PdfInt(64) {
		t.Fatalf("/DecodeParms = %v, want a PNG predictor over 3 colours and 64 columns", parms)
	}
}

func TestRecompressImagesLeavesAlone(t *testing.T) {
	t.Parallel()
	samples := gradientRGB(16, 16)
	best := zlibAt(t, zlib.BestSpeed, samples)
	damaged := append([]byte(nil), best...)
	damaged[len(damaged)-1] ^= 0xff // the Adler-32 checksum no longer matches

	claim := filter.NewDecoder([]plugins.FilterDecoder{flateClaim{}}, limits.Default())

	cases := []struct {
		name string
		dict ast.PdfDict
		data []byte
		cfg  ImageRecompression
	}{
		{"jpeg", imageDict(16, 16, ast.PdfDict{"Filter": ast.PdfName("DCTDecode")}), []byte{0xff, 0xd8, 0xff, 0xd9}, ImageRecompression{}},
		{"salvaged flate", imageDict(16, 16, ast.PdfDict{"Filter": ast.PdfName("FlateDecode")}), damaged, ImageRecompression{}},
		{"past the image budget", imageDict(16, 16, nil), samples, ImageRecompression{Limits: limits.Limits{MaxImagePixels: 255}}},
		{"plugin claims the filter", imageDict(16, 16, ast.PdfDict{"Filter": ast.PdfName("FlateDecode")}), best, ImageRecompression{Decoder: claim}},
		{"external data", imageDict(16, 16, ast.PdfDict{"F": ast.PdfString("x.bin")}), samples, ImageRecompression{}},
		{"not an image", ast.PdfDict{"Type": ast.PdfName("XObject"), "Subtype": ast.PdfName("Form")}, samples, ImageRecompression{}},
		{"filter in an array of refs to nothing", imageDict(16, 16, ast.PdfDict{"Filter": ast.PdfArray{ast.PdfIndirectRef{ObjectNum: 77}}}), samples, ImageRecompression{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			alloc := NewObjAllocator(0)
			ref := alloc.Alloc(ast.NewStream(tc.dict, tc.data))
			stats, err := RecompressImages(context.Background(), alloc, tc.cfg)
			if err != nil {
				t.Fatal(err)
			}
			if stats.Images != 0 {
				t.Fatalf("recompressed %d images, want none", stats.Images)
			}
			got, err := streamAt(t, alloc, ref.ObjectNum).Bytes(context.Background())
			if err != nil || !bytes.Equal(got, tc.data) {
				t.Fatal("the stream's data changed")
			}
		})
	}
}

// TestRecompressImagesKeepsSmallerOriginal checks that an image already at
// zlib's best compression of noise is not replaced by a re-encoding that is
// no smaller.
func TestRecompressImagesKeepsSmallerOriginal(t *testing.T) {
	t.Parallel()
	noise := make([]byte, 32*32*3)
	x := uint32(2463534242)
	for i := range noise {
		x ^= x << 13
		x ^= x >> 17
		x ^= x << 5
		noise[i] = byte(x)
	}
	alloc := NewObjAllocator(0)
	data := zlibAt(t, zlib.BestCompression, noise)
	alloc.Alloc(ast.NewStream(imageDict(32, 32, ast.PdfDict{"Filter": ast.PdfName("FlateDecode")}), data))
	stats, err := RecompressImages(context.Background(), alloc, ImageRecompression{})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Images != 0 {
		t.Fatal("replaced an image with a re-encoding that is not smaller")
	}
}

// TestRecompressImagesResolvesIndirectParms covers a /DecodeParms held in an
// object of its own, which the decoder reads only once it is made direct.
func TestRecompressImagesResolvesIndirectParms(t *testing.T) {
	t.Parallel()
	samples := gradientRGB(20, 10)
	predicted, err := filter.EncodePNGPredictor(samples, 3, 8, 20)
	if err != nil {
		t.Fatal(err)
	}
	alloc := NewObjAllocator(0)
	parms := alloc.Alloc(ast.PdfDict{"Predictor": ast.PdfInt(12), "Colors": ast.PdfInt(3), "Columns": ast.PdfInt(20)})
	ref := alloc.Alloc(ast.NewStream(imageDict(20, 10, ast.PdfDict{
		"Filter": ast.PdfName("FlateDecode"), "DecodeParms": parms,
	}), zlibAt(t, zlib.NoCompression, predicted)))
	stats, err := RecompressImages(context.Background(), alloc, ImageRecompression{})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Images != 1 {
		t.Fatalf("stats = %+v", stats)
	}
	if !bytes.Equal(decodedSamples(t, alloc, ref.ObjectNum), samples) {
		t.Fatal("recompression changed the decoded samples")
	}
}

// TestRecompressImagesLazyPathMatchesRetained checks that an image whose
// re-encoding is redone at write time, past the retention budget, serializes
// to the bytes it would have had in memory.
func TestRecompressImagesLazyPathMatchesRetained(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	samples := gradientRGB(64, 48)
	build := func() (*ObjAllocator, int) {
		alloc := NewObjAllocator(0)
		ref := alloc.Alloc(ast.NewStream(imageDict(64, 48, nil), samples))
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
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Fatal("the lazy re-encoding differs from the retained one")
	}
}

// TestWriteRewriteRecompressesOnlyInTheClear checks the pass runs inside a
// rewrite, and is declined when the trailer carries /Encrypt.
func TestWriteRewriteRecompressesOnlyInTheClear(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	samples := gradientRGB(64, 48)
	for _, encrypted := range []bool{false, true} {
		alloc := NewObjAllocator(0)
		img := alloc.Alloc(ast.NewStream(imageDict(64, 48, nil), samples))
		root := alloc.Alloc(ast.PdfDict{"Type": ast.PdfName("Catalog"), "Img": img})
		trailer := ast.PdfDict{"Root": root}
		if encrypted {
			trailer["Encrypt"] = alloc.Alloc(ast.PdfDict{"Filter": ast.PdfName("Standard")})
		}
		var out bytes.Buffer
		if err := WriteRewrite(ctx, &out, RewriteConfig{
			Allocator: alloc, Trailer: trailer, RecompressImages: &ImageRecompression{},
		}); err != nil {
			t.Fatal(err)
		}
		got, err := streamAt(t, alloc, img.ObjectNum).Bytes(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if changed := !bytes.Equal(got, samples); changed == encrypted {
			t.Fatalf("encrypted=%v: recompressed=%v", encrypted, changed)
		}
	}
}

// flateClaim is a filter plugin that claims FlateDecode.
type flateClaim struct{}

func (flateClaim) FilterName() string { return "FlateDecode" }

func (flateClaim) Decode(data []byte, _ map[string]any) ([]byte, error) { return data, nil }

// gradientJPEG returns a w x h RGB gradient encoded by image/jpeg, whose
// encoder writes the example Huffman tables of ITU-T T.81, Annex K.3 rather
// than tables fitted to the data.
func gradientJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetRGBA(x, y, color.RGBA{R: byte(x * 3), G: byte(y * 5), B: byte(x ^ y), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestRecompressImagesRecodesJPEG checks that a DCTDecode image keeps its
// filter and every dictionary entry, shrinks, and decodes to the same samples.
func TestRecompressImagesRecodesJPEG(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := gradientJPEG(t, 96, 72)
	dict := imageDict(96, 72, ast.PdfDict{
		"Filter":      ast.PdfName("DCTDecode"),
		"DecodeParms": ast.PdfDict{"ColorTransform": ast.PdfInt(1)},
		"SMask":       ast.PdfIndirectRef{ObjectNum: 99},
	})
	alloc := NewObjAllocator(0)
	ref := alloc.Alloc(ast.NewStream(dict, data))
	want := decodedSamples(t, alloc, ref.ObjectNum)

	stats, err := RecompressImages(ctx, alloc, ImageRecompression{})
	if err != nil {
		t.Fatalf("RecompressImages: %v", err)
	}
	if stats.Images != 1 || stats.SavedBytes <= 0 {
		t.Fatalf("stats = %+v, want the JPEG re-coded with a saving", stats)
	}
	got := streamAt(t, alloc, ref.ObjectNum)
	if got.Dict.String() != dict.String() {
		t.Fatalf("the dictionary changed: %v", got.Dict)
	}
	recoded, err := got.Bytes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(data)-len(recoded)) != stats.SavedBytes {
		t.Fatalf("saved %d bytes, stats say %d", len(data)-len(recoded), stats.SavedBytes)
	}
	if !bytes.Equal(decodedSamples(t, alloc, ref.ObjectNum), want) {
		t.Fatal("re-coding changed the decoded samples")
	}
}

// TestRecompressImagesLeavesOtherJPEGsAlone covers the JPEG data the pass
// does not re-code.
func TestRecompressImagesLeavesOtherJPEGsAlone(t *testing.T) {
	t.Parallel()
	data := gradientJPEG(t, 32, 32)
	progressive, err := os.ReadFile("../../filter/dct/testdata/progressive.jpg")
	if err != nil {
		t.Fatal(err)
	}
	claim := filter.NewDecoder([]plugins.FilterDecoder{dctClaim{}}, limits.Default())
	jpegDict := func() ast.PdfDict { return imageDict(32, 32, ast.PdfDict{"Filter": ast.PdfName("DCTDecode")}) }
	cases := []struct {
		name string
		dict ast.PdfDict
		data []byte
		cfg  ImageRecompression
	}{
		{"progressive", jpegDict(), progressive, ImageRecompression{}},
		{"plugin claims DCTDecode", jpegDict(), data, ImageRecompression{Decoder: claim}},
		{
			"under another filter", imageDict(32, 32, ast.PdfDict{"Filter": ast.PdfArray{ast.PdfName("ASCIIHexDecode"), ast.PdfName("DCTDecode")}}),
			[]byte(hex.EncodeToString(data) + ">"),
			ImageRecompression{},
		},
		{"past the image budget", jpegDict(), data, ImageRecompression{Limits: limits.Limits{MaxImagePixels: 32*32 - 1}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			alloc := NewObjAllocator(0)
			ref := alloc.Alloc(ast.NewStream(tc.dict, tc.data))
			stats, err := RecompressImages(context.Background(), alloc, tc.cfg)
			if err != nil {
				t.Fatal(err)
			}
			if stats.Images != 0 {
				t.Fatalf("recompressed %d images, want none", stats.Images)
			}
			got, err := streamAt(t, alloc, ref.ObjectNum).Bytes(context.Background())
			if err != nil || !bytes.Equal(got, tc.data) {
				t.Fatal("the stream's data changed")
			}
		})
	}
}

// TestRecompressImagesLazyJPEGMatchesRetained checks that a JPEG whose
// re-coding is redone at write time, past the retention budget, serializes to
// the bytes it would have had in memory.
func TestRecompressImagesLazyJPEGMatchesRetained(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := gradientJPEG(t, 48, 40)
	build := func() (*ObjAllocator, int) {
		alloc := NewObjAllocator(0)
		ref := alloc.Alloc(ast.NewStream(imageDict(48, 40, ast.PdfDict{"Filter": ast.PdfName("DCTDecode")}), data))
		return alloc, ref.ObjectNum
	}
	retained, num := build()
	if _, err := RecompressImages(ctx, retained, ImageRecompression{}); err != nil {
		t.Fatal(err)
	}
	lazy, _ := build()
	stats, err := RecompressImages(ctx, lazy, ImageRecompression{Limits: limits.Limits{MaxStreamBytes: 1}})
	if err != nil || stats.Images != 1 {
		t.Fatalf("stats = %+v, err = %v", stats, err)
	}
	var a, b bytes.Buffer
	if _, err := Serialize(ctx, &a, streamAt(t, retained, num)); err != nil {
		t.Fatal(err)
	}
	if _, err := Serialize(ctx, &b, streamAt(t, lazy, num)); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) || bytes.Contains(a.Bytes(), data) {
		t.Fatal("the lazy re-coding differs from the retained one")
	}
}

// dctClaim is a filter plugin that claims DCTDecode.
type dctClaim struct{}

func (dctClaim) FilterName() string { return "DCTDecode" }

func (dctClaim) Decode(data []byte, _ map[string]any) ([]byte, error) { return data, nil }
