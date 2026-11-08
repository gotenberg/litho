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
	r.own(l)
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
	r.own(l)
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
	// typed reports a type the file names; skip an index of 4294967295,
	// which Impress looks up nowhere.
	typed, skip bool
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
		p.typed = p.typ != ""
		if !p.typed {
			p.typ = "obj"
		}
		if v, ok := ph.Attr("idx"); ok {
			if n, err := strconv.ParseUint(v, 10, 32); err == nil {
				p.idx, p.hasIdx = int(n), true // fits int: at most 2^32-1, parsed as 32 bits
				p.skip = n == 1<<32-1
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
// Part 1, 19.3.1.36), nil for none, by Impress's priorities: the same type
// and index, the same type, then for a centred title the title, for a
// subtitle or an object a body, with the same index and without, then
// the same index alone. A placeholder of no type that the slide matches
// by index takes the layout placeholder's type first (inherited).
//
// # Corpus evidence
//
//   - libreoffice/pptx: sd/tdf120028's body placeholder of an index its layout lacks,
//     on a layout of a title and a subtitle, inherits nothing: its text
//     prints left-aligned and black, not as the subtitle's centred grey.
func (l *layer) find(p placeholder) *ooxml.Element {
	if l == nil {
		return nil
	}
	second := ""
	switch p.typ {
	case "ctrTitle":
		second = "title"
	case "subTitle", "obj":
		second = "body"
	}
	var choice [5]*ooxml.Element
	take := func(slot **ooxml.Element, k *ooxml.Element) {
		if *slot == nil {
			*slot = k
		}
	}
	for _, k := range l.phs {
		q, _ := phOf(k)
		sameIdx := p.hasIdx == q.hasIdx && (!p.hasIdx || p.idx == q.idx)
		switch {
		case q.typ == p.typ && sameIdx:
			take(&choice[0], k)
		case q.typ == p.typ:
			take(&choice[1], k)
		case second != "" && q.typ == second && sameIdx:
			take(&choice[2], k)
		case second != "" && q.typ == second:
			take(&choice[3], k)
		case sameIdx:
			take(&choice[4], k)
		}
	}
	for _, k := range choice {
		if k != nil {
			return k
		}
	}
	return nil
}

// inherited returns the chain of shapes el inherits from in layer l: el,
// then its layout's placeholder, then that one's master placeholder. A
// placeholder its layout has no placeholder for inherits nothing, not even
// from the master, and its text takes the master's other style, as Impress
// imports it; so does a placeholder of the index 4294967295, which Impress
// looks up nowhere. A placeholder without a type that its layout has one
// of the same index for takes that one's type.
//
// # Corpus evidence
//
//   - libreoffice/pptx: sd-data/n759180's body placeholder of idx 4294967295, on a
//     layout of a title, a footer and a slide number, prints at its box's
//     inset without the master body style's bullet and indent.
func inherited(l *layer, el *ooxml.Element) []*ooxml.Element {
	chain := []*ooxml.Element{el}
	p, ok := phOf(el)
	if !ok || p.skip {
		return chain
	}
	if !p.typed && p.hasIdx && l.parent != nil {
		for _, k := range l.parent.phs {
			if q, _ := phOf(k); q.hasIdx && q.idx == p.idx {
				p.typ = q.typ
				break
			}
		}
	}
	for up := l.parent; up != nil; up = up.parent {
		k := up.find(p)
		if k == nil {
			break
		}
		chain = append(chain, k)
		if q, ok := phOf(k); ok {
			p = q
		}
	}
	return chain
}

// styleFor returns the master text style a text body of placeholder p
// takes (19.3.1.51 txStyles): the title style for the titles, the body
// style for the bodies and content placeholders, the other style for the
// other placeholders; none for a shape that is no placeholder, whose text
// takes the presentation's default text style alone, as Impress prints
// it.
//
// # Corpus evidence
//
//   - libreoffice/pptx: oox/tdf151008_eaVertAnchor's text boxes, of no size of their
//     own on a master whose other style sets 15.12 points, print at the
//     default text style's 18 points, upright or vertical.
func (l *layer) styleFor(p placeholder, isPh bool) *ooxml.Element {
	m := l.masterOf()
	switch {
	case !isPh:
		return nil
	case p.class() == "title":
		return m.title
	case p.class() == "body":
		return m.body
	}
	return m.other
}
