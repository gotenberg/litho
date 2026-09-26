package compliance

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// pagePacketPDF builds a two-page document with a clean catalog packet (obj 5)
// and a page-level /Metadata packet (obj 6) carrying the given <?xpacket?>
// header attributes and body. The second page holds the defective packet.
func pagePacketPDF(t *testing.T, headerAttrs, body string) (*document.Document, ast.PdfDict) {
	t.Helper()
	catalogXMP := "<?xpacket begin='' id='W5M0MpCehiHzreSzNTczkc9d'?>" +
		`<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` +
		`<rdf:Description rdf:about=""/></rdf:RDF></x:xmpmeta><?xpacket end='r'?>`
	pageXMP := fmt.Sprintf("<?xpacket begin='' id='W5M0MpCehiHzreSzNTczkc9d'%s?>%s", headerAttrs, body)

	return objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R /Metadata 5 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Metadata 6 0 R >>",
		fmt.Sprintf("<< /Type /Metadata /Subtype /XML /Length %d >>\nstream\n%s\nendstream", len(catalogXMP), catalogXMP),
		fmt.Sprintf("<< /Type /Metadata /Subtype /XML /Length %d >>\nstream\n%s\nendstream", len(pageXMP), pageXMP),
	)
}

// TestXMPPacketScope_PageLevelMetadata pins that the XMP packet tests cover
// every metadata stream in the file, not only the catalog's. ISO 19005-4
// §6.7.2.1 and ISO 19005-2/3 §6.6.2.1 govern "all content of all XMP packets
// located in any metadata stream present in the PDF", and veraPDF reports a
// page-level defect against those clauses. litho checked the catalog packet
// alone and reported nothing.
//
// ISO 19005-1 carries no such scope, so the same defect stays silent at
// PDF/A-1b: veraPDF builds an XMPPackage for a non-catalog metadata stream
// only outside part 1, and firing there would be a false positive.
func TestXMPPacketScope_PageLevelMetadata(t *testing.T) {
	t.Parallel()

	wellFormed := `<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` +
		`<rdf:Description rdf:about=""/></rdf:RDF></x:xmpmeta><?xpacket end='r'?>`
	malformed := `<x:xmpmeta xmlns:x="adobe:ns:meta/"><dc:creator xmlns:dc="d">x</dc:Creator></x:xmpmeta>`

	tests := []struct {
		name        string
		headerAttrs string
		body        string
		part4       Rule
		part2       Rule
	}{
		{"bytes attribute", ` bytes="1024"`, wellFormed, Rule_19005_4_6_7_2_1_t02, Rule_19005_2_6_6_2_1_t02},
		{"encoding attribute", ` encoding="UTF-8"`, wellFormed, Rule_19005_4_6_7_2_1_t03, Rule_19005_2_6_6_2_1_t03},
		{"not well-formed", "", malformed, Rule_19005_4_6_7_2_1_t04, Rule_19005_2_6_6_2_1_t04},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			doc, catalog := pagePacketPDF(t, tc.headerAttrs, tc.body)

			part4 := checkXMPGroup(ctx, &validationCtx{doc: doc, catalog: catalog, profile: "PDF/A-4", part: 4})
			if !hasRule(part4, tc.part4) {
				t.Errorf("PDF/A-4: page packet defect not reported, got %v", part4)
			}

			part2 := checkXMPGroup(ctx, &validationCtx{doc: doc, catalog: catalog, profile: "PDF/A-2b", part: 2})
			if !hasRule(part2, tc.part2) {
				t.Errorf("PDF/A-2b: page packet defect not reported, got %v", part2)
			}

			part1 := checkXMPGroup(ctx, &validationCtx{doc: doc, catalog: catalog, profile: "PDF/A-1b", part: 1})
			for _, r := range []Rule{Rule_19005_1_6_7_5_t01, Rule_19005_1_6_7_5_t02, Rule_19005_1_6_7_9_t01} {
				if hasRule(part1, r) {
					t.Errorf("PDF/A-1b: page packet is out of scope, got %v", part1)
				}
			}
		})
	}
}

// TestXMPPacketScope_SharedStreamReportedOnce pins that a metadata stream
// referenced by several objects yields one violation, not one per reference.
func TestXMPPacketScope_SharedStreamReportedOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	pageXMP := `<?xpacket begin='' id='W5M0MpCehiHzreSzNTczkc9d' bytes="1024"?>` +
		`<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` +
		`<rdf:Description rdf:about=""/></rdf:RDF></x:xmpmeta><?xpacket end='r'?>`

	doc, catalog := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 /Metadata 5 0 R >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Metadata 5 0 R >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Metadata 5 0 R >>",
		fmt.Sprintf("<< /Type /Metadata /Subtype /XML /Length %d >>\nstream\n%s\nendstream", len(pageXMP), pageXMP),
	)

	results := checkXMPGroup(ctx, &validationCtx{doc: doc, catalog: catalog, profile: "PDF/A-4", part: 4})
	count := 0
	for _, v := range results {
		if v.Rule == Rule_19005_4_6_7_2_1_t02 {
			count++
		}
	}
	if count != 1 {
		t.Errorf("shared metadata stream reported %d times, want 1: %v", count, results)
	}
}

// packetScanPDF returns the bytes of a document whose second page carries a
// defective XMP packet (obj 6), the input the repeat-run check validates
// twice.
func packetScanPDF() []byte {
	catalogXMP := "<?xpacket begin='' id='W5M0MpCehiHzreSzNTczkc9d'?>" +
		`<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` +
		`<rdf:Description rdf:about=""/></rdf:RDF></x:xmpmeta><?xpacket end='r'?>`
	pageXMP := `<?xpacket begin='' id='W5M0MpCehiHzreSzNTczkc9d' bytes="1024"?>` +
		`<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` +
		`<rdf:Description rdf:about=""/></rdf:RDF></x:xmpmeta><?xpacket end='r'?>`

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /Metadata 5 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Metadata 6 0 R >>",
		fmt.Sprintf("<< /Type /Metadata /Subtype /XML /Length %d >>\nstream\n%s\nendstream", len(catalogXMP), catalogXMP),
		fmt.Sprintf("<< /Type /Metadata /Subtype /XML /Length %d >>\nstream\n%s\nendstream", len(pageXMP), pageXMP),
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

// TestXMPPacketScope_RepeatRunsAreIdentical pins that two consecutive
// validation passes on one [document.Document] report the same violations.
// The metadata scan resolves every indirect object, which warms and re-reads
// the resolver cache and can append parser-level findings that
// [document.Document.ResetFormatViolations] drains at the start of the next
// run. A scan that reported a packet once per pass, or that leaked findings
// across the drain boundary, would make the second pass differ.
func TestXMPPacketScope_RepeatRunsAreIdentical(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	data := packetScanPDF()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	render := func(errs []error) []string {
		out := make([]string, 0, len(errs))
		for _, e := range errs {
			out = append(out, e.Error())
		}
		slices.Sort(out)
		return out
	}

	first, err := ValidatePDFA(ctx, doc, models.PDFA4)
	if err != nil {
		t.Fatalf("first pass: %v", err)
	}
	second, err := ValidatePDFA(ctx, doc, models.PDFA4)
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}

	if !hasRule(toViolations(t, first), Rule_19005_4_6_7_2_1_t02) {
		t.Fatalf("fixture no longer exercises the page packet scan: %v", render(first))
	}
	if !slices.Equal(render(first), render(second)) {
		t.Errorf("consecutive runs differ:\nfirst:  %v\nsecond: %v", render(first), render(second))
	}
}

// toViolations converts the error slice a validation pass returns back into
// [Violation] values so rule-level assertions can run on it.
func toViolations(t *testing.T, errs []error) []Violation {
	t.Helper()
	out := make([]Violation, 0, len(errs))
	for _, e := range errs {
		v, ok := e.(Violation)
		if !ok {
			t.Fatalf("validation returned %T, want Violation", e)
		}
		out = append(out, v)
	}
	return out
}
