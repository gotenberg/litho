package recolor

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// bpcEngine converts as [naiveEngine] does, and halves every component of a
// conversion built with black point compensation, so a test reads which
// compensation each colour was converted under.
type bpcEngine struct{}

func (bpcEngine) Transform(src, dst Endpoint, opts Options) (Converter, error) {
	conv, err := naiveEngine{}.Transform(src, dst, opts)
	if err != nil || !opts.BlackPointCompensation {
		return conv, err
	}
	return halving{conv}, nil
}

type halving struct{ next Converter }

func (h halving) Convert(in, out []float64) {
	h.next.Convert(in, out)
	for i := range out {
		out[i] /= 2
	}
}

// bpcStates are the graphics states the tests paint under.
const bpcStates = "/ExtGState << /On << /UseBlackPtComp /ON >> /Off << /UseBlackPtComp /OFF >> /Def << /UseBlackPtComp /Default >> /Odd << /UseBlackPtComp /Maybe >> >>"

func bpcOptions(on bool) Options {
	return Options{Engine: bpcEngine{}, BlackPointCompensation: on}
}

func TestUseBlackPtCompSolidColours(t *testing.T) {
	f := onePage("/Resources << "+bpcStates+" >>",
		"0.5 g /On gs 0.5 g q /Off gs 0.5 g Q 0.5 g /Def gs 0.5 g /On gs /Odd gs 0.5 g")
	r := recolor(t, f, rgbTarget, bpcOptions(false))
	// Each gs that changes the compensation writes the colour in force
	// again, converted under it, before the 0.5 g that follows.
	want := strings.Join([]string{
		"0.5 0.5 0.5 rg",
		"/On gs", "0.25 0.25 0.25 rg", "0.25 0.25 0.25 rg",
		"q", "/Off gs", "0.5 0.5 0.5 rg", "0.5 0.5 0.5 rg", "Q",
		"0.25 0.25 0.25 rg",
		"/Def gs", "0.5 0.5 0.5 rg", "0.5 0.5 0.5 rg",
		// A value ISO 32000-2, Table 57 does not define leaves /ON in force.
		"/On gs", "0.25 0.25 0.25 rg", "/Odd gs", "0.25 0.25 0.25 rg",
	}, "\n")
	if got := r.text(0); got != want {
		t.Errorf("content:\n%s\nwant:\n%s", got, want)
	}
	if got := r.report.BlackPoint.Operators; got != 5 {
		t.Errorf("BlackPoint.Operators = %d, want 5", got)
	}
}

func TestUseBlackPtCompOffOverridesTheOption(t *testing.T) {
	f := onePage("/Resources << "+bpcStates+" >>", "/DeviceGray cs 0.5 sc /Off gs 0 0 1 1 re f")
	r := recolor(t, f, rgbTarget, bpcOptions(true))
	// cs writes the initial black, then sc its colour, both compensated.
	want := "0 0 0 rg\n0.25 0.25 0.25 rg\n/Off gs\n0.5 0.5 0.5 rg\n0 0 1 1 re\nf"
	if got := r.text(0); got != want {
		t.Errorf("content:\n%s\nwant:\n%s", got, want)
	}
}

func TestUseBlackPtCompPatternBase(t *testing.T) {
	f := onePage("/Resources << "+bpcStates+" /ColorSpace << /P [/Pattern /DeviceGray] >> /Pattern << /T 5 0 R >> >>",
		"/P cs 0.5 /T scn /On gs 0 0 1 1 re f",
		stream("/PatternType 1 /PaintType 2 /TilingType 1 /BBox [0 0 1 1] /XStep 1 /YStep 1 /Resources << >>", "0 0 1 1 re f"))
	r := recolor(t, f, rgbTarget, bpcOptions(false))
	want := "/P cs\n0.5 0.5 0.5 /T scn\n/On gs\n0.25 0.25 0.25 /T scn\n0 0 1 1 re\nf"
	if got := r.text(0); got != want {
		t.Errorf("content:\n%s\nwant:\n%s", got, want)
	}
}

func TestUseBlackPtCompImageCopies(t *testing.T) {
	// Pages 3 and 5 share the resources, object 7, naming image 8: the first
	// paints it under /Default, the second under /ON and /OFF.
	f := fixture{
		pages: []fixturePage{
			{dict: "/Resources 7 0 R", content: "/Im Do"},
			{dict: "/Resources 7 0 R", content: "/On gs /Im Do /Im Do /Off gs /Im Do"},
		},
		objs: []string{
			"<< /XObject << /Im 8 0 R /Im_BPCON 9 0 R >> " + bpcStates + " >>",
			stream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /BitsPerComponent 8 /ColorSpace /DeviceGray", "\xc8"),
			stream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /BitsPerComponent 1 /ImageMask true", "\x00"),
		},
	}
	r := recolor(t, f, rgbTarget, bpcOptions(false))
	if got := r.text(0); got != "/Im Do" {
		t.Errorf("page 1: %q", got)
	}
	// Im_BPCON is taken, so the copy is named Im_BPCON1, once for both
	// paintings; /OFF paints the image's own conversion.
	if got, want := r.text(1), "/On gs\n/Im_BPCON1 Do\n/Im_BPCON1 Do\n/Off gs\n/Im Do"; got != want {
		t.Errorf("page 2:\n%s\nwant:\n%s", got, want)
	}
	if got := r.report.BlackPoint.Copies; got != 1 {
		t.Errorf("BlackPoint.Copies = %d, want 1", got)
	}
	shared := r.dict(r.dict(ast.PdfIndirectRef{ObjectNum: 7})["XObject"])
	if _, ok := shared["Im_BPCON1"]; ok {
		t.Error("the shared resources gained the copy, which only page 2 paints")
	}
	page2 := r.dict(r.pages[1].Dict["Resources"])
	xobjects := r.dict(page2["XObject"])
	for _, name := range []ast.PdfName{"Im", "Im_BPCON", "Im_BPCON1"} {
		if xobjects[name] == nil {
			t.Errorf("page 2's resources lack %s", name)
		}
	}
	if page2["ExtGState"] == nil {
		t.Error("page 2's resources lost their graphics states")
	}
	// A grey image converts to an Indexed image over the target, its
	// samples kept and its colour table converted (see
	// [Images.convertToIndexed]).
	own := r.dict(ast.PdfIndirectRef{ObjectNum: 8})["ColorSpace"].(ast.PdfArray)
	copied := r.dict(xobjects["Im_BPCON1"])["ColorSpace"].(ast.PdfArray)
	ownTable, copyTable := string(r.resolve(own[3]).(ast.PdfString)), string(r.resolve(copied[3]).(ast.PdfString))
	if ownTable[200*3] != 200 || copyTable[200*3] != 100 {
		t.Errorf("entry 200: own %d, copy %d; want 200 and its half", ownTable[200*3], copyTable[200*3])
	}
}

func TestUseBlackPtCompSharedStreamDefinesTheCopy(t *testing.T) {
	// Pages 3 and 5 show stream 7, which names the copy; each has
	// resources of its own, and both must define it.
	res := "/Resources << /XObject << /Im 8 0 R >> " + bpcStates + " >>"
	f := fixture{
		pages: []fixturePage{{dict: res, contents: "[7 0 R]"}, {dict: res, contents: "[7 0 R]"}},
		objs: []string{
			stream("", "/On gs /Im Do"),
			stream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /BitsPerComponent 8 /ColorSpace /DeviceGray", "\xc8"),
		},
	}
	r := recolor(t, f, rgbTarget, bpcOptions(false))
	for i := range r.pages {
		if got := r.text(i); got != "/On gs\n/Im_BPCON Do" {
			t.Errorf("page %d: %q", i+1, got)
		}
		xobjects := r.dict(r.dict(r.pages[i].Dict["Resources"])["XObject"])
		if xobjects["Im_BPCON"] == nil {
			t.Errorf("page %d's resources lack the copy", i+1)
		}
	}
}

func TestUseBlackPtCompShadingCopy(t *testing.T) {
	f := onePage("/Resources << "+bpcStates+" /Shading << /Sh << /ShadingType 2 /ColorSpace /DeviceGray /Coords [0 0 1 0] /Function << /FunctionType 2 /Domain [0 1] /C0 [0] /C1 [1] /N 1 >> >> >> >>",
		"/Sh sh /On gs /Sh sh")
	r := recolor(t, f, rgbTarget, bpcOptions(false))
	if got, want := r.text(0), "/Sh sh\n/On gs\n/Sh_BPCON sh"; got != want {
		t.Errorf("content:\n%s\nwant:\n%s", got, want)
	}
	shadings := r.dict(r.dict(r.pages[0].Dict["Resources"])["Shading"])
	copied := r.dict(shadings["Sh_BPCON"])
	if copied["ColorSpace"] != ast.PdfName("DeviceRGB") {
		t.Errorf("copy: %v", copied)
	}
	if r.report.BlackPoint.Copies != 1 || r.report.Shadings != 1 {
		t.Errorf("report: copies %d, shadings %d; want 1 and 1", r.report.BlackPoint.Copies, r.report.Shadings)
	}
}

func TestUseBlackPtCompInlineImage(t *testing.T) {
	img := "BI /W 1 /H 1 /CS /G /BPC 8 ID \xc8 EI"
	under := recolor(t, onePage("/Resources << "+bpcStates+" >>", "/On gs "+img), rgbTarget, bpcOptions(false))
	option := recolor(t, onePage("", img), rgbTarget, bpcOptions(true))
	plain := recolor(t, onePage("", img), rgbTarget, bpcOptions(false))
	got, want, off := spell(under.ops(0)[1]), spell(option.ops(0)[0]), spell(plain.ops(0)[0])
	if got != want || got == off {
		t.Errorf("inline image under /ON:\n%q\nwant it converted as under the option:\n%q\nnot as without it:\n%q", got, want, off)
	}
	if under.report.BlackPoint.InlineImages != 1 {
		t.Errorf("BlackPoint.InlineImages = %d, want 1", under.report.BlackPoint.InlineImages)
	}
}

func TestUseBlackPtCompFormUnhonoured(t *testing.T) {
	f := onePage("/Resources << "+bpcStates+" /XObject << /F 5 0 R >> >>", "/On gs /F Do",
		stream("/Type /XObject /Subtype /Form /BBox [0 0 9 9] /Resources << >>", "0.5 g 0 0 9 9 re f"))
	r := recolor(t, f, rgbTarget, bpcOptions(false))
	// The form converts once, under the option, for every painting.
	if got := string(decode(t, r, ast.PdfIndirectRef{ObjectNum: 5})); got != " 0.5 0.5 0.5 rg 0 0 9 9 re f" {
		t.Errorf("form: %q", got)
	}
	if r.report.BlackPoint.Unhonoured != 1 {
		t.Errorf("BlackPoint.Unhonoured = %d, want 1", r.report.BlackPoint.Unhonoured)
	}
}

func TestUseBlackPtCompAbsentChangesNothing(t *testing.T) {
	// Without the entry, a gs neither writes the colour again nor copies.
	f := onePage("/Resources << /ExtGState << /A << /CA 1 >> >> /XObject << /Im 5 0 R >> >>", "0.5 g /A gs /Im Do",
		stream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /BitsPerComponent 8 /ColorSpace /DeviceGray", "\xc8"))
	r := recolor(t, f, rgbTarget, bpcOptions(false))
	if got := r.text(0); got != "0.5 0.5 0.5 rg\n/A gs\n/Im Do" {
		t.Errorf("content: %q", got)
	}
	if r.report.BlackPoint != (BlackPointReport{}) {
		t.Errorf("BlackPoint = %+v, want zero", r.report.BlackPoint)
	}
}

func TestUseBlackPtCompNoOwnResources(t *testing.T) {
	// The form has no /Resources: it paints with the page's, which a copy's
	// name cannot be added to for the form alone.
	f := onePage("/Resources << "+bpcStates+" /XObject << /F 5 0 R /Im 6 0 R >> >>", "/F Do",
		stream("/Type /XObject /Subtype /Form /BBox [0 0 9 9]", "/On gs /Im Do"),
		stream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /BitsPerComponent 8 /ColorSpace /DeviceGray", "\xc8"))
	r := recolor(t, f, rgbTarget, bpcOptions(false))
	if r.report.BlackPoint.Unhonoured != 1 || r.report.BlackPoint.Copies != 0 {
		t.Errorf("BlackPoint = %+v, want one unhonoured painting and no copy", r.report.BlackPoint)
	}
	if n := len(r.alloc.Pending()); n != 1 {
		t.Errorf("%d objects written, want the image alone", n)
	}
}
