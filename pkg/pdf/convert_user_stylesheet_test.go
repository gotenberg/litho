package pdf

import (
	"bytes"
	"context"
	"math"
	"strings"
	"testing"
)

// userSheetPage opens data and returns the size and the text of its page
// n.
func userSheetPage(t *testing.T, data []byte, n int) (w, h float64, text string) {
	t.Helper()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	p, err := r.Page(ctx, n)
	if err != nil {
		t.Fatalf("Page(%d): %v", n, err)
	}
	got, err := r.ExtractText(ctx, n)
	if err != nil {
		t.Fatalf("ExtractText(%d): %v", n, err)
	}
	return p.Width, p.Height, strings.Join(strings.Fields(got), " ")
}

func TestConvert_UserStylesheetCascadesUnderTheDocument(t *testing.T) {
	t.Parallel()
	dir := convertFontDir(t)
	src := `<style>.nav { display: block } .keep { display: block }</style>` +
		`<p class="nav">Navigation</p><p class="keep">Kept</p><p class="ad">Advert</p><p>Body</p>`
	data, rep := convertHTMLTest(t, dir, src,
		WithUserStylesheet(`.nav { display: none !important } .keep { display: none }`),
		WithUserStylesheet(`.ad { display: none } @page { size: A5 landscape; @bottom-center { content: "Page " counter(page) } }`),
	)
	if rep.Pages != 1 || len(rep.Warnings) != 0 {
		t.Errorf("report %+v, want 1 page and no warning", rep)
	}
	w, h, text := userSheetPage(t, data, 1)
	for _, want := range []string{"Kept", "Body"} {
		if !strings.Contains(text, want) {
			t.Errorf("text %q lacks %q", text, want)
		}
	}
	for _, gone := range []string{"Navigation", "Advert"} {
		if strings.Contains(text, gone) {
			t.Errorf("text %q holds %q, which a user rule hides", text, gone)
		}
	}
	if math.Abs(w-595.276) > 0.01 || math.Abs(h-419.528) > 0.01 {
		t.Errorf("page %vx%v, want the user's A5 landscape", w, h)
	}
	// The same rules written in the document, in the order that gives the
	// same result, write the same file: the margin box included.
	twin := `<style>@page { size: A5 landscape; @bottom-center { content: "Page " counter(page) } } .keep { display: block }</style>` +
		`<p class="nav" style="display: none">Navigation</p><p class="keep">Kept</p><p class="ad" style="display: none">Advert</p><p>Body</p>`
	if want, _ := convertHTMLTest(t, dir, twin); !bytes.Equal(data, want) {
		t.Error("the output differs from the document carrying the same rules")
	}
}

func TestConvert_UserStylesheetPageSizeYieldsToThePaper(t *testing.T) {
	t.Parallel()
	dir := convertFontDir(t)
	user := WithUserStylesheet(`@page { size: A5 landscape }`)
	for _, tc := range []struct {
		name string
		opts []HTMLOption
		w, h float64
	}{
		{"default paper", []HTMLOption{user, WithDefaultPaper(PaperLetter)}, 595.276, 419.528},
		{"paper", []HTMLOption{user, WithPaper(PaperA4)}, 841.89, 595.276},
	} {
		data, _ := convertHTMLTest(t, dir, `<p>x`, tc.opts...)
		if w, h, _ := userSheetPage(t, data, 1); math.Abs(w-tc.w) > 0.01 || math.Abs(h-tc.h) > 0.01 {
			t.Errorf("%s: %vx%v, want %vx%v", tc.name, w, h, tc.w, tc.h)
		}
	}
}

func TestConvert_UserStylesheetReportsWhatItLeavesOut(t *testing.T) {
	t.Parallel()
	_, rep := convertHTMLTest(t, convertFontDir(t), `<p>x`, WithUserStylesheet(`@import "missing.css"; p { color: red }`))
	var got []string
	for _, w := range rep.Warnings {
		got = append(got, w.Code)
	}
	if strings.Join(got, ",") != "css-import-failed" {
		t.Errorf("warnings %+v, want the import's failure, as a <style> element's", rep.Warnings)
	}
}

func TestConvert_UserStylesheetStylesMarkdown(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	src := "# Title\n\nSecret\n\nBody\n"
	rep, err := FromMarkdown(strings.NewReader(src), WithConvertFontDirs(convertFontDir(t)),
		WithUserStylesheet(`p:first-of-type { display: none } @page { size: A5 }`)).WriteTo(context.Background(), &out)
	if err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if rep.Pages != 1 {
		t.Errorf("%d pages, want 1", rep.Pages)
	}
	w, h, text := userSheetPage(t, out.Bytes(), 1)
	if strings.Contains(text, "Secret") || !strings.Contains(text, "Body") {
		t.Errorf("text %q, want the first paragraph hidden", text)
	}
	if math.Abs(w-419.528) > 0.01 || math.Abs(h-595.276) > 0.01 {
		t.Errorf("page %vx%v, want A5", w, h)
	}
}
