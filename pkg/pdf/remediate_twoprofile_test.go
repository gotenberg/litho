package pdf_test

import (
	"bytes"
	"context"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
	"github.com/gotenberg/litho/pkg/pdf"
)

// TestRemediatePDFAUnifiesOnThePDFAIntentProfile pins that remediation gives
// every output intent the GTS_PDFA1 intent's destination profile, as ISO
// 19005-1, 6.2.2 and ISO 19005-2, 6.2.3 t2 ask of intents naming different
// ones. twoprofile_1b lists a GTS_PDFX CMYK intent before a GTS_PDFA1 RGB one,
// over DeviceRGB content.
//
// At PDF/A-1b the output kept 6.2.2 t2, since only clause 6.2.3 was claimed.
// At PDF/A-2b the PDF/A intent took the CMYK profile of the first entry, which
// uncovered the DeviceRGB content, and a second round replaced every intent
// with a stock sRGB one.
func TestRemediatePDFAUnifiesOnThePDFAIntentProfile(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data, err := os.ReadFile("../../test/integration/corpus/litho/pdfs/twoprofile_1b.pdf")
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}

	const unified = "unified /DestOutputProfile on OutputIntent[0] (/S /GTS_PDFX) with OutputIntent[1] (/S /GTS_PDFA1)"
	for _, profile := range []models.PDFAProfile{models.PDFA1b, models.PDFA2b} {
		t.Run(string(profile), func(t *testing.T) {
			t.Parallel()
			r, err := pdf.Open(ctx, bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer r.Close()
			w := pdf.NewWriter(r)
			defer w.Close()
			report, err := w.RemediatePDFA(ctx, profile)
			if err != nil {
				t.Fatalf("RemediatePDFA: %v", err)
			}
			if !slices.ContainsFunc(report.Fixed, func(f string) bool { return strings.HasPrefix(f, unified) }) {
				t.Errorf("no fix unifies the PDF/X intent on the PDF/A one; fixes = %q", report.Fixed)
			}
			if slices.ContainsFunc(report.Fixed, func(f string) bool { return strings.Contains(f, "OutputIntent to catalog") }) {
				t.Errorf("remediation replaced the output intents; fixes = %q", report.Fixed)
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
			vs, err := remediated.ValidatePDFAReport(ctx, profile)
			if err != nil {
				t.Fatalf("ValidatePDFAReport: %v", err)
			}
			for _, v := range vs {
				t.Errorf("remediated output reports %s", v.Error())
			}
		})
	}
}
