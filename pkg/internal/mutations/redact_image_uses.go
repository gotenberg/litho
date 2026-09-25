package mutations

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/heuristics"
)

// maxImageUseNesting bounds how deeply the walk in [CheckBlankedImageUses]
// descends through the direct dictionaries and arrays of one object. It is an
// anti-DoS guard, not spec-mandated: references are followed through a queue,
// so only direct nesting recurses, and real documents nest a handful of levels
// (a page's /Resources /XObject is three). Hitting it counts as a use the check
// could not account for, so it refuses rather than passes.
const maxImageUseNesting = 64

// useKind says what a reference is reached through, which decides whether an
// image it names is painted where [CheckBlankedImageUses] can see it.
type useKind int

const (
	// useOther is anything the check cannot place: an annotation appearance,
	// a pattern, a form's resources, a thumbnail, an ExtGState soft mask.
	useOther useKind = iota
	useCatalog
	// usePageTree covers the page tree nodes and the pages themselves.
	usePageTree
	// useResources is a /Resources dictionary reached from the page tree.
	useResources
	// useXObjects is the /XObject subdictionary of such resources. An image
	// named here is painted by the page's own content, which is where the
	// check measures placements.
	useXObjects
)

// CheckBlankedImageUses refuses a blanking that would reach past the regions.
//
// Blanking rewrites the image object in place, so every other place that paints
// the same object shows the blanked samples too: a logo repeated on every page,
// or one picture drawn twice on a page, lost the samples under a region that
// covered only one of its placements. It also keeps the blanking where a stage
// that rebuilds a page from the source document still finds it, which a copy
// under a new name would not survive.
//
// So an image blanked on the pages in byPage (page number to the regions of
// this redaction) passes only when every place that paints it is one the check
// can measure, and every such place had exactly the same samples covered:
//
//   - Each reference to the object must come through the page tree's own
//     /Resources /XObject. Any other, a form's resources, an annotation
//     appearance, a pattern, a soft mask, reports [ErrImageShared]: that paints
//     it where this check does not look.
//   - Each placement the pages' own content streams paint must cover the same
//     samples as every other, including placements on pages this redaction does
//     not touch, which cover none. A placement the machine cannot follow reports
//     [ErrImageShared] as well.
func CheckBlankedImageUses(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	pages []*document.Page,
	byPage map[int][][4]float64,
	blanked []int,
) error {
	if len(blanked) == 0 {
		return nil
	}
	targets := make(map[int]bool, len(blanked))
	for _, objNum := range blanked {
		targets[objNum] = true
	}

	if err := checkImageReferences(ctx, alloc, doc, targets); err != nil {
		return err
	}

	// covers[objNum] holds the covered-sample set of every placement found.
	covers := make(map[int][][]uint64, len(blanked))
	for i, page := range pages {
		if err := ctx.Err(); err != nil {
			return err
		}
		pageCovers, err := placementCovers(ctx, alloc, doc, page, byPage[i+1], targets)
		if err != nil {
			return fmt.Errorf("page %d: %w", i+1, err)
		}
		for objNum, sets := range pageCovers {
			covers[objNum] = append(covers[objNum], sets...)
		}
	}

	for _, objNum := range blanked {
		sets := covers[objNum]
		if len(sets) == 0 {
			// The blanking happened, so a page paints the image; finding no
			// placement means the check could not see it.
			return fmt.Errorf("%w: no placement of object %d could be measured", ErrImageShared, objNum)
		}
		for _, set := range sets[1:] {
			if !slices.Equal(set, sets[0]) {
				return fmt.Errorf("%w: object %d is painted %d times and the regions cover a different part of it at each",
					ErrImageShared, objNum, len(sets))
			}
		}
	}
	return nil
}

// checkImageReferences walks the document as a full rewrite would write it,
// alloc first, and refuses when a target is referenced from anywhere but a
// page-tree /XObject dictionary.
func checkImageReferences(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, targets map[int]bool) error {
	type visit struct {
		objNum int
		kind   useKind
	}
	visited := make(map[visit]bool)
	var queue []visit

	var walk func(obj ast.PdfObject, kind useKind, depth int) error
	walk = func(obj ast.PdfObject, kind useKind, depth int) error {
		if depth > maxImageUseNesting {
			return fmt.Errorf("%w: objects nest deeper than %d levels", ErrImageShared, maxImageUseNesting)
		}
		switch v := obj.(type) {
		case ast.PdfIndirectRef:
			if targets[v.ObjectNum] {
				if kind != useXObjects {
					return fmt.Errorf("%w: object %d is referenced outside the pages' own resources", ErrImageShared, v.ObjectNum)
				}
				return nil
			}
			// The XObject a page names is a thing of its own: a form's
			// resources that paint the image are a use outside the page's
			// content, so it is walked as one.
			if kind == useXObjects {
				kind = useOther
			}
			if next := (visit{v.ObjectNum, kind}); !visited[next] {
				visited[next] = true
				queue = append(queue, next)
			}
		case ast.PdfArray:
			for _, item := range v {
				if err := walk(item, kind, depth+1); err != nil {
					return err
				}
			}
		case ast.PdfDict:
			return walkDictUses(v, kind, func(child ast.PdfObject, childKind useKind) error {
				return walk(child, childKind, depth+1)
			})
		case ast.PdfStream:
			return walkDictUses(v.Dict, useOther, func(child ast.PdfObject, childKind useKind) error {
				return walk(child, childKind, depth+1)
			})
		}
		return nil
	}

	catalogRef, hasRef := CatalogRef(ctx, doc)
	if hasRef {
		if err := walk(catalogRef, useCatalog, 0); err != nil {
			return err
		}
	} else {
		catalog, err := document.ResolveCatalog(ctx, doc)
		if err != nil {
			return fmt.Errorf("resolving catalog: %w", err)
		}
		if err := walk(catalog.Dict, useCatalog, 0); err != nil {
			return err
		}
	}
	if info, ok := doc.Index.Trailer["Info"]; ok {
		if err := walk(info, useOther, 0); err != nil {
			return err
		}
	}

	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		next := queue[0]
		queue = queue[1:]
		obj, err := resolveAllocFirst(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: next.objNum})
		if err != nil {
			// A reference to an object litho cannot parse paints nothing it
			// can place, and refusing over it would block redaction in every
			// file carrying a dangling reference, which is common.
			doc.Logger.Debug("image use walk: skipping an unreadable object",
				slog.Int("object", next.objNum), slog.Any("err", err))
			continue
		}
		// Pages are walked from the page tree. Reached from anywhere else, a
		// structure element's /Pg, an annotation's /P, a destination, they are
		// only being pointed at, and walking their resources as "other" would
		// refuse every tagged document or outline.
		if next.kind != usePageTree && next.kind != useCatalog && isPageTreeNode(obj) {
			continue
		}
		if err := walk(obj, next.kind, 0); err != nil {
			return err
		}
	}
	return nil
}

// walkDictUses visits each value of dict under the kind its key leads to.
func walkDictUses(dict ast.PdfDict, kind useKind, visit func(ast.PdfObject, useKind) error) error {
	for _, key := range slices.Sorted(maps.Keys(dict)) {
		childKind := useOther
		switch kind {
		case useCatalog:
			if key == "Pages" {
				childKind = usePageTree
			}
		case usePageTree:
			switch key {
			case "Parent":
				continue
			case "Kids":
				childKind = usePageTree
			case "Resources":
				childKind = useResources
			}
		case useResources:
			if key == "XObject" {
				childKind = useXObjects
			}
		case useXObjects:
			// Each value is an XObject the page's content can paint by name.
			childKind = useXObjects
		case useOther:
			// Everything under a use the check cannot place stays one.
		}
		if err := visit(dict[key], childKind); err != nil {
			return err
		}
	}
	return nil
}

// isPageTreeNode reports whether obj is a page or a page tree node.
func isPageTreeNode(obj ast.PdfObject) bool {
	dict, ok := obj.(ast.PdfDict)
	if !ok {
		return false
	}
	typ, _ := dict["Type"].(ast.PdfName)
	return typ == "Page" || typ == "Pages"
}

// placementCovers runs the page's own content and returns, for each target it
// paints, the set of samples the regions cover at each placement, as a bitset
// over the image's width x height.
func placementCovers(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	page *document.Page,
	regions [][4]float64,
	targets map[int]bool,
) (map[int][][]uint64, error) {
	page = latestPage(ctx, alloc, doc, page)
	if !resourcesNameTarget(ctx, alloc, doc, page.Resources, targets) {
		return nil, nil
	}

	data, err := collectLatestPageContents(ctx, alloc, doc, page)
	if err != nil {
		return nil, fmt.Errorf("%w: assembling page content: %w", ErrImageShared, err)
	}
	ops, err := content.ParseStreamData(ctx, data, content.SplitGluedOperators())
	if err != nil {
		return nil, fmt.Errorf("%w: parsing page content: %w", ErrImageShared, err)
	}
	machine := graphics.NewMachine(pageBox(page))
	machine.Doc = doc
	machine.Resources = page.Resources
	machine.IgnoreOCVisibility = true
	machine.RecordImageBoxes = true
	if err := machine.Execute(ctx, ops); err != nil {
		return nil, fmt.Errorf("%w: running page content: %w", ErrImageShared, err)
	}
	if n := machine.UnresolvedXObjects(); n > 0 {
		return nil, fmt.Errorf("%w: %d Do operators on the page could not be followed", ErrImageShared, n)
	}

	zones := make([]heuristics.Zone, 0, len(regions))
	for _, r := range regions {
		zones = append(zones, heuristics.ZoneFromRect(r))
	}

	out := make(map[int][][]uint64)
	for _, box := range machine.ImageBoxes() {
		if !targets[box.ObjNum] {
			continue
		}
		w, h, err := imageSize(ctx, alloc, doc, box.ObjNum)
		if err != nil {
			return nil, err
		}
		set := make([]uint64, (w*h+63)/64)
		if len(zones) > 0 {
			if _, err := forCoveredSamples(box, zones, w, h, func(px, py int) {
				i := py*w + px
				set[i/64] |= 1 << (i % 64)
			}); err != nil {
				return nil, fmt.Errorf("%w: %w", ErrImageShared, err)
			}
		}
		out[box.ObjNum] = append(out[box.ObjNum], set)
	}
	return out, nil
}

// resourcesNameTarget reports whether the /XObject dictionary of resources
// names any target, so a page that cannot paint one is not run at all.
func resourcesNameTarget(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, resources ast.PdfDict, targets map[int]bool) bool {
	obj, ok := resources["XObject"]
	if !ok {
		return false
	}
	resolved, err := resolveAllocFirst(ctx, alloc, doc, obj)
	if err != nil {
		// Unreadable here means the machine cannot follow it either, and
		// running the page is what reports that as a refusal.
		return true
	}
	xobjects, ok := resolved.(ast.PdfDict)
	if !ok {
		return false
	}
	for _, value := range xobjects {
		if ref, isRef := value.(ast.PdfIndirectRef); isRef && targets[ref.ObjectNum] {
			return true
		}
	}
	return false
}

// imageSize returns the /Width and /Height of the image at objNum.
func imageSize(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, objNum int) (int, int, error) {
	stream, err := latestImageStream(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: objNum})
	if err != nil {
		return 0, 0, fmt.Errorf("%w: %w", ErrImageShared, err)
	}
	w, wErr := document.ResolveInt(ctx, doc, stream.Dict["Width"])
	h, hErr := document.ResolveInt(ctx, doc, stream.Dict["Height"])
	if wErr != nil || hErr != nil || w <= 0 || h <= 0 {
		return 0, 0, fmt.Errorf("%w: object %d has no usable /Width and /Height", ErrImageShared, objNum)
	}
	return int(w), int(h), nil
}
