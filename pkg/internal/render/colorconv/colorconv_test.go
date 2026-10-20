package colorconv

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/icc"
	"github.com/gotenberg/litho/pkg/internal/graphics/image"
)

// buildPDF assembles a one-page document from objects numbered from 1. The
// first three are the catalog, the page tree and the page.
func buildPDF(t testing.TB, objs []string) (context.Context, *document.Document, *document.Page) {
	t.Helper()
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	data := buf.Bytes()
	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil || len(pages) != 1 {
		t.Fatalf("ResolvePages: %v (%d pages)", err, len(pages))
	}
	return ctx, doc, pages[0]
}

func streamObj(dict, data string) string {
	return fmt.Sprintf("<< %s /Length %d >>\nstream\n%s\nendstream", dict, len(data), data)
}

// sharedObjects are objects 4 to 8 of every test document: the content
// stream, two ICC profiles and two tint transforms the cases refer to.
func sharedObjects(content string) []string {
	return []string{
		streamObj("", content),
		streamObj("/N 3", string(icc.SRGBProfile())),
		streamObj("/N 4", string(icc.CMYKPrinterProfile())),
		"<< /FunctionType 2 /Domain [0 1] /C0 [0 0 0 0] /C1 [0 1 0.5 0] /N 1 >>",
		streamObj("/FunctionType 4 /Domain [0 1 0 1] /Range [0 1 0 1 0 1]", "{ 0.5 }"),
	}
}

const (
	d65Cal = "/WhitePoint [0.9505 1 1.089]"
	srgbM  = "/Matrix [0.4124 0.2126 0.0193 0.3576 0.7152 0.1192 0.1805 0.0722 0.9505]"
	labDef = "[/Lab << /WhitePoint [0.9642 1 0.8249] /Range [-128 127 -128 127] >>]"
	sepDef = "[/Separation /Spot /DeviceCMYK 7 0 R]"
)

// colorCase is one colour in one space: the space's definition, written
// inline, and the components a vector colour gives.
type colorCase struct {
	name  string
	def   string
	comps []float64
	// poppler is the sRGB pdftoppm 26.09.0 paints a filled square of the
	// colour with at 72 dpi (oraclePage).
	poppler []uint8
	// tol is how many levels litho may differ from poppler by on a channel.
	tol int
}

// oracleCases are the colours of the oracle page. Every value of poppler
// was read off the centre of the square pdftoppm 26.09.0, built with lcms2,
// painted at 72 dpi.
//
// Two families diverge by design, and their tolerance is the measured
// divergence, so a further drift still fails:
//
//   - DeviceCMYK with no output intent, alone or as a tint transform's
//     alternate: litho reads it through its built-in CMYK profile, as it
//     decodes CMYK images, where poppler applies its own formula. The two
//     agree on pure cyan, and the same colours through an ICCBased space of
//     litho's CMYK printer profile match poppler to the level.
//   - Lab: litho converts it as its image decoder does, mirroring PDF.js
//     (no chromatic adaptation, a square-root transfer curve), where poppler
//     goes through lcms2.
var oracleCases = []colorCase{
	{name: "DeviceGray", def: "/DeviceGray", comps: []float64{0.5}, poppler: []uint8{128, 128, 128}, tol: 1},
	{name: "DeviceRGB", def: "/DeviceRGB", comps: []float64{0.2, 0.4, 0.6}, poppler: []uint8{51, 102, 153}, tol: 1},
	{name: "DeviceCMYK cyan", def: "/DeviceCMYK", comps: []float64{1, 0, 0, 0}, poppler: []uint8{0, 173, 239}, tol: 1},
	{name: "DeviceCMYK mix", def: "/DeviceCMYK", comps: []float64{0, 0.5, 0.5, 0.2}, poppler: []uint8{203, 108, 88}, tol: 17},
	{name: "CalGray", def: "[/CalGray << " + d65Cal + " /Gamma 2.2 >>]", comps: []float64{0.5}, poppler: []uint8{128, 128, 128}, tol: 1},
	{name: "CalRGB", def: "[/CalRGB << " + d65Cal + " /Gamma [2.2 2.2 2.2] " + srgbM + " >>]", comps: []float64{0.2, 0.4, 0.6}, poppler: []uint8{48, 102, 154}, tol: 1},
	{name: "Lab", def: labDef, comps: []float64{50, 20, -30}, poppler: []uint8{133, 108, 170}, tol: 10},
	{name: "ICCBased RGB", def: "[/ICCBased 5 0 R]", comps: []float64{0.2, 0.4, 0.6}, poppler: []uint8{51, 102, 153}, tol: 1},
	{name: "ICCBased CMYK", def: "[/ICCBased 6 0 R]", comps: []float64{1, 0, 0, 0}, poppler: []uint8{0, 174, 239}, tol: 1},
	{name: "Indexed RGB", def: "[/Indexed /DeviceRGB 2 <FF0000 00FF00 0000FF>]", comps: []float64{1}, poppler: []uint8{0, 255, 0}, tol: 1},
	{name: "Separation", def: sepDef, comps: []float64{0.5}, poppler: []uint8{246, 129, 153}, tol: 23},
	{name: "DeviceN", def: "[/DeviceN [/A /B] /DeviceRGB 8 0 R]", comps: []float64{0.3, 0.7}, poppler: []uint8{76, 178, 128}, tol: 1},
	{name: "Indexed Lab", def: "[/Indexed " + labDef + " 1 <FF8080 80FF80>]", comps: []float64{1}, poppler: []uint8{255, 0, 127}, tol: 10},
	{name: "Indexed Separation", def: "[/Indexed " + sepDef + " 2 <0080FF>]", comps: []float64{1}, poppler: []uint8{246, 129, 152}, tol: 23},
	{name: "ICCBased CMYK mix", def: "[/ICCBased 6 0 R]", comps: []float64{0, 0.5, 0.5, 0.2}, poppler: []uint8{204, 125, 101}, tol: 1},
	{name: "Lab saturated", def: labDef, comps: []float64{90, -50, 60}, poppler: []uint8{149, 250, 101}, tol: 9},
}

// oraclePage returns the content stream and colour space resources of the
// oracle page: a 20-point square per case, six to a row from the top left
// of a 200-point page.
func oraclePage() (content, spaces string) {
	var cs, sb strings.Builder
	for i, c := range oracleCases {
		x, y := 10+30*(i%6), 170-30*(i/6)
		fmt.Fprintf(&cs, "/C%d %s ", i, c.def)
		vals := make([]string, len(c.comps))
		for j, v := range c.comps {
			vals[j] = fmt.Sprint(v)
		}
		fmt.Fprintf(&sb, "/C%d cs %s sc %d %d 20 20 re f\n", i, strings.Join(vals, " "), x, y)
	}
	return sb.String(), cs.String()
}

// oracleObjects are the objects of the oracle page, which pdftoppm renders
// for the poppler values.
func oracleObjects() []string {
	content, spaces := oraclePage()
	return append([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R" +
			" /Resources << /ColorSpace << " + spaces + " >> >> >>",
	}, sharedObjects(content)...)
}

func level(v float32) int { return int(math.Round(float64(v) * 255)) }

// TestImageAgreement pins that a vector colour converts to the sRGB the image
// decoder gives a one-pixel image of the same colour in the same space, for
// every family.
func TestImageAgreement(t *testing.T) {
	t.Parallel()
	cases := append(cloneCases(oracleCases),
		colorCase{name: "Indexed past hival", def: "[/Indexed /DeviceRGB 1 <FF0000 00FF00>]", comps: []float64{7}},
		colorCase{name: "DeviceGray clipped", def: "/DeviceGray", comps: []float64{1.7}},
	)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			// The first pass reads the sample bytes the colour quantises to;
			// the second paints an image of them.
			probe := agreementDoc(t, c.def, nil)
			sc, err := image.NewSampleColor(probe.ctx, probe.doc, probe.space(t), probe.page.Resources, probe.page.Dict, icc.RelativeColorimetric)
			if err != nil {
				t.Fatalf("NewSampleColor: %v", err)
			}
			samples := make([]byte, sc.Components())
			sc.Samples(c.comps, samples)

			d := agreementDoc(t, c.def, samples)
			conv, err := ForObject(d.ctx, d.doc, d.space(t), d.page.Resources, Options{Page: d.page.Dict})
			if err != nil {
				t.Fatalf("ForObject: %v", err)
			}
			r, g, b := conv.RGB(c.comps)
			img, err := image.DecodeImage(d.ctx, d.doc, d.page, "Im0")
			if err != nil {
				t.Fatalf("DecodeImage: %v", err)
			}
			ir, ig, ib, _ := img.At(0, 0).RGBA()
			want := [3]int{int(ir >> 8), int(ig >> 8), int(ib >> 8)}
			if got := [3]int{level(r), level(g), level(b)}; got != want {
				t.Errorf("vector colour %v converts to %v, the image of samples %v decodes to %v", c.comps, got, samples, want)
			}
		})
	}
}

func cloneCases(cs []colorCase) []colorCase { return append([]colorCase(nil), cs...) }

type testDoc struct {
	ctx  context.Context
	doc  *document.Document
	page *document.Page
}

// space returns the definition of the page's /CS colour space resource.
func (d testDoc) space(t *testing.T) ast.PdfObject {
	t.Helper()
	spaces, err := document.ResolveDict(d.ctx, d.doc, d.page.Resources["ColorSpace"])
	if err != nil {
		t.Fatalf("resolving /ColorSpace: %v", err)
	}
	obj, err := document.ResolveObject(d.ctx, d.doc, spaces["CS"])
	if err != nil {
		t.Fatalf("resolving /CS: %v", err)
	}
	return obj
}

// agreementDoc builds a page whose resources name the space def /CS and,
// when samples is not nil, hold a one-pixel image Im0 of them in def.
func agreementDoc(t testing.TB, def string, samples []byte) testDoc {
	t.Helper()
	objs := append([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 10 10] /Contents 4 0 R" +
			" /Resources << /ColorSpace << /CS " + def + " >> /XObject << /Im0 9 0 R >> >> >>",
	}, sharedObjects("")...)
	objs = append(objs, streamObj("/Type /XObject /Subtype /Image /Width 1 /Height 1 /BitsPerComponent 8 /ColorSpace "+def, string(samples)))
	ctx, doc, page := buildPDF(t, objs)
	return testDoc{ctx: ctx, doc: doc, page: page}
}

// TestPopplerOracle compares each case with what pdftoppm 26.09.0 paints.
func TestPopplerOracle(t *testing.T) {
	t.Parallel()
	ctx, doc, page := buildPDF(t, oracleObjects())
	spaces, err := document.ResolveDict(ctx, doc, page.Resources["ColorSpace"])
	if err != nil {
		t.Fatal(err)
	}
	for i, c := range oracleCases {
		if c.poppler == nil {
			continue
		}
		t.Run(c.name, func(t *testing.T) {
			obj, err := document.ResolveObject(ctx, doc, spaces[ast.PdfName(fmt.Sprintf("C%d", i))])
			if err != nil {
				t.Fatal(err)
			}
			conv, err := ForObject(ctx, doc, obj, page.Resources, Options{Page: page.Dict})
			if err != nil {
				t.Fatalf("ForObject: %v", err)
			}
			r, g, b := conv.RGB(c.comps)
			got := []int{level(r), level(g), level(b)}
			for ch := range 3 {
				if d := got[ch] - int(c.poppler[ch]); d < -c.tol || d > c.tol {
					t.Errorf("%v converts to %v, poppler paints %v (tolerance %d)", c.comps, got, c.poppler, c.tol)
					break
				}
			}
		})
	}
}

// TestHandComputed checks families against values computed from the
// formulas of ISO 32000-1, 8.6, independently of the code under test.
func TestHandComputed(t *testing.T) {
	t.Parallel()
	encode := func(lin float64) int {
		v := 12.92 * lin
		if lin > 0.0031308 {
			v = 1.055*math.Pow(lin, 1/2.4) - 0.055
		}
		return int(math.Round(v * 255))
	}
	// CalGray with a D65 white: Y = 0.5^2.2, grey on every channel
	// (8.6.5.2). The colour quantises to sample 128.
	calGrey := encode(math.Pow(128.0/255, 2.2))
	// Separation over DeviceCMYK through the built-in profile is checked by
	// agreement; over DeviceGray the tint transform's output is the grey.
	tests := []struct {
		name  string
		def   string
		comps []float64
		want  [3]int
	}{
		{"DeviceGray", "/DeviceGray", []float64{0.5}, [3]int{128, 128, 128}},
		{"DeviceRGB", "/DeviceRGB", []float64{0.2, 0.4, 0.6}, [3]int{51, 102, 153}},
		{"DeviceRGB clipped", "/DeviceRGB", []float64{-1, 2, 0.6}, [3]int{0, 255, 153}},
		{"CalGray", "[/CalGray << " + d65Cal + " /Gamma 2.2 >>]", []float64{0.5}, [3]int{calGrey, calGrey, calGrey}},
		{"Indexed", "[/Indexed /DeviceRGB 2 <FF0000 00FF00 0000FF>]", []float64{2}, [3]int{0, 0, 255}},
		{"Indexed rounds", "[/Indexed /DeviceRGB 2 <FF0000 00FF00 0000FF>]", []float64{0.6}, [3]int{0, 255, 0}},
		{"Indexed clips", "[/Indexed /DeviceRGB 2 <FF0000 00FF00 0000FF>]", []float64{-3}, [3]int{255, 0, 0}},
		{"Separation over gray", "[/Separation /Spot /DeviceGray << /FunctionType 2 /Domain [0 1] /C0 [1] /C1 [0] /N 1 >>]", []float64{0.25}, [3]int{191, 191, 191}},
		{"DeviceN over RGB", "[/DeviceN [/A /B] /DeviceRGB 8 0 R]", []float64{0.2, 1}, [3]int{51, 255, 128}},
		{"Lab white", labDef, []float64{100, 0, 0}, [3]int{255, 255, 255}},
		{"Lab black", labDef, []float64{0, 0, 0}, [3]int{0, 0, 0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d := agreementDoc(t, tt.def, nil)
			conv, err := ForObject(d.ctx, d.doc, d.space(t), d.page.Resources, Options{})
			if err != nil {
				t.Fatalf("ForObject: %v", err)
			}
			r, g, b := conv.RGB(tt.comps)
			// The decoder truncates its Lab channels, so the white of a Lab
			// space comes out a level short of 255 on two channels; one level
			// is the tolerance throughout.
			got := [3]int{level(r), level(g), level(b)}
			for ch := range 3 {
				if d := got[ch] - tt.want[ch]; d < -1 || d > 1 {
					t.Errorf("%v converts to %v, want %v", tt.comps, got, tt.want)
					break
				}
			}
		})
	}
}

// TestDefaultSpaces pins ISO 32000-1, 8.6.5.6: a DefaultRGB space in the
// resources in force stands in for DeviceRGB, and resources without one
// leave it alone.
func TestDefaultSpaces(t *testing.T) {
	t.Parallel()
	// A CalRGB with gamma 1 and the identity matrix over a D65 white reads
	// its components as linear XYZ, which is not what DeviceRGB means.
	const def = "/DeviceRGB"
	d := agreementDoc(t, def, nil)
	resources := ast.PdfDict{"ColorSpace": ast.PdfDict{
		"DefaultRGB": ast.PdfArray{ast.PdfName("CalRGB"), ast.PdfDict{
			"WhitePoint": ast.PdfArray{ast.PdfReal(0.9505), ast.PdfInt(1), ast.PdfReal(1.089)},
		}},
	}}
	plain, err := For(d.ctx, d.doc, graphics.ColorSpace{Family: graphics.FamilyDeviceRGB, Components: 3}, nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	substituted, err := For(d.ctx, d.doc, graphics.ColorSpace{Family: graphics.FamilyDeviceRGB, Components: 3}, resources, Options{})
	if err != nil {
		t.Fatal(err)
	}
	comps := []float64{0.2, 0.4, 0.6}
	pr, pg, pb := plain.RGB(comps)
	sr, sg, sb := substituted.RGB(comps)
	if level(pr) != 51 || level(pg) != 102 || level(pb) != 153 {
		t.Errorf("DeviceRGB with no default converts to %v %v %v", level(pr), level(pg), level(pb))
	}
	if level(sr) == level(pr) && level(sg) == level(pg) && level(sb) == level(pb) {
		t.Errorf("DefaultRGB did not stand in for DeviceRGB: both convert to %v %v %v", level(sr), level(sg), level(sb))
	}
}

// TestSpecialSpaces covers the spaces that convert no colour of their own.
func TestSpecialSpaces(t *testing.T) {
	t.Parallel()
	d := agreementDoc(t, "/DeviceGray", nil)
	ctx, doc := d.ctx, d.doc

	none := ast.PdfArray{ast.PdfName("Separation"), ast.PdfName("None"), ast.PdfName("DeviceGray"),
		ast.PdfDict{"FunctionType": ast.PdfInt(2), "Domain": ast.PdfArray{ast.PdfInt(0), ast.PdfInt(1)}, "N": ast.PdfInt(1)}}
	if _, err := ForObject(ctx, doc, none, nil, Options{}); !errors.Is(err, ErrPaintsNothing) {
		t.Errorf("Separation /None: err = %v, want ErrPaintsNothing", err)
	}
	if _, err := For(ctx, doc, graphics.ColorSpace{Family: graphics.FamilyPattern}, nil, Options{}); !errors.Is(err, ErrPattern) {
		t.Errorf("Pattern: err = %v, want ErrPattern", err)
	}
	if _, err := ForObject(ctx, doc, ast.PdfName("Pattern"), nil, Options{}); !errors.Is(err, ErrPattern) {
		t.Errorf("Pattern object: err = %v, want ErrPattern", err)
	}
	if _, err := For(ctx, doc, graphics.ColorSpace{Resource: "CS9"}, nil, Options{}); !errors.Is(err, ErrUnresolved) {
		t.Errorf("unresolved: err = %v, want ErrUnresolved", err)
	}
	if _, err := ForObject(ctx, doc, ast.PdfName("Bogus"), nil, Options{}); !errors.Is(err, ErrUnsupported) {
		t.Errorf("unknown family: err = %v, want ErrUnsupported", err)
	}
	// An uncoloured pattern's colour is read in the base space (8.7.3.3).
	base := graphics.ColorSpace{Family: graphics.FamilyDeviceRGB, Components: 3}
	conv, err := For(ctx, doc, graphics.ColorSpace{Family: graphics.FamilyPattern, Base: &base, Components: 3}, nil, Options{})
	if err != nil {
		t.Fatalf("Pattern with base: %v", err)
	}
	if r, g, b := conv.RGB([]float64{1, 0, 0}); level(r) != 255 || level(g) != 0 || level(b) != 0 {
		t.Errorf("Pattern over DeviceRGB converts red to %v %v %v", r, g, b)
	}
}

// TestIntents pins that every rendering intent name converts, and that the
// cache keeps one converter per intent.
func TestIntents(t *testing.T) {
	t.Parallel()
	d := agreementDoc(t, "[/ICCBased 6 0 R]", nil)
	cache := &Cache{}
	first := map[string]Converter{}
	for _, intent := range []string{"", "Perceptual", "RelativeColorimetric", "Saturation", "AbsoluteColorimetric", "Bogus"} {
		conv, err := ForObject(d.ctx, d.doc, d.space(t), nil, Options{Intent: intent, Cache: cache})
		if err != nil {
			t.Fatalf("intent %q: %v", intent, err)
		}
		if r, _, _ := conv.RGB([]float64{1, 0, 0, 0}); r > 0.1 {
			t.Errorf("intent %q: cyan converts to red %v", intent, r)
		}
		again, err := ForObject(d.ctx, d.doc, d.space(t), nil, Options{Intent: intent, Cache: cache})
		if err != nil || again != conv {
			t.Errorf("intent %q: the cache built a second converter", intent)
		}
		first[intent] = conv
	}
	// "" and an unrecognised name select relative colorimetric (8.6.5.8).
	if first[""] != first["RelativeColorimetric"] || first["Bogus"] != first["RelativeColorimetric"] {
		t.Error("the default intent is not relative colorimetric")
	}
}

// TestConcurrentRGB runs one converter from several goroutines under -race.
func TestConcurrentRGB(t *testing.T) {
	t.Parallel()
	d := agreementDoc(t, "[/ICCBased 6 0 R]", nil)
	cache := &Cache{}
	var wg sync.WaitGroup
	for w := range 4 {
		wg.Go(func() {
			conv, err := ForObject(d.ctx, d.doc, d.space(t), nil, Options{Cache: cache})
			if err != nil {
				t.Error(err)
				return
			}
			for i := range 2000 {
				conv.RGB([]float64{float64(i%50) / 50, float64(w) / 4, 0, 0})
			}
		})
	}
	wg.Wait()
}

// BenchmarkRGB measures a converter over a run of colours that repeat, as a
// shading's do.
func BenchmarkRGB(b *testing.B) {
	d := agreementDoc(b, "[/ICCBased 6 0 R]", nil)
	conv, err := ForObject(d.ctx, d.doc, ast.PdfArray{ast.PdfName("ICCBased"), ast.PdfIndirectRef{ObjectNum: 6}}, nil, Options{})
	if err != nil {
		b.Fatal(err)
	}
	comps := make([]float64, 4)
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		comps[0] = float64(i%256) / 255
		conv.RGB(comps)
	}
}
