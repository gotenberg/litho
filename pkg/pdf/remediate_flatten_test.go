package pdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/remediation"
	"github.com/gotenberg/litho/pkg/models"
)

// TestRemediatePDFA1FlattensTransparency pins that a PDF/A-1 remediation
// removes the transparency ISO 19005-1, 6.4 forbids by flattening it, in both
// modes: the output holds no 6.4 violation, its text extracts as before, and
// the page draws as the source does. Where the half-transparent red square of
// [transparentPDF] overlaps the blue backdrop, the source shows their blend;
// setting the square opaque, the repair that ran before, painted it pure red.
func TestRemediatePDFA1FlattensTransparency(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	in := transparentPDF()
	src := openColorPDF(t, in)
	before := renderRGBA(t, src, 72)
	text, err := src.ExtractText(ctx, 1)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	// User space (50, 60) lies in the red square and on the blue backdrop;
	// the page is 200 points high, so it is device row 140 at 72 dpi.
	const x, y = 50, 140
	sr, sg, sb, _ := before.At(x, y).RGBA()
	if sr>>8 < 64 || sb>>8 < 64 || sg>>8 > 64 {
		t.Fatalf("precondition: the source overlap is not a red-blue blend: %d %d %d", sr>>8, sg>>8, sb>>8)
	}

	for _, aggressive := range []bool{false, true} {
		for _, profile := range []models.PDFAProfile{models.PDFA1b, models.PDFA1a} {
			w := NewWriter(openColorPDF(t, in))
			var opts []RemediateOption
			if aggressive {
				opts = append(opts, WithAggressiveFixes())
			}
			report, err := w.RemediatePDFA(ctx, profile, opts...)
			if err != nil {
				t.Fatalf("%s aggressive=%v: RemediatePDFA: %v", profile, aggressive, err)
			}
			if !slicesContainPrefix(report.Fixed, "flattened transparency on 1 page(s)") {
				t.Errorf("%s aggressive=%v: no flattening reported: %q", profile, aggressive, report.Fixed)
			}
			var buf bytes.Buffer
			if err := w.WriteTo(ctx, &buf); err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
			_ = w.Close()
			out := openColorPDF(t, buf.Bytes())
			violations, err := out.ValidatePDFAReport(ctx, profile)
			if err != nil {
				t.Fatalf("ValidatePDFAReport: %v", err)
			}
			for _, v := range violations {
				if v.Clause == "6.4" {
					t.Errorf("%s aggressive=%v: transparency left: %v", profile, aggressive, v)
				}
			}
			got, err := out.ExtractText(ctx, 1)
			if err != nil {
				t.Fatalf("ExtractText: %v", err)
			}
			if strings.Join(strings.Fields(got), "") != strings.Join(strings.Fields(text), "") {
				t.Errorf("%s aggressive=%v: text changed from %q to %q", profile, aggressive, text, got)
			}
			after := renderRGBA(t, out, 72)
			if share := pixelsApart(before, after, 24); share > 0.01 {
				t.Errorf("%s aggressive=%v: %.2f%% of the pixels draw apart from the source", profile, aggressive, 100*share)
			}
			r, g, b, _ := after.At(x, y).RGBA()
			if d := max(absDiff(r>>8, sr>>8), absDiff(g>>8, sg>>8), absDiff(b>>8, sb>>8)); d > 8 {
				t.Errorf("%s aggressive=%v: the overlap draws %d %d %d, the source %d %d %d",
					profile, aggressive, r>>8, g>>8, b>>8, sr>>8, sg>>8, sb>>8)
			}
		}
	}
}

// remediateA1b remediates data at PDF/A-1b with opts and returns the report
// and the output, failing on a 6.4 violation left in it.
func remediateA1b(t *testing.T, data []byte, opts ...RemediateOption) (*RemediationReport, *Reader) {
	t.Helper()
	ctx := context.Background()
	w := NewWriter(openColorPDF(t, data))
	t.Cleanup(func() { _ = w.Close() })
	report, err := w.RemediatePDFA(ctx, models.PDFA1b, opts...)
	if err != nil {
		t.Fatalf("RemediatePDFA: %v", err)
	}
	var buf bytes.Buffer
	if err := w.WriteTo(ctx, &buf); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	out := openColorPDF(t, buf.Bytes())
	violations, err := out.ValidatePDFAReport(ctx, models.PDFA1b)
	if err != nil {
		t.Fatalf("ValidatePDFAReport: %v", err)
	}
	for _, v := range violations {
		if v.Clause == "6.4" {
			t.Errorf("transparency left: %v", v)
		}
	}
	return report, out
}

// TestRemediatePDFA1KeepsFlatteningOfInvisibleText pins that the integrity
// probe takes text shown at constant alpha 0, which flattening draws in
// rendering mode 3 and the default extraction then leaves out, as kept: here
// most of the page's text. It used to refuse the output, and the remediation
// fell back to setting the transparency opaque, which made the text visible.
func TestRemediatePDFA1KeepsFlatteningOfInvisibleText(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	in := colorFixturePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R " +
			"/Resources << /Font << /F1 5 0 R >> /ExtGState << /Zero 6 0 R >> >> >>",
		colorStream("", "q /Zero gs BT /F1 12 Tf 10 100 Td (Searchable text nobody sees) Tj ET Q "+
			"BT /F1 12 Tf 10 50 Td (Shown) Tj ET"),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
		"<< /Type /ExtGState /ca 0 >>",
	})
	report, out := remediateA1b(t, in)
	if !slicesContainPrefix(report.Fixed, "flattened transparency on 1 page(s)") || len(report.RolledBack) != 0 {
		t.Errorf("flattening not kept: Fixed = %q, RolledBack = %q", report.Fixed, report.RolledBack)
	}
	text, err := out.ExtractText(ctx, 1, WithInvisibleText())
	if err != nil || !strings.Contains(text, "Searchable text nobody sees") {
		t.Errorf("text lost: %q, %v", text, err)
	}
	if shown, err := out.ExtractText(ctx, 1); err != nil || strings.Contains(shown, "nobody") {
		t.Errorf("the hidden text shows: %q, %v", shown, err)
	}
}

// gradientImage returns an image XObject of size x size RGB samples, smooth
// gradients under a fine grain seeded by seed: the many colours and the
// noise of a photograph, which Flate compresses poorly.
func gradientImage(size, seed int) string {
	data := make([]byte, 0, 3*size*size)
	for y := range size {
		for x := range size {
			grain := ((x*7919 + y*104729 + seed*31) ^ (x * y)) % 16
			data = append(data, byte(x+grain), byte(y+grain), byte((x+y)/2+seed/4+grain))
		}
	}
	return colorStream(fmt.Sprintf("/Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8", size, size), string(data))
}

// photoPDF draws three photographs and a half-transparent shading over them:
// a region of images and shadings only, which flattening composites into one
// image, as JPEG under a quality.
func photoPDF() []byte {
	return colorFixturePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 300] /Contents 4 0 R " +
			"/Resources << /XObject << /A 5 0 R /B 6 0 R /C 7 0 R >> /ExtGState << /Half 8 0 R >> " +
			"/Shading << /Sh << /ShadingType 2 /ColorSpace /DeviceRGB /Coords [0 0 300 0] " +
			"/Function << /FunctionType 2 /Domain [0 1] /C0 [1 1 0] /C1 [0 1 1] /N 1 >> >> >> >> >>",
		colorStream("", "q 120 0 0 120 20 20 cm /A Do Q q 120 0 0 120 100 60 cm /B Do Q "+
			"q 120 0 0 120 160 140 cm /C Do Q q /Half gs 40 40 220 200 re W n /Sh sh Q"),
		gradientImage(200, 0),
		gradientImage(200, 70),
		gradientImage(200, 140),
		"<< /Type /ExtGState /ca 0.5 >>",
	})
}

// TestRemediatePDFA1CountsACompositeAsTheImagesItReplaced pins that the
// integrity probe counts a composite as the images it replaced: under a
// tolerance that accepts no image lost, the three photographs flattening
// composites into one image keep their flattening, encoded as JPEG.
func TestRemediatePDFA1CountsACompositeAsTheImagesItReplaced(t *testing.T) {
	t.Parallel()
	in := photoPDF()
	src := openColorPDF(t, in)
	report, out := remediateA1b(t, in, WithRegressionTolerance(RegressionTolerance{MaxTextDropRatio: 0.1}))
	if !slicesContainPrefix(report.Fixed, "flattened transparency on 1 page(s)") || len(report.RolledBack) != 0 {
		t.Fatalf("flattening not kept: Fixed = %q, RolledBack = %q", report.Fixed, report.RolledBack)
	}
	if !slices.ContainsFunc(report.Fixed, func(l string) bool { return strings.Contains(l, "1 photographic image(s) as JPEG") }) {
		t.Errorf("the composite is not JPEG: %q", report.Fixed)
	}
	if share := pixelsApart(renderRGBA(t, src, 72), renderRGBA(t, out, 72), 24); share > 0.01 {
		t.Errorf("%.2f%% of the pixels draw apart from the source", 100*share)
	}
}

// TestFlattenTransparencyJPEGQuality pins [WithFlattenJPEGQuality]: lossless
// by default, a photographic composite as JPEG under a quality, drawing as
// the source does, a vector one lossless, and a quality outside 1 to 100
// refused.
func TestFlattenTransparencyJPEGQuality(t *testing.T) {
	t.Parallel()
	in := photoPDF()
	src := openColorPDF(t, in)
	for _, tc := range []struct {
		opts []FlattenOption
		data []byte
		jpeg int
	}{
		{nil, in, 0},
		{[]FlattenOption{WithFlattenJPEGQuality(90)}, in, 1},
		{[]FlattenOption{WithFlattenJPEGQuality(75)}, in, 1},
		{[]FlattenOption{WithFlattenJPEGQuality(90)}, transparentPDF(), 0},
	} {
		report, out := flattenPDF(t, tc.data, tc.opts...)
		jpeg := 0
		for _, p := range report.Pages {
			jpeg += p.JPEGImages
		}
		if jpeg != tc.jpeg {
			t.Errorf("%d composites as JPEG, want %d", jpeg, tc.jpeg)
		}
		if tc.jpeg > 0 {
			if share := pixelsApart(renderRGBA(t, src, 72), renderRGBA(t, openColorPDF(t, out), 72), 24); share > 0.01 {
				t.Errorf("%.2f%% of the pixels draw apart from the source", 100*share)
			}
		}
	}
	w := NewWriter(openColorPDF(t, in))
	t.Cleanup(func() { _ = w.Close() })
	if _, err := w.FlattenTransparency(context.Background(), WithFlattenJPEGQuality(101)); !errors.Is(err, ErrInvalidFlattenOptions) {
		t.Errorf("quality 101: %v, want ErrInvalidFlattenOptions", err)
	}
}

// TestRemediateFlatteningFallsBackOnALoss pins that an output the integrity
// probe refuses with its transparency flattened, for a loss flattening does
// not account for, is remediated again with the flattening disabled. The
// engine here drops page 1's content after a run that flattened it.
func TestRemediateFlatteningFallsBackOnALoss(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	w := NewWriter(openColorPDF(t, transparentPDF()))
	t.Cleanup(func() { _ = w.Close() })
	revalidate := func(ctx context.Context, r *Reader) ([]error, error) { return r.ValidatePDFA(ctx, models.PDFA1b) }
	report, err := w.remediateFlattening(ctx, remediationTarget{method: "RemediatePDFA"}, DefaultRegressionTolerance, revalidate,
		func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, disabled []string) (*remediation.Report, error) {
			rep, err := remediation.RemediatePDFA(ctx, doc, alloc, models.PDFA1b, remediation.Options{PreserveContent: true}.WithDisabled(disabled))
			if err != nil || len(rep.Flattened) == 0 {
				return rep, err
			}
			page, ok := alloc.Pending()[3].(ast.PdfDict)
			if !ok {
				t.Fatalf("page 1 is not queued: %v", alloc.Pending()[3])
			}
			page = maps.Clone(page)
			page["Contents"] = alloc.Alloc(ast.NewStream(ast.PdfDict{"Length": ast.PdfInt(0)}, nil))
			alloc.AllocAt(3, page)
			// The loss sits outside the content tier, as the flattening
			// does, so no rollback reaches it.
			rep.ContentSnapshot = nil
			return rep, nil
		})
	if err != nil {
		t.Fatalf("remediateFlattening: %v", err)
	}
	if !slicesContainPrefix(report.RolledBack, "flattened transparency on 1 page(s)") || slicesContainPrefix(report.Fixed, "flattened transparency") {
		t.Errorf("flattening not abandoned: Fixed = %q, RolledBack = %q", report.Fixed, report.RolledBack)
	}
	var buf bytes.Buffer
	if err := w.WriteTo(ctx, &buf); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if text, err := openColorPDF(t, buf.Bytes()).ExtractText(ctx, 1); err != nil || !strings.Contains(text, "Above everything") {
		t.Errorf("text lost: %q, %v", text, err)
	}
}

// slicesContainPrefix reports whether some line of lines starts with prefix.
func slicesContainPrefix(lines []string, prefix string) bool {
	return slices.ContainsFunc(lines, func(l string) bool { return strings.HasPrefix(l, prefix) })
}

// absDiff returns |a - b|.
func absDiff(a, b uint32) uint32 {
	if a > b {
		return a - b
	}
	return b - a
}
