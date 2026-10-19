package pdf_test

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
	"github.com/gotenberg/litho/pkg/pdf"
)

// TestRemediatePDFUAClosesANeverClosedSequence pins that remediation closes a
// marked-content sequence the page's content never closes. unclosedmc_ua1
// holds a closed /P sequence, then /Artifact BMC and a stroked rule with no
// EMC, which ISO 32000-1, 14.6 makes no sequence, so ISO 14289-1, 7.1 t3 and
// ISO 14289-2, 8.2.2 t1 judge the rule unmarked. The content-tag fixer leaves
// a page that holds marked content, and both findings stayed on the output,
// in the default and the aggressive mode, as veraPDF 1.30.2 reported them.
func TestRemediatePDFUAClosesANeverClosedSequence(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data, err := os.ReadFile("../../test/integration/corpus/litho/pdfs/unclosedmc_ua1.pdf")
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}

	for _, tc := range []struct {
		profile models.PDFUAProfile
		clause  string
		test    int
	}{
		{models.PDFUA1, "7.1", 3},
		{models.PDFUA2, "8.2.2", 1},
	} {
		for mode, opts := range map[string][]pdf.RemediateOption{
			"default":    nil,
			"aggressive": {pdf.WithAggressiveFixes()},
		} {
			t.Run(string(tc.profile)+" "+mode, func(t *testing.T) {
				t.Parallel()
				r, err := pdf.Open(ctx, bytes.NewReader(data), int64(len(data)))
				if err != nil {
					t.Fatalf("Open: %v", err)
				}
				defer r.Close()
				w := pdf.NewWriter(r)
				defer w.Close()
				if _, err := w.RemediatePDFUA(ctx, tc.profile, opts...); err != nil {
					t.Fatalf("RemediatePDFUA: %v", err)
				}
				var out bytes.Buffer
				if err := w.WriteTo(ctx, &out); err != nil {
					t.Fatalf("WriteTo: %v", err)
				}

				remediated, err := pdf.Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
				if err != nil {
					t.Fatalf("Open remediated: %v", err)
				}
				defer remediated.Close()
				vs, err := remediated.ValidatePDFUAReport(ctx, tc.profile)
				if err != nil {
					t.Fatalf("ValidatePDFUAReport: %v", err)
				}
				for _, v := range vs {
					if v.Clause == tc.clause && v.TestNumber == tc.test {
						t.Errorf("remediated output still reports %s test %d: %s", tc.clause, tc.test, v.Error())
					}
				}
			})
		}
	}
}
