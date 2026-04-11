package remediation

import (
	"bytes"
	"strings"
	"testing"
)

// pdfaidNS is defined in fixer_xmp.go. The test file reuses it.

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

func TestFindXMPProperty_DepthBound(t *testing.T) {
	// Build an XMP packet whose rdf:Description contains a deeply
	// nested element tree. The splicer must bail out rather than
	// walking all the way down.
	var b strings.Builder
	b.WriteString(`<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
  <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
    <rdf:Description rdf:about="" xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/">
`)
	const depth = maxXMPDepth + 50
	for range depth {
		b.WriteString("<x:n>")
	}
	for range depth {
		b.WriteString("</x:n>")
	}
	b.WriteString(`
    </rdf:Description>
  </rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`)

	loc := findXMPProperty([]byte(b.String()), pdfaidNS, "conformance")
	if loc.Form != xmpPropertyNotFound {
		t.Errorf("expected not-found (depth exceeded), got %v", loc)
	}
}

func TestRemoveXMPProperty_NoOpWhenNotFound(t *testing.T) {
	xmp := []byte("<x>hello</x>")
	removed := removeXMPProperty(xmp, xmpPropertyLocation{Form: xmpPropertyNotFound})
	if string(removed) != string(xmp) {
		t.Errorf("unexpected mutation: got %q, want %q", removed, xmp)
	}
}

// --- insertChildElement ---

func TestInsertChildElement_IntoDescription(t *testing.T) {
	xmp := []byte(`<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
  <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
    <rdf:Description rdf:about="" xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/">
      <pdfaid:part>4</pdfaid:part>
    </rdf:Description>
  </rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`)

	out, err := insertChildElement(xmp, `<pdfaid:conformance>E</pdfaid:conformance>`)
	if err != nil {
		t.Fatalf("insertChildElement: %v", err)
	}
	if !strings.Contains(string(out), "<pdfaid:conformance>E</pdfaid:conformance>") {
		t.Errorf("injection missing:\n%s", out)
	}
	// The original pdfaid:part must still be present.
	if !strings.Contains(string(out), "<pdfaid:part>4</pdfaid:part>") {
		t.Errorf("sibling element lost:\n%s", out)
	}
	// Result must still parse via findXMPProperty.
	loc := findXMPProperty(out, pdfaidNS, "conformance")
	if loc.Form != xmpPropertyAsElement {
		t.Errorf("injected property not found: %v", loc)
	}
}

func TestInsertChildElement_NoDescription(t *testing.T) {
	xmp := []byte(`<root><notadescription/></root>`)
	_, err := insertChildElement(xmp, `<x:foo/>`)
	if err == nil {
		t.Error("expected error when no rdf:Description is present")
	}
}

// --- replacePropertyValue ---

func TestReplacePropertyValue_ElementForm(t *testing.T) {
	xmp := []byte(`<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
  <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
    <rdf:Description rdf:about="" xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/">
      <pdfaid:part>2</pdfaid:part>
    </rdf:Description>
  </rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`)

	out, err := replacePropertyValue(xmp, pdfaidNS, "part", "4")
	if err != nil {
		t.Fatalf("replacePropertyValue: %v", err)
	}
	if !strings.Contains(string(out), "<pdfaid:part>4</pdfaid:part>") {
		t.Errorf("value not replaced:\n%s", out)
	}
	if strings.Contains(string(out), "<pdfaid:part>2</pdfaid:part>") {
		t.Errorf("old value not removed:\n%s", out)
	}
}

func TestReplacePropertyValue_NotFound(t *testing.T) {
	xmp := []byte(`<?xpacket begin=""?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
  <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
    <rdf:Description rdf:about=""/>
  </rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`)
	_, err := replacePropertyValue(xmp, pdfaidNS, "part", "4")
	if err == nil {
		t.Error("expected error when property is not present")
	}
}

// --- ensureNamespaceDecl ---

func TestEnsureNamespaceDecl_Adds(t *testing.T) {
	xmp := []byte(`<?xpacket begin=""?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
  <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
    <rdf:Description rdf:about=""/>
  </rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`)

	out, added, err := ensureNamespaceDecl(xmp, "pdfaid", pdfaidNS)
	if err != nil {
		t.Fatalf("ensureNamespaceDecl: %v", err)
	}
	if !added {
		t.Error("expected added=true when namespace is new")
	}
	if !strings.Contains(string(out), `xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/"`) {
		t.Errorf("namespace declaration missing:\n%s", out)
	}
}

func TestEnsureNamespaceDecl_SkipsIfPresent(t *testing.T) {
	xmp := []byte(`<?xpacket begin=""?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
  <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
    <rdf:Description rdf:about="" xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/"/>
  </rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`)

	out, added, err := ensureNamespaceDecl(xmp, "pdfaid", pdfaidNS)
	if err != nil {
		t.Fatalf("ensureNamespaceDecl: %v", err)
	}
	if added {
		t.Error("expected added=false when namespace is already declared")
	}
	if !bytes.Equal(out, xmp) {
		t.Error("output should match input byte-for-byte when no change")
	}
}
