package remediation

import (
	"bytes"
	"encoding/xml"
	"io"
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
	// conformance attribute again. It should be gone, and re-parsing the
	// result should succeed (exercised by running findXMPProperty on
	// it: a parse error would return not-found).
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

// TestInsertChildElement_Minified guards against the indentation bug where a
// packet with no newline before the close tag made the indent walk capture the
// entire preceding document and splice a duplicate of it in, producing a second
// <?xpacket>/<x:xmpmeta> preamble and non-well-formed XML. Minified XMP, where
// the whole packet is one line with no newline before the close tag, triggers
// the bug, so a remediated packet must stay well-formed for that shape.
func TestInsertChildElement_Minified(t *testing.T) {
	xmp := []byte(`<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?><x:xmpmeta xmlns:x="adobe:ns:meta/">  <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">    <rdf:Description rdf:about="" xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>t</dc:title></rdf:Description>    <rdf:Description rdf:about="" xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/" pdfaid:conformance="B"/>  </rdf:RDF></x:xmpmeta><?xpacket end="w"?>`)

	out, err := insertChildElement(xmp, `<pdfaid:part>2</pdfaid:part>`)
	if err != nil {
		t.Fatalf("insertChildElement: %v", err)
	}
	s := string(out)
	if n := strings.Count(s, "<?xpacket begin"); n != 1 {
		t.Fatalf("xpacket preamble duplicated: got %d begins\n%s", n, s)
	}
	if n := strings.Count(s, "<x:xmpmeta"); n != 1 {
		t.Errorf("xmpmeta duplicated: got %d\n%s", n, s)
	}
	if n := strings.Count(s, "<pdfaid:part>2</pdfaid:part>"); n != 1 {
		t.Errorf("element not injected exactly once: got %d\n%s", n, s)
	}
	// The output must be well-formed XML (the corruption was not).
	dec := xml.NewDecoder(bytes.NewReader(out))
	for {
		_, terr := dec.Token()
		if terr == io.EOF {
			break
		}
		if terr != nil {
			t.Fatalf("output not well-formed XML: %v\n%s", terr, s)
		}
	}
}

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

func TestEnsureNamespaceDecl_Adds(t *testing.T) {
	xmp := []byte(`<?xpacket begin=""?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
  <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
    <rdf:Description rdf:about=""/>
  </rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`)

	out, err := ensureNamespaceDecl(xmp, "pdfaid", pdfaidNS)
	if err != nil {
		t.Fatalf("ensureNamespaceDecl: %v", err)
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

	out, err := ensureNamespaceDecl(xmp, "pdfaid", pdfaidNS)
	if err != nil {
		t.Fatalf("ensureNamespaceDecl: %v", err)
	}
	if !bytes.Equal(out, xmp) {
		t.Error("output should match input byte-for-byte when no change")
	}
}

func TestCanonicalizePrefix_DropsAliasWhenCanonicalPresent(t *testing.T) {
	// An alias prefix (pdfuaia) and the canonical prefix (pdfuaid) both bind
	// the PDF/UA id namespace on one rdf:Description, with a property under
	// the alias. Canonicalizing must not emit a second xmlns:pdfuaid: XML
	// well-formedness forbids two attributes with the same name on one
	// element (XML 1.0 Unique Att Spec constraint), so a duplicate xmlns
	// declaration makes the packet non-well-formed. The alias declaration is
	// dropped and its usage merged.
	xmp := []byte(`<?xpacket begin=""?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
  <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
    <rdf:Description rdf:about=""
        xmlns:pdfuaia="http://www.aiim.org/pdfua/ns/id/"
        xmlns:pdfuaid="http://www.aiim.org/pdfua/ns/id/">
      <pdfuaid:part>1</pdfuaid:part>
      <pdfuaia:amd>A</pdfuaia:amd>
    </rdf:Description>
  </rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`)

	got := string(canonicalizePrefix(xmp, "pdfuaid", pdfuaidNS))

	if n := strings.Count(got, "xmlns:pdfuaid="); n != 1 {
		t.Errorf("want exactly 1 xmlns:pdfuaid declaration (no duplicate), got %d:\n%s", n, got)
	}
	if strings.Contains(got, "xmlns:pdfuaia") {
		t.Errorf("alias xmlns:pdfuaia should be dropped:\n%s", got)
	}
	if !strings.Contains(got, "<pdfuaid:amd>") || strings.Contains(got, "<pdfuaia:amd>") {
		t.Errorf("aliased property should be rewritten to the canonical prefix:\n%s", got)
	}
}

func TestCanonicalizePrefix_RenamesWhenAliasOnly(t *testing.T) {
	// Only the alias prefix is present (no canonical declaration). The alias
	// is renamed to the canonical prefix and its usages rewritten.
	xmp := []byte(`<?xpacket begin=""?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
  <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
    <rdf:Description rdf:about=""
        xmlns:pdfuaia="http://www.aiim.org/pdfua/ns/id/">
      <pdfuaia:part>1</pdfuaia:part>
    </rdf:Description>
  </rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`)

	got := string(canonicalizePrefix(xmp, "pdfuaid", pdfuaidNS))

	if strings.Contains(got, "pdfuaia") {
		t.Errorf("alias prefix should be renamed away:\n%s", got)
	}
	if n := strings.Count(got, "xmlns:pdfuaid="); n != 1 {
		t.Errorf("want 1 xmlns:pdfuaid declaration, got %d:\n%s", n, got)
	}
	if !strings.Contains(got, "<pdfuaid:part>") {
		t.Errorf("usage should be renamed to the canonical prefix:\n%s", got)
	}
}

// TestCanonicalizePrefix_RenamesAmd confirms the existing canonicalize pass also
// repairs a wrongly-prefixed pdfaid:amd: the compliance side now detects the
// wrong prefix (§6.6.4 t06), and remediation already fixes it for free.
func TestCanonicalizePrefix_RenamesAmd(t *testing.T) {
	xmp := []byte(`<?xpacket begin=""?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
  <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
    <rdf:Description rdf:about=""
        xmlns:pdfa="http://www.aiim.org/pdfa/ns/id/">
      <pdfa:amd>2016</pdfa:amd>
    </rdf:Description>
  </rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`)

	got := string(canonicalizePrefix(xmp, "pdfaid", pdfaidNS))

	if strings.Contains(got, "pdfa:amd") {
		t.Errorf("pdfa:amd should be canonicalized to pdfaid:amd:\n%s", got)
	}
	if !strings.Contains(got, "<pdfaid:amd>") {
		t.Errorf("expected <pdfaid:amd> after canonicalization:\n%s", got)
	}
}

// TestInsertAttributeAtDescription_SelectsByURI guards the namespace-aware
// description selection: a tag that declares the same prefix letters bound
// to a different URI must be skipped, or the spliced attribute lands in the
// wrong namespace and the validator still reports the property missing.
func TestInsertAttributeAtDescription_SelectsByURI(t *testing.T) {
	xmp := []byte(`<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description xmlns:pdfaid="http://example.com/wrong/" rdf:about="" pdfaid:custom="x"/>
  <rdf:Description xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/" rdf:about="" pdfaid:part="2"/>
</rdf:RDF>`)

	out, err := insertAttributeAtDescription(xmp, "pdfaid", pdfaidNS, `pdfaid:conformance="B"`)
	if err != nil {
		t.Fatalf("insertAttributeAtDescription: %v", err)
	}
	loc := findXMPProperty(out, pdfaidNS, "conformance")
	if loc.Form != xmpPropertyAsAttribute {
		t.Fatalf("conformance not found in the pdfaid namespace after splice:\n%s", out)
	}
	if wrong := findXMPProperty(out, "http://example.com/wrong/", "conformance"); wrong.Form != xmpPropertyNotFound {
		t.Errorf("conformance spliced into the wrong-URI description:\n%s", out)
	}
}

// TestXMPToUTF8 pins the UTF-16 transcoding that unblocks the byte-range
// splicer on UTF-16 packets (the 6-7-9-t04 fixture family ships BOM-less
// UTF-16BE with a trailing newline; without conversion the pdfaid repairs
// silently no-op and the UTF-8 requirement stays unrepaired).
func TestXMPToUTF8(t *testing.T) {
	cases := []struct {
		name    string
		in      []byte
		want    string
		changed bool
	}{
		{"bom BE", []byte{0xFE, 0xFF, 0x00, '<', 0x00, '?'}, "<?", true},
		{"bom LE", []byte{0xFF, 0xFE, '<', 0x00, '?', 0x00}, "<?", true},
		{"bomless BE", []byte{0x00, '<', 0x00, '?'}, "<?", true},
		{"bomless LE", []byte{'<', 0x00, '?', 0x00}, "<?", true},
		{"bomless BE trailing newline", []byte{0x00, '<', 0x00, '?', 0x0A}, "<?", true},
		{"utf32 bom BE", []byte{0x00, 0x00, 0xFE, 0xFF, 0x00, 0x00, 0x00, '<', 0x00, 0x00, 0x00, '?'}, "<?", true},
		{"utf32 bom LE", []byte{0xFF, 0xFE, 0x00, 0x00, '<', 0x00, 0x00, 0x00, '?', 0x00, 0x00, 0x00}, "<?", true},
		{"utf32 bomless BE", []byte{0x00, 0x00, 0x00, '<', 0x00, 0x00, 0x00, '?'}, "<?", true},
		{"utf32 bomless LE", []byte{'<', 0x00, 0x00, 0x00, '?', 0x00, 0x00, 0x00}, "<?", true},
		{"utf32 bomless LE leading newline", []byte{0x0A, 0x00, 0x00, 0x00, '<', 0x00, 0x00, 0x00, '?', 0x00, 0x00, 0x00}, "\n<?", true},
		{"utf8 passthrough", []byte("<?xpacket"), "<?xpacket", false},
		// The trailing-whitespace trim exists to stop an odd byte count masking
		// a UTF-16 packet, so it must not survive into a pass-through return:
		// the godoc promises UTF-8 packets come back untouched, and a stream
		// payload carrying the usual newline before endstream is the common
		// shape that would otherwise come back silently shortened.
		{"utf8 passthrough with trailing newline", []byte("<?xpacket\n"), "<?xpacket\n", false},
		{"utf16 BE not misread as utf32", []byte{0x00, '<', 0x00, '?', 0x00, 'x', 0x00, 'p'}, "<?xp", true},
		{"empty", nil, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, changed := xmpToUTF8(tc.in)
			if changed != tc.changed || string(out) != tc.want {
				t.Errorf("got %q changed=%v, want %q changed=%v", out, changed, tc.want, tc.changed)
			}
		})
	}
}

// TestEnsureNamespaceDecl_RewritesEveryWrongBinding pins the promise both
// functions make: rewriteNamespaceURI replaces every xmlns:prefix declaration
// whose URI differs, and ensureNamespaceDecl advertises that as happening
// across the entire buffer.
//
// The scan restarted at the buffer start after each replacement, so the next
// pass re-found the declaration it had just corrected and stopped there. A
// packet whose second rdf:Description repeated the same wrong binding kept it,
// which leaves properties under that prefix resolving to a namespace no
// validator recognizes.
func TestEnsureNamespaceDecl_RewritesEveryWrongBinding(t *testing.T) {
	xmp := []byte(`<?xpacket begin=""?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
  <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
    <rdf:Description rdf:about="" xmlns:pdfaid="http://example.com/wrong-one/"/>
    <rdf:Description rdf:about="" xmlns:pdfaid="http://example.com/wrong-two/"/>
  </rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`)

	out, err := ensureNamespaceDecl(xmp, "pdfaid", pdfaidNS)
	if err != nil {
		t.Fatalf("ensureNamespaceDecl: %v", err)
	}
	got := string(out)
	if n := strings.Count(got, `xmlns:pdfaid="`+pdfaidNS+`"`); n != 2 {
		t.Errorf("want both bindings rewritten to the canonical URI, got %d:\n%s", n, got)
	}
	if strings.Contains(got, "example.com/wrong") {
		t.Errorf("a wrong binding survived the rewrite:\n%s", got)
	}
}

// TestRewriteNamespaceURI_TerminatesWhenURIContainsItsDelimiter guards a loop
// that had no exit.
//
// The rewriter re-read the value it had just written to decide whether to stop.
// When the replacement URI contains the quote byte that delimits it, that
// re-read returns a truncated prefix which never equals the URI, so the old
// loop spliced a slightly longer buffer forever. No production caller passes
// such a URI: all of them pass package constants. A loop with no exit is still
// a trap for the next caller, and it wedges any fuzz target that reaches it
// rather than failing outright.
//
// A regression does not fail the assertion below, it hangs. The package test
// timeout is what reports it.
func TestRewriteNamespaceURI_TerminatesWhenURIContainsItsDelimiter(t *testing.T) {
	xmp := []byte(`<rdf:Description xmlns:pdfaid="http://example.com/wrong/"/>`)
	got := string(rewriteNamespaceURI(xmp, "pdfaid", `http://example.com/a"b/`))
	if !strings.Contains(got, `http://example.com/a"b/`) {
		t.Errorf("replacement URI missing from the result:\n%s", got)
	}
}
