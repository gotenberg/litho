package recolor

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// mean converts to one component, the mean of the source's.
func mean(in, out []float64) {
	s := 0.0
	for _, v := range in {
		s += v
	}
	out[0] = s / float64(len(in))
}

func TestConvertPixels_FoldsDecodeIntoTheSamples(t *testing.T) {
	img := imageStream(2, 1, familyRGB, 8, []byte{0, 51, 102, 255, 255, 255},
		ast.PdfDict{"Decode": ast.PdfArray{ast.PdfInt(1), ast.PdfInt(0), ast.PdfInt(1), ast.PdfInt(0), ast.PdfInt(1), ast.PdfInt(0)}})
	p := newTestPage(2, 1, img)
	alloc, _, outcome, err := p.convert(t, deviceTarget(familyGray), fixedColors(&funcColors{f: mean}))
	if err != nil || outcome != ImageConverted {
		t.Fatalf("outcome %v, %v; want converted", outcome, err)
	}
	out := pending(t, alloc, 7)
	if cs := out.Dict["ColorSpace"]; cs != familyGray {
		t.Errorf("ColorSpace = %v, want DeviceGray", cs)
	}
	if _, ok := out.Dict["Decode"]; ok {
		t.Error("the converted image keeps /Decode, which its samples already carry")
	}
	if bpc := out.Dict["BitsPerComponent"]; bpc != ast.PdfInt(8) {
		t.Errorf("BitsPerComponent = %v, want 8", bpc)
	}
	doc, _ := p.open(t)
	if got, want := inflate(t, doc, out), []byte{204, 0}; !bytes.Equal(got, want) {
		t.Errorf("samples = %v, want %v: the mean of (1, 0.8, 0.6) and of black", got, want)
	}
}

func TestConvertPixels_KeepsSixteenBits(t *testing.T) {
	img := imageStream(2, 1, familyGray, 16, []byte{0x80, 0x00, 0xff, 0xff}, nil)
	p := newTestPage(2, 1, img)
	f := func(in, out []float64) { out[0], out[1], out[2] = in[0], in[0]/2, 1-in[0] }
	alloc, _, _, err := p.convert(t, deviceTarget(familyRGB), fixedColors(&funcColors{f: f}))
	if err != nil {
		t.Fatal(err)
	}
	out := pending(t, alloc, 7)
	if bpc := out.Dict["BitsPerComponent"]; bpc != ast.PdfInt(16) {
		t.Fatalf("BitsPerComponent = %v, want 16 for a 16-bit source", bpc)
	}
	doc, _ := p.open(t)
	got := inflate(t, doc, out)
	v := 32768.0 / 65535
	want := []uint16{uint16(math.Round(v * 65535)), uint16(math.Round(v / 2 * 65535)), uint16(math.Round((1 - v) * 65535)), 65535, 32768, 0}
	for i, w := range want {
		if s := uint16(got[2*i])<<8 | uint16(got[2*i+1]); s != w {
			t.Errorf("sample %d = %d, want %d", i, s, w)
		}
	}
}

func TestConvertPixels_UnpacksSubByteRows(t *testing.T) {
	// 2 x 2 RGB pixels of 4 bits: rows of 24 bits (ISO 32000-1, 8.9.3).
	img := imageStream(2, 2, familyRGB, 4, []byte{0x0f, 0x5a, 0xa5, 0xf0, 0x12, 0x34}, nil)
	p := newTestPage(2, 2, img)
	copyRGB := func(in, out []float64) {
		// The target's black is asked for in DeviceGray.
		copy(out, in)
		out[3] = 0
	}
	alloc, _, _, err := p.convert(t, deviceTarget(familyCMYK), fixedColors(&funcColors{f: copyRGB}))
	if err != nil {
		t.Fatal(err)
	}
	doc, _ := p.open(t)
	got := inflate(t, doc, pending(t, alloc, 7))
	for i, s := range []byte{0x0, 0xf, 0x5, 0xa, 0xa, 0x5, 0xf, 0x0, 0x1, 0x2, 0x3, 0x4} {
		if want := s * 17; got[i/3*4+i%3] != want {
			t.Errorf("component %d = %d, want %d", i, got[i/3*4+i%3], want)
		}
	}
}

func TestConvertToIndexed_KeepsTheSamplesOfAScan(t *testing.T) {
	// A 1-bit scan under /Decode [1 0]: sample 0 stands for white. Its
	// samples and filter stay, and a two-entry table of the target says
	// what each stands for (ISO 32000-1, 8.6.6.3).
	img := imageStream(8, 1, familyGray, 1, nil, ast.PdfDict{
		"Filter": ast.PdfName("ASCIIHexDecode"), "Decode": ast.PdfArray{ast.PdfInt(1), ast.PdfInt(0)},
	})
	img = ast.NewStream(img.Dict, []byte("a5>"))
	p := newTestPage(8, 1, img)
	toCMYK := func(in, out []float64) { out[0], out[1], out[2], out[3] = 0, 0, 0, 1-in[0] }
	alloc, im, outcome, err := p.convert(t, deviceTarget(familyCMYK), fixedColors(&funcColors{f: toCMYK}))
	if err != nil || outcome != ImageConverted {
		t.Fatalf("outcome %v, %v; want converted", outcome, err)
	}
	out := pending(t, alloc, 7)
	if raw, _ := out.Bytes(context.Background()); string(raw) != "a5>" || out.Dict["Filter"] != ast.PdfName("ASCIIHexDecode") {
		t.Errorf("data %q under %v, want the samples as encoded", raw, out.Dict["Filter"])
	}
	if _, ok := out.Dict["Decode"]; ok {
		t.Error("/Decode kept, which the table already carries")
	}
	want := ast.PdfArray{familyIndexed, familyCMYK, ast.PdfInt(1), ast.PdfString("\x00\x00\x00\x00\x00\x00\x00\xff")}
	if got, ok := out.Dict["ColorSpace"].(ast.PdfArray); !ok || len(got) != 4 || got[1] != want[1] || got[2] != want[2] || got[3] != want[3] {
		t.Errorf("ColorSpace = %v, want %v: sample 0 white, sample 1 full black ink", out.Dict["ColorSpace"], want)
	}
	if e := im.Report().Encoded["ASCIIHexDecode"]; e.Before != e.After {
		t.Errorf("encoded sizes %+v, want them equal", e)
	}
}

func TestConvertIndexed_ConvertsTheTableOnly(t *testing.T) {
	cs := ast.PdfArray{familyIndexed, familyRGB, ast.PdfInt(1), ast.PdfString("\xff\x00\x00\x00\x00\xff")}
	data := []byte{0, 1, 1, 0}
	p := newTestPage(4, 1, imageStream(4, 1, cs, 8, data, nil))
	alloc, _, outcome, err := p.convert(t, deviceTarget(familyGray), fixedColors(&funcColors{f: mean}))
	if err != nil || outcome != ImagePalette {
		t.Fatalf("outcome %v, %v; want the palette converted", outcome, err)
	}
	out := pending(t, alloc, 7)
	raw, err := out.Bytes(context.Background())
	if err != nil || !bytes.Equal(raw, data) {
		t.Errorf("samples = %v, want the indices %v as they were", raw, data)
	}
	got, ok := out.Dict["ColorSpace"].(ast.PdfArray)
	if !ok || len(got) != 4 || got[1] != familyGray || got[3] != ast.PdfString("\x55\x55") {
		t.Errorf("ColorSpace = %v, want [/Indexed /DeviceGray 1 <5555>]", out.Dict["ColorSpace"])
	}
}

func TestConvertSpots_KeptOrConverted(t *testing.T) {
	tint := ast.PdfDict{"FunctionType": ast.PdfInt(2), "Domain": ast.PdfArray{ast.PdfInt(0), ast.PdfInt(1)},
		"C0": ast.PdfArray{ast.PdfInt(1)}, "C1": ast.PdfArray{ast.PdfInt(0)}, "N": ast.PdfInt(1)}
	cs := ast.PdfArray{familySeparation, ast.PdfName("Spot"), familyGray, tint}
	data := []byte{0, 128, 255}

	p := newTestPage(3, 1, imageStream(3, 1, cs, 8, data, nil))
	colors := &funcColors{f: mean, spot: ast.PdfName("KeptSpot")}
	alloc, _, outcome, err := p.convert(t, deviceTarget(familyCMYK), fixedColors(colors))
	if err != nil || outcome != ImageSpotKept {
		t.Fatalf("outcome %v, %v; want the spot kept", outcome, err)
	}
	out := pending(t, alloc, 7)
	if out.Dict["ColorSpace"] != ast.PdfName("KeptSpot") {
		t.Errorf("ColorSpace = %v, want the space SpotSpace returned", out.Dict["ColorSpace"])
	}
	if raw, _ := out.Bytes(context.Background()); !bytes.Equal(raw, data) {
		t.Errorf("tints = %v, want %v as they were", raw, data)
	}

	cfg := deviceTarget(familyGray)
	cfg.ConvertSpots = true
	colors = &funcColors{f: func(in, out []float64) { out[0] = 1 - in[0] }}
	alloc, _, outcome, err = p.convert(t, cfg, fixedColors(colors))
	if err != nil || outcome != ImageConverted {
		t.Fatalf("outcome %v, %v; want converted to process", outcome, err)
	}
	if !contains(colors.asked, "Separation") {
		t.Errorf("converters asked for %v, want the Separation space itself, tint transform and all", colors.asked)
	}
	cs, _ = pending(t, alloc, 7).Dict["ColorSpace"].(ast.PdfArray)
	if len(cs) != 4 || cs[1] != familyGray || len(cs[3].(ast.PdfString)) != 256 {
		t.Fatalf("ColorSpace = %v, want a 256-entry DeviceGray table", cs)
	}
	if table := cs[3].(ast.PdfString); table[0] != 255 || table[128] != 127 || table[255] != 0 {
		t.Errorf("table entries 0, 128, 255 = %d %d %d, want 255 127 0", table[0], table[128], table[255])
	}
}

func TestConvert_LeavesWhatHasNoColour(t *testing.T) {
	tint := ast.PdfDict{"FunctionType": ast.PdfInt(2), "Domain": ast.PdfArray{ast.PdfInt(0), ast.PdfInt(1)},
		"C0": ast.PdfArray{ast.PdfInt(1)}, "C1": ast.PdfArray{ast.PdfInt(0)}, "N": ast.PdfInt(1)}
	cases := map[string]ast.PdfStream{
		"stencil mask":  imageStream(8, 1, nil, 1, []byte{0xaa}, ast.PdfDict{"ImageMask": ast.PdfBool(true)}),
		"None colorant": imageStream(1, 1, ast.PdfArray{familySeparation, ast.PdfName("None"), familyGray, tint}, 8, []byte{9}, nil),
		"in the target": imageStream(1, 1, familyCMYK, 8, []byte{1, 2, 3, 4}, nil),
	}
	for name, img := range cases {
		t.Run(name, func(t *testing.T) {
			p := newTestPage(1, 1, img)
			alloc, im, outcome, err := p.convert(t, deviceTarget(familyCMYK), fixedColors(&funcColors{f: mean}))
			if err != nil || outcome != ImageUnchanged {
				t.Fatalf("outcome %v, %v; want unchanged", outcome, err)
			}
			if alloc.Writes() != 0 {
				t.Errorf("%d objects queued, want none", alloc.Writes())
			}
			if r := im.Report(); r.Unchanged != 1 {
				t.Errorf("report %+v, want one unchanged", r)
			}
		})
	}
}

func TestConvert_DefaultSpaceIsNotTheDevice(t *testing.T) {
	// ISO 32000-1, 8.6.5.6: /DefaultGray stands in for DeviceGray, so an
	// image in DeviceGray under it is not in a DeviceGray target.
	p := newTestPage(1, 1, imageStream(1, 1, familyGray, 8, []byte{100}, nil))
	p.objs[5] = ast.PdfDict{
		"XObject":    ast.PdfDict{"Im1": imageRef},
		"ColorSpace": ast.PdfDict{"DefaultGray": ast.PdfArray{familyCalGray, ast.PdfDict{"WhitePoint": ast.PdfArray{ast.PdfReal(0.9505), ast.PdfInt(1), ast.PdfReal(1.089)}}}},
	}
	identity := func(in, out []float64) { out[0] = in[0] }
	_, _, outcome, err := p.convert(t, deviceTarget(familyGray), fixedColors(&funcColors{f: identity}))
	if err != nil || outcome != ImageConverted {
		t.Errorf("outcome %v, %v; want converted through the default space", outcome, err)
	}
}

func TestConvertPixels_ColourKeyBecomesAStencil(t *testing.T) {
	data := []byte{0, 0, 0, 255, 255, 255, 0, 0, 0}
	key := ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(0)}
	p := newTestPage(3, 1, imageStream(3, 1, familyRGB, 8, data, ast.PdfDict{"Mask": key}))
	alloc, _, _, err := p.convert(t, deviceTarget(familyGray), fixedColors(&funcColors{f: mean}))
	if err != nil {
		t.Fatal(err)
	}
	ref, ok := pending(t, alloc, 7).Dict["Mask"].(ast.PdfIndirectRef)
	if !ok {
		t.Fatalf("Mask = %v, want a stencil mask stream", pending(t, alloc, 7).Dict["Mask"])
	}
	mask := pending(t, alloc, ref.ObjectNum)
	if mask.Dict["ImageMask"] != ast.PdfBool(true) {
		t.Errorf("mask dictionary %v, want an image mask (ISO 32000-1, 8.9.6.3)", mask.Dict)
	}
	doc, _ := p.open(t)
	if got := inflate(t, doc, mask); !bytes.Equal(got, []byte{0xa0}) {
		t.Errorf("mask bits = %08b, want 10100000: the two black pixels masked out", got)
	}

	// A soft mask overrides /Mask (Table 89), which is then dropped.
	p.objs[8] = imageStream(3, 1, familyGray, 8, []byte{255, 255, 255}, nil)
	p.objs[7] = imageStream(3, 1, familyRGB, 8, data, ast.PdfDict{"Mask": key, "SMask": ast.PdfIndirectRef{ObjectNum: 8}})
	alloc, _, _, err = p.convert(t, deviceTarget(familyGray), fixedColors(&funcColors{f: mean}))
	if err != nil {
		t.Fatal(err)
	}
	if m, ok := pending(t, alloc, 7).Dict["Mask"]; ok {
		t.Errorf("Mask = %v, want it dropped under the /SMask", m)
	}
}

func TestConvertPixels_ReblendsAMatte(t *testing.T) {
	// The second pixel is black at opacity 128/255, preblended with a white
	// matte (ISO 32000-1, 11.6.5.3): 1 - a, stored as 127.
	p := newTestPage(2, 1, imageStream(2, 1, familyGray, 8, []byte{255, 127}, ast.PdfDict{"SMask": ast.PdfIndirectRef{ObjectNum: 8}}))
	p.objs[8] = imageStream(2, 1, familyGray, 8, []byte{255, 128}, ast.PdfDict{"Matte": ast.PdfArray{ast.PdfInt(1)}})
	square := func(in, out []float64) {
		for i := range out {
			out[i] = in[0] * in[0]
		}
	}
	alloc, _, _, err := p.convert(t, deviceTarget(familyRGB), fixedColors(&funcColors{f: square}))
	if err != nil {
		t.Fatal(err)
	}
	out := pending(t, alloc, 7)
	doc, _ := p.open(t)
	got := inflate(t, doc, out)
	// Unblended, the pixel is black, which squares to black and blends back
	// to 127; converting the stored 127 itself would give 63.
	if got[0] != 255 || got[3] < 126 || got[3] > 128 {
		t.Errorf("samples = %v, want white then 127", got)
	}
	ref, ok := out.Dict["SMask"].(ast.PdfIndirectRef)
	if !ok || ref.ObjectNum == 8 {
		t.Fatalf("SMask = %v, want a copy of the soft mask", out.Dict["SMask"])
	}
	matte, _ := pending(t, alloc, ref.ObjectNum).Dict["Matte"].(ast.PdfArray)
	if len(matte) != 3 || matte[0] != ast.PdfReal(1) {
		t.Errorf("Matte = %v, want the white matte in RGB", matte)
	}
	if _, queued := alloc.Pending()[8]; queued {
		t.Error("the source soft mask was rewritten, which another image may share")
	}
}

func TestConvertXObject_ConvertsASharedImageOnce(t *testing.T) {
	p := newTestPage(1, 1, imageStream(1, 1, familyRGB, 8, []byte{1, 2, 3}, nil))
	alloc, im, _, err := p.convert(t, deviceTarget(familyGray), fixedColors(&funcColors{f: mean}))
	if err != nil {
		t.Fatal(err)
	}
	_, pg := p.open(t)
	if _, err := im.ConvertXObject(context.Background(), imageRef, pg.Resources, pg.Dict); err != nil {
		t.Fatal(err)
	}
	if alloc.Writes() != 1 {
		t.Errorf("%d writes, want the image converted once", alloc.Writes())
	}
	if r := im.Report(); r.Converted != 1 || r.Encoded[""].Images != 1 {
		t.Errorf("report %+v, want one converted image", r)
	}
}

func TestConvertPixels_JPEGBecomesFlate(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := range 16 {
		for x := range 16 {
			src.Set(x, y, color.RGBA{R: uint8(x * 16), G: uint8(y * 16), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, src, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	p := newTestPage(16, 16, imageStream(16, 16, familyRGB, 8, buf.Bytes(), ast.PdfDict{"Filter": ast.PdfName("DCTDecode")}))
	alloc, im, _, err := p.convert(t, deviceTarget(familyGray), fixedColors(&funcColors{f: mean}))
	if err != nil {
		t.Fatal(err)
	}
	out := pending(t, alloc, 7)
	if out.Dict["Filter"] != ast.PdfName("FlateDecode") {
		t.Errorf("Filter = %v, want FlateDecode", out.Dict["Filter"])
	}
	e := im.Report().Encoded["DCTDecode"]
	if e.Images != 1 || e.Before != int64(buf.Len()) || e.After <= 0 {
		t.Errorf("encoded sizes %+v, want the JPEG's %d bytes and the result's", e, buf.Len())
	}
}

func TestConvertPixels_GreyJPEGDeclaredRGB(t *testing.T) {
	src := image.NewGray(image.Rect(0, 0, 8, 8))
	for i := range src.Pix {
		src.Pix[i] = 200
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, src, nil); err != nil {
		t.Fatal(err)
	}
	p := newTestPage(8, 8, imageStream(8, 8, familyRGB, 8, buf.Bytes(), ast.PdfDict{"Filter": ast.PdfName("DCTDecode")}))
	colors := &funcColors{f: func(in, out []float64) { out[0], out[1], out[2], out[3] = 0, 0, 0, 1-in[0] }}
	alloc, _, _, err := p.convert(t, deviceTarget(familyCMYK), fixedColors(colors))
	if err != nil {
		t.Fatal(err)
	}
	if !contains(colors.asked, "DeviceGray") {
		t.Errorf("converters asked for %v, want DeviceGray: the codestream holds one component", colors.asked)
	}
	doc, _ := p.open(t)
	if got := inflate(t, doc, pending(t, alloc, 7)); len(got) != 8*8*4 {
		t.Errorf("%d samples, want 256", len(got))
	}
}

func TestConvertPixels_RefusesWhatItCannotKeep(t *testing.T) {
	cases := map[string]ast.PdfStream{
		"no colour space": imageStream(1, 1, nil, 8, []byte{1, 2, 3}, nil),
		"too large":       imageStream(1<<20, 1<<20, familyRGB, 8, nil, nil),
		"bad depth":       imageStream(1, 1, familyRGB, 3, []byte{1, 2, 3}, nil),
		"no data":         imageStream(4, 4, familyRGB, 8, nil, nil),
		"pattern":         imageStream(1, 1, familyPattern, 8, []byte{1}, nil),
	}
	for name, img := range cases {
		t.Run(name, func(t *testing.T) {
			p := newTestPage(1, 1, img)
			alloc, im, outcome, err := p.convert(t, deviceTarget(familyGray), fixedColors(&funcColors{f: mean}))
			if !errors.Is(err, ErrImageNotConverted) || outcome != ImageUnchanged {
				t.Fatalf("outcome %v, %v; want ErrImageNotConverted", outcome, err)
			}
			if alloc.Writes() != 0 {
				t.Errorf("%d objects queued for a refused image", alloc.Writes())
			}
			if im.Report().Refused != 1 {
				t.Errorf("report %+v, want one refused", im.Report())
			}
		})
	}
}

func TestConvertPixels_RoundTripsThroughCMYK(t *testing.T) {
	data := make([]byte, 0, 4*4*3)
	for i := range 16 {
		data = append(data, byte(i*16), byte(255-i*13), byte(i*i))
	}
	p := newTestPage(4, 4, imageStream(4, 4, familyRGB, 8, data, nil))
	alloc, _, _, err := p.convert(t, deviceTarget(familyCMYK), fixedColors(deviceColors{tc: 4}))
	if err != nil {
		t.Fatal(err)
	}
	back := p.with(alloc)
	alloc, _, _, err = back.convert(t, deviceTarget(familyRGB), fixedColors(deviceColors{tc: 3}))
	if err != nil {
		t.Fatal(err)
	}
	doc, _ := back.open(t)
	got := inflate(t, doc, pending(t, alloc, 7))
	for i := range data {
		if d := int(got[i]) - int(data[i]); d < -1 || d > 1 {
			t.Fatalf("sample %d = %d after the round trip, want %d within 1", i, got[i], data[i])
		}
	}
}

func TestConvertInline_WritesTheTargetSpace(t *testing.T) {
	p := newTestPage(1, 1, imageStream(1, 1, familyGray, 8, []byte{0}, nil))
	doc, pg := p.open(t)
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber(context.Background()))
	cfg := deviceTarget(familyGray)
	cfg.Colors = &funcColors{f: mean}
	im, err := NewImages(doc, alloc, cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	dict := ast.PdfDict{"W": ast.PdfInt(2), "H": ast.PdfInt(1), "CS": ast.PdfName("RGB"), "BPC": ast.PdfInt(8), "I": ast.PdfBool(true)}
	out, data, outcome, err := im.ConvertInline(ctx, dict, []byte{30, 60, 90, 0, 0, 3}, pg.Resources, pg.Dict)
	if err != nil || outcome != ImageConverted {
		t.Fatalf("outcome %v, %v; want converted", outcome, err)
	}
	if out["ColorSpace"] != familyGray || out["Interpolate"] != ast.PdfBool(true) {
		t.Errorf("dictionary %v, want DeviceGray and /Interpolate kept", out)
	}
	stream := out
	stream["Type"], stream["Subtype"] = ast.PdfName("XObject"), ast.PdfName("Image")
	if got := inflate(t, doc, ast.NewStream(stream, data)); !bytes.Equal(got, []byte{60, 1}) {
		t.Errorf("samples = %v, want [60 1]", got)
	}
	text, err := InlineImageOperator(ctx, out, data)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(text), "BI /W 2 /H 1 /CS /DeviceGray /BPC 8 /F ") || !strings.HasSuffix(string(text), "\nEI") {
		t.Errorf("operator = %q", text)
	}
	if bytes.Contains(text[strings.Index(string(text), " ID ")+4:len(text)-3], []byte("EI")) {
		t.Error("the data holds EI, which ends an inline image early")
	}
}

func TestConvertInline_KeepsASpotResource(t *testing.T) {
	tint := ast.PdfDict{"FunctionType": ast.PdfInt(2), "Domain": ast.PdfArray{ast.PdfInt(0), ast.PdfInt(1)},
		"C0": ast.PdfArray{ast.PdfInt(1)}, "C1": ast.PdfArray{ast.PdfInt(0)}, "N": ast.PdfInt(1)}
	p := newTestPage(1, 1, imageStream(1, 1, familyGray, 8, []byte{0}, nil))
	p.objs[5] = ast.PdfDict{"ColorSpace": ast.PdfDict{"CS0": ast.PdfArray{familySeparation, ast.PdfName("Spot"), familyGray, tint}}}
	doc, pg := p.open(t)
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber(context.Background()))
	cfg := deviceTarget(familyGray)
	cfg.Colors = &funcColors{f: mean}
	im, err := NewImages(doc, alloc, cfg)
	if err != nil {
		t.Fatal(err)
	}
	dict := ast.PdfDict{"W": ast.PdfInt(1), "H": ast.PdfInt(1), "CS": ast.PdfName("CS0"), "BPC": ast.PdfInt(8)}
	_, _, outcome, err := im.ConvertInline(context.Background(), dict, []byte{7}, pg.Resources, pg.Dict)
	if err != nil || outcome != ImageUnchanged {
		t.Errorf("outcome %v, %v; want unchanged, the resource converted where it is defined", outcome, err)
	}
}

func TestInlineImageOperator_WrapsDataHoldingEI(t *testing.T) {
	text, err := InlineImageOperator(context.Background(), ast.PdfDict{
		"Width": ast.PdfInt(1), "Height": ast.PdfInt(1), "ColorSpace": familyRGB, "BitsPerComponent": ast.PdfInt(8),
		"Filter": ast.PdfName("FlateDecode"), "DecodeParms": ast.PdfDict{"Predictor": ast.PdfInt(15)},
	}, []byte("xEIy"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(text)
	if !strings.Contains(s, "/F [/ASCIIHexDecode /FlateDecode]") || !strings.Contains(s, "/DP [null <<") || !strings.Contains(s, "ID 78454979>\nEI") {
		t.Errorf("operator = %q, want the data hex-encoded ahead of the Flate filter", s)
	}
}
