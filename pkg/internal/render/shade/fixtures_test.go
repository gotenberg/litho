package shade

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/render/geom"
	"github.com/gotenberg/litho/pkg/internal/render/surface"
)

// pageSide is the side of every fixture page, in points. The oracle renders
// it at 72 dpi, one pixel per point.
const pageSide = 100

// pageToDevice maps the fixture page's user space onto a pageSide-pixel
// canvas, y flipped, as a 72 dpi render does.
var pageToDevice = geom.Matrix{1, 0, 0, -1, 0, pageSide}

// fixture is a one-page PDF whose page paints shading object 5 or, through
// pattern object 6, fills a shape.
type fixture struct {
	content string // the page's content stream
	shading string // object 5
	pattern string // object 6, a pattern, may be empty
	extra   string // object 6 when there is no pattern, may be empty
}

// bytes serialises the fixture as a PDF.
func (f fixture) bytes() []byte {
	resources := "/Shading << /Sh 5 0 R >>"
	if f.pattern != "" {
		resources += " /Pattern << /P 6 0 R >>"
	}
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %d %d] /Resources << %s >> /Contents 4 0 R >>",
			pageSide, pageSide, resources),
		streamObj("", f.content),
		f.shading,
	}
	switch {
	case f.pattern != "":
		objs = append(objs, f.pattern)
	case f.extra != "":
		objs = append(objs, f.extra)
	}
	return buildPDF(objs)
}

// streamObj writes a stream object with extra dictionary entries.
func streamObj(dict, data string) string {
	return fmt.Sprintf("<< %s /Length %d >>\nstream\n%s\nendstream", dict, len(data), data)
}

// buildPDF serialises objs as objects 1 to n with a classic xref table.
func buildPDF(objs []string) []byte {
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
	return buf.Bytes()
}

// openFixture parses data.
func openFixture(t testing.TB, data []byte) (context.Context, *document.Document) {
	t.Helper()
	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	return ctx, doc
}

// deviceComponents resolves the three device spaces, all the fixtures use.
func deviceComponents(_ context.Context, cs ast.PdfObject) (int, error) {
	switch cs {
	case ast.PdfName("DeviceGray"):
		return 1, nil
	case ast.PdfName("DeviceRGB"):
		return 3, nil
	case ast.PdfName("DeviceCMYK"):
		return 4, nil
	}
	return 0, fmt.Errorf("unsupported colour space %v", cs)
}

// deviceRGB converts DeviceGray and DeviceRGB components without colour
// management, as the oracle does for device spaces.
type deviceRGB struct{}

func (deviceRGB) RGB(c []float64) (r, g, b float32) {
	if len(c) == 1 {
		return float32(c[0]), float32(c[0]), float32(c[0])
	}
	return float32(c[0]), float32(c[1]), float32(c[2])
}

// renderFixture paints the fixture's shading as "/Sh sh" over the whole
// page, or fills it as a pattern over clip when pattern is set, on a white
// canvas.
func renderFixture(t testing.TB, f fixture, clip *surface.Mask) *surface.Canvas {
	t.Helper()
	ctx, doc := openFixture(t, f.bytes())
	canvas, err := surface.NewCanvas(pageSide, pageSide, pageSide*pageSide)
	if err != nil {
		t.Fatal(err)
	}
	canvas.Fill(1, 1, 1, 1)
	target := Target{Canvas: canvas, Clip: clip, Alpha: 1, Convert: deviceRGB{}}
	if f.pattern != "" {
		pat, err := NewPattern(ctx, doc, ast.PdfIndirectRef{ObjectNum: 6}, deviceComponents)
		if err != nil {
			t.Fatalf("NewPattern: %v", err)
		}
		if err := pat.Fill(ctx, target, pageToDevice); err != nil {
			t.Fatalf("Fill: %v", err)
		}
		return canvas
	}
	sh, err := New(ctx, doc, ast.PdfIndirectRef{ObjectNum: 5}, deviceComponents)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := sh.Paint(ctx, target, pageToDevice); err != nil {
		t.Fatalf("Paint: %v", err)
	}
	return canvas
}

// rgbFunc is a type 2 function from the RGB colour c0 to c1.
func rgbFunc(c0, c1 string) string {
	return fmt.Sprintf("<< /FunctionType 2 /Domain [0 1] /C0 [%s] /C1 [%s] /N 1 >>", c0, c1)
}

// packBits packs fields MSB first: each pair is (value, width in bits); a
// width of 0 aligns to the next byte.
func packBits(fields ...uint64) string {
	var out []byte
	var acc uint64
	n := 0
	for i := 0; i < len(fields); i += 2 {
		v, w := fields[i], int(fields[i+1])
		if w == 0 {
			if n > 0 {
				out = append(out, byte(acc<<(8-n)))
				acc, n = 0, 0
			}
			continue
		}
		for k := w - 1; k >= 0; k-- {
			acc = acc<<1 | (v>>uint(k))&1
			n++
			if n == 8 {
				out = append(out, byte(acc))
				acc, n = 0, 0
			}
		}
	}
	if n > 0 {
		out = append(out, byte(acc<<(8-n)))
	}
	return string(out)
}

// The oracle fixtures: each is rendered by the pinned poppler and by this
// package, and the tests compare sample pixels.
var (
	// fixtureAxial runs red to blue from x=10 to x=90, extended before the
	// start only.
	fixtureAxial = fixture{
		content: "/Sh sh",
		shading: "<< /ShadingType 2 /ColorSpace /DeviceRGB /Coords [10 0 90 0] /Extend [true false] /Function " +
			rgbFunc("1 0 0", "0 0 1") + " >>",
	}

	// fixtureRadial is a cone: the start circle (20,50,r5) lies outside the
	// end circle (70,50,r20), extended at both ends, so the painted area is
	// the cone tangent to both and the larger-s rule decides every overlap.
	fixtureRadial = fixture{
		content: "/Sh sh",
		shading: "<< /ShadingType 3 /ColorSpace /DeviceRGB /Coords [20 50 5 70 50 20] /Extend [true true] /Function " +
			rgbFunc("1 1 0", "0 0.5 1") + " >>",
	}

	// fixtureGouraud is one free-form triangle: red at (10,10), green at
	// (90,10), blue at (50,90).
	fixtureGouraud = fixture{
		content: "/Sh sh",
		shading: streamObj("/ShadingType 4 /ColorSpace /DeviceRGB /BitsPerCoordinate 8 /BitsPerComponent 8"+
			" /BitsPerFlag 8 /Decode [0 255 0 255 0 1 0 1 0 1]",
			packBits(0, 8, 10, 8, 10, 8, 255, 8, 0, 8, 0, 8,
				0, 8, 90, 8, 10, 8, 0, 8, 255, 8, 0, 8,
				0, 8, 50, 8, 90, 8, 0, 8, 0, 8, 255, 8)),
	}

	// fixtureCoons is one Coons patch with bulging edges and four corner
	// colours: c00 red at (10,10), c03 green at (10,90), c33 blue at
	// (90,90), c30 white at (90,10).
	fixtureCoons = fixture{
		content: "/Sh sh",
		shading: streamObj("/ShadingType 6 /ColorSpace /DeviceRGB /BitsPerCoordinate 8 /BitsPerComponent 8"+
			" /BitsPerFlag 8 /Decode [0 255 0 255 0 1 0 1 0 1]",
			packBits(0, 8,
				10, 8, 10, 8, 0, 8, 37, 8, 0, 8, 63, 8, 10, 8, 90, 8, // p00 p01 p02 p03 (left edge bulges left)
				37, 8, 90, 8, 63, 8, 90, 8, 90, 8, 90, 8, // p13 p23 p33
				90, 8, 63, 8, 90, 8, 37, 8, 90, 8, 10, 8, // p32 p31 p30
				63, 8, 10, 8, 37, 8, 10, 8, // p20 p10
				255, 8, 0, 8, 0, 8, 0, 8, 255, 8, 0, 8, 0, 8, 0, 8, 255, 8, 255, 8, 255, 8, 255, 8)),
	}

	// fixtureCoonsShared is two Coons patches: the second continues the
	// first through flag 1, so it shares the first's edge D2 (its top) and
	// the colours of that edge's corners.
	fixtureCoonsShared = fixture{
		content: "/Sh sh",
		shading: streamObj("/ShadingType 6 /ColorSpace /DeviceRGB /BitsPerCoordinate 8 /BitsPerComponent 8"+
			" /BitsPerFlag 8 /Decode [0 255 0 255 0 1 0 1 0 1]",
			packBits(0, 8,
				10, 8, 10, 8, 10, 8, 23, 8, 10, 8, 37, 8, 10, 8, 50, 8,
				37, 8, 50, 8, 63, 8, 50, 8, 90, 8, 50, 8,
				90, 8, 37, 8, 90, 8, 23, 8, 90, 8, 10, 8,
				63, 8, 10, 8, 37, 8, 10, 8,
				255, 8, 0, 8, 0, 8, 0, 8, 255, 8, 0, 8, 0, 8, 0, 8, 255, 8, 0, 8, 0, 8, 0, 8,
				1, 8, // flag 1: the new D1 is the old top edge, from (10,50) to (90,50)
				90, 8, 63, 8, 90, 8, 77, 8, 90, 8, 90, 8, // new p13 p23 p33: up the right side
				63, 8, 90, 8, 37, 8, 90, 8, 10, 8, 90, 8, // p32 p31 p30: back along the top
				10, 8, 77, 8, 10, 8, 63, 8, // p20 p10: down the left side
				255, 8, 255, 8, 255, 8, 128, 8, 128, 8, 128, 8)),
	}

	// fixtureLattice is a 2x2 lattice in DeviceGray with 20-bit vertices
	// (8+8+4), so whether each vertex starts on a byte boundary decides
	// what the stream means.
	fixtureLattice = fixture{
		content: "/Sh sh",
		shading: streamObj("/ShadingType 5 /ColorSpace /DeviceGray /BitsPerCoordinate 8 /BitsPerComponent 4"+
			" /VerticesPerRow 2 /Decode [0 255 0 255 0 1]",
			packBits(10, 8, 10, 8, 0, 4, 0, 0,
				90, 8, 10, 8, 15, 4, 0, 0,
				10, 8, 90, 8, 5, 4, 0, 0,
				90, 8, 90, 8, 10, 4, 0, 0)),
	}

	// fixtureFunctionBased maps the unit square onto (10,10)-(90,90) and
	// paints the grey (x+y)/2 of a PostScript function over it.
	fixtureFunctionBased = fixture{
		content: "/Sh sh",
		shading: "<< /ShadingType 1 /ColorSpace /DeviceGray /Matrix [80 0 0 80 10 10] /Function 6 0 R >>",
		extra:   streamObj("/FunctionType 4 /Domain [0 1 0 1] /Range [0 1]", "{ add 2 div }"),
	}

	// fixtureTensor is one tensor-product patch over (10,10)-(90,90) whose
	// interior points are pulled towards the c00 corner, so it differs from
	// the Coons patch of the same boundary.
	fixtureTensor = fixture{
		content: "/Sh sh",
		shading: streamObj("/ShadingType 7 /ColorSpace /DeviceRGB /BitsPerCoordinate 8 /BitsPerComponent 8"+
			" /BitsPerFlag 8 /Decode [0 255 0 255 0 1 0 1 0 1]",
			packBits(0, 8,
				10, 8, 10, 8, 10, 8, 37, 8, 10, 8, 63, 8, 10, 8, 90, 8, // p00 p01 p02 p03
				37, 8, 90, 8, 63, 8, 90, 8, 90, 8, 90, 8, // p13 p23 p33
				90, 8, 63, 8, 90, 8, 37, 8, 90, 8, 10, 8, // p32 p31 p30
				63, 8, 10, 8, 37, 8, 10, 8, // p20 p10
				15, 8, 15, 8, 15, 8, 40, 8, 40, 8, 40, 8, 40, 8, 15, 8, // p11 p12 p22 p21
				255, 8, 0, 8, 0, 8, 0, 8, 255, 8, 0, 8, 0, 8, 0, 8, 255, 8, 255, 8, 255, 8, 255, 8)),
	}

	// fixtureStrip is a free-form strip of three triangles chained by edge
	// flags 1 and 2, in DeviceGray with a /Function mapping t to grey.
	fixtureStrip = fixture{
		content: "/Sh sh",
		shading: streamObj("/ShadingType 4 /ColorSpace /DeviceGray /BitsPerCoordinate 8 /BitsPerComponent 8"+
			" /BitsPerFlag 8 /Decode [0 255 0 255 0 1] /Function << /FunctionType 2 /Domain [0 1] /C0 [0.2] /C1 [1] /N 1 >>",
			packBits(0, 8, 10, 8, 10, 8, 0, 8,
				0, 8, 50, 8, 10, 8, 128, 8,
				0, 8, 10, 8, 50, 8, 64, 8,
				1, 8, 50, 8, 50, 8, 255, 8, // (vb, vc, v): (50,10) (10,50) (50,50)
				2, 8, 90, 8, 90, 8, 0, 8)), // (va, vc, v): (50,10) (50,50) (90,90)
	}

	// fixturePattern fills the square (20,20)-(80,80) with an axial pattern
	// whose matrix scales and shifts it, and whose /Background (grey) shows
	// where the unextended axis ends.
	fixturePattern = fixture{
		content: "/Pattern cs /P scn 20 20 60 60 re f",
		shading: "<< /ShadingType 2 /ColorSpace /DeviceRGB /Background [0.5 0.5 0.5] /Coords [0 0 20 0] /Function " +
			rgbFunc("0 1 0", "1 0 1") + " >>",
		pattern: "<< /PatternType 2 /Shading 5 0 R /Matrix [2 0 0 2 30 0] >>",
	}
)
