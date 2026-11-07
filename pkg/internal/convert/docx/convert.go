package docx

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/convert/pagerange"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// Convert reads the DOCX package src holds and lays it out as a paged
// document, with the fonts of the index fonts. It returns ctx's error when
// ctx ends first, and the errors office.Options documents:
// office.ErrInvalidOptions for options out of range or a nil fonts,
// text.ErrNoFonts for an index without faces, office.ErrEncrypted for an
// encrypted package, office.ErrMalformed for a package that is not a
// WordprocessingML document, office.ErrInputTooLarge past a size cap and
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
	ranges, err := pagerange.Parse(opts.PageRanges)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", office.ErrInvalidOptions, err)
	}
	data, err := office.ReadAll(ctx, src, opts.MaxInputBytes)
	if err != nil {
		return nil, fmt.Errorf("reading DOCX: %w", err)
	}
	if data, err = decrypt(ctx, data, opts.Password); err != nil {
		return nil, fmt.Errorf("decrypting DOCX: %w", err)
	}
	pkg, err := ooxml.Open(data)
	if err != nil {
		return nil, fmt.Errorf("opening DOCX: %w", err)
	}
	d, err := load(ctx, pkg, fonts, opts)
	if err != nil {
		return nil, fmt.Errorf("reading DOCX: %w", err)
	}
	d.ranges = ranges
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
// styles, else its theme font language.
func (d *document) lang() string {
	if l := d.styles.defRun.lang; l != nil && *l != "" {
		return *l
	}
	return d.settings.lang
}

// assemble returns the document of the layout res: the pages the page
// ranges select, their links and the outline kept for them, the
// metadata and the warnings.
func (d *document) assemble(res *layout.Result) (*compose.Document, error) {
	keep, err := pagerange.Select(d.ranges, len(res.Pages))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", office.ErrInvalidOptions, err)
	}
	at := make([]int, len(res.Pages))
	for i, k := range keep {
		at[k] = i + 1
	}
	dest := func(dd compose.Dest) (compose.Dest, bool) {
		if dd.Page < 1 || dd.Page > len(at) || at[dd.Page-1] == 0 {
			return dd, false
		}
		dd.Page = at[dd.Page-1]
		return dd, true
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
	for _, k := range keep {
		pg := res.Pages[k]
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
	if len(keep) < len(res.Pages) {
		compose.PruneStructure(doc)
	}
	for _, wn := range res.Warnings {
		if wn.Page > 0 && wn.Page <= len(at) {
			wn.Page = at[wn.Page-1]
		}
		doc.Warnings = append(doc.Warnings, wn)
	}
	return doc, nil
}
