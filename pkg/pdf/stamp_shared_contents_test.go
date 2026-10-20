package pdf

import (
	"bytes"
	"context"
	"fmt"
	"os"
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
