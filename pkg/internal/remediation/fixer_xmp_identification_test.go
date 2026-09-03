package remediation

import (
	"strings"
	"testing"
)

// proxyRewrittenRDF is issue15833.pdf's packet in miniature: a web proxy
// rewrote every namespace URI, so the rdf prefix names something that is not
// the RDF namespace. The packet is still well-formed XML.
const proxyRewrittenRDF = `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
<rdf:RDF xmlns:rdf="http://libdb.example.edu:80/rwt/ACS/http/P75Y/1999/02/22-rdf-syntax-ns#">
<rdf:Description rdf:about="" xmlns:prism="http://libdb.example.edu:80/rwt/ACS/http/PB3G/namespaces/basic/3.0/"><prism:doi>10.1021/x</prism:doi></rdf:Description>
</rdf:RDF></x:xmpmeta>
<?xpacket end="w"?>`

// TestPatchExistingXMPWritesTheIdentificationPairTogether pins that a
// conformance level is never written without a part number.
//
// The two are inserted by different primitives: pdfaid:part as a child
// element, which resolves the rdf prefix, and pdfaid:conformance as an
// attribute, which matches the prefix as text. On a packet whose rdf prefix is
// bound to a proxy-rewritten URI only the second one succeeds, and the file
// came out of remediation with an empty pdfaid:part, newly failing
// ISO 19005-2, 6.6.4 test 2.
func TestPatchExistingXMPWritesTheIdentificationPairTogether(t *testing.T) {
	t.Parallel()

	patched, _ := patchExistingXMP(proxyRewrittenRDF, 2, "B", 0, infoMetadata{})

	hasPart := strings.Contains(patched, "pdfaid:part")
	hasConformance := strings.Contains(patched, "pdfaid:conformance")
	if hasConformance && !hasPart {
		t.Errorf("the packet gained a conformance level with no part number:\n%s", patched)
	}
}

// TestPatchExistingXMPStillWritesBothOnASoundPacket is the control: when the
// packet is ordinary, remediation must still add the whole schema.
func TestPatchExistingXMPStillWritesBothOnASoundPacket(t *testing.T) {
	t.Parallel()

	sound := `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
<rdf:Description rdf:about="" xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:format>application/pdf</dc:format></rdf:Description>
</rdf:RDF></x:xmpmeta>
<?xpacket end="w"?>`

	patched, fixes := patchExistingXMP(sound, 2, "B", 0, infoMetadata{})

	if !strings.Contains(patched, "pdfaid:part") {
		t.Errorf("pdfaid:part was not written, fixes=%v:\n%s", fixes, patched)
	}
	if !strings.Contains(patched, "pdfaid:conformance") {
		t.Errorf("pdfaid:conformance was not written, fixes=%v:\n%s", fixes, patched)
	}
	wellFormed(t, patched)
}

// TestPatchExistingXMPStillCorrectsAWrongConformance is the control for the
// other branch: the guard only declines to INJECT a conformance level, so one
// the packet already carries is still corrected.
func TestPatchExistingXMPStillCorrectsAWrongConformance(t *testing.T) {
	t.Parallel()

	wrong := `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
<rdf:Description rdf:about="" xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/">
<pdfaid:part>2</pdfaid:part><pdfaid:conformance>A</pdfaid:conformance></rdf:Description>
</rdf:RDF></x:xmpmeta>
<?xpacket end="w"?>`

	patched, _ := patchExistingXMP(wrong, 2, "B", 0, infoMetadata{})

	if !strings.Contains(patched, "<pdfaid:conformance>B</pdfaid:conformance>") {
		t.Errorf("the conformance level was not corrected to B:\n%s", patched)
	}
	wellFormed(t, patched)
}
