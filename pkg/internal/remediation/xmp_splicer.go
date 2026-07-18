package remediation

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"unicode/utf16"
)

// maxXMPDepth caps the element nesting level while walking XMP. Real
// packets rarely nest deeper than ~10 levels (x:xmpmeta → rdf:RDF →
// rdf:Description → property → rdf:Alt → rdf:li). A cap of 1000 leaves
// two orders of magnitude of headroom and bounds CPU on pathological
// input from a malicious producer.
const maxXMPDepth = 1000

// xmpPropertyForm describes how an XMP property is expressed in the
// serialized XML: as a child element of rdf:Description, or as a
// namespaced attribute on an rdf:Description start tag.
type xmpPropertyForm int

const (
	xmpPropertyNotFound xmpPropertyForm = iota
	xmpPropertyAsElement
	xmpPropertyAsAttribute
)

// xmpPropertyLocation records where a specific property sits in the
// original XMP byte stream. The byte range is always expressed against
// the source bytes that were fed to [findXMPProperty] so the caller can
// splice without re-encoding.
type xmpPropertyLocation struct {
	Form xmpPropertyForm
	// Start and End bracket the bytes that should be removed, including
	// any leading whitespace so that pretty-printed XMP stays stable
	// across edits.
	Start int
	End   int
}

// findXMPProperty scans xmp as XML and locates the first occurrence of a
// namespaced property inside any rdf:Description element. The scan is
// structural (via encoding/xml.Decoder), so it works regardless of
// whitespace, attribute order, namespace prefix alias, or self-closing
// form.
//
// When the property is expressed as a child element, Start/End bracket
// the element including any preceding indentation and the trailing
// newline. Removing those bytes leaves well-formed XML. When the
// property is expressed as an attribute, Start/End bracket the attribute
// including the single leading space separating it from the previous
// attribute (or tag name).
//
// Returns a location with Form=xmpPropertyNotFound if the property does
// not appear anywhere, or if the input is not parseable as XML.
func findXMPProperty(xmp []byte, nsURI, localName string) xmpPropertyLocation {
	dec := xml.NewDecoder(bytes.NewReader(xmp))
	dec.Strict = false
	// Treat unknown entities as literal. Some XMP producers emit them
	// around non-ASCII content and strict mode would reject the input.
	dec.Entity = xml.HTMLEntity

	for {
		startOff := dec.InputOffset()
		tok, err := dec.Token()
		if err != nil {
			if err == io.EOF {
				return xmpPropertyLocation{Form: xmpPropertyNotFound}
			}
			return xmpPropertyLocation{Form: xmpPropertyNotFound}
		}
		endOff := dec.InputOffset()

		startElem, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}

		// Only look for properties on rdf:Description: all XMP metadata
		// lives under one (or more) of these elements, and restricting
		// the search avoids false positives from nested element forms.
		if !isRDFDescription(startElem.Name) {
			continue
		}

		// Attribute form: ` pdfaid:conformance="..."` on the start tag.
		if attrLoc, found := findAttributeRange(xmp, int(startOff), int(endOff), nsURI, localName); found {
			return attrLoc
		}

		// Element form: scan forward through child tokens until the
		// matching child or the corresponding EndElement is reached.
		// A cursor over the byte stream tracks the byte range of the
		// matching child element including its closing tag.
		if elemLoc, found := findChildElementRange(dec, xmp, startElem.Name, nsURI, localName); found {
			return elemLoc
		}

		// If neither form was found under this rdf:Description, continue
		// scanning. The decoder is already positioned past the closing
		// tag because findChildElementRange consumed tokens until EMC.
	}
}

// isRDFDescription reports whether the given XML name is rdf:Description,
// regardless of the prefix alias the producer chose.
func isRDFDescription(n xml.Name) bool {
	return n.Local == "Description" &&
		n.Space == "http://www.w3.org/1999/02/22-rdf-syntax-ns#"
}

// findAttributeRange searches for ` nsURI:localName="..."` inside the
// byte range that contains an rdf:Description start tag. Returns a
// location with Form=xmpPropertyAsAttribute when found. The byte range
// includes the leading space that separates the attribute from its
// left neighbor.
func findAttributeRange(xmp []byte, tagStart, tagEnd int, nsURI, localName string) (xmpPropertyLocation, bool) {
	// Walk the raw bytes of the start tag looking for attributes whose
	// expanded name matches (nsURI, localName). The StartElement.Attr
	// slice is not directly usable because xml.Decoder doesn't expose
	// per-attribute byte offsets. The tag text is bounded, so a
	// targeted scan is both cheap and unambiguous.
	//
	// Strategy: for each candidate "name=value" pair in the tag bytes,
	// check if the prefix resolves to nsURI in the tag's namespace
	// declarations, and if the local name matches.
	tagBytes := xmp[tagStart:tagEnd]

	nsDecls := collectNamespaceDecls(tagBytes)
	// Also inherit the element's declared namespaces from the decoder's
	// view, not just textual xmlns. For the attribute case the prefix
	// must be explicit since default namespaces don't apply to attributes.

	pos := 0
	for pos < len(tagBytes) {
		// Find the next attribute. Skip whitespace and the tag-name on
		// the first iteration.
		nameStart := skipAttrGap(tagBytes, pos)
		if nameStart >= len(tagBytes) {
			break
		}
		if tagBytes[nameStart] == '>' || tagBytes[nameStart] == '/' {
			break
		}
		nameEnd := nameStart
		for nameEnd < len(tagBytes) && tagBytes[nameEnd] != '=' && !isSpace(tagBytes[nameEnd]) {
			nameEnd++
		}
		if nameEnd >= len(tagBytes) {
			break
		}
		name := string(tagBytes[nameStart:nameEnd])

		// Skip whitespace + '='.
		cur := nameEnd
		for cur < len(tagBytes) && isSpace(tagBytes[cur]) {
			cur++
		}
		if cur >= len(tagBytes) || tagBytes[cur] != '=' {
			pos = cur
			continue
		}
		cur++
		for cur < len(tagBytes) && isSpace(tagBytes[cur]) {
			cur++
		}
		if cur >= len(tagBytes) {
			break
		}
		quote := tagBytes[cur]
		if quote != '"' && quote != '\'' {
			pos = cur + 1
			continue
		}
		valStart := cur + 1
		valEnd := valStart
		for valEnd < len(tagBytes) && tagBytes[valEnd] != quote {
			valEnd++
		}
		if valEnd >= len(tagBytes) {
			break
		}
		attrEnd := valEnd + 1
		pos = attrEnd

		// Check if this attribute matches. Namespaced attributes have
		// the form `prefix:local`.
		prefix, local := splitPrefix(name)
		if local != localName {
			continue
		}
		if ns, ok := nsDecls[prefix]; ok && ns == nsURI {
			// Found. Compute splice range: include the leading space
			// before the attribute so "a=\"1\" b=\"2\"" → "a=\"1\"" (not "a=\"1\" ").
			spliceStart := tagStart + nameStart
			for spliceStart > tagStart && isSpace(xmp[spliceStart-1]) {
				spliceStart--
			}
			return xmpPropertyLocation{
				Form:  xmpPropertyAsAttribute,
				Start: spliceStart,
				End:   tagStart + attrEnd,
			}, true
		}
	}
	return xmpPropertyLocation{}, false
}

// findChildElementRange walks the decoder forward from inside an
// rdf:Description start element, looking for the first child element
// whose expanded name is (nsURI, localName). Returns a location with
// Form=xmpPropertyAsElement when found, with the byte range spanning
// the element and its preceding indentation.
func findChildElementRange(dec *xml.Decoder, xmp []byte, descName xml.Name, nsURI, localName string) (xmpPropertyLocation, bool) {
	depth := 1
	for {
		tokStart := dec.InputOffset()
		tok, err := dec.Token()
		if err != nil {
			return xmpPropertyLocation{}, false
		}

		switch t := tok.(type) {
		case xml.StartElement:
			if depth == 1 && t.Name.Space == nsURI && t.Name.Local == localName {
				// Found: consume tokens until the matching EndElement
				// so the byte offset of the child element's closing
				// tag is known.
				if endOff, ok := consumeElement(dec, t.Name); ok {
					spliceStart := int(tokStart)
					// Expand backwards to include the indentation and
					// any preceding \n so the removed slice is a clean
					// line (or segment) removal.
					for spliceStart > 0 && isSpaceOrNL(xmp[spliceStart-1]) && xmp[spliceStart-1] != '\n' {
						spliceStart--
					}
					spliceEnd := endOff
					// Include the trailing newline if present.
					if spliceEnd < len(xmp) && xmp[spliceEnd] == '\n' {
						spliceEnd++
					}
					return xmpPropertyLocation{
						Form:  xmpPropertyAsElement,
						Start: spliceStart,
						End:   spliceEnd,
					}, true
				}
				return xmpPropertyLocation{}, false
			}
			depth++
			if depth > maxXMPDepth {
				return xmpPropertyLocation{}, false
			}
		case xml.EndElement:
			depth--
			if depth == 0 && t.Name == descName {
				return xmpPropertyLocation{}, false
			}
		}
	}
}

// consumeElement drains tokens from dec until it has consumed the
// matching EndElement for name. Returns the byte offset immediately
// after the EndElement, or 0 and false on error. The nesting depth
// is capped at [maxXMPDepth] so pathological input cannot block the
// decoder indefinitely.
func consumeElement(dec *xml.Decoder, name xml.Name) (int, bool) {
	depth := 1
	for {
		tok, err := dec.Token()
		if err != nil {
			return 0, false
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			if depth > maxXMPDepth {
				return 0, false
			}
		case xml.EndElement:
			depth--
			if depth == 0 && t.Name == name {
				return int(dec.InputOffset()), true
			}
		}
	}
}

// removeXMPProperty splices the given byte range out of xmp and returns
// the result. The returned slice is always a fresh allocation, so the
// caller may freely mutate the source afterwards.
func removeXMPProperty(xmp []byte, loc xmpPropertyLocation) []byte {
	if loc.Form == xmpPropertyNotFound || loc.End <= loc.Start {
		return append([]byte(nil), xmp...)
	}
	out := make([]byte, 0, len(xmp)-(loc.End-loc.Start))
	out = append(out, xmp[:loc.Start]...)
	out = append(out, xmp[loc.End:]...)
	return out
}

// insertAttributeAtDescription injects attr onto the first rdf:Description
// tag that binds prefix to uri in its xmlns attributes, so the inserted
// attribute resolves to the intended namespace. A tag that merely declares
// the same prefix letters against a different URI is skipped: splicing there
// would put the property in the wrong namespace and the validator would
// still see it as missing. Falls back to the first rdf:Description when no
// tag binds the namespace; callers run [ensureNamespaceDecl] first so that
// fallback receives the declaration.
func insertAttributeAtDescription(xmp []byte, prefix, uri, attrSuffix string) ([]byte, error) {
	offset := 0
	for offset < len(xmp) {
		idx := bytes.Index(xmp[offset:], []byte("<rdf:Description"))
		if idx < 0 {
			break
		}
		tagStart := offset + idx
		tagEnd := bytes.IndexAny(xmp[tagStart:], ">")
		if tagEnd < 0 {
			break
		}
		tagEnd += tagStart
		if decls := collectNamespaceDecls(xmp[tagStart:tagEnd]); decls[prefix] == uri {
			return insertAttrBeforeTagEnd(xmp, tagEnd, attrSuffix), nil
		}
		offset = tagEnd + 1
	}
	idx := bytes.Index(xmp, []byte("<rdf:Description"))
	if idx < 0 {
		return append([]byte(nil), xmp...), errXMPPropertyNotFound
	}
	tagStart := idx
	tagEnd := bytes.IndexAny(xmp[tagStart:], ">")
	if tagEnd < 0 {
		return append([]byte(nil), xmp...), errXMPPropertyNotFound
	}
	tagEnd += tagStart
	return insertAttrBeforeTagEnd(xmp, tagEnd, attrSuffix), nil
}

// insertAttrBeforeTagEnd writes attrSuffix just before the end of a
// tag whose closing '>' sits at tagEnd. The attribute suffix is the
// full `name="value"` text; a single space prefix is inserted.
func insertAttrBeforeTagEnd(xmp []byte, tagEnd int, attrSuffix string) []byte {
	insertAt := tagEnd
	if tagEnd > 0 && xmp[tagEnd-1] == '/' {
		insertAt = tagEnd - 1
	}
	out := make([]byte, 0, len(xmp)+len(attrSuffix)+1)
	out = append(out, xmp[:insertAt]...)
	out = append(out, ' ')
	out = append(out, []byte(attrSuffix)...)
	out = append(out, xmp[insertAt:]...)
	return out
}

// insertChildElement inserts a new child element into the first
// rdf:Description found in xmp. The content argument is raw XML; the
// caller is responsible for escaping any user-provided values via
// [xmlEscape]. Returns a fresh byte slice with the element spliced in
// just before the closing `</rdf:Description>`, preserving the
// existing indentation of that closing tag.
//
// Returns the original bytes unchanged plus an error if no
// rdf:Description is found or if the XMP is not parseable.
func insertChildElement(xmp []byte, elementXML string) ([]byte, error) {
	// A self-closing first rdf:Description (Distiller attribute-form XMP,
	// <rdf:Description .../>) has no close tag to insert before, and its
	// namespace declarations live on that start tag. Convert it to open form
	// and nest the element inside, so it inherits those namespaces. Inserting
	// after the self-closing tag would orphan the element as a direct child of
	// rdf:RDF, which is invalid RDF and invisible to conforming XMP readers.
	if _, tagEnd, err := findRDFDescriptionOpenRange(xmp); err == nil &&
		tagEnd >= 2 && xmp[tagEnd-2] == '/' && xmp[tagEnd-1] == '>' {
		out := make([]byte, 0, len(xmp)+len(elementXML)+32)
		out = append(out, xmp[:tagEnd-2]...)
		out = append(out, '>')
		out = append(out, "\n      "...)
		out = append(out, elementXML...)
		out = append(out, "\n    </rdf:Description>"...)
		out = append(out, xmp[tagEnd:]...)
		return out, nil
	}

	descEnd, err := findRDFDescriptionCloseOffset(xmp)
	if err != nil {
		return append([]byte(nil), xmp...), err
	}
	// Adopt only the run of spaces and tabs immediately before the close
	// tag as the injected element's indentation. Walking back to the
	// previous newline would, in a minified packet with no newline before
	// the close tag, capture the entire preceding document as "indent" and
	// splice a duplicate of it into the packet, corrupting the XML.
	insertAt := descEnd
	indentStart := descEnd
	for indentStart > 0 && (xmp[indentStart-1] == ' ' || xmp[indentStart-1] == '\t') {
		indentStart--
	}
	indent := string(xmp[indentStart:descEnd])

	injection := indent + elementXML + "\n"
	out := make([]byte, 0, len(xmp)+len(injection))
	out = append(out, xmp[:insertAt]...)
	out = append(out, []byte(injection)...)
	out = append(out, xmp[insertAt:]...)
	return out, nil
}

// replacePropertyValue rewrites the text content of an existing
// element-form XMP property to newValue. Attribute-form properties are
// not currently supported. The caller must fall back to
// [insertChildElement] if the property is not found.
//
// Returns the original bytes unchanged plus an error if the property
// does not exist in element form.
func replacePropertyValue(xmp []byte, nsURI, localName, newValue string) ([]byte, error) {
	loc := findXMPProperty(xmp, nsURI, localName)
	switch loc.Form {
	case xmpPropertyNotFound:
		return append([]byte(nil), xmp...), errXMPPropertyNotFound
	case xmpPropertyAsElement:
		elementBytes := xmp[loc.Start:loc.End]
		openStart, openEnd, closeStart, ok := elementTextRange(elementBytes)
		if !ok {
			return append([]byte(nil), xmp...), errXMPPropertyNotFound
		}
		out := make([]byte, 0, len(xmp)+len(newValue))
		if closeStart < openEnd {
			// Self-closing <prefix:Name/>: rebuild as an open/close pair.
			// The naive splice would leave the trailing "/>" behind as
			// stray text, which invalidates the whole packet for XMP
			// readers.
			name := elementBytes[openStart:closeStart]
			if sp := bytes.IndexAny(name, " \t\r\n"); sp >= 0 {
				name = name[:sp]
			}
			out = append(out, xmp[:loc.Start+closeStart]...)
			out = append(out, '>')
			out = append(out, newValue...)
			out = append(out, "</"...)
			out = append(out, name...)
			out = append(out, '>')
			out = append(out, xmp[loc.Start+openEnd:]...)
			return out, nil
		}
		absOpenEnd := loc.Start + openEnd
		absCloseStart := loc.Start + closeStart
		out = append(out, xmp[:absOpenEnd]...)
		out = append(out, newValue...)
		out = append(out, xmp[absCloseStart:]...)
		return out, nil
	case xmpPropertyAsAttribute:
		// Locate the attribute value bracketed by the quote characters
		// inside xmp[loc.Start:loc.End]. The range includes the leading
		// whitespace separator and the trailing quote.
		attrBytes := xmp[loc.Start:loc.End]
		valStart, valEnd, ok := attrValueRange(attrBytes)
		if !ok {
			return append([]byte(nil), xmp...), errXMPPropertyNotFound
		}
		out := make([]byte, 0, len(xmp)+len(newValue))
		absValStart := loc.Start + valStart
		absValEnd := loc.Start + valEnd
		out = append(out, xmp[:absValStart]...)
		out = append(out, newValue...)
		out = append(out, xmp[absValEnd:]...)
		return out, nil
	}
	return append([]byte(nil), xmp...), errXMPPropertyNotFound
}

// xmpPropertyRawValue returns a located property's current text: the
// element text for element form, the attribute value for attribute
// form, trimmed of surrounding whitespace.
func xmpPropertyRawValue(xmp []byte, loc xmpPropertyLocation) string {
	frag := xmp[loc.Start:loc.End]
	switch loc.Form {
	case xmpPropertyAsElement:
		_, openEnd, closeStart, ok := elementTextRange(frag)
		if !ok || closeStart < openEnd {
			return ""
		}
		return strings.TrimSpace(string(frag[openEnd:closeStart]))
	case xmpPropertyAsAttribute:
		vs, ve, ok := attrValueRange(frag)
		if !ok {
			return ""
		}
		return strings.TrimSpace(string(frag[vs:ve]))
	case xmpPropertyNotFound:
	}
	return ""
}

// attrValueRange walks a ` name="value"` attribute slice and returns
// byte offsets that bracket the inner value (exclusive of the quotes).
// Returns ok=false when the slice isn't shaped like an attribute.
func attrValueRange(attr []byte) (int, int, bool) {
	eq := bytes.IndexByte(attr, '=')
	if eq < 0 || eq+1 >= len(attr) {
		return 0, 0, false
	}
	pos := eq + 1
	for pos < len(attr) && isSpace(attr[pos]) {
		pos++
	}
	if pos >= len(attr) {
		return 0, 0, false
	}
	quote := attr[pos]
	if quote != '"' && quote != '\'' {
		return 0, 0, false
	}
	valStart := pos + 1
	valEnd := valStart
	for valEnd < len(attr) && attr[valEnd] != quote {
		valEnd++
	}
	if valEnd >= len(attr) {
		return 0, 0, false
	}
	return valStart, valEnd, true
}

// ensureNamespaceDecl adds xmlns:prefix="uri" to the first
// rdf:Description start tag if the namespace is not already declared.
// Also rewrites any wrong-URI bindings for the given prefix across the
// entire XMP buffer so properties using the canonical prefix (e.g.
// pdfaid) always bind to the canonical namespace URI.
func ensureNamespaceDecl(xmp []byte, prefix, uri string) ([]byte, error) {
	// Global rewrite pass: any xmlns:prefix="wrongURI" gets replaced with
	// xmlns:prefix="uri". Covers the multi-Description case where the
	// wrong binding lives outside the first start tag.
	out := rewriteNamespaceURI(xmp, prefix, uri)

	tagStart, tagEnd, err := findRDFDescriptionOpenRange(out)
	if err != nil {
		return out, err
	}
	tagBytes := out[tagStart:tagEnd]
	existing := collectNamespaceDecls(tagBytes)
	if existing[prefix] == uri {
		return out, nil
	}

	// Insert just before the closing `>` (or `/>`) of the start tag.
	closePos := tagEnd - 1
	for closePos > tagStart && (out[closePos] == '>' || out[closePos] == '/') {
		closePos--
	}
	insertAt := closePos + 1

	injection := fmt.Sprintf(` xmlns:%s="%s"`, prefix, uri)
	result := make([]byte, 0, len(out)+len(injection))
	result = append(result, out[:insertAt]...)
	result = append(result, injection...)
	result = append(result, out[insertAt:]...)
	return result, nil
}

// canonicalizePrefix renames every xmlns:X="uri" binding to
// xmlns:canonical="uri" and rewrites the corresponding X:local usages
// to canonical:local across the buffer. This is used when a producer
// bound the PDF/A-required namespace (e.g.
// http://www.aiim.org/pdfa/ns/id/) to a non-canonical prefix.
func canonicalizePrefix(xmp []byte, canonical, uri string) []byte {
	out := xmp
	for _, quote := range []byte{'"', '\''} {
		marker := fmt.Appendf(nil, `="%s"`, uri)
		altMarker := fmt.Appendf(nil, `='%s'`, uri)
		if quote == '\'' {
			marker = altMarker
		}
		for {
			idx := bytes.Index(out, marker)
			if idx < 0 {
				break
			}
			// Walk backward to locate the prefix in `xmlns:PREFIX=`.
			nameEnd := idx
			nameStart := nameEnd - 1
			for nameStart >= 0 {
				c := out[nameStart]
				if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '<' {
					nameStart++
					break
				}
				nameStart--
			}
			if nameStart < 0 {
				nameStart = 0
			}
			declName := out[nameStart:nameEnd]
			prefix := ""
			if p := bytes.IndexByte(declName, ':'); p >= 0 && bytes.HasPrefix(declName, []byte("xmlns:")) {
				prefix = string(declName[p+1:])
			}
			if prefix == "" || prefix == canonical {
				// Either a default-namespace declaration (xmlns=) or
				// already canonical: advance past this marker and
				// continue.
				next := idx + len(marker)
				if next >= len(out) {
					break
				}
				rewrittenHead := append([]byte(nil), out[:next]...)
				out = append(rewrittenHead, out[next:]...)
				break
			}
			// If the canonical prefix is already bound to this URI on the
			// element, renaming the alias would create a second
			// xmlns:<canonical> attribute. XML 1.0 forbids duplicate
			// attribute names on one element, so the packet would no longer
			// be well-formed and any conforming XMP parser would reject it.
			// Drop the alias declaration instead; the usage rewrite below
			// still merges the alias's properties into the existing
			// canonical namespace.
			declEnd := idx + len(marker)
			if bytes.Contains(out, fmt.Appendf(nil, `xmlns:%s="%s"`, canonical, uri)) ||
				bytes.Contains(out, fmt.Appendf(nil, `xmlns:%s='%s'`, canonical, uri)) {
				rewritten := make([]byte, 0, len(out)-(declEnd-nameStart))
				rewritten = append(rewritten, out[:nameStart]...)
				rewritten = append(rewritten, out[declEnd:]...)
				out = rewritten
			} else {
				newDecl := fmt.Appendf(nil, "xmlns:%s", canonical)
				rewritten := make([]byte, 0, len(out)+len(newDecl)-len(declName))
				rewritten = append(rewritten, out[:nameStart]...)
				rewritten = append(rewritten, newDecl...)
				rewritten = append(rewritten, out[nameEnd:]...)
				out = rewritten
			}

			// Rewrite uses: prefix:local → canonical:local. Walk the
			// buffer finding word boundaries.
			out = rewriteQualifiedName(out, prefix, canonical)
		}
	}
	return out
}

// rewriteQualifiedName replaces occurrences of `oldPrefix:` with
// `newPrefix:` when preceded by '<', '/', or a whitespace-like byte so
// that substrings inside PCDATA or longer identifiers are left alone.
func rewriteQualifiedName(xmp []byte, oldPrefix, newPrefix string) []byte {
	old := []byte(oldPrefix + ":")
	out := make([]byte, 0, len(xmp))
	i := 0
	for i < len(xmp) {
		idx := bytes.Index(xmp[i:], old)
		if idx < 0 {
			out = append(out, xmp[i:]...)
			break
		}
		abs := i + idx
		// Preceding byte must signal a name start: one of < / space tab
		// newline carriage-return.
		validPrev := abs == 0
		if !validPrev {
			p := xmp[abs-1]
			validPrev = p == '<' || p == '/' || p == ' ' || p == '\t' || p == '\n' || p == '\r'
		}
		if !validPrev {
			out = append(out, xmp[i:abs+len(old)]...)
			i = abs + len(old)
			continue
		}
		out = append(out, xmp[i:abs]...)
		out = append(out, []byte(newPrefix+":")...)
		i = abs + len(old)
	}
	return out
}

// rewriteNamespaceURI replaces every xmlns:prefix="X" declaration where X
// differs from the canonical uri with xmlns:prefix="uri". Handles both
// single- and double-quoted attribute values.
func rewriteNamespaceURI(xmp []byte, prefix, uri string) []byte {
	out := xmp
	for _, quote := range []byte{'"', '\''} {
		prefixMarker := fmt.Sprintf(`xmlns:%s=%c`, prefix, quote)
		for {
			idx := bytes.Index(out, []byte(prefixMarker))
			if idx < 0 {
				break
			}
			valueStart := idx + len(prefixMarker)
			end := bytes.IndexByte(out[valueStart:], quote)
			if end < 0 {
				break
			}
			currentURI := string(out[valueStart : valueStart+end])
			if currentURI == uri {
				// Move past this correct binding and keep searching.
				out = append([]byte(nil), out...)
				// No replacement, skip past it by searching from after.
				scanFrom := valueStart + end + 1
				if scanFrom >= len(out) {
					break
				}
				// Replace only the first occurrence of the marker before
				// scanFrom with a sentinel that won't match, then restore.
				// Simpler: reconstruct out so the next search skips past.
				out = append(append([]byte{}, out[:idx]...),
					append(fmt.Appendf(nil, `xmlns:%s=%c%s%c`, prefix, quote, currentURI, quote),
						out[valueStart+end+1:]...)...)
				// To avoid an infinite loop, break scanning on matching
				// URIs. Earlier iterations have already handled every
				// mismatch; reaching a match means the remaining
				// occurrences are correct too for the scanned bytes.
				break
			}
			replacement := fmt.Sprintf(`xmlns:%s=%c%s%c`, prefix, quote, uri, quote)
			out = append(append([]byte{}, out[:idx]...),
				append([]byte(replacement), out[valueStart+end+1:]...)...)
		}
	}
	return out
}

// elementTextRange identifies the byte ranges of the opening-tag body
// and the closing-tag opening for an element of the form
// `<ns:local ...>text</ns:local>`. It returns offsets RELATIVE to the
// passed element slice:
//
//	openStart:  byte after `<` of the opening tag
//	openEnd:    byte after the final `>` of the opening tag
//	closeStart: byte at the `<` of the closing `</ns:local>` tag
//
// Returns ok=false for self-closing elements and for any input that
// does not contain a matching close tag.
func elementTextRange(element []byte) (int, int, int, bool) {
	// Find the opening tag's start.
	openStart := bytes.IndexByte(element, '<')
	if openStart < 0 {
		return 0, 0, 0, false
	}
	// Find its closing `>`.
	openEnd := bytes.IndexByte(element[openStart:], '>')
	if openEnd < 0 {
		return 0, 0, 0, false
	}
	openEnd += openStart + 1
	// Self-closing?
	if openEnd >= 2 && element[openEnd-2] == '/' {
		return openStart + 1, openEnd, openEnd - 2, true
	}
	// Find the matching close tag. A naive reverse search for `</` is
	// correct because XMP properties are leaves. They do not contain
	// nested elements of the same name in practice.
	closeStart := bytes.LastIndex(element, []byte("</"))
	if closeStart < 0 || closeStart < openEnd {
		return 0, 0, 0, false
	}
	return openStart + 1, openEnd, closeStart, true
}

// findRDFDescriptionOpenRange returns the byte range of the first
// `<rdf:Description ...>` start tag in xmp. The range is [start, end)
// where start is the `<` and end is one past the final `>` / `/>`.
func findRDFDescriptionOpenRange(xmp []byte) (int, int, error) {
	dec := xml.NewDecoder(bytes.NewReader(xmp))
	dec.Strict = false
	dec.Entity = xml.HTMLEntity
	for {
		tokStart := dec.InputOffset()
		tok, err := dec.Token()
		if err != nil {
			return 0, 0, fmt.Errorf("no rdf:Description found: %w", err)
		}
		tokEnd := dec.InputOffset()
		startElem, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if isRDFDescription(startElem.Name) {
			return int(tokStart), int(tokEnd), nil
		}
	}
}

// findRDFDescriptionCloseOffset returns the byte offset of the `<`
// starting the first matching `</rdf:Description>` close tag in xmp.
// This is the insertion point for appending a new child element.
func findRDFDescriptionCloseOffset(xmp []byte) (int, error) {
	dec := xml.NewDecoder(bytes.NewReader(xmp))
	dec.Strict = false
	dec.Entity = xml.HTMLEntity
	seenOpen := false
	for {
		tokStart := dec.InputOffset()
		tok, err := dec.Token()
		if err != nil {
			return 0, fmt.Errorf("no </rdf:Description> found: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if isRDFDescription(t.Name) {
				seenOpen = true
			}
		case xml.EndElement:
			if seenOpen && isRDFDescription(t.Name) {
				return int(tokStart), nil
			}
		}
	}
}

func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\r' || b == '\n'
}

func isSpaceOrNL(b byte) bool {
	return b == ' ' || b == '\t' || b == '\r'
}

// skipAttrGap advances past whitespace and the element name (on the
// first call) until it reaches the start of the next attribute, the
// closing `>`, or the self-closing `/>`. On subsequent calls it just
// skips whitespace.
func skipAttrGap(tag []byte, pos int) int {
	// Skip the opening `<tagname` on the first iteration. A simple
	// heuristic: find the first whitespace after `<`.
	if pos == 0 && len(tag) > 0 && tag[0] == '<' {
		pos = 1
		for pos < len(tag) && !isSpace(tag[pos]) && tag[pos] != '>' && tag[pos] != '/' {
			pos++
		}
	}
	for pos < len(tag) && isSpace(tag[pos]) {
		pos++
	}
	return pos
}

// collectNamespaceDecls scans the raw bytes of a start tag for xmlns
// declarations and returns a prefix → URI map. The default namespace
// (xmlns=...) is mapped under the empty key.
func collectNamespaceDecls(tag []byte) map[string]string {
	result := make(map[string]string)
	s := string(tag)
	idx := 0
	for {
		i := bytes.Index([]byte(s[idx:]), []byte("xmlns"))
		if i < 0 {
			break
		}
		start := idx + i
		cur := start + len("xmlns")
		if cur >= len(s) {
			break
		}
		var prefix string
		if s[cur] == ':' {
			cur++
			nameStart := cur
			for cur < len(s) && s[cur] != '=' && !isSpace(s[cur]) {
				cur++
			}
			prefix = s[nameStart:cur]
		}
		for cur < len(s) && isSpace(s[cur]) {
			cur++
		}
		if cur >= len(s) || s[cur] != '=' {
			idx = cur
			continue
		}
		cur++
		for cur < len(s) && isSpace(s[cur]) {
			cur++
		}
		if cur >= len(s) || (s[cur] != '"' && s[cur] != '\'') {
			idx = cur
			continue
		}
		quote := s[cur]
		cur++
		valStart := cur
		for cur < len(s) && s[cur] != quote {
			cur++
		}
		if cur >= len(s) {
			break
		}
		result[prefix] = s[valStart:cur]
		idx = cur + 1
	}
	return result
}

// splitPrefix splits an attribute name like "pdfaid:conformance" into
// ("pdfaid", "conformance"). Names without a colon return ("", name).
func splitPrefix(name string) (prefix, local string) {
	for i := 0; i < len(name); i++ {
		if name[i] == ':' {
			return name[:i], name[i+1:]
		}
	}
	return "", name
}

// String returns a human-readable summary of a splice location for
// use in log messages and test failures.
func (l xmpPropertyLocation) String() string {
	switch l.Form {
	case xmpPropertyNotFound:
		return "not-found"
	case xmpPropertyAsElement:
		return fmt.Sprintf("element@[%d,%d)", l.Start, l.End)
	case xmpPropertyAsAttribute:
		return fmt.Sprintf("attribute@[%d,%d)", l.Start, l.End)
	}
	return "unknown"
}

// xmpToUTF8 transcodes a UTF-16 XMP packet (detected by its BOM) to UTF-8,
// returning the converted bytes and whether a conversion happened. UTF-8 and
// BOM-less packets pass through untouched: the splicer's byte-range
// operations require UTF-8 (ISO 19005-2/3 §6.6.2.1 and ISO 19005-4 §6.7.2.1
// demand it of conforming files anyway).
func xmpToUTF8(data []byte) ([]byte, bool) {
	// UTF-32 first: a UTF-32LE packet opens "<\x00\x00\x00", whose first two
	// bytes ("<\x00") also satisfy the UTF-16LE BOM-less test below, so the
	// wider unit must win. xmpUTF32ToUTF8 returns false for non-UTF-32 input.
	if out, ok := xmpUTF32ToUTF8(data); ok {
		return out, true
	}

	// Stream payloads often carry a trailing newline before endstream;
	// trim ASCII whitespace so an odd byte count cannot mask a UTF-16
	// packet (the parity of the CONTENT is what matters).
	for len(data) > 0 {
		switch data[len(data)-1] {
		case 0x0A, 0x0D, 0x20, 0x09:
			data = data[:len(data)-1]
			continue
		}
		break
	}
	if len(data) < 2 {
		return data, false
	}
	var be bool
	start := 2
	switch {
	case data[0] == 0xFE && data[1] == 0xFF:
		be = true
	case data[0] == 0xFF && data[1] == 0xFE:
		be = false
	case data[0] == 0x00 && data[1] == '<':
		// BOM-less UTF-16BE: every packet opens with "<?xpacket" or
		// "<?xml", so a NUL before '<' pins the byte order (the
		// 6-7-9-t04 fixture family ships exactly this shape).
		be = true
		start = 0
	case data[0] == '<' && data[1] == 0x00:
		be = false
		start = 0
	default:
		return data, false
	}
	units := make([]uint16, 0, (len(data)-start)/2)
	for i := start; i+1 < len(data); i += 2 {
		if be {
			units = append(units, uint16(data[i])<<8|uint16(data[i+1]))
		} else {
			units = append(units, uint16(data[i+1])<<8|uint16(data[i]))
		}
	}
	return []byte(string(utf16.Decode(units))), true
}

// xmpUTF32ToUTF8 transcodes a UTF-32 XMP packet (BOM or BOM-less, either byte
// order) to UTF-8, returning false for anything that is not UTF-32 so the
// caller falls through to the UTF-16 path. BOM-less detection keys on the
// three-NUL-per-code-point pattern that ASCII-range XMP content exhibits
// across its first two code points; UTF-16 and UTF-8 packets never match it.
// Keying on the pattern rather than a leading "<" tolerates a leading
// whitespace code point (the 6-7-9-t04 UTF-32LE fixture opens with an encoded
// newline before "<?xpacket"). Like the UTF-16 path, this defeats the
// byte-range splicer, which requires UTF-8 (ISO 19005-4 §6.7.2.1).
func xmpUTF32ToUTF8(data []byte) ([]byte, bool) {
	if len(data) < 8 {
		return nil, false
	}
	var be bool
	start := 0
	switch {
	case data[0] == 0x00 && data[1] == 0x00 && data[2] == 0xFE && data[3] == 0xFF:
		be, start = true, 4
	case data[0] == 0xFF && data[1] == 0xFE && data[2] == 0x00 && data[3] == 0x00:
		be, start = false, 4
	case data[1] == 0 && data[2] == 0 && data[3] == 0 && data[5] == 0 && data[6] == 0 && data[7] == 0:
		be = false // BOM-less little-endian: ASCII byte then three NULs.
	case data[0] == 0 && data[1] == 0 && data[2] == 0 && data[4] == 0 && data[5] == 0 && data[6] == 0:
		be = true // BOM-less big-endian: three NULs then ASCII byte.
	default:
		return nil, false
	}
	var b strings.Builder
	b.Grow((len(data) - start) / 4)
	for i := start; i+3 < len(data); i += 4 {
		var r uint32
		if be {
			r = uint32(data[i])<<24 | uint32(data[i+1])<<16 | uint32(data[i+2])<<8 | uint32(data[i+3])
		} else {
			r = uint32(data[i+3])<<24 | uint32(data[i+2])<<16 | uint32(data[i+1])<<8 | uint32(data[i])
		}
		// Out-of-range and surrogate code points are invalid in UTF-32; emit
		// the replacement character rather than corrupt the packet.
		if r > 0x10FFFF || (r >= 0xD800 && r <= 0xDFFF) {
			r = 0xFFFD
		}
		b.WriteRune(rune(r))
	}
	return []byte(b.String()), true
}
