package mutations

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// PDFStampOptions describes a single PDF-page overlay applied by
// [StampPDF]. It is the internal form of the public PDFStamp API.
type PDFStampOptions struct {
	// SourceDoc is the PDF document the stamp page is copied from. The
	// caller keeps ownership and is responsible for keeping it valid
	// until the enclosing write finishes.
	SourceDoc *document.Document
	// SourcePages is the ordered page list from SourceDoc. Passed in so
	// the mutation doesn't have to re-resolve the page tree.
	SourcePages []*document.Page
	// Page is the 1-based page number to use from SourceDoc. Zero is
	// treated as 1.
	Page int
	// X and Y position the bottom-left corner of the stamp in points.
	X, Y float64
	// Scale is a uniform scale factor. Zero is treated as 1.0.
	Scale float64
	// Opacity is 0.0-1.0; zero is treated as fully opaque.
	Opacity float64
	// Rotation is the counter-clockwise rotation in degrees around (X, Y).
	Rotation float64
}

// StampPDF draws a page from another PDF document as a stamp on each
// selected target page. The stamp page is converted to a Form XObject via
// [pageToFormXObject] and then referenced just like an image stamp.
func StampPDF(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	pages []*document.Page,
	opts PDFStampOptions,
	pageNums []int,
) error {
	if opts.SourceDoc == nil {
		return fmt.Errorf("pdf stamp: source doc is nil")
	}
	if len(opts.SourcePages) == 0 {
		return fmt.Errorf("pdf stamp: source has no pages")
	}
	if len(pageNums) == 0 {
		pageNums = make([]int, len(pages))
		for i := range pages {
			pageNums[i] = i + 1
		}
	}

	pageIdx := opts.Page - 1
	if opts.Page == 0 {
		pageIdx = 0
	}
	if pageIdx < 0 || pageIdx >= len(opts.SourcePages) {
		return fmt.Errorf("pdf stamp: page %d out of range (source has %d)",
			opts.Page, len(opts.SourcePages))
	}

	srcPage := opts.SourcePages[pageIdx]
	copier := writer.NewCopier(opts.SourceDoc, alloc)
	formRef, err := pageToFormXObject(ctx, opts.SourceDoc, srcPage, copier, alloc)
	if err != nil {
		return fmt.Errorf("converting source page to form xobject: %w", err)
	}

	formResName := ast.PdfName(fmt.Sprintf("StampFm%d", formRef.ObjectNum))
	extGSName, extGSRef := buildExtGState(alloc, opts.Opacity)

	scale := opts.Scale
	if scale == 0 {
		scale = 1.0
	}

	var ops strings.Builder
	ops.WriteString("q ")
	ops.WriteString(extGStateInvocation(extGSName))
	ops.WriteString(buildTransformMatrix(opts.X, opts.Y, scale, scale, opts.Rotation))
	// string(): PdfName.String already adds the slash, and "//Name Do" names
	// no XObject at all, so the stamp never drew.
	fmt.Fprintf(&ops, "/%s Do Q", string(formResName))

	resources := []stampResource{
		{category: "XObject", name: formResName, ref: formRef},
	}
	if extGSName != "" {
		resources = append(resources, stampResource{
			category: "ExtGState", name: extGSName, ref: extGSRef,
		})
	}

	return applyStampToPages(ctx, doc, alloc, pages, pageNums, resources, ops.String())
}

// pageToFormXObject converts a PDF page into a Form XObject stream in the
// allocator's namespace. The form's /BBox is the source page's /MediaBox,
// /Resources is a deep copy of the source page's resources, and the
// stream content is the concatenated raw bytes of the source page's
// content stream(s). This is the standard PDF §8.10 transformation used
// for n-up, stamping, and watermarking.
func pageToFormXObject(
	ctx context.Context,
	srcDoc *document.Document,
	srcPage *document.Page,
	copier *writer.Copier,
	alloc *writer.ObjAllocator,
) (ast.PdfIndirectRef, error) {
	// BBox: prefer MediaBox, fall back to CropBox.
	bbox := srcPage.MediaBox
	if len(bbox) != 4 && len(srcPage.CropBox) == 4 {
		bbox = srcPage.CropBox
	}
	if len(bbox) != 4 {
		return ast.PdfIndirectRef{}, fmt.Errorf("source page has no usable MediaBox")
	}

	// Copy resources into the destination allocator. Use the resolved
	// resources, which include those the source page inherits from ancestor
	// /Pages nodes (ISO 32000-1 §7.7.3.4): the raw page dict often omits
	// /Resources, and a Form XObject built from an empty resource dict would
	// render blank because the copied content stream references fonts and
	// XObjects it no longer declares. When the page genuinely has no
	// resources, emit an empty dict, which Form XObjects require.
	var resources ast.PdfObject = ast.PdfDict{}
	if srcPage.Resources != nil {
		copied, err := copier.CopyObject(ctx, srcPage.Resources)
		if err != nil {
			return ast.PdfIndirectRef{}, fmt.Errorf("copying source resources: %w", err)
		}
		resources = copied
	}

	contents, err := collectPageContents(ctx, srcDoc, srcPage)
	if err != nil {
		return ast.PdfIndirectRef{}, fmt.Errorf("reading source page contents: %w", err)
	}

	formDict := ast.PdfDict{
		"Type":      ast.PdfName("XObject"),
		"Subtype":   ast.PdfName("Form"),
		"FormType":  ast.PdfInt(1),
		"BBox":      bbox,
		"Resources": resources,
	}
	return alloc.Alloc(ast.NewStream(formDict, contents)), nil
}

// collectPageContents returns the decoded bytes of a page's content stream,
// concatenating multiple streams with a newline separator as ISO 32000-1, 7.8.2
// allows. The caller writes the result into a form XObject that declares no
// /Filter, so the content has to be decoded here: [ast.PdfStream.Bytes] returns
// the body before filter decoding, and handing a FlateDecode page's compressed
// bytes to an unfiltered stream made the stamp draw nothing.
func collectPageContents(ctx context.Context, doc *document.Document, page *document.Page) ([]byte, error) {
	return collectContents(ctx, nil, doc, page.Dict)
}

// collectLatestPageContents is [collectPageContents] for the page as earlier
// stages staged it: the page dict and every content stream resolve through
// alloc first. A stage that rewrites /Contents queues a new stream object the
// source document has never seen, so reading the source here would edit the
// original content and silently discard that stage's work.
func collectLatestPageContents(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, page *document.Page) ([]byte, error) {
	return collectContents(ctx, alloc, doc, latestPageDict(alloc, page))
}

// collectContents decodes the /Contents of pageDict. A nil alloc reads the
// source document only.
//
// # Corpus evidence
//
//   - govdocs: 26 of 1091 documents write /Contents as a reference naming an
//     array, which redaction, excision and stamping all refused until the
//     reference was resolved before the switch.
func collectContents(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pageDict ast.PdfDict) ([]byte, error) {
	obj, ok := pageDict["Contents"]
	if !ok {
		return nil, nil
	}

	// Resolved BEFORE the switch, not inside it. Per ISO 32000-1, 7.7.3.3
	// /Contents is "a stream or an array of streams", and either spelling may
	// itself be written as an indirect reference. Switching on the UNRESOLVED
	// object sent every `/Contents N 0 R` into the stream case, so a reference
	// naming an ARRAY failed as "expected PdfStream, got ast.PdfArray" and
	// redaction, excision and stamping all refused a legal page.
	// tagging.pageContentBytes resolves first and was never affected, which is
	// what made the omission visible.
	resolved, err := resolveAllocFirst(ctx, alloc, doc, obj)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	switch v := resolved.(type) {
	case ast.PdfStream:
		data, err := doc.DecodeStream(ctx, v)
		if err != nil {
			return nil, err
		}
		buf.Write(data)
	case ast.PdfArray:
		for _, elem := range v {
			ref, ok := elem.(ast.PdfIndirectRef)
			if !ok {
				continue
			}
			item, err := resolveAllocFirst(ctx, alloc, doc, ref)
			if err != nil {
				return nil, err
			}
			stream, ok := item.(ast.PdfStream)
			if !ok {
				return nil, fmt.Errorf("/Contents element %d %d R is %T, not a stream", ref.ObjectNum, ref.GenNum, item)
			}
			data, err := doc.DecodeStream(ctx, stream)
			if err != nil {
				return nil, err
			}
			if buf.Len() > 0 {
				buf.WriteByte('\n')
			}
			buf.Write(data)

			// Bounded for the reason given on content.MaxPageContentSize: the
			// per-stream decompression cap says nothing about their sum, so a
			// page naming thousands of streams grows this buffer with the file's
			// object count rather than with anything the page could draw.
			if buf.Len() > content.MaxPageContentSize {
				return nil, fmt.Errorf("page content exceeds %d MB limit", content.MaxPageContentSize>>20)
			}
		}
	default:
		return nil, fmt.Errorf("unsupported /Contents type %T", obj)
	}
	return buf.Bytes(), nil
}

// resolveAllocFirst resolves obj, preferring the object alloc has queued at
// its number. A nil alloc resolves against the source document only.
func resolveAllocFirst(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, obj ast.PdfObject) (ast.PdfObject, error) {
	if ref, ok := obj.(ast.PdfIndirectRef); ok && alloc != nil {
		if pending, ok := alloc.Pending()[ref.ObjectNum]; ok {
			return pending, nil
		}
	}
	resolved, err := document.ResolveObject(ctx, doc, obj)
	if err != nil {
		return nil, fmt.Errorf("resolving object: %w", err)
	}
	return resolved, nil
}
