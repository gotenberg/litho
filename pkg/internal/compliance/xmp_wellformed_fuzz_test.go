package compliance

import "testing"

// validXMPPacket is a minimal well-formed XMP packet: an xpacket header, an
// rdf:RDF root with one namespaced description, and the trailer.
const validXMPPacket = `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>` +
	`<x:xmpmeta xmlns:x="adobe:ns:meta/">` +
	`<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` +
	`<rdf:Description rdf:about="" xmlns:dc="http://purl.org/dc/elements/1.1/">` +
	`<dc:title><rdf:Alt><rdf:li xml:lang="x-default">T</rdf:li></rdf:Alt></dc:title>` +
	`</rdf:Description></rdf:RDF></x:xmpmeta><?xpacket end="w"?>`

// FuzzXMPWellFormed asserts the XMP well-formedness test never panics.
//
// The packet is read straight out of the document's /Metadata stream, so its
// bytes are entirely file-controlled, and the check runs them through
// encoding/xml with a pass-through CharsetReader and namespace tracking. Both
// the validator (ISO 19005-2, 6.6.2.1 t4) and the remediation XMP fixer gate on
// the answer, so a panic here would take down validation and repair alike.
func FuzzXMPWellFormed(f *testing.F) {
	f.Add([]byte(validXMPPacket))
	// Truncated mid-element: the header and an unclosed tag.
	f.Add([]byte(validXMPPacket[:len(validXMPPacket)/2]))
	f.Add([]byte{})
	// A declared encoding the bytes are not, which the pass-through
	// CharsetReader exists to tolerate.
	f.Add([]byte(`<?xml version="1.0" encoding="UTF-16"?><rdf:RDF/>`))

	f.Fuzz(func(_ *testing.T, data []byte) {
		_ = XMPWellFormed(data)
	})
}
