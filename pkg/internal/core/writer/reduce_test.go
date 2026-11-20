package writer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"maps"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/filter/dct"
	"github.com/gotenberg/litho/pkg/models"
)

// noisyPhoto returns w x h pixels of c components that vary smoothly with
// grain on top, as a photograph does: FlateDecode holds them in more bytes
// than a JPEG.
func noisyPhoto(w, h, c int) []byte {
	out := make([]byte, 0, w*h*c)
	seed := uint32(1)
	for y := range h {
		for x := range w {
			for k := range c {
				seed = seed*1664525 + 1013904223
				out = append(out, byte(x*3+y*2+k*40+int(seed>>27)))
			}
		}
	}
	return out
}

// reduceDoc is a one-page document drawing with content under resources,
// the objects of extra added at their numbers.
func reduceDoc(content string, resources ast.PdfDict, extra map[int]ast.PdfObject) (*ObjAllocator, ast.PdfDict) {
	alloc := NewObjAllocator(0)
	alloc.AllocAt(1, ast.PdfDict{"Type": ast.PdfName("Catalog"), "Pages": ast.PdfIndirectRef{ObjectNum: 2}})
	alloc.AllocAt(2, ast.PdfDict{"Type": ast.PdfName("Pages"), "Kids": ast.PdfArray{ast.PdfIndirectRef{ObjectNum: 3}}, "Count": ast.PdfInt(1)})
	alloc.AllocAt(3, ast.PdfDict{
		"Type": ast.PdfName("Page"), "Parent": ast.PdfIndirectRef{ObjectNum: 2},
		"MediaBox":  ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(612), ast.PdfInt(792)},
		"Resources": resources, "Contents": ast.PdfIndirectRef{ObjectNum: 4},
	})
	alloc.AllocAt(4, ast.NewStream(ast.PdfDict{}, []byte(content)))
	for num, obj := range extra {
		alloc.AllocAt(num, obj)
	}
	return alloc, ast.PdfDict{"Root": ast.PdfIndirectRef{ObjectNum: 1}}
}

// xobjects returns resources naming each object as /Im<num> or /Fm<num>.
func xobjects(names map[string]int) ast.PdfDict {
	x := ast.PdfDict{}
	for name, num := range names {
		x[ast.PdfName(name)] = ast.PdfIndirectRef{ObjectNum: num}
	}
	return ast.PdfDict{"XObject": x}
}

// rawImage returns an uncompressed image of w x h pixels in cs.
func rawImage(w, h int, cs ast.PdfObject, samples []byte, extra ast.PdfDict) ast.PdfStream {
	d := imageDict(w, h, extra)
	d["ColorSpace"] = cs
	return ast.NewStream(d, samples)
}

// sizeAfter runs the pass at dpi over an image of w x h pixels drawn by
// content, and returns its size after.
func sizeAfter(t *testing.T, content string, extra map[int]ast.PdfObject, dpi float64) (int64, int64) {
	t.Helper()
	alloc, trailer := reduceDoc(content, xobjects(map[string]int{"Im10": 10, "Fm11": 11}), extra)
	if _, err := ReduceImages(context.Background(), alloc, trailer, ImageReduction{MaxDPI: dpi}); err != nil {
		t.Fatal(err)
	}
	d := streamAt(t, alloc, 10).Dict
	return int64(d["Width"].(ast.PdfInt)), int64(d["Height"].(ast.PdfInt))
}

// TestReduceImages_DrawnResolution: an image's resolution is its pixels over
// the length the unit square takes under the transformation at its Do (ISO
// 32000-1, 8.9.5 and 8.3.4), its largest placement deciding, each side
// along its own direction, the coarser side deciding for both. An image of
// 300 x 300 drawn one inch square is at 300 dpi and goes to 150 x 150 at
// 150 dpi.
func TestReduceImages_DrawnResolution(t *testing.T) {
	t.Parallel()
	img := rawImage(300, 300, ast.PdfName("DeviceGray"), noisyPhoto(300, 300, 1), nil)
	form := func(matrix ast.PdfArray, content string) ast.PdfStream {
		return ast.NewStream(ast.PdfDict{
			"Type": ast.PdfName("XObject"), "Subtype": ast.PdfName("Form"),
			"BBox": ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(1), ast.PdfInt(1)}, "Matrix": matrix,
			"Resources": xobjects(map[string]int{"Im10": 10}),
		}, []byte(content))
	}
	cases := []struct {
		name    string
		content string
		form    ast.PdfStream
		w, h    int64
	}{
		{"one inch", "q 72 0 0 72 0 0 cm /Im10 Do Q", ast.PdfStream{}, 150, 150},
		{"largest placement wins", "q 72 0 0 72 0 0 cm /Im10 Do Q q 144 0 0 144 0 0 cm /Im10 Do Q", ast.PdfStream{}, 300, 300},
		{"the coarser side decides", "q 72 0 0 144 0 0 cm /Im10 Do Q", ast.PdfStream{}, 300, 300},
		{"one factor keeps the shape", "q 36 0 0 72 0 0 cm /Im10 Do Q", ast.PdfStream{}, 150, 150},
		{"rotated a quarter turn", "q 0 72 -72 0 0 0 cm /Im10 Do Q", ast.PdfStream{}, 150, 150},
		{"skewed keeps its true lengths", "q 72 0 72 72 0 0 cm /Im10 Do Q", ast.PdfStream{}, 300, 300},
		{"skewed and finer", "q 36 0 36 36 0 0 cm /Im10 Do Q", ast.PdfStream{}, 107, 107},
		{"through a form matrix", "q 36 0 0 36 0 0 cm /Fm11 Do Q", form(ast.PdfArray{ast.PdfInt(2), ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(2), ast.PdfInt(5), ast.PdfInt(5)}, "/Im10 Do"), 150, 150},
		{"within the threshold", "q 110 0 0 110 0 0 cm /Im10 Do Q", ast.PdfStream{}, 300, 300},
		{"drawn nowhere", "", ast.PdfStream{}, 300, 300},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			extra := map[int]ast.PdfObject{10: img}
			if tc.form.Dict != nil {
				extra[11] = tc.form
			}
			w, h := sizeAfter(t, tc.content, extra, 150)
			if w != tc.w || h != tc.h {
				t.Fatalf("%dx%d, want %dx%d", w, h, tc.w, tc.h)
			}
		})
	}
}

// TestReduceImages_UnknownSizeKeepsPixels: an image whose drawn size the
// walk cannot vouch for keeps its pixels: drawn by a stream that does not
// decode, or named from outside the resources the walk read.
func TestReduceImages_UnknownSizeKeepsPixels(t *testing.T) {
	t.Parallel()
	img := rawImage(300, 300, ast.PdfName("DeviceGray"), noisyPhoto(300, 300, 1), nil)
	t.Run("content that does not decode", func(t *testing.T) {
		t.Parallel()
		alloc, trailer := reduceDoc("", xobjects(map[string]int{"Im10": 10}), map[int]ast.PdfObject{10: img})
		alloc.AllocAt(4, ast.NewStream(ast.PdfDict{"Filter": ast.PdfName("FlateDecode")}, []byte("not zlib")))
		stats, err := ReduceImages(context.Background(), alloc, trailer, ImageReduction{MaxDPI: 150})
		if err != nil {
			t.Fatal(err)
		}
		if stats.Downsampled != 0 || stats.Kept[string(reasonUnreadContent)] != 1 {
			t.Fatalf("stats = %+v", stats)
		}
	})
	t.Run("named from the catalog", func(t *testing.T) {
		t.Parallel()
		alloc, trailer := reduceDoc("q 72 0 0 72 0 0 cm /Im10 Do Q", xobjects(map[string]int{"Im10": 10}), map[int]ast.PdfObject{10: img})
		alloc.AllocAt(1, ast.PdfDict{"Type": ast.PdfName("Catalog"), "Pages": ast.PdfIndirectRef{ObjectNum: 2}, "Extra": ast.PdfIndirectRef{ObjectNum: 10}})
		stats, err := ReduceImages(context.Background(), alloc, trailer, ImageReduction{MaxDPI: 150})
		if err != nil {
			t.Fatal(err)
		}
		if stats.Downsampled != 0 || stats.Kept[string(reasonReferencedApart)] != 1 {
			t.Fatalf("stats = %+v", stats)
		}
	})
}

// TestReduceImages_AnnotationAndUserUnit: an appearance is drawn with its
// bounding box fitted to the annotation's rectangle (ISO 32000-1, 12.5.5),
// and a page's /UserUnit scales its default unit (8.3.2.3).
func TestReduceImages_AnnotationAndUserUnit(t *testing.T) {
	t.Parallel()
	img := rawImage(300, 300, ast.PdfName("DeviceGray"), noisyPhoto(300, 300, 1), nil)
	ap := ast.NewStream(ast.PdfDict{
		"Type": ast.PdfName("XObject"), "Subtype": ast.PdfName("Form"),
		"BBox":      ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(1), ast.PdfInt(1)},
		"Resources": xobjects(map[string]int{"Im10": 10}),
	}, []byte("/Im10 Do"))
	alloc, trailer := reduceDoc("", ast.PdfDict{}, map[int]ast.PdfObject{
		10: img, 11: ap,
		12: ast.PdfDict{"Type": ast.PdfName("Annot"), "Subtype": ast.PdfName("Stamp"),
			"Rect": ast.PdfArray{ast.PdfInt(100), ast.PdfInt(100), ast.PdfInt(136), ast.PdfInt(136)},
			"AP":   ast.PdfDict{"N": ast.PdfIndirectRef{ObjectNum: 11}}},
	})
	page := alloc.Pending()[3].(ast.PdfDict)
	page["Annots"] = ast.PdfArray{ast.PdfIndirectRef{ObjectNum: 12}}
	page["UserUnit"] = ast.PdfInt(2)
	if _, err := ReduceImages(context.Background(), alloc, trailer, ImageReduction{MaxDPI: 150}); err != nil {
		t.Fatal(err)
	}
	// 36 units of 2/72 inch: one inch.
	if d := streamAt(t, alloc, 10).Dict; d["Width"] != ast.PdfInt(150) || d["Height"] != ast.PdfInt(150) {
		t.Fatalf("%v x %v, want 150 x 150", d["Width"], d["Height"])
	}
}

// TestReduceImages_SoftMaskFollowsItsImage: a soft mask is drawn where its
// image is (ISO 32000-1, 11.6.5.3), so a mask of the image's size is
// reduced to the image's new size, and it stays lossless.
func TestReduceImages_SoftMaskFollowsItsImage(t *testing.T) {
	t.Parallel()
	mask := rawImage(300, 300, ast.PdfName("DeviceGray"), noisyPhoto(300, 300, 1), nil)
	img := rawImage(300, 300, ast.PdfName("DeviceRGB"), noisyPhoto(300, 300, 3), ast.PdfDict{"SMask": ast.PdfIndirectRef{ObjectNum: 11}})
	alloc, trailer := reduceDoc("q 72 0 0 72 0 0 cm /Im10 Do Q", xobjects(map[string]int{"Im10": 10}), map[int]ast.PdfObject{10: img, 11: mask})
	stats, err := ReduceImages(context.Background(), alloc, trailer, ImageReduction{MaxDPI: 150, Quality: 75})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Downsampled != 2 || stats.JPEG != 1 || stats.Lossless != 1 {
		t.Fatalf("stats = %+v", stats)
	}
	for _, num := range []int{10, 11} {
		if d := streamAt(t, alloc, num).Dict; d["Width"] != ast.PdfInt(150) || d["Height"] != ast.PdfInt(150) {
			t.Fatalf("object %d: %v x %v", num, d["Width"], d["Height"])
		}
	}
	if f := streamAt(t, alloc, 11).Dict["Filter"]; f != ast.PdfName("FlateDecode") {
		t.Fatalf("soft mask /Filter = %v, want FlateDecode", f)
	}
}

// TestReduceImages_WhoQualifiesForJPEG states which images become JPEG and
// which keep their encoding, with the reason.
func TestReduceImages_WhoQualifiesForJPEG(t *testing.T) {
	t.Parallel()
	photo := noisyPhoto(64, 64, 3)
	indexed := ast.PdfArray{ast.PdfName("Indexed"), ast.PdfName("DeviceRGB"), ast.PdfInt(255), ast.PdfString(bytes.Repeat([]byte{1, 2, 3}, 256))}
	lab := ast.PdfArray{ast.PdfName("Lab"), ast.PdfDict{"WhitePoint": ast.PdfArray{ast.PdfReal(0.9505), ast.PdfInt(1), ast.PdfReal(1.089)}}}
	cases := []struct {
		name string
		img  ast.PdfStream
		kept string
	}{
		{"RGB photo", rawImage(64, 64, ast.PdfName("DeviceRGB"), photo, nil), ""},
		{"CMYK photo", rawImage(32, 32, ast.PdfName("DeviceCMYK"), noisyPhoto(32, 32, 4), ast.PdfDict{"Decode": ast.PdfArray{ast.PdfInt(1), ast.PdfInt(0), ast.PdfInt(1), ast.PdfInt(0), ast.PdfInt(1), ast.PdfInt(0), ast.PdfInt(1), ast.PdfInt(0)}}), ""},
		{"Indexed", rawImage(64, 64, indexed, noisyPhoto(64, 64, 1), nil), keptIndexed},
		{"colour-key mask", rawImage(64, 64, ast.PdfName("DeviceRGB"), photo, ast.PdfDict{"Mask": ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(0)}}), keptColourKey},
		{"one bit", rawImage(64, 64, ast.PdfName("DeviceGray"), bytes.Repeat([]byte{0x55}, 8*64), ast.PdfDict{"BitsPerComponent": ast.PdfInt(1)}), keptDepth},
		{"stencil", ast.NewStream(ast.PdfDict{"Subtype": ast.PdfName("Image"), "Width": ast.PdfInt(64), "Height": ast.PdfInt(64), "ImageMask": ast.PdfBool(true)}, bytes.Repeat([]byte{0x55}, 8*64)), keptStencil},
		{"Lab", rawImage(64, 64, lab, photo, nil), keptNoJPEGSpace},
		{"malformed Decode", rawImage(64, 64, ast.PdfName("DeviceRGB"), photo, ast.PdfDict{"Decode": ast.PdfArray{ast.PdfInt(0), ast.PdfInt(1)}}), keptDecode},
		{"JPEG 2000", rawImage(64, 64, ast.PdfName("DeviceRGB"), []byte("x"), ast.PdfDict{"Filter": ast.PdfName("JPXDecode")}), keptFilter},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			alloc, trailer := reduceDoc("", ast.PdfDict{}, map[int]ast.PdfObject{10: tc.img})
			stats, err := ReduceImages(context.Background(), alloc, trailer, ImageReduction{Quality: 75})
			if err != nil {
				t.Fatal(err)
			}
			if tc.kept == "" {
				d := streamAt(t, alloc, 10).Dict
				if stats.JPEG != 1 || d["Filter"] != ast.PdfName("DCTDecode") {
					t.Fatalf("stats = %+v, /Filter %v: want a JPEG", stats, d["Filter"])
				}
				if !equalObjects(d["Decode"], tc.img.Dict["Decode"]) || d["ColorSpace"] != tc.img.Dict["ColorSpace"] {
					t.Fatalf("an entry the JPEG keeps changed: %v", d)
				}
				return
			}
			if stats.Kept[tc.kept] != 1 || stats.JPEG+stats.Lossless != 0 {
				t.Fatalf("stats = %+v, want one kept for %q", stats, tc.kept)
			}
		})
	}
}

// equalObjects compares two values by their serialization.
func equalObjects(a, b ast.PdfObject) bool {
	var x, y bytes.Buffer
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if _, err := Serialize(context.Background(), &x, a); err != nil {
		return false
	}
	if _, err := Serialize(context.Background(), &y, b); err != nil {
		return false
	}
	return bytes.Equal(x.Bytes(), y.Bytes())
}

// TestReduceImages_JPEGGenerations: a JPEG not downsampled is re-encoded only
// when its own quality is above the quality asked, since each generation of
// JPEG coding loses detail of its own.
func TestReduceImages_JPEGGenerations(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, tc := range []struct {
		own, asked int
		recoded    bool
	}{{95, 50, true}, {60, 75, false}, {75, 75, false}} {
		data, err := dct.Encode(ctx, noisyPhoto(64, 64, 3), 64, 64, 3, dct.EncodeOptions{Quality: tc.own})
		if err != nil {
			t.Fatal(err)
		}
		img := rawImage(64, 64, ast.PdfName("DeviceRGB"), data, ast.PdfDict{"Filter": ast.PdfName("DCTDecode")})
		alloc, trailer := reduceDoc("", ast.PdfDict{}, map[int]ast.PdfObject{10: img})
		stats, err := ReduceImages(ctx, alloc, trailer, ImageReduction{Quality: tc.asked})
		if err != nil {
			t.Fatal(err)
		}
		if got := stats.JPEG == 1; got != tc.recoded {
			t.Errorf("a JPEG at %d asked %d: stats = %+v", tc.own, tc.asked, stats)
		}
	}
}

// TestReduceImages_SameBytesOnEveryMachine pins the bytes of one image the
// pass downsamples and encodes as JPEG: the resampler is integer arithmetic
// and the encoder rounds each product on its own, so amd64 and arm64 write
// the same bytes.
func TestReduceImages_SameBytesOnEveryMachine(t *testing.T) {
	t.Parallel()
	img := rawImage(300, 200, ast.PdfName("DeviceRGB"), noisyPhoto(300, 200, 3), nil)
	alloc, trailer := reduceDoc("q 72 0 0 48 0 0 cm /Im10 Do Q", xobjects(map[string]int{"Im10": 10}), map[int]ast.PdfObject{10: img})
	if _, err := ReduceImages(context.Background(), alloc, trailer, ImageReduction{MaxDPI: 96, Quality: 75}); err != nil {
		t.Fatal(err)
	}
	data, err := streamAt(t, alloc, 10).Bytes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	const want = "5199290ae9c548b633e3fa46bdd164680af293f167557ea9e10cae6a171ce39c"
	if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != want {
		t.Fatalf("SHA-256 %x, want %s", sum, want)
	}
}

// TestResampleArea_WeighsByCoveredArea: each target pixel is the mean of
// the source area it covers, a source pixel it covers in part counting for
// that part, rounded half up.
func TestResampleArea_WeighsByCoveredArea(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	got, err := resampleArea(ctx, []byte{0, 30, 90, 255}, 4, 1, 1, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte{15, 173}) {
		t.Fatalf("4 to 2: %v, want [15 173]", got)
	}
	// Three to two: the first target pixel covers the first source pixel
	// and half the second, (2*0 + 1*30) / 3 = 10.
	got, err = resampleArea(ctx, []byte{0, 30, 90}, 3, 1, 1, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte{10, 70}) {
		t.Fatalf("3 to 2: %v, want [10 70]", got)
	}
	// Down a column, two components kept apart.
	got, err = resampleArea(ctx, []byte{10, 200, 20, 100, 31, 0}, 1, 3, 2, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte{20, 100}) {
		t.Fatalf("a column: %v, want [20 100]", got)
	}
	if _, err := resampleArea(ctx, []byte{1, 2}, 2, 1, 1, 3, 1); err == nil {
		t.Fatal("upsampling accepted")
	}
}

// TestReduceImages_PlacementsBeyondForms: an image is also drawn by a tiling
// pattern, over the default space of the stream the pattern is a resource of
// (ISO 32000-1, 8.7.3.1), by a Type 3 glyph, under the font matrix and the
// text rendering matrix (9.6.5, 9.4.4), and by the group of a soft mask, in
// the space in force when gs sets it (11.6.5.2). Each draws the 300 x 300
// image one inch square; past the form depth budget its size is unknown.
func TestReduceImages_PlacementsBeyondForms(t *testing.T) {
	t.Parallel()
	img := rawImage(300, 300, ast.PdfName("DeviceGray"), noisyPhoto(300, 300, 1), nil)
	unit := ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(1), ast.PdfInt(1)}
	stream := func(d ast.PdfDict, content string) ast.PdfStream {
		d["Resources"] = xobjects(map[string]int{"Im10": 10})
		return ast.NewStream(d, []byte(content))
	}
	pattern := stream(ast.PdfDict{
		"PatternType": ast.PdfInt(1), "PaintType": ast.PdfInt(1), "TilingType": ast.PdfInt(1),
		"BBox": unit, "XStep": ast.PdfInt(1), "YStep": ast.PdfInt(1),
		"Matrix": ast.PdfArray{ast.PdfInt(72), ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(72), ast.PdfInt(0), ast.PdfInt(0)},
	}, "/Im10 Do")
	glyph := ast.NewStream(ast.PdfDict{}, []byte("1000 0 0 0 1000 1000 d1 q 1000 0 0 1000 0 0 cm /Im10 Do Q"))
	font := ast.PdfDict{
		"Type": ast.PdfName("Font"), "Subtype": ast.PdfName("Type3"),
		"FontMatrix": ast.PdfArray{ast.PdfReal(0.001), ast.PdfInt(0), ast.PdfInt(0), ast.PdfReal(0.001), ast.PdfInt(0), ast.PdfInt(0)},
		"CharProcs":  ast.PdfDict{"a": ast.PdfIndirectRef{ObjectNum: 13}},
		"Resources":  xobjects(map[string]int{"Im10": 10}),
	}
	group := stream(ast.PdfDict{"Type": ast.PdfName("XObject"), "Subtype": ast.PdfName("Form"), "BBox": unit}, "q 72 0 0 72 0 0 cm /Im10 Do Q")
	form := func(inner string) ast.PdfStream {
		d := ast.PdfDict{"Type": ast.PdfName("XObject"), "Subtype": ast.PdfName("Form"), "BBox": unit}
		s := stream(d, inner)
		s.Dict["Resources"].(ast.PdfDict)["XObject"].(ast.PdfDict)["Fm12"] = ast.PdfIndirectRef{ObjectNum: 12}
		return s
	}
	cases := []struct {
		name    string
		content string
		res     ast.PdfDict
		objects map[int]ast.PdfObject
		limits  models.Limits
		w       ast.PdfObject
	}{
		{"tiling pattern", "/Pattern cs /P1 scn 0 0 72 72 re f", ast.PdfDict{"Pattern": ast.PdfDict{"P1": ast.PdfIndirectRef{ObjectNum: 11}}}, map[int]ast.PdfObject{11: pattern}, models.Limits{}, ast.PdfInt(150)},
		{"Type 3 glyph", "BT /F1 72 Tf (a) Tj ET", ast.PdfDict{"Font": ast.PdfDict{"F1": ast.PdfIndirectRef{ObjectNum: 12}}}, map[int]ast.PdfObject{12: font, 13: glyph}, models.Limits{}, ast.PdfInt(150)},
		{"soft-mask group", "/GS1 gs", ast.PdfDict{"ExtGState": ast.PdfDict{"GS1": ast.PdfDict{"SMask": ast.PdfDict{"S": ast.PdfName("Luminosity"), "G": ast.PdfIndirectRef{ObjectNum: 14}}}}}, map[int]ast.PdfObject{14: group}, models.Limits{}, ast.PdfInt(150)},
		{"past the form depth budget", "/Fm11 Do", ast.PdfDict{"XObject": ast.PdfDict{"Fm11": ast.PdfIndirectRef{ObjectNum: 15}}}, map[int]ast.PdfObject{15: form("/Fm12 Do"), 12: form("q 72 0 0 72 0 0 cm /Im10 Do Q")}, models.Limits{MaxFormDepth: 1}, ast.PdfInt(300)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			objects := map[int]ast.PdfObject{10: img}
			maps.Copy(objects, tc.objects)
			alloc, trailer := reduceDoc(tc.content, tc.res, objects)
			stats, err := ReduceImages(context.Background(), alloc, trailer, ImageReduction{MaxDPI: 150, Limits: tc.limits})
			if err != nil {
				t.Fatal(err)
			}
			if w := streamAt(t, alloc, 10).Dict["Width"]; w != tc.w {
				t.Fatalf("width %v, want %v (stats %+v)", w, tc.w, stats)
			}
		})
	}
}

// TestReduceImages_EncoderStaysInTheStreamBudget: an image is encoded as JPEG
// only when the encoder's working set, three bytes a sample beside the
// samples, fits the stream budget.
func TestReduceImages_EncoderStaysInTheStreamBudget(t *testing.T) {
	t.Parallel()
	img := rawImage(64, 64, ast.PdfName("DeviceRGB"), noisyPhoto(64, 64, 3), nil)
	for _, tc := range []struct {
		budget int64
		jpeg   int
	}{{64 * 64 * 3 * jpegWorkPerSample, 1}, {64*64*3*jpegWorkPerSample - 1, 0}} {
		alloc, trailer := reduceDoc("", ast.PdfDict{}, map[int]ast.PdfObject{10: img})
		stats, err := ReduceImages(context.Background(), alloc, trailer, ImageReduction{Quality: 75, Limits: models.Limits{MaxStreamBytes: tc.budget}})
		if err != nil {
			t.Fatal(err)
		}
		if stats.JPEG != tc.jpeg || (tc.jpeg == 0 && stats.Kept[keptBudget] != 1) {
			t.Errorf("stream budget %d: stats = %+v", tc.budget, stats)
		}
	}
}
