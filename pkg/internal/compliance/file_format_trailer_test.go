package compliance

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// trailerDictPDF builds a one-page PDF whose cross-reference table ends in the
// trailer dictionary trailer, a format string that receives the /Size.
func trailerDictPDF(trailer string) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources << >> /Contents 4 0 R >>",
		exponentStream("", "0 0 m 10 10 l S"),
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n")
	offs := make([]int, len(objs))
	for i, o := range objs {
		offs[i] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, o := range offs {
		fmt.Fprintf(&buf, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&buf, "trailer\n%s\nstartxref\n%d\n%%%%EOF\n", fmt.Sprintf(trailer, len(objs)+1), xref)
	return buf.Bytes()
}

// updatedTrailerPDF appends to a [trailerDictPDF] whose trailer carries
// oldEntries an incremental update rewriting its content stream, whose trailer
// carries newEntries.
func updatedTrailerPDF(oldEntries, newEntries string) []byte {
	const id = "/ID [<00112233445566778899AABBCCDDEEFF> <00112233445566778899AABBCCDDEEFF>]"
	base := trailerDictPDF("<< /Size %d /Root 1 0 R " + id + oldEntries + " >>")
	_, tail, _ := bytes.Cut(base[bytes.LastIndex(base, []byte("startxref")):], []byte("\n"))
	prev, _, _ := bytes.Cut(tail, []byte("\n"))
	buf := bytes.NewBuffer(base)
	off := buf.Len()
	fmt.Fprintf(buf, "4 0 obj\n%s\nendobj\n", exponentStream("", "0 0 m 20 20 l S"))
	xref := buf.Len()
	fmt.Fprintf(buf, "xref\n0 1\n0000000000 65535 f \n4 1\n%010d 00000 n \n", off)
	fmt.Fprintf(buf, "trailer\n<< /Size 5 /Root 1 0 R /Prev %s %s%s >>\nstartxref\n%d\n%%%%EOF\n", prev, id, newEntries, xref)
	return buf.Bytes()
}

// TestTrailerValuesJudgedAsVeraPDFReachesThem pins that the format and limit
// rules judge the values of the trailer dictionary once, and those of a
// document information dictionary written directly in it twice; see
// [trailerFormatViolations] and [checkNumericLimits]. Every count was measured
// with veraPDF 1.30.
func TestTrailerValuesJudgedAsVeraPDFReachesThem(t *testing.T) {
	t.Parallel()

	const id = "/ID [<00112233445566778899AABBCCDDEEFF> <00112233445566778899AABBCCDDEEFF>]"
	withEntries := func(entries string) []byte {
		return trailerDictPDF("<< /Size %d /Root 1 0 R " + id + entries + " >>")
	}

	cases := []struct {
		name    string
		data    []byte
		profile models.PDFAProfile
		rule    Rule
		want    int
	}{
		{
			"odd hex string in a direct /Info", withEntries(" /Info << /Producer <414> >>"),
			models.PDFA1b, Rule_19005_1_6_1_6_t01, 2,
		},
		{
			"odd hex string in a direct /Info at 2b", withEntries(" /Info << /Producer <414> >>"),
			models.PDFA2b, Rule_19005_2_6_1_6_t01, 2,
		},
		{
			"odd hex string in a direct /Info at 4", withEntries(" /Info << /Producer <414> >>"),
			models.PDFA4, Rule_19005_4_6_1_5_t01, 2,
		},
		{
			"non-hex character in a direct /Info", withEntries(" /Info << /Producer <41G1> >>"),
			models.PDFA2b, Rule_19005_2_6_1_6_t02, 2,
		},
		{
			"odd hex string in an array in a direct /Info", withEntries(" /Info << /Producer [<414>] >>"),
			models.PDFA1b, Rule_19005_1_6_1_6_t01, 2,
		},
		{
			"odd hex string in a dictionary in a direct /Info", withEntries(" /Info << /Foo << /Bar <414> >> >>"),
			models.PDFA2b, Rule_19005_2_6_1_6_t01, 2,
		},
		{
			"odd hex string in another trailer entry", withEntries(" /Foo <414>"),
			models.PDFA1b, Rule_19005_1_6_1_6_t01, 1,
		},
		{
			"non-hex character in another trailer entry", withEntries(" /Foo <41G1>"),
			models.PDFA2b, Rule_19005_2_6_1_6_t02, 1,
		},
		{
			"odd hex string in /ID", trailerDictPDF("<< /Size %d /Root 1 0 R /ID [<00112233445566778899AABBCCDDEEF> <00112233445566778899AABBCCDDEEFF>] >>"),
			models.PDFA2b, Rule_19005_2_6_1_6_t01, 1,
		},
		{
			"non-hex character in /ID", trailerDictPDF("<< /Size %d /Root 1 0 R /ID [<00112233445566778899AABBCCDDEEFG> <00112233445566778899AABBCCDDEEFF>] >>"),
			models.PDFA1b, Rule_19005_1_6_1_6_t02, 1,
		},
		{
			"integer out of range in a direct /Info", withEntries(" /Info << /Foo 2147483648 >>"),
			models.PDFA1b, Rule_19005_1_6_1_12_t01, 2,
		},
		{
			"integer out of range in a direct /Info at 2b", withEntries(" /Info << /Foo 2147483648 >>"),
			models.PDFA2b, Rule_19005_2_6_1_13_t01, 2,
		},
		{
			"real out of range in a direct /Info", withEntries(" /Info << /Foo 40000.5 >>"),
			models.PDFA1b, Rule_19005_1_6_1_12_t02, 2,
		},
		{
			"long string in a direct /Info", withEntries(" /Info << /Producer (" + strings.Repeat("x", 70000) + ") >>"),
			models.PDFA2b, Rule_19005_2_6_1_13_t03, 2,
		},
		{
			"long name key in a direct /Info", withEntries(" /Info << /" + strings.Repeat("K", 200) + " (x) >>"),
			models.PDFA1b, Rule_19005_1_6_1_12_t04, 2,
		},
		{
			"long name key in the trailer", withEntries(" /" + strings.Repeat("K", 200) + " 1"),
			models.PDFA2b, Rule_19005_2_6_1_13_t04, 1,
		},
		// Only the trailer of the section startxref designates counts, and
		// nothing an older one holds, carried forward or not.
		{
			"odd hex string in the newest trailer", updatedTrailerPDF("", " /Foo <414>"),
			models.PDFA2b, Rule_19005_2_6_1_6_t01, 1,
		},
		{
			"odd hex string in an older trailer", updatedTrailerPDF(" /Foo <414>", ""),
			models.PDFA2b, Rule_19005_2_6_1_6_t01, 0,
		},
		{
			"odd hex string in an older trailer's direct /Info", updatedTrailerPDF(" /Info << /Producer <414> >>", ""),
			models.PDFA2b, Rule_19005_2_6_1_6_t01, 0,
		},
		{
			"integer out of range in an older trailer's direct /Info", updatedTrailerPDF(" /Info << /Foo 2147483648 >>", ""),
			models.PDFA2b, Rule_19005_2_6_1_13_t01, 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, err := document.NewDocument(context.Background(), bytes.NewReader(tc.data), int64(len(tc.data)), nil)
			if err != nil {
				t.Fatalf("NewDocument: %v", err)
			}
			viols, err := ValidatePDFA(context.Background(), doc, tc.profile)
			if err != nil {
				t.Fatalf("ValidatePDFA: %v", err)
			}
			if got := countRuleErrs(t, viols, tc.rule); got != tc.want {
				t.Errorf("%s test %d fired %d times, want %d", tc.rule.Clause, tc.rule.TestNumber, got, tc.want)
			}
		})
	}
}
