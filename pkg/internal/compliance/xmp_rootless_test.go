package compliance

import "testing"

// TestXMPWellFormed_RequiresRootElement pins that bytes carrying no element at
// all are not a well-formed XML document.
//
// XML 1.0, 2.1 makes the root element part of the document production, so an
// element-free byte string is not a document however cleanly it tokenises. Go's
// encoding/xml reads such input as one run of character data and reports no
// error, so litho called arbitrary bytes well-formed and never raised the
// invalid-serialization rule (ISO 19005-2 §6.6.2.1 t4) on them.
func TestXMPWellFormed_RequiresRootElement(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		data string
		want bool
	}{
		{
			"a real packet is well-formed",
			`<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>` +
				`<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF ` +
				`xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"></rdf:RDF></x:xmpmeta>`,
			true,
		},
		{"bytes carrying no element are not", "not xml at all, just text", false},
		{"empty input is not", "", false},
		// A processing instruction and a comment are prolog content, not the
		// document's element.
		{"a prolog with no element is not", `<?xpacket begin="" id="W5M0Mp"?><!-- nothing -->`, false},
		{"a bare root element is", `<rdf:RDF xmlns:rdf="http://x/"/>`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := xmpWellFormed([]byte(tc.data)); got != tc.want {
				t.Errorf("xmpWellFormed = %v, want %v", got, tc.want)
			}
		})
	}
}
