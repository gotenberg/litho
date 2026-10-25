package recolor

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics/icc"
)

// intentEngine converts as [naiveEngine] does, and scales every component by
// a factor of the conversion's rendering intent, so a test reads which
// intent each colour was converted under: relative colorimetric 1,
// perceptual 1/2, saturation 1/4 and absolute colorimetric 3/4. Black point
// compensation scales by 1/10 more.
type intentEngine struct{}

func (intentEngine) Transform(src, dst Endpoint, opts Options) (Converter, error) {
	conv, err := naiveEngine{}.Transform(src, dst, opts)
	if err != nil {
		return conv, err
	}
	f := map[icc.Intent]float64{icc.RelativeColorimetric: 1, icc.Perceptual: 0.5, icc.Saturation: 0.25, icc.AbsoluteColorimetric: 0.75}[opts.Intent]
	if opts.BlackPointCompensation {
		f /= 10
	}
	return scaling{conv, f}, nil
}

type scaling struct {
	next Converter
	f    float64
}

func (s scaling) Convert(in, out []float64) {
	s.next.Convert(in, out)
	for i := range out {
		out[i] *= s.f
	}
}

// intentStates are the graphics states the tests paint under.
const intentStates = "/ExtGState << /S << /RI /Saturation >> /R << /RI /RelativeColorimetric >> /A << /CA 1 >> /PB << /RI /Perceptual /UseBlackPtComp /ON >> >>"

func intentOptions(intent icc.Intent) Options {
	return Options{Engine: intentEngine{}, Intent: intent}
}

func TestRenderingIntentSolidColours(t *testing.T) {
	f := onePage("/Resources << "+intentStates+" >>",
		"0.8 g /Perceptual ri 0.8 g q /S gs 0.8 g Q 0.8 g /A gs /Bogus ri 0.8 g /PB gs 0.8 g")
	r := recolor(t, f, rgbTarget, intentOptions(icc.RelativeColorimetric))
	// Each ri or gs that changes the intent writes the colour in force
	// again, converted under it; one that changes nothing stays as it is.
	want := strings.Join([]string{
		"0.8 0.8 0.8 rg",
		"/Perceptual ri", "0.4 0.4 0.4 rg", "0.4 0.4 0.4 rg",
		"q", "/S gs", "0.2 0.2 0.2 rg", "0.2 0.2 0.2 rg", "Q",
		"0.4 0.4 0.4 rg",
		"/A gs",
		// ISO 32000-1, 8.6.5.8: a name it does not define selects relative
		// colorimetric.
		"/Bogus ri", "0.8 0.8 0.8 rg", "0.8 0.8 0.8 rg",
		// Both the compensation and the intent change: one colour, under
		// both.
		"/PB gs", "0.04 0.04 0.04 rg", "0.04 0.04 0.04 rg",
	}, "\n")
	if got := r.text(0); got != want {
		t.Errorf("content:\n%s\nwant:\n%s", got, want)
	}
	if got := r.report.Intent.Operators; got != 7 {
		t.Errorf("Intent.Operators = %d, want 7", got)
	}
	if got := r.report.BlackPoint.Operators; got != 2 {
		t.Errorf("BlackPoint.Operators = %d, want 2", got)
	}
}

func TestRenderingIntentContentOverridesTheOption(t *testing.T) {
	f := onePage("/Resources << "+intentStates+" >>", "0.8 g /R gs 0 0 1 1 re f")
	r := recolor(t, f, rgbTarget, intentOptions(icc.Perceptual))
	if got, want := r.text(0), "0.4 0.4 0.4 rg\n/R gs\n0.8 0.8 0.8 rg\n0 0 1 1 re\nf"; got != want {
		t.Errorf("content:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderingIntentImageCopies(t *testing.T) {
	gray := "/Type /XObject /Subtype /Image /Width 1 /Height 1 /BitsPerComponent 8 /ColorSpace /DeviceGray"
	f := onePage("/Resources << "+intentStates+" /XObject << /Im 5 0 R /Own 6 0 R >> >>",
		"/Im Do /Own Do /Perceptual ri /Im Do /Own Do /Im Do /PB gs /Im Do /Own Do",
		stream(gray, "\xc8"),
		stream(gray+" /Intent /Saturation", "\xc8"))
	r := recolor(t, f, rgbTarget, intentOptions(icc.RelativeColorimetric))
	// An image with its own /Intent converts under it everywhere (ISO
	// 32000-1, 8.6.5.8), and is copied for a state's compensation alone.
	want := "/Im Do\n/Own Do\n/Perceptual ri\n/Im_Perceptual Do\n/Own Do\n/Im_Perceptual Do\n/PB gs\n/Im_BPCON_Perceptual Do\n/Own_BPCON Do"
	if got := r.text(0); got != want {
		t.Errorf("content:\n%s\nwant:\n%s", got, want)
	}
	if r.report.Intent.Copies != 2 || r.report.BlackPoint.Copies != 2 {
		t.Errorf("copies: intent %d, black point %d; want 2 and 2", r.report.Intent.Copies, r.report.BlackPoint.Copies)
	}
	xobjects := r.dict(r.dict(r.pages[0].Dict["Resources"])["XObject"])
	entry := func(name ast.PdfName) byte {
		t.Helper()
		cs, ok := r.dict(xobjects[name])["ColorSpace"].(ast.PdfArray)
		if !ok || len(cs) != 4 {
			t.Fatalf("%s: %v, want an Indexed space", name, r.dict(xobjects[name])["ColorSpace"])
		}
		return string(r.resolve(cs[3]).(ast.PdfString))[200*3]
	}
	for name, want := range map[ast.PdfName]byte{"Im": 200, "Own": 50, "Im_Perceptual": 100, "Im_BPCON_Perceptual": 10, "Own_BPCON": 5} {
		if got := entry(name); got != want {
			t.Errorf("%s: entry 200 reads %d, want %d", name, got, want)
		}
	}
}

func TestRenderingIntentShadingCopy(t *testing.T) {
	f := onePage("/Resources << /Shading << /Sh << /ShadingType 2 /ColorSpace /DeviceGray /Coords [0 0 1 0] /Function << /FunctionType 2 /Domain [0 1] /C0 [0] /C1 [1] /N 1 >> >> >> >>",
		"/Sh sh /AbsoluteColorimetric ri /Sh sh")
	r := recolor(t, f, rgbTarget, intentOptions(icc.RelativeColorimetric))
	if got, want := r.text(0), "/Sh sh\n/AbsoluteColorimetric ri\n/Sh_AbsoluteColorimetric sh"; got != want {
		t.Errorf("content:\n%s\nwant:\n%s", got, want)
	}
	if r.report.Intent.Copies != 1 {
		t.Errorf("Intent.Copies = %d, want 1", r.report.Intent.Copies)
	}
}

func TestRenderingIntentInlineImage(t *testing.T) {
	img := "BI /W 1 /H 1 /CS /G /BPC 8 ID \xc8 EI"
	own := "BI /W 1 /H 1 /CS /G /BPC 8 /Intent /RelativeColorimetric ID \xc8 EI"
	under := recolor(t, onePage("", "/Perceptual ri "+img+" "+own), rgbTarget, intentOptions(icc.RelativeColorimetric))
	option := recolor(t, onePage("", img), rgbTarget, intentOptions(icc.Perceptual))
	plain := recolor(t, onePage("", img), rgbTarget, intentOptions(icc.RelativeColorimetric))
	got, want, rel := spell(under.ops(0)[1]), spell(option.ops(0)[0]), spell(plain.ops(0)[0])
	if got != want || got == rel {
		t.Errorf("inline image under /Perceptual ri:\n%q\nwant it converted as under the option:\n%q\nnot as without it:\n%q", got, want, rel)
	}
	if ownGot := spell(under.ops(0)[2]); strings.Replace(ownGot, " /Intent /RelativeColorimetric", "", 1) != rel {
		t.Errorf("inline image with its own /Intent:\n%q\nwant it converted under that intent:\n%q", ownGot, rel)
	}
	if under.report.Intent.InlineImages != 1 {
		t.Errorf("Intent.InlineImages = %d, want 1", under.report.Intent.InlineImages)
	}
}

func TestRenderingIntentFormUnhonoured(t *testing.T) {
	f := onePage("/Resources << /XObject << /F 5 0 R >> >>", "/Saturation ri /F Do",
		stream("/Type /XObject /Subtype /Form /BBox [0 0 9 9] /Resources << >>", "0.8 g 0 0 9 9 re f"))
	r := recolor(t, f, rgbTarget, intentOptions(icc.RelativeColorimetric))
	// The form converts once, under the option, for every painting.
	if got := string(decode(t, r, ast.PdfIndirectRef{ObjectNum: 5})); got != " 0.8 0.8 0.8 rg 0 0 9 9 re f" {
		t.Errorf("form: %q", got)
	}
	if r.report.Intent.Unhonoured != 1 || r.report.BlackPoint.Unhonoured != 0 {
		t.Errorf("unhonoured: intent %d, black point %d; want 1 and 0", r.report.Intent.Unhonoured, r.report.BlackPoint.Unhonoured)
	}
}

func TestRenderingIntentOfTheOptionChangesNothing(t *testing.T) {
	// An ri naming the option's intent, and a gs without /RI, neither write
	// the colour again nor copy.
	f := onePage("/Resources << "+intentStates+" /XObject << /Im 5 0 R >> >>", "0.8 g /RelativeColorimetric ri /A gs /R gs /Im Do",
		stream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /BitsPerComponent 8 /ColorSpace /DeviceGray", "\xc8"))
	r := recolor(t, f, rgbTarget, intentOptions(icc.RelativeColorimetric))
	if got := r.text(0); got != "0.8 0.8 0.8 rg\n/RelativeColorimetric ri\n/A gs\n/R gs\n/Im Do" {
		t.Errorf("content: %q", got)
	}
	if r.report.Intent != (IntentReport{}) {
		t.Errorf("Intent = %+v, want zero", r.report.Intent)
	}
}
