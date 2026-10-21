package recolor

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

func TestSharedFormConvertedOnce(t *testing.T) {
	// Pages are objects 3 and 5, their content 4 and 6; the form is 7.
	res := "/Resources << /XObject << /F 7 0 R >> >>"
	f := fixture{
		pages: []fixturePage{{dict: res, content: "/F Do"}, {dict: res, content: "q /F Do Q"}},
		objs:  []string{stream("/Type /XObject /Subtype /Form /BBox [0 0 9 9] /Resources << >>", "1 0 0 rg 0 0 9 9 re f")},
	}
	r := recolor(t, f, cmyk, naive(Options{}))
	if got := string(decode(t, r, ast.PdfIndirectRef{ObjectNum: 7})); got != " 0 1 1 0 k 0 0 9 9 re f" {
		t.Errorf("form: %q", got)
	}
	// The form's stream is replaced at its number, and nothing else is
	// written: the pages' content and resources hold no colour of their own.
	if pending := r.alloc.Pending(); len(pending) != 1 || pending[7] == nil {
		t.Errorf("written objects: %v", keys(pending))
	}
	if r.report.Streams != 1 {
		t.Errorf("report: %+v", r.report)
	}
}

func keys(m map[int]ast.PdfObject) []int {
	var out []int
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestFormCycleEnds(t *testing.T) {
	f := onePage("/Resources << /XObject << /F 5 0 R >> >>", "/F Do",
		stream("/Subtype /Form /BBox [0 0 9 9] /Resources << /XObject << /F 5 0 R >> >>", "0 0 1 rg /F Do"))
	r := recolor(t, f, cmyk, naive(Options{}))
	if got := string(decode(t, r, ast.PdfIndirectRef{ObjectNum: 5})); got != " 1 1 0 0 k /F Do" {
		t.Errorf("form: %q", got)
	}
}

func TestFormWithoutResourcesUsesPage(t *testing.T) {
	f := onePage("/Resources << /ColorSpace << /CS0 /DeviceRGB >> /XObject << /F 5 0 R >> >>", "/F Do",
		stream("/Subtype /Form /BBox [0 0 9 9]", "/CS0 cs 0 0 1 sc"))
	r := recolor(t, f, cmyk, naive(Options{}))
	if got := string(decode(t, r, ast.PdfIndirectRef{ObjectNum: 5})); got != " 0 0 0 1 k 1 1 0 0 k" {
		t.Errorf("form: %q", got)
	}
}

func TestFormGroupBlendingSpace(t *testing.T) {
	f := onePage("/Group << /S /Transparency /CS /DeviceRGB >> /Resources << /XObject << /F 5 0 R >> >>", "/F Do",
		stream("/Subtype /Form /BBox [0 0 9 9] /Group << /S /Transparency /CS /DeviceGray >>", "0 0 9 9 re f"))
	r := recolor(t, f, cmyk, naive(Options{}))
	if cs := r.dict(r.pages[0].Dict["Group"])["CS"]; cs != ast.PdfName("DeviceCMYK") {
		t.Errorf("page group: %v", cs)
	}
	if cs := r.dict(r.dict(ast.PdfIndirectRef{ObjectNum: 5})["Group"])["CS"]; cs != ast.PdfName("DeviceCMYK") {
		t.Errorf("form group: %v", cs)
	}
}

func TestType3Glyphs(t *testing.T) {
	f := onePage("/Resources << /Font << /T3 5 0 R >> >>", "BT /T3 1 Tf (ab) Tj ET",
		"<< /Type /Font /Subtype /Type3 /FontBBox [0 0 1 1] /FontMatrix [1 0 0 1 0 0] /CharProcs << /a 6 0 R /b 7 0 R >> "+
			"/Encoding << /Differences [97 /a /b] >> /FirstChar 97 /LastChar 98 /Widths [1 1] >>",
		stream("", "1 0 d0 1 0 0 rg 0 0 1 1 re f"),
		stream("", "1 0 0 0 1 1 d1 1 0 0 rg 0 0 1 1 re f"))
	r := recolor(t, f, cmyk, naive(Options{}))
	if got := string(decode(t, r, ast.PdfIndirectRef{ObjectNum: 6})); got != "1 0 d0 0 1 1 0 k 0 0 1 1 re f" {
		t.Errorf("d0 glyph: %q", got)
	}
	// A d1 glyph's colour operators are ignored (ISO 32000-1, 9.6.5,
	// Table 113): they convert, and are still ignored.
	if got := string(decode(t, r, ast.PdfIndirectRef{ObjectNum: 7})); got != "1 0 0 0 1 1 d1 0 1 1 0 k 0 0 1 1 re f" {
		t.Errorf("d1 glyph: %q", got)
	}
}

func TestAnnotationAppearances(t *testing.T) {
	f := onePage("/Annots [5 0 R]", "",
		"<< /Type /Annot /Subtype /Square /Rect [0 0 9 9] /AP << /N 6 0 R /D << /On 7 0 R >> >> >>",
		stream("/BBox [0 0 9 9]", "1 0 0 RG 0 0 9 9 re S"),
		stream("/BBox [0 0 9 9] /Subtype /Form", "0 g 0 0 9 9 re f"))
	r := recolor(t, f, cmyk, naive(Options{}))
	if got := string(decode(t, r, ast.PdfIndirectRef{ObjectNum: 6})); got != " 0 1 1 0 K 0 0 9 9 re S" {
		t.Errorf("normal appearance: %q", got)
	}
	if got := string(decode(t, r, ast.PdfIndirectRef{ObjectNum: 7})); got != " 0 0 0 1 k 0 0 9 9 re f" {
		t.Errorf("down appearance: %q", got)
	}
}

func TestOperatorSplitAcrossStreams(t *testing.T) {
	// 7.8.2: a page's streams read as one, so an operator's operands may
	// lie in one and the operator in the next; the page then gets one
	// stream of the whole.
	f := fixture{
		pages: []fixturePage{{contents: "[4 0 R 5 0 R]", content: "1 0"}},
		objs:  []string{stream("", "0 rg 0 0 9 9 re f")},
	}
	r := recolor(t, f, cmyk, naive(Options{}))
	if got, want := r.text(0), "0 1 1 0 k\n0 0 9 9 re\nf"; got != want {
		t.Errorf("content:\n%s\nwant:\n%s", got, want)
	}
	if _, ok := r.pages[0].Dict["Contents"].(ast.PdfIndirectRef); !ok {
		t.Errorf("contents: %v", r.pages[0].Dict["Contents"])
	}
}

func TestStateCarriesAcrossStreams(t *testing.T) {
	f := fixture{
		pages: []fixturePage{{contents: "[4 0 R 5 0 R]", content: "/DeviceRGB cs"}},
		objs:  []string{stream("", "1 0 0 sc")},
	}
	r := recolor(t, f, cmyk, naive(Options{}))
	if got := string(decode(t, r, ast.PdfIndirectRef{ObjectNum: 5})); got != " 0 1 1 0 k" {
		t.Errorf("second stream: %q", got)
	}
}

func TestInheritedResources(t *testing.T) {
	// The page tree states the resources; the page inherits them whole
	// (7.7.3.4), and they are rewritten where they are.
	data := buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 /Resources 5 0 R >>",
		"<< /Type /Page /Parent 2 0 R /Contents 4 0 R /MediaBox [0 0 9 9] >>",
		stream("", "/S0 cs 1 sc"),
		"<< /ColorSpace << /S0 [/Separation /Spot /DeviceRGB << /FunctionType 2 /Domain [0 1] /C0 [1 1 1] /C1 [0 0 0] /N 1 >>] >> >>",
	})
	r := recolorBytes(t, data, cmyk, naive(Options{}))
	spaces := r.dict(r.dict(ast.PdfIndirectRef{ObjectNum: 5})["ColorSpace"])
	if cs := r.resolve(spaces["S0"]).(ast.PdfArray); cs[2] != ast.PdfName("DeviceCMYK") {
		t.Errorf("inherited spot: %v", cs)
	}
}

func TestDeterministic(t *testing.T) {
	f := onePage("/Resources << /ColorSpace << /S0 [/Separation /A /DeviceRGB 5 0 R] /S1 [/Separation /B /DeviceRGB 5 0 R] >> /Shading << /Sh 6 0 R >> >>",
		"/S0 cs 1 sc /S1 CS 1 SC /Sh sh 1 0 0 rg",
		"<< /FunctionType 2 /Domain [0 1] /C0 [1 1 1] /C1 [1 0 0] /N 1 >>",
		"<< /ShadingType 2 /ColorSpace /DeviceRGB /Coords [0 0 9 0] /Function 5 0 R >>")
	a := recolor(t, f, cmyk, naive(Options{}))
	b := recolor(t, f, cmyk, naive(Options{}))
	if !bytes.Equal(a.data, b.data) {
		t.Error("two runs wrote different bytes")
	}
}

func TestCancelled(t *testing.T) {
	doc, pages := open(t, onePage("", "1 0 0 rg").bytes())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Content(ctx, doc, nil, pages, cmyk, Options{}); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v", err)
	}
}
