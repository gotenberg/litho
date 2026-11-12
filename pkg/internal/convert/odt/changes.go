package odt

import (
	"context"
	"encoding/xml"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/limits"
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

// changeMark is how the marked view draws the text of a change: its kind
// and the colour of its author ([office.MarkColor]).
type changeMark struct {
	color css.Color
	kind  office.MarkKind
}

// changes builds the text of a tracked-changes view of a text body in
// place (ODF 1.3, Part 3, 5.5).
type changes struct {
	body    *odf.Element
	regions map[string]*change
	// parent maps the change marks, their ancestors and the nodes the
	// view rebuilt to the element holding each, kept current as the tree
	// is rebuilt.
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
		body: body, regions: map[string]*change{},
		parent: map[*odf.Element]*odf.Element{},
		starts: map[string]*odf.Element{}, ends: map[string]*odf.Element{}, points: map[string]*odf.Element{},
		rowTracked: rowTracked, warn: warn,
	}
	c.readRegions(tc)
	if len(c.regions) == 0 {
		return nil, nil
	}
	c.index(body, nil)
	var marks map[string]changeMark
	switch view {
	case office.ChangesAccepted:
		c.dropRows(changeDeletion)
		if err := c.cutAll(ctx, changeDeletion); err != nil {
			return nil, err
		}
	case office.ChangesRejected:
		c.dropRows(changeInsertion)
		if err := c.cutAll(ctx, changeInsertion); err != nil {
			return nil, err
		}
		if err := c.spliceAll(ctx, false); err != nil {
			return nil, err
		}
	case office.ChangesMarked:
		if err := c.spliceAll(ctx, true); err != nil {
			return nil, err
		}
		marks = c.marks()
	}
	// The parents recorded and the elements the view made live as long
	// as the parsed tree, and cost about as much a node.
	if err := limits.BudgetFrom(ctx).ChargeUnits(int64(len(c.parent)), limits.XMLElementCost, "XML trees"); err != nil {
		return nil, err
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

// index records the change marks under el, path holding el's ancestors
// from the body, and the parents of each mark and of its ancestors: the
// nodes the view climbs from. Text:tracked-changes is skipped.
func (c *changes) index(el *odf.Element, path []*odf.Element) {
	if len(path) > odf.MaxDepth || el.Is(odf.NSText, "tracked-changes") {
		return
	}
	if el.Name.Space == odf.NSText {
		id := el.A(odf.NSText, "change-id")
		var m map[string]*odf.Element
		switch el.Name.Local {
		case "change-start":
			m = c.starts
		case "change-end":
			m = c.ends
		case "change":
			m = c.points
		}
		if m != nil && c.mark(m, id, el) {
			child := el
			for _, a := range slices.Backward(path) {
				if _, ok := c.parent[child]; ok {
					break
				}
				c.parent[child] = a
				child = a
			}
		}
	}
	path = append(path, el)
	for _, k := range el.Children {
		if !k.IsText() {
			c.index(k, path)
		}
	}
}

// mark records the first mark of a known change, reporting whether it
// did.
func (c *changes) mark(m map[string]*odf.Element, id string, el *odf.Element) bool {
	if _, ok := c.regions[id]; !ok {
		return false
	}
	if _, seen := m[id]; seen {
		return false
	}
	m[id] = el
	c.order = append(c.order, el)
	return true
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
func (c *changes) cutAll(ctx context.Context, kind changeKind) error {
	for n, m := range c.order {
		if n%256 == 0 {
			if err := ctx.Err(); err != nil {
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
func (c *changes) spliceAll(ctx context.Context, wrap bool) error {
	for n, m := range c.order {
		if n%256 == 0 {
			if err := ctx.Err(); err != nil {
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
		kids := slices.Concat(open, blocks, closing)
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
	return rowCovered(row, func(id string) bool {
		r := c.regions[id]
		return r != nil && r.kind == kind
	})
}

// rowCovered reports a row each of whose cells changes covers whole, the
// changes whose ids of reports.
func rowCovered(row *odf.Element, of func(id string) bool) bool {
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
					if id := k.A(odf.NSText, "change-id"); of(id) {
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

// marks returns how the marked view draws each change, the authors
// coloured in the order the body first names them.
//
// # Corpus evidence
//
//   - probe, odt: LibreOffice 26.8 draws the text a text:format-change
//     spans in its author's colour, and a line holding it with a change
//     bar.
//   - probe, docx: it draws the text of a w:rPrChange or w:pPrChange
//     bold in its author's colour, a run whose new formatting is not
//     bold too: the bold is the mark of the change.
func (c *changes) marks() map[string]changeMark {
	out := map[string]changeMark{}
	authors := map[string]int{}
	moved := c.moves()
	for _, m := range c.order {
		id := m.A(odf.NSText, "change-id")
		ch := c.regions[id]
		n, ok := authors[ch.author]
		if !ok {
			n = len(authors)
			authors[ch.author] = n
		}
		kind := office.MarkInsert
		switch ch.kind {
		case changeDeletion:
			kind = office.MarkDelete
		case changeFormat:
			kind = office.MarkAttribute
		case changeInsertion:
		}
		if moved[id] {
			kind = office.MarkMoveTo
			if ch.kind == changeDeletion {
				kind = office.MarkMoveFrom
			}
		}
		out[id] = changeMark{color: office.RGB(office.MarkColor(kind, n)), kind: kind}
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

// markText sets in t how the marked view draws the text of the change m:
// its colour, line and text alike, and its line or weight
// ([office.MarkKind]).
func markText(t *textState, m changeMark) {
	t.color = &m.color
	switch m.kind {
	case office.MarkInsert:
		t.under = decoLine{on: true, style: css.DecorationSolid}
	case office.MarkDelete:
		t.through = decoLine{on: true, style: css.DecorationSolid}
	case office.MarkMoveTo:
		t.under = decoLine{on: true, style: css.DecorationDouble}
	case office.MarkMoveFrom:
		t.through = decoLine{on: true, style: css.DecorationDouble}
	case office.MarkAttribute:
		t.weight = 700
	}
}

// changeBound returns, at the text:change-start or text:change-end c of
// a change of the marked view, an empty inline node that gives the line
// it lands on a change bar, nil for the other views.
//
// # Corpus evidence
//
//   - probe, odt: LibreOffice 26.8 draws a change bar beside a line a
//     change only starts or ends on: a paragraph whose inserted break
//     follows its text, the paragraph after it.
func (b *builder) changeBound(c *odf.Element) *layout.Node {
	if _, ok := b.changeMarks[c.A(odf.NSText, "change-id")]; !ok {
		return nil
	}
	return b.node(&layout.Node{Kind: layout.Element, Style: baseStyle(), ChangeBar: true})
}

// rowFill returns the ground the marked view draws the table row row on:
// an inserted row's or a deleted one's, a row of a table an insertion or
// a deletion holds included, and whether it draws one. The ground covers
// the row's cells, over their own background ([office.RowInsertedFill]).
func (b *builder) rowFill(row *odf.Element) (css.Color, bool) {
	if b.changeMarks == nil {
		return css.Color{}, false
	}
	kind := office.MarkKind(0)
	if n := len(b.openChanges); n > 0 {
		kind = b.changeMarks[b.openChanges[n-1]].kind
	}
	if kind != office.MarkInsert && kind != office.MarkDelete && b.rowTracked(row) {
		for _, k := range []office.MarkKind{office.MarkInsert, office.MarkDelete} {
			if rowCovered(row, func(id string) bool { m, ok := b.changeMarks[id]; return ok && m.kind == k }) {
				kind = k
				break
			}
		}
	}
	switch kind {
	case office.MarkInsert:
		return office.RGB(office.RowInsertedFill), true
	case office.MarkDelete:
		return office.RGB(office.RowDeletedFill), true
	case office.MarkAttribute, office.MarkMoveTo, office.MarkMoveFrom:
	}
	return css.Color{}, false
}

// maxMoveText bounds the characters of a change's text the marked view
// compares to find moved text: 4,096. Anti-DoS guard, not spec-mandated:
// the texts are copied to be compared, and a moved sentence or paragraph
// is far shorter; a longer change is drawn as an insertion or a deletion.
const maxMoveText = 4096

// moves returns the ids of the deletions and insertions the marked view
// draws as moved text: a deletion of one paragraph's text and an
// insertion inside one paragraph whose texts pair ([office.MoveKey]).
func (c *changes) moves() map[string]bool {
	texts := map[string]*strings.Builder{}
	var open []string
	broken := map[string]bool{}
	var walk func(el *odf.Element, depth int)
	walk = func(el *odf.Element, depth int) {
		if depth > odf.MaxDepth || el.Is(odf.NSText, "tracked-changes") {
			return
		}
		if isPara(el) {
			// An insertion crossing a paragraph break pairs with nothing.
			for _, id := range open {
				broken[id] = true
			}
		}
		for _, k := range el.Children {
			switch {
			case k.IsText():
				for _, id := range open {
					if b := texts[id]; b.Len() <= maxMoveText {
						b.WriteString(k.Text)
					}
				}
			case k.Is(odf.NSText, "change-start"):
				id := k.A(odf.NSText, "change-id")
				if r := c.regions[id]; r != nil && r.kind == changeInsertion && texts[id] == nil {
					texts[id] = &strings.Builder{}
					open = append(open, id)
				}
			case k.Is(odf.NSText, "change-end"):
				id := k.A(odf.NSText, "change-id")
				open = slices.DeleteFunc(open, func(s string) bool { return s == id })
			case k.Is(odf.NSText, "s"):
				for _, id := range open {
					if b := texts[id]; b.Len() <= maxMoveText {
						b.WriteString(strings.Repeat(" ", spaces(k)))
					}
				}
			default:
				walk(k, depth+1)
			}
		}
		if isPara(el) {
			for _, id := range open {
				broken[id] = true
			}
		}
	}
	walk(c.body, 0)
	type pair struct{ del, ins []string }
	keys := map[string]*pair{}
	add := func(id, author, text string, del bool) {
		if len(text) > maxMoveText {
			return
		}
		k, ok := office.MoveKey(author, text)
		if !ok {
			return
		}
		p := keys[k]
		if p == nil {
			p = &pair{}
			keys[k] = p
		}
		if del {
			p.del = append(p.del, id)
		} else {
			p.ins = append(p.ins, id)
		}
	}
	for id, b := range texts {
		if !broken[id] {
			add(id, c.regions[id].author, b.String(), false)
		}
	}
	for id, r := range c.regions {
		if r.kind == changeDeletion && len(r.content) == 1 && isPara(r.content[0]) {
			add(id, r.author, flatText(r.content[0], 0), true)
		}
	}
	out := map[string]bool{}
	for _, p := range keys {
		if len(p.del) > 0 && len(p.ins) > 0 {
			for _, id := range slices.Concat(p.del, p.ins) {
				out[id] = true
			}
		}
	}
	return out
}

// flatText returns the characters of el, a text:s standing for its
// spaces, at most a few past [maxMoveText].
func flatText(el *odf.Element, depth int) string {
	var b strings.Builder
	var walk func(el *odf.Element, depth int)
	walk = func(el *odf.Element, depth int) {
		if depth > odf.MaxDepth {
			return
		}
		for _, k := range el.Children {
			if b.Len() > maxMoveText {
				return
			}
			switch {
			case k.IsText():
				b.WriteString(k.Text)
			case k.Is(odf.NSText, "s"):
				b.WriteString(strings.Repeat(" ", spaces(k)))
			default:
				walk(k, depth+1)
			}
		}
	}
	walk(el, depth)
	return b.String()
}

// spaces returns the spaces a text:s stands for (ODF 1.3, Part 3, 6.1.3):
// its text:c, 1 when absent, at most [maxMoveText].
func spaces(el *odf.Element) int {
	n, err := strconv.Atoi(el.A(odf.NSText, "c"))
	if err != nil || n < 1 {
		return 1
	}
	return min(n, maxMoveText)
}
