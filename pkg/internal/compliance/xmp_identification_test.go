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
// identification schema in well-formed XMP, not a silent pass. The byte-scan
// fallback only applies to unparseable packets.
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

// TestXMPIdentification_UnparseableFallbackStillWorks keeps the byte-scan
// fallback alive for truncated packets where the XML parser gives up early.
func TestXMPIdentification_UnparseableFallbackStillWorks(t *testing.T) {
	data := []byte(`<x:xmpmeta xmlns:x="adobe:ns:meta/">
  <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
    <rdf:Description rdf:about="" pdfaid:part="2" pdfaid:conformance="B" <<broken`)

	var results []Violation
	xmpCheckPDFAIdentification(context.Background(), data, "PDF/A-2b", 2, &results)
	// The fallback must detect pdfaid presence (no missing-schema verdict
	// based on parse failure alone). Whatever else is reported, the
	// missing-identification rule must not fire.
	for _, v := range results {
		if v.Rule == Rule_19005_2_6_6_4_t01 {
			t.Errorf("byte-scan fallback should have detected pdfaid presence, got %v", results)
		}
	}
}
