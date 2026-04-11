package remediation

import (
	"strings"
	"testing"
)

const pdfaidNS = "http://www.aiim.org/pdfa/ns/id/"

func TestFindXMPProperty_ChildElementForm(t *testing.T) {
	xmp := []byte(`<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
  <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
    <rdf:Description rdf:about="" xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/">
      <pdfaid:part>4</pdfaid:part>
      <pdfaid:conformance>B</pdfaid:conformance>
    </rdf:Description>
  </rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`)

	loc := findXMPProperty(xmp, pdfaidNS, "conformance")
	if loc.Form != xmpPropertyAsElement {
		t.Fatalf("form: got %v, want element", loc)
	}
	removed := removeXMPProperty(xmp, loc)
	removedStr := string(removed)
	if strings.Contains(removedStr, "pdfaid:conformance") {
		t.Errorf("conformance not removed:\n%s", removedStr)
	}
	if !strings.Contains(removedStr, "<pdfaid:part>4</pdfaid:part>") {
		t.Errorf("sibling element lost:\n%s", removedStr)
	}
}

func TestFindXMPProperty_AttributeForm(t *testing.T) {
	// pdfaid:conformance is on the rdf:Description start tag rather than
	// as a child element. The old string-based patcher bailed out with
	// "too complex to patch safely" on this shape.
	xmp := []byte(`<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
  <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
    <rdf:Description rdf:about="" xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/" pdfaid:part="4" pdfaid:conformance="B"/>
  </rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`)

	loc := findXMPProperty(xmp, pdfaidNS, "conformance")
	if loc.Form != xmpPropertyAsAttribute {
		t.Fatalf("form: got %v, want attribute", loc)
	}
	removed := removeXMPProperty(xmp, loc)
	removedStr := string(removed)
	if strings.Contains(removedStr, "pdfaid:conformance") {
		t.Errorf("conformance attribute not removed:\n%s", removedStr)
	}
	if !strings.Contains(removedStr, `pdfaid:part="4"`) {
		t.Errorf("sibling attribute lost:\n%s", removedStr)
	}
	// After removal, the document must still be valid XML: find the
	// conformance attribute again — should be gone, and re-parsing the
	// result should succeed (exercised by running findXMPProperty on
	// it — a parse error would return not-found).
	if loc2 := findXMPProperty(removed, pdfaidNS, "conformance"); loc2.Form != xmpPropertyNotFound {
		t.Errorf("conformance still findable after removal: %v", loc2)
	}
}

func TestFindXMPProperty_NotFound(t *testing.T) {
	xmp := []byte(`<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
  <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
    <rdf:Description rdf:about="" xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/">
      <pdfaid:part>4</pdfaid:part>
    </rdf:Description>
  </rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`)

	loc := findXMPProperty(xmp, pdfaidNS, "conformance")
	if loc.Form != xmpPropertyNotFound {
		t.Errorf("expected not-found, got %v", loc)
	}
}

func TestFindXMPProperty_DifferentNamespacePrefix(t *testing.T) {
	// Same namespace URI, different prefix alias. The splicer must
	// resolve by namespace, not by textual prefix.
	xmp := []byte(`<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
  <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
    <rdf:Description rdf:about="" xmlns:aid="http://www.aiim.org/pdfa/ns/id/">
      <aid:part>4</aid:part>
      <aid:conformance>B</aid:conformance>
    </rdf:Description>
  </rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`)

	loc := findXMPProperty(xmp, pdfaidNS, "conformance")
	if loc.Form != xmpPropertyAsElement {
		t.Fatalf("form: got %v, want element with alias prefix", loc)
	}
	removed := removeXMPProperty(xmp, loc)
	if strings.Contains(string(removed), "aid:conformance") {
		t.Errorf("conformance not removed:\n%s", removed)
	}
}

func TestFindXMPProperty_MalformedReturnsNotFound(t *testing.T) {
	// Garbage input must not panic and must be reported as not-found.
	loc := findXMPProperty([]byte("not really xml <<<<"), pdfaidNS, "conformance")
	if loc.Form != xmpPropertyNotFound {
		t.Errorf("malformed input: got %v, want not-found", loc)
	}
}

func TestRemoveXMPProperty_NoOpWhenNotFound(t *testing.T) {
	xmp := []byte("<x>hello</x>")
	removed := removeXMPProperty(xmp, xmpPropertyLocation{Form: xmpPropertyNotFound})
	if string(removed) != string(xmp) {
		t.Errorf("unexpected mutation: got %q, want %q", removed, xmp)
	}
}
