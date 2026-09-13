package compliance

import (
	"context"
	"fmt"
	"testing"
)

// TestOutputIntentProfileComponents pins ISO 19005-1 §6.2.3.2 t2 for an
// OutputIntent's destination profile. The test reads
// (N == 1 && GRAY) || (N == 3 && (RGB or Lab)) || (N == 4 && CMYK), and veraPDF
// hangs it on its ICCProfile object, which covers an OutputIntent's
// /DestOutputProfile as well as an ICCBased colour space. litho judged only the
// latter.
//
// Only part 1 states the requirement, the same scope [iccBasedNRule] records.
// Measured over 1984 /DestOutputProfile streams in the corpora, seven answer the
// test: five by a genuine mismatch and two by an ICC profile that will not
// decode, and veraPDF reports all seven.
func TestOutputIntentProfileComponents(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// icc builds a minimal ICC profile header declaring the given 4-byte
	// colour space signature.
	icc := func(colorSpace string) string {
		b := make([]byte, iccHeaderSize)
		b[8] = 2 // major version 2, under every part's ceiling
		copy(b[12:16], "mntr")
		copy(b[16:20], colorSpace)
		return string(b)
	}

	cases := []struct {
		name       string
		n          int
		colorSpace string
		profile    string
		fires      bool
	}{
		{"N 3 for RGB", 3, "RGB ", "", false},
		{"N 1 for GRAY", 1, "GRAY", "", false},
		{"N 4 for CMYK", 4, "CMYK", "", false},
		{"N 3 for Lab", 3, "Lab ", "", false},
		{"N 4 for RGB", 4, "RGB ", "", true},
		{"N 3 for an unknown signature", 3, "YYY ", "", true},
		{"a profile too short to parse", 3, "", "short", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			body := tc.profile
			if body == "" {
				body = icc(tc.colorSpace)
			}
			doc, catalog := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R /OutputIntents [4 0 R] >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
				"<< /Type /OutputIntent /S /GTS_PDFA1 /OutputConditionIdentifier (x) /DestOutputProfile 5 0 R >>",
				fmt.Sprintf("<< /N %d /Length %d >>\nstream\n%s\nendstream", tc.n, len(body), body),
			)

			var got []Violation
			checkOutputIntentICC(ctx, doc, catalog, 1, &got)
			if fired := hasRule(got, Rule_19005_1_6_2_3_2_t02); fired != tc.fires {
				t.Errorf("§6.2.3.2 t2 fired = %v, want %v (%v)", fired, tc.fires, got)
			}
		})
	}
}

// TestOutputIntentProfileComponents_Part1Only pins the scope: parts 2 to 4
// state no component-count requirement, so a mismatch there reports nothing
// rather than borrowing a neighbouring rule.
func TestOutputIntentProfileComponents_Part1Only(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	b := make([]byte, iccHeaderSize)
	b[8] = 2
	copy(b[12:16], "mntr")
	copy(b[16:20], "RGB ")
	body := string(b)

	for _, part := range []int{2, 3, 4} {
		doc, catalog := objsDocPDF(t,
			"<< /Type /Catalog /Pages 2 0 R /OutputIntents [4 0 R] >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
			"<< /Type /OutputIntent /S /GTS_PDFA1 /OutputConditionIdentifier (x) /DestOutputProfile 5 0 R >>",
			fmt.Sprintf("<< /N 4 /Length %d >>\nstream\n%s\nendstream", len(body), body),
		)

		var got []Violation
		checkOutputIntentICC(ctx, doc, catalog, part, &got)
		for _, v := range got {
			if v.Rule.Clause == "6.2.3.2" {
				t.Errorf("part %d must state no component-count rule, got %v", part, v)
			}
		}
	}
}
