package compliance

import (
	"context"
	"testing"
)

// TestCheckStreamLengths_NonNumberLength pins that a stream /Length that is no
// number fails ISO 19005-2, 6.1.7.1 t1.
//
// ISO 32000-1, Table 5 makes /Length an integer, and 7.3.10 treats a reference
// to an undefined object as a reference to the null object. veraPDF reads any
// /Length that is not a number as null, which matches no real extent, while a
// real that equals the extent passes. Every case was measured against veraPDF.
//
// # Corpus evidence
//
//   - pdfjs issue19510.pdf: gives its XRef stream /Length 9 0 R, and object 9
//     is the page's content stream. veraPDF reports the XRef stream, and
//     litho, which skipped any /Length it could not read as an integer, said
//     nothing.
func TestCheckStreamLengths_NonNumberLength(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name   string
		length string
		want   bool
	}{
		{"an integer that matches", "3", false},
		{"a real that matches", "3.0", false},
		{"a reference to an undefined object", "99 0 R", true},
		{"null", "null", true},
		{"a name", "/Foo", true},
		{"a string", "(3)", true},
		{"a boolean", "true", true},
		{"an array", "[3]", true},
		{"a dictionary", "<< >>", true},
		{"a reference to a stream", "5 0 R", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R >>",
				"<< /Length "+tt.length+" >>\nstream\nq Q\nendstream",
				"<< /Length 2 >>\nstream\nab\nendstream",
			)
			if got := len(checkStreamLengths(ctx, doc, "PDF/A-2b", nil)) > 0; got != tt.want {
				t.Errorf("6.1.7.1 t1 fired = %v, want %v", got, tt.want)
			}
		})
	}
}
