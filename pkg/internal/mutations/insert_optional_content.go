package mutations

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// insertOptionalContent adds the optional content groups the pages a page
// insertion copied from srcDoc draw to the /OCProperties of doc, the target,
// shown or hidden as the source's default configuration draws them.
//
// ISO 32000-1, 8.11.4.3 asks /OCProperties /OCGs to list every optional
// content group of the document, and 8.11.2.3 draws a group's content as the
// configuration says. The inserted groups joined neither, so a layer the
// source hid was drawn, as it was in a merge before [mergeOptionalContent].
// A group copier copied is one the inserted content draws, and only those
// join.
//
// Each group joins the target's configurations, the default one and every one
// /Configs lists (Table 100): the end of /Order, which ISO 19005-2, 6.9 asks
// to name every group, when the configuration has one, and /ON or /OFF as the
// configuration's base state needs to draw it as the source did (Table 101). A
// target with no /OCProperties gets one naming the inserted groups, its
// default configuration named as ISO 14289-1, 7.10 and ISO 19005-2, 6.9 ask:
// the source's name, and [mergedConfigName] when it has none.
//
// # Corpus evidence
//
//   - All corpora: copying page 1 of each of 161 files with optional content
//     after its last page, as mutate-pages does, left 31 outputs with a page
//     drawing a group /OCProperties does not list, and none now.
func insertOptionalContent(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, catalogRef ast.PdfIndirectRef, srcDoc *document.Document, copier *writer.Copier) error {
	srcCatalog, err := document.ResolveCatalog(ctx, srcDoc)
	if err != nil || srcCatalog == nil {
		return nil
	}
	srcOCP, err := document.ResolveDict(ctx, srcDoc, srcCatalog.Dict["OCProperties"])
	if err != nil || srcOCP == nil {
		return nil
	}
	groups, err := document.ResolveArray(ctx, srcDoc, srcOCP["OCGs"])
	if err != nil {
		return nil
	}
	hidden := hiddenOptionalContent(ctx, srcDoc, srcOCP, groups)
	var added, shown, off ast.PdfArray
	seen := make(map[int]bool)
	for _, group := range groups {
		ref, isRef := group.(ast.PdfIndirectRef)
		if !isRef {
			continue
		}
		dst, copied := copier.Mapping()[ref.ObjectNum]
		if !copied || seen[dst] {
			continue
		}
		seen[dst] = true
		out := ast.PdfIndirectRef{ObjectNum: dst}
		added = append(added, out)
		if hidden[ref.ObjectNum] {
			off = append(off, out)
		} else {
			shown = append(shown, out)
		}
	}
	if len(added) == 0 {
		return nil
	}

	catalog, err := LiveCatalogDict(ctx, alloc, doc, catalogRef)
	if err != nil {
		return fmt.Errorf("reading the catalog: %w", err)
	}
	ocpObj, has := catalog["OCProperties"]
	ocp, err := resolveDictAllocFirst(ctx, alloc, doc, ocpObj)
	if !has || err != nil || ocp == nil {
		name := optionalContentConfigName(ctx, srcDoc, srcOCP["D"])
		if name == "" {
			name = document.EncodePdfString(mergedConfigName)
		}
		config := ast.PdfDict{"Name": name, "Order": added}
		if len(off) > 0 {
			config["OFF"] = off
		}
		updated := maps.Clone(catalog)
		updated["OCProperties"] = ast.PdfDict{"OCGs": added, "D": config}
		alloc.AllocAt(catalogRef.ObjectNum, updated)
		return nil
	}

	updatedOCP := maps.Clone(ocp)
	updatedOCP["OCGs"] = append(arrayCopy(ctx, alloc, doc, ocp["OCGs"]), added...)
	// A configuration object /D and /Configs both name joins once.
	joined := make(map[int]bool)
	join := func(obj ast.PdfObject) (ast.PdfObject, bool) {
		if ref, isRef := obj.(ast.PdfIndirectRef); isRef {
			if joined[ref.ObjectNum] {
				return obj, false
			}
			joined[ref.ObjectNum] = true
		}
		return joinOptionalContentConfig(ctx, alloc, doc, obj, added, shown, off)
	}
	if d, isDict := join(ocp["D"]); isDict {
		updatedOCP["D"] = d
	}
	if configs, err := resolveArrayAllocFirst(ctx, alloc, doc, ocp["Configs"]); err == nil && configs != nil {
		updatedConfigs := slices.Clone(configs)
		for i, config := range configs {
			if d, isDict := join(config); isDict {
				updatedConfigs[i] = d
			}
		}
		if ref, isRef := ocp["Configs"].(ast.PdfIndirectRef); isRef {
			alloc.AllocAt(ref.ObjectNum, updatedConfigs)
		} else {
			updatedOCP["Configs"] = updatedConfigs
		}
	}
	if ref, isRef := ocpObj.(ast.PdfIndirectRef); isRef {
		alloc.AllocAt(ref.ObjectNum, updatedOCP)
		return nil
	}
	updated := maps.Clone(catalog)
	updated["OCProperties"] = updatedOCP
	alloc.AllocAt(catalogRef.ObjectNum, updated)
	return nil
}

// joinOptionalContentConfig adds the groups added to the optional content
// configuration dictionary at obj, see [insertOptionalContent]: shown drawn and
// off hidden under its base state. A configuration written as an object of its
// own is rewritten in place and obj returned; one written directly is
// returned rebuilt for its holder to store. isDict is false when obj is no
// dictionary, which is left as it is.
func joinOptionalContentConfig(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, obj ast.PdfObject, added, shown, off ast.PdfArray) (_ ast.PdfObject, isDict bool) {
	config, err := resolveDictAllocFirst(ctx, alloc, doc, obj)
	if err != nil || config == nil {
		return nil, false
	}
	updated := maps.Clone(config)
	if _, ordered := config["Order"]; ordered {
		updated["Order"] = append(arrayCopy(ctx, alloc, doc, config["Order"]), added...)
	}
	if base, _ := config["BaseState"].(ast.PdfName); base == "OFF" {
		if len(shown) > 0 {
			updated["ON"] = append(arrayCopy(ctx, alloc, doc, config["ON"]), shown...)
		}
	} else if len(off) > 0 {
		updated["OFF"] = append(arrayCopy(ctx, alloc, doc, config["OFF"]), off...)
	}
	if ref, isRef := obj.(ast.PdfIndirectRef); isRef {
		alloc.AllocAt(ref.ObjectNum, updated)
		return obj, true
	}
	return updated, true
}

// arrayCopy returns a copy of the array obj resolves to, read from alloc
// first, and nil when it resolves to none.
func arrayCopy(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, obj ast.PdfObject) ast.PdfArray {
	arr, err := resolveArrayAllocFirst(ctx, alloc, doc, obj)
	if err != nil {
		return nil
	}
	return slices.Clone(arr)
}
