package pdf

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"slices"
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
