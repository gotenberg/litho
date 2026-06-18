package remediation

import (
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestFixOutputIntent_SubtypeIsAlwaysGTSPDFA1 pins the OutputIntent subtype
// for every profile. The fixer used to write /S /GTS_PDFA for PDF/A-4, a
// subtype that does not exist (ISO 32000-2 Table 401 keeps GTS_PDFA1 for
// every PDF/A part). An unrecognized subtype is not a PDF/A output intent,
// so the device-colour rules still find no output intent to anchor to and
// keep firing on the "fixed" output.
func TestFixOutputIntent_SubtypeIsAlwaysGTSPDFA1(t *testing.T) {
	for _, profile := range []string{"PDF/A-2b", "PDF/A-4"} {
		pdf := buildStructPDF(
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		)
		doc, alloc := newTestDoc(t, pdf)

		v := compliance.Violation{
			Rule:    compliance.Rule{Clause: "6.2.4.3", TestNumber: 2},
			Context: "DeviceRGB used without OutputIntent",
		}
		descs, err := fixOutputIntent(context.Background(), alloc, doc, Options{}, []compliance.Violation{v}, profile)
		if err != nil {
			t.Fatalf("%s: fixOutputIntent: %v", profile, err)
		}
		if len(descs) == 0 {
			t.Fatalf("%s: expected an OutputIntent fix", profile)
		}

		found := false
		for _, obj := range alloc.Pending() {
			d, ok := obj.(ast.PdfDict)
			if !ok || d["Type"] != ast.PdfName("OutputIntent") {
				continue
			}
			found = true
			if d["S"] != ast.PdfName("GTS_PDFA1") {
				t.Errorf("%s: OutputIntent /S: got %v, want GTS_PDFA1", profile, d["S"])
			}
		}
		if !found {
			t.Fatalf("%s: no OutputIntent allocated; fixes=%s", profile, strings.Join(descs, "; "))
		}
	}
}
