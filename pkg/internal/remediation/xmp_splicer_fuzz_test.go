package remediation

import (
	"bytes"
	"testing"
)

// The XMP splicer edits a metadata packet in place, by byte range, and
// fixer_xmp.go hands it whatever the file contained: the packet arrives through
// doc.DecodeStream, so every byte below is attacker controlled. CLAUDE.md asks
// for a fuzz target beside any function whose leading argument is untrusted
// []byte, and before this file the package had two, both CMap leaf scanners in
// fixer_fonts_fuzz_test.go, neither of which reaches xmp_splicer.go at all.
//
// Two earlier examinations of this family answered different questions, so
// neither covers this one: the injectPropertyOnce work measured remediation
// fixpoint growth, and the fixXMPSchema study scoped extending the fixer to
// embedded streams. Neither asked whether a crafted packet can make the splicer
// index outside its own buffer.
//
// What these targets assert is the slice-bound contract, not a repair outcome.
// findXMPProperty returns a byte range that removeXMPProperty,
// replacePropertyValue and xmpPropertyRawValue all slice the source with, so a
// range outside the buffer is not a wrong answer but a panic one frame up.
// Asserting anything about which properties a fuzzer-built packet yields would
// encode a promise the code does not make.

const (
	xmpSeedElementForm = `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
  <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
    <rdf:Description rdf:about="" xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/">
      <pdfaid:part>2</pdfaid:part>
      <pdfaid:conformance>B</pdfaid:conformance>
    </rdf:Description>
  </rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`

	// Attribute form on a self-closing rdf:Description: the shape that drives
	// findAttributeRange and the self-closing branches of the inserters.
	xmpSeedAttributeForm = `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
  <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
    <rdf:Description rdf:about="" xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/" pdfaid:part="2" pdfaid:conformance="B"/>
  </rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`

	// One line, no newline before the close tag: the shape whose indentation
	// walk once spliced the whole preceding document back into the packet.
	xmpSeedMinified = `<?xpacket begin=""?><x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><rdf:Description rdf:about="" xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>t</dc:title></rdf:Description></rdf:RDF></x:xmpmeta><?xpacket end="w"?>`

	// A non-canonical prefix bound to a required namespace, alongside the
	// canonical one: the input canonicalizePrefix exists for.
	xmpSeedAliasPrefix = `<?xpacket begin=""?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
  <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
    <rdf:Description rdf:about="" xmlns:pdfuaia="http://www.aiim.org/pdfua/ns/id/" xmlns:pdfuaid="http://www.aiim.org/pdfua/ns/id/">
      <pdfuaia:amd>A</pdfuaia:amd>
    </rdf:Description>
  </rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`
)

// FuzzFindXMPProperty asserts the byte range findXMPProperty hands back is a
// valid slice bound for the buffer it was given, then drives the two callers
// that slice the source with it.
//
// The namespace and local name are fuzzed alongside the packet because they
// steer findAttributeRange: it compares a candidate attribute's local name and
// resolved URI before computing any splice offset, so pinning them would leave
// the attribute path reachable only through the seeds.
func FuzzFindXMPProperty(f *testing.F) {
	f.Add([]byte(xmpSeedElementForm), pdfaidNS, "conformance")
	f.Add([]byte(xmpSeedAttributeForm), pdfaidNS, "conformance")
	f.Add([]byte(xmpSeedMinified), "http://purl.org/dc/elements/1.1/", "title")
	f.Add([]byte(xmpSeedElementForm[:len(xmpSeedElementForm)/2]), pdfaidNS, "part")
	f.Add([]byte("not really xml <<<<"), pdfaidNS, "conformance")
	f.Add([]byte("<rdf:Description"), pdfaidNS, "part")
	f.Add([]byte{}, "", "")

	f.Fuzz(func(t *testing.T, data []byte, nsURI, localName string) {
		loc := findXMPProperty(data, nsURI, localName)
		if loc.Form == xmpPropertyNotFound {
			return
		}
		if loc.Start < 0 || loc.End < loc.Start || loc.End > len(data) {
			t.Fatalf("findXMPProperty returned %v for a %d-byte packet: "+
				"removeXMPProperty slices the source with this and would panic",
				loc, len(data))
		}
		_ = removeXMPProperty(data, loc)
		_ = xmpPropertyRawValue(data, loc)
	})
}

// FuzzElementTextRange asserts the offsets replacePropertyValue splices an
// element with.
//
// The self-closing branch returns closeStart = openEnd-2 while openStart is the
// byte after '<', and replacePropertyValue slices element[openStart:closeStart]
// on exactly that path. The two cannot invert today: element[openStart] is '<',
// so the '>' lands at openStart+1 or later, and where openEnd is openStart+2 the
// '/' test reads that same '<' and fails, which leaves openEnd >= openStart+3.
// That reasoning is the only thing standing between the current code and a
// panic, so this target pins it rather than leaving it in a comment.
func FuzzElementTextRange(f *testing.F) {
	f.Add([]byte(`<pdfaid:part>2</pdfaid:part>`))
	f.Add([]byte(`<pdfaid:part/>`))
	f.Add([]byte(`<pdfaid:part xml:lang="x-default">2</pdfaid:part>`))
	f.Add([]byte(`</>`))
	f.Add([]byte(`<>`))
	f.Add([]byte(`<pdfaid:part>`))
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, element []byte) {
		openStart, openEnd, closeStart, ok := elementTextRange(element)
		if !ok {
			return
		}
		for _, off := range []struct {
			name  string
			value int
		}{{"openStart", openStart}, {"openEnd", openEnd}, {"closeStart", closeStart}} {
			if off.value < 0 || off.value > len(element) {
				t.Fatalf("elementTextRange %s = %d for a %d-byte element: "+
					"replacePropertyValue slices with it", off.name, off.value, len(element))
			}
		}
		if closeStart < openEnd && openStart > closeStart {
			t.Fatalf("self-closing range inverts: replacePropertyValue slices "+
				"element[%d:%d] on this path and would panic", openStart, closeStart)
		}
	})
}

// FuzzAttrValueRange asserts the value bounds the attribute path slices with.
func FuzzAttrValueRange(f *testing.F) {
	f.Add([]byte(` pdfaid:conformance="B"`))
	f.Add([]byte(` pdfaid:conformance='B'`))
	f.Add([]byte(` pdfaid:conformance=`))
	f.Add([]byte(` pdfaid:conformance="B`))
	f.Add([]byte(`=`))
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, attr []byte) {
		valStart, valEnd, ok := attrValueRange(attr)
		if !ok {
			return
		}
		if valStart < 0 || valEnd < valStart || valEnd > len(attr) {
			t.Fatalf("attrValueRange returned [%d,%d) for a %d-byte attribute: "+
				"replacePropertyValue slices with it", valStart, valEnd, len(attr))
		}
	})
}

// FuzzReplacePropertyValue drives the splicer that rewrites an existing
// property, which carries the most index arithmetic in the family: it resolves
// a location, re-derives element or attribute offsets inside that location, and
// rebuilds the buffer around them in absolute coordinates.
//
// Its godoc promises the original bytes back when the property is absent, and
// the fallback chain in fixer_xmp.go relies on that to try the next strategy,
// so the promise is asserted rather than assumed.
func FuzzReplacePropertyValue(f *testing.F) {
	f.Add([]byte(xmpSeedElementForm), pdfaidNS, "part", "4")
	f.Add([]byte(xmpSeedAttributeForm), pdfaidNS, "conformance", "A")
	f.Add([]byte(xmpSeedMinified), "http://purl.org/dc/elements/1.1/", "title", "x")
	f.Add([]byte(xmpSeedElementForm[:len(xmpSeedElementForm)/3]), pdfaidNS, "part", "")
	f.Add([]byte{}, "", "", "")

	f.Fuzz(func(t *testing.T, data []byte, nsURI, localName, newValue string) {
		out, err := replacePropertyValue(data, nsURI, localName, newValue)
		if err != nil && !bytes.Equal(out, data) {
			t.Fatalf("replacePropertyValue reported %v but changed the buffer: "+
				"its godoc promises the original bytes back unchanged", err)
		}
	})
}

// FuzzInsertChildElement drives the splicer that appends a property element,
// including the self-closing rdf:Description branch that rewrites the tag into
// open form and the indentation walk that a minified packet once tricked into
// duplicating the whole document.
//
// Its godoc promises the original bytes alongside the error when no anchor is
// found, which is what lets fixer_xmp.go fall through to another strategy.
func FuzzInsertChildElement(f *testing.F) {
	f.Add([]byte(xmpSeedElementForm))
	f.Add([]byte(xmpSeedAttributeForm))
	f.Add([]byte(xmpSeedMinified))
	f.Add([]byte(`<rdf:Description`))
	f.Add([]byte(`<root><notadescription/></root>`))
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		out, err := insertChildElement(data, `<pdfaid:part>2</pdfaid:part>`)
		if err != nil && !bytes.Equal(out, data) {
			t.Fatalf("insertChildElement reported %v but changed the buffer", err)
		}
	})
}

// FuzzInsertAttributeAtDescription drives the namespace-aware description
// selection, whose loop scans every rdf:Description start tag in the packet and
// reads the raw bytes of each to resolve the prefix binding.
func FuzzInsertAttributeAtDescription(f *testing.F) {
	f.Add([]byte(xmpSeedAttributeForm), "pdfaid", pdfaidNS)
	f.Add([]byte(xmpSeedElementForm), "pdfaid", pdfaidNS)
	f.Add([]byte(xmpSeedMinified), "pdfuaid", pdfuaidNS)
	f.Add([]byte(`<rdf:Description`), "pdfaid", pdfaidNS)
	f.Add([]byte(`<root><notadescription/></root>`), "pdfaid", pdfaidNS)
	f.Add([]byte{}, "", "")

	f.Fuzz(func(t *testing.T, data []byte, prefix, uri string) {
		out, err := insertAttributeAtDescription(data, prefix, uri, `pdfaid:conformance="B"`)
		if err != nil && !bytes.Equal(out, data) {
			t.Fatalf("insertAttributeAtDescription reported %v but changed the buffer", err)
		}
	})
}

// FuzzEnsureNamespaceDecl drives the declaration injector and, through it,
// rewriteNamespaceURI. The uri is fuzzed deliberately: production passes only
// package constants, but the rewriter walks its own buffer rather than a
// decoder's tokens, and a uri carrying the quote byte that delimits it used to
// make that walk repeat without end.
func FuzzEnsureNamespaceDecl(f *testing.F) {
	f.Add([]byte(xmpSeedElementForm), "pdfaid", pdfaidNS)
	f.Add([]byte(xmpSeedAttributeForm), "pdfaid", pdfaidNS)
	f.Add([]byte(xmpSeedAliasPrefix), "pdfuaid", pdfuaidNS)
	f.Add([]byte(`<rdf:Description xmlns:pdfaid="http://example.com/wrong/"/>`), "pdfaid", pdfaidNS)
	f.Add([]byte(`<rdf:Description xmlns:pdfaid="a"/>`), "pdfaid", `a"b`)
	f.Add([]byte{}, "", "")

	f.Fuzz(func(_ *testing.T, data []byte, prefix, uri string) {
		_, _ = ensureNamespaceDecl(data, prefix, uri)
	})
}

// FuzzCanonicalizePrefix drives the namespace-alias rewriter. It walks BACKWARD
// from a matched xmlns value to find where the declaration name starts, using
// its own bounds handling rather than a decoder's, and rebuilds the buffer
// around the result. It also loops until it stops finding its marker, so a hang
// here surfaces as a fuzz timeout rather than a crash.
func FuzzCanonicalizePrefix(f *testing.F) {
	f.Add([]byte(xmpSeedAliasPrefix), "pdfuaid", pdfuaidNS)
	f.Add([]byte(xmpSeedElementForm), "pdfaid", pdfaidNS)
	f.Add([]byte(`="`+pdfaidNS+`"`), "pdfaid", pdfaidNS)
	f.Add([]byte(`xmlns:a="`+pdfaidNS+`"`), "pdfaid", pdfaidNS)
	f.Add([]byte(`xmlns="`+pdfaidNS+`"`), "pdfaid", pdfaidNS)
	f.Add([]byte{}, "", "")

	f.Fuzz(func(_ *testing.T, data []byte, canonical, uri string) {
		_ = canonicalizePrefix(data, canonical, uri)
	})
}

// FuzzXMPToUTF8 drives the transcoder that runs before any splicing, on the raw
// decoded packet. Its UTF-16 and UTF-32 paths walk the buffer two and four
// bytes at a time, which is the classic overrun shape, and both accept BOM-less
// input so the byte order is inferred from content rather than a marker.
//
// The pass-through promise is asserted: a packet the transcoder declines to
// convert must come back byte for byte. It did not, until the trailing-newline
// case was fixed alongside this target.
func FuzzXMPToUTF8(f *testing.F) {
	f.Add([]byte(xmpSeedMinified))
	f.Add([]byte("<?xpacket\n"))
	f.Add([]byte{0xFE, 0xFF, 0x00, '<'})
	f.Add([]byte{0xFF, 0xFE, '<', 0x00})
	f.Add([]byte{0x00, '<', 0x00, '?'})
	f.Add([]byte{'<', 0x00, '?', 0x00})
	f.Add([]byte{0x00, 0x00, 0xFE, 0xFF, 0x00, 0x00, 0x00, '<'})
	f.Add([]byte{0xFF, 0xFE, 0x00, 0x00, '<', 0x00, 0x00, 0x00})
	f.Add([]byte{0x00, '<', 0x00}) // Odd byte count.
	f.Add([]byte(" \t\r\n"))       // Whitespace only: trims away to nothing.
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		out, changed := xmpToUTF8(data)
		if !changed && !bytes.Equal(out, data) {
			t.Fatalf("xmpToUTF8 declined to convert a %d-byte packet but returned %d bytes: "+
				"an unconverted packet is promised back untouched", len(data), len(out))
		}
	})
}
