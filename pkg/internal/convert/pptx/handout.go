package pptx

import (
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/convert/slides"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
)

// handoutMaster reads the presentation's handout master into doc
// (ECMA-376 Part 1, 19.3.1.24 handoutMaster, named by 19.2.1.13
// handoutMasterIdLst): its background, its shapes that are no
// placeholder, and its header, date, footer and slide number placeholders
// its header and footer flags show (19.3.1.25 hf, each true by default),
// with the text the master gives them. A handout master names no page
// size and places no slide: its placeholders span the notes size, which
// is the handout page's, and the slides take the builder's layout. A
// presentation without a handout master, or with one that does not
// parse, prints its handout pages without one.
//
// # Corpus evidence
//
//   - litho/pptx: handout-master's placeholders span 0 to 6,858,000 by 0
//     to 9,144,000 EMU, its notesSz; no part of the fixtures places the
//     slides of a handout page. Where PowerPoint puts them is not
//     measured: a Microsoft reference needs the lead's sign-in, and Office
//     for the web prints no handout.
func (r *reader) handoutMaster(root *ooxml.Element, doc *slides.Document) error {
	id := root.Child(nsP, "handoutMasterIdLst").Child(nsP, "handoutMasterId")
	if id == nil {
		return nil
	}
	rid, _ := id.AttrNS(ooxml.NSR, "id")
	rel, ok := r.pkg.RelationshipByID(r.main, rid)
	if !ok || rel.External || rel.Type != relHandoutMaster || !r.pkg.Has(rel.Target) {
		r.warn("malformed-part", fmt.Sprintf("handout master %s is missing and left out", rid))
		return nil
	}
	hm, err := r.master(rel.Target)
	if err != nil {
		if isMalformed(err) {
			r.warn("malformed-part", fmt.Sprintf("the handout master %s does not parse and is left out: %v", rel.Target, err))
			return nil
		}
		return err
	}
	doc.HandoutBackground = r.background(hm)
	c := &shapeCtx{layer: hm, sc: hm.sc, ns: nsP, part: hm.part, master: true}
	if err := r.shapeList(&doc.HandoutShapes, c, hm.tree, 0); err != nil {
		return err
	}
	// The master's own placeholders are what a handout page prints: they
	// resolve as a page's placeholders resolve against their master.
	page := &layer{part: hm.part, root: hm.root, tree: hm.tree, parent: hm, sc: hm.sc, phs: hm.phs, showMasterSp: true}
	pc := &shapeCtx{layer: page, sc: hm.sc, ns: nsP, part: hm.part, slide: true, master: true}
	hf := hm.root.Child(nsP, "hf")
	for _, k := range hm.phs {
		p, _ := phOf(k)
		if !footerKind(p) || hidden(k) || !handoutShows(hf, p) {
			continue
		}
		if s := r.autoShape(pc, k); s != nil {
			s.Decorative = true
			doc.HandoutShapes = append(doc.HandoutShapes, s)
		}
	}
	return nil
}

// handoutShows reports whether the handout master's header and footer
// flags hf show the placeholder p (19.3.1.25 hf: hdr, dt, ftr and sldNum,
// each true when absent).
func handoutShows(hf *ooxml.Element, p placeholder) bool {
	switch p.typ {
	case "hdr":
		return boolAttr(hf, "hdr", true)
	case "dt":
		return boolAttr(hf, "dt", true)
	case "ftr":
		return boolAttr(hf, "ftr", true)
	case "sldNum":
		return boolAttr(hf, "sldNum", true)
	}
	return false
}
