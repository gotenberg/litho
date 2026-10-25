package pdf

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"image"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/render/page"
)

// approximatedPDF is a 200 by 100 page drawn with every approximation a
// fixture can reach without a page past the render budget: three glyphs of a
// font the file does not embed, an image whose data does not decode, a Square
// annotation and a FreeText annotation with no appearance stream, and a widget
// whose form asks for new appearances.
func approximatedPDF() []byte {
	const content = "BT /F1 12 Tf 10 80 Td (abc) Tj ET q 20 0 0 20 150 70 cm /Im1 Do Q"
	const widgetAP = "0 0 1 rg 0 0 30 10 re f"
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [7 0 R] /NeedAppearances true >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Contents 4 0 R " +
			"/Resources << /Font << /F1 << /Type /Font /Subtype /Type1 /BaseFont /LithoNoSuchFont >> >> /XObject << /Im1 5 0 R >> >> " +
			"/Annots [6 0 R 7 0 R 9 0 R] >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /XObject /Subtype /Image /Width 2 /Height 2 /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /FlateDecode /Length 4 >>\nstream\nzzzz\nendstream",
		"<< /Type /Annot /Subtype /Square /Rect [10 10 60 40] /C [1 0 0] /F 4 >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (f) /Rect [100 10 130 20] /F 4 /P 3 0 R /AP << /N 8 0 R >> >>",
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 30 10] /Length %d >>\nstream\n%s\nendstream", len(widgetAP), widgetAP),
		"<< /Type /Annot /Subtype /FreeText /Rect [150 10 170 30] /Contents (note) /DA (/Helv 12 Tf 0 g) /F 4 >>",
	})
}

func TestRenderPageReport(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// A page drawn as the file describes it reports nothing, and the image is
	// the one RenderPage draws.
	r := openLimited(t, renderPDF(), Limits{})
	img, rep, err := r.RenderPageReport(ctx, 1, WithRenderDPI(72))
	if err != nil {
		t.Fatal(err)
	}
	if rep != (RenderReport{}) {
		t.Errorf("report of an exact page = %+v, want zero", rep)
	}
	want, err := r.RenderPage(ctx, 1, WithRenderDPI(72))
	if err != nil {
		t.Fatal(err)
	}
	if string(img.(*image.NRGBA).Pix) != string(want.(*image.NRGBA).Pix) {
		t.Error("RenderPageReport draws another image than RenderPage")
	}

	r = openLimited(t, approximatedPDF(), Limits{})
	img, rep, err = r.RenderPageReport(ctx, 1, WithRenderDPI(72))
	if err != nil {
		t.Fatal(err)
	}
	wantRep := RenderReport{
		PlaceholderGlyphs:            3,
		SynthesizedAnnotations:       1,
		AnnotationsWithoutAppearance: 1,
		StaleAppearances:             1,
		Skipped:                      1,
	}
	if rep != wantRep {
		t.Errorf("report = %+v, want %+v", rep, wantRep)
	}
	want, err = r.RenderPage(ctx, 1, WithRenderDPI(72))
	if err != nil {
		t.Fatal(err)
	}
	if string(img.(*image.NRGBA).Pix) != string(want.(*image.NRGBA).Pix) {
		t.Error("RenderPageReport draws another image than RenderPage")
	}

	// Content left out by WithRenderContent is not drawn, so not reported.
	_, rep, err = r.RenderPageReport(ctx, 1, WithRenderDPI(72), WithRenderContent(RenderPaths))
	if err != nil {
		t.Fatal(err)
	}
	if rep.SynthesizedAnnotations != 0 || rep.StaleAppearances != 0 {
		t.Errorf("annotations left out are reported: %+v", rep)
	}
}

func TestRenderPageReportErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := openLimited(t, renderPDF(), Limits{})
	if _, _, err := r.RenderPageReport(ctx, 2); !errors.Is(err, ErrInvalidPageNumber) {
		t.Errorf("page 2: %v, want ErrInvalidPageNumber", err)
	}
	if _, _, err := r.RenderPageReport(ctx, 1, WithRenderTarget(7)); !errors.Is(err, ErrInvalidRenderOptions) {
		t.Errorf("bad target: %v, want ErrInvalidRenderOptions", err)
	}
	small := openLimited(t, renderPDF(), Limits{MaxRenderPixels: 5_000})
	if _, _, err := small.RenderPageReport(ctx, 1, WithRenderDPI(73)); !errors.Is(err, ErrRenderTooLarge) {
		t.Errorf("past MaxRenderPixels: %v, want ErrRenderTooLarge", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := r.RenderPageReport(cancelled, 1); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled context: %v, want context.Canceled", err)
	}
}

// TestRenderReportCoversEveryKey keeps [renderReport] complete. The
// renderer's report counts unsupported constructs under string keys, the
// constants of package page; a key added there without a public field would
// reach only a Debug log, which is the gap [RenderReport] closes. The keys are
// read from the package source with go/ast, so a new one fails here.
func TestRenderReportCoversEveryKey(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, "../internal/render/page", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parsing package page: %v", err)
	}
	var keys []string
	for name, pkg := range pkgs {
		if name != "page" {
			continue
		}
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				gd, ok := decl.(*ast.GenDecl)
				if !ok || gd.Tok != token.CONST {
					continue
				}
				for _, spec := range gd.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for _, v := range vs.Values {
						lit, ok := v.(*ast.BasicLit)
						if !ok || lit.Kind != token.STRING {
							continue
						}
						s, err := strconv.Unquote(lit.Value)
						if err != nil {
							t.Fatalf("unquoting %s: %v", lit.Value, err)
						}
						keys = append(keys, s)
					}
				}
			}
		}
	}
	// Control: the five keys known when this test was written. A scan that
	// matched nothing would pass vacuously.
	if len(keys) < 5 {
		t.Fatalf("found only %d string constants in package page, so the scan is not reading it", len(keys))
	}
	for _, k := range keys {
		got := renderReport(slog.New(slog.DiscardHandler), page.Report{Unsupported: map[string]int{k: 1}})
		if got == (RenderReport{}) {
			t.Errorf("page key %q has no field in RenderReport", k)
		}
	}
	got := renderReport(slog.New(slog.DiscardHandler), page.Report{CoarseMasks: 2, Synthesized: 3})
	if got.CoarseMasks != 2 || got.SynthesizedAnnotations != 3 {
		t.Errorf("counters = %+v, want CoarseMasks 2 and SynthesizedAnnotations 3", got)
	}
}
