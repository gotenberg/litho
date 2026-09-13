package compliance

import (
	"context"
	"fmt"
	"testing"
)

// TestXMPIdentification_LevelUIsNotAPart1Level pins that conformance level U
// belongs to ISO 19005-2 and later. ISO 19005-1 §6.7.11 t3 names only "A" and
// "B", so a file validated as PDF/A-1 that claims "U" satisfies no level of
// that part, even though the A > U > B hierarchy lets U cover B from part 2 on.
//
// The real file is the veraPDF test suite's
// PDF_A-2u__veraPDF_test_suite_6-2-11-7-2-t02-fail-a.pdf (part=2,
// conformance=U): validated at PDF/A-1b, veraPDF reports both §6.7.11 t2 for
// the part and t3 for the conformance, and litho reported only t2.
func TestXMPIdentification_LevelUIsNotAPart1Level(t *testing.T) {
	t.Parallel()

	packetFor := func(part, conf string) []byte {
		return xmpPacket(fmt.Sprintf(
			`<rdf:Description rdf:about="" xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/"`+
				` pdfaid:part="%s" pdfaid:conformance="%s"/>`, part, conf))
	}

	cases := []struct {
		profile string
		part    int
		claim   string
		fires   bool
	}{
		{"PDF/A-1b", 1, "B", false},
		{"PDF/A-1b", 1, "A", false},
		{"PDF/A-1b", 1, "U", true},
		{"PDF/A-1a", 1, "U", true},
		// Level U is a real level from part 2 onwards and still covers B.
		{"PDF/A-2b", 2, "U", false},
		{"PDF/A-2u", 2, "U", false},
	}
	for _, tc := range cases {
		t.Run(tc.profile+"/"+tc.claim, func(t *testing.T) {
			t.Parallel()

			var got []Violation
			xmpCheckPDFAIdentification(context.Background(),
				packetFor(fmt.Sprintf("%d", tc.part), tc.claim), tc.profile, tc.part, &got)

			rule := map[int]Rule{
				1: Rule_19005_1_6_7_11_t03,
				2: Rule_19005_2_6_6_4_t03,
			}[tc.part]
			if fired := hasRule(got, rule); fired != tc.fires {
				t.Errorf("%s claiming %q: conformance violation fired = %v, want %v (%v)",
					tc.profile, tc.claim, fired, tc.fires, got)
			}
		})
	}
}
