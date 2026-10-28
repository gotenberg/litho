package remediation

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/intconv"
)

// xmpRDFNS is the namespace of the RDF syntax XMP is written in.
const xmpRDFNS = "http://www.w3.org/1999/02/22-rdf-syntax-ns#"

// xmpSplice replaces the bytes start to end of a packet with text.
type xmpSplice struct {
	start, end int
	text       string
}

// extSchemaEntry is what [repairExtensionSchemaGrammar] reads of one entry of
// a pdfaExtension:schemas container.
type extSchemaEntry struct {
	// depth is the nesting depth of the entry's rdf:li.
	depth int
	// propertyArray records a pdfaSchema:property holding an RDF array.
	propertyArray bool
	// valueTypes removes the entry's pdfaSchema:valueType, when it has one.
	valueTypes *xmpSplice
	// unresolved records a field type of those value types that no basic
	// type answers.
	unresolved bool
}

// repairExtensionSchemaGrammar repairs in place, in every
// pdfaExtension:schemas block of the packet xmp, two defects of the extension
// schema container schema, keeping every declaration the producer wrote, and
// returns the packet with a note per repair.
//
// The container a block holds its entries in shall be an rdf:Bag (ISO
// 19005-2 and -3, 6.6.2.3.3 t1, ISO 19005-1, 6.7.8 t2), and one written as an
// rdf:Seq has its start and end tags renamed: the two arrays hold their items
// alike, so every entry keeps its meaning.
//
// A value type an entry defines is judged against the validators of the entry
// that counts for its namespace, and against the basic types alone when none
// counts (6.6.2.3.3 t17 and t8, 6.7.8 t18 and t9; see
// [compliance.XMPBasicValueType]). An entry with no property array never
// counts, so its value types serve no property: a property resolves its type
// through the entry declaring it. So when one of them names a field type no
// basic type answers, the entry's pdfaSchema:valueType goes, which the table
// of 6.6.2.3.3 makes optional, and every property keeps the type it had.
//
// A packet the decoder cannot read to its end is returned as it is.
//
// # Corpus evidence
//
//   - litho xmppage_2b.pdf: the packet of its third page holds its entries in
//     an rdf:Seq, and the packet of its fourth page an entry with no property
//     array whose value type has a field of type Dimensions. Both kept their
//     ISO 19005-2, 6.6.2.3.3 t1 and t17 findings after a PDF/A-2b and a
//     PDF/A-3b remediation.
//   - litho exttypes_2b.pdf: an entry with no property array has fields of
//     type Dimensions, Locale and Box, which kept three t17 findings, three
//     6.7.8 t18 at PDF/A-1b.
//   - litho schemaprefix_1b.pdf: its catalog packet holds its entries in an
//     rdf:Seq, which kept 6.7.8 t2 at PDF/A-1b and t1 at PDF/A-2b and 3b.
//   - All corpora: 641 files carry an extension schema block, and no other
//     holds either defect. Over their PDF/A-1b and PDF/A-2b remediations and
//     200 PDF/A-3b ones, only those three outputs change, each losing the
//     findings above and gaining none.
func repairExtensionSchemaGrammar(xmp []byte) ([]byte, []string) {
	dec := xml.NewDecoder(bytes.NewReader(xmp))
	dec.Strict = false
	dec.Entity = xml.HTMLEntity

	w := extSchemaGrammarWalk{xmp: xmp}
	for {
		startOff := intconv.Clamp(dec.InputOffset())
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return xmp, nil
		}
		endOff := intconv.Clamp(dec.InputOffset())

		ok := true
		switch t := tok.(type) {
		case xml.StartElement:
			ok = w.startElement(t, startOff, endOff)
		case xml.CharData:
			if w.refDepth > 0 {
				w.refText.Write(t)
			}
		case xml.EndElement:
			ok = w.endElement(startOff, endOff)
		}
		if !ok {
			return xmp, nil
		}
	}
	if len(w.edits) == 0 {
		return xmp, nil
	}

	slices.SortFunc(w.edits, func(a, b xmpSplice) int { return b.start - a.start })
	out := slices.Clone(xmp)
	for _, e := range w.edits {
		out = slices.Concat(out[:e.start], []byte(e.text), out[e.end:])
	}
	var fixes []string
	if w.containers > 0 {
		fixes = append(fixes, fmt.Sprintf("rewrote %d pdfaExtension:schemas container(s) as rdf:Bag", w.containers))
	}
	if w.deadLists > 0 {
		fixes = append(fixes, fmt.Sprintf("removed the value types of %d extension schema entr(ies) declaring no property", w.deadLists))
	}
	return out, fixes
}

// extSchemaGrammarWalk is the state of [repairExtensionSchemaGrammar] as it
// reads a packet: the names of the open elements, the depth of each
// construct it is inside (0 when outside), and the edits it has decided.
type extSchemaGrammarWalk struct {
	xmp   []byte
	stack []xml.Name
	edits []xmpSplice
	// containers and deadLists count the two repairs.
	containers, deadLists int

	schemasDepth   int
	containerDepth int
	// containerSeq records a container written as an rdf:Seq, and
	// containerOpen one that is not self-closing, so its end tag is renamed
	// too.
	containerSeq, containerOpen bool
	entry                       *extSchemaEntry
	propertyDepth               int
	valueTypeDepth              int
	valueTypeStart              int
	refDepth                    int
	refText                     strings.Builder
}

// startElement reads the start tag t, found at xmp[startOff:endOff], and
// reports false when the packet must be left as it is.
func (w *extSchemaGrammarWalk) startElement(t xml.StartElement, startOff, endOff int) bool {
	w.stack = append(w.stack, t.Name)
	depth := len(w.stack)
	if depth > maxXMPDepth {
		return false
	}
	switch {
	case w.schemasDepth == 0:
		if t.Name.Space == pdfaExtensionNS && t.Name.Local == "schemas" {
			w.schemasDepth = depth
		}
	case depth == w.schemasDepth+1 && w.containerDepth == 0:
		w.containerDepth = depth
		w.containerSeq = false
		if isRDFName(t.Name, "Seq") {
			edit, ok := renameTag(w.xmp, startOff, endOff, "Seq", "Bag")
			if !ok {
				return false
			}
			w.edits = append(w.edits, edit)
			w.containerSeq = true
			w.containerOpen = !bytes.HasSuffix(w.xmp[startOff:endOff], []byte("/>"))
			w.containers++
		}
	case w.containerDepth > 0 && depth == w.containerDepth+1 && w.entry == nil:
		if isRDFName(t.Name, "li") {
			w.entry = &extSchemaEntry{depth: depth}
		}
	case w.entry != nil:
		w.entryElement(t, startOff)
	}
	return true
}

// entryElement reads the start tag t below the rdf:li of the current entry: a
// field of the entry itself, written on the rdf:li or on the rdf:Description
// it holds, opens its property array or its value types, and a
// pdfaField:valueType inside those value types names a field type, as an
// element or as an attribute.
func (w *extSchemaGrammarWalk) entryElement(t xml.StartElement, startOff int) {
	depth := len(w.stack)
	entryField := depth == w.entry.depth+1 ||
		(depth == w.entry.depth+2 && isRDFName(w.stack[w.entry.depth], "Description"))
	switch {
	case entryField && t.Name.Space == pdfaSchemaNS && t.Name.Local == "property":
		w.propertyDepth = depth
	case entryField && t.Name.Space == pdfaSchemaNS && t.Name.Local == "valueType" && w.valueTypeDepth == 0:
		w.valueTypeDepth, w.valueTypeStart = depth, startOff
	case w.propertyDepth > 0 && depth == w.propertyDepth+1 && isRDFName(t.Name, "Seq", "Bag", "Alt"):
		w.entry.propertyArray = true
	case w.valueTypeDepth > 0 && depth > w.valueTypeDepth:
		if t.Name.Space == pdfaFieldNS && t.Name.Local == "valueType" {
			w.refDepth = depth
			w.refText.Reset()
		}
		for _, a := range t.Attr {
			if a.Name.Space == pdfaFieldNS && a.Name.Local == "valueType" && !compliance.XMPBasicValueType(strings.TrimSpace(a.Value)) {
				w.entry.unresolved = true
			}
		}
	}
}

// endElement reads the end tag found at xmp[startOff:endOff], empty for the
// end the decoder reports of a self-closing tag, and reports false when the
// packet must be left as it is.
func (w *extSchemaGrammarWalk) endElement(startOff, endOff int) bool {
	depth := len(w.stack)
	if depth == 0 {
		return false
	}
	w.stack = w.stack[:depth-1]
	switch {
	case w.refDepth == depth:
		if !compliance.XMPBasicValueType(strings.TrimSpace(w.refText.String())) {
			w.entry.unresolved = true
		}
		w.refDepth = 0
	case w.valueTypeDepth == depth:
		removal := elementRemoval(w.xmp, w.valueTypeStart, endOff)
		w.entry.valueTypes = &removal
		w.valueTypeDepth = 0
	case w.propertyDepth == depth:
		w.propertyDepth = 0
	case w.entry != nil && w.entry.depth == depth:
		if !w.entry.propertyArray && w.entry.valueTypes != nil && w.entry.unresolved {
			w.edits = append(w.edits, *w.entry.valueTypes)
			w.deadLists++
		}
		w.entry = nil
	case w.containerDepth == depth:
		// Any container but a self-closing one has an end tag to rename with
		// its start tag, or the packet is left as it is: the decoder closes
		// an element whose end tag it never met, and renaming the start tag
		// alone would break the XML.
		if w.containerSeq && w.containerOpen {
			edit, ok := renameTag(w.xmp, startOff, endOff, "Seq", "Bag")
			if !ok {
				return false
			}
			w.edits = append(w.edits, edit)
		}
		w.containerDepth = 0
	case w.schemasDepth == depth:
		w.schemasDepth = 0
	}
	return true
}

// isRDFName reports whether n is one of the RDF syntax elements local names.
func isRDFName(n xml.Name, local ...string) bool {
	return n.Space == xmpRDFNS && slices.Contains(local, n.Local)
}

// renameTag returns the splice that renames the start or end tag
// xmp[start:end], whose local name is from, to the local name to, keeping its
// prefix, its attributes and its spacing. ok is false when the bytes are no
// such tag.
func renameTag(xmp []byte, start, end int, from, to string) (xmpSplice, bool) {
	if start < 0 || end > len(xmp) || end-start < 3 || xmp[start] != '<' {
		return xmpSplice{}, false
	}
	nameStart := start + 1
	if xmp[nameStart] == '/' {
		nameStart++
	}
	nameEnd := nameStart
	for nameEnd < end && !strings.ContainsRune(" \t\r\n/>", rune(xmp[nameEnd])) {
		nameEnd++
	}
	qname := string(xmp[nameStart:nameEnd])
	localStart := nameStart + strings.LastIndexByte(qname, ':') + 1
	if string(xmp[localStart:nameEnd]) != from {
		return xmpSplice{}, false
	}
	return xmpSplice{start: localStart, end: nameEnd, text: to}, true
}

// elementRemoval returns the splice that removes the element xmp[start:end]
// with the indentation before it and the line break after it, as
// [findXMPProperty] brackets a property, so pretty-printed XMP keeps its
// lines.
func elementRemoval(xmp []byte, start, end int) xmpSplice {
	for start > 0 && (xmp[start-1] == ' ' || xmp[start-1] == '\t') {
		start--
	}
	if end < len(xmp) && xmp[end] == '\n' && (start == 0 || xmp[start-1] == '\n') {
		end++
	}
	return xmpSplice{start: start, end: end}
}
