package layout

import (
	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
)

// tagEvent is one thing a structure element holds, in document order: a
// child element, met at its first box, or a piece of the element's own
// content, a box that paints content or an outside list marker.
type tagEvent struct {
	kid  *compose.StructElem
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
// Anonymous elements add two levels at most below [maxStructDepth], and
// stop once the tree holds [compose.MaxStructElems] elements: the content
// past that stays with its element.
func (e *engine) anonymize(rb *box) {
	parent := map[*compose.StructElem]*compose.StructElem{}
	count := 0
	stack := []*compose.StructElem{e.root}
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
	t := &tagWalk{parent: parent, seen: map[*compose.StructElem]bool{}, events: map[*compose.StructElem][]tagEvent{}}
	t.walk(rb, 0)
	budget := compose.MaxStructElems - count
	for _, el := range t.order {
		budget = wrapContent(el, t.events[el], budget)
	}
}

// headerScopes sets the Scope of each header cell of the row tr, under
// group, that its node left unset: ISO 14289-1, 7.5 asks a TH for a Scope
// when no Headers attribute says which cells it heads. A header cell heads
// its column in a header row group (ISO 32000-1, 14.8.4.3.4, Table 337) or
// in a row of header cells alone, and its row otherwise.
func headerScopes(tr, group *compose.StructElem) {
	scope := compose.ScopeColumn
	if group == nil || group.Role != "THead" {
		for _, k := range tr.Kids {
			if k.Role == "TD" {
				scope = compose.ScopeRow
				break
			}
		}
	}
	for _, k := range tr.Kids {
		if k.Role == "TH" && k.Scope == compose.ScopeNone {
			k.Scope = scope
		}
	}
}

// tagWalk gathers the events of every element in document order.
type tagWalk struct {
	parent map[*compose.StructElem]*compose.StructElem
	seen   map[*compose.StructElem]bool
	events map[*compose.StructElem][]tagEvent
	// order lists the elements with events, first met first.
	order []*compose.StructElem
	runs  int
}

func (t *tagWalk) add(el *compose.StructElem, ev tagEvent) {
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
	if b.elem != nil && (b.kind == kindText || b.kind == kindReplaced || b.chain != nil) {
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
func wrapContent(el *compose.StructElem, evs []tagEvent, budget int) int {
	if opaqueRoles[el.Role] {
		return budget
	}
	hasContent, hasKid, blockKid := false, false, false
	for _, ev := range evs {
		if ev.kid == nil {
			hasContent = true
			continue
		}
		hasKid = true
		blockKid = blockKid || ev.run == 0
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
func rebuild(el *compose.StructElem, evs []tagEvent, budget int, paragraphs bool) int {
	var kids []*compose.StructElem
	placed := map[*compose.StructElem]bool{}
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
		anon := &compose.StructElem{Role: role}
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

// segment returns the segment ev belongs to, -1 for a kid that stays a kid:
// own content groups by its inline formatting context, and with
// paragraphs so do the inline-level kids of a run, an inline element
// standing alone among blocks, a flex or grid item, joining the content
// outside any run.
func segment(ev tagEvent, paragraphs bool) int {
	switch {
	case ev.kid == nil:
		return ev.run
	case !paragraphs:
		return -1
	case ev.run != 0:
		return ev.run
	case inlineRoles[ev.kid.Role]:
		return 0
	}
	return -1
}
