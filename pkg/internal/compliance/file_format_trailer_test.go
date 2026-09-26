package compliance

import (
	"bytes"
	"context"
	"encoding/binary"
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

	cases := []trailerCase{
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
	runTrailerCases(t, cases)
}

// trailerCase is one fixture a trailer test validates, with the count of the
// rule veraPDF 1.30 states for it.
type trailerCase struct {
	name    string
	data    []byte
	profile models.PDFAProfile
	rule    Rule
	want    int
}

// runTrailerCases validates each case and compares the count of its rule.
func runTrailerCases(t *testing.T, cases []trailerCase) {
	t.Helper()
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

// xrefStreamTestID is the /ID the cross-reference stream fixtures carry.
const xrefStreamTestID = "/ID [<00112233445566778899AABBCCDDEEFF> <00112233445566778899AABBCCDDEEFF>]"

// xrefStreamRow encodes one cross-reference stream entry under /W [1 4 2].
func xrefStreamRow(kind byte, field2 int, field3 uint16) []byte {
	row := make([]byte, 7)
	row[0] = kind
	binary.BigEndian.PutUint32(row[1:5], uint32(field2))
	binary.BigEndian.PutUint16(row[5:], field3)
	return row
}

// xrefStreamPDF builds a one-page PDF indexed by a cross-reference stream,
// object 5, whose dictionary carries entries after /Type, /W and /Root.
func xrefStreamPDF(entries string) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources << >> /Contents 4 0 R >>",
		exponentStream("", "0 0 m 10 10 l S"),
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n")
	rows := xrefStreamRow(0, 0, 65535)
	for i, o := range objs {
		rows = append(rows, xrefStreamRow(1, buf.Len(), 0)...)
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	rows = append(rows, xrefStreamRow(1, xref, 0)...)
	fmt.Fprintf(&buf, "5 0 obj\n%s\nendobj\nstartxref\n%d\n%%%%EOF\n",
		exponentStream("/Type /XRef /W [1 4 2] /Root 1 0 R"+entries, string(rows)), xref)
	return buf.Bytes()
}

// updatedXRefStreamPDF appends to an [xrefStreamPDF] whose stream carries
// oldEntries an incremental update rewriting its content stream, indexed by a
// cross-reference stream, object 6, whose dictionary carries newEntries.
func updatedXRefStreamPDF(oldEntries, newEntries string) []byte {
	base := xrefStreamPDF(" /Size 6 " + xrefStreamTestID + oldEntries)
	_, tail, _ := bytes.Cut(base[bytes.LastIndex(base, []byte("startxref")):], []byte("\n"))
	prev, _, _ := bytes.Cut(tail, []byte("\n"))
	buf := bytes.NewBuffer(base)
	rows := xrefStreamRow(1, buf.Len(), 0)
	fmt.Fprintf(buf, "4 0 obj\n%s\nendobj\n", exponentStream("", "0 0 m 20 20 l S"))
	xref := buf.Len()
	rows = append(rows, xrefStreamRow(1, xref, 0)...)
	dict := fmt.Sprintf("/Type /XRef /Size 7 /Index [4 1 6 1] /W [1 4 2] /Root 1 0 R /Prev %s %s%s", prev, xrefStreamTestID, newEntries)
	fmt.Fprintf(buf, "6 0 obj\n%s\nendobj\nstartxref\n%d\n%%%%EOF\n", exponentStream(dict, string(rows)), xref)
	return buf.Bytes()
}

// TestXRefStreamTrailerValuesJudgedAsVeraPDFReachesThem pins that the format
// and limit rules judge the trailer entries of a cross-reference stream
// dictionary, those ISO 32000-1, Table 15 defines, once more than the object
// sweep does, and a document information dictionary written directly there
// twice more; see [trailerFormatViolations], [checkNumericLimits] and
// [document.Document.TrailerAsWritten]. Every count was measured with veraPDF
// 1.30.
func TestXRefStreamTrailerValuesJudgedAsVeraPDFReachesThem(t *testing.T) {
	t.Parallel()

	withEntries := func(entries string) []byte {
		return xrefStreamPDF(" /Size 6 " + xrefStreamTestID + entries)
	}

	runTrailerCases(t, []trailerCase{
		{
			"odd hex string in a direct /Info", withEntries(" /Info << /Producer <414> >>"),
			models.PDFA1b, Rule_19005_1_6_1_6_t01, 3,
		},
		{
			"odd hex string in a direct /Info at 2b", withEntries(" /Info << /Producer <414> >>"),
			models.PDFA2b, Rule_19005_2_6_1_6_t01, 3,
		},
		{
			"odd hex string in a direct /Info at 4", withEntries(" /Info << /Producer <414> >>"),
			models.PDFA4, Rule_19005_4_6_1_5_t01, 3,
		},
		{
			"non-hex character in a direct /Info", withEntries(" /Info << /Producer <41G1> >>"),
			models.PDFA2b, Rule_19005_2_6_1_6_t02, 3,
		},
		{
			"odd hex string in /ID", xrefStreamPDF(" /Size 6 /ID [<00112233445566778899AABBCCDDEEF> <00112233445566778899AABBCCDDEEFF>]"),
			models.PDFA2b, Rule_19005_2_6_1_6_t01, 2,
		},
		{
			"odd hex string in /ID at 4", xrefStreamPDF(" /Size 6 /ID [<00112233445566778899AABBCCDDEEF> <00112233445566778899AABBCCDDEEFF>]"),
			models.PDFA4, Rule_19005_4_6_1_5_t01, 2,
		},
		{
			"odd hex string in another entry", withEntries(" /Foo <414>"),
			models.PDFA2b, Rule_19005_2_6_1_6_t01, 1,
		},
		{
			"integer out of range in another entry", withEntries(" /Foo 2147483648"),
			models.PDFA1b, Rule_19005_1_6_1_12_t01, 1,
		},
		{
			"integer out of range in another entry at 2b", withEntries(" /Foo 2147483648"),
			models.PDFA2b, Rule_19005_2_6_1_13_t01, 1,
		},
		{
			"integer out of range in /Size", xrefStreamPDF(" /Size 2147483648 /Index [0 6] " + xrefStreamTestID),
			models.PDFA2b, Rule_19005_2_6_1_13_t01, 2,
		},
		{
			"integer out of range in a direct /Info", withEntries(" /Info << /Foo 2147483648 >>"),
			models.PDFA1b, Rule_19005_1_6_1_12_t01, 3,
		},
		{
			"real out of range in a direct /Info", withEntries(" /Info << /Foo 40000.5 >>"),
			models.PDFA1b, Rule_19005_1_6_1_12_t02, 3,
		},
		{
			"long string in a direct /Info", withEntries(" /Info << /Producer (" + strings.Repeat("x", 70000) + ") >>"),
			models.PDFA2b, Rule_19005_2_6_1_13_t03, 3,
		},
		{
			"long name key in a direct /Info", withEntries(" /Info << /" + strings.Repeat("K", 200) + " (x) >>"),
			models.PDFA1b, Rule_19005_1_6_1_12_t04, 3,
		},
		{
			"long name key in the dictionary", withEntries(" /" + strings.Repeat("K", 200) + " 1"),
			models.PDFA2b, Rule_19005_2_6_1_13_t04, 1,
		},
		// An older section's stream is an indirect object the sweep judges,
		// and nothing judges its trailer entries again.
		{
			"odd hex string in the newest stream's direct /Info", updatedXRefStreamPDF("", " /Info << /Producer <414> >>"),
			models.PDFA2b, Rule_19005_2_6_1_6_t01, 3,
		},
		{
			"odd hex string in an older stream's direct /Info", updatedXRefStreamPDF(" /Info << /Producer <414> >>", ""),
			models.PDFA2b, Rule_19005_2_6_1_6_t01, 1,
		},
		{
			"integer out of range in an older stream's direct /Info", updatedXRefStreamPDF(" /Info << /Foo 2147483648 >>", ""),
			models.PDFA2b, Rule_19005_2_6_1_13_t01, 1,
		},
	})
}
