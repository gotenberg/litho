package docx

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/offcrypto"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
	"github.com/gotenberg/litho/pkg/internal/svg"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// Convert reads the DOCX package src holds and lays it out as a paged
// document, with the fonts of the index fonts. It returns ctx's error when
// ctx ends first, and the errors office.Options documents:
// typeset.ErrInvalidOptions for options out of range or a nil fonts,
// text.ErrNoFonts for an index without faces, office.ErrEncrypted for an
// encrypted package opts.Password does not open (missing, wrong, or a
// scheme offcrypto does not read), office.ErrMalformed for a package that
// is not a WordprocessingML document or a compound file that cannot be read, limits.ErrInputTooLarge past a size cap and
// limits.ErrTooManyPages past the page cap. The output depends on src,
// fonts' index and opts alone.
func Convert(ctx context.Context, src io.Reader, fonts *text.Fonts, opts office.Options) (*typeset.Document, error) {
	// The instances of variable fonts the conversion draws belong to it.
	fonts = fonts.ForConversion()
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	if fonts == nil {
		return nil, fmt.Errorf("%w: no font index", typeset.ErrInvalidOptions)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
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
	// A zip package comes back unchanged; a compound file is an encrypted
	// package (MS-OFFCRYPTO, 2.3.4.4) opened with the password.
	if data, err = offcrypto.OpenOOXML(ctx, data, opts.Password); err != nil {
		return nil, fmt.Errorf("opening the package: %w", err)
	}
	pkg, err := ooxml.Open(ctx, data)
	if err != nil {
		return nil, fmt.Errorf("opening the package: %w", err)
	}
	d, err := load(ctx, pkg, fonts, opts)
	if err != nil {
		return nil, err
	}
	doc, err := d.convert()
	if err != nil {
		return nil, err
	}
	doc.Warnings = append(doc.Warnings, drawings.Take()...)
	return doc, nil
}

// convert builds the layout tree, lays it out and assembles the document.
func (d *document) convert() (*typeset.Document, error) {
	root := d.root
	if !root.Is(w, "document") {
		return nil, fmt.Errorf("%w: the main part is not a w:document", office.ErrMalformed)
	}
	d.background = d.pageBackground(root.Child(w, "background"))
	secs := d.body(root)
	if err := d.ctx.Err(); err != nil {
		return nil, err
	}
	if d.err != nil {
		return nil, d.err
	}
	tree := &layout.Node{Kind: layout.Element, Style: d.blockStyle()}
	for i, s := range secs {
		var extra []*layout.Node
		if i == len(secs)-1 && len(d.endnoteBodies) > 0 {
			extra = d.endnoteBlocks()
		}
		tree.Children = append(tree.Children, d.sectionNode(s, extra))
	}
	single := d.opts.SinglePage
	fittedLimit := 0.0
	if single {
		fittedLimit = d.opts.FittedPageLimit()
	}
	if single {
		if err := layout.OnePage(d.ctx, tree); err != nil {
			return nil, err
		}
	}
	// The tree is finished: its paragraphs and runs styled alike share
	// one style.
	layout.ShareStyles(d.ctx, tree, d.shared)
	pg := d.newPages(secs)
	first := pg.box(layout.PageInfo{Name: secs[0].name})
	lay := func() (*layout.Result, error) {
		return layout.Layout(d.ctx, tree, d.fonts, layout.Options{
			Page:          first,
			PageFor:       pg.box,
			Placed:        pg.place,
			MarginContent: pg.margins,
			FitFirstPage:  single,
			FittedLimit:   fittedLimit,
			Lang:          d.lang(),
			MaxPages:      d.opts.MaxPages,
			Logger:        d.opts.Logger,
		})
	}
	res, err := lay()
	if err != nil {
		return nil, err
	}
	if d.err != nil {
		return nil, d.err
	}
	if single && len(res.Pages) > 1 && fittedLimit > office.MaxPageSide {
		// The content passes 14,400 points: it is laid out again on the
		// tallest single page. A shorter document keeps the page it was
		// laid out on.
		pg.tall = true
		if res, err = lay(); err != nil {
			return nil, err
		}
		if d.err != nil {
			return nil, d.err
		}
	}
	if single && len(res.Pages) == 1 && d.placeOnSinglePage(res) {
		// The drawings positioned from the page's middle or bottom moved
		// to the single page's: the document lays out again on a page that
		// tall, which a drawing in its bottom margin then does not grow.
		// Text a moved drawing pushes past it keeps the first layout.
		pg.height = res.Pages[0].Height
		again, err := lay()
		if err != nil {
			return nil, err
		}
		if d.err != nil {
			return nil, d.err
		}
		if len(again.Pages) == 1 {
			res = again
		}
	}
	if single {
		if w := office.FirstPageOnly(res, d.opts.FittedPageLimit()); w != nil {
			d.warn(w.Code, w.Effect, w.Message)
		}
	}
	if checkSharedStyles && !d.shared.Intact() {
		return nil, errSharedStyleWritten
	}
	return d.assemble(res)
}

// placeOnSinglePage moves the drawings whose position depends on the
// page's height ([heightAnchor]) to where they sit on the single page of
// res, and reports whether any moved. The page is as tall as the content
// laid out with the drawings on the first section's page, so each still
// fits on it.
func (d *document) placeOnSinglePage(res *layout.Result) bool {
	if len(d.heightAnchors) == 0 || len(res.Pages) == 0 {
		return false
	}
	h := res.Pages[0].Height
	moved := false
	for _, ha := range d.heightAnchors {
		moved = ha.moveTo(d, h) || moved
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
func (d *document) share(st *css.Style) *css.Style {
	return d.shared.ShareCopy(st)
}

// endnoteBlocks returns the endnote bodies, laid out after the text under a
// short rule, as LibreOffice prints a DOCX document's endnotes.
func (d *document) endnoteBlocks() []*layout.Node {
	rule := d.blockStyle()
	rule.Width = css.Dimension{Kind: css.Percent, Value: 0.25}
	rule.Border[css.Top] = css.Border{Width: 0.5, Style: css.Solid, Color: css.Color{A: 1}}
	rule.Margin[css.Top], rule.Margin[css.Bottom] = css.Pts(12), css.Pts(6)
	out := make([]*layout.Node, 0, 1+len(d.endnoteBodies))
	out = append(out, &layout.Node{Kind: layout.Element, Style: rule})
	return append(out, d.endnoteBodies...)
}

// lang returns the document's language: the default run language of its
// styles, else its theme font language, else the caller's default.
func (d *document) lang() string {
	if l := d.styles.defRun.lang; l != nil && *l != "" {
		return *l
	}
	if d.settings.lang != "" {
		return d.settings.lang
	}
	return d.opts.DefaultLang
}

// assemble returns the document of the layout res: every page with its
// links, the outline, the metadata and the warnings. The caller selects
// pages afterwards. With a sink in the options, each page goes to it
// once its links are resolved, and the document holds no page.
func (d *document) assemble(res *layout.Result) (*typeset.Document, error) {
	dest := func(dd typeset.Dest) (typeset.Dest, bool) {
		return dd, dd.Page >= 1 && dd.Page <= len(res.Pages)
	}
	cp, err := d.pkg.CoreProperties(d.ctx)
	if err != nil {
		return nil, err
	}
	subject := cp.Subject
	if subject == "" {
		subject = cp.Description
	}
	lang := cp.Language
	if lang == "" {
		lang = d.lang()
	}
	padTableRows(res.Structure)
	if err := d.attachControls(res); err != nil {
		return nil, err
	}
	doc := &typeset.Document{
		Metadata:  typeset.Metadata{Title: cp.Title, Author: cp.Creator, Subject: subject, Keywords: cp.Keywords, Lang: lang},
		Structure: res.Structure,
		Warnings:  d.warnings,
	}
	sink := d.opts.Sink
	if sink != nil {
		sink.Expect(len(res.Pages))
	}
	for k, pg := range res.Pages {
		d.symbols.Restore(pg)
		links := pg.Links[:0:0]
		for _, l := range pg.Links {
			if l.Dest != nil {
				dd, ok := dest(*l.Dest)
				if !ok {
					continue
				}
				l.Dest = &dd
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
		if err := sink.Page(d.ctx, pg); err != nil {
			// The stream names the page it refuses.
			return nil, err
		}
		res.Pages[k] = nil
	}
	if !d.opts.OmitOutline {
		anchors := make(map[string]typeset.Dest, len(res.Anchors))
		for id, a := range res.Anchors {
			if a, ok := dest(a); ok {
				anchors[id] = a
			}
		}
		doc.Outline = outline(d.headings, anchors)
	}
	doc.Warnings = append(doc.Warnings, res.Warnings...)
	return doc, nil
}
