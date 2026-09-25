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

	got := checkContentStreamFormat(ctx, doc, catalog, "PDF/A-1b")

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

// TestContentStreamExponentOperands pins the rule a number written with an
// exponent answers to in a page content stream. Where an operand stands, the
// token is no number (ISO 32000-1, 7.3.3), so it reads as an operator ISO
// 32000-1 does not define: ISO 19005-1, 6.2.10 t1 and ISO 19005-2, 6.2.2 t1,
// once per token. Inside an array, a dictionary or an inline image dictionary it
// is a syntax error no rule states. Measured with veraPDF 1.30 on one-page
// fixtures: "1.5e1 w", "1.5E-1 w" and "1e1 w" each fail the operator test once,
// and it gives up on a stream holding the nested forms, reporting nothing from
// it. The hex string rules, where the exponent used to land, stay silent.
func TestContentStreamExponentOperands(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	content := "1.5e1 w [2.5e1 3] 0 d /P << /MCID 1.5e1 >> BDC EMC " +
		"q BI /W 1 /H 1 /BPC 8 /CS /G /Foo 1.5e1 ID \x80 EI Q 1E1 w 0 0 m 10 10 l S"
	doc, catalog := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >>",
		"<< /Length "+strconv.Itoa(len(content))+" >>\nstream\n"+content+"\nendstream",
	)

	for _, tc := range []struct {
		profile string
		want    Rule
	}{
		{"PDF/A-1b", Rule_19005_1_6_2_10_t01},
		{"PDF/A-2b", Rule_19005_2_6_2_2_t01},
	} {
		got := checkContentStreamFormat(ctx, doc, catalog, tc.profile)
		if len(got) != 2 {
			t.Fatalf("%s: got %d findings, want 2 (the two unnested exponents): %v", tc.profile, len(got), got)
		}
		for _, v := range got {
			if v.Rule != tc.want {
				t.Errorf("%s: got rule %s test %d, want %s test %d",
					tc.profile, v.Rule.Clause, v.Rule.TestNumber, tc.want.Clause, tc.want.TestNumber)
			}
		}
	}
}

// TestContentStreamHexStringsOnlyWhereRead pins which strings of a content
// stream answer to the hex string tests, ISO 19005-2, 6.1.6 t1: those veraPDF
// reads through an operator, never the operands of an operator ISO 32000-1
// does not define, an operand an operator does not read as text or as a
// dictionary, or operands no operator follows. Measured with veraPDF 1.30 at
// PDF/A-2b, each content on a one-page fixture.
func TestContentStreamHexStringsOnlyWhereRead(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	for _, tc := range []struct {
		content string
		want    int
	}{
		{"BT <414> Tj ET", 1},
		{"BT <434> <414> Tj ET", 1},
		{"BT [<414> 5 [<434>]] TJ ET", 2},
		{"BT <414> ' ET", 1},
		{"BT 0 0 <414> \" ET", 1},
		{"/Span << /E <414> >> BDC EMC", 1},
		{"/Span << /X [<414>] >> BDC EMC", 1},
		{"/Foo << /X << /Y <414> >> >> DP", 1},
		{"q BI /W 1 /H 1 /BPC 8 /CS /G /Foo <414> ID \x80 EI Q", 1},
		{"<414> <434> zz", 0},
		{"<414> zz <434> Tj", 1},
		{"[<414>] zz", 0},
		{"<414> w", 0},
		{"<414> Do", 0},
		{"<414> BMC EMC", 0},
		{"BT <414> 12 Tf ET", 0},
		{"BT <414> 5 Tj ET", 0},
		{"/Span <414> BDC EMC", 0},
		{"0 0 m 10 10 l S <414>", 0},
	} {
		t.Run(tc.content, func(t *testing.T) {
			t.Parallel()
			doc, catalog := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >>",
				"<< /Length "+strconv.Itoa(len(tc.content))+" >>\nstream\n"+tc.content+"\nendstream",
			)
			got := 0
			for _, v := range checkContentStreamFormat(ctx, doc, catalog, "PDF/A-2b") {
				if v.Rule == Rule_19005_2_6_1_6_t01 {
					got++
				}
			}
			if got != tc.want {
				t.Errorf("t1 fired %d times, want %d", got, tc.want)
			}
		})
	}
}
