package pdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
)

// convertTestFont builds a "Convert Serif" face covering printable ASCII,
// every glyph half an em wide.
func convertTestFont() []byte {
	glyphs := []fontbuild.Glyph{{Name: "space", Unicode: ' ', Advance: 500, Empty: true}}
	for r := rune('!'); r <= '~'; r++ {
		glyphs = append(glyphs, fontbuild.Glyph{Name: fmt.Sprintf("uni%04X", r), Unicode: r, Advance: 500})
	}
	return fontbuild.TrueType{
		PostScriptName: "ConvertSerif-Regular",
		Family:         "Convert Serif",
		Subfamily:      "Regular",
		NotdefAdvance:  500,
		Glyphs:         glyphs,
	}.Build()
}

// convertFontDir returns a fresh directory holding the test face.
func convertFontDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "convert_serif.ttf"), convertTestFont(), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// convertText converts src with the test face and opts, failing the test on
// error.
func convertText(t *testing.T, dir, src string, opts ...ConvertOption) ([]byte, *ConvertReport) {
	t.Helper()
	var out bytes.Buffer
	rep, err := Convert(context.Background(), strings.NewReader(src), FormatText, &out, append([]ConvertOption{WithConvertFontDirs(dir)}, opts...)...)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	return out.Bytes(), rep
}

func TestConvert_TextRoundTrips(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data, rep := convertText(t, convertFontDir(t), "Hello world\n\nSecond line\fThird page")
	if rep.Pages != 2 || len(rep.Warnings) != 0 {
		t.Errorf("report %+v, want 2 pages and no warning", rep)
	}
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	if n, err := r.PageCount(ctx); err != nil || n != 2 {
		t.Fatalf("PageCount = %d, %v", n, err)
	}
	for page, want := range map[int]string{1: "Hello world\nSecond line", 2: "Third page"} {
		got, err := r.ExtractText(ctx, page)
		if err != nil {
			t.Fatalf("ExtractText(%d): %v", page, err)
		}
		if strings.Join(strings.Fields(got), " ") != strings.Join(strings.Fields(want), " ") {
			t.Errorf("page %d text %q, want %q", page, got, want)
		}
	}
	if tagged, err := r.IsTagged(ctx); err != nil || !tagged {
		t.Errorf("IsTagged = %v, %v", tagged, err)
	}
	p, err := r.Page(ctx, 1)
	if err != nil || math.Abs(p.Width-595.276) > 0.01 || math.Abs(p.Height-841.89) > 0.01 {
		t.Errorf("page 1 %vx%v, %v, want A4", p.Width, p.Height, err)
	}
}

func TestConvert_AppliesPageSetup(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	src := strings.Repeat("ABCD EFGH\n", 30)
	data, rep := convertText(t, convertFontDir(t), src,
		WithPaperSize(612, 792), WithMargins(10, 10, 10, 10), WithFontSize(20), WithFontFamilies("Convert Serif"))
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	p, err := r.Page(ctx, 1)
	if err != nil || p.Width != 612 || p.Height != 792 {
		t.Errorf("page 1 %vx%v, %v, want Letter", p.Width, p.Height, err)
	}
	// 23 points a line at 20 points: 33 lines fit 772 points, so 30 lines
	// take one page.
	if rep.Pages != 1 {
		t.Errorf("%d pages, want 1", rep.Pages)
	}
}

func TestConvert_ReportsMissingGlyphs(t *testing.T) {
	t.Parallel()
	_, rep := convertText(t, convertFontDir(t), "café\fé")
	if len(rep.Warnings) != 1 {
		t.Fatalf("warnings %+v, want one", rep.Warnings)
	}
	if w := rep.Warnings[0]; w.Code != "missing-glyph" || !strings.Contains(w.Message, "U+00E9") || w.Page != 1 {
		t.Errorf("warning %+v", w)
	}
}

func TestConvert_RefusesBadInput(t *testing.T) {
	t.Parallel()
	dir := convertFontDir(t)
	for name, tc := range map[string]struct {
		format Format
		opts   []ConvertOption
		src    string
		want   error
	}{
		"unknown format":     {format: "docx", want: ErrUnsupportedFormat},
		"empty format":       {format: "", want: ErrUnsupportedFormat},
		"bad paper":          {format: FormatText, opts: []ConvertOption{WithPaperSize(-1, 10)}, want: ErrInvalidConvertOptions},
		"no room":            {format: FormatText, opts: []ConvertOption{WithMargins(500, 0, 500, 0)}, want: ErrInvalidConvertOptions},
		"NaN font size":      {format: FormatText, opts: []ConvertOption{WithFontSize(math.NaN())}, want: ErrInvalidConvertOptions},
		"negative limit":     {format: FormatText, opts: []ConvertOption{WithConvertLimits(Limits{MaxConvertPages: -1})}, want: ErrInvalidLimits},
		"input past limit":   {format: FormatText, src: "ABCD", opts: []ConvertOption{WithConvertLimits(Limits{MaxConvertInputBytes: 3})}, want: ErrConvertInputTooLarge},
		"pages past limit":   {format: FormatText, src: "A\fB", opts: []ConvertOption{WithConvertLimits(Limits{MaxConvertPages: 1})}, want: ErrConvertTooManyPages},
		"no font in the dir": {format: FormatText, opts: []ConvertOption{WithConvertFontDirs(t.TempDir())}, want: ErrConvertNoFonts},
	} {
		var out bytes.Buffer
		opts := append([]ConvertOption{WithConvertFontDirs(dir)}, tc.opts...)
		rep, err := Convert(context.Background(), strings.NewReader(tc.src), tc.format, &out, opts...)
		if !errors.Is(err, tc.want) || rep != nil {
			t.Errorf("%s: %v, %+v, want %v", name, err, rep, tc.want)
		}
		if out.Len() != 0 {
			t.Errorf("%s: wrote %d bytes, want none", name, out.Len())
		}
		if errors.Is(err, ErrConvertNoFonts) == IsInvalidInput(err) {
			t.Errorf("%s: IsInvalidInput(%v) = %v", name, err, IsInvalidInput(err))
		}
	}
	// The limits pass at their bound.
	convertText(t, dir, "ABCD", WithConvertLimits(Limits{MaxConvertInputBytes: 4}))
	convertText(t, dir, "A\fB", WithConvertLimits(Limits{MaxConvertPages: 2}))
}

func TestConvert_IsDeterministicAndConcurrent(t *testing.T) {
	t.Parallel()
	dir := convertFontDir(t)
	src := "Line one\n\tTabbed\n" + strings.Repeat("words and more words ", 50)
	want, _ := convertText(t, dir, src)
	var wg sync.WaitGroup
	got := make([][]byte, 8)
	errs := make([]error, 8)
	for i := range got {
		wg.Go(func() {
			var out bytes.Buffer
			_, errs[i] = Convert(context.Background(), strings.NewReader(src), FormatText, &out, WithConvertFontDirs(dir))
			got[i] = out.Bytes()
		})
	}
	wg.Wait()
	for i := range got {
		if errs[i] != nil || !bytes.Equal(got[i], want) {
			t.Errorf("call %d: %v, or different bytes", i, errs[i])
		}
	}
}

func TestConvert_HonoursTheContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out bytes.Buffer
	if _, err := Convert(ctx, strings.NewReader("A"), FormatText, &out, WithConvertFontDirs(convertFontDir(t))); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: %v, want context.Canceled", err)
	}
}

// TestConvertFonts_CachesABoundedNumberOfIndexes pins that one directory list
// is indexed once and its index shared, concurrent first calls included, and
// that the cache keeps at most maxConvertFontIndexes lists, dropping the
// oldest. Not parallel: it reads the process-wide cache's membership.
func TestConvertFonts_CachesABoundedNumberOfIndexes(t *testing.T) {
	ctx := context.Background()
	first := convertFontDir(t)

	var wg sync.WaitGroup
	indexes := make([]any, 8)
	for i := range indexes {
		wg.Go(func() {
			f, err := convertFonts(ctx, nil, []string{first})
			if err != nil {
				t.Errorf("convertFonts: %v", err)
			}
			indexes[i] = f
		})
	}
	wg.Wait()
	for i, f := range indexes {
		if f != indexes[0] || f == nil {
			t.Fatalf("call %d got another index", i)
		}
	}
	again, err := convertFonts(ctx, nil, []string{first + string(filepath.Separator)})
	if err != nil || any(again) != indexes[0] {
		t.Errorf("the same directory, uncleaned, built a new index: %v", err)
	}

	for range maxConvertFontIndexes {
		if _, err := convertFonts(ctx, nil, []string{convertFontDir(t)}); err != nil {
			t.Fatal(err)
		}
	}
	convertFontCache.RLock()
	n := len(convertFontCache.entries)
	_, kept := convertFontCache.entries[convertFontKey([]string{first})]
	convertFontCache.RUnlock()
	if n > maxConvertFontIndexes || kept {
		t.Errorf("cache holds %d indexes, the oldest kept %v; want at most %d, the oldest dropped", n, kept, maxConvertFontIndexes)
	}
	rebuilt, err := convertFonts(ctx, nil, []string{first})
	if err != nil || any(rebuilt) == indexes[0] {
		t.Errorf("the dropped list was not rebuilt: %v", err)
	}

	// A build ctx ends is not kept.
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	dir := convertFontDir(t)
	if _, err := convertFonts(cancelled, nil, []string{dir}); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled build: %v", err)
	}
	if f, err := convertFonts(ctx, nil, []string{dir}); err != nil || f.Len() != 1 {
		t.Errorf("the build after a cancelled one: %v", err)
	}
}

// convertHTMLPage is the page the HTML tests convert: a heading, a
// paragraph with an internal and an external link, a list, an image and a
// forced page break.
const convertHTMLPage = `<!doctype html><html lang="en"><head><title>Test page</title>
<meta name="author" content="Tester"><link rel="stylesheet" href="css/site.css"></head>
<body><h1 id="top">Heading one</h1>
<p>Go to <a href="#second">the second part</a> or <a href="https://example.com/">away</a>.</p>
<ul><li>First item</li><li>Second item</li></ul>
<p><img src="img/dot.png" alt="A dot"></p>
<h2 id="second" class="break">Second part</h2><p>The end.</p></body></html>`

// convertHTMLResources is the resource tree convertHTMLPage reads.
func convertHTMLResources(t *testing.T) fstest.MapFS {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return fstest.MapFS{
		"css/site.css": {Data: []byte(`.break { break-before: page } h1 { font-size: 20pt }`)},
		"img/dot.png":  {Data: b.Bytes()},
	}
}

// convertHTMLTest converts src as HTML with the test face, the test resources
// and opts, failing the test on error.
func convertHTMLTest(t *testing.T, dir, src string, opts ...ConvertOption) ([]byte, *ConvertReport) {
	t.Helper()
	var out bytes.Buffer
	base := []ConvertOption{WithConvertFontDirs(dir), WithResources(convertHTMLResources(t))}
	rep, err := Convert(context.Background(), strings.NewReader(src), FormatHTML, &out, append(base, opts...)...)
	if errors.Is(err, ErrUnsupportedFormat) {
		t.Fatalf("Convert refuses FormatHTML: %v", err)
	}
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	return out.Bytes(), rep
}

func TestConvert_HTMLLaysOutATaggedLinkedDocument(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data, rep := convertHTMLTest(t, convertFontDir(t), convertHTMLPage)
	if rep.Pages != 2 || len(rep.Warnings) != 0 {
		t.Errorf("report %+v, want 2 pages and no warning", rep)
	}
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	if n, err := r.PageCount(ctx); err != nil || n != 2 {
		t.Fatalf("PageCount = %d, %v", n, err)
	}
	for page, want := range map[int][]string{
		1: {"Heading one", "Go to the second part or away.", "First item", "Second item"},
		2: {"Second part", "The end."},
	} {
		got, err := r.ExtractText(ctx, page)
		if err != nil {
			t.Fatalf("ExtractText(%d): %v", page, err)
		}
		flat := strings.Join(strings.Fields(got), " ")
		for _, w := range want {
			if !strings.Contains(flat, w) {
				t.Errorf("page %d text %q lacks %q", page, flat, w)
			}
		}
	}
	if tagged, err := r.IsTagged(ctx); err != nil || !tagged {
		t.Errorf("IsTagged = %v, %v", tagged, err)
	}
	annots, err := r.ExtractAnnotations(ctx, 1)
	if err != nil {
		t.Fatalf("ExtractAnnotations: %v", err)
	}
	var internal, external bool
	for _, a := range annots {
		if a.Subtype != "Link" {
			continue
		}
		internal = internal || a.DestPage == 2
		external = external || a.URI == "https://example.com/"
	}
	if !internal || !external {
		t.Errorf("links %+v, want one to page 2 and one to https://example.com/", annots)
	}
	if imgs, err := r.ExtractImages(ctx, 1); err != nil || len(imgs) != 1 {
		t.Errorf("images on page 1: %d, %v", len(imgs), err)
	}
	marks, err := r.Bookmarks(ctx)
	if err != nil || len(marks) != 1 || marks[0].Title != "Heading one" || len(marks[0].Children) != 1 || marks[0].Children[0].PageNumber != 2 {
		t.Errorf("bookmarks %+v, %v", marks, err)
	}
	meta, err := r.Metadata(ctx)
	if err != nil || meta["Title"] != "Test page" || meta["Author"] != "Tester" {
		t.Errorf("metadata %v, %v", meta, err)
	}
}

func TestConvert_HTMLAppliesPageSetupAndMedia(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := convertFontDir(t)
	src := `<style>@media screen { p { break-before: page } }</style><p>one</p><p>two</p>`
	data, rep := convertHTMLTest(t, dir, src, WithPaperSize(612, 792), WithMargins(0, 0, 0, 0))
	if rep.Pages != 1 {
		t.Errorf("print: %d pages, want 1", rep.Pages)
	}
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	if p, err := r.Page(ctx, 1); err != nil || p.Width != 612 || p.Height != 792 {
		t.Errorf("page 1 %vx%v, %v, want Letter", p.Width, p.Height, err)
	}
	if _, rep := convertHTMLTest(t, dir, src, WithMediaType("screen")); rep.Pages != 2 {
		t.Errorf("screen: %d pages, want 2", rep.Pages)
	}
	// With the CSS page size preferred the @page size is the page;
	// without, the paper is, turned to the @page orientation.
	for _, tc := range []struct {
		prefer bool
		w, h   float64
	}{{true, 595.276, 419.528}, {false, 841.89, 595.276}} {
		data, _ = convertHTMLTest(t, dir, `<style>@page { size: A5 landscape }</style><p>x`, WithPreferCSSPageSize(tc.prefer))
		r2, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		p, err := r2.Page(ctx, 1)
		r2.Close()
		if err != nil || math.Abs(p.Width-tc.w) > 0.01 || math.Abs(p.Height-tc.h) > 0.01 {
			t.Errorf("@page A5 landscape, prefer %v: %vx%v, %v; want %vx%v", tc.prefer, p.Width, p.Height, err, tc.w, tc.h)
		}
	}
}

func TestConvert_HTMLRefusesBadInput(t *testing.T) {
	t.Parallel()
	dir := convertFontDir(t)
	for name, tc := range map[string]struct {
		opts []ConvertOption
		src  string
		want error
	}{
		"bad media":        {opts: []ConvertOption{WithMediaType("tv")}, want: ErrInvalidConvertOptions},
		"bad paper":        {opts: []ConvertOption{WithPaperSize(-1, 10)}, want: ErrInvalidConvertOptions},
		"input past limit": {src: "<p>ABCD", opts: []ConvertOption{WithConvertLimits(Limits{MaxConvertInputBytes: 4})}, want: ErrConvertInputTooLarge},
		"pages past limit": {src: `<p style="break-after: page">A<p>B`, opts: []ConvertOption{WithConvertLimits(Limits{MaxConvertPages: 1})}, want: ErrConvertTooManyPages},
		"too deep":         {src: strings.Repeat("<div>", 600), want: ErrConvertInputTooLarge},
	} {
		var out bytes.Buffer
		opts := append([]ConvertOption{WithConvertFontDirs(dir)}, tc.opts...)
		rep, err := Convert(context.Background(), strings.NewReader(tc.src), FormatHTML, &out, opts...)
		if !errors.Is(err, tc.want) || rep != nil {
			t.Errorf("%s: %v, %+v, want %v", name, err, rep, tc.want)
		}
		if out.Len() != 0 {
			t.Errorf("%s: wrote %d bytes, want none", name, out.Len())
		}
		if !IsInvalidInput(err) {
			t.Errorf("%s: IsInvalidInput(%v) = false", name, err)
		}
	}
}

func TestConvert_HTMLReportsWhatItLeavesOut(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	rep, err := Convert(context.Background(), strings.NewReader(`<p><img src="a.png" alt="alt text"><iframe src="x"></iframe>`), FormatHTML, &out, WithConvertFontDirs(convertFontDir(t)))
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	var got []string
	for _, w := range rep.Warnings {
		got = append(got, w.Code)
	}
	if strings.Join(got, ",") != "resource-refused,unsupported-element" {
		t.Errorf("warnings %+v", rep.Warnings)
	}
}

func TestConvert_HTMLIsDeterministic(t *testing.T) {
	t.Parallel()
	dir := convertFontDir(t)
	want, _ := convertHTMLTest(t, dir, convertHTMLPage)
	for range 3 {
		if got, _ := convertHTMLTest(t, dir, convertHTMLPage); !bytes.Equal(got, want) {
			t.Fatal("two conversions wrote different bytes")
		}
	}
}
