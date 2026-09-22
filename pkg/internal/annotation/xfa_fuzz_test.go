package annotation

import (
	"strings"
	"testing"
)

// FuzzParseXFADatasets feeds arbitrary XFA packets to the datasets parser.
//
// The bytes are document-supplied: the /XFA value is a stream, or an array of
// name/stream pairs, which [assembleXFAXML] concatenates and FlateDecodes
// before handing them here, and [pdf.Reader.FormData] reaches it through
// ExtractFormData. CONTRIBUTING requires a target beside any function whose
// leading argument is attacker-controlled []byte, and this package had none at
// all. The eight existing tests are hand-written well-formed packets nested at
// most four levels deep, so nothing exercised malformed or adversarial input.
func FuzzParseXFADatasets(f *testing.F) {
	for _, seed := range []string{
		`<xfa:datasets xmlns:xfa="http://www.xfa.org/schema/xfa-data/1.0/"><xfa:data><form><field>v</field></form></xfa:data></xfa:datasets>`,
		`<xfa:datasets><xfa:data><root><l1><l2><l3>deep</l3></l2></l1></root></xfa:data></xfa:datasets>`,
		`<xfa:datasets><xfa:data></xfa:data></xfa:datasets>`,
		`<xfa:datasets><xfa:data><f>  spaced  </f></xfa:data></xfa:datasets>`,
		`<xfa:datasets><xfa:data><ns:form xmlns:ns="http://e"><ns:x>1</ns:x></ns:form></xfa:data></xfa:datasets>`,
		`<xfa:datasets><xfa:data><a><b>x</b><b>y</b></a></xfa:data></xfa:datasets>`,
		`<?xml version="1.0"?><not-closed>`,
		`<other><thing/></other>`,
		`<xfa:datasets><xfa:data><trunc>`,
		``,
	} {
		f.Add([]byte(seed))
	}

	f.Fuzz(func(t *testing.T, xmlData []byte) {
		// Cap the packet size. walkXFADataTree/collectXFAElement recurse with
		// NO depth bound, and that mechanism is already adjudicated "real
		// mechanism, no reach, do not change": the path building is quadratic
		// in depth while the deepest packet in the corpora parses in 1ms.
		// Fuzzing past the cap would red this target on a known non-defect
		// instead of on anything new. At 8 KB the shallowest possible nesting,
		// three bytes per level, bounds depth near 2700, which the recorded
		// measurements put in the low milliseconds.
		if len(xmlData) > 8192 {
			return
		}

		result, err := parseXFADatasets(xmlData)
		if err != nil {
			if result != nil {
				t.Fatalf("parseXFADatasets returned %d keys alongside error %v", len(result), err)
			}
			return
		}

		for k, v := range result {
			if k == "" {
				t.Fatalf("empty key holding value %q", v)
			}
			// NO assertion about where dots fall. An earlier version rejected
			// ".." and a leading or trailing ".", reasoning that dots are only
			// separators. That is FALSE: XML allows "." inside a name (it is a
			// NameChar, just not a NameStartChar), so <A.> is well formed and
			// "A." is the correct path for it. The fuzzer produced exactly
			// that, and the assertion was the thing that was wrong.
			//
			// It does expose a real property, documented rather than fixed in
			// TestParseXFADatasets_DotInElementNameIsAmbiguous: the flat
			// dotted map cannot distinguish a root element named "A.B" from a
			// "B" nested inside an "A". Changing that means changing the shape
			// [pdf.Reader.FormData] returns, for an edge no corpus file holds.
			// collectXFAElement returns strings.TrimSpace of a leaf's text, so
			// every stored value is trimmed. Its error path at xfa.go:169
			// returns the buffer untrimmed, which this asserts is unreachable
			// or a defect rather than tolerating the difference.
			if v != strings.TrimSpace(v) {
				t.Fatalf("value %q for key %q is not trimmed", v, k)
			}
		}
	})
}
