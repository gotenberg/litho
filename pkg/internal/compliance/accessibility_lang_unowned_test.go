package compliance

import (
	"context"
	"fmt"
	"testing"
)

// TestCheckLangRequired_TextNoElementOwns pins ISO 14289-1 §7.2 t34 for text no
// structure element owns, in a tagged document with no catalog /Lang.
//
// The element walk judges only text a structure element owns through an /MCID,
// so text outside every marked-content sequence, or inside one with no /MCID,
// went unexamined whenever a structure tree existed. veraPDF resolves every
// SETextItem through the marked content around it and reports it when nothing
// there names a language: pdfjs issue16176.pdf shows a Tj outside every
// sequence, bug1755507.pdf three inside a /Part with no /MCID, and on a
// one-page fixture veraPDF reports artifact text the same way. A /Lang on the
// enclosing sequence covers the text.
func TestCheckLangRequired_TextNoElementOwns(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		fires   bool
	}{
		{"text outside every sequence", "BT 10 50 Td (x) Tj ET\n", true},
		{"text in a sequence with no MCID", "/Part <<>> BDC\nBT 10 50 Td (x) Tj ET\nEMC\n", true},
		{"artifact text", "/Artifact BMC\nBT 10 50 Td (x) Tj ET\nEMC\n", true},
		{"text under a marked-content /Lang", "/Span <</Lang (en-US)>> BDC\nBT 10 50 Td (x) Tj ET\nEMC\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, cat := tableDoc(t, 1,
				"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 5 0 R >>",
				"<< /Type /StructTreeRoot >>",
				fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(tt.content), tt.content),
			)
			got := checkLangRequired(context.Background(), doc, cat, "PDF/UA-1")
			if fired := hasRule(got, Rule_14289_1_7_2_t34); fired != tt.fires {
				t.Errorf("7.2 t34 fired = %v, want %v (%v)", fired, tt.fires, got)
			}
		})
	}
}
