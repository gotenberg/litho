package compliance

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestUA1HeaderAfterPrefix pins that the PDF/UA-1 header rule (ISO 14289-1,
// 6.1 t1) judges the line that holds the header, wherever the file writes it:
// whole lines before a well-formed %PDF-1.7 pass, as veraPDF passes them,
// bytes before it on its own line fail, and a prefix before %PDF-2.0 still
// fails on the version. Each prefix was measured on veraPDF 1.30.2.
func TestUA1HeaderAfterPrefix(t *testing.T) {
	t.Parallel()
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
	}

	for _, tc := range []struct {
		prefix, header string
		want           bool
	}{
		{"SomeData\n", "%PDF-1.7", false},
		{"\r\n", "%PDF-1.7", false},
		{" ", "%PDF-1.7", true},
		{"\x00\x81\x81", "%PDF-1.7", true},
		{"SomeData\n", "%PDF-2.0", true},
	} {
		data := bytes.Replace(cyclePDF(objs...), []byte("%PDF-1.7"), []byte(tc.header), 1)
		data = append([]byte(tc.prefix), data...)
		doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			t.Fatalf("%q%s: NewDocument: %v", tc.prefix, tc.header, err)
		}
		if doc.HeaderOffset != int64(len(tc.prefix)) {
			t.Fatalf("%q%s: header offset = %d, want %d", tc.prefix, tc.header, doc.HeaderOffset, len(tc.prefix))
		}
		got := hasRule(checkUAHeaderGroup(context.Background(), &validationCtx{doc: doc}), Rule_14289_1_6_1_t01)
		if got != tc.want {
			t.Errorf("%q before %s: 6.1 t1 reported = %v, want %v", tc.prefix, tc.header, got, tc.want)
		}
	}
}

// TestUA1HeaderLine pins the line veraPDF's PDFParser.parseHeader takes for
// the header: the first line when it holds "%PDF-", else the first later line
// holding "PDF-" or starting with a digit.
func TestUA1HeaderLine(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		in, want string
		none     bool
	}{
		{in: "%PDF-1.7\n%\xe2\xe3", want: "%PDF-1.7"},
		{in: "junk %PDF-1.7\rrest", want: "junk %PDF-1.7"},
		{in: "junk\r\n\r\n%PDF-1.4\n", want: "%PDF-1.4"},
		{in: "junk\n1 0 obj\n%PDF-1.4\n", want: "1 0 obj"},
		{in: "junk\nPDF-1.4\n", want: "PDF-1.4"},
		{in: "1 junk\n%PDF-1.4", want: "%PDF-1.4"},
		{in: "junk\nmore junk", none: true},
		{in: "", none: true},
	} {
		got := ua1HeaderLine([]byte(tc.in))
		if tc.none {
			if got != nil {
				t.Errorf("ua1HeaderLine(%q) = %q, want none", tc.in, got)
			}
			continue
		}
		if string(got) != tc.want {
			t.Errorf("ua1HeaderLine(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
