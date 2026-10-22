package flatten

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/limits"
)

// pagePDF is a one-page PDF of size × size points: object 3 the page with
// the resources given, object 4 its content, and objs objects 5 onwards.
func pagePDF(size int, resources, data string, objs ...string) []byte {
	return buildPDF(append([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		fmt.Sprintf("<< /Type /Page /Parent 2 0 R /Contents 4 0 R /MediaBox [0 0 %d %d] /Resources << %s >> >>", size, size, resources),
		stream("", data),
	}, objs...))
}

// flattenWith runs [Document] over data's first page under lim.
func flattenWith(t *testing.T, data []byte, lim limits.Limits) (*Report, error) {
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
	return Document(ctx, writer.NewObjAllocator(doc.HighestObjectNumber(ctx)), doc, pages[:1], Options{})
}

// TestFlattenAcceptsCoarseMasks checks that a page whose soft mask's group
// passes the render pixel budget at every resolution flattening tries is
// composited with the mask drawn coarse at the last one, and reported,
// rather than refused. The page is smaskbudget_2b.pdf's first: a 4000 pt
// page, red through a luminosity mask white in a square and grey 0.5 in a
// band, over blue, whose mask group needs 80 million pixels at 72 dpi.
func TestFlattenAcceptsCoarseMasks(t *testing.T) {
	t.Parallel()
	data := pagePDF(4000, "/ExtGState << /M << /SMask << /S /Luminosity /G 5 0 R >> >> >>",
		"0 0 1 rg 0 0 4000 4000 re f /M gs 1 0 0 rg 0 0 4000 4000 re f",
		stream("/Type /XObject /Subtype /Form /BBox [0 0 4000 4000] /Group << /S /Transparency /CS /DeviceGray >>",
			"1 g 1000 1500 2000 2000 re f 0.5 g 1000 500 2000 800 re f"))
	out, report := flattenBytes(t, data, Options{})
	if len(report.Pages) != 1 || report.Pages[0].CoarseMasks == 0 || report.Pages[0].Regions == 0 {
		t.Fatalf("report %+v, want one page composited with coarse masks", report.Pages)
	}
	// One pixel per 100 points, at the centre of each area.
	before, after := renderAt(t, data, 0.72), renderAt(t, out, 0.72)
	if share := differing(before, after, 64); share > 0.01 {
		t.Errorf("%.2f%% of pixels differ from the source, want at most 1%%", share*100)
	}
	for _, p := range []struct {
		x, y    int
		r, g, b uint8
	}{{20, 20, 255, 0, 0}, {20, 31, 128, 0, 128}, {5, 5, 0, 0, 255}} {
		c := after.NRGBAAt(p.x, p.y)
		if !near8(c.R, p.r) || !near8(c.G, p.g) || !near8(c.B, p.b) {
			t.Errorf("flattened (%d,%d) = %v, want %d %d %d", p.x, p.y, c, p.r, p.g, p.b)
		}
	}
}

// TestFlattenRefusesHiddenMask checks that a page whose soft mask does not
// fit the render pixel budget even coarse, which the renderer then draws as
// hiding what it masks, is still refused: six nested knockout groups (ISO
// 32000-1, 11.4.6) hold the budget the page-wide mask group would need.
func TestFlattenRefusesHiddenMask(t *testing.T) {
	t.Parallel()
	sizes := []int{560, 320, 180, 160, 126, 80}
	objs := []string{stream("/Type /XObject /Subtype /Form /BBox [0 0 1000 1000] /Group << /S /Transparency /CS /DeviceGray >>",
		"1 g 0 0 1000 1000 re f")}
	for i, size := range sizes {
		body, res := "/G Do", fmt.Sprintf("/Resources << /XObject << /G %d 0 R >> >>", 7+i)
		if i == len(sizes)-1 {
			body = "/M gs 1 0 0 rg 0 0 100 100 re f"
			res = "/Resources << /ExtGState << /M << /SMask << /S /Luminosity /G 5 0 R >> >> >> >>"
		}
		objs = append(objs, stream(fmt.Sprintf("/Type /XObject /Subtype /Form /BBox [0 0 %d %d] /Group << /S /Transparency /I true /K true >> %s", size, size, res), body))
	}
	data := pagePDF(1000, "/XObject << /G 6 0 R >>", "0 0 1 rg 0 0 1000 1000 re f /G Do", objs...)
	_, err := flattenWith(t, data, limits.Limits{MaxRenderPixels: 1_000_000})
	if !errors.Is(err, ErrTooComplex) {
		t.Fatalf("err = %v, want ErrTooComplex", err)
	}
}

// near8 reports whether two 8-bit levels differ by at most 4.
func near8(a, b uint8) bool {
	return max(a, b)-min(a, b) <= 4
}
