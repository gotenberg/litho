package page

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/render/glyph"
)

// white is the background every fixture is drawn over, as pdftoppm draws.
var white = [4]float32{1, 1, 1, 1}

// fixture is a one-page PDF: object 3 is the page, object 4 its content,
// and objs are objects 5 onwards.
type fixture struct {
	// page holds the page dictionary's entries besides /Type, /Parent and
	// /Contents; /MediaBox [0 0 100 100] when it states none.
	page string
	// catalog holds extra entries of the catalog.
	catalog string
	content string
	objs    []string
	// substitute is the program served for every font the fixture does not
	// embed, nil for none: such fonts then draw placeholders.
	substitute []byte
}

// options returns the options a fixture is drawn with: 72 dpi over white,
// with its substitute program, if any.
func (f fixture) options() Options {
	opts := Options{DPI: 72, Background: white}
	if f.substitute != nil {
		opts.Fonts = func(context.Context, glyph.Request) ([]glyph.Candidate, error) {
			return []glyph.Candidate{func(context.Context) ([]byte, error) { return f.substitute, nil }}, nil
		}
	}
	return opts
}

// bytes serialises the fixture.
func (f fixture) bytes() []byte {
	page := f.page
	if !strings.Contains(page, "/MediaBox") {
		page += " /MediaBox [0 0 100 100]"
	}
	objs := append([]string{
		"<< /Type /Catalog /Pages 2 0 R " + f.catalog + " >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /Contents 4 0 R " + page + " >>",
		stream("", f.content),
	}, f.objs...)
	return buildPDF(objs)
}

// stream writes a stream object with extra dictionary entries.
func stream(dict, data string) string {
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

// open parses data and returns its first page.
func open(t testing.TB, data []byte, lim limits.Limits) (*document.Document, *document.Page) {
	t.Helper()
	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil, document.WithLimits(lim))
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil || len(pages) == 0 {
		t.Fatalf("ResolvePages: %d pages, %v", len(pages), err)
	}
	return doc, pages[0]
}

// draw renders the fixture at 72 dpi over white.
func draw(t testing.TB, f fixture) (*image.NRGBA, Report) {
	t.Helper()
	doc, pg := open(t, f.bytes(), limits.Limits{})
	c, report, err := Render(context.Background(), doc, pg, f.options())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	return c.Image(), report
}

// rgb is an opaque 8-bit colour.
type rgb struct{ r, g, b uint8 }

var (
	black = rgb{0, 0, 0}
	paper = rgb{255, 255, 255}
	red   = rgb{255, 0, 0}
	blue  = rgb{0, 0, 255}
)

// at reads pixel (x, y), failing the test when it is not opaque.
func at(t testing.TB, img *image.NRGBA, x, y int) rgb {
	t.Helper()
	c := img.NRGBAAt(x, y)
	if c.A != 255 {
		t.Fatalf("pixel (%d,%d) = %v, not opaque", x, y, c)
	}
	return rgb{c.R, c.G, c.B}
}

// close8 reports whether a and b differ by at most tol in every channel.
func close8(a, b rgb, tol int) bool {
	d := func(x, y uint8) int {
		if x > y {
			return int(x - y)
		}
		return int(y - x)
	}
	return d(a.r, b.r) <= tol && d(a.g, b.g) <= tol && d(a.b, b.b) <= tol
}

// expect checks pixels against colours within tol.
func expect(t testing.TB, img *image.NRGBA, tol int, want map[[2]int]rgb) {
	t.Helper()
	for p, w := range want {
		if got := at(t, img, p[0], p[1]); !close8(got, w, tol) {
			t.Errorf("pixel (%d,%d) = %v, want %v", p[0], p[1], got, w)
		}
	}
}

// toNRGBA converts any image for comparison.
func toNRGBA(img image.Image) *image.NRGBA {
	if n, ok := img.(*image.NRGBA); ok {
		return n
	}
	b := img.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := range b.Dy() {
		for x := range b.Dx() {
			out.Set(x, y, color.NRGBAModel.Convert(img.At(b.Min.X+x, b.Min.Y+y)))
		}
	}
	return out
}
