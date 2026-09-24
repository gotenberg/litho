package compliance

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// TestMetadataLang_AnyMetadataStream pins that ISO 14289-1, 7.2 t33 judges
// every metadata stream a document carries, not the catalog's alone.
//
// ISO 32000-1, 14.3.2 lets any component carry a /Metadata stream, and veraPDF
// models one for every dictionary in the cross-reference table whose /Metadata
// is a metadata stream (GFCosDict.getMetadata over GFCosDocument's
// indirectObjects). With no catalog /Lang, a language alternative qualified only
// by x-default leaves that metadata's language undetermined. Measured against
// veraPDF on fixtures of each shape.
//
// # Corpus evidence
//
//   - pdfjs bug867484.pdf: gives a structure element such a packet; veraPDF
//     reports it and litho, which read the catalog's packet only, did not.
func TestMetadataLang_AnyMetadataStream(t *testing.T) {
	t.Parallel()

	packet := func(lang string) string {
		return "<?xpacket begin=\"\ufeff\" id=\"W5M0MpCehiHzreSzNTczkc9d\"?>" +
			"<x:xmpmeta xmlns:x=\"adobe:ns:meta/\"><rdf:RDF xmlns:rdf=\"http://www.w3.org/1999/02/22-rdf-syntax-ns#\">" +
			"<rdf:Description rdf:about=\"\" xmlns:dc=\"http://purl.org/dc/elements/1.1/\">" +
			"<dc:title><rdf:Alt><rdf:li xml:lang=\"" + lang + "\">Executive</rdf:li></rdf:Alt></dc:title>" +
			"</rdf:Description></rdf:RDF></x:xmpmeta><?xpacket end=\"w\"?>"
	}

	// doc gives the structure element (object 6) a /Metadata stream (object 7)
	// whose dc:title alternative carries lang; catalogLang is appended to the
	// catalog.
	doc := func(lang, catalogLang string) []byte {
		xmp := packet(lang)
		return cyclePDF(
			"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R /MarkInfo << /Marked true >>"+catalogLang+" >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
			"<< /Type /StructTreeRoot /K 5 0 R >>",
			"<< /Type /StructElem /S /Document /P 4 0 R /K [6 0 R] >>",
			"<< /Type /StructElem /S /Part /P 5 0 R /Metadata 7 0 R >>",
			fmt.Sprintf("<< /Type /Metadata /Subtype /XML /Length %d >>\nstream\n%s\nendstream", len(xmp), xmp),
		)
	}

	fires := func(t *testing.T, data []byte) bool {
		t.Helper()
		d, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			t.Fatalf("NewDocument: %v", err)
		}
		viols, err := ValidatePDFUA(context.Background(), d, models.PDFUA1)
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

	tests := []struct {
		name        string
		lang        string
		catalogLang string
		want        bool
	}{
		{"a structure element's x-default title, no catalog /Lang", "x-default", "", true},
		{"a structure element's title in a specific language", "en-US", "", false},
		{"a catalog /Lang determines the metadata language", "x-default", " /Lang (en-US)", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := fires(t, doc(tt.lang, tt.catalogLang)); got != tt.want {
				t.Errorf("7.2 t33 fired = %v, want %v", got, tt.want)
			}
		})
	}
}
