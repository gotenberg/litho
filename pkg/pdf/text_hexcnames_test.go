package pdf

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
)

// TestExtractText_HexadecimalCNamesReadAsHexadecimal pins that a font whose
// /Differences names glyphs "Cxx" reads every such name in hexadecimal once
// one of them is no decimal number: "C2c" proves the font's convention, so
// "C41" and "C42" name "A" and "B", not ")" and "*". A font whose "Cdd" names
// are all decimal keeps reading them so.
func TestExtractText_HexadecimalCNamesReadAsHexadecimal(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		differences string
		want        string
	}{
		{"one name is hexadecimal", "[65 /C41 /C42 /C2c]", "AB,"},
		{"every name is decimal", "[65 /C65 /C66 /C44]", "AB,"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			const content = "BT /F1 12 Tf 100 700 Td (ABC) Tj ET\n"
			data := buildTestPDF([]string{
				"<< /Type /Catalog /Pages 2 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 4 0 R /Contents 5 0 R >>",
				"<< /Font << /F1 6 0 R >> >>",
				fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
				"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding << /Type /Encoding /Differences " + tc.differences + " >> >>",
			}, "")

			ctx := context.Background()
			r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer r.Close()

			got, err := r.ExtractText(ctx, 1)
			if err != nil {
				t.Fatalf("ExtractText: %v", err)
			}
			if got = strings.TrimSpace(got); got != tc.want {
				t.Fatalf("ExtractText = %q, want %q", got, tc.want)
			}
		})
	}
}
