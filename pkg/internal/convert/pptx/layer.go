package pptx

import (
	"fmt"
	"strconv"

	"github.com/gotenberg/litho/pkg/internal/ooxml"
)

// layer is one sheet of the placeholder hierarchy (ECMA-376 Part 1,
// 19.3.1.36 ph and 19.2.1 the slide masters, layouts and slides): a slide,
// its layout, its master, or a notes slide and its notes master. A layout
// or master is read once and shared by the slides that chain to it.
type layer struct {
	part   string
	root   *ooxml.Element
	tree   *ooxml.Element
	parent *layer
	sc     scope
	// phs are the placeholder shapes of the layer's shape tree.
	phs []*ooxml.Element
	// title, body and other are a master's text styles (19.3.1.51
	// txStyles: titleStyle, bodyStyle, otherStyle); a notes master's
	// notesStyle is its body and other style.
	title, body, other *ooxml.Element
	// showMasterSp is false when the layer hides its master's shapes
	// (19.3.1.38 sld and 19.3.1.39 sldLayout showMasterSp).
	showMasterSp bool
}

// newLayer reads the shape tree, the placeholders and the flags of root.
func newLayer(part string, root *ooxml.Element, parent *layer) *layer {
	l := &layer{part: part, root: root, parent: parent, showMasterSp: boolAttr(root, "showMasterSp", true)}
	l.tree = root.Child(nsP, "cSld").Child(nsP, "spTree")
	for _, k := range l.tree.Kids() {
		if _, ok := phOf(k); ok {
			l.phs = append(l.phs, k)
		}
	}
	return l
}

// master returns the slide master part, read once.
func (r *reader) master(part string) (*layer, error) {
	if l, ok := r.layers[part]; ok {
		return l, nil
	}
	root, err := r.pkg.ParseXML(r.ctx, part, understood)
	if err != nil {
		return nil, err
	}
	if !root.Is(nsP, "sldMaster") && !root.Is(nsP, "notesMaster") {
		return nil, fmt.Errorf("%w: %s is not a master", errNotPresentationPart, part)
	}
	l := newLayer(part, root, nil)
	l.sc = scope{theme: r.themeOf(part), cmap: readColorMap(root.Child(nsP, "clrMap"), defaultColorMap)}
	if ts := root.Child(nsP, "txStyles"); ts != nil {
		l.title, l.body, l.other = ts.Child(nsP, "titleStyle"), ts.Child(nsP, "bodyStyle"), ts.Child(nsP, "otherStyle")
	}
	if ns := root.Child(nsP, "notesStyle"); ns != nil {
		l.body, l.other = ns, ns
	}
	r.layers[part] = l
	return l, nil
}

// layout returns the slide layout part with its master, read once.
func (r *reader) layout(part string) (*layer, error) {
	if l, ok := r.layers[part]; ok {
		return l, nil
	}
	root, err := r.pkg.ParseXML(r.ctx, part, understood)
	if err != nil {
		return nil, err
	}
	if !root.Is(nsP, "sldLayout") {
		return nil, fmt.Errorf("%w: %s is not a slide layout", errNotPresentationPart, part)
	}
	mpart := r.related(part, relSlideMaster)
	if mpart == "" {
		return nil, fmt.Errorf("%w: layout %s relates no master", errNotPresentationPart, part)
	}
	m, err := r.master(mpart)
	if err != nil {
		return nil, err
	}
	l := newLayer(part, root, m)
	l.sc = scope{theme: m.sc.theme, cmap: overrideColorMap(root, m.sc.cmap)}
	r.layers[part] = l
	return l, nil
}

// overrideColorMap returns the colour mapping of a slide, layout or notes
// slide: its p:clrMapOvr's a:overrideClrMapping over base, base for
// a:masterClrMapping or none (19.3.1.7 clrMapOvr).
func overrideColorMap(root *ooxml.Element, base colorMap) colorMap {
	return readColorMap(root.Child(nsP, "clrMapOvr").Child(ooxml.NSA, "overrideClrMapping"), base)
}

// masterOf returns the master at the top of l's chain.
func (l *layer) masterOf() *layer {
	for l.parent != nil {
		l = l.parent
	}
	return l
}

// placeholder is a shape's placeholder reference (ECMA-376 Part 1,
// 19.3.1.36 ph): its type, "obj" when the file names none, and its index.
type placeholder struct {
	typ    string
	idx    int
	hasIdx bool
}

// phOf returns the placeholder reference of a shape element, false for a
// shape that is no placeholder.
func phOf(el *ooxml.Element) (placeholder, bool) {
	for _, nv := range el.Kids() {
		if len(nv.Local) < 4 || nv.Local[:2] != "nv" || nv.Local[len(nv.Local)-2:] != "Pr" {
			continue
		}
		ph := nv.Child(nsP, "nvPr").Child(nsP, "ph")
		if ph == nil {
			return placeholder{}, false
		}
		p := placeholder{typ: attr(ph, "type")}
		if p.typ == "" {
			p.typ = "obj"
		}
		if v, ok := ph.Attr("idx"); ok {
			if n, err := strconv.ParseUint(v, 10, 32); err == nil {
				p.idx, p.hasIdx = int(n), true // fits int: at most 2^32-1, parsed as 32 bits
			}
		}
		return p, true
	}
	return placeholder{}, false
}

// class groups placeholder types that stand in for one another when a
// layout or master lacks the exact type, as PowerPoint and LibreOffice
// match them: the titles, the bodies and content placeholders, and each
// of the others alone.
func (p placeholder) class() string {
	switch p.typ {
	case "title", "ctrTitle":
		return "title"
	case "body", "subTitle", "obj", "chart", "tbl", "clipArt", "dgm", "media", "pic":
		return "body"
	}
	return p.typ
}

// find returns the placeholder shape of l that p inherits from (ECMA-376
// Part 1, 19.3.1.36: a slide's placeholder from its layout's of the same
// idx, else of the same type; a layout's from its master's of the same
// type), nil for none. byIdx enables the index match, which holds between
// a slide and its layout only.
func (l *layer) find(p placeholder, byIdx bool) *ooxml.Element {
	if l == nil {
		return nil
	}
	if byIdx && p.hasIdx {
		for _, k := range l.phs {
			if q, _ := phOf(k); q.hasIdx && q.idx == p.idx {
				return k
			}
		}
	}
	for _, k := range l.phs {
		if q, _ := phOf(k); q.typ == p.typ {
			return k
		}
	}
	for _, k := range l.phs {
		if q, _ := phOf(k); q.class() == p.class() {
			return k
		}
	}
	return nil
}

// inherited returns the chain of shapes el inherits from in layer l: el,
// then its layout's placeholder, then that one's master placeholder, as
// far as each exists.
func inherited(l *layer, el *ooxml.Element) []*ooxml.Element {
	chain := []*ooxml.Element{el}
	p, ok := phOf(el)
	if !ok {
		return chain
	}
	byIdx := true
	for up := l.parent; up != nil; up = up.parent {
		k := up.find(p, byIdx)
		if k == nil {
			continue
		}
		chain = append(chain, k)
		if q, ok := phOf(k); ok {
			p = q
		}
		byIdx = false
	}
	return chain
}

// styleFor returns the master text style a text body of placeholder p
// takes (19.3.1.51 txStyles): the title style for the titles, the body
// style for the bodies and content placeholders, the other style for the
// rest and for shapes that are no placeholder.
func (l *layer) styleFor(p placeholder, isPh bool) *ooxml.Element {
	m := l.masterOf()
	switch {
	case !isPh:
		return m.other
	case p.class() == "title":
		return m.title
	case p.class() == "body":
		return m.body
	}
	return m.other
}
