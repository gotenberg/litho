package odt

import (
	"strconv"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/odf"
)

// The page Writer lays a document out on when it names no page layout:
// A4 with 2 cm margins.
const (
	defaultPageWidth  = 595.276
	defaultPageHeight = 841.890
	defaultPageMargin = 2 * 72 / 2.54
)

// instance is one use of a master page: the pages from the paragraph
// that names it, which may restart the page numbers, to the next such
// paragraph (ODF 1.3, Part 3, 20.307, style:master-page-name, and
// 20.321, style:page-number). Its pages are a named page of the layout.
type instance struct {
	name    string
	master  *odf.MasterPage
	restart int
	first   int
	seen    bool
}

// pageEntry is what the layout learned of one page: the instance it
// belongs to, the master page drawn on it, its number and its box.
type pageEntry struct {
	inst   *instance
	master *odf.MasterPage
	number int
	box    layout.PageBox
	geom   *pageGeom
	left   bool
	first  bool
}

// pageFlow is the master pages of the body and the pages laid out on
// them.
type pageFlow struct {
	b         *builder
	opts      office.Options
	instances []*instance
	byName    map[string]*instance
	entries   []pageEntry
	geoms     map[string]*pageGeom
	// blanks marks the pages before which Writer inserts a blank page.
	blanks map[int]bool
	// building counts the geometries being built: a frame of a header
	// that asks for its page's geometry gets one without the header and
	// footer, which are what is being measured.
	building int
}

// hfGeom is a header's or footer's placement on its page.
type hfGeom struct {
	el *odf.Element
	// x and w are its box's left edge and width; h its height, at least
	// its minimum; space the room between it and the body.
	x, w, h, space float64
}

// pageGeom is the geometry of a master page in one variant (left or
// right, first or not): the page box and its header and footer.
type pageGeom struct {
	box            layout.PageBox
	header, footer *hfGeom
	format         string
	mirrored       bool
	// top and bottom are the page area's edges less the header and
	// footer: where the header starts and the footer ends.
	top, bottom float64
}

// newPageFlow returns the page flow of the document's master pages.
func newPageFlow(b *builder, opts office.Options) *pageFlow {
	return &pageFlow{b: b, opts: opts, byName: map[string]*instance{}, geoms: map[string]*pageGeom{}, blanks: map[int]bool{}}
}

// reset forgets the pages a layout laid out, for another layout of the
// same document.
func (f *pageFlow) reset() {
	f.entries = nil
	f.blanks = map[int]bool{}
	for _, inst := range f.instances {
		inst.first, inst.seen = 0, false
	}
}

// defaultMaster returns the master page the document starts on when its
// first paragraph names none: Writer's "Standard", else the first.
func (f *pageFlow) defaultMaster() *odf.MasterPage {
	if mp := f.b.styles.Master("Standard"); mp != nil {
		return mp
	}
	if len(f.b.styles.Masters) > 0 {
		return f.b.styles.Masters[0]
	}
	return nil
}

// start opens a new instance of the master page named master, restarting
// the numbers at restart when it is positive.
func (f *pageFlow) start(master string, restart int) *instance {
	mp := f.b.styles.Master(master)
	if mp == nil {
		mp = f.defaultMaster()
	}
	inst := &instance{name: "odt-page-" + strconv.Itoa(len(f.instances)+1), master: mp, restart: restart}
	f.instances = append(f.instances, inst)
	f.byName[inst.name] = inst
	return inst
}

// maxNextChain bounds the style:next-style-name steps followed for one
// page: 64. Anti-DoS guard, not spec-mandated: a chain of master pages
// is a few deep, and a cycle repeats.
const maxNextChain = 64

// masterAt returns the master page of the page offset pages after the
// first page of an instance whose master is mp: style:next-style-name
// followed that many times (ODF 1.3, Part 3, 16.9).
func (f *pageFlow) masterAt(mp *odf.MasterPage, offset int) *odf.MasterPage {
	for i := 0; i < offset && i < maxNextChain && mp != nil; i++ {
		next := f.b.styles.Master(mp.Next)
		if next == nil || next == mp {
			break
		}
		mp = next
	}
	return mp
}

// pageFor is the layout's PageFor: the page box of the master page the
// page shows, the instance its name says, at its offset in it.
func (f *pageFlow) pageFor(info layout.PageInfo) layout.PageBox {
	inst := f.byName[info.Name]
	if inst == nil && len(f.instances) > 0 {
		inst = f.instances[0]
	}
	if inst == nil {
		inst = f.start("", 0)
	}
	if !inst.seen || info.Index < inst.first {
		inst.first, inst.seen = info.Index, true
	}
	offset := info.Index - inst.first
	for i := range f.blanks {
		if i > info.Index {
			delete(f.blanks, i)
		}
	}
	mp := f.masterAt(inst.master, offset)
	number := 1
	if info.Index > 0 && info.Index-1 < len(f.entries) {
		number = f.entries[info.Index-1].number + 1
	}
	if offset == 0 && inst.restart > 0 {
		if info.Index > 0 && (inst.restart-number)%2 != 0 {
			// Writer keeps odd numbers on right pages: a restart of the
			// other parity than the page would have inserts a blank page,
			// which the PDF leaves out and the page count counts.
			f.blanks[info.Index] = true
		} else {
			delete(f.blanks, info.Index)
		}
		number = inst.restart
	}
	left := number%2 == 0
	g := f.geom(mp, left, offset == 0)
	box := g.box
	for len(f.entries) <= info.Index {
		f.entries = append(f.entries, pageEntry{})
	}
	f.entries[info.Index] = pageEntry{inst: inst, master: mp, number: number, box: box, geom: g, left: left, first: offset == 0}
	f.entries = f.entries[:info.Index+1]
	return box
}

// geom returns the geometry of the master page mp on a left or right
// page, the first of its instance or not.
func (f *pageFlow) geom(mp *odf.MasterPage, left, first bool) *pageGeom {
	key := "\x00"
	if mp != nil {
		key = mp.Name
	}
	key += map[bool]string{true: "L", false: "R"}[left] + map[bool]string{true: "F", false: ""}[first]
	if g, ok := f.geoms[key]; ok {
		return g
	}
	if f.building > 0 {
		return f.buildGeom(mp, left, first, false)
	}
	f.building++
	g := f.buildGeom(mp, left, first, true)
	f.building--
	f.geoms[key] = g
	return g
}

// pageSize returns a page layout's size, as office.Options overrides it.
func (f *pageFlow) pageSize(pp odf.Props) (float64, float64) {
	w, okW := odf.Length(pp.Get("fo:page-width"))
	h, okH := odf.Length(pp.Get("fo:page-height"))
	if !okW || w <= 0 || w > office.MaxPageSide {
		w = defaultPageWidth
	}
	if !okH || h <= 0 || h > office.MaxPageSide {
		h = defaultPageHeight
	}
	if f.opts.PageWidth > 0 {
		w, h = f.opts.PageWidth, f.opts.PageHeight
	}
	if f.opts.Landscape && h > w {
		w, h = h, w
	}
	return w, h
}

// minPageArea is the least page area a page keeps on each axis, a
// point: margins and headers that leave less are cut back.
const minPageArea = 1

// buildGeom lays a master page's page layout out (ODF 1.3, Part 3, 17.2
// and 17.5): the size, the margins, the border and padding drawn as the
// page frame inside them, then the header and the footer with their
// spacing, the body below; the left page of a mirrored layout swaps its
// side margins. hf false leaves the header and the footer out.
func (f *pageFlow) buildGeom(mp *odf.MasterPage, left, first, hf bool) *pageGeom {
	var pl *odf.PageLayout
	if mp != nil {
		pl = f.b.styles.PageLayouts[mp.PageLayout]
	}
	if pl == nil {
		pl = &odf.PageLayout{}
	}
	pp := pl.Page
	if va := pp.Get("style:vertical-align"); va != "" && va != "top" && va != "auto" {
		f.b.warn("page-vertical-align", "a page's text is laid out from the top, not "+va)
	}
	g := &pageGeom{format: pp.Get("style:num-format"), mirrored: pl.PageUsage == "mirrored"}
	if _, ok := pp.Lookup("style:num-format"); !ok {
		g.format = "1"
	}
	w, h := f.pageSize(pp)
	margin := [4]float64{defaultPageMargin, defaultPageMargin, defaultPageMargin, defaultPageMargin}
	if !pp.Empty() {
		margin = [4]float64{}
	}
	ps := paraState{}
	ps = foldPara(ps, pp)
	for i := range margin {
		if _, ok := pp.Lookup("fo:margin-" + sideNames[i]); ok || pp.Has("fo:margin") {
			margin[i] = max(ps.margin[i], 0)
		}
	}
	if g.mirrored && left {
		margin[css.Left], margin[css.Right] = margin[css.Right], margin[css.Left]
	}
	box := layout.PageBox{Width: w, Height: h}
	inner := margin
	if hasBorder(ps.border) {
		fs := baseStyle()
		fs.Border = ps.border
		box.Frame = &layout.PageFrame{Inset: margin, Style: fs}
	}
	for i := range inner {
		inner[i] += usedWidth(ps.border[i]) + ps.padding[i]
	}
	if bg := f.pageBackground(mp, ps); bg != nil {
		box.Background = bg
	}
	areaW := w - inner[css.Left] - inner[css.Right]
	g.top, g.bottom = inner[css.Top], h-inner[css.Bottom]
	if mp != nil && hf {
		g.header = f.hf(hfElement(mp.Header, mp.HeaderLeft, mp.HeaderFirst, left, first), pl.Header, inner[css.Left], areaW, true)
		g.footer = f.hf(hfElement(mp.Footer, mp.FooterLeft, mp.FooterFirst, left, first), pl.Footer, inner[css.Left], areaW, false)
	}
	if g.header != nil {
		inner[css.Top] += g.header.h + g.header.space
	}
	if g.footer != nil {
		inner[css.Bottom] += g.footer.h + g.footer.space
	}
	if inner[css.Top]+inner[css.Bottom] > h-minPageArea {
		inner[css.Bottom] = max(h-minPageArea-inner[css.Top], 0)
		inner[css.Top] = min(inner[css.Top], h-minPageArea)
	}
	if inner[css.Left]+inner[css.Right] > w-minPageArea {
		inner[css.Left], inner[css.Right] = 0, 0
	}
	box.Margin = inner
	box.Footnote = f.b.footnoteArea(pp)
	g.box = box
	return g
}

// hasBorder reports a border that draws.
func hasBorder(bd [4]css.Border) bool {
	for _, s := range bd {
		if usedWidth(s) > 0 {
			return true
		}
	}
	return false
}

// usedWidth returns a border's used width: 0 for none or hidden.
func usedWidth(bd css.Border) float64 {
	if bd.Style == css.BorderNone || bd.Style == css.BorderHidden {
		return 0
	}
	return bd.Width
}

// pageBackground returns the page box's background: the page layout's
// fo:background-color, else the fill of the master page's drawing-page
// style (draw:fill, which Writer 7 and later writes); nil for none.
func (f *pageFlow) pageBackground(mp *odf.MasterPage, ps paraState) *css.Style {
	c := ps.background
	if c.A == 0 && mp != nil {
		dp := f.b.styles.Resolve(odf.ScopeStyles, odf.FamilyDrawingPage, mp.El.A(odf.NSDraw, "style-name"), odf.DrawingPageProps)
		if dp.Get("draw:fill") == "solid" {
			if col, ok := odf.ParseColor(dp.Get("draw:fill-color")); ok {
				c = cssColor(col)
			}
		}
	}
	if c.A == 0 {
		return nil
	}
	st := baseStyle()
	st.BackgroundColor = c
	return st
}

// hfElement returns the header or footer element a page shows: the
// first page's on the first page of its instance, the left one on a
// left page, else the default; nil when it is absent or not displayed.
func hfElement(def, leftEl, firstEl *odf.Element, left, first bool) *odf.Element {
	el := def
	if left && leftEl != nil {
		el = leftEl
	}
	if first && firstEl != nil {
		el = firstEl
	}
	if el == nil || el.A(odf.NSStyle, "display") == "false" {
		return nil
	}
	return el
}

// hf returns the placement of the header (top) or footer el, whose
// header-footer-properties are props, on a page whose area starts at x
// and is w wide (ODF 1.3, Part 3, 17.5): its side margins narrow it, its
// height is its content's, at least fo:min-height, or svg:height; its
// spacing is fo:margin-bottom of a header and fo:margin-top of a footer.
func (f *pageFlow) hf(el *odf.Element, props odf.Props, x, w float64, top bool) *hfGeom {
	if el == nil {
		return nil
	}
	ps := foldPara(paraState{}, props)
	g := &hfGeom{el: el, x: x + ps.margin[css.Left], w: max(w-ps.margin[css.Left]-ps.margin[css.Right], minPageArea)}
	if top {
		g.space = max(ps.margin[css.Bottom], 0)
	} else {
		g.space = max(ps.margin[css.Top], 0)
	}
	minH, _ := odf.Length(props.Get("fo:min-height"))
	fixed, hasFixed := odf.Length(props.Get("svg:height"))
	// Writer's minimum and fixed heights include the spacing: the
	// header's box is at least that tall with its spacing.
	content := f.measure(g, 0)
	g.h = max(content, minH-g.space)
	if hasFixed && fixed > 0 {
		g.h = max(fixed-g.space, 0)
	}
	return g
}

// maxSubLayoutPages bounds the pages a header or footer lays out on: one
// is drawn.
const maxSubLayoutPages = 2

// hfNodes builds the content of a header or footer for the page number
// of pages.
func (f *pageFlow) hfNodes(g *hfGeom, number, pages int, format string) []*layout.Node {
	b := f.b
	saved, savedScope := b.hf, b.scope
	b.hf = &hfFields{number: number, count: pages, format: format}
	b.scope = odf.ScopeStyles
	if b.pkg.Flat {
		b.scope = odf.ScopeContent
	}
	nodes := b.blocks(g.el, blockCtx{})
	b.hf, b.scope = saved, savedScope
	return nodes
}

// measure returns the height the header or footer g's content takes.
func (f *pageFlow) measure(g *hfGeom, pages int) float64 {
	if f.b.fonts == nil {
		return 0
	}
	nodes := f.hfNodes(g, 1, max(pages, 1), "1")
	st := baseStyle()
	st.Display = css.Block
	st.Width = css.Pts(g.w)
	box := &layout.Node{Kind: layout.Element, Style: st, ID: "odt-hf-measure", Children: nodes}
	res, err := layout.Layout(f.b.ctx, wrap(box), f.b.fonts, layout.Options{
		Page: layout.PageBox{Width: g.w + 2, Height: office.MaxPageSide}, MaxPages: maxSubLayoutPages,
		Image: f.b.loadPicture,
	})
	if err != nil {
		return 0
	}
	if r, ok := res.Extents["odt-hf-measure"]; ok {
		return r.Height
	}
	return 0
}

// wrap returns a root block holding n.
func wrap(n *layout.Node) *layout.Node {
	st := baseStyle()
	st.Display = css.Block
	return &layout.Node{Kind: layout.Element, Style: st, Children: []*layout.Node{n}}
}

// marginContent is the layout's MarginContent: the header and footer of
// the master page a page shows, its page fields resolved, as artifacts.
func (f *pageFlow) marginContent(info layout.PageInfo, pages int) []compose.Fragment {
	if info.Index >= len(f.entries) {
		return nil
	}
	e := f.entries[info.Index]
	g := e.geom
	if g == nil || (g.header == nil && g.footer == nil) {
		return nil
	}
	root := baseStyle()
	root.Display = css.Block
	rn := &layout.Node{Kind: layout.Element, Style: root}
	for _, hg := range []*hfGeom{g.header, g.footer} {
		if hg == nil {
			continue
		}
		y := g.top
		if hg == g.footer {
			y = g.bottom - hg.h
		}
		st := baseStyle()
		st.Display = css.Block
		st.Position = css.Absolute
		st.Left, st.Top = css.Pts(hg.x), css.Pts(y)
		st.Width = css.Pts(hg.w)
		rn.Children = append(rn.Children, &layout.Node{Kind: layout.Element, Style: st, Children: f.hfNodes(hg, e.number, pages+len(f.blanks), g.format)})
	}
	res, err := layout.Layout(f.b.ctx, rn, f.b.fonts, layout.Options{
		Page: layout.PageBox{Width: e.box.Width, Height: e.box.Height}, MaxPages: maxSubLayoutPages,
		Image: f.b.loadPicture,
	})
	if err != nil || len(res.Pages) == 0 {
		return nil
	}
	return artifacts(res.Pages[0].Content)
}

// artifacts strips the structure from frags: headers and footers are
// pagination artifacts (ISO 32000-1, 14.8.2.2.2).
func artifacts(frags []compose.Fragment) []compose.Fragment {
	for _, fr := range frags {
		switch v := fr.(type) {
		case *compose.Text:
			v.Parent = nil
		case *compose.Image:
			v.Parent = nil
		case *compose.Path:
			v.Parent = nil
		case *compose.Group:
			artifacts(v.Content)
		}
	}
	return frags
}

// footnoteArea returns the style of the footnote area of a page layout
// (ODF 1.3, Part 3, 17.2 and 19.354, style:footnote-sep): the separator
// line drawn at its top, as wide as style:rel-width of the area and
// placed as style:adjustment says, the distances before and after it,
// and style:footnote-max-height.
func (b *builder) footnoteArea(pp odf.Props) *css.Style {
	st := baseStyle()
	st.Display = css.Block
	if mh, ok := odf.Length(pp.Get("style:footnote-max-height")); ok && mh > 0 {
		st.MaxHeight = css.Pts(mh)
	}
	sep := pp.Child("style:footnote-sep")
	if sep == nil {
		return st
	}
	w, _ := odf.Length(sep.A(odf.NSStyle, "width"))
	before, _ := odf.Length(sep.A(odf.NSStyle, "distance-before-sep"))
	after, _ := odf.Length(sep.A(odf.NSStyle, "distance-after-sep"))
	rel, ok := odf.Percent(sep.A(odf.NSStyle, "rel-width"))
	if !ok {
		rel = 0.25
	}
	st.Padding[css.Top] = css.Pts(max(before, 0) + max(w, 0) + max(after, 0))
	if w > 0 && rel > 0 && sep.A(odf.NSStyle, "line-style") != "none" {
		c := css.Color{A: 1}
		if col, ok := odf.ParseColor(sep.A(odf.NSStyle, "color")); ok {
			c = cssColor(col)
		}
		st.BackgroundGradient = &css.Gradient{Kind: css.LinearGradient, Angle: 90, Stops: []css.GradientStop{
			{Color: c, Position: css.Dimension{Kind: css.Percent}},
			{Color: c, Position: css.Dimension{Kind: css.Percent, Value: 1}},
		}}
		st.BackgroundRepeat = [2]css.Repeat{css.RepeatNoRepeat, css.RepeatNoRepeat}
		st.BackgroundSize = css.BackgroundSize{Kind: css.SizeExplicit, Width: css.Dimension{Kind: css.Percent, Value: rel}, Height: css.Pts(w)}
		x := 0.0
		switch sep.A(odf.NSStyle, "adjustment") {
		case "center":
			x = 0.5
		case "right":
			x = 1
		}
		st.BackgroundPosition = [2]css.Dimension{{Kind: css.Percent, Value: x}, css.Pts(max(before, 0))}
	}
	return st
}
