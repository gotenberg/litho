package compliance

import "testing"

// TestXMPWellFormed_UndeclaredPrefix covers a name written through a prefix no
// declaration binds. Namespaces in XML 1.0, §3 requires every prefix to be
// declared and veraPDF's XMPCore treats a violation as a parse failure, but
// Go's encoding/xml reads on and leaves the raw prefix in Name.Space, so such a
// packet parsed cleanly here.
func TestXMPWellFormed_UndeclaredPrefix(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		xml  string
		want bool
	}{
		{
			name: "an element named through an undeclared prefix",
			xml:  `<r><zz:bogus>x</zz:bogus></r>`,
			want: false,
		},
		{
			name: "an attribute named through an undeclared prefix",
			xml:  `<r><t zz:q="1">x</t></r>`,
			want: false,
		},
		{
			name: "a prefix used outside the element that declares it",
			xml:  `<r><a xmlns:p="urn:x"/><p:b/></r>`,
			want: false,
		},
		{
			name: "a declared prefix on the element that declares it",
			xml:  `<p:r xmlns:p="urn:x">x</p:r>`,
			want: true,
		},
		{
			name: "a declared prefix on a descendant",
			xml:  `<r xmlns:dc="urn:d"><dc:t dc:q="1">x</dc:t></r>`,
			want: true,
		},
		{
			name: "the xml prefix needs no declaration",
			xml:  `<r xml:lang="en">x</r>`,
			want: true,
		},
		{
			name: "a default namespace binds unprefixed names",
			xml:  `<r xmlns="urn:def"><t>x</t></r>`,
			want: true,
		},
		{
			name: "no namespaces at all",
			xml:  `<r><t a="1">x</t></r>`,
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
