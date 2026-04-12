package remediation

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
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

		// Element form: scan forward through child tokens until we hit
		// the matching child or the corresponding EndElement. We keep a
		// cursor over the byte stream so we can record the byte range
		// of the matching child element including its closing tag.
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
	// expanded name matches (nsURI, localName). We can't directly use
	// the StartElement.Attr slice because xml.Decoder doesn't expose
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
				// so we know where the child element ends in bytes.
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
	descEnd, err := findRDFDescriptionCloseOffset(xmp)
	if err != nil {
		return append([]byte(nil), xmp...), err
	}
	// Expand backwards to include the whitespace preceding the close
	// tag so the injected element adopts that indentation.
	insertAt := descEnd
	lineStart := descEnd
	for lineStart > 0 && xmp[lineStart-1] != '\n' {
		lineStart--
	}
	indent := string(xmp[lineStart:descEnd])

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
	if loc.Form != xmpPropertyAsElement {
		return append([]byte(nil), xmp...), errXMPPropertyNotFound
	}
	// The element range from findXMPProperty includes leading
	// indentation and the trailing newline. Re-parse just the slice
	// to locate the text-content byte range inside the element so we
	// only replace the value, not the surrounding tags.
	elementBytes := xmp[loc.Start:loc.End]
	openStart, openEnd, closeStart, ok := elementTextRange(elementBytes)
	if !ok {
		return append([]byte(nil), xmp...), errXMPPropertyNotFound
	}

	out := make([]byte, 0, len(xmp)+len(newValue))
	// Absolute byte ranges in the source buffer.
	absOpenEnd := loc.Start + openEnd
	absCloseStart := loc.Start + closeStart
	_ = openStart // captured for potential future use
	out = append(out, xmp[:absOpenEnd]...)
	out = append(out, newValue...)
	out = append(out, xmp[absCloseStart:]...)
	return out, nil
}

// ensureNamespaceDecl adds xmlns:prefix="uri" to the first
// rdf:Description start tag if the namespace is not already declared.
// Returns the new bytes plus true if a declaration was inserted. A
// nil-delta result (second return false) is returned unchanged when
// the namespace is already declared.
func ensureNamespaceDecl(xmp []byte, prefix, uri string) ([]byte, bool, error) {
	tagStart, tagEnd, err := findRDFDescriptionOpenRange(xmp)
	if err != nil {
		return append([]byte(nil), xmp...), false, err
	}
	tagBytes := xmp[tagStart:tagEnd]
	existing := collectNamespaceDecls(tagBytes)
	if existing[prefix] == uri {
		return append([]byte(nil), xmp...), false, nil
	}

	// Insert just before the closing `>` (or `/>`) of the start tag.
	closePos := tagEnd - 1
	for closePos > tagStart && (xmp[closePos] == '>' || xmp[closePos] == '/') {
		closePos--
	}
	// closePos now points at the last non-'>' non-'/' byte. The
	// injection should happen at closePos+1.
	insertAt := closePos + 1

	injection := fmt.Sprintf(` xmlns:%s="%s"`, prefix, uri)
	out := make([]byte, 0, len(xmp)+len(injection))
	out = append(out, xmp[:insertAt]...)
	out = append(out, injection...)
	out = append(out, xmp[insertAt:]...)
	return out, true, nil
}

// errXMPPropertyNotFound is returned by the splicer helpers when a
// property lookup fails. It is kept internal; callers switch on it via
// [errors.Is].
var errXMPPropertyNotFound = fmt.Errorf("xmp property not found")

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

// --- small byte helpers ---

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

// xmpDebugDump returns a human-readable summary of a splice location for
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
