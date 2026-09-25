package signature

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// byteRangeSlot is the placeholder a certified fixture writes for the length
// of its second signed byte range. It has the width of the number that
// replaces it, so no offset in the file moves.
const byteRangeSlot = "LLLLLLLLLL"

// certifiedWithUnparsablePKCS7 is a document certified at the given DocMDP
// policy whose /ByteRange covers the whole file and whose /Contents holds no
// PKCS#7 data a parser accepts.
func certifiedWithUnparsablePKCS7(t *testing.T, policy int) []byte {
	t.Helper()
	data := buildSigPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] /SigFlags 3 >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Sig /T (cert) /Rect [0 0 0 0] /P 3 0 R /V 5 0 R >>",
		fmt.Sprintf("<< /Type /Sig /ByteRange [0 1 1 %s] /Contents <30> /Reference [<< /Type /SigRef /TransformMethod /DocMDP /TransformParams << /Type /TransformParams /P %d /V /1.2 >> >>] >>", byteRangeSlot, policy),
	}, "")
	length := fmt.Sprintf("%0*d", len(byteRangeSlot), len(data)-1)
	return bytes.Replace(data, []byte(byteRangeSlot), []byte(length), 1)
}

// TestVerifySignatures_MDPWithoutParsablePKCS7 pins that the DocMDP policy,
// which the signature dictionary's /Reference carries (ISO 32000-1, 12.8.1,
// Tables 252 and 253), is reported when the PKCS#7 data does not parse, beside
// the error that says so.
func TestVerifySignatures_MDPWithoutParsablePKCS7(t *testing.T) {
	tests := []struct {
		name   string
		policy int
		update []updatedObject
		want   bool
	}{
		{"P=1 unchanged", 1, nil, true},
		{"P=2 unchanged", 2, nil, true},
		{"P=1 changed", 1, []updatedObject{{3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Annots [4 0 R] >>"}}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			data := certifiedWithUnparsablePKCS7(t, tc.policy)
			if tc.update != nil {
				data = appendUpdate(t, data, tc.update...)
			}
			doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
			if err != nil {
				t.Fatalf("NewDocument: %v", err)
			}
			results, err := VerifySignatures(ctx, doc, nil)
			if err != nil {
				t.Fatalf("VerifySignatures: %v", err)
			}
			if len(results) != 1 {
				t.Fatalf("got %d validations, want 1", len(results))
			}
			r := results[0]
			if !strings.Contains(r.ErrorMessage, "failed to parse certificates") {
				t.Errorf("ErrorMessage %q does not report the PKCS#7 parse failure", r.ErrorMessage)
			}
			if r.HashValid || r.CertChainValid {
				t.Errorf("HashValid = %v, CertChainValid = %v: want both false for unparsable PKCS#7", r.HashValid, r.CertChainValid)
			}
			if r.MDPPolicy != tc.policy {
				t.Errorf("MDPPolicy = %d, want %d", r.MDPPolicy, tc.policy)
			}
			if r.MDPValid != tc.want || r.MDPInfo == "" {
				t.Errorf("MDPValid = %v (MDPInfo %q), want %v with a reason", r.MDPValid, r.MDPInfo, tc.want)
			}
		})
	}
}
