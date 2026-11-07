package ooxml

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

// MaxElements caps the XML elements parsed from one package, all parts
// together. Anti-DoS guard, not spec-mandated: a five-hundred-page report
// holds about half a million elements, and the tree costs a few hundred
// bytes an element.
const MaxElements = 2_000_000

// MaxDepth caps the nesting of XML elements. Anti-DoS guard, not
// spec-mandated: tables nested in tables, content controls and drawings
// rarely pass forty levels, and the converters walk the tree recursively.
const MaxDepth = 256

// The namespaces the converters read, in their Transitional form (ECMA-376
// Part 1, Annex A; the Microsoft extensions of MS-ODRAWXML and MS-DOCX).
const (
	NSW    = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
	NSR    = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
	NSA    = "http://schemas.openxmlformats.org/drawingml/2006/main"
	NSWP   = "http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing"
	NSPic  = "http://schemas.openxmlformats.org/drawingml/2006/picture"
	NSM    = "http://schemas.openxmlformats.org/officeDocument/2006/math"
	NSMC   = "http://schemas.openxmlformats.org/markup-compatibility/2006"
	NSWPS  = "http://schemas.microsoft.com/office/word/2010/wordprocessingShape"
	NSWPG  = "http://schemas.microsoft.com/office/word/2010/wordprocessingGroup"
	NSWP14 = "http://schemas.microsoft.com/office/word/2010/wordprocessingDrawing"
	NSW14  = "http://schemas.microsoft.com/office/word/2010/wordml"
	NSV    = "urn:schemas-microsoft-com:vml"
	NSO    = "urn:schemas-microsoft-com:office:office"
	NSW10  = "urn:schemas-microsoft-com:office:word"
	NSASVG = "http://schemas.microsoft.com/office/drawing/2016/SVG/main"
	NSCP   = "http://schemas.openxmlformats.org/package/2006/metadata/core-properties"
	NSDC   = "http://purl.org/dc/elements/1.1/"
)

// strictNSPrefix starts every ISO/IEC 29500 Strict namespace; the
// Transitional one inserts the year (ISO/IEC 29500-1, Annex A).
const strictNSPrefix = "http://purl.oclc.org/ooxml/"

// Element is one XML element of a part: its namespace URI and local name,
// its attributes but the namespace declarations, its child elements, and
// its character data. Character data that is only whitespace around child
// elements is dropped; an element without children keeps its text as
// written.
type Element struct {
	Space, Local string
	Attrs        []xml.Attr
	Children     []*Element
	Text         string
}

// Is reports whether e is the element space:local.
func (e *Element) Is(space, local string) bool {
	return e != nil && e.Local == local && e.Space == space
}

// Attr returns the value of the attribute local in no namespace or in
// e's own namespace, as WordprocessingML qualifies its attributes, and
// whether it is present.
func (e *Element) Attr(local string) (string, bool) {
	if e == nil {
		return "", false
	}
	for _, a := range e.Attrs {
		if a.Name.Local == local && (a.Name.Space == "" || a.Name.Space == e.Space) {
			return a.Value, true
		}
	}
	return "", false
}

// AttrNS returns the value of the attribute space:local and whether it is
// present.
func (e *Element) AttrNS(space, local string) (string, bool) {
	if e == nil {
		return "", false
	}
	for _, a := range e.Attrs {
		if a.Name.Local == local && a.Name.Space == space {
			return a.Value, true
		}
	}
	return "", false
}

// Kids returns e's children, nil for a nil e.
func (e *Element) Kids() []*Element {
	if e == nil {
		return nil
	}
	return e.Children
}

// Child returns the first child space:local, nil for none.
func (e *Element) Child(space, local string) *Element {
	if e == nil {
		return nil
	}
	for _, c := range e.Children {
		if c.Local == local && c.Space == space {
			return c
		}
	}
	return nil
}

// Find returns the first descendant space:local in document order, nil
// for none.
func (e *Element) Find(space, local string) *Element {
	if e == nil {
		return nil
	}
	stack := []*Element{e}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if cur != e && cur.Is(space, local) {
			return cur
		}
		for i := len(cur.Children) - 1; i >= 0; i-- {
			stack = append(stack, cur.Children[i])
		}
	}
	return nil
}

// attrOr returns e's attribute local, "" when it is absent.
func attrOr(e *Element, local string) string {
	v, _ := e.Attr(local)
	return v
}

// ParseXML parses the part name into an element tree, its markup
// compatibility resolved: an mc:AlternateContent is replaced by the
// content of its first mc:Choice whose Requires names only namespaces of
// understood, else by its mc:Fallback (ECMA-376 Part 3, 10.2). It fails
// with [ErrNoPart] for a missing part, office.ErrMalformed for XML that
// does not parse or declares a document type, and office.ErrInputTooLarge
// past [MaxElements] for the package or [MaxDepth].
func (p *Package) ParseXML(ctx context.Context, name string, understood []string) (*Element, error) {
	data, err := p.Read(name)
	if err != nil {
		return nil, err
	}
	root, err := parse(ctx, data, understood, &p.elements)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", name, err)
	}
	return root, nil
}

// Parse parses data as [Package.ParseXML] does a part, against a budget of
// [MaxElements] of its own.
func Parse(ctx context.Context, data []byte, understood []string) (*Element, error) {
	n := 0
	return parse(ctx, data, understood, &n)
}

// errDocType reports a document type declaration, which no OOXML part
// carries and which could name external entities.
var errDocType = errors.New("a document type declaration")

// parse is [Parse] with the package's element count.
func parse(ctx context.Context, data []byte, understood []string, count *int) (*Element, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = true
	type frame struct {
		el  *Element
		ns  map[string]string
		raw xml.Name
	}
	doc := &Element{}
	stack := []frame{{el: doc, ns: map[string]string{"xml": "http://www.w3.org/XML/1998/namespace"}}}
	for {
		tok, err := dec.RawToken()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: %w", office.ErrMalformed, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			*count++
			if *count > MaxElements {
				return nil, fmt.Errorf("%w: more than %d XML elements", office.ErrInputTooLarge, MaxElements)
			}
			if *count%4096 == 0 {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
			}
			if len(stack) > MaxDepth {
				return nil, fmt.Errorf("%w: XML nested deeper than %d", office.ErrInputTooLarge, MaxDepth)
			}
			parent := stack[len(stack)-1]
			ns := parent.ns
			copied := false
			for _, a := range t.Attr {
				if a.Name.Space == "xmlns" || a.Name.Space == "" && a.Name.Local == "xmlns" {
					if !copied {
						copied = true
						ns = make(map[string]string, len(parent.ns)+4)
						for k, v := range parent.ns {
							ns[k] = v
						}
					}
					prefix := a.Name.Local
					if a.Name.Space == "" {
						prefix = ""
					}
					ns[prefix] = normalizeNS(a.Value)
				}
			}
			el := &Element{Space: resolvePrefix(ns, t.Name.Space, true), Local: t.Name.Local}
			for _, a := range t.Attr {
				if a.Name.Space == "xmlns" || a.Name.Space == "" && a.Name.Local == "xmlns" {
					continue
				}
				a.Name.Space = resolvePrefix(ns, a.Name.Space, false)
				el.Attrs = append(el.Attrs, a)
			}
			if el.Is(NSMC, "Choice") {
				// Requires lists prefixes in the scope of the Choice.
				var uris []string
				for _, prefix := range strings.Fields(attrOr(el, "Requires")) {
					uris = append(uris, resolvePrefix(ns, prefix, false))
				}
				el.Attrs = append(el.Attrs, xml.Attr{Name: xml.Name{Local: requiresURIs}, Value: strings.Join(uris, " ")})
			}
			if len(parent.el.Children) == 0 && strings.TrimSpace(parent.el.Text) == "" {
				parent.el.Text = ""
			}
			parent.el.Children = append(parent.el.Children, el)
			stack = append(stack, frame{el: el, ns: ns, raw: t.Name})
		case xml.EndElement:
			if len(stack) <= 1 {
				return nil, fmt.Errorf("%w: unbalanced end element", office.ErrMalformed)
			}
			top := stack[len(stack)-1]
			if top.raw != t.Name {
				return nil, fmt.Errorf("%w: end element %s:%s does not close %s:%s", office.ErrMalformed, t.Name.Space, t.Name.Local, top.raw.Space, top.raw.Local)
			}
			el := top.el
			stack = stack[:len(stack)-1]
			if el.Is(NSMC, "AlternateContent") {
				parent := stack[len(stack)-1].el
				parent.Children = parent.Children[:len(parent.Children)-1]
				parent.Children = append(parent.Children, chooseAlternate(el, understood)...)
			}
		case xml.CharData:
			cur := stack[len(stack)-1].el
			if len(cur.Children) > 0 && len(bytes.TrimSpace(t)) == 0 {
				continue
			}
			cur.Text += string(t)
		case xml.Directive:
			if bytes.HasPrefix(bytes.TrimSpace(t), []byte("DOCTYPE")) {
				return nil, fmt.Errorf("%w: %w", office.ErrMalformed, errDocType)
			}
		}
	}
	if len(stack) != 1 {
		return nil, fmt.Errorf("%w: unclosed element", office.ErrMalformed)
	}
	if len(doc.Children) > 0 {
		return doc.Children[0], nil
	}
	return nil, fmt.Errorf("%w: no root element", office.ErrMalformed)
}

// requiresURIs is the attribute the parser adds to an mc:Choice: the
// namespace URIs its Requires prefixes stand for.
const requiresURIs = "\x00requires"

// chooseAlternate returns what an mc:AlternateContent stands for: the
// children of its first Choice whose required namespaces are all
// understood, else of its Fallback, else nothing (ECMA-376 Part 3,
// 10.2.1).
func chooseAlternate(ac *Element, understood []string) []*Element {
	for _, c := range ac.Children {
		if !c.Is(NSMC, "Choice") {
			continue
		}
		uris, _ := c.Attr(requiresURIs)
		ok := uris != ""
		for _, u := range strings.Fields(uris) {
			if !contains(understood, u) {
				ok = false
				break
			}
		}
		if ok {
			return c.Children
		}
	}
	if fb := ac.Child(NSMC, "Fallback"); fb != nil {
		return fb.Children
	}
	return nil
}

// contains reports whether list holds s.
func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// resolvePrefix returns the namespace URI prefix stands for in scope ns:
// the default namespace for an element without prefix, no namespace for
// an attribute without one, the prefix itself when it is undeclared.
func resolvePrefix(ns map[string]string, prefix string, element bool) string {
	if prefix == "" && !element {
		return ""
	}
	if uri, ok := ns[prefix]; ok {
		return uri
	}
	return prefix
}

// normalizeNS maps an ISO/IEC 29500 Strict namespace to its Transitional
// form: http://purl.oclc.org/ooxml/<area>/<name> is
// http://schemas.openxmlformats.org/<area>/2006/<name>.
func normalizeNS(uri string) string {
	rest, ok := strings.CutPrefix(uri, strictNSPrefix)
	if !ok {
		return uri
	}
	area, name, ok := strings.Cut(rest, "/")
	if !ok {
		return uri
	}
	return "http://schemas.openxmlformats.org/" + area + "/2006/" + name
}
