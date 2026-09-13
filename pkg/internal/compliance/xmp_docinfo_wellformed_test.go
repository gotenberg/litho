package compliance

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestXMPCheckDocInfoSync_MalformedPacketYieldsNoProperties pins that a packet
// which is not well-formed XML supplies no §6.7.3 analogue at all, so every
// Info entry present is unmatched. XMPCore builds properties only from a packet
// it parsed, which is the same behaviour the namespace and predefined-property
// audits are already gated on; recovering values from a broken packet instead
// let litho pass tests veraPDF fails.
//
// The real file is pdfjs issue18941.pdf, whose packet ends "</x:xmpmeta" with
// no closing ">". veraPDF reports all six of its Info keys; litho passed three
// of them by reading values a lenient parse recovered.
func TestXMPCheckDocInfoSync_MalformedPacketYieldsNoProperties(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const dc = `<rdf:Description rdf:about="" xmlns:dc="http://purl.org/dc/elements/1.1/">` +
		`<dc:title><rdf:Alt><rdf:li xml:lang="x-default">Report</rdf:li></rdf:Alt></dc:title>` +
		`</rdf:Description>`

	// Both packets carry the same dc:title text as /Title, so only
	// well-formedness separates them.
	wellFormed := `<x:xmpmeta xmlns:x="adobe:ns:meta/">` +
		`<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` + dc +
		`</rdf:RDF></x:xmpmeta>`
	malformed := `<x:xmpmeta xmlns:x="adobe:ns:meta/">` +
		`<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` + dc +
		`</rdf:RDF></x:xmpmeta`

	for _, tc := range []struct {
		name     string
		packet   string
		wantFire bool
	}{
		{"well-formed packet matches /Title", wellFormed, false},
		{"packet missing its final '>' supplies nothing", malformed, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			doc, catalog := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R /Metadata 4 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
				fmt.Sprintf("<< /Type /Metadata /Subtype /XML /Length %d >>\nstream\n%s\nendstream",
					len(tc.packet), tc.packet),
				"<< /Title (Report) >>",
			)
			doc.Index.Trailer["Info"] = ast.PdfIndirectRef{ObjectNum: 5}

			results := checkXMPGroup(ctx, &validationCtx{
				doc: doc, catalog: catalog, profile: "PDF/A-1b", part: 1,
			})

			fired := false
			for _, v := range results {
				if v.Rule == Rule_19005_1_6_7_3_t02 {
					fired = true
				}
			}
			if fired != tc.wantFire {
				t.Errorf("/Title violation fired = %v, want %v (%d violations)",
					fired, tc.wantFire, len(results))
			}
		})
	}
}
