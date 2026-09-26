package compliance

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestUA1HeaderAfterPrefix pins that the PDF/UA-1 header rule (ISO 14289-1,
// 6.1 t1) reads the header where the file writes it: bytes before a
// well-formed %PDF-1.7 pass, as veraPDF passes them, while the same prefix
// before %PDF-2.0 still fails on the version.
func TestUA1HeaderAfterPrefix(t *testing.T) {
	t.Parallel()
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
	}
	prefix := []byte("garbage\r\n")

	for _, tc := range []struct {
		header string
		want   bool
	}{
		{"%PDF-1.7", false},
		{"%PDF-2.0", true},
	} {
		data := bytes.Replace(cyclePDF(objs...), []byte("%PDF-1.7"), []byte(tc.header), 1)
		data = append(append([]byte{}, prefix...), data...)
		doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			t.Fatalf("%s: NewDocument: %v", tc.header, err)
		}
		if doc.HeaderOffset != int64(len(prefix)) {
			t.Fatalf("%s: header offset = %d, want %d", tc.header, doc.HeaderOffset, len(prefix))
		}
		got := hasRule(checkUAHeaderGroup(context.Background(), &validationCtx{doc: doc}), Rule_14289_1_6_1_t01)
		if got != tc.want {
			t.Errorf("%s after a %d-byte prefix: 6.1 t1 reported = %v, want %v", tc.header, len(prefix), got, tc.want)
		}
	}
}
