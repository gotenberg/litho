package pdf

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestStampText_KeepsSharedContentsShared pins that stamping every page of a
// document whose pages share one /Contents stream adds no ISO 14289-1, 7.20
// rule (the stamp's own unembedded font adds 7.21.4.1, which this ignores). A
// stamped array for each page made the shared stream read once per array, so
// the marked form it draws gained a second reference and the stamp added ISO
// 14289-1, 7.20 test 2; pages sharing /Contents now share one stamped array.
func TestStampText_KeepsSharedContentsShared(t *testing.T) {
	t.Parallel()
	for _, fixture := range []string{"formshare_ua1", "sharedtag_ua1", "sharedclose_ua1"} {
		t.Run(fixture, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			data, err := os.ReadFile("../../test/integration/corpus/litho/pdfs/" + fixture + ".pdf")
			if err != nil {
				t.Fatalf("reading the fixture: %v", err)
			}
			before := uaRules(t, data)

			r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer r.Close()
			w := NewWriter(r)
			defer w.Close()
			if err := w.StampText(TextStamp{Text: "DRAFT", X: 20, Y: 20, FontSize: 12}); err != nil {
				t.Fatalf("StampText: %v", err)
			}
			var out bytes.Buffer
			if err := w.WriteTo(ctx, &out); err != nil {
				t.Fatalf("WriteTo: %v", err)
			}

			for rule := range uaRules(t, out.Bytes()) {
				if !before[rule] && strings.HasPrefix(rule, "7.20/") {
					t.Errorf("stamping added PDF/UA-1 rule %s", rule)
				}
			}
		})
	}
}

// TestStampText_PerPageCallsKeepSharedContentsShared pins that stamping the
// pages of a document whose pages share one /Contents stream one call at a
// time, as the corpus gate's mutate-stamp cell does, adds no ISO 14289-1,
// 7.20 rule either, and that each page draws its own stamp and no other's. A
// later call used to give the page it stamped a /Contents array of its own,
// so veraPDF read the shared stream once per page and counted the marked form
// it draws again.
func TestStampText_PerPageCallsKeepSharedContentsShared(t *testing.T) {
	t.Parallel()
	for _, fixture := range []string{"formshare_ua1", "sharedtag_ua1", "sharedclose_ua1"} {
		t.Run(fixture, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			data, err := os.ReadFile("../../test/integration/corpus/litho/pdfs/" + fixture + ".pdf")
			if err != nil {
				t.Fatalf("reading the fixture: %v", err)
			}
			before := uaRules(t, data)
			plain := plainTexts(t, data)

			r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer r.Close()
			w := NewWriter(r)
			defer w.Close()
			for i := range plain {
				if err := w.StampText(TextStamp{Text: fmt.Sprintf("MARKQ%dZ", i+1), X: 20, Y: 20, FontSize: 12}, i+1); err != nil {
					t.Fatalf("StampText page %d: %v", i+1, err)
				}
			}
			var out bytes.Buffer
			if err := w.WriteTo(ctx, &out); err != nil {
				t.Fatalf("WriteTo: %v", err)
			}

			for rule := range uaRules(t, out.Bytes()) {
				if !before[rule] && strings.HasPrefix(rule, "7.20/") {
					t.Errorf("stamping page by page added PDF/UA-1 rule %s", rule)
				}
			}
			assertStamps(t, out.Bytes(), plain, func(page int) []string { return []string{fmt.Sprintf("MARKQ%dZ", page)} })
		})
	}
}

// TestStampText_SecondCallKeepsSharedContent pins that a second stamp over
// pages that share one /Contents stream keeps the page's own content and the
// first stamp. The first call gives those pages one stamped array, which only
// the writer holds, and the second read it from the source document, found
// nothing there, and left the page its own stamp alone.
func TestStampText_SecondCallKeepsSharedContent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data, err := os.ReadFile("../../test/integration/corpus/litho/pdfs/sharedtag_ua1.pdf")
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	plain := plainTexts(t, data)
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	w := NewWriter(r)
	defer w.Close()
	for _, text := range []string{"FIRSTQZ", "SECONDQZ"} {
		if err := w.StampText(TextStamp{Text: text, X: 20, Y: 20, FontSize: 12}); err != nil {
			t.Fatalf("StampText %s: %v", text, err)
		}
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	assertStamps(t, out.Bytes(), plain, func(int) []string { return []string{"FIRSTQZ", "SECONDQZ"} })
}

// TestStampImage_PerPageCallsKeepSharedContentsShared pins that an image
// stamped page by page on pages sharing one /Contents stream reaches each page
// once, through the form its own resources name, and adds no ISO 14289-1,
// 7.20 rule.
func TestStampImage_PerPageCallsKeepSharedContentsShared(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data, err := os.ReadFile("../../test/integration/corpus/litho/pdfs/sharedtag_ua1.pdf")
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	before := uaRules(t, data)
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	pageCount, err := r.PageCount(ctx)
	if err != nil {
		t.Fatalf("PageCount: %v", err)
	}
	images := make([]int, pageCount)
	for i := range images {
		metas, err := r.ExtractImages(ctx, i+1)
		if err != nil {
			t.Fatalf("ExtractImages page %d: %v", i+1, err)
		}
		images[i] = len(metas)
	}
	w := NewWriter(r)
	defer w.Close()
	png := buildPNG(t, false)
	for i := range pageCount {
		if err := w.StampImage(ImageStamp{Data: png, MimeType: "image/png", X: 20, Y: 20}, i+1); err != nil {
			t.Fatalf("StampImage page %d: %v", i+1, err)
		}
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	for rule := range uaRules(t, out.Bytes()) {
		if !before[rule] && strings.HasPrefix(rule, "7.20/") {
			t.Errorf("stamping page by page added PDF/UA-1 rule %s", rule)
		}
	}
	stamped, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open stamped: %v", err)
	}
	defer stamped.Close()
	for i := range images {
		metas, err := stamped.ExtractImages(ctx, i+1)
		if err != nil {
			t.Fatalf("ExtractImages stamped page %d: %v", i+1, err)
		}
		if len(metas) != images[i]+1 {
			t.Errorf("stamped page %d holds %d images, want %d", i+1, len(metas), images[i]+1)
		}
	}
}

// TestStampText_DispatchNameAvoidsThePageXObjects pins that the XObject name
// a shared /Contents array draws each page's stamps under is one no page naming
// the array already maps. Both pages here share object 6, which draws their own
// form as /StampShared6, the name the dispatch takes first: mapping it to the
// stamp form would replace the form the shared content draws.
func TestStampText_DispatchNameAvoidsThePageXObjects(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	content := "/StampShared6 Do"
	form := "BT /F1 12 Tf 72 700 Td (ORIGINALFORM) Tj ET"
	data := []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 5 0 R /Contents 6 0 R >>\nendobj\n" +
		"4 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 5 0 R /Contents 6 0 R >>\nendobj\n" +
		"5 0 obj\n<< /XObject << /StampShared6 7 0 R >> >>\nendobj\n" +
		"6 0 obj\n<< /Length " + strconv.Itoa(len(content)) + " >>\nstream\n" + content + "\nendstream\nendobj\n" +
		"7 0 obj\n<< /Type /XObject /Subtype /Form /BBox [0 0 612 792] /Resources << /Font << /F1 8 0 R >> >> /Length " + strconv.Itoa(len(form)) + " >>\nstream\n" + form + "\nendstream\nendobj\n" +
		"8 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
		"trailer\n<< /Size 9 /Root 1 0 R >>\n%%EOF\n")
	plain := plainTexts(t, data)
	for i, text := range plain {
		if !strings.Contains(text, "ORIGINALFORM") {
			t.Fatalf("precondition: page %d reads %q, want the form's text", i+1, text)
		}
	}

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	w := NewWriter(r)
	defer w.Close()
	for i := range plain {
		if err := w.StampText(TextStamp{Text: fmt.Sprintf("MARKQ%dZ", i+1), X: 20, Y: 20, FontSize: 12}, i+1); err != nil {
			t.Fatalf("StampText page %d: %v", i+1, err)
		}
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	assertStamps(t, out.Bytes(), plain, func(page int) []string { return []string{fmt.Sprintf("MARKQ%dZ", page)} })
}

// plainTexts returns the text of every page of data, artifacts left out.
func plainTexts(t *testing.T, data []byte) []string {
	t.Helper()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	pageCount, err := r.PageCount(ctx)
	if err != nil {
		t.Fatalf("PageCount: %v", err)
	}
	texts := make([]string, pageCount)
	for i := range texts {
		if texts[i], err = r.ExtractText(ctx, i+1); err != nil {
			t.Fatalf("ExtractText page %d: %v", i+1, err)
		}
	}
	return texts
}

// assertStamps checks that every page of data reads as plain without
// artifacts, carries each stamp stamps names for it exactly once with them,
// and carries no stamp stamps names for another page only.
func assertStamps(t *testing.T, data []byte, plain []string, stamps func(page int) []string) {
	t.Helper()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open stamped: %v", err)
	}
	defer r.Close()
	for i := range plain {
		page := i + 1
		got, err := r.ExtractText(ctx, page)
		if err != nil {
			t.Fatalf("ExtractText page %d: %v", page, err)
		}
		if got != plain[i] {
			t.Errorf("page %d reads %q without artifacts, want %q", page, got, plain[i])
		}
		all, err := r.ExtractText(ctx, page, WithArtifacts())
		if err != nil {
			t.Fatalf("ExtractText page %d with artifacts: %v", page, err)
		}
		own := stamps(page)
		for _, stamp := range own {
			if n := strings.Count(all, stamp); n != 1 {
				t.Errorf("page %d carries stamp %q %d times, want once: %q", page, stamp, n, all)
			}
		}
		for other := range plain {
			for _, stamp := range stamps(other + 1) {
				if !slices.Contains(own, stamp) && strings.Contains(all, stamp) {
					t.Errorf("page %d carries page %d's stamp %q: %q", page, other+1, stamp, all)
				}
			}
		}
	}
}

// uaRules returns the PDF/UA-1 rules data fails, as clause/test.
func uaRules(t *testing.T, data []byte) map[string]bool {
	t.Helper()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	vs, err := r.ValidatePDFUA(ctx, models.PDFUA1)
	if err != nil {
		t.Fatalf("ValidatePDFUA: %v", err)
	}
	rules := make(map[string]bool)
	for _, v := range toViolations(vs) {
		rules[fmt.Sprintf("%s/%d", v.Clause, v.TestNumber)] = true
	}
	return rules
}
