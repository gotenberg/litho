package layout

import (
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// tagEvent is one thing a structure element holds, in document order: a
// child element, met at its first box, or a piece of the element's own
// content, a box that paints content or an outside list marker.
type tagEvent struct {
	kid  *typeset.StructElem
	leaf *box
	mark *markerBox
	// run numbers the inline formatting context (CSS 2.1, 9.4.2) the event
	// sits in, 0 for none or for a kid that is not inline-level, which
	// breaks the run.
	run int
}

// groupingRoles are the grouping types of ISO 32000-1, 14.8.4.2, Table 334
// and ISO 32000-2, 14.8.4.4, Table 364: they group elements and hold no
// content items of their own (ISO 32005, Table 5), nor does a TOCI, whose
// text a P carries. Immutable lookup table.
var groupingRoles = map[string]bool{
	"Document": true, "DocumentFragment": true, "Part": true, "Art": true, "Sect": true,
	"Div": true, "BlockQuote": true, "Aside": true, "NonStruct": true, "Private": true,
	"Index": true, "TOCI": true,
}

// containerRoles hold either content or blocks (ISO 32000-1, 14.8.4.3.3 and
// 14.8.4.3.4: a list body, a table cell, a caption, a note): their own
// content becomes a paragraph only when block-level elements stand beside
// it. Immutable lookup table.
var containerRoles = map[string]bool{
	"LBody": true, "TD": true, "TH": true, "Caption": true, "Note": true, "FENote": true,
}

// opaqueRoles are left as they are: an illustration owns its content as a
// whole (ISO 32000-1, 14.8.4.5), and a list, a table, its row groups and
// rows and a table of contents hold only their own kinds of elements,
// which an anonymous paragraph would not be. Immutable lookup table.
var opaqueRoles = map[string]bool{
	"Figure": true, "Formula": true, "Form": true,
	"L": true, "Table": true, "THead": true, "TBody": true, "TFoot": true, "TR": true,
	"TOC": true,
}

// anonymize gives the own content of a structure element an element of its
// own wherever that content sits beside child elements, or under a grouping
// element, so that no element holds content items and elements at once and
// the order of /K is the document order (ISO 32000-1, 14.7.2: an element's
// kids are in logical order; the backend writes an element's content items
// before its kids). It runs on the box tree rb, after generation and before
// layout copies any box.
//
// Under a grouping element, each run of an inline formatting context, its
// content and the inline-level elements in it, becomes an anonymous P, the
// block element that holds inline content (14.8.4.3.1); so does such a run
// beside block-level kids of a list body, a table cell, a caption or a
// note. Elsewhere, each run of own content between child elements becomes
// an anonymous Span (14.8.4.4), the inline element that adds no meaning.
// An illustration, a list, a table and its parts are left alone.
//
// It also gives each header cell whose node says nothing a Scope
// ([headerScopes]).
//
// A block-level element that an inline-level box starts (an inline-block,
// a frame) in a paragraph, a heading or an inline element, which none of
// them may hold, splits that element around it: the parts keep their role
// and the block stands between them ([hoistBlocks]). A DocumentFragment
// whose holder may not hold one gets a Sect of its own ([wrapFragments]).
//
// Anonymous elements add two levels at most below [maxStructDepth], and
// stop once the tree holds [typeset.MaxStructElems] elements: the content
// past that stays with its element.
func (e *engine) anonymize(rb *box) {
	parent := map[*typeset.StructElem]*typeset.StructElem{}
	count := 0
	stack := []*typeset.StructElem{e.root}
	for len(stack) > 0 {
		el := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		count++
		for _, k := range el.Kids {
			parent[k] = el
			stack = append(stack, k)
		}
		if el.Role == "TR" {
			headerScopes(el, parent[el])
		}
	}
	t := &tagWalk{parent: parent, seen: map[*typeset.StructElem]bool{}, events: map[*typeset.StructElem][]tagEvent{}}
	t.walk(rb, 0)
	budget := typeset.MaxStructElems - count
	for _, el := range t.order {
		budget = wrapContent(el, t.events[el], budget)
	}
	own := map[*typeset.StructElem]bool{}
	for _, el := range t.order {
		for _, ev := range t.events[el] {
			if ev.leaf != nil && ev.leaf.elem == el || ev.mark != nil && ev.mark.elem == el {
				own[el] = true
			}
		}
	}
	budget = hoistBlocks(e.root, own, budget, 0)
	wrapFragments(e.root, e.root.Role, budget, 0)
}

// headerScopes sets the Scope of each header cell of the row tr, under
// group, that its node left unset: ISO 14289-1, 7.5 asks a TH for a Scope
// when no Headers attribute says which cells it heads. A header cell heads
// its column in a header row group (ISO 32000-1, 14.8.4.3.4, Table 337) or
// in a row of header cells alone, and its row otherwise.
func headerScopes(tr, group *typeset.StructElem) {
	scope := typeset.ScopeColumn
	if group == nil || group.Role != "THead" {
		for _, k := range tr.Kids {
			if k.Role == "TD" {
				scope = typeset.ScopeRow
				break
			}
		}
	}
	for _, k := range tr.Kids {
		if k.Role == "TH" && k.Scope == typeset.ScopeNone {
			k.Scope = scope
		}
	}
}

// tagWalk gathers the events of every element in document order.
type tagWalk struct {
	parent map[*typeset.StructElem]*typeset.StructElem
	seen   map[*typeset.StructElem]bool
	events map[*typeset.StructElem][]tagEvent
	// order lists the elements with events, first met first.
	order []*typeset.StructElem
	runs  int
}

func (t *tagWalk) add(el *typeset.StructElem, ev tagEvent) {
	if _, ok := t.events[el]; !ok {
		t.order = append(t.order, el)
	}
	t.events[el] = append(t.events[el], ev)
}

// walk records the events of b and its descendants. run is the inline
// formatting context b sits in, 0 for none.
func (t *tagWalk) walk(b *box, run int) {
	if b == nil {
		return
	}
	inlineLevel := run != 0 && b.inline && b.kind != kindBlock && b.float == css.FloatNone && !b.abs
	if el := b.elem; el != nil && !t.seen[el] {
		t.seen[el] = true
		if p, ok := t.parent[el]; ok {
			r := 0
			if inlineLevel {
				r = run
			}
			t.add(p, tagEvent{kid: el, run: r})
		}
	}
	if b.outside != nil && b.outside.elem != nil {
		if el := b.outside.elem; !t.seen[el] {
			t.seen[el] = true
			if p, ok := t.parent[el]; ok {
				t.add(p, tagEvent{kid: el})
			}
		}
		t.add(b.outside.elem, tagEvent{mark: b.outside})
	}
	if b.elem != nil && (b.kind == kindText || b.kind == kindReplaced && !b.embeds() || b.chain != nil) {
		r := 0
		if inlineLevel {
			r = run
		}
		t.add(b.elem, tagEvent{leaf: b, run: r})
	}
	if b.fnBody != nil {
		t.walk(b.fnBody, 0)
	}
	kidRun := 0
	switch {
	case b.kind == kindInline:
		kidRun = run
	case b.inlineKids:
		t.runs++
		kidRun = t.runs
	}
	for _, k := range b.kids {
		t.walk(k, kidRun)
	}
}

// wrapContent rebuilds the kids of el from its events evs, as
// [engine.anonymize] says, creating at most budget elements, and returns
// the budget left.
func wrapContent(el *typeset.StructElem, evs []tagEvent, budget int) int {
	if opaqueRoles[el.Role] {
		if el.Role != "Figure" && el.Role != "Formula" && el.Role != "Form" {
			// A list, a table and its parts hold no content (ISO 32005,
			// Table 5): the white space they hold between their items, a
			// space an item of display: contents leaves at a line's end,
			// is a layout artifact (ISO 32000-1, 14.8.2.2.1).
			for _, ev := range evs {
				if ev.leaf != nil && isSpace(ev.leaf) {
					ev.leaf.elem = nil
				}
			}
		}
		return budget
	}
	hasContent, hasKid, blockKid := false, false, false
	for _, ev := range evs {
		if ev.kid == nil {
			hasContent = true
			continue
		}
		hasKid = true
		// A block-level element stands among blocks even where an
		// inline-level box starts it.
		blockKid = blockKid || ev.run == 0 || blockRoles[ev.kid.Role]
	}
	paragraphs := groupingRoles[el.Role] || containerRoles[el.Role] && blockKid
	switch {
	case paragraphs && (hasContent || groupingRoles[el.Role] && hasKid):
		return rebuild(el, evs, budget, true)
	case !paragraphs && hasContent && hasKid:
		return rebuild(el, evs, budget, false)
	}
	return budget
}

// rebuild replaces the kids of el by its events in order, each segment of
// own content in an anonymous element: with paragraphs, a P per run of an
// inline formatting context, its inline-level kids moved into it; without,
// a Span per run of content between kids.
func rebuild(el *typeset.StructElem, evs []tagEvent, budget int, paragraphs bool) int {
	var kids []*typeset.StructElem
	placed := map[*typeset.StructElem]bool{}
	for i := 0; i < len(evs); {
		ev := evs[i]
		key := segment(ev, paragraphs)
		if key < 0 {
			kids = append(kids, ev.kid)
			placed[ev.kid] = true
			i++
			continue
		}
		j := i + 1
		for j < len(evs) && segment(evs[j], paragraphs) == key {
			j++
		}
		seg := evs[i:j]
		if budget <= 0 {
			// Past the cap the content stays with el, and its kids keep
			// their place.
			for _, s := range seg {
				if s.kid != nil {
					kids = append(kids, s.kid)
					placed[s.kid] = true
				}
			}
			i = j
			continue
		}
		role := "Span"
		if paragraphs {
			role = "P"
		}
		anon := &typeset.StructElem{Role: role}
		budget--
		for _, s := range seg {
			switch {
			case s.kid != nil:
				anon.Kids = append(anon.Kids, s.kid)
				placed[s.kid] = true
			case s.leaf != nil:
				s.leaf.elem = anon
			case s.mark != nil:
				s.mark.elem = anon
			}
		}
		kids = append(kids, anon)
		if paragraphs && len(anon.Kids) > 0 {
			budget = wrapContent(anon, seg, budget)
		}
		i = j
	}
	for _, k := range el.Kids {
		if !placed[k] {
			kids = append(kids, k)
		}
	}
	el.Kids = kids
	return budget
}

// inlineRoles are the inline-level structure types (ISO 32000-1, 14.8.4.4,
// Table 338; ISO 32000-2, 14.8.4.7): a grouping element holds them inside
// a paragraph, never as its own kids (ISO 32005, Table 5). Immutable
// lookup table.
var inlineRoles = map[string]bool{
	"Span": true, "Quote": true, "Note": true, "Reference": true, "BibEntry": true,
	"Code": true, "Link": true, "Annot": true, "Ruby": true, "Warichu": true,
	"Em": true, "Strong": true, "Sub": true,
}

// blockRoles are the block-level structure types (ISO 32000-1, 14.8.4.2
// and 14.8.4.3) a paragraph never holds (ISO 32005, Table 5): the grouping
// elements, paragraphs, headings, lists and tables. A kid of one of them
// whose content sits in an inline formatting context, an element of
// display: contents, stays a kid between the paragraphs of the run.
// Immutable lookup table.
var blockRoles = map[string]bool{
	"Document": true, "DocumentFragment": true, "Part": true, "Art": true, "Sect": true,
	"Div": true, "BlockQuote": true, "Aside": true, "NonStruct": true, "Index": true,
	"P": true, "H": true, "H1": true, "H2": true, "H3": true, "H4": true, "H5": true, "H6": true,
	"L": true, "LI": true, "Table": true, "TOC": true, "TOCI": true,
}

// segment returns the segment ev belongs to, -1 for a kid that stays a kid:
// own content groups by its inline formatting context, and with
// paragraphs so do the inline-level kids of a run but those of
// [blockRoles], an inline element
// standing alone among blocks, a flex or grid item, joining the content
// outside any run.
func segment(ev tagEvent, paragraphs bool) int {
	switch {
	case ev.kid == nil:
		return ev.run
	case !paragraphs, blockRoles[ev.kid.Role]:
		return -1
	case ev.run != 0:
		return ev.run
	case inlineRoles[ev.kid.Role]:
		return 0
	}
	return -1
}

// phraseRoles are the elements that hold phrasing alone: a paragraph, a
// heading and the inline elements that ISO 32005, Table 5 forbids every
// grouping and block-level element in. Each maps to the roles it may not
// hold: [blockRoles] and the table and list parts, less L and Table under
// P, which Table 5 lets a paragraph hold. Immutable lookup table.
var phraseRoles = map[string]func(string) bool{
	"P": func(r string) bool { return r != "L" && r != "Table" && heldByNone(r) },
	"H": heldByNone, "H1": heldByNone, "H2": heldByNone, "H3": heldByNone, "H4": heldByNone, "H5": heldByNone, "H6": heldByNone,
	"Span": heldByNone, "Em": heldByNone, "Strong": heldByNone, "Sub": heldByNone, "Quote": heldByNone,
}

// heldByNone reports a role no element of [phraseRoles] holds (ISO 32005,
// Table 5): the grouping and block-level elements, list items and their
// bodies, captions and the parts of a table.
func heldByNone(role string) bool {
	switch role {
	case "Caption", "LBody", "THead", "TBody", "TFoot", "TR", "TH", "TD", "Title":
		return true
	}
	return blockRoles[role]
}

// hoistBlocks splits, under el and below it, every element of
// [phraseRoles] around the kids it may not hold, as [engine.anonymize]
// says: the kid moves up beside its parent, between the parent's part
// before it and a copy of the parent, of the same role and language,
// holding what came after. The deepest elements split first, so a block
// in a Span in a P rises past both. An element that own says holds
// content of its own stays whole; so does the tree past [maxStructDepth]
// levels, and an element whose copies would pass budget. It returns the
// budget left.
func hoistBlocks(el *typeset.StructElem, own map[*typeset.StructElem]bool, budget, depth int) int {
	if depth > maxStructDepth {
		return budget
	}
	var kids []*typeset.StructElem
	changed := false
	for i, k := range el.Kids {
		budget = hoistBlocks(k, own, budget, depth+1)
		forbids := phraseRoles[k.Role]
		copies, split := 0, false
		if forbids != nil && !own[k] {
			copies, split = splitCopies(k.Kids, forbids)
		}
		if !split || copies > budget {
			if changed {
				kids = append(kids, k)
			}
			continue
		}
		if !changed {
			kids = append(kids, el.Kids[:i]...)
			changed = true
		}
		budget -= copies
		held := k.Kids
		part := k
		part.Kids = nil
		kids = append(kids, part)
		for _, c := range held {
			if forbids(c.Role) {
				kids = append(kids, c)
				part = nil
				continue
			}
			if part == nil {
				part = &typeset.StructElem{Role: k.Role, Lang: k.Lang}
				kids = append(kids, part)
			}
			part.Kids = append(part.Kids, c)
		}
	}
	if changed {
		el.Kids = kids
	}
	return budget
}

// splitCopies reports whether kids hold one that forbids takes, and how
// many copies of their parent splitting them around those makes: one for
// each run of other kids that follows one of them.
func splitCopies(kids []*typeset.StructElem, forbids func(string) bool) (int, bool) {
	n, split, after := 0, false, false
	for _, c := range kids {
		switch {
		case forbids(c.Role):
			split, after = true, true
		case after:
			n++
			after = false
		}
	}
	return n, split
}

// fragmentHolders are the elements that may hold a DocumentFragment (ISO
// 32005, Table 5): the grouping elements that Table 5 does not look
// through, but the index and a table of contents item. Immutable lookup
// table.
var fragmentHolders = map[string]bool{
	"Document": true, "DocumentFragment": true, "Art": true, "Sect": true,
	"BlockQuote": true, "Aside": true, "Private": true,
}

// passThroughRoles are the grouping elements ISO 32005, Table 5 looks
// through: it judges what a Div, a Part or a NonStruct holds as held by
// the element around it, as veraPDF does (compliance's
// iso32005PassThroughTypes). Immutable lookup table.
var passThroughRoles = map[string]bool{"Div": true, "Part": true, "NonStruct": true}

// wrapFragments gives every DocumentFragment under el whose holder may not
// hold one a Sect of its own, which a table cell, a list body, a caption
// and a figure may hold and which may hold a DocumentFragment (ISO 32005,
// Table 5); a Div would not do, Table 5 looking through it. holder is the
// role of el, or of the element around it when Table 5 looks through el.
// It goes [maxStructDepth] levels deep and makes at most budget elements.
func wrapFragments(el *typeset.StructElem, holder string, budget, depth int) int {
	if depth > maxStructDepth {
		return budget
	}
	for i, k := range el.Kids {
		kh := k.Role
		if passThroughRoles[kh] {
			kh = holder
		}
		budget = wrapFragments(k, kh, budget, depth+1)
		if k.Role == "DocumentFragment" && !fragmentHolders[holder] && budget > 0 {
			el.Kids[i] = &typeset.StructElem{Role: "Sect", Kids: []*typeset.StructElem{k}}
			budget--
		}
	}
	return budget
}
