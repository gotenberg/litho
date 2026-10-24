package pdf

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// whiteTextPDF is a letter page holding four runs, each a word no other
// contains:
//
//   - HEADING, white on a navy bar: the old judgement dropped it as white on
//     white, and a rendering shows it;
//   - GHOSTED, white on the white page: invisible, dropped;
//   - SCANNED, white in rendering mode 3 on a second navy bar: mode 3 paints
//     nothing (ISO 32000-1, 9.3.6, Table 106), dropped without a rendering;
//   - BODYTEXT, black: the control that plain text still reads.
//
// Helvetica is not embedded, so the renderer draws each glyph as a hollow box:
// the box shows on the bar and not on the page, which is the visibility the
// extraction asks about.
func whiteTextPDF() []byte {
	const content = "0 0 0.5 rg 50 690 400 30 re f\n" +
		"BT 1 1 1 rg /F1 14 Tf 60 700 Td (HEADING) Tj ET\n" +
		"BT 1 g /F1 12 Tf 60 600 Td (GHOSTED) Tj ET\n" +
		"0 0 0.5 rg 50 490 400 30 re f\n" +
		"BT 3 Tr 1 1 1 rg /F1 14 Tf 60 500 Td (SCANNED) Tj ET\n" +
		"BT 0 Tr 0 g /F1 12 Tf 60 400 Td (BODYTEXT) Tj ET\n"
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	})
}

func TestExtractText_WhiteTextJudgedByRendering(t *testing.T) {
	t.Parallel()
	r := openLimited(t, whiteTextPDF(), Limits{})
	ctx := context.Background()

	got, err := r.ExtractText(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"HEADING", "BODYTEXT"} {
		if !strings.Contains(got, want) {
			t.Errorf("ExtractText = %q, want %s", got, want)
		}
	}
	for _, gone := range []string{"GHOSTED", "SCANNED"} {
		if strings.Contains(got, gone) {
			t.Errorf("ExtractText = %q, want no %s", got, gone)
		}
	}

	// WithInvisibleText still reads every run.
	all, err := r.ExtractText(ctx, 1, WithInvisibleText())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"HEADING", "GHOSTED", "SCANNED", "BODYTEXT"} {
		if !strings.Contains(all, want) {
			t.Errorf("ExtractText(WithInvisibleText) = %q, want %s", all, want)
		}
	}

	// The verdict is cached on the page.
	if _, ok := r.doc.CachedTextVisibility(3); !ok {
		t.Error("no text visibility verdict cached for the page")
	}
}

func TestExtractTextElements_WhiteTextJudgedByRendering(t *testing.T) {
	t.Parallel()
	r := openLimited(t, whiteTextPDF(), Limits{})

	els, err := r.ExtractTextElements(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	invisible := map[string]bool{}
	for _, el := range els {
		invisible[strings.TrimSpace(el.Text)] = el.Invisible
	}
	for word, want := range map[string]bool{"HEADING": false, "GHOSTED": true, "SCANNED": true, "BODYTEXT": false} {
		got, ok := invisible[word]
		if !ok {
			t.Errorf("no element %s in %v", word, invisible)
			continue
		}
		if got != want {
			t.Errorf("%s Invisible = %v, want %v", word, got, want)
		}
	}
}

// TestExtractText_WhiteTextUnrenderableKeepsJudgement lowers the render budget
// below the page, so the page cannot be drawn: white text keeps the judgement
// of its colour alone and HEADING stays out, as it did before rendering judged
// it.
func TestExtractText_WhiteTextUnrenderableKeepsJudgement(t *testing.T) {
	t.Parallel()
	r := openLimited(t, whiteTextPDF(), Limits{MaxRenderPixels: 100})

	got, err := r.ExtractText(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "HEADING") {
		t.Errorf("ExtractText = %q, want no HEADING on an unrenderable page", got)
	}
	if !strings.Contains(got, "BODYTEXT") {
		t.Errorf("ExtractText = %q, want BODYTEXT", got)
	}
}

// TestExtractText_WhiteTextConcurrent reads the page from several goroutines,
// which share the document's verdict cache.
func TestExtractText_WhiteTextConcurrent(t *testing.T) {
	t.Parallel()
	r := openLimited(t, whiteTextPDF(), Limits{})
	ctx := context.Background()

	var wg sync.WaitGroup
	errs := make([]error, 8)
	texts := make([]string, 8)
	for i := range 8 {
		wg.Go(func() {
			if i%2 == 0 {
				texts[i], errs[i] = r.ExtractText(ctx, 1)
				return
			}
			els, err := r.ExtractTextElements(ctx, 1)
			errs[i] = err
			for _, el := range els {
				if !el.Invisible {
					texts[i] += el.Text
				}
			}
		})
	}
	wg.Wait()
	for i := range 8 {
		if errs[i] != nil {
			t.Fatalf("goroutine %d: %v", i, errs[i])
		}
		if !strings.Contains(texts[i], "HEADING") {
			t.Errorf("goroutine %d read %q, want HEADING", i, texts[i])
		}
	}
}
