package remediation

import (
	"context"
	"fmt"
	"log/slog"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

func matchImageIssues(v compliance.Violation) bool {
	return descriptionContains(v, "Interpolate") || descriptionContains(v, "OPI key")
}

func fixImageIssues(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("resolving catalog: %w", err)
	}

	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		return nil, fmt.Errorf("resolving pages: %w", err)
	}

	var notes []string
	visited := make(map[int]bool)

	for i, page := range pages {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("fixing image issues on page %d: %w", i+1, err)
		}

		if page.Resources == nil {
			continue
		}

		pageNotes, err := fixImageXObjects(ctx, alloc, doc, page.Resources, visited)
		if err != nil {
			return nil, fmt.Errorf("fixing image issues on page %d: %w", i+1, err)
		}
		notes = append(notes, pageNotes...)
	}

	return notes, nil
}

func fixImageXObjects(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, resources ast.PdfDict, visited map[int]bool) ([]string, error) {
	xobjObj, ok := resources["XObject"]
	if !ok {
		return nil, nil
	}

	xobjDict, err := document.ResolveDict(ctx, doc, xobjObj)
	if err != nil {
		doc.Logger.Debug("resolving XObject dict for image fixes", slog.Any("err", err))
		return nil, nil
	}
	if xobjDict == nil {
		return nil, nil
	}

	var notes []string

	for name, entryObj := range xobjDict {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		ref, ok := entryObj.(ast.PdfIndirectRef)
		if !ok {
			continue
		}

		if visited[ref.ObjectNum] {
			continue
		}
		visited[ref.ObjectNum] = true

		// Resolve from allocator first, then from document.
		var xobjStream ast.PdfStream
		if obj, ok := alloc.Pending()[ref.ObjectNum]; ok {
			if s, ok := obj.(ast.PdfStream); ok {
				xobjStream = s
			}
		}
		if xobjStream.Dict == nil {
			resolved, err := document.ResolveStream(ctx, doc, ref)
			if err != nil {
				continue
			}
			xobjStream = resolved
		}
		if xobjStream.Dict == nil {
			continue
		}

		subtype, _ := xobjStream.Dict["Subtype"].(ast.PdfName)
		if subtype != "Image" {
			continue
		}

		modified := false
		newDict := make(ast.PdfDict, len(xobjStream.Dict))
		maps.Copy(newDict, xobjStream.Dict)

		// Set /Interpolate to false if true (clause 6.2.4).
		if interpObj, ok := newDict["Interpolate"]; ok {
			if b, ok := interpObj.(ast.PdfBool); ok && bool(b) {
				newDict["Interpolate"] = ast.PdfBool(false)
				modified = true
				notes = append(notes, fmt.Sprintf("set /Interpolate to false in image %s", string(name)))
			}
		}

		// Remove /OPI key (clauses 6.2.7.1, 6.2.8.1).
		if _, ok := newDict["OPI"]; ok {
			delete(newDict, "OPI")
			modified = true
			notes = append(notes, fmt.Sprintf("removed /OPI from image %s", string(name)))
		}

		if modified {
			// Carry the original body across. Only the dict changed, and a nil
			// body serialises as /Length 0, which drops the image's pixels
			// (the same trap fixResourcesGraphics guards against). The bytes
			// stay raw so they still match the /Filter kept in newDict.
			data, err := xobjStream.Bytes(ctx)
			if err != nil {
				// The payload is unreadable, so it cannot be carried across.
				// Leaving the object untouched keeps the image intact and the
				// violation visible, which beats writing a valid dict over an
				// empty stream.
				doc.Logger.Debug("remediation: skipping image dict fix, body read failed",
					slog.String("name", string(name)), slog.Any("err", err))
				continue
			}
			alloc.AllocAt(ref.ObjectNum, ast.NewStream(newDict, data))
		}
	}

	return notes, nil
}
