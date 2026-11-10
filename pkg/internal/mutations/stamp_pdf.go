package mutations

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/limits"
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
	// X and Y position the bottom-left corner of the stamp in points. With a
	// Position, they offset the stamp from it instead.
	X, Y float64
	// Position places the source page, as a viewer shows it, against a
	// position of each page (see [stampPlacement]). [PositionNone] places it
	// by X and Y.
	Position StampPosition
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
	if err := checkStampPosition(opts.Position); err != nil {
		return err
	}
	pages, pageNums = pagesToEdit(ctx, alloc, doc, pages, pageNums)
	if err := stampPagesInRange(pages, pageNums); err != nil {
		return err
	}

	pageIdx := opts.Page - 1
	if opts.Page == 0 {
		pageIdx = 0
	}
	if pageIdx < 0 || pageIdx >= len(opts.SourcePages) {
		return fmt.Errorf("pdf stamp: source page %d: %w (source has %d pages)",
			opts.Page, ErrInvalidPageNumber, len(opts.SourcePages))
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
	// The form fills [0 0 W H] under the identity, W and H the source page's
	// width and height as viewed (see [pageToFormXObject]).
	srcBox := normalizedBox(srcPage.ViewedBox())
	srcW, srcH := srcBox[2]-srcBox[0], srcBox[3]-srcBox[1]
	if r := normalizedRotate(srcPage.Rotate); r == 90 || r == 270 {
		srcW, srcH = srcH, srcW
	}
	x, y, place := placedOrigin(opts.Position, opts.X, opts.Y, srcW*scale, srcH*scale, opts.Rotation)
	ops.WriteString(buildTransformMatrix(x, y, scale, scale, opts.Rotation))
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

	return applyStampToPages(ctx, doc, alloc, pages, pageNums, resources, ops.String(), place)
}

// pageToFormXObject converts a PDF page into a Form XObject stream in the
// allocator's namespace, drawn as a viewer shows the page. The form's /BBox is
// the region a viewer shows of the source page, its /Matrix moves that
// region's lower-left corner to the origin and turns it by the page's
// /Rotate, /Resources is a deep copy of the source page's resources, and the
// stream content is the concatenated raw bytes of the source page's content
// stream(s). This is the standard PDF §8.10 transformation used for n-up,
// stamping, and watermarking. Drawn under the identity, the form fills [0 0 W
// H], W and H the page's width and height as a viewer shows it. /UserUnit is
// not applied.
//
// # Corpus evidence
//
//   - pdfjs boundingBox_invalid.pdf: page 1 states /MediaBox [0 0 0 0], and
//     read as the rectangle ISO 19005-2, 6.1.13 measures, the /BBox clipped
//     the stamp away whole.
//   - All corpora: 10 pages in 7 files state a media box no viewer uses, or
//     none, and stamping them was refused, among them govdocs
//     thread_400_400739.pdf, pdfjs issue9105_other.pdf and litho
//     pageboxes_2b.pdf.
func pageToFormXObject(
	ctx context.Context,
	srcDoc *document.Document,
	srcPage *document.Page,
	copier *writer.Copier,
	alloc *writer.ObjAllocator,
) (ast.PdfIndirectRef, error) {
	// The /BBox clips the form (ISO 32000-1, 8.10.2, Table 95), and the form
	// clips where a viewer shows the source page: its crop box, reduced to the
	// media box, the media box where the crop box is none a viewer uses, and
	// US Letter where the media box is none either (see
	// [document.Page.ViewedBox]). Showing the whole media box drew what a
	// viewer crops away. Either is four numbers, never a reference to an
	// object of srcDoc, which would name another object in the target.
	box := normalizedBox(srcPage.ViewedBox())
	bbox := ast.PdfArray{
		ast.PdfReal(box[0]), ast.PdfReal(box[1]),
		ast.PdfReal(box[2]), ast.PdfReal(box[3]),
	}

	// Copy resources into the destination allocator. Use the ones the source
	// page reaches, which it inherits from an ancestor /Pages node when it
	// states none (ISO 32000-1 §7.7.3.4, see [pageResourcesEntry]): the raw page
	// dict often omits /Resources, and a Form XObject built from an empty
	// resource dict would render blank because the copied content stream
	// references fonts and XObjects it no longer declares. The Reader's merged
	// resources would instead let the form reach what the page could not. When
	// the page reaches no resources, emit an empty dict, which Form XObjects
	// require.
	var resources ast.PdfObject = ast.PdfDict{}
	if entry := pageResourcesEntry(srcPage.Dict, srcPage); entry != nil {
		if d, err := document.ResolveDict(ctx, srcDoc, entry); err == nil && d != nil {
			copied, err := copier.CopyObject(ctx, entry)
			if err != nil {
				return ast.PdfIndirectRef{}, fmt.Errorf("copying source resources: %w", err)
			}
			resources = copied
		}
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
		"Matrix":    viewedPageMatrix(box, srcPage.Rotate),
		"Resources": resources,
	}
	return alloc.Alloc(ast.NewStream(formDict, contents)), nil
}

// normalizedBox returns box, four numbers, as [llx lly urx ury]: a rectangle
// may name either pair of opposite corners (ISO 32000-1, 7.9.5).
func normalizedBox(box ast.PdfArray) [4]float64 {
	x0, y0 := document.Float(box[0]), document.Float(box[1])
	x1, y1 := document.Float(box[2]), document.Float(box[3])
	return [4]float64{min(x0, x1), min(y0, y1), max(x0, x1), max(y0, y1)}
}

// viewedPageMatrix returns the form matrix (ISO 32000-1, 8.10.1: form space to
// user space) that draws box as a viewer shows a page with that box and
// /Rotate: its lower-left corner at the origin, turned clockwise by rotate
// (ISO 32000-1, 7.7.3.3, Table 30), so that the form spans [0 0 W H], W and H
// the width and height pdf.Reader.Page reports. A /Rotate that is not a multiple
// of 90 is read as 0, as the reader reads it.
func viewedPageMatrix(box [4]float64, rotate int) ast.PdfArray {
	llx, lly, urx, ury := box[0], box[1], box[2], box[3]
	var m [6]float64
	switch normalizedRotate(rotate) {
	case 90:
		// (x, y) maps to (y - lly, urx - x): H wide, W tall.
		m = [6]float64{0, -1, 1, 0, -lly, urx}
	case 180:
		m = [6]float64{-1, 0, 0, -1, urx, ury}
	case 270:
		m = [6]float64{0, 1, -1, 0, ury, -llx}
	default:
		m = [6]float64{1, 0, 0, 1, -llx, -lly}
	}
	arr := make(ast.PdfArray, len(m))
	for i, v := range m {
		if v == 0 {
			// An entry that came out as -0 writes as 0.
			v = 0
		}
		arr[i] = ast.PdfReal(v)
	}
	return arr
}

// normalizedRotate returns rotate as 0, 90, 180 or 270, the clockwise
// rotations ISO 32000-1, Table 30 allows. A value that is not a multiple of
// 90 is read as 0.
func normalizedRotate(rotate int) int {
	r := rotate % 360
	if r < 0 {
		r += 360
	}
	switch r {
	case 90, 180, 270:
		return r
	}
	return 0
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
			var stream ast.PdfStream
			switch e := elem.(type) {
			case ast.PdfIndirectRef:
				item, err := resolveAllocFirst(ctx, alloc, doc, e)
				if err != nil {
					return nil, err
				}
				s, ok := item.(ast.PdfStream)
				if !ok {
					return nil, fmt.Errorf("/Contents element %d %d R is %T, not a stream", e.ObjectNum, e.GenNum, item)
				}
				stream = s
			case ast.PdfStream:
				// Deliberate leniency: ISO 32000-1, 7.3.8.1 makes every stream
				// an indirect object, but the parser reads one written inline
				// and viewers draw it. Skipping it left what it draws out of
				// the page as a redaction reads it: a region over that content
				// reported success and removed nothing.
				stream = e
			default:
				// A null or any other non-stream draws nothing.
				continue
			}
			data, err := doc.DecodeStream(ctx, stream)
			if err != nil {
				return nil, err
			}
			if buf.Len() > 0 {
				buf.WriteByte('\n')
			}
			buf.Write(data)

			// Bounded for the reason given on limits.Limits.MaxPageContentBytes:
			// the per-stream budget says nothing about their sum, so a page
			// naming thousands of streams grows this buffer with the file's
			// object count rather than with anything the page could draw.
			if limit := doc.Limits().MaxPageContentBytes(); int64(buf.Len()) > limit {
				return nil, fmt.Errorf("%w of %d bytes", limits.ErrPageContentTooLarge, limit)
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

// resolveDictAllocFirst resolves obj to a dictionary as [document.ResolveDict]
// does, preferring the object alloc has queued at its number.
func resolveDictAllocFirst(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, obj ast.PdfObject) (ast.PdfDict, error) {
	if ref, ok := obj.(ast.PdfIndirectRef); ok && alloc != nil {
		if pending, ok := alloc.Pending()[ref.ObjectNum]; ok {
			obj = pending
		}
	}
	return document.ResolveDict(ctx, doc, obj)
}

// resolveArrayAllocFirst resolves obj to an array as [document.ResolveArray]
// does, preferring the object alloc has queued at its number.
func resolveArrayAllocFirst(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, obj ast.PdfObject) (ast.PdfArray, error) {
	if ref, ok := obj.(ast.PdfIndirectRef); ok && alloc != nil {
		if pending, ok := alloc.Pending()[ref.ObjectNum]; ok {
			obj = pending
		}
	}
	return document.ResolveArray(ctx, doc, obj)
}
