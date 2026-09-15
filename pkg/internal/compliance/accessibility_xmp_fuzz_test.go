package compliance

import (
	"context"
	"testing"
)

// uaXMPPacket is a packet carrying the PDF/UA identification schema plus a
// dc:title, so the identification scanners and the title probe all have
// something to find rather than returning at the first miss.
const uaXMPPacket = `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>` +
	`<x:xmpmeta xmlns:x="adobe:ns:meta/">` +
	`<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` +
	`<rdf:Description rdf:about="" xmlns:pdfuaid="http://www.aiim.org/pdfua/ns/id/">` +
	`<pdfuaid:part>1</pdfuaid:part><pdfuaid:rev>2024</pdfuaid:rev>` +
	`</rdf:Description>` +
	`<rdf:Description rdf:about="" xmlns:dc="http://purl.org/dc/elements/1.1/">` +
	`<dc:title><rdf:Alt><rdf:li xml:lang="x-default">A Title</rdf:li></rdf:Alt></dc:title>` +
	`</rdf:Description>` +
	`</rdf:RDF></x:xmpmeta><?xpacket end="w"?>`

// FuzzAccessibilityXMP feeds arbitrary packet bytes to the PDF/UA
// identification scanners and asserts crash-freedom.
//
// These read the /Metadata stream directly rather than through the XMP
// machinery: they scan for the pdfuaid part, rev and prefix bindings, for a
// dc:title, and for an undetermined xml:lang. Every one takes the packet as its
// leading argument, so the bytes are file-controlled, and they run on every
// PDF/UA validation.
//
// [FuzzXMPChecks] covers the PDF/A side of the same stream; measured against
// the fuzz corpora these UA scanners were reached by nothing.
func FuzzAccessibilityXMP(f *testing.F) {
	f.Add([]byte(uaXMPPacket))
	f.Add([]byte(uaXMPPacket[:len(uaXMPPacket)/2]))
	f.Add([]byte{})
	// The identification schema under a different prefix, which the prefix
	// check exists to catch.
	f.Add([]byte(`<rdf:Description xmlns:ua="http://www.aiim.org/pdfua/ns/id/"><ua:part>1</ua:part></rdf:Description>`))
	// An undetermined language, the value the lang probe looks for.
	f.Add([]byte(`<rdf:li xml:lang="und">x</rdf:li>`))
	f.Add([]byte(`<dc:title>`))

	f.Fuzz(func(_ *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		ctx := context.Background()

		_ = containsDCTitle(data)
		_ = containsPDFUAIDProperty(ctx, data)
		_ = containsPDFUAIDRev(ctx, data)
		_ = xmpLangUndetermined(data)
		for _, part := range []string{"1", "2", ""} {
			_ = containsPDFUAIDPart(ctx, data, part)
		}
		for _, profile := range []string{"PDF/UA-1", "PDF/UA-2"} {
			_ = checkPDFUAIdentificationIn(ctx, data, profile)
			_ = checkPDFUAIDPrefixesIn(ctx, data, profile)
		}
	})
}

// TestUAXMPPacketIsScanned pins that the seed reaches the scanners: each must
// find what the packet declares, or the target runs against a packet they all
// reject and covers only the early returns.
func TestUAXMPPacketIsScanned(t *testing.T) {
	t.Parallel()

	data := []byte(uaXMPPacket)
	ctx := context.Background()

	if !containsDCTitle(data) {
		t.Error("the dc:title in the seed was not found")
	}
	if !containsPDFUAIDProperty(ctx, data) {
		t.Error("the pdfuaid schema in the seed was not found")
	}
	if !containsPDFUAIDPart(ctx, data, "1") {
		t.Error("pdfuaid:part 1 was not found")
	}
	if !containsPDFUAIDRev(ctx, data) {
		t.Error("pdfuaid:rev was not found")
	}
}
