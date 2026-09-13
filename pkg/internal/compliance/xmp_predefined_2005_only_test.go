package compliance

import (
	"context"
	"fmt"
	"testing"
)

// TestXMPPredefinedProperties_SchemasTheEditionsDisagreeOn pins the three
// schemas the 2005 XMP Specification introduced: Dynamic Media, EXIF Aux and
// Camera Raw Settings. PDF/A-2 and PDF/A-3 read the 2005 edition and predefine
// them (ISO 19005-2/3 §6.6.2.3.1 t1, isPredefinedInXMP2005), while PDF/A-1
// reads the 2004 edition, which has no such schema, so every property under
// them needs an extension schema (ISO 19005-1 §6.7.9 t2,
// isPredefinedInXMP2004).
//
// litho's 2004 catalog carried no entry at all for these namespaces, which let
// the caller treat them as some other known schema's business and wave the
// property through: nine veraPDF test-suite files carrying a bare xmpDM
// property passed §6.7.9 t2, and 203 across the whole corpus.
func TestXMPPredefinedProperties_SchemasTheEditionsDisagreeOn(t *testing.T) {
	t.Parallel()

	cases := []struct {
		prefix   string
		ns       string
		property string
	}{
		{"xmpDM", "http://ns.adobe.com/xmp/1.0/DynamicMedia/", "videoPixelDepth"},
		{"aux", "http://ns.adobe.com/exif/1.0/aux/", "Lens"},
		{"crs", "http://ns.adobe.com/camera-raw-settings/1.0/", "Exposure"},
	}
	for _, tc := range cases {
		t.Run(tc.prefix+":"+tc.property, func(t *testing.T) {
			t.Parallel()

			data := xmpPacket(fmt.Sprintf(
				`<rdf:Description rdf:about="" xmlns:%s="%s"><%s:%s>v</%s:%s></rdf:Description>`,
				tc.prefix, tc.ns, tc.prefix, tc.property, tc.prefix, tc.property))

			var part1 []Violation
			xmpCheckPredefinedProperties(context.Background(), data, 1, nil, &part1)
			if !hasRule(part1, Rule_19005_1_6_7_9_t02) {
				t.Errorf("%s:%s under PDF/A-1 (XMP 2004): got %v, want §6.7.9 t2",
					tc.prefix, tc.property, part1)
			}

			// An extension schema declaring the namespace restores the pass,
			// which is the isDefinedInCurrentPackage half of the same test.
			var declared []Violation
			xmpCheckPredefinedProperties(context.Background(), data, 1,
				map[string]bool{tc.ns: true}, &declared)
			if len(declared) != 0 {
				t.Errorf("%s:%s declared by an extension schema must pass, got %v",
					tc.prefix, tc.property, declared)
			}

			// Parts 2 and 3 read the 2005 edition, which predefines all three.
			for _, part := range []int{2, 3} {
				var got []Violation
				xmpCheckPredefinedProperties(context.Background(), data, part, nil, &got)
				if len(got) != 0 {
					t.Errorf("%s:%s under PDF/A-%d is predefined in XMP 2005, got %v",
						tc.prefix, tc.property, part, got)
				}
			}
		})
	}
}
