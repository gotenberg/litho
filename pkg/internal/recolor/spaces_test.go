package recolor

import (
	"context"
	"errors"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics/function"
)

// evalFunction compiles a function of the output and evaluates it.
func (r result) evalFunction(obj ast.PdfObject, in ...float64) []float64 {
	r.t.Helper()
	fn, err := function.New(context.Background(), r.doc, obj)
	if err != nil {
		r.t.Fatalf("function.New: %v", err)
	}
	out, err := fn.Eval(in)
	if err != nil {
		r.t.Fatalf("Eval: %v", err)
	}
	return out
}

// colorSpace returns the output's definition of the /ColorSpace resource
// name on page 0.
func (r result) colorSpace(name ast.PdfName) ast.PdfArray {
	r.t.Helper()
	spaces := r.dict(r.dict(r.pages[0].Dict["Resources"])["ColorSpace"])
	arr, ok := r.resolve(spaces[name]).(ast.PdfArray)
	if !ok {
		r.t.Fatalf("/%s is %v", name, spaces[name])
	}
	return arr
}

// sampleTolerance is how far a 16-bit sample of a resampled function may be
// from the exact value: half a step of rounding, plus interpolation between
// samples of a transform that is linear here.
const sampleTolerance = 2.0 / 65535

func TestIndexedConvertsPaletteKeepsIndices(t *testing.T) {
	f := onePage("/Resources << /ColorSpace << /I0 [/Indexed /DeviceRGB 1 <FF000000FF00>] >> >>",
		"/I0 cs 1 sc 0 0 10 10 re f")
	r := recolor(t, f, cmyk, naive(Options{}))
	if got, want := r.text(0), "/I0 cs\n1 sc\n0 0 10 10 re\nf"; got != want {
		t.Errorf("content:\n%s\nwant:\n%s", got, want)
	}
	cs := r.colorSpace("I0")
	if cs[1] != ast.PdfName("DeviceCMYK") || cs[2] != ast.PdfInt(1) {
		t.Fatalf("definition: %v", cs)
	}
	if got, want := string(cs[3].(ast.PdfString)), "\x00\xff\xff\x00\xff\x00\xff\x00"; got != want {
		t.Errorf("palette %x, want %x", got, want)
	}
	if r.report.Spaces != 1 {
		t.Errorf("report: %+v", r.report)
	}
}

func TestIndexedPaletteStream(t *testing.T) {
	f := onePage("/Resources << /ColorSpace << /I0 [/Indexed /DeviceGray 1 5 0 R] >> >>",
		"/I0 cs 0 sc", stream("", "\x00\xff"))
	r := recolor(t, f, rgbTarget, naive(Options{}))
	cs := r.colorSpace("I0")
	if got, want := string(cs[3].(ast.PdfString)), "\x00\x00\x00\xff\xff\xff"; got != want {
		t.Errorf("palette %x, want %x", got, want)
	}
}

func TestSeparationKeepsColorantConvertsAlternate(t *testing.T) {
	f := onePage("/Resources << /ColorSpace << /S0 [/Separation /Spot /DeviceRGB 5 0 R] >> >>",
		"/S0 cs 0.5 sc 0 0 10 10 re f",
		"<< /FunctionType 2 /Domain [0 1] /C0 [1 1 1] /C1 [1 0 0] /N 1 >>")
	r := recolor(t, f, cmyk, naive(Options{}))
	if got, want := r.text(0), "/S0 cs\n0.5 sc\n0 0 10 10 re\nf"; got != want {
		t.Errorf("content:\n%s\nwant:\n%s", got, want)
	}
	cs := r.colorSpace("S0")
	if cs[0] != ast.PdfName("Separation") || cs[1] != ast.PdfName("Spot") || cs[2] != ast.PdfName("DeviceCMYK") {
		t.Fatalf("definition: %v", cs)
	}
	for _, tint := range []float64{0, 0.25, 0.5, 1} {
		got := r.evalFunction(cs[3], tint)
		// The tint gives RGB (1, 1-t, 1-t), whose naive CMYK is (0, t, t, 0).
		want := []float64{0, tint, tint, 0}
		for i := range want {
			if !near(got[i], want[i], sampleTolerance) {
				t.Errorf("tint %v: %v, want %v", tint, got, want)
				break
			}
		}
	}
	if r.report.SpotsKept != 1 {
		t.Errorf("report: %+v", r.report)
	}
}

func TestSeparationAlternateAlreadyTarget(t *testing.T) {
	f := onePage("/Resources << /ColorSpace << /S0 [/Separation /Spot /DeviceCMYK 5 0 R] >> >>",
		"/S0 cs 0.5 sc",
		"<< /FunctionType 2 /Domain [0 1] /C0 [0 0 0 0] /C1 [0 1 1 0] /N 1 >>")
	r := recolor(t, f, cmyk, naive(Options{}))
	if n := len(r.alloc.Pending()); n != 0 {
		t.Errorf("%d objects written for a spot whose alternate is the target", n)
	}
}

func TestConvertSpots(t *testing.T) {
	f := onePage("/Resources << /ColorSpace << /S0 [/Separation /Spot /DeviceRGB 5 0 R] /N0 [/Separation /None /DeviceRGB 5 0 R] >> >>",
		"/S0 cs 0.5 sc /S0 CS /N0 cs 1 sc",
		"<< /FunctionType 2 /Domain [0 1] /C0 [1 1 1] /C1 [1 0 0] /N 1 >>")
	r := recolor(t, f, cmyk, naive(Options{ConvertSpots: true}))
	// cs takes the initial tint 1.0 (8.6.6.4), here red; the None colorant
	// paints nothing and stays.
	want := "0 1 1 0 k\n0 0.5 0.5 0 k\n0 1 1 0 K\n/N0 cs\n1 sc"
	if got := r.text(0); got != want {
		t.Errorf("content:\n%s\nwant:\n%s", got, want)
	}
}

func TestDeviceNKept(t *testing.T) {
	f := onePage("/Resources << /ColorSpace << /D0 [/DeviceN [/Cyan /Spot] /DeviceRGB 5 0 R] >> >>",
		"/D0 cs 0.5 1 sc",
		stream("/FunctionType 4 /Domain [0 1 0 1] /Range [0 1 0 1 0 1]", "{ pop 1 exch sub dup dup }"))
	r := recolor(t, f, grayTarget, naive(Options{}))
	cs := r.colorSpace("D0")
	if cs[2] != ast.PdfName("DeviceGray") {
		t.Fatalf("definition: %v", cs)
	}
	// The transform gives grey RGB (1-c, 1-c, 1-c), which is its own luma.
	for _, c := range []float64{0, 0.5, 1} {
		if got := r.evalFunction(cs[3], c, 0.3); !near(got[0], 1-c, sampleTolerance) {
			t.Errorf("tints (%v, 0.3): %v, want %v", c, got, 1-c)
		}
	}
}

func TestUncolouredPatternBase(t *testing.T) {
	f := onePage("/Resources << /ColorSpace << /P0 [/Pattern /DeviceRGB] >> >>",
		"/P0 cs 1 0 0 /Pat scn /P0 CS 0 0 1 /Pat SCN /Pattern cs /Col scn")
	r := recolor(t, f, cmyk, naive(Options{}))
	want := "/P0 cs\n0 1 1 0 /Pat scn\n/P0 CS\n1 1 0 0 /Pat SCN\n/Pattern cs\n/Col scn"
	if got := r.text(0); got != want {
		t.Errorf("content:\n%s\nwant:\n%s", got, want)
	}
	cs := r.colorSpace("P0")
	if len(cs) != 2 || cs[0] != ast.PdfName("Pattern") || cs[1] != ast.PdfName("DeviceCMYK") {
		t.Errorf("definition: %v", cs)
	}
}

func TestCIESpaces(t *testing.T) {
	// D65 white, the white point of sRGB.
	wp := "/WhitePoint [0.9505 1 1.089]"
	f := onePage("/Resources << /ColorSpace << /L [/Lab << "+wp+" >>] /C [/CalRGB << "+wp+
		" /Gamma [2.2 2.2 2.2] /Matrix [0.4124 0.2126 0.0193 0.3576 0.7152 0.1192 0.1805 0.0722 0.9505] >>] /G [/CalGray << "+wp+" >>] >> >>",
		"/L cs 100 0 0 sc /C cs 1 1 1 sc /C cs 1 0 0 sc /G cs 0.5 sc")
	r := recolor(t, f, rgbTarget, naive(Options{}))
	ops := r.ops(0)
	// Each cs writes its initial colour, then each sc its colour.
	check := func(i int, want []float64, tol float64) {
		t.Helper()
		op := ops[i]
		if op.Operator != "rg" {
			t.Fatalf("operation %d: %s", i, spell(op))
		}
		for j, w := range want {
			if v, _ := number2(op.Operands[j]); !near(v, w, tol) {
				t.Errorf("operation %d: %s, want %v", i, spell(op), want)
				return
			}
		}
	}
	check(1, []float64{1, 1, 1}, 0.002)
	check(3, []float64{1, 1, 1}, 0.002)
	// CalRGB with the sRGB primaries and a 2.2 gamma: red stays red.
	check(5, []float64{1, 0, 0}, 0.01)
	// CalGray 0.5 at gamma 1 is a luminance of 0.5: sRGB 0.735.
	check(7, []float64{0.7354, 0.7354, 0.7354}, 0.003)
}

func number2(o ast.PdfObject) (float64, bool) {
	switch v := o.(type) {
	case ast.PdfInt:
		return float64(v), true
	case ast.PdfReal:
		return float64(v), true
	}
	return 0, false
}

func TestICCBasedSourceAndFallback(t *testing.T) {
	f := onePage("/Resources << /ColorSpace << /RGB [/ICCBased 5 0 R] /Bad [/ICCBased 6 0 R] >> >>",
		"/RGB cs 0 0 1 sc /Bad cs 0.5 sc",
		stream("/N 3", string(builtinRGB)),
		stream("/N 1 /Alternate /DeviceGray", "not a profile"))
	r := recolor(t, f, cmyk, naive(Options{}))
	// The unreadable profile reads as its /Alternate (8.6.5.5).
	want := "0 0 0 1 k\n1 1 0 0 k\n0 0 0 1 k\n0 0 0 0.5 k"
	if got := r.text(0); got != want {
		t.Errorf("content:\n%s\nwant:\n%s", got, want)
	}
}

// TestICCBasedFallbackFollowsRenderer checks that an unusable ICCBased
// profile gives way where the renderer's reading does
// ([pdfimage.SubstituteICCAlternate]): as a Separation's alternate, its own
// /Alternate may not be another Separation (ISO 32000-1, 8.6.6.4), so it
// reads as DeviceGray, the device space of its /N (8.6.5.5, Table 66), and
// the inner spot's tint transform is never run.
func TestICCBasedFallbackFollowsRenderer(t *testing.T) {
	f := onePage("/Resources << /ColorSpace << /S0 [/Separation /Spot [/ICCBased 6 0 R] 5 0 R] >> >>",
		"/S0 cs 0.5 sc",
		"<< /FunctionType 2 /Domain [0 1] /C0 [0] /C1 [1] /N 1 >>",
		stream("/N 1 /Alternate [/Separation /Inner /DeviceCMYK 7 0 R]", "not a profile"),
		"<< /FunctionType 2 /Domain [0 1] /C0 [0 0 0 0] /C1 [0 1 1 0] /N 1 >>")
	r := recolor(t, f, cmyk, naive(Options{ConvertSpots: true}))
	want := "0 0 0 0 k\n0 0 0 0.5 k"
	if got := r.text(0); got != want {
		t.Errorf("content:\n%s\nwant:\n%s", got, want)
	}
}

func TestDefaultSpaces(t *testing.T) {
	// DefaultRGB stands in for DeviceRGB (8.6.5.6): here a CalRGB whose
	// first component is the sRGB blue primary, so rg's red paints blue.
	// DefaultCMYK would reinterpret the target's own operators, so it is
	// removed, and DeviceCMYK colour converts from it to the target, whose
	// profile here is another.
	f := onePage("/Resources << /ColorSpace << /DefaultRGB [/CalRGB << /WhitePoint [0.9505 1 1.089] /Gamma [2.2 2.2 2.2] "+
		"/Matrix [0.1805 0.0722 0.9505 0.3576 0.7152 0.1192 0.4124 0.2126 0.0193] >>] /DefaultCMYK [/ICCBased 5 0 R] >> >>",
		"1 0 0 rg 0.5 0.5 0.5 0 k",
		stream("/N 4", string(builtinCMYK)))
	target := Target{Family: "DeviceCMYK", Profile: []byte("another CMYK profile")}
	r := recolor(t, f, target, naive(Options{}))
	ops := r.ops(0)
	if len(ops) != 2 || ops[0].Operator != "k" || ops[1].Operator != "k" {
		t.Fatalf("content:\n%s", r.text(0))
	}
	if c, _ := number2(ops[0].Operands[0]); c < 0.95 {
		t.Errorf("DefaultRGB not applied: %s", spell(ops[0]))
	}
	if got := spell(ops[1]); got != "0 0 0 0.5 k" {
		t.Errorf("DefaultCMYK colour: %s", got)
	}
	spaces := r.dict(r.dict(r.pages[0].Dict["Resources"])["ColorSpace"])
	if _, ok := spaces["DefaultCMYK"]; ok {
		t.Errorf("DefaultCMYK kept: %v", spaces)
	}
	if _, ok := spaces["DefaultRGB"]; !ok {
		t.Errorf("DefaultRGB removed: %v", spaces)
	}
}

func TestUnknownSpaceReported(t *testing.T) {
	f := onePage("/Resources << /ColorSpace << /X [/Nonsense] >> >>", "/X cs 1 sc 1 0 0 rg")
	r := recolor(t, f, cmyk, naive(Options{}))
	if got, want := r.text(0), "/X cs\n1 sc\n0 1 1 0 k"; got != want {
		t.Errorf("content:\n%s\nwant:\n%s", got, want)
	}
	if len(r.report.Skipped) != 1 || !errors.Is(r.report.Skipped[0].Err, ErrUnsupportedSpace) {
		t.Errorf("skipped: %+v", r.report.Skipped)
	}
}

func TestInvalidTarget(t *testing.T) {
	doc, pages := open(t, onePage("", "").bytes())
	for _, target := range []Target{{Family: "Lab"}, {Family: "ICCBased"}, {Family: "ICCBased", Profile: []byte("x"), ColorSpace: iccRef(9)}} {
		if _, err := Content(context.Background(), doc, nil, pages, target, Options{}); !errors.Is(err, ErrUnsupportedTarget) {
			t.Errorf("%+v: %v", target.Family, err)
		}
	}
}
