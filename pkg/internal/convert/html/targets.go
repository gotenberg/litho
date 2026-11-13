package html

import (
	"context"
	"fmt"
	"slices"
	"strings"

	dom "golang.org/x/net/html"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// maxTargetRefs bounds the cross-references a document generates: 65,536.
// Anti-DoS guard, not spec-mandated: a table of contents holds an entry per
// heading, a few hundred, and each layout of the document rewrites every
// one that reads a page.
const maxTargetRefs = 1 << 16

// maxTargetLayouts bounds the layouts of a document whose cross-references
// read the page counter: 4. Anti-DoS guard, not spec-mandated: the second
// layout draws the page numbers the first found, and settles unless their
// widths move a target to another page; each layout costs as much as the
// first.
const maxTargetLayouts = 4

// targetInfo is what a cross-reference reads at the element an id names
// (CSS Generated Content 3, 6): the values of the counters it reads there,
// the element's text and the boxes of its ::before and ::after, and the
// anchor that says on which page the element lands.
type targetInfo struct {
	n             *dom.Node
	anchor        string
	counters      [][]int
	before, after *layout.Node
}

// targetRef is one cross-reference of generated content: the Text node
// that shows its value, the item, and the id of the element it reads, ""
// for a URL that names none of the document.
type targetRef struct {
	node *layout.Node
	item css.ContentItem
	id   string
}

// targetState holds the cross-references of a document and the elements
// they read.
type targetState struct {
	// use is what the stylesheets' cross-references read; nothing is
	// recorded without one.
	use  css.TargetUse
	byID map[string]*targetInfo
	refs []targetRef
	// infos are the pages and anchors of the layout being painted, for
	// the cross-references of the page-margin boxes; pageNums are the
	// values of the page counter on those pages, computed when first read.
	infos    []layout.PageInfo
	anchors  map[string]typeset.Dest
	pageNums []int
}

// readsPage reports a cross-reference whose value the layout decides: the
// page or pages counter at its target.
func readsPage(it css.ContentItem) bool {
	return it.Kind != css.ItemTargetText && (it.Text == pageCounter || it.Text == pagesCounter)
}

// markTarget records what a cross-reference reads at the element n, built
// as node, whose ::before and ::after boxes are before and after: the
// counters values were taken before its children ran theirs. The first
// element of an id is the one a URL names (HTML Standard, 7.4.6.4).
func (b *builder) markTarget(n *dom.Node, node, before, after *layout.Node, values [][]int) {
	t := &b.targets
	id, ok := attr(n, "id")
	if !ok || id == "" || t.byID[id] != nil || len(t.byID) >= MaxElements {
		return
	}
	if t.byID == nil {
		t.byID = map[string]*targetInfo{}
	}
	t.byID[id] = &targetInfo{n: n, anchor: node.ID, counters: values, before: before, after: after}
}

// targetValues returns the values of the counters the cross-references
// read, in the order of [css.TargetUse.Counters].
func (b *builder) targetValues() [][]int {
	vals := make([][]int, len(b.targets.use.Counters))
	for i, name := range b.targets.use.Counters {
		vals[i] = b.counters.values(name)
	}
	return vals
}

// movedTarget records that the element n, a target, left the flow as a
// running element: its place in the flow is anchored at id.
func (b *builder) movedTarget(n *dom.Node, id string) {
	if v, ok := attr(n, "id"); ok {
		if t := b.targets.byID[v]; t != nil && t.n == n {
			t.anchor = id
		}
	}
}

// targetNode returns the Text node, styled ps, that shows the cross-reference
// it of generated content of the element n: its value once the document is
// built ([builder.resolveTargets]), or once it is laid out for one that reads
// a page. nil past [maxTargetRefs].
func (b *builder) targetNode(n *dom.Node, ps *css.Style, it css.ContentItem) *layout.Node {
	if len(b.targets.refs) >= maxTargetRefs {
		b.warnOnce(warn.LimitReached, warn.Omitted, "targets", fmt.Sprintf("more than %d cross-references; the others were left out", maxTargetRefs))
		return nil
	}
	u := it.Target
	if it.TargetAttr {
		u, _ = attr(n, it.Target)
	}
	id := ""
	if frag, ok := strings.CutPrefix(b.linkTarget(u), "#"); ok {
		id = frag
	}
	node := &layout.Node{Kind: layout.Text, Style: ps}
	if readsPage(it) {
		// The first layout reads the page counter at 0: the value is not
		// known before the target is laid out.
		node.Text = counterText(b.counterStyles, it.Style, it.StyleName, 0)
	}
	b.targets.refs = append(b.targets.refs, targetRef{node: node, item: it, id: id})
	return node
}

// resolveTargets writes the value of every cross-reference that does not
// read a page: the counters and the text at its target, as the document's
// elements are built (CSS Generated Content 3, 6). A URL that names no
// element of the document, or one that generates no box, is an error the
// draft leaves open (Issue 2): the cross-reference generates nothing.
func (b *builder) resolveTargets() {
	for _, r := range b.targets.refs {
		t := b.targets.byID[r.id]
		if t == nil {
			b.warnOnce(warn.LinkUnresolved, warn.Omitted, "target:"+r.id, fmt.Sprintf(
				"a cross-reference names %q, which no element of the document carries: it generates nothing", "#"+r.id))
			r.node.Text = ""
			continue
		}
		if readsPage(r.item) {
			continue
		}
		r.node.Text = strings.ToValidUTF8(b.targetText(t, r.item), "�")
	}
}

// targetText returns the value of the cross-reference it, which reads no
// page, at its target t.
func (b *builder) targetText(t *targetInfo, it css.ContentItem) string {
	if it.Kind == css.ItemTargetText {
		// The parts are those of content() (CSS GCPM 3, 1.1.1.1): white
		// space collapsed.
		switch it.Text {
		case "before":
			return collapse(nodeText(t.before))
		case "after":
			return collapse(nodeText(t.after))
		case "first-letter":
			return firstLetter(collapse(domText(t.n)))
		}
		return collapse(domText(t.n))
	}
	var vals []int
	if i := slices.Index(b.targets.use.Counters, it.Text); i >= 0 && i < len(t.counters) {
		vals = t.counters[i]
	}
	return counterValues(b.counterStyles, it, vals)
}

// counterValues spells vals as the counter() or counters() form of it
// does: its innermost value, or every value joined by its separator, 0
// for a counter not in scope (CSS Lists 3, 4.5).
func counterValues(cs *css.CounterStyles, it css.ContentItem, vals []int) string {
	if len(vals) == 0 {
		vals = []int{0}
	}
	if it.Kind == css.ItemCounter || it.Kind == css.ItemTargetCounter {
		return counterText(cs, it.Style, it.StyleName, vals[len(vals)-1])
	}
	var sb strings.Builder
	for i, v := range vals {
		if i > 0 {
			sb.WriteString(it.Separator)
		}
		sb.WriteString(counterText(cs, it.Style, it.StyleName, v))
	}
	return sb.String()
}

// pagedTargets reports a document with a cross-reference that reads a
// page, which the layout decides.
func (t *targetState) pagedTargets() bool {
	for _, r := range t.refs {
		if readsPage(r.item) && t.byID[r.id] != nil {
			return true
		}
	}
	return false
}

// pageValue returns the value of the page counter on the page of index of
// the layout t.infos holds: as a page-margin box reads it (CSS Page 3,
// 6.1, see [prepared.pageCounters]).
func (p *prepared) pageValue(index int) int {
	t := &p.b.targets
	if t.pageNums == nil {
		t.pageNums = make([]int, len(t.infos))
		cur := map[string]int{pageCounter: p.o.PageCounterStart}
		for k, info := range t.infos {
			ps := p.pages.entry(info).style
			st := css.Style{CounterReset: ps.CounterReset, CounterIncrement: ps.CounterIncrement, CounterSet: ps.CounterSet}
			runCounterOps(cur, func(name string) int { return cur[name] }, &st, true)
			t.pageNums[k] = cur[pageCounter]
		}
	}
	if index < 0 || index >= len(t.pageNums) {
		return index + 1
	}
	return t.pageNums[index]
}

// pagedText returns the value of the cross-reference it, which reads the
// page or pages counter, at the element id names, as the layout whose
// anchors and pages t holds placed it; false when the element landed on no
// page.
func (p *prepared) pagedText(it css.ContentItem, id string, anchors map[string]typeset.Dest) (string, bool) {
	t := &p.b.targets
	info := t.byID[id]
	if info == nil {
		return "", false
	}
	d, ok := anchors[info.anchor]
	if !ok || info.anchor == "" {
		return "", false
	}
	v := len(t.infos)
	if it.Text == pageCounter {
		v = p.pageValue(d.Page - 1)
	}
	return counterValues(p.b.counterStyles, it, []int{v}), true
}

// settleTargets writes into the cross-references that read a page the
// values the layout res gives, and reports whether any changed: the
// document must then be laid out again for them to show.
func (p *prepared) settleTargets(res *layout.Result) bool {
	t := &p.b.targets
	changed := false
	for _, r := range t.refs {
		if !readsPage(r.item) || t.byID[r.id] == nil {
			continue
		}
		v, ok := p.pagedText(r.item, r.id, res.Anchors)
		if !ok {
			p.lateWarning(warn.LinkUnresolved, warn.Omitted, "target-page:"+r.id, fmt.Sprintf(
				"a cross-reference reads the page of %q, which lands on no page: it generates nothing", "#"+r.id))
		}
		if r.node.Text != v {
			r.node.Text, changed = v, true
		}
	}
	return changed
}

// marginTarget returns the value of a cross-reference in a page-margin box:
// a page-margin box has no element, so only a URL written in the style
// sheet names a target, and the layout being painted places it.
func (p *prepared) marginTarget(it css.ContentItem) string {
	if p.b == nil || it.TargetAttr {
		return ""
	}
	frag, ok := strings.CutPrefix(p.b.linkTarget(it.Target), "#")
	if !ok {
		return ""
	}
	t := p.b.targets.byID[frag]
	if t == nil {
		return ""
	}
	if readsPage(it) {
		v, _ := p.pagedText(it, frag, p.b.targets.anchors)
		return v
	}
	return p.b.targetText(t, it)
}

// layOutTargets lays the prepared document p out with lay, again while its
// cross-references that read a page change: a layout draws the values the
// one before it found, and a cross-reference's width may move the pages.
// It stops at [maxTargetLayouts] layouts with a warning: the numbers of the
// last one may then be those of the layout before it.
func layOutTargets(ctx context.Context, lay layoutFunc, p *prepared, res *layout.Result) (*layout.Result, error) {
	if p.b == nil || !p.b.targets.pagedTargets() {
		return res, nil
	}
	for k := 1; p.settleTargets(res); k++ {
		if k == maxTargetLayouts {
			p.lateWarning(warn.LimitReached, warn.Approximated, "target-layouts", fmt.Sprintf(
				"the page numbers of the cross-references changed in each of %d layouts: some may name the page of an earlier layout", maxTargetLayouts))
			break
		}
		p.resetPages()
		var err error
		if res, err = lay(ctx, p.root, p.fonts, p.layout); err != nil {
			return nil, err
		}
	}
	return res, nil
}

// resetPages forgets what painting a layout's pages recorded, before the
// document is laid out again.
func (p *prepared) resetPages() {
	p.infos = nil
	p.pages.counters, p.pages.links = nil, nil
	p.pages.dropOverflow()
	if p.b == nil {
		return
	}
	p.b.gcpm.placed, p.b.gcpm.markAt = nil, 0
	t := &p.b.targets
	t.infos, t.anchors, t.pageNums = nil, nil, nil
}

// lateWarning records a warning of the builder's that only a layout finds,
// once per key.
func (p *prepared) lateWarning(code warn.Code, effect warn.Effect, key, msg string) {
	k := code.String() + "\x00" + effect.String() + "\x00" + key
	if p.b.warned[k] {
		return
	}
	p.b.warned[k] = true
	p.warnings = append(p.warnings, typeset.Warning{Code: code, Effect: effect, Message: msg})
}
