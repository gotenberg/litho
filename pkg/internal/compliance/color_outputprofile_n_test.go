package compliance

import (
	"context"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// iccHeaderOnly returns a 132-byte ICC profile of device class cls and colour
// space cs: a version 2.1 header and an empty tag table.
func iccHeaderOnly(cls, cs string) string {
	b := make([]byte, 132)
	binary.BigEndian.PutUint32(b[0:], 132)
	b[8], b[9] = 2, 0x10
	copy(b[12:], cls)
	copy(b[16:], cs)
	copy(b[20:], "XYZ ")
	copy(b[36:], "acsp")
	return string(b)
}

// TestOutputProfileNMatchesColorSpace pins ISO 19005-1, 6.2.3.2 t2 on an output
// intent's /DestOutputProfile, which veraPDF judges as it judges an ICCBased
// colour space: /N must equal the component count of the colour space the
// profile header names (three for Lab), and an empty profile names none.
// PDF/A-1 alone states the rule. Every case was measured against veraPDF.
func TestOutputProfileNMatchesColorSpace(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name    string
		n       int
		profile string
		want    int
	}{
		{"an RGB profile declared /N 1", 1, iccHeaderOnly("mntr", "RGB "), 1},
		{"an RGB profile declared /N 3", 3, iccHeaderOnly("mntr", "RGB "), 0},
		{"an RGB profile declared /N 4", 4, iccHeaderOnly("mntr", "RGB "), 1},
		{"a Lab profile declared /N 3", 3, iccHeaderOnly("abst", "Lab "), 0},
		{"an empty profile declared /N 3", 3, "", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R /OutputIntents [<< /Type /OutputIntent /S /GTS_PDFA1"+
					" /OutputConditionIdentifier (x) /DestOutputProfile 4 0 R >>] >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
				fmt.Sprintf("<< /N %d /Length %d >>\nstream\n%s\nendstream", tt.n, len(tt.profile), tt.profile),
			)
			viols, err := ValidatePDFA(ctx, doc, models.PDFA1b)
			if err != nil {
				t.Fatalf("ValidatePDFA: %v", err)
			}
			if got := countRuleErrs(t, viols, Rule_19005_1_6_2_3_2_t02); got != tt.want {
				t.Errorf("6.2.3.2 t2 fired %d times, want %d", got, tt.want)
			}
		})
	}

	t.Run("an ICCBased Lab colour space declared /N 3", func(t *testing.T) {
		t.Parallel()
		lab := iccHeaderOnly("abst", "Lab ")
		body := "/CS0 cs 0.5 0.5 0.5 sc 0 0 10 10 re f"
		doc, _ := objsDocPDF(t,
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R /Resources << /ColorSpace << /CS0 [/ICCBased 5 0 R] >> >> >>",
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(body), body),
			fmt.Sprintf("<< /N 3 /Length %d >>\nstream\n%s\nendstream", len(lab), lab),
		)
		viols, err := ValidatePDFA(ctx, doc, models.PDFA1b)
		if err != nil {
			t.Fatalf("ValidatePDFA: %v", err)
		}
		if got := countRuleErrs(t, viols, Rule_19005_1_6_2_3_2_t02); got != 0 {
			t.Errorf("6.2.3.2 t2 fired %d times, want 0", got)
		}
	})
}
