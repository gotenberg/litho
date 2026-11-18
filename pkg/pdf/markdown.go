package pdf

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/annotation"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	pdfimage "github.com/gotenberg/litho/pkg/internal/graphics/image"
	"github.com/gotenberg/litho/pkg/internal/heuristics"
	"github.com/gotenberg/litho/pkg/internal/markdown"
	"github.com/gotenberg/litho/pkg/internal/tagging"
	"github.com/gotenberg/litho/pkg/internal/typography"
)

// ExtractMarkdown returns the text of the given 1-based pages as Markdown:
// CommonMark, with the tables of GitHub Flavored Markdown. A nil or empty
// pages reads every page. The pages are read in document order, each once,
// whatever order pages lists them in.
//
// The document's structure tree gives the structure when it reaches the
// text (ISO 32000-1, 14.7), as in a tagged PDF. Otherwise it is read from
// the layout, as auto-tagging reads it: larger text is a heading, and lines
// set as a list, a table or a running header or footer are read as one.
//
// Headings, paragraphs, lists, tables, block quotes, strong and emphasized
// text and code carry over, and so do links to a URI. A figure is written as
// an image with its alternative text and no destination, and is left out
// when it has none. Text the structure does not reach, such as text drawn by
// a form XObject on an untagged page, follows the page's other blocks, a
// paragraph per line.
//
// The options select the text as they do for [Reader.ExtractText].
// [WithArtifacts] keeps the running headers, footers and page numbers, and
// [WithAnnotationText] adds the text annotations show after the page's other
// blocks. A page that shows no text and looks scanned contributes the text
// the [WithOCREngine] engine reads, and nothing without one.
//
// Not supported: images themselves, colour and font styling beyond strong
// and emphasized text, and footnotes as Markdown footnotes.
//
// It returns [ErrInvalidPageNumber] when a page is out of range.
func (r *Reader) ExtractMarkdown(ctx context.Context, pages []int, opts ...ExtractTextOption) (_ string, err error) {
	defer recoverToError(r.logger(), "Reader.ExtractMarkdown", &err)
	var o extractTextOpts
	for _, opt := range opts {
		opt(&o)
	}

	all, err := r.resolvePages(ctx)
	if err != nil {
		return "", err
	}
	selected := slices.Clone(pages)
	if len(selected) == 0 {
		selected = make([]int, len(all))
		for i := range all {
			selected[i] = i + 1
		}
	}
	slices.Sort(selected)
	selected = slices.Compact(selected)
	for _, num := range selected {
		if num < 1 || num > len(all) {
			return "", fmt.Errorf("page %d: %w: the document has %d pages", num, ErrInvalidPageNumber, len(all))
		}
	}

	pageMap := document.BuildPageMap(all)
	read := make([]markdown.Page, len(selected))
	for i, num := range selected {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		read[i], err = r.markdownPage(ctx, all[num-1], num, pageMap, o)
		if err != nil {
			return "", fmt.Errorf("page %d: %w", num, err)
		}
	}

	return markdown.Write(ctx, r.doc, read, markdown.Options{Artifacts: o.includeHidden || o.includeArtifacts})
}

// markdownPage extracts what [markdown.Write] reads of page num.
func (r *Reader) markdownPage(ctx context.Context, page *document.Page, num int, pageMap map[ast.PdfIndirectRef]int, o extractTextOpts) (markdown.Page, error) {
	read := markdown.Page{Page: page, Index: num - 1}

	// The text is read from the bytes the layout's structure addresses, so a
	// glyph is found by the offset of the operation that drew it.
	data, err := tagging.PageContent(ctx, r.doc, page)
	if err != nil {
		return markdown.Page{}, err
	}
	ops, ranges, err := content.ParseStreamDataWithOffsets(ctx, data, content.WholeStream(), content.SplitGluedOperators())
	if err != nil {
		return markdown.Page{}, err
	}
	machine, config := r.textExtraction(page, o)
	read.Shown, err = typography.ExtractShownText(ctx, r.doc, machine, page, ops, config)
	if err != nil {
		if errors.Is(err, typography.ErrFontNotFound) {
			return markdown.Page{}, fmt.Errorf("text extraction halted: %w", ErrFontMissing)
		}
		return markdown.Page{}, err
	}
	read.Ranges = ranges

	annots, err := annotation.ExtractAnnotations(ctx, r.doc, page, pageMap)
	if err != nil {
		return markdown.Page{}, err
	}
	for _, a := range annots {
		if a.Subtype == "Link" && a.URI != "" {
			read.Links = append(read.Links, markdown.Link{Rect: a.Rect, URI: a.URI})
		}
	}

	if len(read.Shown.Elements) == 0 && r.ocr != nil {
		pageW, pageH := mediaBoxDimensions(page.MediaBox)
		images := pdfimage.ExtractImages(ctx, r.doc, page)
		if heuristics.RequiresOCR(nil, images, pageW, pageH) {
			read.Recognized, err = r.runOCR(ctx, page, images)
			if err != nil {
				return markdown.Page{}, err
			}
		}
	}
	return read, nil
}
