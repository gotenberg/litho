package remediation

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// maxInlineFileSpecDepth bounds the descent into the direct values of one
// object while looking for file specifications written inline. Anti-DoS guard,
// not spec-mandated: indirect values are not followed, so the walk cannot
// cycle, and only a crafted object nests deeper than a handful of levels. It
// equals compliance's maxInlineFileSpecDepth, the bound of the walk that finds
// the file specifications the embedded-file rules judge, so the repairs reach
// every one that walk reports.
const maxInlineFileSpecDepth = 16

// inlineFileSpecRepair repairs spec, a file specification written inline at
// path in the direct values of the indirect object container. It returns the
// value to write in its place and whether that differs from spec.
type inlineFileSpecRepair func(container int, path string, spec ast.PdfDict) (ast.PdfObject, bool)

// rewriteInlineFileSpecs offers every /Type /Filespec dictionary written inline
// in an indirect object to repair, and writes back each object in which repair
// replaced one.
//
// ISO 32000-1, 7.11.3 does not require a file specification to be an indirect
// object, so one can sit anywhere a value can: an annotation's /FS or /AF
// entry, the annotation itself written inline in a page's /Annots, a reference
// XObject's /Ref, an /OPI dictionary's /F. The walk reaches what compliance
// judges (collectFileSpecs): the direct values of every object, a stream's
// dictionary included, below the object's top, and it spells path as
// compliance's violation contexts do, so a repair keyed on a context finds the
// dictionary it names. An inline dictionary without /Type is no file
// specification to either validator, and is left alone.
//
// Each object is read through the allocator, as [walkNumberedObjects] reads it,
// so an earlier fixer's pending rewrite is the one repaired. Keys are visited in
// sorted order: a repair that allocates objects numbers them in a stable order,
// and the output bytes do not vary between runs.
func rewriteInlineFileSpecs(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, repair inlineFileSpecRepair) error {
	_, err := walkNumberedObjects(ctx, alloc, doc,
		func(objNum int, d ast.PdfDict) (ast.PdfDict, bool) {
			rebuilt, changed := rewriteInlineValue(ctx, doc, d, objNum, "", 0, repair)
			nd, ok := rebuilt.(ast.PdfDict)
			return nd, changed && ok
		},
		func(objNum int, a ast.PdfArray) (ast.PdfArray, bool) {
			rebuilt, changed := rewriteInlineValue(ctx, doc, a, objNum, "", 0, repair)
			na, ok := rebuilt.(ast.PdfArray)
			return na, changed && ok
		})
	if err != nil {
		return fmt.Errorf("repairing inline file specifications: %w", err)
	}
	return nil
}

// rewriteInlineValue returns obj, a value at depth below the top of object
// container, with repair applied to every file specification written inline in
// it, and whether anything changed. The object's own top, at depth 0, is never
// offered: an indirect file specification is repaired where it is defined.
func rewriteInlineValue(ctx context.Context, doc *document.Document, obj ast.PdfObject, container int, path string, depth int, repair inlineFileSpecRepair) (ast.PdfObject, bool) {
	if depth > maxInlineFileSpecDepth {
		return obj, false
	}
	switch v := obj.(type) {
	case ast.PdfArray:
		if ctx.Err() != nil {
			return obj, false
		}
		var rebuilt ast.PdfArray
		for i, item := range v {
			nv, changed := rewriteInlineValue(ctx, doc, item, container, fmt.Sprintf("%s[%d]", path, i), depth+1, repair)
			if !changed {
				continue
			}
			if rebuilt == nil {
				rebuilt = slices.Clone(v)
			}
			rebuilt[i] = nv
		}
		if rebuilt == nil {
			return obj, false
		}
		return rebuilt, true
	case ast.PdfDict:
		if ctx.Err() != nil {
			return obj, false
		}
		var rebuilt ast.PdfDict
		for _, key := range sortedDictKeys(v) {
			nv, changed := rewriteInlineValue(ctx, doc, v[key], container, path+"/"+string(key), depth+1, repair)
			if !changed {
				continue
			}
			if rebuilt == nil {
				rebuilt = maps.Clone(v)
			}
			rebuilt[key] = nv
		}
		current := v
		if rebuilt != nil {
			current = rebuilt
		}
		if typeName, _ := document.ResolveName(ctx, doc, v["Type"]); depth > 0 && typeName == "Filespec" {
			if nv, changed := repair(container, path, current); changed {
				return nv, true
			}
		}
		if rebuilt == nil {
			return obj, false
		}
		return rebuilt, true
	}
	return obj, false
}
