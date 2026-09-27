package pdf

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// pageBoxPDF builds a one-page document whose page dictionary carries the given
// entries, so a test can leave the media box out or make it degenerate.
func pageBoxPDF(pageEntries string) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R " + pageEntries + " >>",
	}

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

// TestPageWithoutMediaBoxReportsLetter pins the size reported for a page that
// declares no media box.
//
// ISO 32000-1, Table 30 requires /MediaBox, so such a file is invalid and the
// spec does not say what to report. Answering 0 by 0 describes a page no viewer
// would draw: poppler reports 612 x 792 for such a page, and pdf.js substitutes
// its LETTER_SIZE_MEDIABOX.
//
// # Corpus evidence
//
//   - govdocs thread_400_400739.pdf: has no /MediaBox anywhere, and poppler
//     reports 612 x 792 for it.
func TestPageWithoutMediaBoxReportsLetter(t *testing.T) {
	data := pageBoxPDF("")
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	p, err := r.Page(context.Background(), 1)
	if err != nil {
		t.Fatalf("Page: %v", err)
	}
	if !almostEqual(p.Width, 612, 0.01) || !almostEqual(p.Height, 792, 0.01) {
		t.Errorf("page measures %v x %v, want 612 x 792", p.Width, p.Height)
	}
	if p.MediaBox != ([4]float64{}) {
		t.Errorf("MediaBox = %v, want the zero rectangle: the fallback must not invent a declared box",
			p.MediaBox)
	}
}

// TestPageWithZeroAreaMediaBoxReportsLetter pins the same for a media box that
// is present but encloses no area.
//
// # Corpus evidence
//
//   - pdfjs boundingBox_invalid.pdf: its first page declares a media box that
//     is present but encloses no area, the shape this test builds.
func TestPageWithZeroAreaMediaBoxReportsLetter(t *testing.T) {
	data := pageBoxPDF("/MediaBox [0 0 0 0]")
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	p, err := r.Page(context.Background(), 1)
	if err != nil {
		t.Fatalf("Page: %v", err)
	}
	if !almostEqual(p.Width, 612, 0.01) || !almostEqual(p.Height, 792, 0.01) {
		t.Errorf("page measures %v x %v, want 612 x 792", p.Width, p.Height)
	}
	if p.MediaBox != ([4]float64{}) {
		t.Errorf("MediaBox = %v, want the declared zero rectangle: the fallback changes only "+
			"the effective dimensions", p.MediaBox)
	}
}

// TestPageWithZeroAreaCropBoxFallsBackToMediaBox pins that a usable media box
// still wins over a degenerate crop box, so the Letter fallback stays reserved
// for pages with no usable box at all.
//
// # Corpus evidence
//
//   - pdfjs boundingBox_invalid.pdf: page 2 declares /MediaBox [0 0 800 600]
//     with /CropBox [0 0 0 0], the shape this test builds, and poppler reports
//     800 x 600 for it.
func TestPageWithZeroAreaCropBoxFallsBackToMediaBox(t *testing.T) {
	data := pageBoxPDF("/MediaBox [0 0 800 600] /CropBox [0 0 0 0]")
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	p, err := r.Page(context.Background(), 1)
	if err != nil {
		t.Fatalf("Page: %v", err)
	}
	if !almostEqual(p.Width, 800, 0.01) || !almostEqual(p.Height, 600, 0.01) {
		t.Errorf("page measures %v x %v, want 800 x 600", p.Width, p.Height)
	}
	if p.MediaBox != ([4]float64{0, 0, 800, 600}) {
		t.Errorf("MediaBox = %v, want the declared [0 0 800 600]", p.MediaBox)
	}
}

// TestUsablePageSizeIsNotOverridden is the control: an ordinary page keeps its
// declared size, including one much smaller than US Letter.
func TestUsablePageSizeIsNotOverridden(t *testing.T) {
	data := pageBoxPDF("/MediaBox [0 0 250 50]")
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	p, err := r.Page(context.Background(), 1)
	if err != nil {
		t.Fatalf("Page: %v", err)
	}
	if !almostEqual(p.Width, 250, 0.01) || !almostEqual(p.Height, 50, 0.01) {
		t.Errorf("page measures %v x %v, want 250 x 50", p.Width, p.Height)
	}
}

// TestPageMeasuresWhatViewersShow pins that [Reader.Page] measures a page as
// pdf.js and poppler show it when a box is no rectangle a viewer uses: four
// numbers (ISO 32000-1, 7.9.5) enclosing an area. They read any other array
// as no box, and draw the page on its default: US Letter for the media box,
// the media box for the crop box. A crop box is reduced to its intersection
// with the page they draw, US Letter where the media box is no box. Each size
// is the page view the pinned pdf.js reports for the page, and the size poppler
// 26.09.0 reports, but where the test names poppler's.
func TestPageMeasuresWhatViewersShow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, tc := range viewerBoxShapes {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := openBytes(t, tc.pdf())
			defer r.Close()
			p, err := r.Page(ctx, 1)
			if err != nil {
				t.Fatalf("Page: %v", err)
			}
			if got := [2]float64{p.Width, p.Height}; got != tc.want {
				t.Errorf("page measures %v, want %v", got, tc.want)
			}
		})
	}
}

// viewerBoxShape is a one-page document whose root node states root and whose
// page states page, and the size pdf.js shows the page at.
type viewerBoxShape struct {
	name       string
	root, page string
	want       [2]float64
}

// pdf builds the document. Object 5 is the number 792 and object 6 the name
// /Foo, for a box to name.
func (s viewerBoxShape) pdf() []byte {
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 " + s.root + " >>",
		"<< /Type /Page /Parent 2 0 R " + s.page + " /Contents 4 0 R >>",
		"<< /Length 0 >>\nstream\n\nendstream",
		"792",
		"/Foo",
	})
}

// viewerBoxShapes are the page boundaries whose reading ISO 19005-2, 6.1.13
// and pdf.js or poppler part, beside the controls, each with the size the
// pinned pdf.js shows the page at, which poppler 26.09.0 shows too but where
// a comment names its own.
//
//nolint:gochecknoglobals // test table shared by the read and the rewrite tests.
var viewerBoxShapes = []viewerBoxShape{
	{name: "a media box of five entries", page: "/MediaBox [0 0 600 800 7]", want: [2]float64{612, 792}},
	{name: "a media box of three entries", page: "/MediaBox [10 10 300]", want: [2]float64{612, 792}},
	{name: "a media box holding a name", page: "/MediaBox [0 /Foo 600 800]", want: [2]float64{612, 792}},
	// poppler draws a page of no width.
	{name: "a media box of no width", page: "/MediaBox [0 0 0 792]", want: [2]float64{612, 792}},
	{name: "a media box naming a number", page: "/MediaBox [0 0 600 5 0 R]", want: [2]float64{600, 792}},
	{name: "a media box naming a name", page: "/MediaBox [0 0 600 6 0 R]", want: [2]float64{612, 792}},
	// poppler takes the node's media box, [0 0 400 500].
	{name: "a media box of five entries under a node's", root: "/MediaBox [0 0 400 500]", page: "/MediaBox [0 0 600 800 7]", want: [2]float64{612, 792}},
	{name: "a crop box of five entries", page: "/MediaBox [0 0 600 800] /CropBox [0 0 300 400 9]", want: [2]float64{600, 800}},
	{name: "a crop box of three entries", page: "/MediaBox [0 0 600 800] /CropBox [10 10 300]", want: [2]float64{600, 800}},
	{name: "a crop box holding a name", page: "/MediaBox [0 0 600 800] /CropBox [0 /Foo 300 400]", want: [2]float64{600, 800}},
	{name: "a crop box on a media box of five entries", page: "/MediaBox [0 0 600 800 7] /CropBox [0 0 300 400]", want: [2]float64{300, 400}},
	{name: "a crop box beyond a media box of five entries", page: "/MediaBox [0 0 600 800 7] /CropBox [0 0 700 900]", want: [2]float64{612, 792}},
	{name: "a crop box on a media box of no area", page: "/MediaBox [0 0 0 0] /CropBox [0 0 300 400]", want: [2]float64{300, 400}},
	// poppler takes the node's crop box, [0 0 300 400].
	{name: "a crop box of no area under a node's", root: "/CropBox [0 0 300 400]", page: "/MediaBox [0 0 600 800] /CropBox [0 0 0 400]", want: [2]float64{600, 800}},
	{name: "boxes of five entries, holding a name or of three entries", page: "/MediaBox [0 0 600 800] /CropBox [0 0 500 700]" +
		" /BleedBox [0 0 300 400 9] /TrimBox [0 /Foo 310 410] /ArtBox [10 10 320]", want: [2]float64{500, 700}},
	{name: "a media box on a node of five entries", root: "/MediaBox [0 0 400 500 /X]", want: [2]float64{612, 792}},
	{name: "control", page: "/MediaBox [0 0 600 800] /CropBox [0 0 300 400] /TrimBox [0 0 200 300]", want: [2]float64{300, 400}},
}

// TestFullRewrite_KeepsWhatViewersShow pins, shape by shape, that a full
// rewrite writes every page boundary as a box a viewer uses or not at all,
// and that the page reads as its source: the size and the effective boxes
// [Reader.Page] reports, which follow what pdf.js and poppler show (see
// [TestPageMeasuresWhatViewersShow]).
func TestFullRewrite_KeepsWhatViewersShow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, tc := range viewerBoxShapes {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := tc.pdf()
			before := readPage(t, src)
			w := NewWriter(openBytes(t, src))
			defer w.Close()
			var out bytes.Buffer
			if err := w.WriteTo(ctx, &out, WithFullRewrite()); err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
			after := readPage(t, out.Bytes())
			if after.Width != before.Width || after.Height != before.Height {
				t.Errorf("the output page measures %v x %v, want the %v x %v of the source",
					after.Width, after.Height, before.Width, before.Height)
			}
			// The effective boxes of a page whose media box no viewer uses
			// default to that box as written, which the output writes as US
			// Letter: only the size is the source's there.
			if document.UsableBox(readOutputTree(t, src).pages[0].MediaBox) {
				for _, box := range []struct {
					name          string
					before, after [4]float64
				}{
					{"CropBox", before.CropBox, after.CropBox},
					{"BleedBox", before.BleedBox, after.BleedBox},
					{"TrimBox", before.TrimBox, after.TrimBox},
					{"ArtBox", before.ArtBox, after.ArtBox},
				} {
					if box.after != box.before {
						t.Errorf("the output %s reads %v, want the %v of the source", box.name, box.after, box.before)
					}
				}
			}
			o := readOutputTree(t, out.Bytes())
			catalog, err := document.ResolveCatalog(ctx, o.doc)
			if err != nil {
				t.Fatalf("ResolveCatalog: %v", err)
			}
			for _, dict := range []ast.PdfDict{o.pages[0].Dict, o.dict(catalog.Dict["Pages"])} {
				for _, key := range []ast.PdfName{"MediaBox", "CropBox", "BleedBox", "TrimBox", "ArtBox"} {
					entry, has := dict[key]
					if !has {
						continue
					}
					box, err := document.ResolveArray(ctx, o.doc, entry)
					if err != nil || !document.UsableBox(document.ResolveRectangle(ctx, o.doc, box)) {
						t.Errorf("the output states /%s %v, which no viewer uses", key, entry)
					}
				}
			}
		})
	}
}

// readPage returns what [Reader.Page] reports for the first page of data.
func readPage(t *testing.T, data []byte) models.Page {
	t.Helper()
	r := openBytes(t, data)
	defer r.Close()
	p, err := r.Page(context.Background(), 1)
	if err != nil {
		t.Fatalf("Page: %v", err)
	}
	return p
}

// TestPageReadsBoxesNoViewerUsesAsAbsent pins that the effective bleed, trim
// and art boxes [Reader.Page] reports take their default, the crop box, for a
// declared box that is no rectangle a viewer uses, as poppler 26.09.0 reports
// them, and that the declared box is nil for one that is not four numbers.
func TestPageReadsBoxesNoViewerUsesAsAbsent(t *testing.T) {
	t.Parallel()
	data := pageBoxPDF("/MediaBox [0 0 600 800] /CropBox [0 0 500 700]" +
		" /BleedBox [0 /Foo 300 400] /TrimBox [0 0 310 410 9] /ArtBox [10 10 320]")
	r := openBytes(t, data)
	defer r.Close()
	p, err := r.Page(context.Background(), 1)
	if err != nil {
		t.Fatalf("Page: %v", err)
	}
	crop := [4]float64{0, 0, 500, 700}
	for _, c := range []struct {
		name     string
		got      [4]float64
		declared *[4]float64
	}{
		{"BleedBox", p.BleedBox, p.DeclaredBleedBox},
		{"TrimBox", p.TrimBox, p.DeclaredTrimBox},
		{"ArtBox", p.ArtBox, p.DeclaredArtBox},
	} {
		if c.got != crop {
			t.Errorf("%s = %v, want the crop box %v", c.name, c.got, crop)
		}
		if c.declared != nil {
			t.Errorf("Declared%s = %v, want nil", c.name, *c.declared)
		}
	}
}
