package mutations

import (
	"bytes"
	"compress/zlib"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// TestAddAttachmentWithOptions_Compression covers the embedded file stream:
// compressed with FlateDecode when that saves bytes, as it came otherwise,
// with /Params /Size the decoded length either way.
func TestAddAttachmentWithOptions_Compression(t *testing.T) {
	invoice := []byte("<?xml version=\"1.0\"?><rsm:CrossIndustryInvoice>" +
		strings.Repeat("<ram:IncludedSupplyChainTradeLineItem><ram:LineID>1</ram:LineID></ram:IncludedSupplyChainTradeLineItem>", 50) +
		"</rsm:CrossIndustryInvoice>")

	for _, tc := range []struct {
		name       string
		data       []byte
		compressed bool
	}{
		{name: "repetitive XML", data: invoice, compressed: true},
		{name: "too small to save the filter entry", data: []byte("<a/>"), compressed: false},
		{name: "empty", data: []byte{}, compressed: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			src := minimalPDF()
			doc, err := document.NewDocument(ctx, bytes.NewReader(src), int64(len(src)), nil)
			if err != nil {
				t.Fatalf("NewDocument: %v", err)
			}
			alloc := writer.NewObjAllocator(3)
			if err := AddAttachmentWithOptions(ctx, alloc, doc, "factur-x.xml", tc.data, AttachmentOptions{MimeType: "text/xml"}); err != nil {
				t.Fatalf("AddAttachmentWithOptions: %v", err)
			}

			var stream ast.PdfStream
			found := false
			for _, obj := range alloc.Pending() {
				if s, ok := obj.(ast.PdfStream); ok && s.Dict["Type"] == ast.PdfName("EmbeddedFile") {
					stream, found = s, true
				}
			}
			if !found {
				t.Fatal("no embedded file stream staged")
			}
			params, _ := stream.Dict["Params"].(ast.PdfDict)
			if got := params["Size"]; got != ast.PdfInt(len(tc.data)) {
				t.Errorf("/Params /Size = %v, want %d", got, len(tc.data))
			}
			raw, err := stream.Bytes(ctx)
			if err != nil {
				t.Fatalf("stream bytes: %v", err)
			}

			_, filtered := stream.Dict["Filter"]
			if filtered != tc.compressed {
				t.Fatalf("/Filter present = %t, want %t", filtered, tc.compressed)
			}
			if !tc.compressed {
				if !bytes.Equal(raw, tc.data) {
					t.Error("unfiltered stream does not hold the file as given")
				}
				return
			}
			if stream.Dict["Filter"] != ast.PdfName("FlateDecode") {
				t.Errorf("/Filter = %v, want /FlateDecode", stream.Dict["Filter"])
			}
			if len(raw) >= len(tc.data) {
				t.Errorf("compressed stream is %d bytes, file is %d", len(raw), len(tc.data))
			}
			r, err := zlib.NewReader(bytes.NewReader(raw))
			if err != nil {
				t.Fatalf("zlib: %v", err)
			}
			decoded, err := io.ReadAll(r)
			if err != nil {
				t.Fatalf("inflate: %v", err)
			}
			if !bytes.Equal(decoded, tc.data) {
				t.Error("decoded stream differs from the file")
			}
		})
	}
}
