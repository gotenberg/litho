package compliance

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// TestUAMetadataLang covers ISO 14289-1 §7.2 t33: the document metadata's
// natural language must be determinable. With no catalog /Lang, it is determined
// by a specific xml:lang in the XMP; a packet qualified only with "x-default"
// leaves it undetermined.
//
// # Corpus evidence
//
//   - verapdf 7.2-t33-fail-a, 7.2-t33-pass-b: the fail and pass files this
//     contrast between an "x-default" packet and a specific xml:lang is drawn
//     from.
func TestUAMetadataLang(t *testing.T) {
	t.Parallel()

	xmp := func(lang string) string {
		return "<?xpacket begin='' id='W5M0MpCehiHzreSzNTczkc9d'?>" +
			"<x:xmpmeta xmlns:x='adobe:ns:meta/'>" +
			"<rdf:RDF xmlns:rdf='http://www.w3.org/1999/02/22-rdf-syntax-ns#' " +
			"xmlns:dc='http://purl.org/dc/elements/1.1/'>" +
			"<rdf:Description rdf:about=''><dc:title><rdf:Alt>" +
			"<rdf:li xml:lang='" + lang + "'>Doc</rdf:li>" +
			"</rdf:Alt></dc:title></rdf:Description>" +
			"</rdf:RDF></x:xmpmeta><?xpacket end='w'?>"
	}
	build := func(lang string) []byte {
		x := xmp(lang)
		return cyclePDF(
			"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 5 0 R /MarkInfo << /Marked true >> /Metadata 6 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
			"<< /Length 0 >>\nstream\n\nendstream",
			"<< /Type /StructTreeRoot /K [7 0 R] >>",
			fmt.Sprintf("<< /Type /Metadata /Subtype /XML /Length %d >>\nstream\n%s\nendstream", len(x), x),
			"<< /Type /StructElem /S /Document /Lang (en-US) >>",
		)
	}
	fires := func(data []byte) bool {
		doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			t.Fatalf("NewDocument: %v", err)
		}
		viols, err := ValidatePDFUA(context.Background(), doc, models.PDFUA1)
		if err != nil {
			t.Fatalf("ValidatePDFUA: %v", err)
		}
		for _, e := range viols {
			if v, ok := e.(Violation); ok && v.Rule.Clause == "7.2" && v.Rule.TestNumber == 33 {
				return true
			}
		}
		return false
	}

	if !fires(build("x-default")) {
		t.Error("metadata with only x-default and no catalog /Lang must fire §7.2 t33")
	}
	if fires(build("en-US")) {
		t.Error("metadata with a specific xml:lang must not fire §7.2 t33")
	}
}
