package flatten

import (
	"bytes"
	"context"
	"errors"
	"image"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/mutations"
	"github.com/gotenberg/litho/pkg/internal/render/geom"
)

// redactWithFallback redacts regions on the fixture's page with the raster
// fallback and returns the updated file and the report.
func redactWithFallback(t *testing.T, data []byte, regions [][4]float64) ([]byte, mutations.RedactReport) {
	t.Helper()
	return redactWith(t, data, regions, Redaction)
}

// redactWith is [redactWithFallback] with the fallback raster.
func redactWith(t *testing.T, data []byte, regions [][4]float64, raster mutations.RasterFunc) ([]byte, mutations.RedactReport) {
	t.Helper()
	ctx := context.Background()
	doc, pg := open(t, data)
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))
	report, err := mutations.RedactPageRegionsWith(ctx, alloc, doc, pg, regions, mutations.RedactOptions{Rasterise: raster})
	if err != nil {
		t.Fatalf("RedactPageRegionsWith: %v", err)
	}
	m := startXRef.FindSubmatch(data)
	if m == nil {
		t.Fatal("no startxref")
	}
	var offset int64
	for _, c := range m[1] {
		offset = offset*10 + int64(c-'0')
	}
	var out bytes.Buffer
	if err := writer.WriteIncremental(ctx, &out, writer.IncrementalConfig{
		Source:          bytes.NewReader(data),
		SourceSize:      int64(len(data)),
		StartXRefOffset: offset,
		Allocator:       alloc,
		Trailer:         doc.Index.Trailer,
	}); err != nil {
		t.Fatalf("WriteIncremental: %v", err)
	}
	return out.Bytes(), report
}

// textForm is a form XObject drawing a blue bar across the page's bottom and
// two words, LEFT at x 10 and RIGHT at x 60, at y 60.
var textForm = stream("/Type /XObject /Subtype /Form /BBox [0 0 100 100] /Resources << /Font << /F1 5 0 R >> >>",
	"0 0 1 rg 0 10 100 20 re f BT /F1 10 Tf 10 60 Td (LEFT) Tj 50 0 Td (RIGHT) Tj ET")

// TestRedactionRasterisesAFormItCuts redacts the right half of a page whose
// content is one form: the plain redaction refuses, and the fallback removes
// the covered word from the form's text, keeps the other, and keeps the bar
// outside the region while nothing of it paints inside.
func TestRedactionRasterisesAFormItCuts(t *testing.T) {
	t.Parallel()
	data := fixture{resources: "/XObject << /Fm0 7 0 R >>", content: "/Fm0 Do", objs: []string{textForm}}.bytes()
	region := [][4]float64{{55, 0, 100, 100}}

	ctx := context.Background()
	doc, pg := open(t, data)
	if _, err := mutations.RedactPageRegions(ctx, writer.NewObjAllocator(doc.HighestObjectNumber(ctx)), doc, pg, region); !errors.Is(err, mutations.ErrRegionInFormXObject) {
		t.Fatalf("plain redaction: err = %v, want ErrRegionInFormXObject", err)
	}

	out, report := redactWithFallback(t, data, region)
	if report.RasterisedUnits != 1 {
		t.Errorf("RasterisedUnits = %d, want 1", report.RasterisedUnits)
	}
	if strings.Join(report.RemovedText, "|") != "RIGHT" {
		t.Errorf("RemovedText = %q, want [RIGHT]", report.RemovedText)
	}
	if got := pageText(t, out); !strings.Contains(got, "LEFT") || strings.Contains(got, "RIGHT") {
		t.Errorf("page text = %q, want LEFT and no RIGHT", got)
	}
	img := renderAt(t, out, 72)
	if c := img.NRGBAAt(20, 80); c.B < 200 || c.R > 60 {
		t.Errorf("the bar outside the region reads %v, want blue", c)
	}
	if c := img.NRGBAAt(80, 80); c.R < 250 || c.G < 250 || c.B < 250 {
		t.Errorf("the bar inside the region reads %v, want paper", c)
	}
}

// TestRedactionKeepsOtherPlacementsOfAForm draws one form twice, each
// placement's box reaching the other's, and redacts the region over the
// second's words: both placements are replaced by copies holding their text,
// and the first keeps its words.
func TestRedactionKeepsOtherPlacementsOfAForm(t *testing.T) {
	t.Parallel()
	form := stream("/Type /XObject /Subtype /Form /BBox [0 0 100 100] /Resources << /Font << /F1 5 0 R >> >>",
		"BT /F1 10 Tf 10 10 Td (SECRET) Tj ET")
	data := fixture{
		resources: "/XObject << /Fm0 7 0 R >>",
		content:   "q 1 0 0 1 0 50 cm /Fm0 Do Q /Fm0 Do",
		objs:      []string{form},
	}.bytes()
	out, report := redactWithFallback(t, data, [][4]float64{{0, 0, 100, 40}})
	if strings.Join(report.RemovedText, "|") != "SECRET" {
		t.Errorf("RemovedText = %q, want one SECRET", report.RemovedText)
	}
	if report.RasterisedUnits != 2 {
		t.Errorf("RasterisedUnits = %d, want both placements", report.RasterisedUnits)
	}
	if got := pageText(t, out); strings.Count(got, "SECRET") != 1 {
		t.Errorf("page text = %q, want the first placement's SECRET alone", got)
	}
}

// TestRedactionEditsANestedFormsOwnCopy redacts text a form draws through a
// form it draws: the nested form is copied before its glyphs go.
func TestRedactionEditsANestedFormsOwnCopy(t *testing.T) {
	t.Parallel()
	outer := stream("/Type /XObject /Subtype /Form /BBox [0 0 100 100] /Resources << /XObject << /In 8 0 R >> >>",
		"/In Do q 1 0 0 1 0 50 cm /In Do Q")
	inner := stream("/Type /XObject /Subtype /Form /BBox [0 0 100 40] /Resources << /Font << /F1 5 0 R >> >>",
		"BT /F1 10 Tf 10 10 Td (AB) Tj 20 0 Td (CD) Tj ET")
	data := fixture{resources: "/XObject << /Fm0 7 0 R >>", content: "/Fm0 Do", objs: []string{outer, inner}}.bytes()
	out, report := redactWithFallback(t, data, [][4]float64{{0, 0, 25, 40}})
	if strings.Join(report.RemovedText, "|") != "AB" {
		t.Errorf("RemovedText = %q, want [AB]", report.RemovedText)
	}
	if got := pageText(t, out); strings.Count(got, "AB") != 1 || strings.Count(got, "CD") != 2 {
		t.Errorf("page text = %q, want both CDs and the upper placement's AB", got)
	}
}

// TestRedactionRasterisesAShading redacts the centre of a page an axial
// shading fills: the fallback removes the sh, and the image standing for it
// keeps the shading outside the region and paints nothing inside.
func TestRedactionRasterisesAShading(t *testing.T) {
	t.Parallel()
	shading := "<< /ShadingType 2 /ColorSpace /DeviceRGB /Coords [0 0 100 0] " +
		"/Function << /FunctionType 2 /Domain [0 1] /C0 [1 0 0] /C1 [0 0 1] /N 1 >> >>"
	data := fixture{resources: "/Shading << /Sh0 7 0 R >>", content: "/Sh0 sh", objs: []string{shading}}.bytes()
	region := [][4]float64{{25, 25, 75, 75}}

	ctx := context.Background()
	doc, pg := open(t, data)
	if _, err := mutations.RedactPageRegions(ctx, writer.NewObjAllocator(doc.HighestObjectNumber(ctx)), doc, pg, region); !errors.Is(err, mutations.ErrRegionCoversAShading) {
		t.Fatalf("plain redaction: err = %v, want ErrRegionCoversAShading", err)
	}
	out, report := redactWithFallback(t, data, region)
	if report.RasterisedUnits != 1 {
		t.Errorf("RasterisedUnits = %d, want 1", report.RasterisedUnits)
	}
	before, after := renderAt(t, data, 72), renderAt(t, out, 72)
	for _, p := range []image.Point{{5, 5}, {95, 50}, {50, 95}} {
		b, a := before.NRGBAAt(p.X, p.Y), after.NRGBAAt(p.X, p.Y)
		if absDiff(b.R, a.R) > 8 || absDiff(b.B, a.B) > 8 {
			t.Errorf("outside at %v: %v before, %v after", p, b, a)
		}
	}
	if c := after.NRGBAAt(50, 50); c.R < 250 || c.G < 250 || c.B < 250 {
		t.Errorf("inside reads %v, want paper", c)
	}
	doc2, pg2 := open(t, out)
	ops, err := content.ParsePageContent(ctx, doc2, pg2.Dict)
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range ops {
		if op.Operator == "sh" {
			t.Error("the page still paints the shading")
		}
	}
	// The clip keeps the image off the region, and its samples there hold
	// nothing of the shading either: the file is what a reader extracts.
	resources, err := document.ResolveDict(ctx, doc2, pg2.Dict["Resources"])
	if err != nil {
		t.Fatal(err)
	}
	xobjects, err := document.ResolveDict(ctx, doc2, resources["XObject"])
	if err != nil {
		t.Fatal(err)
	}
	images := 0
	for _, obj := range xobjects {
		img, err := document.ResolveStream(ctx, doc2, obj)
		if err != nil || img.Dict["Subtype"] != ast.PdfName("Image") {
			continue
		}
		images++
		samples, err := doc2.DecodeStream(ctx, img)
		if err != nil {
			t.Fatal(err)
		}
		w, h := int(document.Float(img.Dict["Width"])), int(document.Float(img.Dict["Height"]))
		i := 3 * (h/2*w + w/2)
		if !bytes.Equal(samples[i:i+3], []byte{255, 255, 255}) {
			t.Errorf("the image's centre sample is %v, want paper", samples[i:i+3])
		}
	}
	if images != 1 {
		t.Errorf("the page draws %d images, want the one standing for the shading", images)
	}
}

func absDiff(a, b uint8) uint8 {
	if a > b {
		return a - b
	}
	return b - a
}

// TestRedactionRefusesAStagedPage pins [ErrPageStaged]: the renderer reads the
// document, so a page an edit queued would be composited as it was.
func TestRedactionRefusesAStagedPage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc, pg := open(t, fixture{content: "/Sh0 sh"}.bytes())
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))
	alloc.AllocAt(pg.Ref.ObjectNum, pg.Dict)
	if _, err := Redaction(ctx, alloc, doc, pg, []int{0}, [][4]float64{{0, 0, 10, 10}}); !errors.Is(err, ErrPageStaged) {
		t.Errorf("err = %v, want ErrPageStaged", err)
	}
}

// TestDisjoint pins that overlapping holes come back as boxes sharing no area
// and covering their union: the even-odd rule would cut an area two holes
// share back into the clip.
func TestDisjoint(t *testing.T) {
	t.Parallel()
	got := disjoint([]Rect{rectOf(0, 0, 10, 10), rectOf(5, 5, 15, 15), rectOf(20, 0, 30, 1)})
	area := 0.0
	for i, a := range got {
		area += (a.MaxX - a.MinX) * (a.MaxY - a.MinY)
		for _, b := range got[i+1:] {
			if a.Overlaps(b) {
				t.Errorf("%v and %v share area", a, b)
			}
		}
	}
	if want := 100.0 + 100 - 25 + 10; area != want {
		t.Errorf("area %v, want the union's %v", area, want)
	}
}

// TestPaintOut pins that every pixel a box reaches, whole or in part, takes
// the paper's colour, and no other.
func TestPaintOut(t *testing.T) {
	t.Parallel()
	px := image.Rect(10, 10, 14, 12)
	samples := make([]byte, 3*px.Dx()*px.Dy())
	// Device pixels from user space at one unit a pixel: x 11.5 to 12.2
	// reaches columns 11 and 12.
	paintOut(samples, px, 3, geom.Identity, []Rect{rectOf(11.5, 10, 12.2, 11)})
	for y := px.Min.Y; y < px.Max.Y; y++ {
		for x := px.Min.X; x < px.Max.X; x++ {
			i := 3 * ((y-px.Min.Y)*px.Dx() + x - px.Min.X)
			want := byte(0)
			if (x == 11 || x == 12) && y == 10 {
				want = 255
			}
			if samples[i] != want {
				t.Errorf("pixel (%d, %d) = %d, want %d", x, y, samples[i], want)
			}
		}
	}
	cmyk := make([]byte, 4)
	cmyk[3] = 9
	paintOut(cmyk, image.Rect(0, 0, 1, 1), 4, geom.Identity, []Rect{rectOf(0, 0, 1, 1)})
	if !bytes.Equal(cmyk, []byte{0, 0, 0, 0}) {
		t.Errorf("CMYK paper = %v, want no ink", cmyk)
	}
}
