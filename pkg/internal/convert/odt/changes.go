package odt

import (
	"context"
	"encoding/xml"
	"fmt"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/odf"
)

// maxTrackedChanges bounds the tracked changes a document's view is built
// from: 20,000. Anti-DoS guard, not spec-mandated: each change rebuilds
// the path from the body to its marks, so the work grows with the changes
// times the body's width; a heavily reviewed contract holds a few
// thousand. Past it, the remaining changes print as the file stores them
// (insertions in, deletions out), with a warning.
const maxTrackedChanges = 20_000

// changeKind is what a changed region records (ODF 1.3, Part 3, 5.5.3 to
// 5.5.5).
type changeKind uint8

const (
	changeInsertion changeKind = iota + 1
	changeDeletion
	changeFormat
)

// change is one text:changed-region (ODF 1.3, Part 3, 5.5.2): its kind,
// its author (dc:creator of office:change-info, 5.5.6) and, for a
// deletion, the content deleted, the blocks a text:change mark stands
// for.
type change struct {
	kind    changeKind
	author  string
	content []*odf.Element
	// spliced is set once the content is put back: a mark repeated
	// with the same id does not put it back twice.
	spliced bool
}

// changeMark is how the marked view draws the text of a change: in its
// author's colour, underlined for an insertion, struck through for a
// deletion.
type changeMark struct {
	color  css.Color
	insert bool
}

// authorColors are the colours of the authors of a marked view, the
// first author of the document in the first, cycling past the ninth.
//
// # Corpus evidence
//
//   - probe, odt: LibreOffice 26.8 (the pinned gotenberg/gotenberg:9.0.0
//     image, soffice --convert-to pdf, changes shown) draws the changes of
//     eleven authors in these nine colours, in the order the authors
//     first appear in the body, the tenth and eleventh in the first two
//     again; an insertion underlined and a deletion struck through, line
//     and text in the colour.
//
//nolint:gochecknoglobals // static palette
var authorColors = [...]css.Color{
	rgb(0xC6, 0x92, 0x00), rgb(0x06, 0x46, 0xA2), rgb(0x57, 0x9D, 0x1C),
	rgb(0x69, 0x2B, 0x9D), rgb(0xC5, 0x00, 0x0B), rgb(0x00, 0x80, 0x80),
	rgb(0x8C, 0x84, 0x00), rgb(0x35, 0x55, 0x6B), rgb(0xD1, 0x76, 0x00),
}

// rgb returns the opaque colour of 8-bit components.
func rgb(r, g, b uint8) css.Color {
	return css.Color{R: float64(r) / 255, G: float64(g) / 255, B: float64(b) / 255, A: 1}
}

// changes builds the text of a tracked-changes view of a text body in
// place (ODF 1.3, Part 3, 5.5).
type changes struct {
	ctx     context.Context
	body    *odf.Element
	view    office.TrackedChanges
	regions map[string]*change
	// parent maps every node of the body to the element holding it,
	// kept current as the tree is rebuilt.
	parent map[*odf.Element]*odf.Element
	// starts, ends and points are the text:change-start, text:change-end
	// and text:change marks, by change id, in document order.
	starts, ends map[string]*odf.Element
	points       map[string]*odf.Element
	order        []*odf.Element
	// rowTracked reports a table row whose changes are the row's own
	// (LibreOffice's loext:text-changes-only false).
	rowTracked func(*odf.Element) bool
	warn       func(code, msg string)
}

// resolveChanges rewrites body, the office:text of a document, into the
// text view prints, and returns how the marked view draws each change
// by id, nil for the other views. The accepted view drops what the
// deletions span; the rejected view drops what the insertions span and
// puts back the text the deletions removed; the marked view puts it back
// too, both kept, each change's text drawn by its mark. A formatting
// change prints with its new formatting under every view: the file keeps
// no record of the old one but what text:format-change names (5.5.5).
func resolveChanges(ctx context.Context, body *odf.Element, view office.TrackedChanges, rowTracked func(*odf.Element) bool, warn func(code, msg string)) (map[string]changeMark, error) {
	tc := body.Child(odf.NSText, "tracked-changes")
	if tc == nil {
		return nil, nil
	}
	c := &changes{
		ctx: ctx, body: body, view: view, regions: map[string]*change{},
		parent: map[*odf.Element]*odf.Element{},
		starts: map[string]*odf.Element{}, ends: map[string]*odf.Element{}, points: map[string]*odf.Element{},
		rowTracked: rowTracked, warn: warn,
	}
	c.readRegions(tc)
	if len(c.regions) == 0 {
		return nil, nil
	}
	c.index(body, nil, 0)
	var marks map[string]changeMark
	switch view {
	case office.ChangesAccepted:
		c.dropRows(changeDeletion)
		if err := c.cutAll(changeDeletion); err != nil {
			return nil, err
		}
	case office.ChangesRejected:
		c.dropRows(changeInsertion)
		if err := c.cutAll(changeInsertion); err != nil {
			return nil, err
		}
		if err := c.spliceAll(false); err != nil {
			return nil, err
		}
	case office.ChangesMarked:
		if err := c.spliceAll(true); err != nil {
			return nil, err
		}
		marks = c.marks()
	}
	return marks, nil
}

// readRegions reads the changed regions of text:tracked-changes, at most
// [maxTrackedChanges].
func (c *changes) readRegions(tc *odf.Element) {
	for _, r := range tc.Children {
		if !r.Is(odf.NSText, "changed-region") {
			continue
		}
		if len(c.regions) >= maxTrackedChanges {
			c.warn("tracked-changes-cap", fmt.Sprintf("changes past the first %d print as the file stores them", maxTrackedChanges))
			return
		}
		// ODF 1.3 names a region by xml:id; text:id is its ODF 1.2 name,
		// which LibreOffice writes beside it (5.5.2).
		id := r.A(odf.NSText, "id")
		if id == "" {
			id = r.A(odf.NSXML, "id")
		}
		if id == "" {
			continue
		}
		for _, k := range r.Children {
			ch := &change{}
			switch {
			case k.Is(odf.NSText, "insertion"):
				ch.kind = changeInsertion
			case k.Is(odf.NSText, "deletion"):
				ch.kind = changeDeletion
				for _, b := range k.Children {
					if !b.IsText() && !b.Is(odf.NSOffice, "change-info") {
						ch.content = append(ch.content, b)
					}
				}
			case k.Is(odf.NSText, "format-change"):
				ch.kind = changeFormat
			default:
				continue
			}
			ch.author = strings.TrimSpace(k.Child(odf.NSOffice, "change-info").Child(odf.NSDC, "creator").TextContent())
			c.regions[id] = ch
			break
		}
	}
}

// index records the parent of every node under el and the change marks,
// skipping text:tracked-changes.
func (c *changes) index(el, parent *odf.Element, depth int) {
	if depth > odf.MaxDepth {
		return
	}
	if parent != nil {
		c.parent[el] = parent
	}
	if el.Is(odf.NSText, "tracked-changes") {
		return
	}
	if el.Name.Space == odf.NSText {
		id := el.A(odf.NSText, "change-id")
		switch el.Name.Local {
		case "change-start":
			c.mark(c.starts, id, el)
		case "change-end":
			c.mark(c.ends, id, el)
		case "change":
			c.mark(c.points, id, el)
		}
	}
	for _, k := range el.Children {
		c.index(k, el, depth+1)
	}
}

// mark records the first mark of a known change.
func (c *changes) mark(m map[string]*odf.Element, id string, el *odf.Element) {
	if _, ok := c.regions[id]; !ok {
		return
	}
	if _, seen := m[id]; seen {
		return
	}
	m[id] = el
	c.order = append(c.order, el)
}

// attached reports whether el still hangs from the body.
func (c *changes) attached(el *odf.Element) bool {
	for range odf.MaxDepth + 1 {
		if el == c.body {
			return true
		}
		p, ok := c.parent[el]
		if !ok {
			return false
		}
		if !slices.Contains(p.Children, el) {
			return false
		}
		el = p
	}
	return false
}

// chain returns the elements from the body down to el, both included.
func (c *changes) chain(el *odf.Element) []*odf.Element {
	var out []*odf.Element
	for range odf.MaxDepth + 1 {
		out = append(out, el)
		if el == c.body {
			slices.Reverse(out)
			return out
		}
		el = c.parent[el]
		if el == nil {
			return nil
		}
	}
	return nil
}

// setKids replaces el's children, updating their parents.
func (c *changes) setKids(el *odf.Element, kids []*odf.Element) {
	el.Children = kids
	for _, k := range kids {
		c.parent[k] = el
	}
}

// clone returns a shallow copy of el with kids for children.
func (c *changes) clone(el *odf.Element, kids []*odf.Element) *odf.Element {
	n := &odf.Element{Name: el.Name, Attr: el.Attr, Text: el.Text}
	c.setKids(n, kids)
	return n
}

// before returns a copy of el holding what precedes the mark at the end
// of path, el's descendants down to the mark; nil when el is the mark.
func (c *changes) before(path []*odf.Element) *odf.Element {
	if len(path) < 2 {
		return nil
	}
	el, next := path[0], path[1]
	i := slices.Index(el.Children, next)
	kids := slices.Clone(el.Children[:max(i, 0)])
	if sub := c.before(path[1:]); sub != nil {
		kids = append(kids, sub)
	}
	return c.clone(el, kids)
}

// after returns a copy of el holding what follows the mark at the end of
// path; nil when el is the mark.
func (c *changes) after(path []*odf.Element) *odf.Element {
	if len(path) < 2 {
		return nil
	}
	el, next := path[0], path[1]
	i := slices.Index(el.Children, next)
	var kids []*odf.Element
	if sub := c.after(path[1:]); sub != nil {
		kids = append(kids, sub)
	}
	kids = append(kids, el.Children[i+1:]...)
	return c.clone(el, kids)
}

// isPara reports a text:p or text:h.
func isPara(el *odf.Element) bool {
	return el != nil && el.Name.Space == odf.NSText && (el.Name.Local == "p" || el.Name.Local == "h")
}

// printsNothing reports an element whose content holds no character,
// frame or field: what is left of a paragraph a change starts at.
func printsNothing(el *odf.Element) bool {
	for _, k := range el.Children {
		if k.IsText() {
			if strings.TrimSpace(k.Text) != "" {
				return false
			}
			continue
		}
		switch {
		case k.Is(odf.NSText, "span"), k.Is(odf.NSText, "a"):
			if !printsNothing(k) {
				return false
			}
		case k.Name.Space == odf.NSText && (strings.HasPrefix(k.Name.Local, "bookmark") || strings.HasPrefix(k.Name.Local, "change") || k.Name.Local == "soft-page-break"):
		default:
			return false
		}
	}
	return true
}

// cutAll drops the text every change of kind spans, from its
// text:change-start to its text:change-end (5.5.7), in document order.
func (c *changes) cutAll(kind changeKind) error {
	for n, m := range c.order {
		if n%256 == 0 {
			if err := c.ctx.Err(); err != nil {
				return err
			}
		}
		if !m.Is(odf.NSText, "change-start") {
			continue
		}
		id := m.A(odf.NSText, "change-id")
		if c.regions[id].kind != kind {
			continue
		}
		end := c.ends[id]
		if end == nil || !c.attached(m) || !c.attached(end) {
			continue
		}
		c.cut(m, end)
	}
	return nil
}

// cut drops what lies between the marks start and end. When they sit in
// two paragraphs, the paragraph start opens takes what follows end in
// the other: the paragraph break between them is part of the change,
// and the paragraph keeps the properties of the one start sits in,
// unless the change starts that paragraph, which then goes whole.
//
// # Corpus evidence
//
//   - probe, odt: LibreOffice 26.8 accepting a deletion from inside one
//     paragraph to the start of another past a table prints one
//     paragraph of the first's style; rejecting an insertion spanning a
//     whole centred paragraph and the break after it prints the next
//     paragraph left-aligned, in its own style.
func (c *changes) cut(start, end *odf.Element) {
	ps, pe := c.chain(start), c.chain(end)
	if ps == nil || pe == nil {
		return
	}
	l := 0
	for l < len(ps) && l < len(pe) && ps[l] == pe[l] {
		l++
	}
	if l == 0 || l >= len(ps) || l >= len(pe) {
		return
	}
	container := ps[l-1]
	ia, ib := slices.Index(container.Children, ps[l]), slices.Index(container.Children, pe[l])
	if ia < 0 || ib < ia {
		return
	}
	left, right := c.before(ps[l:]), c.after(pe[l:])
	var mid []*odf.Element
	switch {
	case isPara(left) && isPara(right):
		if printsNothing(left) {
			mid = append(mid, right)
		} else {
			c.setKids(left, append(left.Children, right.Children...))
			mid = append(mid, left)
		}
	default:
		for _, n := range []*odf.Element{left, right} {
			if n != nil {
				mid = append(mid, n)
			}
		}
	}
	kids := slices.Concat(container.Children[:ia], mid, container.Children[ib+1:])
	c.setKids(container, kids)
}

// spliceAll puts back the text of every deletion at its text:change mark
// (5.5.7), between a change-start and a change-end of the deletion's id
// when wrap is set, for the marked view to draw.
func (c *changes) spliceAll(wrap bool) error {
	for n, m := range c.order {
		if n%256 == 0 {
			if err := c.ctx.Err(); err != nil {
				return err
			}
		}
		if !m.Is(odf.NSText, "change") {
			continue
		}
		id := m.A(odf.NSText, "change-id")
		ch := c.regions[id]
		if ch.kind != changeDeletion || ch.spliced || !c.attached(m) {
			continue
		}
		ch.spliced = true
		c.splice(m, id, ch.content, wrap)
	}
	return nil
}

// changeMarker returns a text:change-start or text:change-end of id.
func changeMarker(local, id string) *odf.Element {
	return &odf.Element{
		Name: xml.Name{Space: odf.NSText, Local: local},
		Attr: []xml.Attr{{Name: xml.Name{Space: odf.NSText, Local: "change-id"}, Value: id}},
	}
}

// splice replaces the mark m by the deleted blocks: one paragraph's
// content in the text around m; more split the paragraph holding m,
// the first block's content ending the part before m and the last's
// starting the part after, both parts keeping that paragraph's
// properties, the blocks between standing whole.
//
// # Corpus evidence
//
//   - probe, odt: LibreOffice 26.8 rejecting the deletion of a centred
//     italic paragraph, recorded at the start of the next paragraph,
//     prints it in the next paragraph's style, left-aligned and upright;
//     so it draws it with the changes shown.
func (c *changes) splice(m *odf.Element, id string, blocks []*odf.Element, wrap bool) {
	path := c.chain(m)
	if path == nil {
		return
	}
	host := -1
	for i := len(path) - 2; i >= 0; i-- {
		if isPara(path[i]) {
			host = i
			break
		}
	}
	open, closing := []*odf.Element(nil), []*odf.Element(nil)
	if wrap {
		open, closing = []*odf.Element{changeMarker("change-start", id)}, []*odf.Element{changeMarker("change-end", id)}
	}
	inline := func(b *odf.Element) []*odf.Element {
		if isPara(b) {
			return b.Children
		}
		return nil
	}
	if host < 0 {
		// A mark between blocks: the blocks go in its place.
		parent := path[len(path)-2]
		i := slices.Index(parent.Children, m)
		var kids []*odf.Element
		kids = append(kids, open...)
		kids = append(kids, blocks...)
		kids = append(kids, closing...)
		c.setKids(parent, slices.Concat(parent.Children[:i], kids, parent.Children[i+1:]))
		return
	}
	if len(blocks) == 1 && isPara(blocks[0]) {
		parent := path[len(path)-2]
		i := slices.Index(parent.Children, m)
		kids := slices.Concat(open, inline(blocks[0]), closing)
		c.setKids(parent, slices.Concat(parent.Children[:i], kids, parent.Children[i+1:]))
		return
	}
	if len(blocks) == 0 {
		return
	}
	h := path[host]
	left, right := c.before(path[host:]), c.after(path[host:])
	first, last := blocks[0], blocks[len(blocks)-1]
	var out []*odf.Element
	if isPara(first) {
		c.setKids(left, slices.Concat(left.Children, open, inline(first)))
		out = append(out, left)
	} else {
		c.setKids(left, slices.Concat(left.Children, open))
		out = append(out, left, first)
	}
	if len(blocks) > 1 {
		out = append(out, blocks[1:len(blocks)-1]...)
	}
	if isPara(last) && len(blocks) > 1 {
		c.setKids(right, slices.Concat(inline(last), closing, right.Children))
		out = append(out, right)
	} else {
		if len(blocks) > 1 {
			out = append(out, last)
		}
		c.setKids(right, slices.Concat(closing, right.Children))
		out = append(out, right)
	}
	keepLeadingSpace(right)
	grand := path[host-1]
	i := slices.Index(grand.Children, h)
	c.setKids(grand, slices.Concat(grand.Children[:i], out, grand.Children[i+1:]))
}

// keepLeadingSpace turns a space opening the paragraph p, which the
// paragraph's white space rules would drop (ODF 1.3, Part 3, 6.1.2), into
// a text:s: the space was inside a paragraph before the split put it
// first.
//
// # Corpus evidence
//
//   - probe, odt: LibreOffice 26.8 rejecting a deleted paragraph break
//     recorded before " with the next one." prints the second paragraph
//     with its opening space.
func keepLeadingSpace(p *odf.Element) {
	el := p
	for range odf.MaxDepth {
		// Change marks print nothing: the first text may follow one.
		i := slices.IndexFunc(el.Children, func(k *odf.Element) bool {
			return !k.Is(odf.NSText, "change-start") && !k.Is(odf.NSText, "change-end")
		})
		if i < 0 {
			return
		}
		first := el.Children[i]
		if first.IsText() {
			if !strings.HasPrefix(first.Text, " ") {
				return
			}
			space := &odf.Element{Name: xml.Name{Space: odf.NSText, Local: "s"}}
			rest := &odf.Element{Text: first.Text[1:]}
			el.Children = slices.Concat(el.Children[:i], []*odf.Element{space, rest}, el.Children[i+1:])
			return
		}
		if !first.Is(odf.NSText, "span") {
			return
		}
		el = first
	}
}

// dropRows drops the table rows whose changes are the row's own and whose
// content every change of kind spans, every cell holding one and nothing
// outside it.
//
// # Corpus evidence
//
//   - libreoffice/odt layout/changed_table_rows: LibreOffice prints a
//     table without the row whose three cells are deleted, the row's
//     style setting loext:text-changes-only false, and keeps the row
//     whose cells are inserted.
//   - probe, odt: LibreOffice 26.8 rejecting the changes of that document
//     prints the deleted row and drops the inserted one; a row so styled
//     whose content is deleted but for an empty cell stays, empty, once
//     the changes are accepted.
func (c *changes) dropRows(kind changeKind) {
	var walk func(el *odf.Element, depth int)
	walk = func(el *odf.Element, depth int) {
		if depth > odf.MaxDepth {
			return
		}
		var kept []*odf.Element
		dropped := false
		for _, k := range el.Children {
			if k.Is(odf.NSTable, "table-row") && c.rowTracked(k) && c.rowChanged(k, kind) {
				dropped = true
				continue
			}
			kept = append(kept, k)
			walk(k, depth+1)
		}
		if dropped {
			c.setKids(el, kept)
		}
	}
	walk(c.body, 0)
}

// rowChanged reports a row each of whose cells a change of kind covers
// whole.
func (c *changes) rowChanged(row *odf.Element, kind changeKind) bool {
	cells := 0
	for _, cell := range row.Children {
		if !cell.Is(odf.NSTable, "table-cell") {
			continue
		}
		cells++
		open := map[string]bool{}
		seen := false
		ok := true
		var walk func(el *odf.Element, depth int)
		walk = func(el *odf.Element, depth int) {
			if depth > odf.MaxDepth || !ok {
				return
			}
			for _, k := range el.Children {
				switch {
				case k.IsText():
					if len(open) == 0 && strings.TrimSpace(k.Text) != "" {
						ok = false
					}
				case k.Is(odf.NSText, "change-start"):
					id := k.A(odf.NSText, "change-id")
					if r := c.regions[id]; r != nil && r.kind == kind {
						open[id], seen = true, true
					}
				case k.Is(odf.NSText, "change-end"):
					delete(open, k.A(odf.NSText, "change-id"))
				case k.Name.Space == odf.NSDraw && len(open) == 0:
					ok = false
				default:
					walk(k, depth+1)
				}
			}
		}
		walk(cell, 0)
		if !ok || !seen {
			return false
		}
	}
	return cells > 0
}

// marks returns how the marked view draws each insertion and deletion,
// the authors coloured in the order the body first names them.
func (c *changes) marks() map[string]changeMark {
	out := map[string]changeMark{}
	authors := map[string]int{}
	for _, m := range c.order {
		id := m.A(odf.NSText, "change-id")
		ch := c.regions[id]
		n, ok := authors[ch.author]
		if !ok {
			n = len(authors)
			authors[ch.author] = n
		}
		if ch.kind == changeFormat {
			continue
		}
		out[id] = changeMark{color: authorColors[n%len(authorColors)], insert: ch.kind == changeInsertion}
	}
	return out
}

// rowTracked reports a table row whose style says its changes are the
// row's own, not its text's alone: LibreOffice's
// loext:text-changes-only false, an extension of the row properties.
func (b *builder) rowTracked(row *odf.Element) bool {
	props := b.styles.Resolve(b.scope, odf.FamilyRow, row.A(odf.NSTable, "style-name"), odf.RowProps)
	return props.Get("loext:text-changes-only") == "false"
}
