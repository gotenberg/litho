package docx

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/offcrypto"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// Convert reads the DOCX package src holds and lays it out as a paged
// document, with the fonts of the index fonts. It returns ctx's error when
// ctx ends first, and the errors office.Options documents:
// office.ErrInvalidOptions for options out of range or a nil fonts,
// text.ErrNoFonts for an index without faces, office.ErrEncrypted for an
// encrypted package opts.Password does not open (missing, wrong, or a
// scheme offcrypto does not read), office.ErrMalformed for a package that
// is not a WordprocessingML document or a compound file that cannot be read, office.ErrInputTooLarge past a size cap and
// office.ErrTooManyPages past the page cap. The output depends on src,
// fonts' index and opts alone.
func Convert(ctx context.Context, src io.Reader, fonts *text.Fonts, opts office.Options) (*compose.Document, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	if fonts == nil {
		return nil, fmt.Errorf("%w: no font index", office.ErrInvalidOptions)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if fonts.Len() == 0 {
		return nil, fmt.Errorf("converting DOCX: %w", text.ErrNoFonts)
	}
	data, err := office.ReadAll(ctx, src, opts.MaxInputBytes)
	if err != nil {
		return nil, fmt.Errorf("reading DOCX: %w", err)
	}
	// A zip package comes back unchanged; a compound file is an encrypted
	// package (MS-OFFCRYPTO, 2.3.4.4) opened with the password.
	if data, err = offcrypto.OpenOOXML(ctx, data, opts.Password); err != nil {
		return nil, fmt.Errorf("opening DOCX: %w", err)
	}
	pkg, err := ooxml.Open(data)
	if err != nil {
		return nil, fmt.Errorf("opening DOCX: %w", err)
	}
	d, err := load(ctx, pkg, fonts, opts)
	if err != nil {
		return nil, fmt.Errorf("reading DOCX: %w", err)
	}
	return d.convert()
}

// convert builds the layout tree, lays it out and assembles the document.
func (d *document) convert() (*compose.Document, error) {
	root, err := d.part(d.main)
	if err != nil {
		return nil, fmt.Errorf("reading the DOCX main part: %w", err)
	}
	if !root.Is(w, "document") {
		return nil, fmt.Errorf("%w: the main part is not a w:document", office.ErrMalformed)
	}
	d.background = d.pageBackground(root.Child(w, "background"))
	secs := d.body(root)
	if err := d.ctx.Err(); err != nil {
		return nil, err
	}
	tree := &layout.Node{Kind: layout.Element, Style: d.blockStyle()}
	for i, s := range secs {
		var extra []*layout.Node
		if i == len(secs)-1 && len(d.endnoteBodies) > 0 {
			extra = d.endnoteBlocks()
		}
		tree.Children = append(tree.Children, d.sectionNode(s, extra))
	}
	// The tree is finished: its paragraphs and runs styled alike share
	// one style.
	layout.ShareStyles(d.ctx, tree)
	pg := d.newPages(secs)
	first := pg.box(layout.PageInfo{Name: secs[0].name})
	res, err := layout.Layout(d.ctx, tree, d.fonts, layout.Options{
		Page:          first,
		PageFor:       pg.box,
		Placed:        pg.place,
		MarginContent: pg.margins,
		Lang:          d.lang(),
		MaxPages:      d.opts.MaxPages,
		Logger:        d.opts.Logger,
	})
	if err != nil {
		if errors.Is(err, layout.ErrTooManyPages) {
			return nil, fmt.Errorf("%w: %w", office.ErrTooManyPages, err)
		}
		return nil, fmt.Errorf("laying out the DOCX document: %w", err)
	}
	return d.assemble(res)
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
// pages afterwards.
func (d *document) assemble(res *layout.Result) (*compose.Document, error) {
	dest := func(dd compose.Dest) (compose.Dest, bool) {
		return dd, dd.Page >= 1 && dd.Page <= len(res.Pages)
	}
	cp := d.pkg.CoreProperties(d.ctx)
	subject := cp.Subject
	if subject == "" {
		subject = cp.Description
	}
	lang := cp.Language
	if lang == "" {
		lang = d.lang()
	}
	doc := &compose.Document{
		Metadata:  compose.Metadata{Title: cp.Title, Author: cp.Creator, Subject: subject, Keywords: cp.Keywords, Lang: lang},
		Structure: res.Structure,
		Warnings:  d.warnings,
	}
	for _, pg := range res.Pages {
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
		doc.Pages = append(doc.Pages, pg)
	}
	if !d.opts.OmitOutline {
		anchors := make(map[string]compose.Dest, len(res.Anchors))
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
