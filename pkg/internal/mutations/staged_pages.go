package mutations

import (
	"context"
	"fmt"
	"io"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// StagedPages returns the document's pages as the edits queued in alloc leave
// its page tree, in order. readerPages, the document's own pages, come back as
// they are when no deletion or insertion staged the tree (see
// [stagedPageKids]), and so does each of them the staged tree keeps. A page an
// insertion copied in exists only in alloc, and comes back built from the
// dictionary alloc holds for it (see [stagedPage]). A nil alloc stages
// nothing, and neither does a nil doc, which has no page tree to stage.
//
// It is what an edit that names no page works on: the Writer's pages at the
// point the edit is queued, pages inserted before it included and pages
// deleted before it left out. Page numbers a caller gives still name
// readerPages.
//
// # Corpus evidence
//
//   - All corpora: 116 files sampled across the seven, each given its first
//     page again as page 2, then a text stamp, an image stamp or a rotation
//     with no page list. Reading every page as the Reader's pages, the three
//     reached 3157, 3160 and 3162 of the 3274 output pages, and they reach
//     3272, 3274 and 3274 now. The two pages left are pdfjs bug1980958.pdf's,
//     10 points square, where the text stamp lands outside the page.
func StagedPages(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, readerPages []*document.Page) []*document.Page {
	if alloc == nil || doc == nil {
		return readerPages
	}
	staged, ok := stagedPageKids(ctx, alloc, doc)
	if !ok {
		return readerPages
	}
	byNum := make(map[int]*document.Page, len(readerPages))
	for _, page := range readerPages {
		if page.Ref.ObjectNum > 0 {
			byNum[page.Ref.ObjectNum] = page
		}
	}
	pages := make([]*document.Page, 0, len(staged))
	for _, ref := range staged {
		if page, held := byNum[ref.ObjectNum]; held {
			pages = append(pages, page)
			continue
		}
		// stagedPageKids resolved every kid to a dictionary already, so a
		// miss here is a kid that stopped resolving since, which leaves no
		// page to edit.
		dict, err := resolveDictAllocFirst(ctx, alloc, doc, ref)
		if err != nil || dict == nil {
			continue
		}
		pages = append(pages, stagedPage(ctx, alloc, doc, ref, dict))
	}
	return pages
}

// stagedPage builds the page an insertion staged at ref from its dictionary.
//
// The insertion materialised the attributes the page inherited in its own
// document onto it (see [copyPageForOutput]), because the flat tree it joins
// has no ancestor to inherit them from (ISO 32000-1, 7.7.3.4). So the page's
// own entries are the whole of them, read alloc-first since the objects they
// name were copied into alloc with it. The crop box defaults to the media box
// (ISO 32000-1, 7.7.3.3, Table 30).
func stagedPage(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, ref ast.PdfIndirectRef, dict ast.PdfDict) *document.Page {
	page := &document.Page{Ref: ref, Dict: dict, UserUnit: 1}
	if res, err := resolveDictAllocFirst(ctx, alloc, doc, dict["Resources"]); err == nil {
		page.Resources = res
	}
	if box, err := resolveArrayAllocFirst(ctx, alloc, doc, dict["MediaBox"]); err == nil && len(box) == 4 {
		page.MediaBox = box
	}
	page.CropBox = page.MediaBox
	if box, err := resolveArrayAllocFirst(ctx, alloc, doc, dict["CropBox"]); err == nil && len(box) == 4 {
		page.CropBox = box
		page.DeclaredCropBox = box
	}
	if rotate, isInt := dict["Rotate"].(ast.PdfInt); isInt {
		page.Rotate = int(rotate)
	}
	if v := document.Float(dict["UserUnit"]); v > 0 {
		page.UserUnit = v
	}
	return page
}

// pagesToEdit returns the pages an edit reads its page numbers against and the
// numbers it edits. Given numbers name readerPages, as every page number a
// caller gives does. No numbers mean every page the document holds as alloc
// stages it (see [StagedPages]), numbered in that order.
func pagesToEdit(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, readerPages []*document.Page, pageNums []int) ([]*document.Page, []int) {
	if len(pageNums) > 0 {
		return readerPages, pageNums
	}
	pages := StagedPages(ctx, alloc, doc, readerPages)
	all := make([]int, len(pages))
	for i := range pages {
		all[i] = i + 1
	}
	return pages, all
}

// WriteStagedPages writes pages, as alloc stages them, to dst as a document of
// their own: a flat page tree holding each page's content, resources and
// boxes, and nothing else. It lets a caller read a page that exists only in
// alloc, one an insertion copied in, with the readers that take a document:
// the objects the page names are numbers doc does not define.
//
// Each page keeps what drawing it needs and no entry that reaches past it
// (/Parent, /Annots, /B, /StructParents), so the copy pulls in no other page.
func WriteStagedPages(ctx context.Context, dst io.Writer, alloc *writer.ObjAllocator, doc *document.Document, pages []*document.Page) error {
	out := writer.NewObjAllocator(0)
	copier := writer.NewCopier(doc, out).WithOverrides(alloc.Pending())

	kids := make(ast.PdfArray, 0, len(pages))
	pageDicts := make([]ast.PdfDict, 0, len(pages))
	for i, page := range pages {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("writing the staged pages: %w", err)
		}
		dict := latestPageDict(alloc, page)
		copied := ast.PdfDict{"Type": ast.PdfName("Page")}
		copyEntry := func(key ast.PdfName, val ast.PdfObject) error {
			c, err := copier.CopyObject(ctx, val)
			if err != nil {
				return fmt.Errorf("copying /%s of staged page %d: %w", key, i+1, err)
			}
			copied[key] = c
			return nil
		}
		// In a fixed order, so the same pages always copy to the same
		// numbers. A nil box is left out: it would reach the copier as an
		// empty array, not as nothing.
		if contents, has := dict["Contents"]; has {
			if err := copyEntry("Contents", contents); err != nil {
				return err
			}
		}
		if resources, has := dict["Resources"]; has {
			if err := copyEntry("Resources", resources); err != nil {
				return err
			}
		} else if page.Resources != nil {
			if err := copyEntry("Resources", page.Resources); err != nil {
				return err
			}
		}
		if page.MediaBox != nil {
			if err := copyEntry("MediaBox", page.MediaBox); err != nil {
				return err
			}
		}
		if page.CropBox != nil {
			if err := copyEntry("CropBox", page.CropBox); err != nil {
				return err
			}
		}
		if page.Rotate != 0 {
			copied["Rotate"] = ast.PdfInt(page.Rotate)
		}
		kids = append(kids, out.Alloc(copied))
		pageDicts = append(pageDicts, copied)
	}

	pagesRef := out.Alloc(ast.PdfDict{
		"Type":  ast.PdfName("Pages"),
		"Kids":  kids,
		"Count": ast.PdfInt(len(kids)),
	})
	for _, d := range pageDicts {
		d["Parent"] = pagesRef
	}
	catalogRef := out.Alloc(ast.PdfDict{"Type": ast.PdfName("Catalog"), "Pages": pagesRef})
	return writer.WriteRewrite(ctx, dst, writer.RewriteConfig{
		Version:   doc.Version,
		Allocator: out,
		Trailer:   ast.PdfDict{"Root": catalogRef},
	})
}
