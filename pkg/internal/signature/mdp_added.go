package signature

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// maxTemplatePageCompare bounds how many pairs of objects [addedPage] compares,
// over every added page and every template together. It is an anti-DoS guard,
// not spec-mandated: an added page is compared with each template of the
// signed revision until one matches, and each comparison walks the content
// streams, resources and widgets of both pages, so a crafted file holding many
// templates and many added pages would otherwise cost their product. 1<<20
// pairs is the scale of [maxReferenceWalk] and far more than a real
// instantiation needs, since a template page reaches a few dozen objects. A
// page whose comparison runs out is reported as a change.
const maxTemplatePageCompare = 1 << 20

// maxTemplateAncestors bounds the /Parent chain [templateMatch.attributes]
// follows from a template page outside the page tree. It is an anti-DoS
// guard, not spec-mandated, and matches maxPageTreeDepth, the depth
// [document.ResolvePages] walks: the chain climbs the same kind of page tree
// nodes (ISO 32000-1, 7.7.3.4), and a visited set ends a cycle before it.
const maxTemplateAncestors = 256

// instanceEntries are the entries of a page dictionary that a template
// instantiation writes anew: /Parent places the page in the page tree, /Annots
// lists the copies of the template's widgets, which [templateMatch.copies]
// compares one by one, and /TemplateInstantiated names the template (ISO
// 32000-1, 7.7.3.3, Table 30).
var instanceEntries = map[ast.PdfName]bool{"Parent": true, "Annots": true, "TemplateInstantiated": true}

// inheritedEntries are the attributes a page may inherit from the page tree
// (ISO 32000-1, 7.7.3.4, Table 30), which [templateMatch.copies] compares as
// the page draws with them rather than as written.
var inheritedEntries = map[ast.PdfName]bool{"Resources": true, "MediaBox": true, "CropBox": true, "Rotate": true}

// addedPage describes the first page of the current document doc that the
// page tree of the signed revision rev does not hold and that policy does not
// permit adding, or returns "" when there is none.
//
// ISO 32000-1, 12.8.2.2, Table 254 permits no change at P=1. P=2 permits
// filling in forms, instantiating page templates and signing, and P=3 adds
// annotation changes, so the one way either adds a page is a template
// instantiation (12.7.6), which copies a page the signed revision's name
// dictionary names to a new page. The classifier permits a page and an untyped
// object from P=2, and [changedPage] compares only the pages the signed
// revision holds, so a page added with content of its own is found here.
// 12.8.2.2.2 lets a reader compare the signed and current versions, which is
// what this does.
//
// At P=1 every added page is reported. Most updates that add one append a
// page the classifier already refuses, but a cross-reference section alone
// can point the page tree at an earlier definition that lists another page.
// From P=2 an added page must copy a template page of the signed revision
// ([templateMatch.copies]).
//
// Pages are matched by object number. A page written directly in /Kids, which
// 7.7.3.2 forbids, has no number: it counts as added unless the signed page
// tree holds the same dictionary, as written, at the same place among such
// pages.
func addedPage(ctx context.Context, doc, rev *document.Document, policy int) (string, error) {
	signedPages, err := pagesOf(ctx, rev)
	if err != nil {
		return "", fmt.Errorf("reading the signed revision's pages: %w", err)
	}
	currentPages, err := pagesOf(ctx, doc)
	if err != nil {
		return "", fmt.Errorf("reading the current pages: %w", err)
	}
	signed := make(map[int]*document.Page, len(signedPages))
	var signedDirect []ast.PdfDict
	for _, p := range signedPages {
		num := p.Ref.ObjectNum
		if num <= 0 {
			signedDirect = append(signedDirect, p.Dict)
			continue
		}
		if _, dup := signed[num]; !dup {
			signed[num] = p
		}
	}

	m := &templateMatch{doc: doc, rev: rev, signed: signed, policy: policy, budget: pairBudget{left: maxTemplatePageCompare}}
	seen := make(map[int]bool)
	direct := 0
	for i, p := range currentPages {
		if i%100 == 0 {
			if err := ctx.Err(); err != nil {
				return "", err
			}
		}
		num := p.Ref.ObjectNum
		desc := fmt.Sprintf("page %d (object %d)", i+1, num)
		if num > 0 {
			if signed[num] != nil || seen[num] {
				continue
			}
			seen[num] = true
		} else {
			at := direct
			direct++
			if at < len(signedDirect) && sameObject(signedDirect[at], p.Dict) {
				continue
			}
			desc = fmt.Sprintf("page %d, written directly in /Kids,", i+1)
		}

		if policy == 1 {
			return fmt.Sprintf("%s was added, which P=1 does not permit", desc), nil
		}
		if m.instantiates(ctx, p) {
			continue
		}
		if m.budget.exhausted {
			return fmt.Sprintf("%s was added and could not be compared with the signed revision's page templates within %d object pairs, which P=%d does not permit", desc, maxTemplatePageCompare, policy), nil
		}
		return fmt.Sprintf("%s was added and instantiates no page template of the signed revision, which P=%d does not permit", desc, policy), nil
	}
	return "", nil
}

// templateMatch compares the pages the current document doc added with the
// template pages of the signed revision rev, whose page tree holds the pages
// signed maps by object number. Every comparison it makes spends budget.
type templateMatch struct {
	doc, rev *document.Document
	signed   map[int]*document.Page
	policy   int
	budget   pairBudget

	templates     []*templatePage
	templatesRead bool
	attrs         map[*templatePage]pageAttributes
	listed        map[*templatePage]map[int]bool
}

// pageAttributes are the attributes a page draws with that it may inherit
// (ISO 32000-1, 7.7.3.4, Table 30), as [document.Page] holds them.
type pageAttributes struct {
	resources         ast.PdfDict
	mediaBox, cropBox ast.PdfArray
	rotate            int
}

// instantiates reports whether page copies one of the signed revision's
// template pages. Templates that cannot be read instantiate nothing, so the
// page stays a change.
func (m *templateMatch) instantiates(ctx context.Context, page *document.Page) bool {
	if !m.templatesRead {
		m.templatesRead = true
		var err error
		if m.templates, err = signedTemplates(ctx, m.rev); err != nil {
			m.rev.Logger.Debug("reading the signed revision's templates", slog.Any("err", err))
		}
	}
	for _, t := range m.templates {
		if m.copies(ctx, page, t) {
			return true
		}
	}
	return false
}

// copies reports whether page, of the current document, is a faithful copy of
// the template page t, as ISO 32000-1, 12.7.6 has an instantiation make one:
//
//   - each entry of the page dictionary other than [instanceEntries] and
//     [inheritedEntries] is the same object as the template's, following
//     references on both sides ([pairBudget.same]), so the page draws the
//     template's content streams, the same stream objects or copies with the
//     same dictionary and bytes, and runs the template's page actions;
//   - the attributes the page inherits or states are the template's, the
//     resources included;
//   - each annotation the page lists is one the template lists, a widget that
//     copies a widget of the template ([copiedWidget]), or a widget of a
//     signature field, which signing may place on any page. At P=3, which
//     permits annotation creation, it may also be any other annotation.
//
// A copied widget may name its field anew: a spawn that renames its fields
// gives each a new field of its own, and [changedField] judges those fields and
// the actions they run.
func (m *templateMatch) copies(ctx context.Context, page *document.Page, t *templatePage) bool {
	for _, key := range allKeys(t.dict, page.Dict) {
		if instanceEntries[key] || inheritedEntries[key] {
			continue
		}
		if !m.budget.same(ctx, m.doc, page.Dict[key], m.rev, t.dict[key]) {
			return false
		}
	}

	attrs := m.attributes(ctx, t)
	if page.Rotate != attrs.rotate ||
		!m.budget.same(ctx, m.doc, dictObject(page.Resources), m.rev, dictObject(attrs.resources)) ||
		!m.budget.same(ctx, m.doc, arrayObject(page.MediaBox), m.rev, arrayObject(attrs.mediaBox)) ||
		!m.budget.same(ctx, m.doc, arrayObject(page.DeclaredCropBox), m.rev, arrayObject(attrs.cropBox)) {
		return false
	}

	listed := m.listedBy(ctx, t)
	annots, err := document.ResolveArray(ctx, m.doc, page.Dict["Annots"])
	if err != nil {
		// An /Annots that cannot be read lists nothing a reader draws.
		m.doc.Logger.Debug("resolving an added page's /Annots", slog.Any("err", err))
	}
	for _, entry := range annots {
		if listed[refNum(entry)] {
			continue
		}
		dict, err := document.ResolveDict(ctx, m.doc, entry)
		if err != nil || dict == nil || !isAnnotationDict(dict) {
			continue
		}
		if dictName(dict, "Subtype") != "Widget" {
			if m.policy == 3 {
				continue
			}
			return false
		}
		if fieldTypeIn(ctx, m.doc, dict) == "Sig" {
			continue
		}
		if _, ok := copiedWidget(ctx, m.doc, dict, t.widgets); !ok {
			return false
		}
	}
	return true
}

// listedBy returns the object numbers the /Annots of the template page t lists
// by reference, reading them on the first call.
func (m *templateMatch) listedBy(ctx context.Context, t *templatePage) map[int]bool {
	if listed, ok := m.listed[t]; ok {
		return listed
	}
	if m.listed == nil {
		m.listed = make(map[*templatePage]map[int]bool)
	}
	listed := listedAnnotations(ctx, m.rev, t.dict["Annots"], make(map[int]map[int]bool))
	m.listed[t] = listed
	return listed
}

// attributes returns the attributes the template page t draws with, reading
// them on the first call. A template the signed page tree holds has them as
// [document.ResolvePages] read them. One outside the tree states them or
// inherits them through its /Parent chain (ISO 32000-1, 7.7.3.4), the nearest
// node that states one giving it, up to [maxTemplateAncestors] nodes.
func (m *templateMatch) attributes(ctx context.Context, t *templatePage) pageAttributes {
	if attrs, ok := m.attrs[t]; ok {
		return attrs
	}
	if m.attrs == nil {
		m.attrs = make(map[*templatePage]pageAttributes)
	}
	var attrs pageAttributes
	if p := m.signed[t.num]; t.num > 0 && p != nil {
		attrs = pageAttributes{resources: p.Resources, mediaBox: p.MediaBox, cropBox: p.DeclaredCropBox, rotate: p.Rotate}
		m.attrs[t] = attrs
		return attrs
	}

	found := make(map[ast.PdfName]bool, len(inheritedEntries))
	seen := map[int]bool{t.num: true}
	for depth, node := 0, t.dict; depth < maxTemplateAncestors; depth++ {
		for key := range inheritedEntries {
			value, ok := node[key]
			if !ok || found[key] {
				continue
			}
			found[key] = true
			switch key {
			case "Resources":
				attrs.resources = m.resolvedDict(ctx, value)
			case "MediaBox":
				attrs.mediaBox = m.resolvedRectangle(ctx, value)
			case "CropBox":
				attrs.cropBox = m.resolvedRectangle(ctx, value)
			case "Rotate":
				if resolved, err := document.ResolveObject(ctx, m.rev, value); err == nil {
					if rotate, ok := resolved.(ast.PdfInt); ok {
						attrs.rotate = int(rotate)
					}
				}
			}
		}
		// Page tree nodes chain through /Parent as fields do, so the same
		// hop, with its visited set, climbs them.
		var ok bool
		if _, node, ok = document.ResolveFieldParent(ctx, m.rev, node, seen); !ok {
			break
		}
	}
	m.attrs[t] = attrs
	return attrs
}

// resolvedDict resolves a /Resources value of the signed revision, or returns
// nil when it is no dictionary, which draws with no resource.
func (m *templateMatch) resolvedDict(ctx context.Context, value ast.PdfObject) ast.PdfDict {
	dict, err := document.ResolveDict(ctx, m.rev, value)
	if err != nil {
		m.rev.Logger.Debug("resolving a template's /Resources", slog.Any("err", err))
		return nil
	}
	return dict
}

// resolvedRectangle resolves a box of the signed revision as
// [document.ResolvePages] does, or returns nil when it is no array.
func (m *templateMatch) resolvedRectangle(ctx context.Context, value ast.PdfObject) ast.PdfArray {
	arr, err := document.ResolveArray(ctx, m.rev, value)
	if err != nil || arr == nil {
		m.rev.Logger.Debug("resolving a template's box", slog.Any("err", err))
		return nil
	}
	return document.ResolveRectangle(ctx, m.rev, arr)
}

// dictObject returns dict as an object, or nil when dict is nil, so that an
// absent dictionary compares as absent.
func dictObject(dict ast.PdfDict) ast.PdfObject {
	if dict == nil {
		return nil
	}
	return dict
}

// arrayObject returns arr as an object, or nil when arr is nil, so that an
// absent array compares as absent.
func arrayObject(arr ast.PdfArray) ast.PdfObject {
	if arr == nil {
		return nil
	}
	return arr
}
