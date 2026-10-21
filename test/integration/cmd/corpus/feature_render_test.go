package main

import (
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"

	"github.com/gotenberg/litho/pkg/pdf"
)

// whitePage returns an opaque white RGBA image of w by h pixels.
func whitePage(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	return img
}

// fill paints r with c.
func fill(img *image.RGBA, r image.Rectangle, c color.Color) {
	draw.Draw(img, r, image.NewUniform(c), image.Point{}, draw.Src)
}

func TestComparePageIdentical(t *testing.T) {
	t.Parallel()

	a := whitePage(100, 80)
	fill(a, image.Rect(10, 10, 50, 40), color.Black)
	res := comparePage(a, a)
	if !res.Match || res.DiffPixels != 0 || res.MaxDelta != 0 || res.Pixels != 8000 {
		t.Errorf("identical pages: %+v", res)
	}
}

// An edge one rasterizer puts a pixel away from the other lies within the
// neighbourhood, and an anti-aliased edge whose coverage differs by less than
// the channel tolerance is equal outright.
func TestComparePageToleratesAntiAliasing(t *testing.T) {
	t.Parallel()

	want := whitePage(100, 80)
	fill(want, image.Rect(10, 10, 50, 40), color.Black)

	shifted := whitePage(100, 80)
	fill(shifted, image.Rect(11, 10, 51, 40), color.Black)
	if res := comparePage(shifted, want); !res.Match || res.DiffPixels != 0 || res.MaxDelta != 255 {
		t.Errorf("edge shifted by one pixel: %+v", res)
	}

	soft := whitePage(100, 80)
	fill(soft, image.Rect(10, 10, 50, 40), color.Black)
	fill(soft, image.Rect(9, 10, 10, 40), color.Gray{Y: 255 - renderChannelTolerance})
	if res := comparePage(soft, want); !res.Match || res.DiffPixels != 0 || res.MaxDelta != renderChannelTolerance {
		t.Errorf("soft edge within tolerance: %+v", res)
	}
}

// A shape drawn two pixels away, or not at all, differs past the share a page
// may differ by.
func TestComparePageCatchesMissingAndMisplacedShapes(t *testing.T) {
	t.Parallel()

	want := whitePage(100, 80)
	fill(want, image.Rect(10, 10, 50, 40), color.Black)

	missing := whitePage(100, 80)
	res := comparePage(missing, want)
	if res.Match || res.DiffPixels != 40*30 || res.MaxDelta != 255 {
		t.Errorf("missing shape: %+v", res)
	}

	moved := whitePage(100, 80)
	fill(moved, image.Rect(13, 10, 53, 40), color.Black)
	if res := comparePage(moved, want); res.Match || res.DiffPixels == 0 {
		t.Errorf("shape moved by three pixels: %+v", res)
	}

	recoloured := whitePage(100, 80)
	fill(recoloured, image.Rect(10, 10, 50, 40), color.RGBA{R: 255, A: 255})
	if res := comparePage(recoloured, want); res.Match {
		t.Errorf("shape painted red instead of black: %+v", res)
	}
}

// The share of differing pixels in the page decides the page bar: 1% passes,
// more fails. Against a blank golden every inked pixel differs, so the ink bar
// fails both pages; TestComparePageInkShare covers it.
func TestComparePageDiffShare(t *testing.T) {
	t.Parallel()

	want := whitePage(100, 100)
	at := whitePage(100, 100)
	fill(at, image.Rect(0, 0, 10, 10), color.Black) // 100 of 10000 pixels: 1%
	if res := comparePage(at, want); !res.withinPageShare() || res.Match || res.DiffPixels != 100 {
		t.Errorf("1%% differing: %+v", res)
	}
	over := whitePage(100, 100)
	fill(over, image.Rect(0, 0, 10, 11), color.Black)
	if res := comparePage(over, want); res.withinPageShare() || res.Match || res.DiffPixels != 110 {
		t.Errorf("1.1%% differing: %+v", res)
	}
}

// A blank golden against a page drawing a little text passes the page bar and
// fails the ink bar; a one-pixel shift of the same text passes both, and the
// share of the ink decides at 50%.
func TestComparePageInkShare(t *testing.T) {
	t.Parallel()

	text := whitePage(612, 792)
	for x := 50; x < 110; x += 8 { // one short word of 12-point glyphs
		fill(text, image.Rect(x, 700, x+6, 709), color.Black)
	}
	blank := whitePage(612, 792)
	res := comparePage(text, blank)
	if !res.withinPageShare() || res.Match || res.DiffPixels != res.InkPixels || res.InkPixels == 0 {
		t.Errorf("text against a blank golden: %+v", res)
	}
	if res := comparePage(blank, text); res.Match {
		t.Errorf("blank page against text: %+v", res)
	}

	shifted := whitePage(612, 792)
	for x := 51; x < 111; x += 8 {
		fill(shifted, image.Rect(x, 700, x+6, 709), color.Black)
	}
	if res := comparePage(shifted, text); !res.Match || res.DiffPixels != 0 {
		t.Errorf("text shifted by one pixel: %+v", res)
	}

	want := whitePage(200, 200)
	fill(want, image.Rect(0, 0, 20, 20), color.Black) // 400 inked pixels
	half := whitePage(200, 200)
	fill(half, image.Rect(0, 0, 20, 10), color.Black) // 199 differ past the neighbourhood
	if res := comparePage(half, want); !res.Match || res.DiffPixels != 199 || res.InkPixels != 400 {
		t.Errorf("half the ink differing: %+v", res)
	}
	over := whitePage(200, 200)
	fill(over, image.Rect(0, 0, 20, 9), color.Black)
	if res := comparePage(over, want); res.Match || !res.withinPageShare() {
		t.Errorf("more than half the ink differing: %+v", res)
	}
}

func TestComparePageSize(t *testing.T) {
	t.Parallel()

	want := whitePage(100, 80)
	if res := comparePage(whitePage(101, 79), want); !res.Match || res.Pixels != 100*79 {
		t.Errorf("one pixel of slack: %+v", res)
	}
	res := comparePage(whitePage(102, 80), want)
	if res.Match || !regexp.MustCompile(`102x80, golden 100x80`).MatchString(res.Error) {
		t.Errorf("two pixels too wide: %+v", res)
	}
}

// A pixel litho leaves transparent reads as the white pdftoppm draws behind
// the page, and a half-covered black one as mid grey.
func TestComparePageComposesOverWhite(t *testing.T) {
	t.Parallel()

	clear := image.NewRGBA(image.Rect(0, 0, 20, 20))
	if res := comparePage(clear, whitePage(20, 20)); !res.Match || res.MaxDelta != 0 {
		t.Errorf("transparent page against white: %+v", res)
	}
	half := image.NewRGBA(image.Rect(0, 0, 1, 1))
	half.SetRGBA(0, 0, color.RGBA{A: 128}) // premultiplied black at half coverage
	if got := flattenOnWhite(half, 1, 1); got[0] != 127 || got[1] != 127 || got[2] != 127 {
		t.Errorf("half-covered black composes to %v, want 127 grey", got)
	}
}

// writeRenderGolden writes an index and the PNG of every page it names.
func writeRenderGolden(t *testing.T, dir string, idx renderIndex, pages map[int]image.Image) {
	t.Helper()
	for _, p := range idx.Pages {
		img, ok := pages[p.Page]
		if p.File == "" || !ok {
			continue
		}
		f, err := os.Create(filepath.Join(dir, p.File))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}
	data, err := json.Marshal(idx)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, idx.ID+".json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRenderFeatureLoadGoldens(t *testing.T) {
	t.Parallel()

	f := newRenderFeature()
	if err := f.LoadGoldens(t.TempDir()); !errors.Is(err, ErrNoGoldens) {
		t.Errorf("empty dir: %v, want ErrNoGoldens", err)
	}

	dir := t.TempDir()
	writeRenderGolden(t, dir, renderIndex{ID: "a", DPI: renderDPI}, nil)
	if err := f.LoadGoldens(dir); err != nil {
		t.Fatalf("load: %v", err)
	}
	if !f.HasGolden("a") || !f.HasGolden("PDF_A-2b/a") || f.HasGolden("b") {
		t.Error("HasGolden does not key on the PDF's base name")
	}

	stale := t.TempDir()
	writeRenderGolden(t, stale, renderIndex{ID: "a", DPI: 150}, nil)
	if err := newRenderFeature().LoadGoldens(stale); err == nil || errors.Is(err, ErrNoGoldens) {
		t.Errorf("golden at 150 dpi: %v, want a refusal", err)
	}

	deep := t.TempDir()
	writeRenderGolden(t, deep, renderIndex{ID: "a", DPI: renderDPI, PageCount: 9,
		Pages: []renderIndexPage{{Page: renderMaxPages + 1, Error: "x"}}}, nil)
	if err := newRenderFeature().LoadGoldens(deep); err == nil || errors.Is(err, ErrNoGoldens) {
		t.Errorf("golden past renderMaxPages: %v, want a refusal", err)
	}
}

// Until litho renders, every fixture is skipped without opening the PDF.
func TestRenderFeatureSkipsUntilLithoRenders(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeRenderGolden(t, dir, renderIndex{ID: "a", DPI: renderDPI, PageCount: 1,
		Pages: []renderIndexPage{{Page: 1, File: "a.1.png"}}}, map[int]image.Image{1: whitePage(10, 10)})
	f := newRenderFeature()
	f.render = nil
	if err := f.LoadGoldens(dir); err != nil {
		t.Fatal(err)
	}
	out, err := f.Run(context.Background(), filepath.Join(dir, "does-not-exist.pdf"), "a")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	v := f.Compare("a", out)
	if !v.Skipped || v.Match || v.Metrics != nil {
		t.Errorf("verdict before litho renders: %+v", v)
	}
	if lithoRenderPage != nil {
		t.Log("lithoRenderPage is wired: the render cells now score")
	}
}

// The fonts provisioning copies next to the goldens reach every render.
func TestRenderFeatureHandsLithoTheGoldensFonts(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "doc.pdf")
	if err := os.WriteFile(pdfPath, twoImagePDF(t), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, renderFontsDir), 0o700); err != nil {
		t.Fatal(err)
	}
	writeRenderGolden(t, dir, renderIndex{ID: "doc", DPI: renderDPI, PageCount: 1,
		Pages: []renderIndexPage{{Page: 1, File: "doc.1.png"}}}, map[int]image.Image{1: whitePage(10, 10)})
	f := newRenderFeature()
	var got int
	f.render = func(_ context.Context, _ *pdf.Reader, _ int, _ float64, opts ...pdf.RenderOption) (image.Image, error) {
		got = len(opts)
		return whitePage(10, 10), nil
	}
	if err := f.LoadGoldens(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Run(context.Background(), pdfPath, "doc"); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got != 1 {
		t.Errorf("rendered with %d options, want the fonts option", got)
	}
}

func TestRenderFeatureScoresPages(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "doc.pdf")
	if err := os.WriteFile(pdfPath, twoImagePDF(t), 0o600); err != nil {
		t.Fatal(err)
	}
	page1 := whitePage(10, 10)
	fill(page1, image.Rect(2, 2, 8, 8), color.Black)
	writeRenderGolden(t, dir, renderIndex{ID: "doc", DPI: renderDPI, PageCount: 3, Pages: []renderIndexPage{
		{Page: 1, File: "doc.1.png"},
		{Page: 2, Error: "pdftoppm: broken page"},
		{Page: 3, File: "doc.3.png"},
	}}, map[int]image.Image{1: page1, 3: page1})
	writeRenderGolden(t, dir, renderIndex{ID: "blank", DPI: renderDPI, PageCount: 1, Pages: []renderIndexPage{
		{Page: 1, File: "blank.1.png"},
	}}, map[int]image.Image{1: whitePage(10, 10)})
	writeRenderGolden(t, dir, renderIndex{ID: "broken", DPI: renderDPI, Error: "pdfinfo: no catalog"}, nil)

	var asked []int
	f := newRenderFeature()
	f.render = func(_ context.Context, _ *pdf.Reader, page int, dpi float64, opts ...pdf.RenderOption) (image.Image, error) {
		if dpi != renderDPI {
			t.Errorf("rendered at %g dpi", dpi)
		}
		if len(opts) != 0 {
			t.Errorf("rendered with %d options, want none: the goldens hold no fonts", len(opts))
		}
		asked = append(asked, page)
		if page == 1 {
			return page1, nil
		}
		return nil, errors.New("unsupported operator")
	}
	if err := f.LoadGoldens(dir); err != nil {
		t.Fatal(err)
	}

	out, err := f.Run(context.Background(), pdfPath, "PDF_A-2b/doc")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(asked) != 2 || asked[0] != 1 || asked[1] != 3 {
		t.Errorf("rendered pages %v, want [1 3]: page 2 has no golden", asked)
	}
	v := f.Compare("PDF_A-2b/doc", out)
	if v.Skipped || v.Match || v.Score != 0.5 || v.Metrics["pages_match"] != 1 || v.Metrics[metricRenderFailed] != 1 || len(v.Mismatches) != 1 {
		t.Errorf("verdict: %+v", v)
	}

	out, err = f.Run(context.Background(), pdfPath, "blank")
	if err != nil {
		t.Fatalf("run blank: %v", err)
	}
	v = f.Compare("blank", out)
	if v.Skipped || v.Match || v.Score != 0 || v.Metrics["pages_match"] != 0 || v.Metrics[metricRenderFailed] != 0 || len(v.Mismatches) != 1 {
		t.Errorf("a page drawn where the golden is blank: %+v", v)
	}

	out, err = f.Run(context.Background(), pdfPath, "broken")
	if err != nil {
		t.Fatalf("run broken: %v", err)
	}
	if v := f.Compare("broken", out); !v.Skipped {
		t.Errorf("a PDF poppler could not open: %+v", v)
	}
}

// The feature compares at the resolution and page count gen-render.sh draws
// the goldens with.
func TestRenderConstantsMatchGenerator(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join(corpusSourceDir, "lib", "gen-render.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]int{"RENDER_DPI": renderDPI, "RENDER_MAX_PAGES": renderMaxPages} {
		m := regexp.MustCompile(`(?m)^` + name + `=(\d+)$`).FindSubmatch(data)
		if m == nil {
			t.Errorf("gen-render.sh sets no %s", name)
			continue
		}
		if got, _ := strconv.Atoi(string(m[1])); got != want {
			t.Errorf("gen-render.sh sets %s=%d, the feature uses %d", name, got, want)
		}
	}
}

func BenchmarkComparePage(b *testing.B) {
	want := whitePage(612, 792)
	got := whitePage(612, 792)
	for y := 0; y < 792; y += 20 {
		fill(want, image.Rect(36, y, 576, y+10), color.Black)
		fill(got, image.Rect(37, y, 577, y+10), color.Black)
	}
	b.ResetTimer()
	for b.Loop() {
		comparePage(got, want)
	}
}
