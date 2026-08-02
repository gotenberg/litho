package compliance

import (
	"fmt"
	"testing"
)

// xmpTitlePacket returns an XMP packet whose dc:title holds title, with no
// specific xml:lang beyond x-default.
func xmpTitlePacket(title string) string {
	return `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
 <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description rdf:about="" xmlns:dc="http://purl.org/dc/elements/1.1/">
   <dc:title><rdf:Alt><rdf:li xml:lang="x-default">` + title + `</rdf:li></rdf:Alt></dc:title>
  </rdf:Description>
 </rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`
}

// uaPDFWithXMP builds a minimal tagged one-page PDF carrying packet as the
// catalog's metadata and no catalog /Lang, so both the dc:title rule and the
// metadata-language rule have something to judge.
func uaPDFWithXMP(packet string) []byte {
	return cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 5 0 R /MarkInfo << /Marked true >> "+
			"/Metadata 7 0 R /ViewerPreferences << /DisplayDocTitle true >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Length 0 >>\nstream\n\nendstream",
		"<< /Type /StructTreeRoot /K [6 0 R] /ParentTree 8 0 R >>",
		"<< /Type /StructElem /S /Document /Lang (en-US) >>",
		fmt.Sprintf("<< /Type /Metadata /Subtype /XML /Length %d >>\nstream\n%s\nendstream",
			len(packet), packet),
		"<< /Nums [] >>",
	)
}

// TestXMPMalformedPacketYieldsNoProperties covers what litho may read out of an
// XMP packet it has itself reported as not well-formed. XML 1.0 §2.1 gives a
// document that is not well-formed no infoset, and veraPDF's XMPCore parse
// fails outright, so every MainXMPPackage property test then sees null:
// ISO 14289-1 §7.1 t9 and ISO 14289-2 §8.11.1 t1 report the missing dc:title,
// and no XMPLangAlt exists for §7.2 t33 to qualify. litho parses XMP leniently
// everywhere else, which recovered both out of a malformed packet.
func TestXMPMalformedPacketYieldsNoProperties(t *testing.T) {
	t.Parallel()

	// A raw '<' in character data: well-formed XML forbids it outright.
	malformed := uaPDFWithXMP(xmpTitlePacket("litho < corpus"))
	wellFormed := uaPDFWithXMP(xmpTitlePacket("litho corpus"))

	if !firesUA1(t, malformed, "7.1", 9) {
		t.Error("a malformed packet resolves no dc:title, so §7.1 t9 must fire")
	}
	if firesUA1(t, malformed, "7.2", 33) {
		t.Error("a malformed packet presents no XMPLangAlt, so §7.2 t33 must not fire")
	}

	// The control: the same packet, well formed. Its dc:title resolves, and its
	// x-default alternative with no catalog /Lang is exactly what §7.2 t33 is
	// for, so suppressing the malformed case must not have silenced it.
	if firesUA1(t, wellFormed, "7.1", 9) {
		t.Error("a well-formed packet carrying dc:title must not fire §7.1 t9")
	}
	if !firesUA1(t, wellFormed, "7.2", 33) {
		t.Error("an x-default-only alternative with no catalog /Lang must fire §7.2 t33")
	}
}
