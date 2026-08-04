package compliance

import (
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestXMPPropertyTypeStatedOncePerProperty pins the granularity of ISO 19005-1,
// 6.7.9 t3 and ISO 19005-2/3, 6.6.2.3.1 t2. veraPDF instantiates one object per
// observed XMP property and asks it `isValueTypeCorrect`, so a property that
// fails its predefined type in more than one way still answers once.
//
// dc:title written as an attribute is exactly that case: it is Simple where an
// Alt is required, and having no container it also carries no xml:lang. Both
// findings belong to the one property.
func TestXMPPropertyTypeStatedOncePerProperty(t *testing.T) {
	t.Parallel()

	packet := []byte(`<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
 <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description rdf:about=""
    xmlns:dc="http://purl.org/dc/elements/1.1/"
    dc:title="a simple title"/>
 </rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`)

	for _, part := range []int{1, 2} {
		var results []Violation
		xmpCheckPropertyTypes(packet, part, ast.PdfStream{}, &results)
		got := slices.DeleteFunc(results, func(v Violation) bool {
			return !strings.Contains(v.Context, "purl.org/dc/elements/1.1/:title")
		})
		if len(got) != 1 {
			t.Fatalf("part %d: got %d violations for dc:title, want 1: %v", part, len(got), got)
		}
		// Both reasons must survive into the one violation, or collapsing the
		// count would cost a diagnostic.
		for _, want := range []string{"expected=Alt", "xml:lang"} {
			if !strings.Contains(got[0].Context, want) {
				t.Errorf("part %d: context %q does not mention %q", part, got[0].Context, want)
			}
		}
	}
}
