package recolor

import (
	"bytes"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// naiveCMYK is what the naive engine gives an RGB colour in CMYK.
func naiveCMYK(rgb ...float64) []float64 {
	out := make([]float64, 4)
	naiveConverter{src: SpaceRGB, dst: SpaceCMYK}.Convert(rgb, out)
	return out
}

func checkNear(t *testing.T, what string, got, want []float64, tol float64) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s: %v, want %v", what, got, want)
		return
	}
	for i := range want {
		if !near(got[i], want[i], tol) {
			t.Errorf("%s: %v, want %v", what, got, want)
			return
		}
	}
}

func TestAxialShadingResampled(t *testing.T) {
	f := onePage("/Resources << /Shading << /Sh0 5 0 R >> >>", "/Sh0 sh",
		"<< /ShadingType 2 /ColorSpace /DeviceRGB /Coords [0 0 40 0] /Background [0 1 0] "+
			"/Function << /FunctionType 2 /Domain [0 1] /C0 [1 0 0] /C1 [0 0 1] /N 1 >> >>")
	r := recolor(t, f, cmyk, naive(Options{}))
	sh := r.dict(ast.PdfIndirectRef{ObjectNum: 5})
	if sh["ColorSpace"] != ast.PdfName("DeviceCMYK") {
		t.Fatalf("shading: %v", sh)
	}
	bg, _ := r.resolve(sh["Background"]).(ast.PdfArray)
	if len(bg) != 4 || bg[0] != ast.PdfInt(1) || bg[1] != ast.PdfInt(0) || bg[2] != ast.PdfInt(1) || bg[3] != ast.PdfInt(0) {
		t.Errorf("background: %v", bg)
	}
	// The naive conversion of the ramp (1-t, 0, t) bends at t = 0.5, between
	// samples 127 and 128 of 256, so the interpolation there is off by a
	// fraction of a sample step.
	for _, x := range []float64{0, 0.2, 0.75, 1} {
		checkNear(t, "ramp", r.evalFunction(sh["Function"], x), naiveCMYK(1-x, 0, x), 1.0/255)
	}
	if r.report.Shadings != 1 {
		t.Errorf("report: %+v", r.report)
	}
}

func TestStitchingKeepsStops(t *testing.T) {
	f := onePage("/Resources << /Shading << /Sh0 5 0 R >> >>", "/Sh0 sh",
		"<< /ShadingType 2 /ColorSpace /DeviceRGB /Coords [0 0 40 0] /Function << /FunctionType 3 /Domain [0 1] /Bounds [0.5] /Encode [0 1 1 0] "+
			"/Functions [<< /FunctionType 2 /Domain [0 1] /C0 [1 0 0] /C1 [1 0 0] /N 1 >> << /FunctionType 2 /Domain [0 1] /C0 [0 0 1] /C1 [0 1 0] /N 1 >>] >> >>")
	r := recolor(t, f, cmyk, naive(Options{}))
	fn := r.dict(r.dict(ast.PdfIndirectRef{ObjectNum: 5})["Function"])
	if fn["FunctionType"] != ast.PdfInt(3) {
		t.Fatalf("function: %v", fn)
	}
	sh := r.dict(ast.PdfIndirectRef{ObjectNum: 5})
	// The stop at 0.5 stays sharp: red up to it, green just past it, as the
	// second part's reversed encoding starts it at C1.
	checkNear(t, "below the stop", r.evalFunction(sh["Function"], 0.499), naiveCMYK(1, 0, 0), sampleTolerance)
	checkNear(t, "past the stop", r.evalFunction(sh["Function"], 0.501), naiveCMYK(0, 0.998, 0.002), 1.0/255)
}

func TestMeshVertexColoursConvertExactly(t *testing.T) {
	data := []byte{
		0, 0, 0, 0xff, 0, 0,
		0, 0x28, 0, 0, 0xff, 0,
		0, 0, 0x28, 0, 0, 0xff,
	}
	f := onePage("/Resources << /Shading << /Sh0 5 0 R >> >>", "/Sh0 sh",
		stream("/ShadingType 4 /ColorSpace /DeviceRGB /BitsPerCoordinate 8 /BitsPerComponent 8 /BitsPerFlag 8 /Decode [0 40 0 40 0 1 0 1 0 1]", string(data)))
	r := recolor(t, f, cmyk, naive(Options{}))
	sh := r.resolve(ast.PdfIndirectRef{ObjectNum: 5}).(ast.PdfStream)
	if sh.Dict["BitsPerComponent"] != ast.PdfInt(16) || len(r.resolve(sh.Dict["Decode"]).(ast.PdfArray)) != 12 {
		t.Fatalf("shading: %v", sh.Dict)
	}
	want := []byte{
		0, 0, 0, 0, 0, 0xff, 0xff, 0xff, 0xff, 0, 0,
		0, 0x28, 0, 0xff, 0xff, 0, 0, 0xff, 0xff, 0, 0,
		0, 0, 0x28, 0xff, 0xff, 0xff, 0xff, 0, 0, 0, 0,
	}
	if got := decode(t, r, sh); !bytes.Equal(got, want) {
		t.Errorf("mesh data:\n% x\nwant:\n% x", got, want)
	}
}

func TestPatchMeshConverts(t *testing.T) {
	// One Coons patch (8.7.4.5.7): a flag, twelve points, four colours, then
	// a truncated second patch the output drops.
	var data []byte
	data = append(data, 0)
	for i := range 24 {
		data = append(data, byte(i))
	}
	data = append(data, 0xff, 0x80, 0x00, 0x00, 0xff)
	data = append(data, 0) // a second flag with nothing after it
	f := onePage("/Resources << /Shading << /Sh0 5 0 R >> >>", "/Sh0 sh",
		stream("/ShadingType 6 /ColorSpace /DeviceGray /BitsPerCoordinate 8 /BitsPerComponent 8 /BitsPerFlag 8 /Decode [0 40 0 40 0 1]", string(data)))
	r := recolor(t, f, rgbTarget, naive(Options{}))
	got := decode(t, r, ast.PdfIndirectRef{ObjectNum: 5})
	if len(got) != 1+24+4*3*2 {
		t.Fatalf("mesh data of %d bytes: % x", len(got), got)
	}
	if !bytes.Equal(got[:25], data[:25]) {
		t.Errorf("flag and points changed: % x", got[:25])
	}
	// Grey 0x80 is 128/255 in each RGB channel, at 16 bits 0x8080.
	if !bytes.Equal(got[31:37], []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x80}) {
		t.Errorf("second colour: % x", got[31:37])
	}
}

func TestMeshUnderFunctionKeepsData(t *testing.T) {
	data := string([]byte{0, 0, 0, 0, 0, 0x28, 0, 0xff, 0, 0, 0x28, 0x80})
	f := onePage("/Resources << /Shading << /Sh0 5 0 R >> >>", "/Sh0 sh",
		stream("/ShadingType 4 /ColorSpace /DeviceGray /BitsPerCoordinate 8 /BitsPerComponent 8 /BitsPerFlag 8 /Decode [0 40 0 40 0 1] "+
			"/Function << /FunctionType 2 /Domain [0 1] /C0 [0] /C1 [1] /N 1 >>", data))
	r := recolor(t, f, rgbTarget, naive(Options{}))
	sh := r.resolve(ast.PdfIndirectRef{ObjectNum: 5}).(ast.PdfStream)
	if got := decode(t, r, sh); string(got) != data {
		t.Errorf("mesh data changed: % x", got)
	}
	checkNear(t, "function", r.evalFunction(sh.Dict["Function"], 0.5), []float64{0.5, 0.5, 0.5}, sampleTolerance)
}

func TestShadingPatternAndTilingPattern(t *testing.T) {
	f := onePage("/Resources << /Pattern << /SP 5 0 R /TP 7 0 R /UP 8 0 R >> >>", "/Pattern cs /SP scn 0 0 9 9 re f /TP scn 0 0 9 9 re f",
		"<< /PatternType 2 /Shading 6 0 R >>",
		"<< /ShadingType 2 /ColorSpace /DeviceGray /Coords [0 0 40 0] /Function << /FunctionType 2 /Domain [0 1] /C0 [0] /C1 [1] /N 1 >> >>",
		stream("/PatternType 1 /PaintType 1 /TilingType 1 /BBox [0 0 5 5] /XStep 5 /YStep 5 /Resources << >>", "0.5 g 0 0 5 5 re f"),
		stream("/PatternType 1 /PaintType 2 /TilingType 1 /BBox [0 0 5 5] /XStep 5 /YStep 5 /Resources << >>", "0.5 g 0 0 5 5 re f"))
	r := recolor(t, f, rgbTarget, naive(Options{}))
	if sh := r.dict(ast.PdfIndirectRef{ObjectNum: 6}); sh["ColorSpace"] != ast.PdfName("DeviceRGB") {
		t.Errorf("pattern shading: %v", sh)
	}
	if got := string(decode(t, r, ast.PdfIndirectRef{ObjectNum: 7})); got != " 0.5 0.5 0.5 rg 0 0 5 5 re f" {
		t.Errorf("coloured tiling pattern: %q", got)
	}
	// An uncoloured cell's colour operators set no colour (ISO 32000-1,
	// 8.7.3.3): they convert, and still set none.
	if got := string(decode(t, r, ast.PdfIndirectRef{ObjectNum: 8})); got != " 0.5 0.5 0.5 rg 0 0 5 5 re f" {
		t.Errorf("uncoloured tiling pattern: %q", got)
	}
}
