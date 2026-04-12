package mutations

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
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
	fmt.Fprintf(&ops, "/%s Do Q", formResName)

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

	// Copy resources into the destination allocator. When the source
	// page has no explicit /Resources, emit an empty dict. Form XObjects
	// require a /Resources entry.
	var resources ast.PdfObject = ast.PdfDict{}
	if srcRes, ok := srcPage.Dict["Resources"]; ok {
		copied, err := copier.CopyObject(ctx, srcRes)
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

// collectPageContents returns the raw bytes of a page's content stream,
// concatenating multiple streams with a newline separator as the PDF spec
// allows. Filters are preserved by using the stream's decoded Bytes method
// and then writing without a /Filter entry, which is always valid.
func collectPageContents(ctx context.Context, doc *document.Document, page *document.Page) ([]byte, error) {
	obj, ok := page.Dict["Contents"]
	if !ok {
		return nil, nil
	}

	var buf bytes.Buffer
	switch v := obj.(type) {
	case ast.PdfIndirectRef:
		stream, err := document.ResolveStream(ctx, doc, v)
		if err != nil {
			return nil, err
		}
		data, err := stream.Bytes()
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
			stream, err := document.ResolveStream(ctx, doc, ref)
			if err != nil {
				return nil, err
			}
			data, err := stream.Bytes()
			if err != nil {
				return nil, err
			}
			if buf.Len() > 0 {
				buf.WriteByte('\n')
			}
			buf.Write(data)
		}
	default:
		return nil, fmt.Errorf("unsupported /Contents type %T", obj)
	}
	return buf.Bytes(), nil
}
