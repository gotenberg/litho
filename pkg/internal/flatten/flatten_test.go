package flatten

import (
	"bytes"
	"context"
	"image"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/render/page"
	"github.com/gotenberg/litho/pkg/internal/typography"
)

var startXRef = regexp.MustCompile(`startxref\s+(\d+)\s+%%EOF\s*$`)

// flattenBytes runs [Document] over data and returns the updated file.
func flattenBytes(t testing.TB, data []byte, opts Options) ([]byte, *Report) {
	t.Helper()
	ctx := context.Background()
	doc, pg := open(t, data)
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))
	report, err := Document(ctx, alloc, doc, []*document.Page{pg}, opts)
	if err != nil {
		t.Fatalf("Document: %v", err)
	}
	m := startXRef.FindSubmatch(data)
	if m == nil {
		t.Fatal("no startxref")
	}
	offset, err := strconv.ParseInt(string(m[1]), 10, 64)
	if err != nil {
		t.Fatal(err)
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

// renderAt draws data's first page at dpi over white.
func renderAt(t testing.TB, data []byte, dpi float64) *image.NRGBA {
	t.Helper()
	doc, pg := open(t, data)
	c, _, err := page.Render(context.Background(), doc, pg, page.Options{DPI: dpi, Background: [4]float32{1, 1, 1, 1}})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	return c.Image()
}

// differing returns the share of pixels of a and b whose channels differ by
// more than tolerance levels.
func differing(a, b *image.NRGBA, tolerance int) float64 {
	if a.Bounds() != b.Bounds() {
		return 1
	}
	n := 0
	for i := 0; i < len(a.Pix); i += 4 {
		for c := range 3 {
			d := int(a.Pix[i+c]) - int(b.Pix[i+c])
			if d > tolerance || d < -tolerance {
				n++
				break
			}
		}
	}
	return float64(n) / float64(len(a.Pix)/4)
}

// pageText extracts the text of data's first page.
func pageText(t testing.TB, data []byte) string {
	t.Helper()
	ctx := context.Background()
	doc, pg := open(t, data)
	ops, err := content.ParsePageContent(ctx, doc, pg.Dict)
	if err != nil {
		t.Fatalf("parsing content: %v", err)
	}
	text, err := typography.ExtractText(ctx, doc, graphics.NewMachine([4]float64{}), pg, ops, typography.ExtractionConfig{})
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	return text
}

// assertOpaque checks the first page of data paints no transparency.
func assertOpaque(t testing.TB, data []byte) {
	t.Helper()
	ctx := context.Background()
	doc, pg := open(t, data)
	ops, err := content.ParsePageContent(ctx, doc, pg.Dict, content.SplitGluedOperators())
	if err != nil {
		t.Fatalf("parsing content: %v", err)
	}
	a, err := Analyze(ctx, doc, pg, ops)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(a.Regions) != 0 || len(a.TransparentText) != 0 {
		t.Errorf("flattened page still paints with transparency: units %q", kinds(a))
	}
	if groupTransparency(ctx, doc, pg.Dict) {
		t.Error("flattened page keeps its transparency group")
	}
}

// checkFlattened flattens f and checks the result draws like it, keeps its
// text and paints no transparency.
func checkFlattened(t *testing.T, f fixture) ([]byte, *Report) {
	t.Helper()
	return checkFlattenedWithin(t, f, 8)
}

// checkFlattenedWithin is [checkFlattened] with pixels allowed tolerance
// levels apart.
func checkFlattenedWithin(t *testing.T, f fixture, tolerance int) ([]byte, *Report) {
	t.Helper()
	in := f.bytes()
	out, report := flattenBytes(t, in, Options{})
	for _, dpi := range []float64{72, 150} {
		if d := differing(renderAt(t, in, dpi), renderAt(t, out, dpi), tolerance); d > 0.01 {
			t.Errorf("at %v dpi %.2f%% of the pixels differ", dpi, 100*d)
		}
	}
	if a, b := pageText(t, in), pageText(t, out); a != b {
		t.Errorf("text %q, want %q", b, a)
	}
	assertOpaque(t, out)
	return out, report
}

func TestFlattenTransparentSquare(t *testing.T) {
	t.Parallel()
	out, report := checkFlattened(t, fixture{
		page:    "/Group << /S /Transparency /CS /DeviceRGB >>",
		content: "0 0 1 rg 0 0 100 100 re f q /GS1 gs 1 0 0 rg 10 10 30 30 re f Q 0 g 80 80 10 10 re f",
	})
	if len(report.Pages) != 1 {
		t.Fatalf("report %+v", report)
	}
	pr := report.Pages[0]
	if pr.Regions != 1 || pr.Removed != 1 || pr.DPI != DefaultDPI || !pr.GroupRemoved {
		t.Errorf("report %+v", pr)
	}
	if !bytes.Contains(out, []byte("/Artifact BMC")) {
		t.Error("the composite is not marked as an artifact")
	}
}

func TestFlattenKeepsTextAboveTheComposite(t *testing.T) {
	t.Parallel()
	_, report := checkFlattened(t, fixture{
		content: "q /GS1 gs 0 0 1 rg 0 0 60 60 re f Q BT /F1 12 Tf 10 20 Td (Hello) Tj ET",
	})
	pr := report.Pages[0]
	if pr.KeptText != 1 || pr.TransparentText != 0 || pr.TextUnderTransparency != 0 {
		t.Errorf("report %+v, want one text object kept", pr)
	}
}

func TestFlattenDrawsTransparentTextOpaque(t *testing.T) {
	t.Parallel()
	// ISO 32000-2, 11.3.7.2: the text's colour composited with its ca of
	// 0.5 over what lies below has no single opaque colour in general, so
	// the text keeps its own colour, drawn opaque, and is counted.
	in := fixture{content: "/GS1 gs 0 0 1 rg 0 0 60 60 re f BT /F1 12 Tf 10 20 Td (Hello) Tj ET"}.bytes()
	out, report := flattenBytes(t, in, Options{})
	if pr := report.Pages[0]; pr.KeptText != 1 || pr.TransparentText != 1 {
		t.Errorf("report %+v, want one text object kept and counted transparent", pr)
	}
	if a, b := pageText(t, in), pageText(t, out); a != b {
		t.Errorf("text %q, want %q", b, a)
	}
	assertOpaque(t, out)
}

func TestFlattenFormShowingText(t *testing.T) {
	t.Parallel()
	out, report := checkFlattened(t, fixture{
		resources: "/XObject << /Fm 7 0 R >>",
		content:   "1 1 0 rg 0 0 100 100 re f /Fm Do",
		objs: []string{stream("/Type /XObject /Subtype /Form /BBox [0 0 100 100] /Group << /S /Transparency >> /Resources << /ExtGState << /G 6 0 R >> /Font << /F 5 0 R >> >>",
			"q /G gs 0 0 1 rg 10 10 50 50 re f Q BT /F 10 Tf 20 30 Td (Inside) Tj ET")},
	})
	if pr := report.Pages[0]; pr.Removed != 1 {
		t.Errorf("report %+v", pr)
	}
	if !strings.Contains(string(out), "/FlatTx") {
		t.Error("the form's text was not kept in a copy")
	}
}

func TestFlattenClipsWhatTheCompositeHolds(t *testing.T) {
	t.Parallel()
	// The long rule crosses the region of the two transparent squares drawn
	// around it: it stays vector outside them, clipped out of the region,
	// under a CTM the clip has to undo.
	out, report := checkFlattened(t, fixture{content: "q /GS1 gs 1 0 0 rg 30 30 30 30 re f Q " +
		"q 0.5 0 0 0.5 0 0 cm 0 g 0 90 200 4 re f Q q /GS1 gs 0 0 1 rg 40 40 10 10 re f Q"})
	if pr := report.Pages[0]; pr.Removed != 2 {
		t.Errorf("report %+v, want the squares alone removed", pr)
	}
	if !bytes.Contains(out, []byte("W* n")) {
		t.Error("the rule is not clipped out of the region")
	}
}

func TestFlattenLeavesAnOpaquePageAlone(t *testing.T) {
	t.Parallel()
	in := fixture{bare: true, content: "0 0 1 rg 0 0 100 100 re f"}.bytes()
	_, report := flattenBytes(t, in, Options{})
	if len(report.Pages) != 0 {
		t.Errorf("report %+v, want no page changed", report)
	}
}

// TestFlattenStrokedLine pins that a transparent stroked straight line, whose
// path box has no area, is composited rather than removed as painting
// nothing.
func TestFlattenStrokedLine(t *testing.T) {
	t.Parallel()
	_, report := checkFlattened(t, fixture{
		content: "0 0 1 rg 0 0 100 100 re f q /GS1 gs 1 0 0 RG 4 w 10 50 m 90 50 l S Q",
	})
	if len(report.Pages) != 1 || report.Pages[0].Regions != 1 {
		t.Errorf("report %+v, want the line composited in one region", report)
	}
}
