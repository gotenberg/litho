package main

import (
	"strings"
	"testing"
)

// TestForms_TextOutputNamesTypeAndPages pins the text output of `litho forms`:
// each field's type and the pages its widgets are on, beside its name. A field
// no page shows names its type alone.
func TestForms_TextOutputNamesTypeAndPages(t *testing.T) {
	src := writeTempPDF(t, renderPrintPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [5 0 R 8 0 R 9 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [6 0 R] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [7 0 R 8 0 R] >>",
		"<< /FT /Tx /T (total) /V (42) /Kids [6 0 R 7 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /Parent 5 0 R /Rect [72 700 272 720] /P 3 0 R >>",
		"<< /Type /Annot /Subtype /Widget /Parent 5 0 R /Rect [72 50 272 70] /P 4 0 R >>",
		"<< /Type /Annot /Subtype /Widget /FT /Btn /T (agree) /Rect [72 300 92 320] /P 4 0 R >>",
		"<< /FT /Tx /T (calc) /V (7) >>",
	}))

	rc, stdout, stderr := runPipeCLI(t, "forms", src)
	if rc != exitOK {
		t.Fatalf("forms: exit %d; stderr: %s", rc, stderr)
	}
	for _, want := range []string{
		"total (text, pages 1,2): 42\n",
		"agree (checkbox, page 2): Off\n",
		"calc (text): 7\n",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output lacks %q; stdout:\n%s", want, stdout)
		}
	}
}
