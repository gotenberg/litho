package compliance

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// maxXMPTreeDepth bounds the element nesting [xmpParseTree] keeps. Anti-DoS
// guard, not spec-mandated: an XMP property nests a container, an item, a
// structure and its fields, and an extension schema declaration nests eight
// levels below rdf:RDF, so 64 keeps every real packet whole while a crafted
// packet cannot drive the recursive walks below arbitrarily deep.
const maxXMPTreeDepth = 64

// xmpElem is one XML element of a packet, with its character data.
type xmpElem struct {
	Name  xml.Name
	attrs []xml.Attr
	text  strings.Builder
	kids  []*xmpElem
}

// xmpParseTree reads packet into an element tree under a synthetic root,
// repairing what veraPDF repairs first (see [xmpRepairForParse]). A packet that
// does not parse yields the elements read before the error; elements nested
// deeper than [maxXMPTreeDepth] are dropped.
func xmpParseTree(packet []byte) *xmpElem {
	root := &xmpElem{}
	dec := xml.NewDecoder(bytes.NewReader(xmpRepairForParse(packet)))
	dec.CharsetReader = xmpPassThroughCharset
	dec.Strict = false
	stack := []*xmpElem{root}
	skipped := 0 // open elements below the depth bound
	for {
		tok, err := dec.Token()
		if err != nil {
			return root
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if skipped > 0 || len(stack) > maxXMPTreeDepth {
				skipped++
				continue
			}
			e := &xmpElem{Name: t.Name, attrs: slices.Clone(t.Attr)}
			parent := stack[len(stack)-1]
			parent.kids = append(parent.kids, e)
			stack = append(stack, e)
		case xml.EndElement:
			if skipped > 0 {
				skipped--
				continue
			}
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			if skipped == 0 {
				stack[len(stack)-1].text.Write(t)
			}
		}
	}
}

// XMPNodeKind is the form of an XMP value in the XMP data model (ISO 16684-1,
// 6.2): a simple value, a structure, or one of the three arrays.
type XMPNodeKind int

// The value forms, as XMPCore's PropertyOptions tell them apart.
const (
	// XMPSimple is a simple value, qualified or not.
	XMPSimple XMPNodeKind = iota
	// XMPStruct is a structure, its fields the node's children.
	XMPStruct
	// XMPBag is an unordered array.
	XMPBag
	// XMPSeq is an ordered array.
	XMPSeq
	// XMPAlt is an alternative array.
	XMPAlt
)

// XMPNode is one XMP property, structure field or array item as XMPCore reads
// it from RDF/XML. The remediation declares a property's type from the same
// reading the validator judges it by, so the two cannot disagree on a shape.
type XMPNode struct {
	NS, Name string // namespace URI and local name; empty for an array item
	Kind     XMPNodeKind
	Value    string // the text of a simple value
	Lang     bool   // the value carries an xml:lang qualifier
	Children []*XMPNode
}

// XML namespaces the value reader tells apart from property namespaces.
const (
	xmlNamespace              = "http://www.w3.org/XML/1998/namespace"
	xmpPdfaExtensionNamespace = "http://www.aiim.org/pdfa/ns/extension/"
)

// xmpPropertyAttr reports whether a is an RDF property attribute (RDF 1.1
// XML Syntax, 2.5), as opposed to RDF syntax, xml: or a namespace binding.
func xmpPropertyAttr(a xml.Attr) bool {
	switch a.Name.Space {
	case "", xmpRDFNamespace, xmlNamespace, "xmlns", "http://www.w3.org/2000/xmlns/":
		return false
	}
	return true
}

// XMPProperties returns the properties packet holds, read as [XMPNode]
// values in document order. A packet that does not parse yields the
// properties read before the error.
func XMPProperties(packet []byte) []*XMPNode {
	return xmpTopLevelNodes(xmpParseTree(packet))
}

// xmpTopLevelNodes returns the properties every rdf:Description of the packet
// carries, as attributes or elements, in document order.
func xmpTopLevelNodes(root *xmpElem) []*XMPNode {
	var out []*XMPNode
	var walk func(e *xmpElem, depth int)
	walk = func(e *xmpElem, depth int) {
		if depth > maxXMPTreeDepth {
			return
		}
		for _, k := range e.kids {
			if k.Name.Space == xmpRDFNamespace && k.Name.Local == "Description" {
				out = append(out, xmpDescriptionFields(k)...)
				continue
			}
			walk(k, depth+1)
		}
	}
	walk(root, 0)
	return out
}

// xmpDescriptionFields returns the properties an rdf:Description holds: its
// property attributes, which are simple values, and its property elements.
func xmpDescriptionFields(desc *xmpElem) []*XMPNode {
	var out []*XMPNode
	for _, a := range desc.attrs {
		if xmpPropertyAttr(a) {
			out = append(out, &XMPNode{NS: a.Name.Space, Name: a.Name.Local, Value: a.Value})
		}
	}
	for _, k := range desc.kids {
		if k.Name.Space == xmpRDFNamespace {
			continue
		}
		out = append(out, xmpPropertyNode(k, 0))
	}
	return out
}

// xmpPropertyNode reads the property element e, or an rdf:li item, into its
// XMP value, following the RDF/XML forms ISO 16684-1, 7.9 serializes: an
// rdf:parseType="Resource" structure, an rdf:resource URI, property attributes
// on an empty element, a nested rdf:Bag, rdf:Seq, rdf:Alt or rdf:Description,
// or text. A structure holding rdf:value is the qualified simple value it
// names. An element holding other elements without saying what they are reads
// as a structure.
func xmpPropertyNode(e *xmpElem, depth int) *XMPNode {
	n := &XMPNode{NS: e.Name.Space, Name: e.Name.Local}
	if e.Name.Space == xmpRDFNamespace && e.Name.Local == "li" {
		n.NS, n.Name = "", ""
	}
	var propAttrs []xml.Attr
	parseResource := false
	for _, a := range e.attrs {
		switch {
		case a.Name.Space == xmlNamespace && a.Name.Local == "lang":
			n.Lang = true
		case a.Name.Space == xmpRDFNamespace && a.Name.Local == "parseType":
			parseResource = a.Value == "Resource"
		case a.Name.Space == xmpRDFNamespace && a.Name.Local == "resource":
			n.Value = a.Value
			return n
		case xmpPropertyAttr(a):
			propAttrs = append(propAttrs, a)
		}
	}
	if depth > maxXMPTreeDepth {
		return n
	}
	if parseResource {
		n.Kind = XMPStruct
		xmpStructFields(n, nil, e.kids, depth)
		return n
	}
	if len(e.kids) == 0 {
		if len(propAttrs) > 0 {
			n.Kind = XMPStruct
			xmpStructFields(n, propAttrs, nil, depth)
			return n
		}
		n.Value = e.text.String()
		return n
	}
	first := e.kids[0]
	if first.Name.Space == xmpRDFNamespace {
		switch first.Name.Local {
		case "Bag", "Seq", "Alt":
			n.Kind = map[string]XMPNodeKind{"Bag": XMPBag, "Seq": XMPSeq, "Alt": XMPAlt}[first.Name.Local]
			for _, li := range first.kids {
				if li.Name.Space == xmpRDFNamespace && li.Name.Local == "li" {
					n.Children = append(n.Children, xmpPropertyNode(li, depth+1))
				}
			}
			return n
		case "Description":
			n.Kind = XMPStruct
			var attrs []xml.Attr
			for _, a := range first.attrs {
				if xmpPropertyAttr(a) {
					attrs = append(attrs, a)
				}
			}
			xmpStructFields(n, attrs, first.kids, depth)
			return n
		}
	}
	n.Kind = XMPStruct
	xmpStructFields(n, propAttrs, e.kids, depth)
	return n
}

// xmpStructFields fills the structure n with the fields attrs and kids write,
// or turns it into the qualified simple value an rdf:value field names.
func xmpStructFields(n *XMPNode, attrs []xml.Attr, kids []*xmpElem, depth int) {
	for _, a := range attrs {
		n.Children = append(n.Children, &XMPNode{NS: a.Name.Space, Name: a.Name.Local, Value: a.Value})
	}
	for _, k := range kids {
		if k.Name.Space == xmpRDFNamespace {
			if k.Name.Local == "value" {
				n.Kind, n.Value, n.Children = XMPSimple, k.text.String(), nil
				return
			}
			continue
		}
		n.Children = append(n.Children, xmpPropertyNode(k, depth+1))
	}
}

// field returns the field ns:name of the structure n, or nil.
func (n *XMPNode) field(ns, name string) *XMPNode {
	if n == nil || n.Kind != XMPStruct {
		return nil
	}
	for _, c := range n.Children {
		if c.NS == ns && c.Name == name {
			return c
		}
	}
	return nil
}

// isArray reports whether n is one of the three arrays.
func (n *XMPNode) isArray() bool {
	return n != nil && (n.Kind == XMPBag || n.Kind == XMPSeq || n.Kind == XMPAlt)
}

// xmpExtType is a value type an extension schema entry defines
// (pdfaSchema:valueType): a structure whose fields live in ns.
type xmpExtType struct {
	NS     string
	fields map[string]string // field name → declared value type
}

// xmpExtEntry is one pdfaSchema entry of pdfaExtension:schemas as veraPDF
// registers it: the properties whose declared type it knows, and the types it
// defines, keyed by their simplified name.
type xmpExtEntry struct {
	NS    string
	props map[string]string // property name → declared value type
	types map[string]xmpExtType
	// textTypes are the types the entry defines with no field, which veraPDF
	// registers as Text.
	textTypes map[string]bool
	// base is the main packet's entry for the same namespace when this one is
	// a component packet's: veraPDF builds the component entry's validators
	// on a copy of the main entry's, so the main entry's types resolve here
	// too, and an array type resolves its items through the main entry alone
	// (see [xmpValueMatches]).
	base *xmpExtEntry
}

// structType returns the structure type t the entry resolves, its own
// definitions first.
func (e *xmpExtEntry) structType(t string) (xmpExtType, bool) {
	if st, ok := e.types[t]; ok {
		return st, true
	}
	if e.textTypes[t] || e.base == nil {
		return xmpExtType{}, false
	}
	return e.base.structType(t)
}

// textType reports whether t is a type the entry resolves as one defined with
// no field, which veraPDF registers as Text.
func (e *xmpExtEntry) textType(t string) bool {
	if e.textTypes[t] {
		return true
	}
	if _, ok := e.types[t]; ok || e.base == nil {
		return false
	}
	return e.base.textType(t)
}

// xmpExtEntries reads the pdfaExtension:schemas bags of the packet into the
// entry each namespace resolves through, the last entry naming it winning, as
// veraPDF's SchemasDefinitionCreator puts one definition per namespace. An
// entry counts only with a namespace URI and a property array, and declares a
// property only with a type it knows (SchemasDefinition.registerProperty), the
// first declaration of a name winning. inherited holds the main packet's
// entries when nodes are a component packet's, and nil otherwise.
func xmpExtEntries(part int, nodes []*XMPNode, inherited map[string]*xmpExtEntry) map[string]*xmpExtEntry {
	entries := make(map[string]*xmpExtEntry)
	for _, top := range nodes {
		if top.NS != xmpPdfaExtensionNamespace || top.Name != "schemas" || !top.isArray() {
			continue
		}
		for _, item := range top.Children {
			nsNode := item.field(xmpPdfaSchemaPrefix, "namespaceURI")
			propArr := item.field(xmpPdfaSchemaPrefix, "property")
			if nsNode == nil || nsNode.Kind != XMPSimple || !propArr.isArray() {
				continue
			}
			entry := &xmpExtEntry{
				NS:        strings.TrimSpace(nsNode.Value),
				props:     make(map[string]string),
				types:     make(map[string]xmpExtType),
				textTypes: make(map[string]bool),
			}
			entry.base = inherited[entry.NS]
			if vtArr := item.field(xmpPdfaSchemaPrefix, "valueType"); vtArr.isArray() {
				for _, typeNode := range vtArr.Children {
					entry.registerType(typeNode)
				}
			}
			for _, propNode := range propArr.Children {
				name, vt := propNode.field(xmpPdfaPropertyPrefix, "name"), propNode.field(xmpPdfaPropertyPrefix, "valueType")
				if name == nil || vt == nil || name.Kind != XMPSimple || vt.Kind != XMPSimple {
					continue
				}
				if _, seen := entry.props[name.Value]; seen || !entry.knownType(part, vt.Value) {
					continue
				}
				entry.props[name.Value] = vt.Value
			}
			entries[entry.NS] = entry
		}
	}
	return entries
}

// registerType records the pdfaSchema:valueType entry typeNode, as veraPDF's
// ValidatorsContainerCreator.registerTypeNode does: a type with no field is
// Text, and one with fields is a structure of its namespace.
func (e *xmpExtEntry) registerType(typeNode *XMPNode) {
	typ, ns := typeNode.field(xmpPdfaTypePrefix, "type"), typeNode.field(xmpPdfaTypePrefix, "namespaceURI")
	if typ == nil || typ.Kind != XMPSimple {
		return
	}
	fields := make(map[string]string)
	if fieldArr := typeNode.field(xmpPdfaTypePrefix, "field"); fieldArr.isArray() {
		for _, f := range fieldArr.Children {
			name, vt := f.field(xmpPdfaFieldPrefix, "name"), f.field(xmpPdfaFieldPrefix, "valueType")
			if name != nil && vt != nil && name.Kind == XMPSimple && vt.Kind == XMPSimple {
				fields[name.Value] = vt.Value
			}
		}
	}
	key := xmpValidatorKey(typ.Value)
	switch {
	case len(fields) == 0:
		e.textTypes[key] = true
		delete(e.types, key)
	case ns != nil && ns.Kind == XMPSimple:
		e.types[key] = xmpExtType{NS: strings.TrimSpace(ns.Value), fields: fields}
		delete(e.textTypes, key)
	}
}

// knownType reports whether veraPDF's ValidatorsContainer.isKnownType answers
// true for typ in this entry: its array wrappers stripped, a type of the part
// or of the entry.
func (e *xmpExtEntry) knownType(part int, typ string) bool {
	t := xmpArrayItemKey(typ)
	if _, ok := e.structType(t); ok || e.textType(t) {
		return true
	}
	return xmpValidatorBuiltin(part, t)
}

// xmpChoiceAnywhere is the choice qualifier veraPDF's getSimplifiedType
// removes with String.replaceAll, so wherever it appears, unlike
// [xmpChoicePattern].
//
//nolint:gochecknoglobals // compiled-once pattern.
var xmpChoiceAnywhere = regexp.MustCompile(`(open |closed )?(choice |choice$)(of )?`)

// xmpValidatorKey mirrors veraPDF's ValidatorsContainer.getSimplifiedType:
// lower case, choice qualifiers removed, an empty type read as text, and a bare
// array keyword completed as an array of text.
func xmpValidatorKey(typ string) string {
	t := strings.TrimSpace(xmpChoiceAnywhere.ReplaceAllString(strings.ToLower(typ), ""))
	if t == "" {
		return "text"
	}
	if strings.HasSuffix(t, "lang alt") {
		return t
	}
	for _, a := range []string{"bag", "alt", "seq"} {
		if strings.HasSuffix(t, a) {
			return t + " text"
		}
	}
	return t
}

// xmpValidatorBuiltin reports whether veraPDF's predefined validators
// container for the part holds key.
func xmpValidatorBuiltin(part int, key string) bool {
	return xmpPredefinedValueTypes1[key] || part != 1 && xmpPredefinedValueTypes23[key]
}

// xmpSimpleTypePatterns are the patterns veraPDF's SimpleTypeValidator and
// ValidatorsContainerCreator register for the simple types that constrain
// their text. The other simple types accept any text.
//
//nolint:gochecknoglobals // compiled-once patterns.
var xmpSimpleTypePatterns = map[string]*regexp.Regexp{
	"boolean":  regexp.MustCompile(`^(True|False)$`),
	"integer":  regexp.MustCompile(`^[+-]?\d+$`),
	"real":     regexp.MustCompile(`^([+-]?\d+\.?\d*|[+-]?\d*\.?\d+)$`),
	"mimetype": regexp.MustCompile(`^[-\w+.]+/[-\w+.]+$`),
}

// xmpGPSCoordinatePattern returns the GPSCoordinate pattern veraPDF registers
// for the part.
func xmpGPSCoordinatePattern(part int) *regexp.Regexp {
	if part == 1 {
		return xmpGPSCoordinate1
	}
	return xmpGPSCoordinate23
}

//nolint:gochecknoglobals // compiled-once patterns.
var (
	xmpGPSCoordinate1  = regexp.MustCompile(`^\d{2},\d{2}[,.]\d{2}[NSEW]$`)
	xmpGPSCoordinate23 = regexp.MustCompile(`^\d{1,3},\d{1,2}(,\d{1,2}|\.\d+)[NSEW]$`)
	// xmpISO8601Loose is the date grammar XMPCore's ISO8601Converter accepts,
	// read loosely: each field is digits of any width, since the converter
	// clamps an out-of-range field rather than refusing it.
	xmpISO8601Loose = regexp.MustCompile(`^-?\d+(-\d+(-\d+)?)?(T\d+:\d+(:\d+(\.\d+)?)?(Z|[+-]\d+:\d+)?)?$`)
)

// xmpValueMatches reports whether n is a value of type typ, as veraPDF's
// ValidatorsContainer.validate decides it for the declarations of entry at the
// given part.
//
// An array type resolves its item type through entry.base when entry has one:
// veraPDF's copy of the main entry's validators keeps the main entry's array
// validators, which look their item types up in the main entry. So a component
// packet's array of a type only the component packet defines fails there,
// measured on veraPDF 1.30.2 at PDF/A-2b, while the same array passes in a
// component packet whose namespace the main packet does not extend.
//
// A structure type the part predefines is checked for being a structure only:
// veraPDF also checks its fields against the type, and reading this looser
// never reports what veraPDF passes. An XPath is likewise checked for being a
// simple value only. Text is trimmed before the Boolean, Integer, Real, MIME
// type and GPS patterns, which veraPDF does not do, for the same reason.
func xmpValueMatches(n *XMPNode, typ string, entry *xmpExtEntry, part, depth int) bool {
	if depth > maxXMPTreeDepth {
		return true
	}
	t := xmpValidatorKey(typ)
	if t == "any" {
		return true
	}
	for _, a := range []struct {
		prefix string
		kind   XMPNodeKind
	}{{"bag ", XMPBag}, {"alt ", XMPAlt}, {"seq ", XMPSeq}} {
		item, ok := strings.CutPrefix(t, a.prefix)
		if !ok {
			continue
		}
		if n.Kind != a.kind {
			return false
		}
		scope := entry
		if entry.base != nil {
			scope = entry.base
		}
		for _, c := range n.Children {
			if !xmpValueMatches(c, item, scope, part, depth+1) {
				return false
			}
		}
		return true
	}
	if st, ok := entry.structType(t); ok {
		if n.Kind != XMPStruct {
			return false
		}
		for _, c := range n.Children {
			ft, declared := st.fields[c.Name]
			if !declared || c.NS != st.NS || !xmpValueMatches(c, ft, entry, part, depth+1) {
				return false
			}
		}
		return true
	}
	if entry.textType(t) {
		return n.Kind == XMPSimple
	}
	switch t {
	case "lang alt":
		// LangAltValidator: an alternative array that is alt-text (an item
		// carries xml:lang, XMPNodeUtils.detectAltText), or has no item.
		if n.Kind != XMPAlt {
			return false
		}
		return len(n.Children) == 0 || slices.ContainsFunc(n.Children, func(c *XMPNode) bool { return c.Lang })
	case "text", "propername", "agentname", "rational", "renditionclass", "locale", "uri", "url", "xpath":
		return n.Kind == XMPSimple
	case "date":
		v := strings.TrimSpace(n.Value)
		return n.Kind == XMPSimple && (v == "" || xmpISO8601Loose.MatchString(v))
	case "gpscoordinate":
		return n.Kind == XMPSimple && xmpGPSCoordinatePattern(part).MatchString(strings.TrimSpace(n.Value))
	}
	if re, ok := xmpSimpleTypePatterns[t]; ok {
		return n.Kind == XMPSimple && re.MatchString(strings.TrimSpace(n.Value))
	}
	if xmpValidatorBuiltin(part, t) {
		return n.Kind == XMPStruct
	}
	// veraPDF answers false for a type it does not know; the property is then
	// undeclared, which [xmpExtEntries] already leaves out.
	return false
}

// xmpCheckExtensionValueTypes reports every property of packet whose value
// does not match the type an extension schema declares for it, under ISO
// 19005-1, 6.7.9 test 3 and ISO 19005-2/3, 6.6.2.3.1 test 2: a property of an
// extension schema shall be of the value type the schema gives it (ISO
// 19005-2, 6.6.2.3.2; ISO 19005-1, 6.7.8).
//
// A property resolves through the schema entry of its namespace in packet, or
// failing that in inherited, the entries of the document's main packet, which
// govern the component packets of parts 2 and 3. A predefined property is left
// to the predefined audit, and a property in reported, which
// [xmpCheckExtensionPropertyTypes] already stated the rule for, is not stated
// twice.
//
// # Corpus evidence
//
//   - verapdf PDF_A-1b__veraPDF_test_suite_6-7-2-t03-fail-i.pdf: remediation
//     declared its xmpTPg:Fonts, a bag of stFnt structures, as "bag Text".
//     veraPDF 1.30.2 failed the output under 6.7.9 t3, and litho, which read
//     no value against its declaration, passed it.
//   - All corpora: over the 640 files whose packets carry an extension
//     schema, litho now reports 6.7.9 t3 at PDF/A-1b and 6.6.2.3.1 t2 at
//     PDF/A-2b exactly where veraPDF does in 639, where it did in 636. verapdf
//     PDF_UA-2__5-t05-fail-a.pdf declares pdfuaid:rev an Integer and holds
//     "2024a", and 6-6-2-3-3-t04-fail-b.pdf and t04-fail-c.pdf define a type
//     whose one field lacks its name or its value type, which leaves a type
//     of no field, Text, that their structure fails. The one left,
//     6-6-2-3-3-t03-fail-c.pdf, defines a type with no namespace URI, which
//     veraPDF does not register, so it reads the property as undeclared.
func xmpCheckExtensionValueTypes(packet []byte, part int, inherited map[string]*xmpExtEntry, reported map[[2]string]bool, results *[]Violation) {
	if part < 1 || part > 3 {
		return
	}
	nodes := xmpTopLevelNodes(xmpParseTree(packet))
	own := xmpExtEntries(part, nodes, inherited)
	if len(own) == 0 && len(inherited) == 0 {
		return
	}
	stated := make(map[[2]string]bool)
	for _, n := range nodes {
		if n.NS == xmpPdfaExtensionNamespace || XMPPredefinedProperty(part, n.NS, n.Name) {
			continue
		}
		entry, typ := xmpExtResolve(own, inherited, n.NS, n.Name)
		key := [2]string{n.NS, n.Name}
		if typ == "" || reported[key] || stated[key] {
			continue
		}
		if xmpValueMatches(n, typ, entry, part, 0) {
			continue
		}
		stated[key] = true
		*results = append(*results, xmpPropertyTypeRule(part, n.NS, n.Name,
			fmt.Sprintf("value does not match its declared type %q", typ))...)
	}
}

// XMPExtensionPropertyChecker reads packet once and returns a function
// reporting whether every occurrence of a property ns:name in it is declared by
// an extension schema, with a value type veraPDF resolves, and holds a value of
// that type: whether ISO 19005-1, 6.7.9 and ISO 19005-2/3, 6.6.2.3.1 accept it
// through its declaration. mainPacket is the document's main packet when
// packet is a component's, whose extension schemas govern it at parts 2 and 3,
// and nil otherwise. A predefined property, and a part without extension
// schemas, answer false.
func XMPExtensionPropertyChecker(packet, mainPacket []byte, part int) func(ns, name string) bool {
	if part < 1 || part > 3 {
		return func(string, string) bool { return false }
	}
	nodes := XMPProperties(packet)
	inherited := xmpMainExtensionEntries(part, mainPacket)
	own := xmpExtEntries(part, nodes, inherited)
	return func(ns, name string) bool {
		if XMPPredefinedProperty(part, ns, name) {
			return false
		}
		found := false
		for _, n := range nodes {
			if n.NS != ns || n.Name != name {
				continue
			}
			entry, typ := xmpExtResolve(own, inherited, ns, name)
			if typ == "" || !xmpValueMatches(n, typ, entry, part, 0) {
				return false
			}
			found = true
		}
		return found
	}
}

// xmpExtensionDeclared reads packet once and returns a function reporting
// whether its extension schemas, or inherited, the main packet's, declare a
// property ns:name with a type veraPDF resolves, the property being no
// predefined one: the properties veraPDF validates through their declaration.
func xmpExtensionDeclared(part int, packet []byte, inherited map[string]*xmpExtEntry) func(ns, name string) bool {
	if part < 1 || part > 3 {
		return func(string, string) bool { return false }
	}
	own := xmpExtEntries(part, XMPProperties(packet), inherited)
	return func(ns, name string) bool {
		if XMPPredefinedProperty(part, ns, name) {
			return false
		}
		_, typ := xmpExtResolve(own, inherited, ns, name)
		return typ != ""
	}
}

// xmpExtResolve returns the schema entry that declares ns:name and the type
// it declares: the packet's own entry for the namespace, or failing that the
// main packet's. The type is "" when neither declares the property.
func xmpExtResolve(own, inherited map[string]*xmpExtEntry, ns, name string) (*xmpExtEntry, string) {
	if entry := own[ns]; entry != nil && entry.props[name] != "" {
		return entry, entry.props[name]
	}
	if entry := inherited[ns]; entry != nil && entry.props[name] != "" {
		return entry, entry.props[name]
	}
	return nil, ""
}

// xmpArrayItemKey returns the simplified item type of typ, its array wrappers
// removed.
func xmpArrayItemKey(typ string) string {
	t := xmpValidatorKey(typ)
	for {
		stripped := false
		for _, a := range xmpValueTypeArrayPrefixes {
			if rest, ok := strings.CutPrefix(t, a); ok {
				t, stripped = rest, true
				break
			}
		}
		if !stripped {
			return t
		}
	}
}

// xmpMainExtensionEntries returns the extension schema entries of the main
// packet, which the component packets of parts 2 and 3 resolve through.
func xmpMainExtensionEntries(part int, packet []byte) map[string]*xmpExtEntry {
	if part != 2 && part != 3 || len(packet) == 0 {
		return nil
	}
	return xmpExtEntries(part, xmpTopLevelNodes(xmpParseTree(packet)), nil)
}
