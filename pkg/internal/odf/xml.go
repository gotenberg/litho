package odf

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
)

// The namespaces of ODF 1.3, Part 3, 1.5 (Table 1), and the LibreOffice
// extension namespaces its documents use.
const (
	NSOffice    = "urn:oasis:names:tc:opendocument:xmlns:office:1.0"
	NSStyle     = "urn:oasis:names:tc:opendocument:xmlns:style:1.0"
	NSText      = "urn:oasis:names:tc:opendocument:xmlns:text:1.0"
	NSTable     = "urn:oasis:names:tc:opendocument:xmlns:table:1.0"
	NSDraw      = "urn:oasis:names:tc:opendocument:xmlns:drawing:1.0"
	NSFO        = "urn:oasis:names:tc:opendocument:xmlns:xsl-fo-compatible:1.0"
	NSSVG       = "urn:oasis:names:tc:opendocument:xmlns:svg-compatible:1.0"
	NSXLink     = "http://www.w3.org/1999/xlink"
	NSDC        = "http://purl.org/dc/elements/1.1/"
	NSMeta      = "urn:oasis:names:tc:opendocument:xmlns:meta:1.0"
	NSNumber    = "urn:oasis:names:tc:opendocument:xmlns:datastyle:1.0"
	NSForm      = "urn:oasis:names:tc:opendocument:xmlns:form:1.0"
	NSManifest  = "urn:oasis:names:tc:opendocument:xmlns:manifest:1.0"
	NSXML       = "http://www.w3.org/XML/1998/namespace"
	NSLOExt     = "urn:org:documentfoundation:names:experimental:office:xmlns:loext:1.0"
	NSOfficeOOO = "http://openoffice.org/2009/office"
)

// prefixes maps the namespaces property names are keyed by to their
// conventional prefixes (ODF 1.3, Part 3, 1.5): a document may bind any
// prefix, so [Props] keys use these.
var prefixes = map[string]string{
	NSOffice: "office", NSStyle: "style", NSText: "text", NSTable: "table",
	NSDraw: "draw", NSFO: "fo", NSSVG: "svg", NSXLink: "xlink", NSDC: "dc",
	NSMeta: "meta", NSNumber: "number", NSForm: "form", NSManifest: "manifest",
	NSXML: "xml", NSLOExt: "loext", NSOfficeOOO: "officeooo",
}

// qname returns the key of a name: its conventional prefix and local name,
// "fo:margin-left", or the namespace URI and the local name for a
// namespace without one.
func qname(n xml.Name) string {
	if p, ok := prefixes[n.Space]; ok {
		return p + ":" + n.Local
	}
	if n.Space == "" {
		return n.Local
	}
	return n.Space + ":" + n.Local
}

// The caps of one parsed XML document.
const (
	// MaxElements bounds the elements and text nodes of one XML document:
	// 2,000,000. Anti-DoS guard, not spec-mandated: a 500-page report
	// holds some 200,000 elements, and each costs about a hundred bytes.
	MaxElements = 2_000_000
	// MaxDepth bounds the element nesting of one XML document: 256.
	// Anti-DoS guard, not spec-mandated: nested lists, tables and frames
	// of real documents stay under 40 levels, and the converters walk the
	// tree recursively.
	MaxDepth = 256
)

// Element is one element of an XML document, or one of its text nodes:
// a text node has an empty Name and its characters in Text. Children are
// in document order. An Element is read-only once parsed and safe for
// concurrent reads.
type Element struct {
	Name     xml.Name
	Attr     []xml.Attr
	Children []*Element
	Text     string
}

// IsText reports a text node.
func (e *Element) IsText() bool { return e != nil && e.Name.Local == "" }

// Is reports an element of namespace space and local name local.
func (e *Element) Is(space, local string) bool {
	return e != nil && e.Name.Local == local && e.Name.Space == space
}

// A returns the value of the attribute space:local, "" when absent.
func (e *Element) A(space, local string) string {
	v, _ := e.Lookup(space, local)
	return v
}

// Lookup returns the value of the attribute space:local and whether it is
// present.
func (e *Element) Lookup(space, local string) (string, bool) {
	if e == nil {
		return "", false
	}
	for _, a := range e.Attr {
		if a.Name.Local == local && a.Name.Space == space {
			return a.Value, true
		}
	}
	return "", false
}

// Child returns the first child element space:local, nil when none.
func (e *Element) Child(space, local string) *Element {
	if e == nil {
		return nil
	}
	for _, c := range e.Children {
		if c.Is(space, local) {
			return c
		}
	}
	return nil
}

// All returns the child elements space:local in document order.
func (e *Element) All(space, local string) []*Element {
	if e == nil {
		return nil
	}
	var out []*Element
	for _, c := range e.Children {
		if c.Is(space, local) {
			out = append(out, c)
		}
	}
	return out
}

// TextContent returns the characters of e's text descendants in document
// order.
func (e *Element) TextContent() string {
	if e == nil {
		return ""
	}
	if e.IsText() {
		return e.Text
	}
	var b strings.Builder
	stack := []*Element{e}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if cur.IsText() {
			b.WriteString(cur.Text)
			continue
		}
		for i := len(cur.Children) - 1; i >= 0; i-- {
			stack = append(stack, cur.Children[i])
		}
	}
	return b.String()
}

// errTooDeep reports a document nested past [MaxDepth].
var errTooDeep = errors.New("odf: XML nested too deep")

// ParseXML parses the XML document data into its root element. A DOCTYPE
// is skipped, never processed, so no entity but the five predefined ones
// expands; a document that is not well-formed fails with
// office.ErrMalformed, one past [MaxElements] or [MaxDepth] with
// office.ErrInputTooLarge. It returns ctx's error when ctx ends first.
func ParseXML(ctx context.Context, data []byte) (*Element, error) {
	d := xml.NewDecoder(bytes.NewReader(data))
	d.Strict = true
	var (
		stack []*Element
		root  *Element
		count int
	)
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: parsing XML: %w", office.ErrMalformed, err)
		}
		count++
		if count > MaxElements {
			return nil, fmt.Errorf("%w: more than %d XML nodes", office.ErrInputTooLarge, MaxElements)
		}
		if count%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if len(stack) >= MaxDepth {
				return nil, fmt.Errorf("%w: %w past %d levels", office.ErrInputTooLarge, errTooDeep, MaxDepth)
			}
			el := &Element{Name: t.Name, Attr: t.Attr}
			if len(stack) == 0 {
				if root != nil {
					return nil, fmt.Errorf("%w: parsing XML: a second root element", office.ErrMalformed)
				}
				root = el
			} else {
				p := stack[len(stack)-1]
				p.Children = append(p.Children, el)
			}
			stack = append(stack, el)
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			if len(stack) == 0 {
				continue
			}
			p := stack[len(stack)-1]
			if n := len(p.Children); n > 0 && p.Children[n-1].IsText() {
				p.Children[n-1].Text += string(t)
				continue
			}
			p.Children = append(p.Children, &Element{Text: string(t)})
		}
	}
	if root == nil {
		return nil, fmt.Errorf("%w: parsing XML: no root element", office.ErrMalformed)
	}
	return root, nil
}
