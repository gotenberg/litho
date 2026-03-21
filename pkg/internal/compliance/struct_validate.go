//nolint:unused // Structure tree checks pending rule set wiring — need FP refinement.
package compliance

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// structVisitor is called for each structure element during tree traversal.
type structVisitor func(ctx context.Context, doc *document.Document, elem ast.PdfDict) []error

// walkStructTree performs a depth-limited recursive walk of the structure tree,
// calling the visitor for each structure element dict. It uses a visited set
// of indirect reference numbers for cycle detection.
func walkStructTree(ctx context.Context, doc *document.Document, catalog ast.PdfDict, visitor structVisitor) []error {
	strObj, ok := catalog["StructTreeRoot"]
	if !ok {
		return nil
	}
	strDict, err := document.ResolveDict(ctx, doc, strObj)
	if err != nil {
		doc.Logger.Debug("resolving StructTreeRoot dict", slog.Any("err", err))
	}
	if strDict == nil {
		return nil
	}
	kObj, ok := strDict["K"]
	if !ok {
		return nil
	}
	kRes, err := document.ResolveObject(ctx, doc, kObj)
	if err != nil {
		doc.Logger.Debug("resolving StructTreeRoot children", slog.Any("err", err))
	}

	var errs []error
	walkStructElem(ctx, doc, kRes, visitor, 0, &errs)
	return errs
}

// maxStructDepth prevents infinite recursion in malformed structure trees.
const maxStructDepth = 200

func walkStructElem(ctx context.Context, doc *document.Document, obj ast.PdfObject, visitor structVisitor, depth int, errs *[]error) {
	if depth > maxStructDepth {
		return
	}
	if ctx.Err() != nil {
		return
	}

	switch v := obj.(type) {
	case ast.PdfArray:
		for _, kid := range v {
			kidRes, err := document.ResolveObject(ctx, doc, kid)
			if err != nil {
				doc.Logger.Debug("resolving struct tree array child", slog.Any("err", err))
			}
			walkStructElem(ctx, doc, kidRes, visitor, depth+1, errs)
		}
	case ast.PdfDict:
		// Visit this element.
		*errs = append(*errs, visitor(ctx, doc, v)...)

		// Recurse into children.
		kObj, ok := v["K"]
		if !ok {
			return
		}
		kRes, err := document.ResolveObject(ctx, doc, kObj)
		if err != nil {
			doc.Logger.Debug("resolving struct element children", slog.Any("err", err))
		}
		walkStructElem(ctx, doc, kRes, visitor, depth+1, errs)
	}
}

// getStructType returns the /S name of a structure element.
func getStructType(ctx context.Context, doc *document.Document, elem ast.PdfDict) ast.PdfName {
	sObj, ok := elem["S"]
	if !ok {
		return ""
	}
	sName, err := document.ResolveName(ctx, doc, sObj)
	if err != nil {
		doc.Logger.Debug("resolving struct element type name", slog.Any("err", err))
	}
	return sName
}

// getStructChildren returns the resolved /K children of a structure element.
func getStructChildren(ctx context.Context, doc *document.Document, elem ast.PdfDict) []ast.PdfDict {
	kObj, ok := elem["K"]
	if !ok {
		return nil
	}
	kRes, err := document.ResolveObject(ctx, doc, kObj)
	if err != nil {
		doc.Logger.Debug("resolving struct element K object", slog.Any("err", err))
	}
	var children []ast.PdfDict
	switch k := kRes.(type) {
	case ast.PdfArray:
		for _, item := range k {
			d, err := document.ResolveDict(ctx, doc, item)
			if err != nil {
				doc.Logger.Debug("resolving struct child dict", slog.Any("err", err))
			}
			if d != nil {
				children = append(children, d)
			}
		}
	case ast.PdfDict:
		children = append(children, k)
	}
	return children
}

// validTableChildren are the allowed direct children of a Table element.
var validTableChildren = map[ast.PdfName]bool{
	"TR": true, "THead": true, "TBody": true, "TFoot": true, "Caption": true,
}

// checkTableStructure validates that Table structure elements have only
// valid children (TR, THead, TBody, TFoot, Caption) and that rows have
// consistent column counts.
func checkTableStructure(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	return walkStructTree(ctx, doc, catalog, func(ctx context.Context, doc *document.Document, elem ast.PdfDict) []error {
		if getStructType(ctx, doc, elem) != "Table" {
			return nil
		}
		children := getStructChildren(ctx, doc, elem)
		if len(children) == 0 {
			return nil
		}

		var errs []error
		var columnCounts []int

		for _, child := range children {
			childType := getStructType(ctx, doc, child)
			if childType == "" {
				continue // Non-struct element child (e.g., MCID).
			}
			if !validTableChildren[childType] {
				errs = append(errs, fmt.Errorf("%w: /%s child of Table",
					models.ErrTableStructureInvalid, childType))
			}

			// Count columns in TR elements.
			if childType == "TR" {
				cols := countRowColumns(ctx, doc, child)
				if cols > 0 {
					columnCounts = append(columnCounts, cols)
				}
			}
			// Count columns in THead/TBody/TFoot containers.
			if childType == "THead" || childType == "TBody" || childType == "TFoot" {
				sectionChildren := getStructChildren(ctx, doc, child)
				for _, row := range sectionChildren {
					if getStructType(ctx, doc, row) == "TR" {
						cols := countRowColumns(ctx, doc, row)
						if cols > 0 {
							columnCounts = append(columnCounts, cols)
						}
					}
				}
			}
		}

		// Check column count consistency.
		if len(columnCounts) > 1 {
			expected := columnCounts[0]
			for _, count := range columnCounts[1:] {
				if count != expected {
					errs = append(errs, fmt.Errorf("%w: expected %d, got %d",
						models.ErrTableColumnCountMismatch, expected, count))
					break
				}
			}
		}

		return errs
	})
}

// countRowColumns counts the number of TH/TD cells in a TR element.
func countRowColumns(ctx context.Context, doc *document.Document, tr ast.PdfDict) int {
	children := getStructChildren(ctx, doc, tr)
	count := 0
	for _, child := range children {
		ct := getStructType(ctx, doc, child)
		if ct == "TH" || ct == "TD" {
			count++
		}
	}
	return count
}

// checkListStructure validates that L (List) structure elements contain
// only LI children (with optional Caption as first child).
func checkListStructure(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	return walkStructTree(ctx, doc, catalog, func(ctx context.Context, doc *document.Document, elem ast.PdfDict) []error {
		if getStructType(ctx, doc, elem) != "L" {
			return nil
		}
		children := getStructChildren(ctx, doc, elem)
		for i, child := range children {
			childType := getStructType(ctx, doc, child)
			if childType == "" {
				continue
			}
			// Caption is allowed only as the first child.
			if childType == "Caption" && i == 0 {
				continue
			}
			if childType != "LI" {
				return []error{fmt.Errorf("%w: /%s child of L",
					models.ErrListStructureInvalid, childType)}
			}
		}
		return nil
	})
}

// checkHeadingHierarchy validates that H1-H6 headings do not skip levels
// in the structure tree.
func checkHeadingHierarchy(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	strObj, ok := catalog["StructTreeRoot"]
	if !ok {
		return nil
	}
	strDict, err := document.ResolveDict(ctx, doc, strObj)
	if err != nil {
		doc.Logger.Debug("resolving StructTreeRoot for heading check", slog.Any("err", err))
	}
	if strDict == nil {
		return nil
	}
	kObj, ok := strDict["K"]
	if !ok {
		return nil
	}
	kRes, err := document.ResolveObject(ctx, doc, kObj)
	if err != nil {
		doc.Logger.Debug("resolving StructTreeRoot children for heading check", slog.Any("err", err))
	}

	// Collect all heading levels in document order.
	var levels []int
	collectHeadingLevels(ctx, doc, kRes, &levels, 0)

	// Check for skipped levels.
	var errs []error
	for i := 1; i < len(levels); i++ {
		if levels[i]-levels[i-1] > 1 {
			errs = append(errs, fmt.Errorf("%w: H%d followed by H%d",
				models.ErrHeadingSkipped, levels[i-1], levels[i]))
		}
	}
	return errs
}

func collectHeadingLevels(ctx context.Context, doc *document.Document, obj ast.PdfObject, levels *[]int, depth int) {
	if depth > maxStructDepth {
		return
	}
	switch v := obj.(type) {
	case ast.PdfArray:
		for _, kid := range v {
			kidRes, err := document.ResolveObject(ctx, doc, kid)
			if err != nil {
				doc.Logger.Debug("resolving heading array child", slog.Any("err", err))
			}
			collectHeadingLevels(ctx, doc, kidRes, levels, depth+1)
		}
	case ast.PdfDict:
		st := getStructType(ctx, doc, v)
		level := headingLevel(st)
		if level > 0 {
			*levels = append(*levels, level)
		}
		kObj, ok := v["K"]
		if !ok {
			return
		}
		kRes, err := document.ResolveObject(ctx, doc, kObj)
		if err != nil {
			doc.Logger.Debug("resolving heading element children", slog.Any("err", err))
		}
		collectHeadingLevels(ctx, doc, kRes, levels, depth+1)
	}
}

// headingLevel returns the numeric level for H1-H6 tags, or 0 if not a heading.
func headingLevel(tag ast.PdfName) int {
	switch tag {
	case "H1":
		return 1
	case "H2":
		return 2
	case "H3":
		return 3
	case "H4":
		return 4
	case "H5":
		return 5
	case "H6":
		return 6
	}
	return 0
}

// checkTHScope validates that TH (table header) elements have a /Scope
// attribute in their attribute dictionary.
func checkTHScope(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	return walkStructTree(ctx, doc, catalog, func(ctx context.Context, doc *document.Document, elem ast.PdfDict) []error {
		if getStructType(ctx, doc, elem) != "TH" {
			return nil
		}
		// Check /A (attributes) for /Scope.
		if hasAttribute(ctx, doc, elem, "Scope") {
			return nil
		}
		return []error{models.ErrMissingTHScope}
	})
}

// hasAttribute checks whether a structure element has a named attribute
// in its /A dictionary (which may be a dict or an array of dicts).
func hasAttribute(ctx context.Context, doc *document.Document, elem ast.PdfDict, attrName string) bool {
	aObj, ok := elem["A"]
	if !ok {
		return false
	}
	aRes, err := document.ResolveObject(ctx, doc, aObj)
	if err != nil {
		doc.Logger.Debug("resolving struct element attribute object", slog.Any("err", err))
	}
	switch a := aRes.(type) {
	case ast.PdfDict:
		if _, ok := a[ast.PdfName(attrName)]; ok {
			return true
		}
	case ast.PdfArray:
		for _, item := range a {
			d, err := document.ResolveDict(ctx, doc, item)
			if err != nil {
				doc.Logger.Debug("resolving attribute array entry", slog.Any("err", err))
			}
			if d != nil {
				if _, ok := d[ast.PdfName(attrName)]; ok {
					return true
				}
			}
		}
	}
	return false
}
