package compliance

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestXMPCheckPropertyTypes_EmptyLangAlt pins a Lang Alt property whose rdf:Alt
// holds no item as correctly typed, as veraPDF 1.30.2 judges dc:title,
// dc:description and dc:rights written that way at PDF/A-1b (6.7.9 t3) and
// PDF/A-2b (6.6.2.3.1 t2). Its LangAltValidator accepts an alternative array
// with no children; an item without xml:lang still fails.
func TestXMPCheckPropertyTypes_EmptyLangAlt(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, body string
		want       bool
	}{
		{"empty dc:title", "<dc:title><rdf:Alt/></dc:title>", false},
		{"dc:title of whitespace", "<dc:title><rdf:Alt>\n  </rdf:Alt></dc:title>", false},
		{"empty dc:description", "<dc:description><rdf:Alt/></dc:description>", false},
		{"empty dc:rights", "<dc:rights><rdf:Alt/></dc:rights>", false},
		{"dc:title item without xml:lang", "<dc:title><rdf:Alt><rdf:li>x</rdf:li></rdf:Alt></dc:title>", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			packet := xmpPacket(`<rdf:Description rdf:about="" xmlns:dc="http://purl.org/dc/elements/1.1/">` + tc.body + `</rdf:Description>`)
			for part, rule := range map[int]Rule{1: Rule_19005_1_6_7_9_t03, 2: Rule_19005_2_6_6_2_3_1_t02} {
				var results []Violation
				xmpCheckPropertyTypes(packet, part, ast.PdfStream{}, nil, &results)
				got := false
				for _, v := range results {
					if v.Rule == rule {
						got = true
					}
				}
				if got != tc.want {
					t.Errorf("part %d: %s fired = %v, want %v (%v)", part, rule.Clause, got, tc.want, results)
				}
			}
		})
	}
}
