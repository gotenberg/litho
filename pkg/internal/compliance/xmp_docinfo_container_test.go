package compliance

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestXMPCheckDocInfoSync_ContainerType pins which RDF container carries each
// container-typed §6.7.3 analogue. dc:title and dc:description are language
// alternatives, so only an rdf:Alt answers the lookup; dc:creator is an ordered
// array, so either ordered container does and only the unordered rdf:Bag does
// not. A property carried by the wrong container leaves the Info entry nothing
// to match, and the rule fires even though the text is identical on both sides.
//
// Measured against veraPDF on a nine-cell matrix built from pdfjs issue4706,
// with the text held equal to the Info entry throughout. The real files behind
// it are issue4706 (dc:description in an rdf:Seq, 6.7.3 t4) and issue12213.1
// (dc:creator in an rdf:Bag, 6.7.3 t3).
func TestXMPCheckDocInfoSync_ContainerType(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// packet wraps a dc property whose single entry holds text, inside the
	// named RDF container.
	packet := func(prop, container, text string) []byte {
		entry := fmt.Sprintf("<rdf:li>%s</rdf:li>", text)
		if container == "Alt" {
			entry = fmt.Sprintf(`<rdf:li xml:lang="x-default">%s</rdf:li>`, text)
		}
		return xmpPacket(fmt.Sprintf(
			`<rdf:Description rdf:about="" xmlns:dc="http://purl.org/dc/elements/1.1/">`+
				`<dc:%s><rdf:%s>%s</rdf:%s></dc:%s></rdf:Description>`,
			prop, container, entry, container, prop))
	}

	cases := []struct {
		prop      string
		container string
		infoKey   string
		rule      Rule
		wantFire  bool
	}{
		{"title", "Alt", "Title", Rule_19005_1_6_7_3_t02, false},
		{"title", "Seq", "Title", Rule_19005_1_6_7_3_t02, true},
		{"title", "Bag", "Title", Rule_19005_1_6_7_3_t02, true},
		{"creator", "Alt", "Author", Rule_19005_1_6_7_3_t03, false},
		{"creator", "Seq", "Author", Rule_19005_1_6_7_3_t03, false},
		{"creator", "Bag", "Author", Rule_19005_1_6_7_3_t03, true},
		{"description", "Alt", "Subject", Rule_19005_1_6_7_3_t04, false},
		{"description", "Seq", "Subject", Rule_19005_1_6_7_3_t04, true},
		{"description", "Bag", "Subject", Rule_19005_1_6_7_3_t04, true},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("dc:%s in rdf:%s", tc.prop, tc.container), func(t *testing.T) {
			t.Parallel()

			const text = "Shared"
			doc, _ := objsDocPDF(t, "<< /Type /Catalog >>",
				fmt.Sprintf("<< /%s (%s) >>", tc.infoKey, text))
			doc.Index.Trailer["Info"] = ast.PdfIndirectRef{ObjectNum: 2}

			var results []Violation
			xmpCheckDocInfoSync(ctx, doc, packet(tc.prop, tc.container, text), 1, &results)

			fired := false
			for _, v := range results {
				if v.Rule == tc.rule {
					fired = true
				}
			}
			if fired != tc.wantFire {
				t.Errorf("dc:%s in rdf:%s: /%s violation fired = %v, want %v (%d violations)",
					tc.prop, tc.container, tc.infoKey, fired, tc.wantFire, len(results))
			}
		})
	}
}
