package mutations

import (
	"context"
	"errors"
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
	"github.com/gotenberg/litho/pkg/internal/intconv"
	"github.com/gotenberg/litho/pkg/internal/typography"
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
	// useForm is an XObject such a dictionary names other than a target: a
	// form the page's content draws, and useFormResources and
	// useFormXObjects its /Resources and their /XObject subdictionary. An
	// image named there is painted by the form as the page draws it, and
	// the check measures those placements too (see [targetImageBoxes]). A
	// form such a form names is one as well, down to the document's form
	// depth budget, which is as deep as the run that measures placements
	// reads: one deeper is a use it does not place.
	useForm
	useFormResources
	useFormXObjects
)

// CheckBlankedImageUses refuses a blanking that would reach past the regions.
//
// Blanking rewrites the image object in place, so every other place that paints
// the same object shows the blanked samples too: a logo repeated on every page,
// or one picture drawn twice on a page, lost the samples under a region that
// covered only one of its placements. Where it is exact it is preferred over a
// copy (see [RedactOptions.PrivateCopies]): the blanking stays where a stage
// that rebuilds a page from the source document still finds it, while a copy
// under a new name has to be checked for at write time (see
// [CopiedImagesPaintedInRegions]).
//
// So an image blanked on the pages in byPage (page number to the regions of
// this redaction) passes only when every place that paints it is one the check
// can measure, and every such place had exactly the same samples covered:
//
//   - Each reference to the object must come through the page tree's own
//     /Resources /XObject, or the /Resources /XObject of a form named there
//     or nested in such a form, down to the document's form depth budget.
//     Any other, a form nested deeper, an annotation appearance, a pattern,
//     a soft mask, reports [ErrImageShared]: that paints it where this check
//     does not look.
//   - Each placement the pages' content streams paint, directly or through
//     the forms they draw, must cover the same samples as every other,
//     including placements on pages this redaction does not touch, which
//     cover none. A placement the machine cannot follow reports
//     [ErrImageShared] as well.
func CheckBlankedImageUses(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	pages []*document.Page,
	byPage map[int][][4]float64,
	blanked []int,
	removed []int,
) error {
	conflicts, err := blankedImageConflicts(ctx, alloc, doc, pages, byPage, blanked, removed)
	if err != nil {
		return err
	}
	for _, objNum := range blanked {
		if reason := conflicts[objNum]; reason != nil {
			return reason
		}
	}
	return nil
}

// SharedBlankedImages returns, ascending, the images of blanked that
// [CheckBlankedImageUses] would refuse to blank in place: those painted, or
// referenced, somewhere the regions do not cover the same samples. A caller
// redacts their covered placements again with each blanked in a copy instead
// (see [RedactOptions.PrivateCopies]), which is exact whatever else paints
// the image, for a placement the page's own content draws.
//
// A failure that names no image, a page whose content the check cannot run
// or a walk nested past its bound, returns every image of blanked: the check
// could not clear any of them. Only a done ctx returns an error.
func SharedBlankedImages(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	pages []*document.Page,
	byPage map[int][][4]float64,
	blanked []int,
	removed []int,
) ([]int, error) {
	conflicts, err := blankedImageConflicts(ctx, alloc, doc, pages, byPage, blanked, removed)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		if !errors.Is(err, ErrImageShared) {
			return nil, err
		}
		doc.Logger.Debug("image use check: blanking every image in a copy", slog.Any("reason", err))
		return slices.Sorted(slices.Values(blanked)), nil
	}
	return slices.Sorted(maps.Keys(conflicts)), nil
}

// blankedImageConflicts returns, for each image of blanked that cannot be
// blanked in place, the [ErrImageShared] saying why. An error is a failure
// that names no single image.
//
// removed names, once per placement, the images of placements the redaction
// removed (see [RedactReport.RemovedImageObjects]). Each counts as a
// placement whose every sample the regions cover: it lay wholly inside them,
// and the edited page no longer paints it for the check to measure.
func blankedImageConflicts(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	pages []*document.Page,
	byPage map[int][][4]float64,
	blanked []int,
	removed []int,
) (map[int]error, error) {
	if len(blanked) == 0 {
		return nil, nil
	}
	targets := make(map[int]bool, len(blanked))
	for _, objNum := range blanked {
		targets[objNum] = true
	}

	conflicts, err := checkImageReferences(ctx, alloc, doc, targets)
	if err != nil {
		return nil, err
	}

	// covers[objNum] holds the covered-sample set of every placement found.
	covers := make(map[int][][]uint64, len(blanked))
	for i, page := range pages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		pageCovers, unknown, err := placementCovers(ctx, alloc, doc, page, byPage[i+1], targets)
		if err != nil {
			return nil, fmt.Errorf("page %d: %w", i+1, err)
		}
		for objNum, sets := range pageCovers {
			covers[objNum] = append(covers[objNum], sets...)
		}
		for objNum, reason := range unknown {
			if conflicts[objNum] == nil {
				conflicts[objNum] = fmt.Errorf("page %d: %w", i+1, reason)
			}
		}
	}
	for _, objNum := range removed {
		if !targets[objNum] {
			continue
		}
		w, h, err := imageSize(ctx, alloc, doc, objNum)
		if err != nil {
			return nil, err
		}
		covers[objNum] = append(covers[objNum], fullCover(w, h))
	}

	for _, objNum := range blanked {
		if conflicts[objNum] != nil {
			continue
		}
		sets := covers[objNum]
		if len(sets) == 0 {
			// The blanking happened, so a page paints the image; finding no
			// placement means the check could not see it.
			conflicts[objNum] = fmt.Errorf("%w: no placement of object %d could be measured", ErrImageShared, objNum)
			continue
		}
		for _, set := range sets[1:] {
			if !slices.Equal(set, sets[0]) {
				conflicts[objNum] = fmt.Errorf("%w: object %d is painted %d times and the regions cover a different part of it at each",
					ErrImageShared, objNum, len(sets))
				break
			}
		}
	}
	return conflicts, nil
}

// checkImageReferences walks the document as a full rewrite would write it,
// alloc first, and returns, for each target referenced from anywhere but a
// page-tree /XObject dictionary or that of a form the pages draw, the
// [ErrImageShared] saying so.
//
// A form is followed into the forms it names down to the document's form
// depth budget, the depth [targetImageBoxes]' run reads to (a form the page
// draws is at depth 1). Each object is walked once per kind and form depth it
// is reached at, arrays written as objects of their own included, so a form
// graph that names a form again, or fans out, is walked a bounded number of
// times. A form reached past the budget by any chain is a use the check does
// not place, even when a shorter chain also reaches it: the run measures the
// placements of the shorter and skips those of the longer, which a viewer
// reading deeper still paints.
func checkImageReferences(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, targets map[int]bool) (map[int]error, error) {
	conflicts := make(map[int]error)
	maxFormDepth := doc.Limits().MaxFormDepth
	type visit struct {
		objNum int
		kind   useKind
		// formDepth is the depth of the form a useForm, useFormResources
		// or useFormXObjects visit lies in, 0 for every other kind.
		formDepth int
	}
	visited := make(map[visit]bool)
	var queue []visit

	var walk func(obj ast.PdfObject, kind useKind, formDepth, depth int) error
	walk = func(obj ast.PdfObject, kind useKind, formDepth, depth int) error {
		if depth > maxImageUseNesting {
			return fmt.Errorf("%w: objects nest deeper than %d levels", ErrImageShared, maxImageUseNesting)
		}
		switch v := obj.(type) {
		case ast.PdfIndirectRef:
			if targets[v.ObjectNum] {
				if kind != useXObjects && kind != useFormXObjects && conflicts[v.ObjectNum] == nil {
					conflicts[v.ObjectNum] = fmt.Errorf("%w: object %d is referenced outside the resources of the pages and the forms they draw",
						ErrImageShared, v.ObjectNum)
				}
				return nil
			}
			// The XObject a page names is a thing of its own, a form whose
			// resources the check follows, and so is one a form names, a
			// level deeper, as far as the run that measures placements
			// reads. Past that it is a use the check does not place.
			switch kind {
			case useXObjects:
				kind, formDepth = useForm, 1
			case useFormXObjects:
				if formDepth < maxFormDepth {
					kind, formDepth = useForm, formDepth+1
				} else {
					kind, formDepth = useOther, 0
				}
			case useOther, useCatalog, usePageTree, useResources, useForm, useFormResources:
				// A reference elsewhere is walked under the kind it was
				// reached by.
			}
			if next := (visit{v.ObjectNum, kind, formDepth}); !visited[next] {
				visited[next] = true
				queue = append(queue, next)
			}
		case ast.PdfArray:
			for _, item := range v {
				if err := walk(item, kind, formDepth, depth+1); err != nil {
					return err
				}
			}
		case ast.PdfDict:
			return walkDictUses(v, kind, func(child ast.PdfObject, childKind useKind) error {
				return walk(child, childKind, childFormDepth(childKind, formDepth), depth+1)
			})
		case ast.PdfStream:
			streamKind := useOther
			if kind == useForm {
				streamKind = useForm
			}
			return walkDictUses(v.Dict, streamKind, func(child ast.PdfObject, childKind useKind) error {
				return walk(child, childKind, childFormDepth(childKind, formDepth), depth+1)
			})
		}
		return nil
	}

	catalogRef, hasRef := CatalogRef(ctx, doc)
	if hasRef {
		if err := walk(catalogRef, useCatalog, 0, 0); err != nil {
			return nil, err
		}
	} else {
		catalog, err := document.ResolveCatalog(ctx, doc)
		if err != nil {
			return nil, fmt.Errorf("resolving catalog: %w", err)
		}
		if err := walk(catalog.Dict, useCatalog, 0, 0); err != nil {
			return nil, err
		}
	}
	if info, ok := doc.Index.Trailer["Info"]; ok {
		if err := walk(info, useOther, 0, 0); err != nil {
			return nil, err
		}
	}

	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
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
		if err := walk(obj, next.kind, next.formDepth, 0); err != nil {
			return nil, err
		}
	}
	return conflicts, nil
}

// childFormDepth is the form depth a value reached under kind carries: the
// enclosing form's for a kind that lies in a form, 0 for any other, so a
// use the check does not place is walked once whatever depth it came from.
func childFormDepth(kind useKind, formDepth int) int {
	switch kind {
	case useForm, useFormResources, useFormXObjects:
		return formDepth
	case useOther, useCatalog, usePageTree, useResources, useXObjects:
	}
	return 0
}

// walkDictUses visits each value of dict under the kind its key leads to.
//
// The /Obj of an object reference is not visited: it names the XObject or
// annotation a structure element holds as a whole (ISO 32000-1, 14.7.4.3),
// which paints nothing from there. An image the page draws is reached
// through the page's resources, where its placements are measured, and one
// blanked in place stays the object the reference names, its parent tree key
// kept ([carryImageEntries]). Counting the reference as a use blanked every
// image a structure element names in a copy, and left the original samples
// in the file under that element.
func walkDictUses(dict ast.PdfDict, kind useKind, visit func(ast.PdfObject, useKind) error) error {
	objRef := dict["Type"] == ast.PdfName("OBJR")
	for _, key := range slices.Sorted(maps.Keys(dict)) {
		if objRef && key == "Obj" {
			continue
		}
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
		case useForm:
			if key == "Resources" {
				childKind = useFormResources
			}
		case useFormResources:
			if key == "XObject" {
				childKind = useFormXObjects
			}
		case useFormXObjects:
			childKind = useFormXObjects
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
//
// A form the run could not parse may paint each target it names (see
// [targetImageBoxes]), and which of the image's samples it shows where is not
// known, only that the form's bounds clip them. One whose bounds lie inside a
// region shows only covered samples: it counts as a placement the regions
// cover whole. One whose bounds meet no region shows only uncovered samples:
// it counts as a placement with no sample covered, the one blanking in place
// would change. Any other covers a part of the image the check cannot tell,
// and the second map names each target it may paint with the
// [ErrImageShared] saying so.
func placementCovers(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	page *document.Page,
	regions [][4]float64,
	targets map[int]bool,
) (map[int][][]uint64, map[int]error, error) {
	page = latestPage(ctx, alloc, doc, page)
	boxes, unmeasured, err := targetImageBoxes(ctx, alloc, doc, page, targets)
	if err != nil {
		return nil, nil, err
	}

	zones := regionZones(regions)
	out := make(map[int][][]uint64)
	var unknown map[int]error
	for _, u := range unmeasured {
		inside, outside := unmeasuredFormCover(u.form, zones)
		for _, objNum := range u.targets {
			if !inside && !outside {
				if unknown == nil {
					unknown = make(map[int]error)
				}
				unknown[objNum] = fmt.Errorf("%w: form %d, whose content does not parse, may paint object %d where a region reaches part of it",
					ErrImageShared, u.form.ObjNum, objNum)
				continue
			}
			w, h, err := imageSize(ctx, alloc, doc, objNum)
			if err != nil {
				return nil, nil, err
			}
			if inside {
				out[objNum] = append(out[objNum], fullCover(w, h))
				continue
			}
			out[objNum] = append(out[objNum], make([]uint64, (w*h+63)/64))
		}
	}

	for _, box := range boxes {
		if !targets[box.ObjNum] {
			continue
		}
		w, h, err := imageSize(ctx, alloc, doc, box.ObjNum)
		if err != nil {
			return nil, nil, err
		}
		set := make([]uint64, (w*h+63)/64)
		if len(zones) > 0 {
			if _, err := forCoveredSamples(box, zones, w, h, func(px, py int) {
				i := py*w + px
				set[i/64] |= 1 << (i % 64)
			}); err != nil {
				return nil, nil, fmt.Errorf("%w: %w", ErrImageShared, err)
			}
		}
		out[box.ObjNum] = append(out[box.ObjNum], set)
	}
	return out, unknown, nil
}

// unmeasuredFormCover reports whether the bounds of a form the run could not
// parse lie inside one of zones, and whether they meet none of them. A form
// with no bounds is neither.
func unmeasuredFormCover(form graphics.UnparsedForm, zones []heuristics.Zone) (inside, outside bool) {
	if form.Unbounded {
		return false, false
	}
	bounds := heuristics.ZoneFromRect([4]float64{form.MinX, form.MinY, form.MaxX, form.MaxY})
	outside = true
	for _, z := range zones {
		if heuristics.ZoneContains(z, bounds) {
			return true, false
		}
		if heuristics.ZonesOverlap(bounds, z) {
			outside = false
		}
	}
	return false, outside
}

// unmeasuredForm is a form the run of a page drew but could not parse, with
// the targets its resources name, ascending: each one it may paint where
// [targetImageBoxes] measured nothing.
type unmeasuredForm struct {
	form    graphics.UnparsedForm
	targets []int
}

// targetImageBoxes runs the content of page, as staged, and returns where it
// paints the images: nil when its resources name no target, so a page that
// cannot paint one is not run at all.
//
// It also returns each form the run drew whose content does not parse and
// whose resources name a target, directly or through the forms they name.
// The run reads no placement from such a form, and a viewer that reads its
// content more leniently than litho may still paint the target from it, so a
// caller treats each as a placement it could not measure: the measurement
// fails closed rather than reading the form as painting nothing.
func targetImageBoxes(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	page *document.Page,
	targets map[int]bool,
) ([]graphics.ImageBox, []unmeasuredForm, error) {
	if !resourcesNameTarget(ctx, alloc, doc, page.Resources, targets) {
		return nil, nil, nil
	}
	data, err := collectLatestPageContents(ctx, alloc, doc, page)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: assembling page content: %w", ErrImageShared, err)
	}
	ops, err := content.ParseStreamData(ctx, data, content.SplitGluedOperators())
	if err != nil {
		return nil, nil, fmt.Errorf("%w: parsing page content: %w", ErrImageShared, err)
	}
	machine := graphics.NewMachine(pageBox(ctx, alloc, doc, page))
	machine.Doc = doc
	machine.Resources = page.Resources
	machine.IgnoreOCVisibility = true
	machine.RecordImageBoxes = true
	// The page as staged, as [RedactPageRegions] reads it.
	machine.Staged = alloc.Pending()
	// Run as the redaction runs it, through the forms the page draws, so a
	// placement a form paints is measured where the redaction blanks it.
	if _, _, err := typography.ExtractElementsAttributed(ctx, doc, machine, page, ops); err != nil {
		return nil, nil, fmt.Errorf("%w: running page content: %w", ErrImageShared, err)
	}
	if n := machine.UnresolvedXObjects(); n > 0 {
		return nil, nil, fmt.Errorf("%w: %d Do operators on the page could not be followed", ErrImageShared, n)
	}
	var unmeasured []unmeasuredForm
	for _, form := range machine.UnparsedForms() {
		named, err := formResourceTargets(ctx, alloc, doc, form.Resources, targets)
		if err != nil {
			return nil, nil, err
		}
		if len(named) == 0 {
			continue
		}
		doc.Logger.Debug("image use check: a form whose content does not parse may paint a target",
			slog.Int("form", form.ObjNum), slog.Any("targets", named))
		unmeasured = append(unmeasured, unmeasuredForm{form: form, targets: named})
	}
	return machine.ImageBoxes(), unmeasured, nil
}

// formResourceTargets returns, ascending, the targets the /XObject
// dictionary of resources names, directly or through the forms it names and
// theirs, as deep as the document's form depth budget: every image a form
// drawn with resources can paint. A nested form without /Resources draws
// from those it inherits (ISO 32000-1, 7.8.3), which are already walked.
//
// Each form is walked once, so a form graph that names a form again, or fans
// out, is walked a bounded number of times. An entry that cannot be read is
// passed over: it names no target, since a target is known by its object
// number before anything is read, and the run of the page refuses a Do it
// cannot follow. Only a done ctx returns an error.
func formResourceTargets(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, resources ast.PdfDict, targets map[int]bool) ([]int, error) {
	found := make(map[int]bool)
	visited := make(map[int]bool)
	maxFormDepth := doc.Limits().MaxFormDepth
	var walk func(resources ast.PdfDict, depth int) error
	walk = func(resources ast.PdfDict, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		obj, ok := resources["XObject"]
		if !ok {
			return nil
		}
		resolved, err := resolveAllocFirst(ctx, alloc, doc, obj)
		if err != nil {
			return nil //nolint:nilerr // An unreadable /XObject names no target (see the doc comment).
		}
		xobjects, ok := resolved.(ast.PdfDict)
		if !ok {
			return nil
		}
		for _, name := range slices.Sorted(maps.Keys(xobjects)) {
			ref, isRef := xobjects[name].(ast.PdfIndirectRef)
			if !isRef {
				continue
			}
			if targets[ref.ObjectNum] {
				found[ref.ObjectNum] = true
				continue
			}
			if visited[ref.ObjectNum] || depth >= maxFormDepth {
				continue
			}
			visited[ref.ObjectNum] = true
			child, err := resolveAllocFirst(ctx, alloc, doc, ref)
			if err != nil {
				continue
			}
			stream, ok := child.(ast.PdfStream)
			if !ok {
				continue
			}
			if subtype, _ := stream.Dict["Subtype"].(ast.PdfName); subtype != "Form" {
				continue
			}
			childRes := resources
			if resObj, ok := stream.Dict["Resources"]; ok {
				if r, err := resolveAllocFirst(ctx, alloc, doc, resObj); err == nil {
					if dict, ok := r.(ast.PdfDict); ok {
						childRes = dict
					}
				}
			}
			if err := walk(childRes, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(resources, 1); err != nil {
		return nil, err
	}
	return slices.Sorted(maps.Keys(found)), nil
}

// CopiedImagesPaintedInRegions reports whether page, as alloc stages it,
// paints one of the images in copied where a region meets it.
//
// A redaction that blanked a placement in a copy (see
// [RedactOptions.PrivateCopies]) left the image itself whole, so a stage that
// rebuilds the page from the source content, which names the image rather
// than the copy, puts every covered sample back. No scan for text finds that.
// A writer asks this of each page such a redaction covered before it writes,
// and applies the redaction again when it answers true. A page whose content
// it cannot run answers true as well: it has not shown the samples are gone.
// A placement the regions meet only over samples that already hold the blank
// is one the redaction leaves as it is, and does not count
// ([copiedPlacementLeftAsIs]).
func CopiedImagesPaintedInRegions(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	page *document.Page,
	regions [][4]float64,
	copied map[int]bool,
) (bool, error) {
	if len(copied) == 0 || len(regions) == 0 {
		return false, nil
	}
	page = latestPage(ctx, alloc, doc, page)
	boxes, unmeasured, err := targetImageBoxes(ctx, alloc, doc, page, copied)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return false, ctxErr
		}
		doc.Logger.Debug("copied image check: page content could not be run", slog.Any("err", err))
		return true, nil
	}
	zones := regionZones(regions)
	// A form the run could not parse paints the image, if at all, somewhere
	// inside its bounds: one whose bounds meet a region has not shown the
	// covered samples are gone.
	for _, u := range unmeasured {
		if _, outside := unmeasuredFormCover(u.form, zones); !outside {
			return true, nil
		}
	}
	for _, box := range boxes {
		if !copied[box.ObjNum] {
			continue
		}
		painted := heuristics.ZoneFromRect([4]float64{box.MinX, box.MinY, box.MaxX, box.MaxY})
		if !slices.ContainsFunc(zones, func(z heuristics.Zone) bool { return heuristics.ZonesOverlap(painted, z) }) {
			continue
		}
		left, err := copiedPlacementLeftAsIs(ctx, alloc, doc, page, box, zones)
		if err != nil {
			return false, err
		}
		if !left {
			return true, nil
		}
	}
	return false, nil
}

// copiedPlacementLeftAsIs reports whether the redaction leaves box, a
// placement of an image blanked in a copy that a region meets, painting the
// image as it is: the region meets it over samples that already hold the
// blank, away from every sample centre ([placementLeftAsIs]). The image a
// copy was made from keeps its samples, so those are the samples it shows
// there, and the region shows the blank as it stands.
//
// Any other answer of the rule, a covered centre or a refusal, is a placement
// the redaction would have blanked or refused, so the image painted there is
// back. A failure to read the image answers false for the same reason: it has
// not shown the samples are blank. Only a done ctx returns an error.
//
// # Corpus evidence
//
//   - pdfjs bug857031 p1-band: the band blanks Im162 (object 11) in a copy at
//     one placement and meets two more 0.012 points deep over samples already
//     black, which Redact left as they were; counting any placement a region
//     meets refused at write time the redaction Redact had found exact.
func copiedPlacementLeftAsIs(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	page *document.Page,
	box graphics.ImageBox,
	zones []heuristics.Zone,
) (bool, error) {
	stream, err := latestImageStream(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: box.ObjNum})
	if err == nil {
		_, err = placementLeftAsIs(ctx, doc, page, box, zones, stream)
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return false, ctxErr
	}
	if errors.Is(err, errSamplesAlreadyBlank) {
		return true, nil
	}
	if err != nil {
		doc.Logger.Debug("copied image check: a placement could not be judged",
			slog.Int("object", box.ObjNum), slog.Any("err", err))
	}
	return false, nil
}

// resourcesNameTarget reports whether the /XObject dictionary of resources
// names any target or any form, which can draw one, so a page that cannot
// paint one is not run at all.
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
		ref, isRef := value.(ast.PdfIndirectRef)
		if !isRef {
			continue
		}
		if targets[ref.ObjectNum] {
			return true
		}
		obj, err := resolveAllocFirst(ctx, alloc, doc, ref)
		if err != nil {
			// The redaction's machine reports it as unresolved if the page
			// draws it, so running the page is what decides.
			return true
		}
		if stream, ok := obj.(ast.PdfStream); ok {
			if subtype, _ := stream.Dict["Subtype"].(ast.PdfName); subtype == "Form" {
				return true
			}
		}
	}
	return false
}

// fullCover returns the covered-sample set of a placement whose every sample
// a region covers, in [placementCovers]' layout: a bitset over w x h.
func fullCover(w, h int) []uint64 {
	n := w * h
	set := make([]uint64, (n+63)/64)
	for i := range set {
		set[i] = ^uint64(0)
	}
	if rem := n % 64; rem != 0 {
		set[len(set)-1] = 1<<rem - 1
	}
	return set
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
	return intconv.Clamp(w), intconv.Clamp(h), nil
}
