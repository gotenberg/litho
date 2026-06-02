package document

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/xref"
)

func TestUndecodableFilters(t *testing.T) {
	d := &Document{} // no plugins registered

	cases := []struct {
		name   string
		filter ast.PdfObject
		want   []string
	}{
		{"flate only", ast.PdfName("FlateDecode"), nil},
		{"flate then jbig2", ast.PdfArray{ast.PdfName("FlateDecode"), ast.PdfName("JBIG2Decode")}, []string{"JBIG2Decode"}},
		{"jpx", ast.PdfName("JPXDecode"), []string{"JPXDecode"}},
		{"unknown filter", ast.PdfName("FooDecode"), []string{"FooDecode"}},
		{"no filter", nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dict := ast.PdfDict{}
			if tc.filter != nil {
				dict["Filter"] = tc.filter
			}
			got := d.undecodableFilters(ast.NewStream(dict, nil))
			if !equalStrings(got, tc.want) {
				t.Errorf("undecodableFilters = %v, want %v", got, tc.want)
			}
		})
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestUndecodableStreams_EndToEnd(t *testing.T) {
	// A single in-use stream object filtered by JBIG2Decode, which the
	// built-in decoders cannot handle without the jbig2 plugin.
	data := []byte("%PDF-1.7\n1 0 obj\n<< /Length 4 /Filter /JBIG2Decode >>\nstream\nDATA\nendstream\nendobj\n")
	doc := &Document{
		r:    bytes.NewReader(data),
		size: int64(len(data)),
		Index: xref.Index{Entries: map[int]xref.XRefEntry{
			1: {Type: xref.XRefTypeNormal, Offset: 9}, // "1 0 obj" begins after "%PDF-1.7\n"
		}},
		objectCache: make(map[int]ast.PdfObject),
		fontCache:   make(map[int]any),
		cmapCache:   make(map[int]any),
		Logger:      slog.New(slog.DiscardHandler),
	}

	diags, err := doc.UndecodableStreams(context.Background())
	if err != nil {
		t.Fatalf("UndecodableStreams: %v", err)
	}
	if len(diags) != 1 {
		t.Fatalf("got %d diagnostics, want 1: %+v", len(diags), diags)
	}
	if diags[0].ObjectNum != 1 || !equalStrings(diags[0].Filters, []string{"JBIG2Decode"}) {
		t.Errorf("diag = %+v, want {ObjectNum:1, Filters:[JBIG2Decode]}", diags[0])
	}
}
