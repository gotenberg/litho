package flatten

import (
	"context"
	"encoding/hex"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/render/geom"
)

// photoFormPDF builds a page a form fills with a 208 by 208 RGB image of
// smooth waves under a fine grain, a photograph to the photographic test,
// whose grain Flate compresses worse than JPEG.
func photoFormPDF() []byte {
	const side = 208
	samples := make([]byte, 0, 3*side*side)
	state := uint32(1)
	grain := func() float64 {
		state = state*1664525 + 1013904223
		return float64(state>>28) - 8
	}
	for y := range side {
		for x := range side {
			fx, fy := float64(x), float64(y)
			samples = append(samples,
				byte(128+90*math.Sin(fx/17)+grain()),
				byte(128+90*math.Cos(fy/13)+grain()),
				byte(128+90*math.Sin((fx+fy)/23)+grain()))
		}
	}
	img := stream(fmt.Sprintf("/Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /ASCIIHexDecode", side, side),
		hex.EncodeToString(samples)+">")
	form := stream("/Type /XObject /Subtype /Form /BBox [0 0 100 100] /Resources << /XObject << /Im 8 0 R >> >>",
		"q 100 0 0 100 0 0 cm /Im Do Q")
	return fixture{resources: "/XObject << /Fm0 7 0 R >>", content: "/Fm0 Do", objs: []string{form, img}}.bytes()
}

// placedImage is an image the page draws: its stream and the matrix mapping
// the unit square onto it in default user space.
type placedImage struct {
	stream ast.PdfStream
	ctm    geom.Matrix
}

// placedImages returns the images the page of data draws at its top level,
// each with the CTM at its Do.
func placedImages(t *testing.T, data []byte) []placedImage {
	t.Helper()
	ctx := context.Background()
	doc, pg := open(t, data)
	ops, err := content.ParsePageContent(ctx, doc, pg.Dict)
	if err != nil {
		t.Fatal(err)
	}
	resources, err := document.ResolveDict(ctx, doc, pg.Dict["Resources"])
	if err != nil {
		t.Fatal(err)
	}
	xobjects, err := document.ResolveDict(ctx, doc, resources["XObject"])
	if err != nil {
		t.Fatal(err)
	}
	var out []placedImage
	ctm := geom.Identity
	var stack []geom.Matrix
	for _, op := range ops {
		switch op.Operator {
		case "q":
			stack = append(stack, ctm)
		case "Q":
			if len(stack) > 0 {
				ctm, stack = stack[len(stack)-1], stack[:len(stack)-1]
			}
		case "cm":
			var m geom.Matrix
			for i := range m {
				m[i] = document.Float(op.Operands[i])
			}
			ctm = m.Mul(ctm)
		case "Do":
			name, _ := op.Operands[0].(ast.PdfName)
			s, err := document.ResolveStream(ctx, doc, xobjects[name])
			if err != nil || s.Dict["Subtype"] != ast.PdfName("Image") {
				continue
			}
			out = append(out, placedImage{stream: s, ctm: ctm})
		}
	}
	return out
}

// TestRedactionJPEGKeepsRegionsPaper redacts the centre of a page a form
// fills with a photograph, with the fallback encoding JPEG at quality 75: some
// tiles are JPEG, none of them reaches the region, and every sample of a
// lossless tile whose pixel centre lies in the region is the paper's colour
// exactly. Lossless, the same redaction draws no JPEG.
func TestRedactionJPEGKeepsRegionsPaper(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := photoFormPDF()
	region := Rect{MinX: 40, MinY: 40, MaxX: 60, MaxY: 60}
	regions := [][4]float64{{region.MinX, region.MinY, region.MaxX, region.MaxY}}

	lossless, _ := redactWith(t, data, regions, Redaction)
	for _, p := range placedImages(t, lossless) {
		if p.stream.Dict["Filter"] == ast.PdfName("DCTDecode") {
			t.Fatal("the lossless fallback drew a JPEG")
		}
	}

	out, report := redactWith(t, data, regions, RedactionJPEG(75))
	if report.RasterisedUnits != 1 {
		t.Errorf("RasterisedUnits = %d, want 1", report.RasterisedUnits)
	}
	if len(out) >= len(lossless) {
		t.Errorf("the JPEG output takes %d bytes, the lossless one %d", len(out), len(lossless))
	}
	doc, _ := open(t, out)
	jpegs := 0
	for _, p := range placedImages(t, out) {
		box := transformRect(p.ctm, Rect{MaxX: 1, MaxY: 1})
		inside := box.Intersect(region)
		if p.stream.Dict["Filter"] == ast.PdfName("DCTDecode") {
			jpegs++
			if !inside.Empty() {
				t.Errorf("a JPEG tile over %v reaches the region by %v", box, inside)
			}
			continue
		}
		if inside.Empty() {
			continue
		}
		samples, err := doc.DecodeStream(ctx, p.stream)
		if err != nil {
			t.Fatal(err)
		}
		w, h := int(document.Float(p.stream.Dict["Width"])), int(document.Float(p.stream.Dict["Height"]))
		if len(samples) != 3*w*h {
			t.Fatalf("a tile holds %d samples, want %d", len(samples), 3*w*h)
		}
		for y := range h {
			for x := range w {
				c := p.ctm.Apply(geom.Point{X: (float64(x) + 0.5) / float64(w), Y: 1 - (float64(y)+0.5)/float64(h)})
				if c.X <= region.MinX || c.X >= region.MaxX || c.Y <= region.MinY || c.Y >= region.MaxY {
					continue
				}
				i := 3 * (y*w + x)
				if samples[i] != 255 || samples[i+1] != 255 || samples[i+2] != 255 {
					t.Fatalf("sample (%d, %d) at %v inside the region is %v, want paper", x, y, c, samples[i:i+3])
				}
			}
		}
	}
	if jpegs == 0 {
		t.Error("no tile was encoded as JPEG")
	}
	if text := pageText(t, out); strings.TrimSpace(text) != "" {
		t.Errorf("page text = %q, want none", text)
	}
}

// TestHolePixelsCoverTheOverlap pins that a hole widened for the JPEG tiles
// reaches at least as far as a tile reaches into its neighbours, so no JPEG
// tile holds a painted-out pixel.
func TestHolePixelsCoverTheOverlap(t *testing.T) {
	t.Parallel()
	if pixelMargin < tileOverlap {
		t.Fatalf("pixelMargin %d is under tileOverlap %d", pixelMargin, tileOverlap)
	}
}
