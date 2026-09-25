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

// reachedTwicePDF builds a one-page PDF whose catalog carries catalogExtra,
// whose page carries pageExtra and whose trailer carries trailerExtra. Object
// 5 is target, the dictionary or stream under test.
func reachedTwicePDF(catalogExtra, pageExtra, trailerExtra, target string) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R" + catalogExtra + " >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources << >> /Contents 4 0 R" + pageExtra + " >>",
		exponentStream("", "0 0 m 10 10 l S"),
		target,
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")
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
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R /ID [<00112233445566778899AABBCCDDEEFF> <00112233445566778899AABBCCDDEEFF>]%s >>\nstartxref\n%d\n%%%%EOF\n",
		len(objs)+1, trailerExtra, xref)
	return buf.Bytes()
}

// TestCOSRulesJudgeInfoAndMetadataTwice pins that the file-structure rules
// judge the trailer's /Info dictionary and every XMP metadata stream twice;
// see [cosReachedTwice]. Every count was measured with veraPDF 1.30.
func TestCOSRulesJudgeInfoAndMetadataTwice(t *testing.T) {
	t.Parallel()

	const xmp = `<?xpacket begin="` + "\xef\xbb\xbf" + `" id="W5M0MpCehiHzreSzNTczkc9d"?>` +
		`<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` +
		`<rdf:Description rdf:about="" xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/">` +
		`<pdfaid:part>2</pdfaid:part><pdfaid:conformance>B</pdfaid:conformance></rdf:Description>` +
		`</rdf:RDF></x:xmpmeta><?xpacket end="w"?>`
	// metadata is a metadata stream whose /Length falls three bytes short of
	// its data, or which carries dict besides.
	metadata := func(dict string, short int) string {
		return fmt.Sprintf("<< %s /Length %d >>\nstream\n%s\nendstream", dict, len(xmp)-short, xmp)
	}
	const metaDict = "/Type /Metadata /Subtype /XML"

	cases := []struct {
		name    string
		data    []byte
		profile models.PDFAProfile
		rule    Rule
		want    int
	}{
		{"odd hex string in /Info", reachedTwicePDF("", "", " /Info 5 0 R", "<< /Producer <414> >>"),
			models.PDFA1b, Rule_19005_1_6_1_6_t01, 2},
		{"odd hex string nested in /Info", reachedTwicePDF("", "", " /Info 5 0 R", "<< /Producer [<414>] >>"),
			models.PDFA2b, Rule_19005_2_6_1_6_t01, 2},
		{"integer out of range in /Info", reachedTwicePDF("", "", " /Info 5 0 R", "<< /Foo 2147483648 >>"),
			models.PDFA1b, Rule_19005_1_6_1_12_t01, 2},
		{"long name key in /Info", reachedTwicePDF("", "", " /Info 5 0 R", "<< /"+strings.Repeat("K", 200)+" (x) >>"),
			models.PDFA2b, Rule_19005_2_6_1_13_t04, 2},
		{"catalog metadata with a short /Length", reachedTwicePDF(" /Metadata 5 0 R", "", "", metadata(metaDict, 3)),
			models.PDFA1b, Rule_19005_1_6_1_7_t01, 2},
		{"catalog metadata with a short /Length at 2b", reachedTwicePDF(" /Metadata 5 0 R", "", "", metadata(metaDict, 3)),
			models.PDFA2b, Rule_19005_2_6_1_7_1_t01, 2},
		{"page metadata with a short /Length", reachedTwicePDF("", " /Metadata 5 0 R", "", metadata(metaDict, 3)),
			models.PDFA2b, Rule_19005_2_6_1_7_1_t01, 2},
		{"catalog metadata with /F", reachedTwicePDF(" /Metadata 5 0 R", "", "", metadata(metaDict+" /F (x.xml)", 0)),
			models.PDFA1b, Rule_19005_1_6_1_7_t03, 2},
		{"catalog metadata holding a long name", reachedTwicePDF(" /Metadata 5 0 R", "", "", metadata(metaDict+" /Foo /"+strings.Repeat("V", 200), 0)),
			models.PDFA1b, Rule_19005_1_6_1_12_t04, 2},
		{"catalog metadata with no /Type", reachedTwicePDF(" /Metadata 5 0 R", "", "", metadata("/Subtype /XML", 3)),
			models.PDFA1b, Rule_19005_1_6_1_7_t01, 1},
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
