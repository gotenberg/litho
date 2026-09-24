package document

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/filter"
)

// jpxPart2Header is a JPEG 2000 codestream opening whose Rsiz of 8000h
// signals ISO/IEC 15444-2 capabilities, which the JPXDecode decoder refuses.
var jpxPart2Header = []byte{
	0xFF, 0x4F, 0xFF, 0x51, 0x00, 0x29, 0x80, 0x00,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x09,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x09,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x01, 0x07, 0x01, 0x01,
}

// jbig2NecessaryExtension is a JBIG2 page followed by an extension segment
// flagged necessary (T.88, 7.4.14), which the JBIG2Decode decoder refuses.
func jbig2NecessaryExtension() []byte {
	page := binary.BigEndian.AppendUint32(nil, 0)
	page = append(page, 48, 0, 1)
	page = binary.BigEndian.AppendUint32(page, 19)
	page = append(page, 0, 0, 0, 8, 0, 0, 0, 8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0)
	ext := binary.BigEndian.AppendUint32(nil, 1)
	ext = append(ext, 62, 0, 1)
	ext = binary.BigEndian.AppendUint32(ext, 4)
	return append(page, append(ext, 0xA0, 0, 0, 1)...)
}

// streamObject formats a stream object body holding data.
func streamObject(entries string, data []byte) string {
	return fmt.Sprintf("<< %s /Length %d >>\nstream\n%s\nendstream", entries, len(data), data)
}

// TestUndecodableStreams_FeatureGaps pins that the whole-document diagnostics
// list a stream whose built-in decoder refuses a feature it lacks, and agree
// with decoding on every stream: the filters listed are exactly the ones
// decoding skips, and exactly the ones the checked reader refuses.
//
// Before, the diagnostics read filter names alone, so each refused stream
// here was called decodable while decoding skipped its filter.
func TestUndecodableStreams_FeatureGaps(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	g4 := "/DecodeParms << /K -1 /Columns 8 /Rows 1 >>"
	data := buildDestPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [] /Count 0 >>",
		streamObject("/Filter /JPXDecode", jpxPart2Header),
		streamObject("/Filter /JBIG2Decode", jbig2NecessaryExtension()),
		streamObject("/Filter /CCITTFaxDecode "+g4, []byte{0x03, 0xC0}),
		streamObject("/Filter /CCITTFaxDecode "+g4, []byte{0x80, 0x10, 0x01}),
		streamObject("/Filter /Crypt /DecodeParms << /Name /OtherCF >>", []byte("still encrypted")),
		streamObject("/Filter /Crypt", []byte("plain")),
		streamObject("/Filter 10 0 R", jpxPart2Header),
		"/JPXDecode",
		streamObject("/Filter /NoSuchDecode", []byte("raw")),
	)
	doc, err := NewDocument(ctx, bytes.NewReader(data), int64(len(data)), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	diags, err := doc.UndecodableStreams(ctx)
	if err != nil {
		t.Fatalf("UndecodableStreams: %v", err)
	}
	got := map[int][]string{}
	for _, d := range diags {
		got[d.ObjectNum] = d.Filters
	}
	want := map[int][]string{
		3:  {"JPXDecode"},
		4:  {"JBIG2Decode"},
		5:  {"CCITTFaxDecode"},
		7:  {"Crypt"},
		9:  {"JPXDecode"}, // named through an indirect /Filter
		11: {"NoSuchDecode"},
	}
	if len(got) != len(want) {
		t.Errorf("UndecodableStreams = %v, want %v", got, want)
	}
	for num, filters := range want {
		if !slices.Equal(got[num], filters) {
			t.Errorf("object %d: listed %v, want %v", num, got[num], filters)
		}
	}

	for num := 3; num <= 11; num++ {
		if num == 10 {
			continue
		}
		stream, err := ResolveStream(ctx, doc, ast.PdfIndirectRef{ObjectNum: num})
		if err != nil {
			t.Fatalf("object %d: %v", num, err)
		}
		_, report, err := doc.DecodeStreamReport(ctx, stream)
		if err != nil {
			t.Errorf("object %d: DecodeStreamReport: %v", num, err)
		}
		listed := doc.UndecodableFilters(ctx, stream)
		var skipped []string
		for _, f := range report.Skipped {
			skipped = append(skipped, string(f))
		}
		if !slices.Equal(listed, skipped) {
			t.Errorf("object %d: diagnosed %v, decoding skipped %v", num, listed, skipped)
		}
		if doc.StreamHasUndecodableFilter(ctx, stream) != (len(listed) > 0) {
			t.Errorf("object %d: StreamHasUndecodableFilter disagrees with %v", num, listed)
		}
		rc, err := doc.NewStreamReaderChecked(ctx, stream)
		if err == nil {
			_, err = io.ReadAll(rc)
			rc.Close()
		}
		if refused := errors.Is(err, filter.ErrUnsupportedFilter); refused != (len(listed) > 0) {
			t.Errorf("object %d: checked reader error %v, diagnosed %v", num, err, listed)
		}
	}
}

// TestUndecodableFilters_CryptFilterAlreadyApplied pins that a Crypt filter
// naming the crypt filter the security handler decrypted the stream with,
// the document's /StmF, decodes as Identity, and that one naming another
// crypt filter, or any crypt filter in a document that applied none, is
// refused (ISO 32000-1, 7.4.10 and 7.6.5).
func TestUndecodableFilters_CryptFilterAlreadyApplied(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	crypt := func(name string) ast.PdfStream {
		return ast.NewStream(ast.PdfDict{
			"Filter":      ast.PdfArray{ast.PdfName("Crypt"), ast.PdfName("ASCIIHexDecode")},
			"DecodeParms": ast.PdfArray{ast.PdfDict{"Name": ast.PdfName(name)}, ast.PdfNull{}},
		}, []byte("616263>"))
	}
	for _, tc := range []struct {
		name        string
		streamCrypt ast.PdfName
		stream      ast.PdfStream
		refused     bool
	}{
		{"the default stream crypt filter", "StdCF", crypt("StdCF"), false},
		{"another crypt filter", "StdCF", crypt("OtherCF"), true},
		{"a crypt filter in a document that applies none", "", crypt("StdCF"), true},
		{"a single dictionary under the filter array", "StdCF", ast.NewStream(ast.PdfDict{
			"Filter":      ast.PdfArray{ast.PdfName("Crypt"), ast.PdfName("ASCIIHexDecode")},
			"DecodeParms": ast.PdfDict{"Name": ast.PdfName("StdCF")},
		}, []byte("616263>")), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d := &Document{streamCrypt: tc.streamCrypt, Logger: slog.New(slog.DiscardHandler)}

			listed := d.UndecodableFilters(ctx, tc.stream)
			data, err := d.DecodeStream(ctx, tc.stream)
			if tc.refused {
				if !slices.Equal(listed, []string{"Crypt"}) {
					t.Errorf("UndecodableFilters = %v, want [Crypt]", listed)
				}
				return
			}
			if len(listed) != 0 || err != nil || string(data) != "abc" {
				t.Errorf("UndecodableFilters = %v, DecodeStream = %q, %v; want none and %q", listed, data, err, "abc")
			}
			// The rewrite is the pipeline's view: the stream keeps its name.
			parms := tc.stream.Dict["DecodeParms"]
			if arr, isArray := parms.(ast.PdfArray); isArray {
				parms = arr[0]
			}
			if name := parms.(ast.PdfDict)["Name"]; name != ast.PdfName("StdCF") {
				t.Errorf("the stream's own Crypt parameters now name %v", name)
			}
		})
	}
}
