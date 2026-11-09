package pdfgen

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
)

// errXMPUnsupported reports an XMP packet whose pikepdf rewrite pdfgen does
// not reproduce. Save with DisableFixMetadataVersion, or extend PikepdfXMP.
var errXMPUnsupported = errors.New("XMP packet pikepdf would rewrite in a way pdfgen does not reproduce")

const (
	xpacketBegin = "<?xpacket begin=\"\xef\xbb\xbf\" id=\"W5M0MpCehiHzreSzNTczkc9d\"?>\n"
	xpacketEnd   = "\n<?xpacket end=\"w\"?>\n"
	xmpEmpty     = "<x:xmpmeta xmlns:x=\"adobe:ns:meta/\" x:xmptk=\"pikepdf\">\n" +
		" <rdf:RDF xmlns:rdf=\"http://www.w3.org/1999/02/22-rdf-syntax-ns#\">\n </rdf:RDF>\n</x:xmpmeta>\n"
)

type xmlNode struct {
	kind     int // 0 element, 1 text, 2 comment
	name     string
	nsDecls  []xml.Attr
	attrs    []xml.Attr
	text     string
	children []*xmlNode
}

// PikepdfXMP returns the packet pikepdf writes back when it opens and
// closes a document's XMP on save (fix_metadata_version, the default): lxml
// parses the packet, every processing instruction (the xpacket wrapper among
// them) is stripped, and the tree is serialized by libxml2 with pretty_print
// inside a fresh xpacket wrapper. Namespace declarations move ahead of the
// other attributes, an empty element closes as <a/>, CDATA becomes escaped
// text, and an element whose children hold no text is re-indented.
//
// A DOCTYPE, an undecodable packet or a pdf:PDFVersion property (which
// pikepdf would rewrite) returns an error.
func PikepdfXMP(packet []byte) ([]byte, error) {
	if len(bytes.TrimSpace(packet)) == 0 {
		packet = []byte(xmpEmpty)
	}
	if bytes.Contains(packet, []byte("PDFVersion")) {
		return nil, fmt.Errorf("pdf:PDFVersion: %w", errXMPUnsupported)
	}
	dec := xml.NewDecoder(bytes.NewReader(packet))
	doc := &xmlNode{}
	stack := []*xmlNode{doc}
	for {
		tok, err := dec.RawToken()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parsing XMP: %w", err)
		}
		top := stack[len(stack)-1]
		switch t := tok.(type) {
		case xml.StartElement:
			n := &xmlNode{kind: 0, name: qname(t.Name)}
			for _, a := range t.Attr {
				a.Value = strings.NewReplacer("\t", " ", "\n", " ", "\r", " ").Replace(a.Value)
				if a.Name.Space == "xmlns" || (a.Name.Space == "" && a.Name.Local == "xmlns") {
					n.nsDecls = append(n.nsDecls, a)
				} else {
					n.attrs = append(n.attrs, a)
				}
			}
			top.children = append(top.children, n)
			stack = append(stack, n)
		case xml.EndElement:
			if len(stack) < 2 {
				return nil, fmt.Errorf("unbalanced end tag %s: %w", qname(t.Name), errXMPUnsupported)
			}
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if top == doc {
				continue
			}
			if k := len(top.children); k > 0 && top.children[k-1].kind == 1 {
				top.children[k-1].text += string(t)
			} else {
				top.children = append(top.children, &xmlNode{kind: 1, text: string(t)})
			}
		case xml.Comment:
			top.children = append(top.children, &xmlNode{kind: 2, text: string(t)})
		case xml.ProcInst:
		case xml.Directive:
			return nil, fmt.Errorf("directive <!%s>: %w", t, errXMPUnsupported)
		}
	}
	rootAt := -1
	for i, c := range doc.children {
		if c.kind == 0 {
			if rootAt >= 0 {
				return nil, fmt.Errorf("two root elements: %w", errXMPUnsupported)
			}
			rootAt = i
		}
	}
	if rootAt < 0 {
		return nil, fmt.Errorf("no root element: %w", errXMPUnsupported)
	}
	var b strings.Builder
	b.WriteString(xpacketBegin)
	for _, c := range doc.children[:rootAt] {
		dumpNode(&b, c, 0, true)
		b.WriteString("\n")
	}
	dumpNode(&b, doc.children[rootAt], 0, true)
	for _, c := range doc.children[rootAt+1:] {
		b.WriteString("\n")
		dumpNode(&b, c, 0, true)
	}
	b.WriteString("\n")
	b.WriteString(xpacketEnd)
	return []byte(b.String()), nil
}

func qname(n xml.Name) string {
	if n.Space == "" {
		return n.Local
	}
	return n.Space + ":" + n.Local
}

// dumpNode follows libxml2's xmlNodeDumpOutputInternal with a two-space
// indent: formatting stays on for an element only while none of its
// children is text, and once off it stays off for the whole subtree.
func dumpNode(b *strings.Builder, n *xmlNode, level int, format bool) {
	switch n.kind {
	case 1:
		b.WriteString(escapeText(n.text))
		return
	case 2:
		b.WriteString("<!--" + n.text + "-->")
		return
	}
	if format {
		for _, c := range n.children {
			if c.kind == 1 {
				format = false
				break
			}
		}
	}
	b.WriteString("<" + n.name)
	for _, a := range n.nsDecls {
		b.WriteString(" " + qname(a.Name) + "=\"" + escapeAttr(a.Value) + "\"")
	}
	for _, a := range n.attrs {
		b.WriteString(" " + qname(a.Name) + "=\"" + escapeAttr(a.Value) + "\"")
	}
	if len(n.children) == 0 {
		b.WriteString("/>")
		return
	}
	b.WriteString(">")
	if format {
		b.WriteString("\n")
	}
	for _, c := range n.children {
		if format && c.kind != 1 {
			b.WriteString(strings.Repeat("  ", level+1))
		}
		dumpNode(b, c, level+1, format)
		if format {
			b.WriteString("\n")
		}
	}
	if format {
		b.WriteString(strings.Repeat("  ", level))
	}
	b.WriteString("</" + n.name + ">")
}

func escapeText(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\r", "&#13;").Replace(s)
}

func escapeAttr(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;",
		"\n", "&#10;", "\r", "&#13;", "\t", "&#9;").Replace(s)
}

// rewriteXMP is pikepdf's update_xmp_pdfversion when the catalog has
// /Metadata: the packet is re-serialized and the catalog points at a new
// stream holding only /Type and /Subtype.
func (d *Doc) rewriteXMP() error {
	m := d.Root.Get("Metadata")
	if m == nil {
		return nil
	}
	s, ok := m.(*Stream)
	if !ok {
		return fmt.Errorf("catalog /Metadata is %T: %w", m, errXMPUnsupported)
	}
	data, ok, err := decodeAll(&s.Dict, s.Data)
	if err != nil {
		return fmt.Errorf("decoding /Metadata: %w", err)
	}
	if !ok {
		return fmt.Errorf("undecodable /Metadata: %w", errXMPUnsupported)
	}
	packet, err := PikepdfXMP(data)
	if err != nil {
		return err
	}
	d.Root.Set("Metadata", d.Stream(packet, "Type", N("Metadata"), "Subtype", N("XML")))
	return nil
}
