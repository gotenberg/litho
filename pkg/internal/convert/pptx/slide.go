package pptx

import (
	"fmt"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/convert/slides"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
)

// slide reads the slide part, the n-th of the show, with its layout and
// master (ECMA-376 Part 1, 19.3.1.38 sld).
func (r *reader) slide(n int, part string) (*slides.Slide, error) {
	root, err := r.pkg.ParseXML(r.ctx, part, understood)
	if err != nil {
		return nil, err
	}
	if !root.Is(nsP, "sld") {
		return nil, fmt.Errorf("%w: %s is not a slide", errNotPresentationPart, part)
	}
	lpart := r.related(part, relSlideLayout)
	if lpart == "" {
		return nil, fmt.Errorf("%w: slide %s relates no layout", errNotPresentationPart, part)
	}
	lay, err := r.layout(lpart)
	if err != nil {
		return nil, err
	}
	sl := newLayer(part, root, lay)
	// A slide with masterClrMapping takes its layout's mapping, the
	// layout's override when it has one, as Impress applies it.
	sl.sc = scope{theme: lay.sc.theme, cmap: overrideColorMap(root, lay.sc.cmap)}
	s := &slides.Slide{Hidden: !boolAttr(root, "show", true)}
	s.Background = r.background(sl)
	m := lay.masterOf()
	if sl.showMasterSp {
		if lay.showMasterSp {
			if err := r.shapeList(&s.Shapes, &shapeCtx{layer: m, sc: sl.sc, ns: nsP, part: m.part}, m.tree, 0); err != nil {
				return nil, err
			}
		}
		if err := r.shapeList(&s.Shapes, &shapeCtx{layer: lay, sc: sl.sc, ns: nsP, part: lay.part}, lay.tree, 0); err != nil {
			return nil, err
		}
	}
	sc := &shapeCtx{layer: sl, sc: sl.sc, ns: nsP, part: part, slide: true}
	if err := r.shapeList(&s.Shapes, sc, sl.tree, 0); err != nil {
		return nil, err
	}
	s.Title = sc.title
	s.Name = s.Title
	if s.Name == "" {
		s.Name = fmt.Sprintf("Slide %d", n)
	}
	r.slideWarnings(part, root)
	notes, err := r.notes(part)
	if err != nil {
		return nil, err
	}
	s.Notes = notes
	return s, nil
}

// slideWarnings reports what the slide holds that a printout leaves out:
// its transition, its animations and its comments.
func (r *reader) slideWarnings(part string, root *ooxml.Element) {
	if root.Child(nsP, "transition") != nil {
		r.warn("unsupported-content", "slide transitions do not print")
	}
	if root.Child(nsP, "timing") != nil {
		r.warn("unsupported-content", "slide animations do not print: shapes show in their final place")
	}
	rels, _ := r.pkg.Relationships(part) //nolint:errcheck // a relationships part that does not parse relates nothing
	for _, rel := range rels {
		if rel.Type == relComments || strings.HasSuffix(rel.Type, "/relationships/comments") {
			r.warn("unsupported-content", "slide comments do not print")
		}
	}
}

// background returns the background of a slide: the first of the slide's,
// its layout's and its master's p:bg (19.3.1.1 bg: a bgPr fill, or a
// bgRef into the theme's background fill styles, 20.1.4.1.7
// bgFillStyleLst, for idx 1001 and up).
func (r *reader) background(sl *layer) slides.Fill {
	for l := sl; l != nil; l = l.parent {
		bg := l.root.Child(nsP, "cSld").Child(nsP, "bg")
		if bg == nil {
			continue
		}
		fc := &fillCtx{sc: sl.sc, part: l.part}
		if pr := bg.Child(nsP, "bgPr"); pr != nil {
			if f, ok := r.fillOf(fc, pr); ok {
				return f
			}
			return slides.Fill{}
		}
		if ref := bg.Child(nsP, "bgRef"); ref != nil {
			return r.styleFill(fc, ref)
		}
	}
	return slides.Fill{}
}

// notes reads the notes of the slide part: the text of its notes slide's
// body placeholder (19.3.1.26 notes), nil for none.
func (r *reader) notes(slidePart string) ([]slides.Paragraph, error) {
	part := r.related(slidePart, relNotesSlide)
	if part == "" {
		return nil, nil
	}
	root, err := r.pkg.ParseXML(r.ctx, part, understood)
	if err != nil {
		if isMalformed(err) {
			r.warn("malformed-part", fmt.Sprintf("the notes %s do not parse and are left out: %v", part, err))
			return nil, nil
		}
		return nil, err
	}
	if !root.Is(nsP, "notes") {
		return nil, nil
	}
	var nm *layer
	if mpart := r.related(part, relNotesMaster); mpart != "" {
		if nm, err = r.master(mpart); err != nil {
			if !isMalformed(err) {
				return nil, err
			}
			nm = nil
		}
	}
	nl := newLayer(part, root, nm)
	base := defaultColorMap
	sc := scope{cmap: base}
	if nm != nil {
		sc = scope{theme: nm.sc.theme, cmap: overrideColorMap(root, nm.sc.cmap)}
	} else {
		sc.theme = r.themeOf("")
	}
	nl.sc = sc
	for _, k := range nl.phs {
		if p, _ := phOf(k); p.typ != "body" {
			continue
		}
		c := &shapeCtx{layer: nl, sc: sc, ns: nsP, part: part}
		body := r.textBody(c, inherited(nl, k), p0(k), nil)
		if body == nil {
			return nil, nil
		}
		return body.Paragraphs, nil
	}
	return nil, nil
}

// p0 returns the placeholder of el, the zero placeholder for none.
func p0(el *ooxml.Element) placeholder {
	p, _ := phOf(el)
	return p
}
