package recolor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"maps"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics/icc"
	pdfimage "github.com/gotenberg/litho/pkg/internal/graphics/image"
	"github.com/gotenberg/litho/pkg/internal/render/page"
)

// imageRef is where the test documents keep the image a page draws.
var imageRef = ast.PdfIndirectRef{ObjectNum: 7}

// testPage is a one-page document drawing image object 7 over its whole
// page, w x h points, under the resources object 5.
type testPage struct {
	objs map[int]ast.PdfObject
	w, h int
}

func newTestPage(w, h int, img ast.PdfStream) *testPage {
	body := fmt.Sprintf("q %d 0 0 %d 0 0 cm /Im1 Do Q", w, h)
	return &testPage{w: w, h: h, objs: map[int]ast.PdfObject{
		1: ast.PdfDict{"Type": ast.PdfName("Catalog"), "Pages": ast.PdfIndirectRef{ObjectNum: 2}},
		2: ast.PdfDict{"Type": ast.PdfName("Pages"), "Kids": ast.PdfArray{ast.PdfIndirectRef{ObjectNum: 3}}, "Count": ast.PdfInt(1)},
		3: ast.PdfDict{
			"Type": ast.PdfName("Page"), "Parent": ast.PdfIndirectRef{ObjectNum: 2},
			"MediaBox":  ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(w), ast.PdfInt(h)},
			"Resources": ast.PdfIndirectRef{ObjectNum: 5}, "Contents": ast.PdfIndirectRef{ObjectNum: 6},
		},
		5: ast.PdfDict{"XObject": ast.PdfDict{"Im1": imageRef}},
		6: ast.NewStream(ast.PdfDict{}, []byte(body)),
		7: img,
	}}
}

// bytes writes the document out, every object at generation 0.
func (p *testPage) bytes(t testing.TB) []byte {
	t.Helper()
	ctx := context.Background()
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	nums := slices.Sorted(maps.Keys(p.objs))
	for _, n := range nums {
		if _, err := writer.SerializeIndirect(ctx, &buf, n, 0, p.objs[n]); err != nil {
			t.Fatalf("writing object %d: %v", n, err)
		}
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\n%%%%EOF\n", nums[len(nums)-1]+1)
	return buf.Bytes()
}

// open parses the document and returns it with its page.
func (p *testPage) open(t testing.TB) (*document.Document, *document.Page) {
	t.Helper()
	ctx := context.Background()
	raw := p.bytes(t)
	doc, err := document.NewDocument(ctx, bytes.NewReader(raw), int64(len(raw)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil || len(pages) != 1 {
		t.Fatalf("ResolvePages: %d pages, %v", len(pages), err)
	}
	return doc, pages[0]
}

// with returns a copy of the document with the allocator's objects in it.
func (p *testPage) with(alloc *writer.ObjAllocator) *testPage {
	out := &testPage{w: p.w, h: p.h, objs: maps.Clone(p.objs)}
	maps.Copy(out.objs, alloc.Pending())
	return out
}

// convert converts the page's image under cfg, its Colors built for the
// opened document by colors, and returns the allocator and the outcome.
func (p *testPage) convert(t *testing.T, cfg ImageConfig, colors func(*document.Document, *writer.ObjAllocator) Colors) (*writer.ObjAllocator, *Images, ImageOutcome, error) {
	t.Helper()
	ctx := context.Background()
	doc, pg := p.open(t)
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))
	cfg.Colors = colors(doc, alloc)
	im, err := NewImages(doc, alloc, cfg)
	if err != nil {
		t.Fatalf("NewImages: %v", err)
	}
	outcome, err := im.ConvertXObject(ctx, imageRef, pg.Resources, pg.Dict)
	return alloc, im, outcome, err
}

// render renders the page at 72 dpi, one device pixel a point, as 8-bit
// RGB.
func (p *testPage) render(t *testing.T) []uint8 {
	t.Helper()
	ctx := context.Background()
	doc, pg := p.open(t)
	canvas, _, err := page.Render(ctx, doc, pg, page.Options{DPI: 72})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	img := canvas.Image()
	b := img.Bounds()
	out := make([]uint8, 0, 3*b.Dx()*b.Dy())
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			out = append(out, uint8(r>>8), uint8(g>>8), uint8(bl>>8))
		}
	}
	return out
}

// maxSampleDiff returns the largest difference between two renderings,
// channel by channel.
func maxSampleDiff(t *testing.T, a, b []uint8) int {
	t.Helper()
	if len(a) != len(b) {
		t.Fatalf("renderings of %d and %d values", len(a), len(b))
	}
	worst := 0
	for i := range a {
		worst = max(worst, int(math.Abs(float64(a[i])-float64(b[i]))))
	}
	return worst
}

// inflate returns the samples of a converted image through the document's
// filters, its Flate and predictor undone.
func inflate(t *testing.T, doc *document.Document, s ast.PdfStream) []byte {
	t.Helper()
	rc, err := doc.NewStreamReaderChecked(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	out, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// pending returns the stream the allocator queued at object n.
func pending(t *testing.T, alloc *writer.ObjAllocator, n int) ast.PdfStream {
	t.Helper()
	s, ok := alloc.Pending()[n].(ast.PdfStream)
	if !ok {
		t.Fatalf("object %d = %T, want a queued stream", n, alloc.Pending()[n])
	}
	return s
}

// imageStream returns an image XObject of w x h samples under extra entries.
func imageStream(w, h int, cs ast.PdfObject, bpc int, data []byte, extra ast.PdfDict) ast.PdfStream {
	d := ast.PdfDict{
		"Type": ast.PdfName("XObject"), "Subtype": ast.PdfName("Image"),
		"Width": ast.PdfInt(w), "Height": ast.PdfInt(h), "BitsPerComponent": ast.PdfInt(bpc),
	}
	if cs != nil {
		d["ColorSpace"] = cs
	}
	maps.Copy(d, extra)
	return ast.NewStream(d, data)
}

// funcColors is a [Colors] whose converter is one exact function, whatever
// the source space, and whose kept spots are written under one name.
type funcColors struct {
	f     func(in, out []float64)
	spot  ast.PdfObject
	asked []string
}

func (c *funcColors) Converter(_ context.Context, space ast.PdfObject, _, _ ast.PdfDict, _ ast.PdfName) (Converter, error) {
	c.asked = append(c.asked, fmt.Sprint(space))
	return converterFunc(c.f), nil
}

func (c *funcColors) SpotSpace(context.Context, ast.PdfObject, ast.PdfDict, ast.PdfDict) (ast.PdfObject, error) {
	if c.spot == nil {
		return nil, fmt.Errorf("no spot space")
	}
	return c.spot, nil
}

type converterFunc func(in, out []float64)

func (f converterFunc) Convert(in, out []float64) { f(in, out) }

// renderedColors is a [Colors] converting through the image decoder's sRGB
// reading, then to the target by the device formulas: to a DeviceRGB target
// it is exactly what the renderer paints, so a converted page renders as the
// source did. It keeps a spot by rewriting its tint transform as a sampled
// function into the target.
type renderedColors struct {
	doc   *document.Document
	alloc *writer.ObjAllocator
	tc    int
	spots map[string]ast.PdfObject
}

func newRenderedColors(tc int) func(*document.Document, *writer.ObjAllocator) Colors {
	return func(doc *document.Document, alloc *writer.ObjAllocator) Colors {
		return &renderedColors{doc: doc, alloc: alloc, tc: tc, spots: map[string]ast.PdfObject{}}
	}
}

func (c *renderedColors) Converter(ctx context.Context, space ast.PdfObject, resources, pg ast.PdfDict, intent ast.PdfName) (Converter, error) {
	sc, err := pdfimage.NewSampleColor(ctx, c.doc, space, resources, pg, icc.ParseIntent(string(intent)))
	if err != nil {
		return nil, err
	}
	buf := make([]byte, sc.Components())
	return converterFunc(func(in, out []float64) {
		sc.Samples(in, buf)
		px, _ := sc.Convert(buf)
		fromRGB(float64(px.R)/255, float64(px.G)/255, float64(px.B)/255, out)
	}), nil
}

func (c *renderedColors) SpotSpace(ctx context.Context, space ast.PdfObject, resources, pg ast.PdfDict) (ast.PdfObject, error) {
	key := fmt.Sprint(space)
	if s, ok := c.spots[key]; ok {
		return s, nil
	}
	arr, err := document.ResolveArray(ctx, c.doc, space)
	if err != nil || len(arr) < 4 {
		return nil, fmt.Errorf("spot space %v", space)
	}
	conv, err := c.Converter(ctx, space, resources, pg, "")
	if err != nil {
		return nil, err
	}
	inputs := 1
	if names, ok := arr[1].(ast.PdfArray); ok {
		inputs = len(names)
	}
	// At most maxSpotSamples grid points, however many inputs.
	const maxSpotSamples = 4096
	size := max(2, int(math.Floor(math.Pow(maxSpotSamples, 1/float64(inputs)))))
	n := int(math.Pow(float64(size), float64(inputs)))
	samples := make([]byte, 0, n*c.tc)
	in := make([]float64, inputs)
	out := make([]float64, c.tc)
	for i := range n {
		// ISO 32000-1, 7.10.2: the first input varies fastest.
		for k, j := 0, i; k < inputs; k, j = k+1, j/size {
			in[k] = float64(j%size) / float64(size-1)
		}
		conv.Convert(in, out)
		for _, v := range out {
			samples = append(samples, byte(math.Round(v*255)))
		}
	}
	domain, sizes := ast.PdfArray{}, ast.PdfArray{}
	for range inputs {
		domain = append(domain, ast.PdfInt(0), ast.PdfInt(1))
		sizes = append(sizes, ast.PdfInt(size))
	}
	rng := ast.PdfArray{}
	for range c.tc {
		rng = append(rng, ast.PdfInt(0), ast.PdfInt(1))
	}
	fn := c.alloc.Alloc(ast.NewStream(ast.PdfDict{
		"FunctionType": ast.PdfInt(0), "Domain": domain, "Range": rng,
		"Size": sizes, "BitsPerSample": ast.PdfInt(8),
	}, samples))
	target := map[int]ast.PdfName{1: familyGray, 3: familyRGB, 4: familyCMYK}[c.tc]
	out2 := ast.PdfArray{arr[0], arr[1], target, fn}
	out2 = append(out2, arr[4:]...)
	c.spots[key] = out2
	return out2, nil
}

// fromRGB writes sRGB as the target's components by the device formulas of
// ISO 32000-1, 10.4.2 and 10.4.3, with full black generation and undercolour
// removal: BG(k) = UCR(k) = k.
func fromRGB(r, g, b float64, out []float64) {
	switch len(out) {
	case 1:
		out[0] = 0.3*r + 0.59*g + 0.11*b
	case 3:
		out[0], out[1], out[2] = r, g, b
	case 4:
		k := 1 - max(r, g, b)
		out[0], out[1], out[2], out[3] = 1-r-k, 1-g-k, 1-b-k, k
	}
}

// deviceColors is a [Colors] for device spaces only, by the formulas of
// ISO 32000-1, 10.4.2 and 10.4.3, which invert one another: a round trip
// through it comes back within quantisation.
type deviceColors struct{ tc int }

func (c deviceColors) Converter(_ context.Context, space ast.PdfObject, _, _ ast.PdfDict, _ ast.PdfName) (Converter, error) {
	name, _ := space.(ast.PdfName)
	return converterFunc(func(in, out []float64) {
		var r, g, b float64
		switch name {
		case familyGray:
			r, g, b = in[0], in[0], in[0]
		case familyRGB:
			r, g, b = in[0], in[1], in[2]
		case familyCMYK:
			r = 1 - min(1, in[0]+in[3])
			g = 1 - min(1, in[1]+in[3])
			b = 1 - min(1, in[2]+in[3])
		}
		fromRGB(r, g, b, out)
	}), nil
}

func (deviceColors) SpotSpace(context.Context, ast.PdfObject, ast.PdfDict, ast.PdfDict) (ast.PdfObject, error) {
	return nil, fmt.Errorf("no spots")
}

func deviceTarget(name ast.PdfName) ImageConfig {
	n := map[ast.PdfName]int{familyGray: 1, familyRGB: 3, familyCMYK: 4}[name]
	return ImageConfig{Space: name, Components: n}
}

func fixedColors(c Colors) func(*document.Document, *writer.ObjAllocator) Colors {
	return func(*document.Document, *writer.ObjAllocator) Colors { return c }
}

func contains(list []string, s string) bool {
	return slices.ContainsFunc(list, func(v string) bool { return strings.Contains(v, s) })
}
