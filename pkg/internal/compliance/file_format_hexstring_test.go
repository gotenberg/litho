package compliance

import (
	"context"
	"strconv"
	"testing"
)

// TestContentStreamHexStringsReportsEveryFinding pins that a page contributes
// every hex-string finding it carries, not just its first.
//
// ISO 19005-1, 6.1.6 splits into two independent tests: t1 an odd digit count,
// t2 a character outside 0-9/A-F/a-f. veraPDF keys both on the CosString, so a
// page holding two of each answers four times. Stopping at the page's first
// finding reported t1 once and lost t2 altogether, since the odd-digit string
// came first in the stream.
func TestContentStreamHexStringsReportsEveryFinding(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Two odd-digit strings, then two carrying a non-hex character.
	content := "BT /F1 12 Tf 50 700 Td <41424> Tj <43444> Tj <4142ZZ> Tj <4344ZZ> Tj ET"
	doc, catalog := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >>",
		"<< /Length "+strconv.Itoa(len(content))+" >>\nstream\n"+content+"\nendstream",
	)

	got := checkContentStreamHexStrings(ctx, doc, catalog, "PDF/A-1b")

	var odd, invalid int
	for _, v := range got {
		switch v.Rule {
		case Rule_19005_1_6_1_6_t01:
			odd++
		case Rule_19005_1_6_1_6_t02:
			invalid++
		default:
			t.Errorf("unexpected rule %s test %d", v.Rule.Clause, v.Rule.TestNumber)
		}
	}
	if odd != 2 {
		t.Errorf("odd-digit findings = %d, want 2", odd)
	}
	if invalid != 2 {
		t.Errorf("invalid-character findings = %d, want 2", invalid)
	}
}
