package mutations

import (
	"context"
	"fmt"
	"maps"
	"strconv"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// privateCopyNamePrefix starts the resource name a blanked copy of an image
// joins the page's /XObject resources under. A counter follows it, and the
// first name the resources do not hold is taken, so the name is the same on
// every run over the same page.
const privateCopyNamePrefix = "LithoRedacted"

// maxPrivateCopyNames bounds the search for a free resource name. It is an
// anti-DoS guard, not spec-mandated: a page's /XObject dictionary holds a
// handful of names, and one holding this many with the prefix above is
// crafted. Running out refuses with [ErrImageShared], the refusal a private
// copy lifts.
const maxPrivateCopyNames = 1 << 16

// privateCopy is one placement whose image was blanked in a copy: op is the
// index of its Do in the page's operations, objNum the copy.
type privateCopy struct {
	op     int
	objNum int
}

// blankPrivateCopies names each copy in the page's resources and returns the
// edits repointing each covered Do at its copy.
//
// The page states its resources afterwards, those it reached plus the copies
// (see [mergeStampResources]): a dictionary the page shares with others, or
// inherits, is not edited, so no other page gains a name. ops and ranges are
// the page's operations and their spans, which the edits address.
//
// The Do is rewritten whole, its operand renamed: ISO 32000-1, 8.8 makes the
// operand a name in the /XObject subdictionary of the current resources, and
// at a Do the page's own content runs that is the page's.
func blankPrivateCopies(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	page *document.Page,
	ops []content.Operation,
	ranges []content.ByteRange,
	copies []privateCopy,
) ([]SpanEdit, error) {
	if len(copies) == 0 {
		return nil, nil
	}
	if page.Ref.ObjectNum == 0 {
		return nil, ErrPageNotAddressable
	}

	baseDict := latestPageDict(alloc, page)
	taken := make(map[ast.PdfName]bool)
	if entry := pageResourcesEntry(baseDict, page); entry != nil {
		if res, err := resolveDictAllocFirst(ctx, alloc, doc, entry); err == nil && res != nil {
			if xobjects, err := resolveDictAllocFirst(ctx, alloc, doc, res["XObject"]); err == nil {
				for name := range xobjects {
					taken[name] = true
				}
			}
		}
	}
	// The merged view the machine resolved names against can hold a name the
	// stated dictionary does not, and the copy must not shadow it.
	if xobjects, err := document.ResolveDict(ctx, doc, page.Resources["XObject"]); err == nil {
		for name := range xobjects {
			taken[name] = true
		}
	}

	additions := make([]stampResource, 0, len(copies))
	edits := make([]SpanEdit, 0, len(copies))
	next := 1
	for _, c := range copies {
		if c.op < 0 || c.op >= len(ops) || c.op >= len(ranges) || ops[c.op].Operator != "Do" {
			return nil, fmt.Errorf("%w: operation %d is not the Do that painted the image", ErrImageShared, c.op)
		}
		name, err := freeCopyName(taken, &next)
		if err != nil {
			return nil, err
		}
		taken[name] = true
		additions = append(additions, stampResource{
			category: "XObject",
			name:     name,
			ref:      ast.PdfIndirectRef{ObjectNum: c.objNum},
		})
		edits = append(edits, SpanEdit{Span: ranges[c.op], Replacement: []byte("/" + string(name) + " Do")})
	}

	newDict := make(ast.PdfDict, len(baseDict)+1)
	maps.Copy(newDict, baseDict)
	newDict["Resources"] = mergeStampResources(ctx, doc, alloc, baseDict, page, additions)
	if !alloc.AllocAt(page.Ref.ObjectNum, newDict) {
		return nil, ErrPageNotAddressable
	}
	return edits, nil
}

// freeCopyName returns the first name of the form [privateCopyNamePrefix]
// plus a counter, starting at *next, that taken does not hold, and moves
// *next past it.
func freeCopyName(taken map[ast.PdfName]bool, next *int) (ast.PdfName, error) {
	for ; *next <= maxPrivateCopyNames; *next++ {
		name := ast.PdfName(privateCopyNamePrefix + strconv.Itoa(*next))
		if !taken[name] {
			*next++
			return name, nil
		}
	}
	return "", fmt.Errorf("%w: no free resource name for a blanked copy", ErrImageShared)
}
