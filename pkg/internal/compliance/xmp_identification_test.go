package compliance

import (
	"context"
	"testing"
)

func xmpPacket(description string) []byte {
	return []byte(`<x:xmpmeta xmlns:x="adobe:ns:meta/">
  <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
    ` + description + `
  </rdf:RDF>
</x:xmpmeta>`)
}

// TestXMPIdentification_ForeignPartNotFlagged guards the FP class where a
// local name like "part" under a different namespace URI (pdfuaid:part on a
// dual PDF/A + PDF/UA file) was reported as a pdfaid wrong-prefix violation.
// XMP properties are identified by namespace URI, not local name, so a "part"
// bound to the PDF/UA URI is a distinct property and never aliases pdfaid:part.
func TestXMPIdentification_ForeignPartNotFlagged(t *testing.T) {
	data := xmpPacket(`<rdf:Description rdf:about=""
      xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/"
      xmlns:pdfuaid="http://www.aiim.org/pdfua/ns/id/"
      pdfaid:part="2" pdfaid:conformance="B" pdfuaid:part="1"/>`)

	var results []Violation
	xmpCheckPDFAIdentification(context.Background(), data, "PDF/A-2b", 2, &results)
	if len(results) != 0 {
		t.Errorf("dual PDF/A+UA identification must not be flagged, got %v", results)
	}
}

// TestXMPIdentification_WrongPrefixStillFlagged pins the true positive: the
// pdfaid namespace URI serialized under a prefix other than "pdfaid".
func TestXMPIdentification_WrongPrefixStillFlagged(t *testing.T) {
	data := xmpPacket(`<rdf:Description rdf:about=""
      xmlns:pdfa="http://www.aiim.org/pdfa/ns/id/"
      pdfa:part="2" pdfa:conformance="B"/>`)

	var results []Violation
	xmpCheckPDFAIdentification(context.Background(), data, "PDF/A-2b", 2, &results)
	if len(results) == 0 {
		t.Error("pdfaid properties under a non-pdfaid prefix must be flagged")
	}
}

// TestXMPIdentification_WrongURIIsMissing pins the wrong-URI case: text that
// looks like pdfaid:part but is bound to a foreign URI is a missing
// identification schema in well-formed XMP, not a silent pass. The packet
// parses cleanly, so the pdfaid namespace URI is genuinely absent.
func TestXMPIdentification_WrongURIIsMissing(t *testing.T) {
	data := xmpPacket(`<rdf:Description rdf:about=""
      xmlns:pdfaid="http://example.com/not-pdfaid/"
      pdfaid:part="2" pdfaid:conformance="B"/>`)

	var results []Violation
	xmpCheckPDFAIdentification(context.Background(), data, "PDF/A-2b", 2, &results)
	if len(results) == 0 {
		t.Error("pdfaid under a wrong namespace URI must be reported as missing identification")
	}
}

// TestXMPIdentification_UnparseableReportsMissing pins the vera-aligned
// behavior for a packet that is not well-formed XML. The pdfaid schema here is
// present and valid (pdfaid:part="4", pdfaid:rev="2020"), but a case-mismatched
// tag (<dc:creator> closed by </dc:Creator>) aborts the XML parse before the
// pdfaid Description is reached. veraPDF models this as
// MainXMPPackage.Identification_size == 0 and reports the missing-identification
// rule (6.7.3 t01), never the field-level rev rule (t05), which is scoped to a
// successfully extracted PDFAIdentification object. litho must do the same:
// report t01, never claim "rev missing" for a rev litho simply could not parse.
//
// # Corpus evidence
//
//   - verapdf 6-7-2-1-t01-fail-d: this is its exact shape.
func TestXMPIdentification_UnparseableReportsMissing(t *testing.T) {
	data := []byte(`<x:xmpmeta xmlns:x="adobe:ns:meta/">
  <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
    <rdf:Description xmlns:dc="http://purl.org/dc/elements/1.1/" rdf:about="">
      <dc:creator><rdf:Seq><rdf:li>litho</rdf:li></rdf:Seq></dc:Creator>
    </rdf:Description>
    <rdf:Description xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/" rdf:about=""
      pdfaid:part="4" pdfaid:rev="2020"/>
  </rdf:RDF>
</x:xmpmeta>`)

	var results []Violation
	xmpCheckPDFAIdentification(context.Background(), data, "PDF/A-4", 4, &results)

	var sawMissing, sawRev bool
	for _, v := range results {
		switch v.Rule {
		case Rule_19005_4_6_7_3_t01:
			sawMissing = true
		case Rule_19005_4_6_7_3_t05:
			sawRev = true
		}
	}
	if !sawMissing {
		t.Errorf("unparseable packet must report missing identification (6.7.3 t01), got %v", results)
	}
	if sawRev {
		t.Errorf("must not report rev missing (6.7.3 t05) for an unparseable packet, got %v", results)
	}
}
