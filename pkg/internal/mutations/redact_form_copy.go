package mutations

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// maxFormCopiesPerPage bounds the form copies one page's redaction makes. It
// is an anti-DoS guard, not spec-mandated: a form drawing a form twice at
// each of its levels reaches 2^depth placements within the form-depth budget,
// and each covered one copies its whole chain. Real pages copy a handful; a
// page past the cap refuses with [ErrImageShared]. It matches the raster
// fallback's [maxRasterFormCopies], the other place a redaction copies forms
// per placement.
const maxFormCopiesPerPage = 1024

// formCopy is one form XObject to copy for a single placement, and what the
// copy changes: the Do operators of its stream, by index, repointed at a
// blanked image copy (images) or at a copy of the form they draw (forms).
type formCopy struct {
	// objNum is the form's object number, 0 when its resource entry was
	// direct. It is checked against the form the Do names when the copy is
	// made, so a stream that changed since the walk refuses.
	objNum int
	images map[int]imageCopy
	forms  map[int]*formCopy
}

// imageCopy is one Do of a form repointed at a blanked copy: source is the
// image the Do painted, copy the object holding its blanked samples.
type imageCopy struct {
	source, copy int
}

// formCopies collects, for one page, the placements of shared images inside
// form XObjects that are blanked in a copy: keyed by the page's Do that
// draws the outermost form, one tree of forms to copy per placement.
type formCopies map[int]*formCopy

// add records that the placement box, painted through the forms box.Forms
// names, paints the blanked image copy at objNum. It refuses a chain the
// machine did not record whole.
func (c formCopies) add(box graphics.ImageBox, objNum int) error {
	if len(box.Forms) == 0 || len(box.Forms) != box.FormDepth || box.Forms[0].Op != box.PageOp {
		return fmt.Errorf("%w: image %q is drawn by a form XObject whose Do could not be traced", ErrImageShared, box.Name)
	}
	first := box.Forms[0]
	node := c[first.Op]
	if node == nil {
		node = &formCopy{objNum: first.ObjNum}
		c[first.Op] = node
	}
	for _, call := range box.Forms[1:] {
		if node.forms == nil {
			node.forms = make(map[int]*formCopy)
		}
		next := node.forms[call.Op]
		if next == nil {
			next = &formCopy{objNum: call.ObjNum}
			node.forms[call.Op] = next
		}
		node = next
	}
	if node.images == nil {
		node.images = make(map[int]imageCopy)
	}
	if _, dup := node.images[box.Op]; dup {
		return fmt.Errorf("%w: image %q is recorded twice at one Do of a form", ErrImageShared, box.Name)
	}
	node.images[box.Op] = imageCopy{source: box.ObjNum, copy: objNum}
	return nil
}

// write copies every form c holds and returns, for each Do of the page that
// draws one, the copy to repoint it at (see [blankPrivateCopies]), with the
// number of forms copied.
//
// A form's content is in its own stream (ISO 32000-1, 8.10.1), drawn by
// every Do naming it, so blanking an image for one placement means a copy of
// the form, and of each form above it, for that placement alone: the copy
// names the blanked image under a fresh name and its covered Do is renamed,
// as [blankPrivateCopies] does on the page. Every other placement keeps
// drawing the form as it was.
func (c formCopies) write(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	page *document.Page,
	ops []content.Operation,
) ([]privateCopy, int, error) {
	if len(c) == 0 {
		return nil, 0, nil
	}
	// The merged view the machine resolved the page's names against, read
	// as staged, as it read it.
	pageXObjects, err := resolveDictAllocFirst(ctx, alloc, doc, page.Resources["XObject"])
	if err != nil || pageXObjects == nil {
		return nil, 0, fmt.Errorf("%w: the page's XObject resources do not resolve", ErrImageShared)
	}
	budget := maxFormCopiesPerPage
	out := make([]privateCopy, 0, len(c))
	for _, op := range slices.Sorted(maps.Keys(c)) {
		objNum, err := copyFormForPlacement(ctx, alloc, doc, pageXObjects, page.Resources, ops, op, c[op], 1, &budget)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, privateCopy{op: op, objNum: objNum})
	}
	return out, maxFormCopiesPerPage - budget, nil
}

// copyFormForPlacement copies the form the Do at ops[op] draws, naming it in
// xobjects, with the edits node holds, and returns the copy's object number.
// inherited is the resource dictionary of the stream holding the Do, which a
// form stating no /Resources of its own draws from, as the extraction reads
// it (ISO 32000-1, 7.8.3 lets a form omit them and take the page's).
func copyFormForPlacement(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	xobjects ast.PdfDict,
	inherited ast.PdfDict,
	ops []content.Operation,
	op int,
	node *formCopy,
	depth int,
	budget *int,
) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	// The walk that found the placement reads no deeper than the document's
	// form-depth budget, so this holds the copy to the same bound should the
	// two ever disagree.
	if maxDepth := doc.Limits().WithDefaults().MaxFormDepth; depth > maxDepth {
		return 0, fmt.Errorf("%w: forms nest past the form-depth budget of %d", ErrImageShared, maxDepth)
	}
	*budget--
	if *budget < 0 {
		return 0, fmt.Errorf("%w: more than %d form copies on one page", ErrImageShared, maxFormCopiesPerPage)
	}
	name, ok := doName(ops, op)
	if !ok {
		return 0, fmt.Errorf("%w: operation %d is not the Do that drew the form", ErrImageShared, op)
	}
	entry := xobjects[name]
	if ref, isRef := entry.(ast.PdfIndirectRef); node.objNum != 0 && (!isRef || ref.ObjectNum != node.objNum) {
		return 0, fmt.Errorf("%w: XObject %q no longer names the form the page drew", ErrImageShared, string(name))
	}
	obj, err := resolveAllocFirst(ctx, alloc, doc, entry)
	if err != nil {
		return 0, fmt.Errorf("%w: resolving form %q: %w", ErrImageShared, string(name), err)
	}
	stream, ok := obj.(ast.PdfStream)
	if !ok {
		return 0, fmt.Errorf("%w: XObject %q is no form", ErrImageShared, string(name))
	}
	if subtype, _ := stream.Dict["Subtype"].(ast.PdfName); subtype != "Form" {
		return 0, fmt.Errorf("%w: XObject %q is no form", ErrImageShared, string(name))
	}
	data, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		return 0, fmt.Errorf("%w: decoding form %q: %w", ErrImageShared, string(name), err)
	}
	formOps, ranges, err := content.ParseStreamDataWithOffsets(ctx, data, content.SplitGluedOperators())
	if err != nil {
		return 0, fmt.Errorf("%w: parsing form %q: %w", ErrImageShared, string(name), err)
	}

	resources := inherited
	if resObj, present := stream.Dict["Resources"]; present {
		own, err := resolveDictAllocFirst(ctx, alloc, doc, resObj)
		if err != nil {
			return 0, fmt.Errorf("%w: resolving the resources of form %q: %w", ErrImageShared, string(name), err)
		}
		if own != nil {
			resources = own
		}
	}
	formXObjects, err := resolveDictAllocFirst(ctx, alloc, doc, resources["XObject"])
	if err != nil || formXObjects == nil {
		return 0, fmt.Errorf("%w: the XObject resources of form %q do not resolve", ErrImageShared, string(name))
	}

	newXObjects := maps.Clone(formXObjects)
	taken := make(map[ast.PdfName]bool, len(newXObjects))
	for key := range newXObjects {
		taken[key] = true
	}
	next := 1
	edits := make([]SpanEdit, 0, len(node.images)+len(node.forms))
	repoint := func(i, target int) error {
		if i < 0 || i >= len(ranges) {
			return fmt.Errorf("%w: operation %d of form %q is out of range", ErrImageShared, i, string(name))
		}
		fresh, err := freeCopyName(taken, &next)
		if err != nil {
			return err
		}
		taken[fresh] = true
		newXObjects[fresh] = ast.PdfIndirectRef{ObjectNum: target}
		// ISO 32000-1, 8.8: the operand names an entry of the /XObject
		// subdictionary of the current resources, which inside a form are
		// the form's own, so the copy states the new name there.
		edits = append(edits, SpanEdit{Span: ranges[i], Replacement: []byte("/" + string(fresh) + " Do")})
		return nil
	}
	for _, i := range slices.Sorted(maps.Keys(node.images)) {
		image, ok := doName(formOps, i)
		if !ok {
			return 0, fmt.Errorf("%w: operation %d of form %q is not the Do that painted the image", ErrImageShared, i, string(name))
		}
		if ref, isRef := formXObjects[image].(ast.PdfIndirectRef); !isRef || ref.ObjectNum != node.images[i].source {
			return 0, fmt.Errorf("%w: XObject %q of form %q no longer names the image the form painted",
				ErrImageShared, string(image), string(name))
		}
		if err := repoint(i, node.images[i].copy); err != nil {
			return 0, err
		}
	}
	for _, i := range slices.Sorted(maps.Keys(node.forms)) {
		objNum, err := copyFormForPlacement(ctx, alloc, doc, formXObjects, resources, formOps, i, node.forms[i], depth+1, budget)
		if err != nil {
			return 0, err
		}
		if err := repoint(i, objNum); err != nil {
			return 0, err
		}
	}

	edited, err := EditSpans(data, edits)
	if err != nil {
		return 0, fmt.Errorf("editing form %q: %w", string(name), err)
	}
	newResources := maps.Clone(resources)
	if newResources == nil {
		newResources = ast.PdfDict{}
	}
	newResources["XObject"] = newXObjects
	// The copy keeps every other entry of the form, its /BBox, /Matrix,
	// /Group and /OC among them, so it paints as the form did.
	dict := plainStreamDict(stream.Dict, len(edited))
	dict["Resources"] = newResources
	return alloc.Alloc(ast.NewStream(dict, edited)).ObjectNum, nil
}

// doName returns the name operand of the Do at ops[i].
func doName(ops []content.Operation, i int) (ast.PdfName, bool) {
	if i < 0 || i >= len(ops) || ops[i].Operator != "Do" || len(ops[i].Operands) != 1 {
		return "", false
	}
	name, ok := ops[i].Operands[0].(ast.PdfName)
	return name, ok
}
