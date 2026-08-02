package compliance

import "testing"

// TestXMPWellFormed_DuplicateAttribute covers a start tag carrying the same
// attribute twice. XML 1.0, §3.1 (WFC: Unique Att Spec) forbids it and
// Namespaces in XML 1.0, §5.3 extends that to two prefixes bound to the same
// URI, but Go's encoding/xml accepts both. A packet writing rdf:about twice
// therefore parsed cleanly here while veraPDF's XMPCore rejected it.
func TestXMPWellFormed_DuplicateAttribute(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		xml  string
		want bool
	}{
		{
			name: "the same attribute written twice",
			xml:  `<x:r xmlns:x="urn:x" a="1" a="2"/>`,
			want: false,
		},
		{
			name: "two prefixes bound to one URI carrying one name",
			xml:  `<r xmlns:a="urn:x" xmlns:b="urn:x" a:k="1" b:k="2"/>`,
			want: false,
		},
		{
			name: "a duplicate deeper in the document",
			xml:  `<r><c d="1" d="2"/></r>`,
			want: false,
		},
		{
			name: "distinct attributes",
			xml:  `<x:r xmlns:x="urn:x" a="1" b="2"/>`,
			want: true,
		},
		{
			name: "one name under two genuinely different namespaces",
			xml:  `<r xmlns:a="urn:x" xmlns:b="urn:y" a:k="1" b:k="2"/>`,
			want: true,
		},
		{
			name: "a single attribute",
			xml:  `<r a="1"/>`,
			want: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := xmpWellFormed([]byte(tc.xml)); got != tc.want {
				t.Errorf("xmpWellFormed(%s) = %v, want %v", tc.xml, got, tc.want)
			}
		})
	}
}
