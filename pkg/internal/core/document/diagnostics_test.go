package document

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/xref"
	"github.com/gotenberg/litho/pkg/internal/filter"
	"github.com/gotenberg/litho/pkg/plugins"
)

func TestUndecodableFilters(t *testing.T) {
	d := &Document{} // no plugins registered

	cases := []struct {
		name   string
		filter ast.PdfObject
		want   []string
	}{
		{"flate only", ast.PdfName("FlateDecode"), nil},
		{"flate then jbig2", ast.PdfArray{ast.PdfName("FlateDecode"), ast.PdfName("JBIG2Decode")}, nil},
		{"flate then unknown", ast.PdfArray{ast.PdfName("FlateDecode"), ast.PdfName("BarDecode")}, []string{"BarDecode"}},
		{"flate then jpx", ast.PdfArray{ast.PdfName("FlateDecode"), ast.PdfName("JPXDecode")}, nil},
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
	// A single in-use stream object filtered by a filter no decoder knows.
	data := []byte("%PDF-1.7\n1 0 obj\n<< /Length 4 /Filter /FooDecode >>\nstream\nDATA\nendstream\nendobj\n")
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
	if diags[0].ObjectNum != 1 || !equalStrings(diags[0].Filters, []string{"FooDecode"}) {
		t.Errorf("diag = %+v, want {ObjectNum:1, Filters:[FooDecode]}", diags[0])
	}
}

// vendorPlugin claims one filter name and either decodes every stream to out
// or declines it.
type vendorPlugin struct {
	name    string
	out     []byte
	decline bool
}

func (p vendorPlugin) FilterName() string { return p.name }

func (p vendorPlugin) Decode(_ []byte, _ map[string]any) ([]byte, error) {
	if p.decline {
		return nil, plugins.ErrUnsupportedFilter
	}
	return p.out, nil
}

// TestUndecodableFilters_AgreeWithDecoding pins that the diagnostics describe
// what decoding does, with and without plugins: a stream they call
// undecodable comes back still encoded and flagged incomplete, and one they
// call decodable is never handed back still encoded.
//
// With any plugin registered, a filter no decoder knew failed the whole
// stream while the diagnostics, and every caller that consulted them, expected
// it skipped. A plugin that declines a filter litho has no decoder for must
// fail the stream, since the diagnostics count the filter as decodable.
func TestUndecodableFilters_AgreeWithDecoding(t *testing.T) {
	t.Parallel()

	decoders := map[string]*filter.Decoder{
		"no plugin":        nil,
		"unrelated plugin": filter.NewDecoder([]plugins.FilterDecoder{vendorPlugin{name: "OtherDecode"}}),
		"decoding plugin":  filter.NewDecoder([]plugins.FilterDecoder{vendorPlugin{name: "VendorDecode", out: []byte("decoded")}}),
		"declining plugin": filter.NewDecoder([]plugins.FilterDecoder{vendorPlugin{name: "VendorDecode", decline: true}}),
	}
	chains := map[string]ast.PdfObject{
		"vendor filter":           ast.PdfName("VendorDecode"),
		"hex then vendor filter":  ast.PdfArray{ast.PdfName("ASCIIHexDecode"), ast.PdfName("VendorDecode")},
		"unknown filter":          ast.PdfName("NoSuchDecode"),
		"hex then unknown filter": ast.PdfArray{ast.PdfName("ASCIIHexDecode"), ast.PdfName("NoSuchDecode")},
		"hex only":                ast.PdfName("ASCIIHexDecode"),
	}
	for dname, dec := range decoders {
		for cname, chain := range chains {
			t.Run(dname+"/"+cname, func(t *testing.T) {
				t.Parallel()

				d := &Document{Filters: dec, Logger: slog.New(slog.DiscardHandler)}
				stream := ast.NewStream(ast.PdfDict{"Filter": chain}, []byte("616263>"))
				bad := d.undecodableFilters(stream)
				data, incomplete, err := d.Filters.DecodeStreamChecked(context.Background(), stream)

				if len(bad) > 0 {
					if err != nil || !incomplete {
						t.Errorf("diagnosed undecodable %v, but decoding = (%q, incomplete %v, %v), want the filter skipped",
							bad, data, incomplete, err)
					}
					return
				}
				if incomplete {
					t.Errorf("diagnosed decodable, but decoding skipped a filter and returned %q still encoded", data)
				}
				if err != nil && !errors.Is(err, filter.ErrUnsupportedFilter) {
					t.Errorf("decoding a stream diagnosed decodable: %v", err)
				}
			})
		}
	}
}
