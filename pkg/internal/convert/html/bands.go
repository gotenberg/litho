package html

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strings"

	dom "golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// Band is a header or a footer: an HTML fragment drawn in the top or the
// bottom margin of the pages it selects, in the place of the page-margin
// boxes of that margin.
//
// The fragment is the content of an element of a document of its own,
// whose html and body elements take the language and direction of the
// source's: the band's document. Its box is a block as wide as the page
// area, in the page-margin box @top-center or @bottom-center made as wide
// as the page area (CSS Page 3, 5.3), centred vertically in the margin as
// that box's vertical-align puts its content. The band's document is
// styled by the source's style sheets, the user's among them, then by
// the <style> and <link rel=stylesheet> elements of the fragment, then by
// CSS; an isolated band by the user agent's style sheet, the fragment's
// and CSS alone. Neither reaches the source: the source's elements are
// not in the band's document.
//
// Its counter(page) and counter(pages) show the page's number and the
// page count, string() the page's named string as a page-margin box shows
// it (CSS GCPM 3, 1.1.2), its cross-references the source's elements (CSS
// Generated Content 3, 6), a page as the layout being painted places it,
// and its links keep their annotations. Its content is an artifact, and
// its headings and bookmark-level add no outline entry: they repeat on
// every page it shows on.
type Band struct {
	// Footer draws the band in the bottom margin, false in the top one.
	Footer bool
	// HTML is the fragment, parsed in the band's element. Empty draws
	// nothing: on the pages it selects, the margin shows what the
	// source's page-margin boxes put there.
	HTML string
	// CSS is a style sheet of the band's document, after the source's.
	CSS string
	// Pages selects the pages; nil selects every page.
	Pages PageSet
	// Room is how the band makes room for itself.
	Room Room
	// Isolated styles the band's document without the source's style
	// sheets: the source's @font-face fonts still draw its text, and its
	// resources still load.
	Isolated bool
}

// Room is how a band makes room for itself.
type Room int

const (
	// RoomMargin keeps the page margin: a band taller than the margin
	// overflows it, with a warning. It is the zero value.
	RoomMargin Room = iota
	// RoomContent grows the margin of each page to the band's height
	// when the band is taller: the document is laid out once with the
	// margins it names, each page's band measured, and once more with
	// the grown margins, which it keeps.
	RoomContent
)

// PageSet selects pages: a page any of its terms selects. nil selects
// every page.
type PageSet []PageTerm

// PageKind is what a [PageTerm] selects.
type PageKind int

// The pages a [PageTerm] selects.
const (
	// PageFirst is the first page (CSS Page 3, 4.2.1, :first).
	PageFirst PageKind = iota + 1
	// PageLast is the last page, which no page selector names.
	PageLast
	// PageLeft and PageRight are the left and the right pages (CSS Page
	// 3, 4.2.2, :left and :right).
	PageLeft
	PageRight
	// PageBlank is a page a forced break left empty (CSS Page 3, 4.2.3,
	// :blank).
	PageBlank
	// PageRange is the pages From to To, counted from 1 in the document,
	// whatever their number shows; To 0 runs to the last page.
	PageRange
	// PageNamed is the pages of the page type Name (CSS Page 3, 4.1).
	PageNamed
)

// PageTerm is one term of a [PageSet].
type PageTerm struct {
	Kind     PageKind
	From, To int
	Name     string
}

// MaxBands caps [Options.Bands]: 64. Anti-DoS guard, not spec-mandated:
// each band is styled and built once and each page picks its band among
// them; a document asks for a header and a footer, a few more for its
// first, last, left and right pages.
const MaxBands = 64

// maxBandBytes caps the HTML and the CSS of one band together: 1 MiB.
// Anti-DoS guard, not spec-mandated: a band is laid out on every page it
// selects, and a header holding a logo as a data: URL takes a few hundred
// kilobytes.
const maxBandBytes = 1 << 20

// The range of [Options.PageCounterStart]: a first page numbered 0 to
// 2^30, the largest counter value litho keeps.
const (
	MinPageCounterStart = -1
	MaxPageCounterStart = maxListValue - 1
)

// checkBands returns an error wrapping [typeset.ErrInvalidOptions] for
// bands out of range.
func checkBands(bands []Band) error {
	if len(bands) > MaxBands {
		return fmt.Errorf("%w: %d headers and footers, more than %d", typeset.ErrInvalidOptions, len(bands), MaxBands)
	}
	for i, b := range bands {
		name := fmt.Sprintf("%s %d", b.edgeName(), i+1)
		if len(b.HTML)+len(b.CSS) > maxBandBytes {
			return fmt.Errorf("%w: %s holds more than %d bytes of HTML and CSS", typeset.ErrInvalidOptions, name, maxBandBytes)
		}
		if b.Room != RoomMargin && b.Room != RoomContent {
			return fmt.Errorf("%w: %s has an unknown room %d", typeset.ErrInvalidOptions, name, b.Room)
		}
		for _, t := range b.Pages {
			switch t.Kind {
			case PageFirst, PageLast, PageLeft, PageRight, PageBlank:
			case PageRange:
				if t.From < 1 || t.To != 0 && t.To < t.From {
					return fmt.Errorf("%w: %s selects pages %d to %d, not a range of pages counted from 1", typeset.ErrInvalidOptions, name, t.From, t.To)
				}
			case PageNamed:
				if t.Name == "" {
					return fmt.Errorf("%w: %s selects the pages of an empty page name", typeset.ErrInvalidOptions, name)
				}
			default:
				return fmt.Errorf("%w: %s has an unknown page set term %d", typeset.ErrInvalidOptions, name, t.Kind)
			}
		}
	}
	return nil
}

// edgeName names the margin of b for a message.
func (b Band) edgeName() string {
	if b.Footer {
		return "footer"
	}
	return "header"
}

// selects reports whether s selects the page info names, a right page
// when right, of a document whose page count is pages.
func (s PageSet) selects(info layout.PageInfo, right bool, pages int) bool {
	if len(s) == 0 {
		return true
	}
	for _, t := range s {
		var ok bool
		switch t.Kind {
		case PageFirst:
			ok = info.Index == 0
		case PageLast:
			ok = info.Index == pages-1
		case PageLeft:
			ok = !right
		case PageRight:
			ok = right
		case PageBlank:
			ok = info.Blank
		case PageRange:
			n := info.Index + 1
			ok = n >= t.From && (t.To == 0 || n <= t.To)
		case PageNamed:
			// A page type name is a custom identifier, matched as written
			// (CSS Page 3, 4.1; CSS Values 4, 4.2).
			ok = info.Name == t.Name
		}
		if ok {
			return true
		}
	}
	return false
}

// bandElement is the element of a band's document that holds its HTML,
// and bandID its id, which the content tree finds it by.
const (
	bandElement = "litho-band"
	bandID      = "litho-band"
)

// bandBaseCSS lays the band's element out as a block, at the user agent's
// origin, under which the source's and the band's rules win.
const bandBaseCSS = bandElement + " { display: block }"

// bandBoxCSS makes the page-margin boxes a band takes as wide as the page
// area (CSS Page 3, 5.3.2.2: a definite width; the boxes beside take what
// is left, none).
const bandBoxCSS = `@page { @top-center { content: ""; width: 100% } @bottom-center { content: ""; width: 100% } }`

// titleString is the named string whose value, before any element of the
// source sets it, is the source's title: what a band or a page-margin box
// shows of the title, as string(litho-title).
const titleString = "litho-title"

// bandState is a band built for drawing.
type bandState struct {
	band Band
	// node is the band element's box, nil for an empty band; gcpm holds
	// the page counter and string() items of its generated content.
	node *layout.Node
	gcpm *gcpmState
	// use is what the band's cross-references read at their targets, and
	// refs are those cross-references: their targets are the source's
	// elements (CSS Generated Content 3, 6).
	use  css.TargetUse
	refs []targetRef
	// need is the tallest the band's box was measured on a page of the
	// first layout, in points of the page: the margin RoomContent gives
	// it.
	need float64
}

// bandEnv is what building the bands of a document reads of it.
type bandEnv struct {
	o      Options
	scan   *scan
	sheets []css.Sheet
	media  css.Media
	res    *resources
	c      *collector
	opts   css.ComputeOptions
}

// buildBands builds the bands of env.o and the styles of the page-margin
// boxes they take, inheriting from root, the source root element's style.
func buildBands(ctx context.Context, env bandEnv, root *css.Style) ([]*bandState, [2]*css.Style, []typeset.Warning, error) {
	var boxes [2]*css.Style
	if len(env.o.Bands) == 0 {
		return nil, boxes, nil, nil
	}
	sheet, err := css.Parse(ctx, []byte(bandBoxCSS), css.ParseOptions{})
	if err != nil {
		return nil, boxes, nil, fmt.Errorf("parsing the band boxes: %w", err)
	}
	ps, err := css.NewResolver(env.media, []css.Sheet{{Sheet: sheet, Origin: css.UserAgent}}).PageIn(ctx, css.PageSelector{First: true, Right: true, Index: 1}, root)
	if err != nil {
		return nil, boxes, nil, err
	}
	boxes[0], boxes[1] = ps.MarginBoxes[css.MarginTopCenter], ps.MarginBoxes[css.MarginBottomCenter]
	if boxes[0] == nil || boxes[1] == nil {
		// The sheet is litho's own constant: a box missing is a litho
		// bug, not an invalid input.
		return nil, boxes, nil, fmt.Errorf("the band boxes did not cascade")
	}
	base, err := css.Parse(ctx, []byte(bandBaseCSS), css.ParseOptions{})
	if err != nil {
		return nil, boxes, nil, fmt.Errorf("parsing the band element's style: %w", err)
	}
	var warns []typeset.Warning
	out := make([]*bandState, 0, len(env.o.Bands))
	for i, b := range env.o.Bands {
		if err := ctx.Err(); err != nil {
			return nil, boxes, nil, err
		}
		s := &bandState{band: b}
		out = append(out, s)
		if b.HTML == "" {
			continue
		}
		ws, err := s.build(ctx, env, base, i+1)
		if err != nil {
			return nil, boxes, nil, err
		}
		warns = append(warns, ws...)
	}
	return out, boxes, warns, nil
}

// build styles and builds the band's document, the n-th band.
func (s *bandState) build(ctx context.Context, env bandEnv, base *css.Stylesheet, n int) ([]typeset.Warning, error) {
	b := s.band
	doc, err := bandDocument(b, env.scan.root)
	if err != nil {
		return nil, err
	}
	sc, err := scanDocument(ctx, doc, documentBase(env.o.BaseURL))
	if err != nil {
		return nil, err
	}
	// The band resolves its URLs and links as the source does.
	sc.resourceBase, sc.linkBase, sc.docURL = env.scan.resourceBase, env.scan.linkBase, env.scan.docURL
	if sc.lang == "" {
		sc.lang = env.scan.lang
	}
	sheets := []css.Sheet{{Sheet: css.HTMLUserAgent(), Origin: css.UserAgent}}
	if !b.Isolated {
		sheets = slices.Clone(env.sheets)
	}
	sheets = append(sheets, css.Sheet{Sheet: base, Origin: css.UserAgent})
	before := len(env.c.warnings)
	sheets = append(sheets, env.c.stylesheets(ctx, nil, sc.sheets)[1:]...)
	if b.CSS != "" {
		if sh := env.c.bandSheet(ctx, n, b.CSS); sh != nil {
			sheets = append(sheets, css.Sheet{Sheet: sh, Origin: css.Author})
		}
	}
	// The collector's warnings of the band's sheets are reported with the
	// band's; the source's stay where they are.
	warns := slices.Clone(env.c.warnings[before:])
	env.c.warnings = env.c.warnings[:before]
	r := css.NewResolver(env.media, sheets)
	opts := env.opts
	opts.Lang = sc.lang
	computed, err := r.Compute(ctx, doc, opts)
	if err != nil {
		return nil, fmt.Errorf("computing the styles of the %s: %w", b.edgeName(), err)
	}
	bb := &builder{
		computed: computed, res: env.res, scan: sc, media: env.media,
		warned: map[string]bool{}, omitBackgrounds: env.o.OmitBackgrounds,
		counterStyles: r.CounterStyles(),
	}
	root, err := bb.buildRoot(ctx, sc.root)
	if err != nil {
		return nil, err
	}
	layout.ShareStyles(ctx, root, nil)
	s.node = findBand(root)
	s.gcpm = &bb.gcpm
	s.use, s.refs = r.TargetUse(), bb.targets.refs
	for _, w := range computed.Warnings() {
		warns = append(warns, typeset.Warning{Code: w.Code, Effect: w.Effect, Message: displayURL(w.Message)})
	}
	return append(warns, bb.warnings...), nil
}

// bandDocument returns the band's document of b: html and body elements
// that take the attributes of the source's, root, or for an isolated band
// their lang and dir alone, and the band's element holding b.HTML parsed
// as a fragment in it (HTML Standard, 13.4), so that nothing of it closes
// the element.
func bandDocument(b Band, root *dom.Node) (*dom.Node, error) {
	el := &dom.Node{Type: dom.ElementNode, Data: bandElement, Attr: []dom.Attribute{{Key: "id", Val: bandID}}}
	kids, err := dom.ParseFragment(strings.NewReader(b.HTML), el)
	if err != nil {
		// A strings.Reader never fails, so the parser fails only on its
		// own nesting cap.
		return nil, fmt.Errorf("%w: %w", ErrTooDeep, err)
	}
	for _, k := range kids {
		el.AppendChild(k)
	}
	html := &dom.Node{Type: dom.ElementNode, Data: "html", DataAtom: atom.Html}
	body := &dom.Node{Type: dom.ElementNode, Data: "body", DataAtom: atom.Body}
	if root != nil {
		html.Attr = bandAttrs(root, b.Isolated)
		if sb := childElement(root, atom.Body); sb != nil {
			body.Attr = bandAttrs(sb, b.Isolated)
		}
	}
	doc := &dom.Node{Type: dom.DocumentNode}
	doc.AppendChild(&dom.Node{Type: dom.DoctypeNode, Data: "html"})
	doc.AppendChild(html)
	html.AppendChild(&dom.Node{Type: dom.ElementNode, Data: "head", DataAtom: atom.Head})
	html.AppendChild(body)
	body.AppendChild(el)
	return doc, nil
}

// bandAttrs returns the attributes of the source's element n the band's
// document copies: every one, or for an isolated band lang and dir, the
// language and direction of the source rather than its style.
func bandAttrs(n *dom.Node, isolated bool) []dom.Attribute {
	var out []dom.Attribute
	for _, a := range n.Attr {
		if a.Namespace != "" || isolated && a.Key != "lang" && a.Key != "dir" {
			continue
		}
		out = append(out, a)
	}
	return out
}

// childElement returns the first child element of n of type a, nil for
// none.
func childElement(n *dom.Node, a atom.Atom) *dom.Node {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == dom.ElementNode && c.DataAtom == a && c.Namespace == "" {
			return c
		}
	}
	return nil
}

// findBand returns the box of the band's element in the content tree of
// its document, its anchor dropped, nil when it generates no box.
func findBand(root *layout.Node) *layout.Node {
	stack := []*layout.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n.ID == bandID {
			c := *n
			c.ID = ""
			return &c
		}
		stack = append(stack, n.Children...)
	}
	return nil
}

// bandsOn returns the bands drawn on the page info names, one of pages,
// top then bottom: the last band of each margin that selects the page,
// nil when none does or it is empty.
func (p *prepared) bandsOn(info layout.PageInfo, pages int) [2]*bandState {
	var out [2]*bandState
	if len(p.bands) == 0 {
		return out
	}
	right := p.pages.key(info).right
	var picked [2]bool
	for _, s := range slices.Backward(p.bands) {
		edge := 0
		if s.band.Footer {
			edge = 1
		}
		if picked[edge] || !s.band.Pages.selects(info, right, pages) {
			continue
		}
		picked[edge] = true
		if s.node != nil {
			out[edge] = s
		}
	}
	return out
}

// bandBox returns the page-margin box that shows the band s on the page
// of index, one of pages, whose box is pb: the content is the band's
// box, its page counters and named strings at the page's values.
func (p *prepared) bandBox(s *bandState, edge, index, pages int, pb layout.PageBox) *marginBox {
	which := css.MarginTopCenter
	if edge == 1 {
		which = css.MarginBottomCenter
	}
	st := p.bandBoxes[edge]
	mb := &marginBox{which: which, st: st, cb: containingBlock(which, pb), band: s}
	p.bandPages(s)
	n := s.node
	if len(s.gcpm.paged) > 0 {
		n = s.gcpm.atPage(n, p.pageText(st, index, pages, pb.Bleed+pb.Margin[css.Top]), 0)
	}
	mb.kids = []*layout.Node{n}
	return mb
}

// bandTargetUse returns use with what the cross-references of bands read
// at their targets, so that the source's builder records it.
func bandTargetUse(use css.TargetUse, bands []*bandState) css.TargetUse {
	for _, s := range bands {
		if !s.use.Any {
			continue
		}
		use.Any = true
		for _, name := range s.use.Counters {
			if !slices.Contains(use.Counters, name) {
				use.Counters = append(use.Counters, name)
			}
		}
	}
	slices.Sort(use.Counters)
	return use
}

// resolveBandTargets writes the value of every cross-reference of the bands
// that reads no page, at its target in the source built by b, and leaves
// out, with a warning, one whose target is not in the source; a
// cross-reference that reads a page is written as each page is painted
// ([prepared.bandPages]).
func resolveBandTargets(b *builder, bands []*bandState) {
	for _, s := range bands {
		for _, r := range s.refs {
			t := b.targets.byID[r.id]
			switch {
			case t == nil:
				b.warnOnce(warn.LinkUnresolved, warn.Omitted, "target:"+r.id, fmt.Sprintf(
					"a cross-reference names %q, which no element of the document carries: it generates nothing", "#"+r.id))
				r.node.Text = ""
			case !readsPage(r.item):
				r.node.Text = strings.ToValidUTF8(b.targetText(t, r.item), "�")
			}
		}
	}
}

// bandPages writes into the cross-references of the band s that read a
// page the values of the layout being painted: the band's box is drawn
// from one tree on every page, and the values are the layout's, the same
// on each of its pages.
func (p *prepared) bandPages(s *bandState) {
	if p.b == nil {
		return
	}
	for _, r := range s.refs {
		if readsPage(r.item) && p.b.targets.byID[r.id] != nil {
			r.node.Text, _ = p.pagedText(r.item, r.id, p.b.targets.anchors)
		}
	}
}

// pageText returns what a counter(), counters() or string() item of
// content shown on the page of index, one of pages, whose page area's top
// is top, writes there: the counters as [prepared.counterText] gives them
// in the box styled box, a named string as the page shows it (CSS GCPM 3,
// 1.1.2).
func (p *prepared) pageText(box *css.Style, index, pages int, top float64) func(css.ContentItem) string {
	counters := p.counterText(box, index, pages)
	return func(it css.ContentItem) string {
		if it.Kind != css.ItemNamedString {
			return counters(it)
		}
		if a, ok := p.namedString(it.Text, it.Pick, index, top); ok {
			return a.value
		}
		return ""
	}
}

// namedString returns the value of the named string name on the page of
// index, whose page area's top is top, under pick (see [gcpmState.pick]):
// the entry value of [titleString] before any element sets it is the
// source's title.
func (p *prepared) namedString(name string, pick css.PagePick, index int, top float64) (assignment, bool) {
	var initial *assignment
	if name == titleString && p.meta.Title != "" {
		initial = &assignment{name: name, value: p.meta.Title}
	}
	if p.b == nil {
		if initial != nil && pick != css.PickFirstExcept {
			return *initial, true
		}
		return assignment{}, false
	}
	g := &p.b.gcpm
	return g.pick(g.strings, name, pick, index, top, initial)
}

// warnBand records a band whose content is taller than its box: in the
// margin the band keeps, or past the room its first layout measured.
func (p *prepared) warnBand(mb *marginBox) {
	round := func(v float64) float64 { return math.Round(v*100) / 100 }
	f := p.setup.factor
	p.warnOverflowOnce(fmt.Sprintf("the %s is %v points tall and its margin leaves it %v points: it overflows the margin",
		mb.band.band.edgeName(), round(mb.contentH/f), round(mb.y.size/f)))
}

// needRoom records the room the band of mb took on a page of the first
// layout, in points of the page.
func (p *prepared) needRoom(mb *marginBox) {
	if p.room.pages > 0 || mb.band.band.Room != RoomContent {
		return
	}
	mb.band.need = max(mb.band.need, (mb.contentH+mb.y.m1+mb.y.m2+mb.y.bp)/p.setup.factor)
}

// roomState is the second layout [RoomContent] asks for: pages is the
// page count of the first layout, which tells the last page, 0 before the
// second layout.
type roomState struct {
	pages int
}

// roomed reports a page of the first layout, whose page count is pages,
// whose margin a band of [RoomContent] grows.
func (p *prepared) roomed(pages int) bool {
	for _, info := range p.infos {
		if p.grown(info, pages).Margin != p.pages.box(info).Margin {
			return true
		}
	}
	return false
}

// grown returns the page box of the page info names, in points of the
// page, its top and bottom margins grown to the room of the bands of
// [RoomContent] that would show on it in a document whose page count is
// pages.
func (p *prepared) grown(info layout.PageInfo, pages int) layout.PageBox {
	pb := p.pages.box(info)
	for edge, s := range p.bandsOn(info, pages) {
		if s == nil || s.band.Room != RoomContent {
			continue
		}
		side := css.Top
		if edge == 1 {
			side = css.Bottom
		}
		pb.Margin[side] = max(pb.Margin[side], s.need)
	}
	return pb
}

// pageBox returns the page box of the page info names, in points of the
// page: the one its @page rules give it, its margins grown in the second
// layout of [RoomContent].
func (p *prepared) pageBox(info layout.PageInfo) layout.PageBox {
	if p.room.pages == 0 {
		return p.pages.box(info)
	}
	return p.grown(info, p.room.pages)
}

// growRoom readies the second layout of [RoomContent] after a first one
// whose page count is pages: every page box goes through
// [prepared.pageBox], and the state the first layout's pages left is
// cleared. The second layout keeps the grown margins even where a page
// then shows another band: the layouts are two, never more.
func (p *prepared) growRoom(pages int) {
	p.room.pages = pages
	p.layout.PageFor = func(info layout.PageInfo) layout.PageBox { return p.setup.layoutBox(p.pageBox(info)) }
	p.resetPages()
}

// warnOverflowOnce records the warning of content overflowing its box
// the first time it is met. The second layout of [RoomContent] drops the
// first's.
func (p *prepared) warnOverflowOnce(msg string) {
	s := p.pages
	if s.seen[msg] {
		return
	}
	s.seen[msg] = true
	s.overflow = append(s.overflow, len(s.warnings))
	s.warnings = append(s.warnings, typeset.Warning{Code: warn.PageSetupUnsupported, Effect: warn.Approximated, Message: msg})
}

// dropOverflow removes the warnings of [prepared.warnOverflowOnce].
func (s *pageSetup) dropOverflow() {
	if len(s.overflow) == 0 {
		return
	}
	keep := s.warnings[:0]
	at := 0
	for i, w := range s.warnings {
		if at < len(s.overflow) && s.overflow[at] == i {
			at++
			delete(s.seen, w.Message)
			continue
		}
		keep = append(keep, w)
	}
	s.warnings, s.overflow = keep, nil
}

// newWarnings returns the warnings of add that have holds none like, in
// order, each once: a band that is not isolated cascades the source's
// style sheets again, and their warnings are the source's.
func newWarnings(have, add []typeset.Warning) []typeset.Warning {
	type key struct {
		code warn.Code
		msg  string
	}
	seen := make(map[key]bool, len(have))
	for _, w := range have {
		seen[key{w.Code, w.Message}] = true
	}
	var out []typeset.Warning
	for _, w := range add {
		if k := (key{w.Code, w.Message}); !seen[k] {
			seen[k] = true
			out = append(out, w)
		}
	}
	return out
}
