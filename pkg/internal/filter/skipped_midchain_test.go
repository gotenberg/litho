package filter

import (
	"bytes"
	"compress/zlib"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/limits"
)

// TestDecodeStreamReport_SkippedMidChain pins that the report tells a chain
// that decodes past a skipped filter from one whose skipped filters end it.
func TestDecodeStreamReport_SkippedMidChain(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	if _, err := zw.Write([]byte("0 0 1 1 re f")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		chain ast.PdfArray
		want  bool
	}{
		{"unknown before Flate", ast.PdfArray{ast.PdfName("Foo"), ast.PdfName("FlateDecode")}, true},
		{"unknown after Flate", ast.PdfArray{ast.PdfName("FlateDecode"), ast.PdfName("Foo")}, false},
		{"two unknowns after Flate", ast.PdfArray{ast.PdfName("FlateDecode"), ast.PdfName("Foo"), ast.PdfName("Bar")}, false},
		{"Flate alone", ast.PdfArray{ast.PdfName("FlateDecode")}, false},
	} {
		stream := ast.NewStream(ast.PdfDict{"Filter": tc.chain}, buf.Bytes())
		data, report, err := NewDecoder(nil, limits.Default()).DecodeStreamReport(context.Background(), stream)
		if err != nil {
			t.Fatalf("%s: DecodeStreamReport: %v", tc.name, err)
		}
		if report.SkippedMidChain != tc.want {
			t.Errorf("%s: SkippedMidChain = %v, want %v", tc.name, report.SkippedMidChain, tc.want)
		}
		if string(data) != "0 0 1 1 re f" {
			t.Errorf("%s: decoded %q, want the content: the report changes no byte", tc.name, data)
		}
	}
}
