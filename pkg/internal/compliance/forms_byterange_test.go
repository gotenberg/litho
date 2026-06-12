package compliance

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// byteRangePDF builds a signed-form PDF whose signature carries the
// given /ByteRange body (or none when empty).
func byteRangePDF(byteRange string) []byte {
	sig := "<< /Type /Sig /Filter /Adobe.PPKLite /Contents <00> "
	if byteRange != "" {
		sig += "/ByteRange " + byteRange + " "
	}
	sig += ">>"
	return cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /FT /Sig /T (Sig1) /V 5 0 R /Rect [0 0 0 0] /Subtype /Widget /Type /Annot >>",
		sig,
	)
}

// TestSignatureByteRange pins ISO 19005-2 §6.4.3 t1: a /ByteRange that
// does not cover the entire file fails, a missing ByteRange stays
// silent (the veraPDF goldens fire t1 only on present, non-covering
// ranges), and part 1 carries no such rule.
func TestSignatureByteRange(t *testing.T) {
	run := func(data []byte, profile models.PDFAProfile) []Violation {
		t.Helper()
		doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			t.Fatalf("NewDocument: %v", err)
		}
		errs, err := ValidatePDFA(context.Background(), doc, profile)
		if err != nil {
			t.Fatalf("ValidatePDFA: %v", err)
		}
		var out []Violation
		for _, e := range errs {
			if v, ok := e.(Violation); ok && v.Rule.Clause == "6.4.3" && v.Rule.TestNumber == 1 {
				out = append(out, v)
			}
		}
		return out
	}

	bad := byteRangePDF("[0 1022 4862 6654]")
	if vs := run(bad, models.PDFA2b); len(vs) == 0 {
		t.Error("non-covering /ByteRange not flagged under PDF/A-2b")
	}
	if vs := run(bad, models.PDFA1b); len(vs) != 0 {
		t.Errorf("part 1 has no 6.4.3 t1 rule, got %v", vs)
	}

	none := byteRangePDF("")
	if vs := run(none, models.PDFA2b); len(vs) != 0 {
		t.Errorf("missing /ByteRange must stay silent for t1, got %v", vs)
	}
}
