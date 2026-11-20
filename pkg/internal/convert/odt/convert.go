package odt

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/intconv"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/odf"
	"github.com/gotenberg/litho/pkg/internal/svg"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
	"github.com/gotenberg/litho/pkg/models"
)

// Convert reads the ODT package src holds, or a flat .fodt document, and
// lays it out as a paged document, with the fonts of the index fonts. It
// returns ctx's error when ctx ends first, and the errors office.Options
// documents: office.ErrMalformed for a source that is not an
// OpenDocument text document, office.ErrEncrypted for an encrypted one
// opts.Password does not open (ODF 1.3, Part 2, 3.4),
// limits.ErrInputTooLarge past the package, XML or layout caps,
// limits.ErrTooManyPages past opts.MaxPages. The output depends on src,
// fonts' index and opts alone. See the package documentation for what is
// drawn.
func Convert(ctx context.Context, src io.Reader, fonts *text.Fonts, opts office.Options) (*typeset.Document, error) {
	// The instances of variable fonts the conversion draws belong to it.
	fonts = fonts.ForConversion()
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if fonts == nil {
		return nil, fmt.Errorf("%w: no font index", typeset.ErrInvalidOptions)
	}
	if fonts.Len() == 0 {
		return nil, text.ErrNoFonts
	}
	// The readers add the SVG pictures they parse to the collector; laying
	// the pages out draws them, which may add warnings the reader never saw.
	ctx, drawings := svg.WithCollector(ctx)
	data, err := office.ReadAll(ctx, src, opts.MaxInputBytes)
	if err != nil {
		return nil, err
	}
	pkg, err := odf.Open(ctx, data, odf.Options{Password: opts.Password})
	if err != nil {
		return nil, fmt.Errorf("opening the package: %w", err)
	}
	if !textMime(pkg.MimeType) {
		return nil, fmt.Errorf("%w: the document is %q, not a text document", office.ErrMalformed, pkg.MimeType)
	}
	body := textBody(odf.Section(pkg.Content, "body"), pkg.MimeType != "")
	if body == nil {
		return nil, fmt.Errorf("%w: the document has no office:text body", office.ErrMalformed)
	}
	b := &builder{
		ctx: ctx, pkg: pkg, styles: odf.NewStyles(pkg), shared: css.NewStyles(), fonts: fonts, symbols: office.NewSymbolCodes(),
		metricsCache: map[string][2]float64{}, warned: map[string]bool{}, pics: map[string]picResult{}, meta: map[*layout.Node]paraMeta{},
		tabsRelative:  configBool(pkg.Settings, "TabsRelativeToIndent", true),
		absoluteAlign: odf.AbsoluteStartEnd(pkg),
	}
	marks, err := resolveChanges(ctx, body, opts.TrackedChanges, b.rowTracked, b.warn)
	if err != nil {
		return nil, err
	}
	b.changeMarks = marks
	b.date = opts.PrintDate
	b.formFields = opts.FormFields
	b.single = opts.SinglePage
	b.derivedAlt, b.defaultLang = opts.DerivedAltText, opts.DefaultLang
	b.pages = newPageFlow(b, opts)
	if opts.Comments == models.CommentsAnnotations {
		b.readComments(body)
	}
	if lc := odf.Section(pkg.Styles, "styles").Child(odf.NSText, "linenumbering-configuration"); lc.A(odf.NSText, "number-lines") == "true" {
		b.warn("line-numbering", warn.ContentUnsupported, warn.Omitted, "line numbers are not drawn")
	}
	root, first := b.document(body)
	if root.Lang == "" {
		root.Lang = opts.DefaultLang
	}
	if b.err != nil {
		return nil, b.err
	}
	if b.single {
		b.pages.warnWidths()
		if err := layout.OnePage(ctx, root); err != nil {
			return nil, err
		}
	}
	// The tree is finished: its paragraphs and spans styled alike share
	// one style.
	layout.ShareStyles(ctx, root, b.shared)
	lay := func() (*layout.Result, error) {
		b.pages.reset()
		return layout.Layout(ctx, root, fonts, layout.Options{
			Page:          first,
			PageFor:       b.pages.pageFor,
			MarginContent: b.pages.marginContent,
			FitFirstPage:  b.single,
			Lang:          root.Lang,
			Fallback:      text.FallbackLibreOffice,
			Image:         b.loadPicture,
			MaxPages:      opts.MaxPages,
			Logger:        opts.Logger,
		})
	}
	res, err := lay()
	if err != nil {
		return nil, err
	}
	if b.err != nil {
		return nil, b.err
	}
	// A table of contents is numbered from where the headings landed,
	// then laid out again until the numbers settle.
	for pass := 1; pass < maxIndexPasses && len(b.tocs) > 0 && b.updateTOCs(res); pass++ {
		if b.err != nil {
			return nil, b.err
		}
		if res, err = lay(); err != nil {
			return nil, err
		}
	}
	if b.err != nil {
		return nil, b.err
	}
	if b.single && len(res.Pages) == 1 && b.placeOnSinglePage(res) {
		// The frames centred in the page or at its bottom moved to the
		// single page's: the document lays out again on a page that tall,
		// which a frame in its bottom margin then does not grow. Text a
		// moved frame pushes past it keeps the first layout.
		b.pages.height = res.Pages[0].Height
		again, err := lay()
		if err != nil {
			return nil, err
		}
		if b.err != nil {
			return nil, b.err
		}
		if len(again.Pages) == 1 {
			res = again
		}
	}
	if b.single {
		if w := office.FirstPageOnly(res); w != nil {
			b.warn("single-page", w.Code, w.Effect, w.Message)
		}
	}
	if checkSharedStyles && !b.shared.Intact() {
		return nil, errSharedStyleWritten
	}
	doc, err := b.assemble(res, opts, root.Lang)
	if err != nil {
		return nil, err
	}
	doc.Warnings = append(doc.Warnings, drawings.Take()...)
	return doc, nil
}

// placeOnSinglePage moves the frames whose position depends on the
// page's height ([heightFrame]) to where they sit on the single page of
// res, and reports whether any moved. The page is as tall as the content
// laid out with the frames on their master page's page, so each still
// fits on it.
func (b *builder) placeOnSinglePage(res *layout.Result) bool {
	if len(b.heightFrames) == 0 || len(res.Pages) == 0 {
		return false
	}
	h := res.Pages[0].Height
	moved := false
	for _, hf := range b.heightFrames {
		moved = hf.moveTo(b, h) || moved
	}
	return moved
}

// checkSharedStyles makes a conversion fail with errSharedStyleWritten
// when a style was written after it was shared, which every node holding
// it would see. The package's tests set it.
var checkSharedStyles bool

// errSharedStyleWritten reports a style written after it was shared: a
// converter bug.
var errSharedStyleWritten = errors.New("a node's style was written after it was shared")

// share returns the style of the conversion equal to st, adopting a copy
// of st when there is none: st stays the caller's, the result is read
// and never written. A node holds the result from the moment it is made,
// and a converter that changes a node's style after copies the shared
// value, changes the copy and shares it again.
func (b *builder) share(st *css.Style) *css.Style {
	return b.shared.ShareCopy(st)
}

// textBody returns the office:text of office:body (ODF 1.3, Part 3,
// 3.3), else, as LibreOffice opens a text document whose body element is
// misnamed, the body's first element child when the media type, named,
// says a text document. nil when the body holds no such element.
func textBody(body *odf.Element, named bool) *odf.Element {
	if t := body.Child(odf.NSOffice, "text"); t != nil {
		return t
	}
	if body == nil || !named {
		return nil
	}
	for _, c := range body.Children {
		if !c.IsText() {
			return c
		}
	}
	return nil
}

// textMime reports the media types of a text document (ODF 1.3, Part 3,
// Appendix C): a text document, its template, a master document, and a
// flat document that names none.
func textMime(m string) bool {
	switch m {
	case "", odf.MimeText, odf.MimeTextTemplate, odf.MimeTextMaster, odf.MimeTextWeb:
		return true
	}
	return false
}

// configNS is the namespace of settings.xml's configuration items (ODF
// 1.3, Part 3, 3.10).
const configNS = "urn:oasis:names:tc:opendocument:xmlns:config:1.0"

// configBool returns the boolean configuration item name of settings,
// def when absent.
func configBool(settings *odf.Element, name string, def bool) bool {
	if settings == nil {
		return def
	}
	stack := []*odf.Element{odf.Section(settings, "settings")}
	for steps := 0; len(stack) > 0 && steps < odf.MaxElements; steps++ {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if cur == nil {
			continue
		}
		if cur.Is(configNS, "config-item") && cur.A(configNS, "name") == name {
			return strings.TrimSpace(cur.TextContent()) == "true"
		}
		for _, c := range cur.Children {
			if !c.IsText() {
				stack = append(stack, c)
			}
		}
	}
	return def
}

// document builds the root of the layout tree from office:text: the body's
// blocks grouped by the master page they lie on, each group a named page
// with its page layout's columns, then the endnotes. It returns the root
// and the first page's box. The drawings before the first block of the
// flow wait for it: a frame is no flow content, and the first paragraph
// or table names the first page's master page, the page the frames are
// placed on.
//
// # Corpus evidence
//
//   - libreoffice/odt: layout/tdf122607.odt holds a page-anchored frame
//     before a paragraph of the First Page master.
func (b *builder) document(body *odf.Element) (*layout.Node, layout.PageBox) {
	rootStyle := baseStyle()
	rootStyle.Display = css.Block
	root := &layout.Node{Kind: layout.Element, Style: rootStyle, Role: "Document"}
	root.Lang = b.paraText("Standard").langTag()
	b.chains = scanChains(body)
	var group *layout.Node
	ctx := blockCtx{}
	open := func(master string, restart int) {
		inst := b.pages.start(master, restart)
		st := baseStyle()
		st.Display = css.Block
		st.Page = inst.name
		if group != nil {
			st.BreakBefore = css.Page
		}
		if mp := inst.master; mp != nil {
			if pl := b.styles.PageLayouts[mp.PageLayout]; pl != nil {
				b.columns(st, pl.Page.Child("style:columns"), b.currentAreaWidth())
			}
		}
		group = &layout.Node{Kind: layout.Element, Style: st}
		root.Children = append(root.Children, group)
	}
	// frames holds the drawings before the first block of the flow.
	var frames []*odf.Element
	flush := func() {
		for _, f := range frames {
			group.Children = append(group.Children, b.block(f, &ctx)...)
		}
		frames = nil
	}
	for _, c := range body.Children {
		if b.err != nil {
			break
		}
		if master, restart, ok := b.masterOf(c); ok {
			open(master, restart)
		} else if group == nil && !c.IsText() && blockLike(c) {
			if c.Name.Space == odf.NSDraw {
				frames = append(frames, c)
				continue
			}
			open("", 0)
		}
		if group == nil {
			// Declarations before the first block.
			continue
		}
		flush()
		group.Children = append(group.Children, b.block(c, &ctx)...)
	}
	if group == nil {
		open("", 0)
	}
	flush()
	for _, g := range root.Children {
		b.spacing(g.Children)
		g.Children = b.wrapLists(g.Children, 0)
	}
	if en := b.endnotes(); en != nil {
		group.Children = append(group.Children, en)
	}
	g := b.pages.geom(b.pages.instances[0].master, false, true)
	b.pageOrigin = [2]float64{g.box.Margin[css.Left], g.box.Margin[css.Top]}
	return root, g.box
}

// blockLike reports a body child that lays out: a paragraph, list, table,
// section, index or frame.
func blockLike(c *odf.Element) bool {
	switch c.Name.Space {
	case odf.NSText:
		switch c.Name.Local {
		case "p", "h", "list", "numbered-paragraph", "section", "table-of-content",
			"alphabetical-index", "illustration-index", "table-index", "object-index",
			"user-index", "bibliography":
			return true
		}
	case odf.NSTable:
		return c.Name.Local == "table"
	case odf.NSDraw:
		return true
	}
	return false
}

// maxMasterSearch bounds the elements searched for the first paragraph
// of a body child: 64. Anti-DoS guard, not spec-mandated: a list's or
// section's first paragraph sits a few levels down.
const maxMasterSearch = 64

// masterOf returns the master page a body child starts, with its page
// number restart: the style:master-page-name of its first paragraph's or
// its table's style (ODF 1.3, Part 3, 20.307), and the
// style:page-number of its paragraph or table properties (20.321).
func (b *builder) masterOf(c *odf.Element) (string, int, bool) {
	stack := []*odf.Element{c}
	for steps := 0; len(stack) > 0 && steps < maxMasterSearch; steps++ {
		cur := stack[0]
		stack = stack[1:]
		var st *odf.Style
		var kind string
		switch {
		case cur.Is(odf.NSText, "p"), cur.Is(odf.NSText, "h"):
			st = b.styles.Style(b.scope, odf.FamilyParagraph, b.paraStyleName(cur))
			kind = odf.ParagraphProps
		case cur.Is(odf.NSTable, "table"):
			st = b.styles.Style(b.scope, odf.FamilyTable, cur.A(odf.NSTable, "style-name"))
			kind = odf.TableProps
		case cur.Is(odf.NSText, "list"), cur.Is(odf.NSText, "list-item"), cur.Is(odf.NSText, "section"),
			cur.Is(odf.NSText, "list-header"), cur.Is(odf.NSText, "numbered-paragraph"):
			for _, k := range cur.Children {
				if !k.IsText() {
					stack = append(stack, k)
				}
			}
			continue
		default:
			continue
		}
		if st == nil || !st.HasMasterPage || st.MasterPage == "" {
			return "", 0, false
		}
		restart := 0
		if n, err := intconv.Atoi(st.Props(kind).Get("style:page-number")); err == nil && n > 0 {
			restart = min(n, intconv.MaxCountStart)
		}
		return st.MasterPage, restart, true
	}
	return "", 0, false
}

// assemble returns the document of the layout res: every page with its
// links, the outline of the headings, the metadata and the warnings. The
// caller selects pages afterwards. With a sink in the options, each page
// goes to it once its links are resolved, and the document holds no page.
func (b *builder) assemble(res *layout.Result, opts office.Options, lang string) (*typeset.Document, error) {
	dest := func(d typeset.Dest) (typeset.Dest, bool) {
		return d, d.Page >= 1 && d.Page <= len(res.Pages)
	}
	doc := &typeset.Document{Metadata: b.metadata(lang), Structure: res.Structure}
	if err := b.attachControls(res); err != nil {
		return nil, err
	}
	sink := opts.Sink
	if sink != nil {
		sink.Expect(len(res.Pages))
	}
	for k, pg := range res.Pages {
		b.symbols.Restore(pg)
		links := pg.Links[:0:0]
		for _, l := range pg.Links {
			if l.Dest != nil {
				d, ok := dest(*l.Dest)
				if !ok {
					continue
				}
				l.Dest = &d
			}
			links = append(links, l)
		}
		pg.Links = links
		if sink == nil {
			doc.Pages = append(doc.Pages, pg)
			continue
		}
		// The page leaves the layout's result once handed over: the sink
		// writes it and keeps none of its fragments.
		if err := sink.Page(b.ctx, pg); err != nil {
			// The stream names the page it refuses.
			return nil, err
		}
		res.Pages[k] = nil
	}
	if !opts.OmitOutline {
		doc.Outline = outline(b.headings, res.Anchors, dest)
	}
	doc.Warnings = append(doc.Warnings, b.warnings...)
	doc.Warnings = append(doc.Warnings, res.Warnings...)
	return doc, nil
}

// metadata reads meta.xml's office:meta (ODF 1.3, Part 3, 4): the title,
// the creator, the subject, else the description, the keywords, the
// generator and the language; never a date.
func (b *builder) metadata(lang string) typeset.Metadata {
	m := typeset.Metadata{Lang: lang}
	meta := odf.Section(b.pkg.Meta, "meta")
	if meta == nil {
		return m
	}
	get := func(space, local string) string {
		return strings.Join(strings.Fields(meta.Child(space, local).TextContent()), " ")
	}
	m.Title = get(odf.NSDC, "title")
	m.Author = get(odf.NSDC, "creator")
	if m.Author == "" {
		m.Author = get(odf.NSMeta, "initial-creator")
	}
	m.Subject = get(odf.NSDC, "subject")
	if m.Subject == "" {
		m.Subject = get(odf.NSDC, "description")
	}
	var kw []string
	for _, k := range meta.All(odf.NSMeta, "keyword") {
		if s := strings.TrimSpace(k.TextContent()); s != "" {
			kw = append(kw, s)
		}
	}
	m.Keywords = strings.Join(kw, ", ")
	m.Creator = get(odf.NSMeta, "generator")
	if l := get(odf.NSDC, "language"); l != "" {
		m.Lang = l
	}
	return m
}
