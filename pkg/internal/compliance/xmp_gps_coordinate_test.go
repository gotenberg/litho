package compliance

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// gpsXMP wraps a single exif GPS property value in a minimal XMP packet.
//
// # Corpus evidence
//
//   - verapdf 6-7-2-t17 fixtures: carry these properties, and the packet is
//     shaped like them.
func gpsXMP(prop, value string) []byte {
	return fmt.Appendf(nil, `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
  <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
    <rdf:Description rdf:about="" xmlns:exif="http://ns.adobe.com/exif/1.0/">
      <exif:%s>%s</exif:%s>
    </rdf:Description>
  </rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`, prop, value, prop)
}

// gpsTypeViolations returns only the property-type violations naming prop.
func gpsTypeViolations(t *testing.T, prop, value string, part int) []Violation {
	t.Helper()
	var results []Violation
	xmpCheckPropertyTypes(gpsXMP(prop, value), part, ast.PdfStream{}, nil, &results)
	var out []Violation
	for _, v := range results {
		if strings.Contains(v.Context, prop) {
			out = append(out, v)
		}
	}
	return out
}

// TestXMPGPSCoordinateType pins that exif GPS coordinate properties are judged
// as GPSCoordinate rather than plain Text.
//
// The XMP Specification part 2 (ISO 16684) types exif:GPSLatitude and its
// siblings as GPSCoordinate, so a value that is not one states something the
// referenced type does not define. Typed as Text they were accepted whatever
// they held, because ISO 16684-1 makes Text any sequence of Unicode characters
// and the type check returns true for it unconditionally.
//
// # Corpus evidence
//
//   - verapdf: the valid coordinates below are the two the corpus actually
//     carries, so a stricter reading of the grammar would still pass them.
//   - verapdf: the fail fixtures for these clauses carry an empty value, and
//     another rule decides them, so the empty case must stay silent or those
//     cells would move.
func TestXMPGPSCoordinateType(t *testing.T) {
	t.Parallel()

	props := []string{"GPSLatitude", "GPSLongitude", "GPSDestLatitude", "GPSDestLongitude"}

	t.Run("valid coordinates are accepted", func(t *testing.T) {
		t.Parallel()
		for _, prop := range props {
			for _, value := range []string{"67,19,26S", "43,40,53W"} {
				if got := gpsTypeViolations(t, prop, value, 2); len(got) != 0 {
					t.Errorf("exif:%s=%q reported %d violation(s), want none: %v",
						prop, value, len(got), got)
				}
			}
		}
	})

	// The discriminating case. Typed as Text this produced nothing at all.
	t.Run("a malformed coordinate is reported", func(t *testing.T) {
		t.Parallel()
		for _, prop := range props {
			got := gpsTypeViolations(t, prop, "not-a-coordinate", 2)
			if len(got) == 0 {
				t.Errorf("exif:%s=%q reported no violation: the property is not being "+
					"judged as a GPS coordinate", prop, "not-a-coordinate")
			}
		}
	})

	// An empty value never reaches the type check: xmpCheckPropertyTypes guards
	// with pv.value != "". Another rule decides that shape, so this must stay
	// silent.
	t.Run("an empty value stays silent", func(t *testing.T) {
		t.Parallel()
		for _, prop := range props {
			if got := gpsTypeViolations(t, prop, "", 2); len(got) != 0 {
				t.Errorf("exif:%s empty reported %d violation(s), want none: %v",
					prop, len(got), got)
			}
		}
	})

	// ISO 19005-4 drops the extension-schema requirements of parts 1 to 3, and
	// xmpPropertyTypeRule returns nothing for part 4. Pinning it keeps a later
	// edit from reporting a clause that part does not state.
	t.Run("part 4 states no such clause", func(t *testing.T) {
		t.Parallel()
		if got := gpsTypeViolations(t, "GPSLatitude", "not-a-coordinate", 4); len(got) != 0 {
			t.Errorf("part 4 reported %d violation(s), want none: %v", len(got), got)
		}
	})
}
