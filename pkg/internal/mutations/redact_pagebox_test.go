package mutations

import (
	"bytes"
	"context"
	"strconv"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// boxShapeFixture builds a one-page document whose /Pages node carries
// nodeEntries and whose page carries pageEntries, with body as its content.
func boxShapeFixture(t *testing.T, nodeEntries, pageEntries, body string) (*document.Document, *document.Page) {
	t.Helper()
	ctx := context.Background()

	data := []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 " + nodeEntries + " >>\nendobj\n" +
		"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
		"5 0 obj\n<< /Font << /F1 4 0 R >> >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R " + pageEntries + " /Resources 5 0 R /Contents 6 0 R >>\nendobj\n" +
		"6 0 obj\n<< /Length " + strconv.Itoa(len(body)) + " >>\nstream\n" + body + "\nendstream\nendobj\n" +
		"trailer\n<< /Size 7 /Root 1 0 R >>\n%%EOF\n")

	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil || len(pages) == 0 {
		t.Fatalf("ResolvePages: %v (%d pages)", err, len(pages))
	}
	return doc, pages[0]
}

// TestPageBox_TakesTheWidestReading pins the one place a write reads a page
// boundary wider than a viewer does: the redaction region is the union of the
// viewer region and every box the page and its ancestors state, whatever its
// shape.
func TestPageBox_TakesTheWidestReading(t *testing.T) {
	cases := []struct {
		name        string
		nodeEntries string
		pageEntries string
		want        [4]float64
	}{
		{
			name:        "plain media box",
			pageEntries: "/MediaBox [0 0 612 792]",
			want:        [4]float64{0, 0, 612, 792},
		},
		{
			// A viewer reads no box and shows US Letter; a reader taking the
			// first four numbers shows 1000 by 1200.
			name:        "ignored five-entry media box",
			pageEntries: "/MediaBox [0 0 1000 1200 5]",
			want:        [4]float64{0, 0, 1000, 1200},
		},
		{
			// A box of three entries describes no rectangle: every reader
			// substitutes US Letter, which the viewer region already holds.
			name:        "short media box",
			pageEntries: "/MediaBox [0 0 700]",
			want:        [4]float64{0, 0, 612, 792},
		},
		{
			name:        "crop box inside the media box",
			pageEntries: "/MediaBox [0 0 612 792] /CropBox [100 100 200 200]",
			want:        [4]float64{0, 0, 612, 792},
		},
		{
			// ISO 32000-1, 14.11.2.1 reduces it to the media box; a reader
			// skipping the reduction shows it whole.
			name:        "crop box past the media box",
			pageEntries: "/MediaBox [0 0 612 792] /CropBox [-50 -50 700 900]",
			want:        [4]float64{-50, -50, 700, 900},
		},
		{
			name:        "bleed, trim and art boxes past the crop box",
			pageEntries: "/MediaBox [0 0 612 792] /BleedBox [-20 0 612 792] /TrimBox [0 -30 612 792] /ArtBox [0 0 640 820]",
			want:        [4]float64{-20, -30, 640, 820},
		},
		{
			name:        "box enclosing no area",
			pageEntries: "/MediaBox [0 0 612 792] /TrimBox [0 0 0 2000]",
			want:        [4]float64{0, 0, 612, 2000},
		},
		{
			name:        "box holding a name",
			pageEntries: "/MediaBox [0 /Foo 800 900]",
			want:        [4]float64{0, 0, 800, 900},
		},
		{
			name:        "inherited media box",
			nodeEntries: "/MediaBox [0 0 900 1000]",
			want:        [4]float64{0, 0, 900, 1000},
		},
		{
			// pdf.js takes the page's unusable box and shows US Letter,
			// poppler passes over it to the node's.
			name:        "inherited media box below an unusable one",
			nodeEntries: "/MediaBox [0 0 2000 2000]",
			pageEntries: "/MediaBox [0 0 500 500 9]",
			want:        [4]float64{0, 0, 2000, 2000},
		},
		{
			name:        "inherited crop box",
			nodeEntries: "/MediaBox [0 0 612 792] /CropBox [-10 -10 50 50]",
			want:        [4]float64{-10, -10, 612, 792},
		},
		{
			name:        "no box anywhere",
			nodeEntries: "",
			pageEntries: "",
			want:        [4]float64{0, 0, 612, 792},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			doc, page := boxShapeFixture(t, tc.nodeEntries, tc.pageEntries, "")
			got := pageBox(ctx, writer.NewObjAllocator(20), doc, page)
			if got != tc.want {
				t.Errorf("pageBox = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestPageBox_ReadsAStagedCropBox checks that a box an earlier stage staged
// on the page is read, not only the one the source holds.
func TestPageBox_ReadsAStagedCropBox(t *testing.T) {
	ctx := context.Background()
	doc, page := boxShapeFixture(t, "", "/MediaBox [0 0 612 792]", "")
	alloc := writer.NewObjAllocator(20)
	staged := make(ast.PdfDict, len(page.Dict)+1)
	for k, v := range page.Dict {
		staged[k] = v
	}
	staged["CropBox"] = ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(1500), ast.PdfInt(792)}
	alloc.AllocAt(page.Ref.ObjectNum, staged)

	want := [4]float64{0, 0, 1500, 792}
	if got := pageBox(ctx, alloc, doc, page); got != want {
		t.Errorf("pageBox = %v, want %v", got, want)
	}
}

// TestPageBox_StopsOnAParentCycle checks that a /Parent chain naming itself
// ends the walk rather than reading it up to the depth cap.
func TestPageBox_StopsOnAParentCycle(t *testing.T) {
	ctx := context.Background()
	doc, page := boxShapeFixture(t, "/MediaBox [0 0 612 792] /Parent 2 0 R", "", "")
	want := [4]float64{0, 0, 612, 792}
	if got := pageBox(ctx, writer.NewObjAllocator(20), doc, page); got != want {
		t.Errorf("pageBox = %v, want %v", got, want)
	}
}

// TestRedactPageRegions_RemovesTextOnlyALenientReaderShows checks that text
// past the region a viewer shows, inside a box only a lenient reader reads,
// is removed when a region covers it.
func TestRedactPageRegions_RemovesTextOnlyALenientReaderShows(t *testing.T) {
	body := "BT /F1 12 Tf 700 1000 Td (SECRET) Tj ET BT /F1 12 Tf 100 600 Td (KEEP) Tj ET"
	doc, page := boxShapeFixture(t, "", "/MediaBox [0 0 1000 1200 5]", body)
	alloc := writer.NewObjAllocator(20)

	if _, err := RedactPageRegions(context.Background(), alloc, doc, page,
		[][4]float64{{690, 990, 900, 1030}}); err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}
	got := newPageContents(t, alloc, page.Ref.ObjectNum)
	if bytes.Contains(got, []byte("SECRET")) {
		t.Error("text only a lenient reader shows is still in the page content")
	}
	if !bytes.Contains(got, []byte("KEEP")) {
		t.Errorf("text outside the region was removed too: %q", got)
	}
}
