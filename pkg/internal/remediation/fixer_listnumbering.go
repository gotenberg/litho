package remediation

import (
	"context"
	"fmt"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// defaultListNumbering is the placeholder written onto an L element that needs a
// ListNumbering attribute. It is a generic, valid (non-None) value; the true
// list style cannot be known without interpreting the label glyphs, so the
// choice is flagged for human review.
const defaultListNumbering = ast.PdfName("Unordered")

// matchListNumbering matches the PDF/UA-2 missing-ListNumbering violation
// (ISO 14289-2 §8.2.5.25 t01): an L element whose LI children carry Lbl labels
// must declare a ListNumbering attribute other than None.
func matchListNumbering(v compliance.Violation) bool {
	return v.Rule.Clause == "8.2.5.25" && v.Rule.TestNumber == 1
}

// fixListNumbering adds a ListNumbering attribute to an L element that has
// labelled list items but no usable ListNumbering. The value is a generic
// placeholder: the genuine list style depends on the label glyphs, which this
// fixer does not interpret, so it only acts in aggressive mode (opts.
// PreserveContent == false) and reports the result as owing human review. In the
// default safe mode it leaves the list untouched, since a wrong style misleads
// assistive technology even though it satisfies the presence check.
func fixListNumbering(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, opts Options, _ []compliance.Violation, _ string) ([]string, error) {
	if opts.PreserveContent {
		return nil, nil
	}

	lists, err := collectStructElems(ctx, alloc, doc, "L")
	if err != nil {
		return nil, err
	}

	var count int
	for _, l := range lists {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !listHasLabelledItem(ctx, doc, l.dict) {
			continue
		}
		if cur := listNumberingValue(ctx, doc, l.dict); cur != "" && cur != "None" {
			continue
		}

		newL := make(ast.PdfDict, len(l.dict)+1)
		maps.Copy(newL, l.dict)
		newL["A"] = appendListNumberingAttr(ctx, doc, l.dict["A"], defaultListNumbering)
		alloc.AllocAt(l.ref.ObjectNum, newL)
		count++
	}

	if count == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("set placeholder /ListNumbering on %d list(s) (generic style; human review still owed)", count)}, nil
}

// listHasLabelledItem reports whether the L element has a direct LI child that
// itself has a direct Lbl child, the condition that makes ListNumbering required.
func listHasLabelledItem(ctx context.Context, doc *document.Document, lElem ast.PdfDict) bool {
	arr, err := document.ResolveArray(ctx, doc, lElem["K"])
	if err != nil {
		return false
	}
	for _, item := range arr {
		d, err := document.ResolveDict(ctx, doc, item)
		if err != nil || d == nil {
			continue
		}
		if t, _ := document.ResolveName(ctx, doc, d["S"]); t != "LI" {
			continue
		}
		if liHasLblDirectChild(ctx, doc, d) {
			return true
		}
	}
	return false
}

// liHasLblDirectChild reports whether an LI element has a direct Lbl child.
func liHasLblDirectChild(ctx context.Context, doc *document.Document, li ast.PdfDict) bool {
	kids, err := document.ResolveArray(ctx, doc, li["K"])
	if err != nil {
		d, derr := document.ResolveDict(ctx, doc, li["K"])
		if derr != nil || d == nil {
			return false
		}
		t, _ := document.ResolveName(ctx, doc, d["S"])
		return t == "Lbl"
	}
	for _, item := range kids {
		d, err := document.ResolveDict(ctx, doc, item)
		if err != nil || d == nil {
			continue
		}
		if t, _ := document.ResolveName(ctx, doc, d["S"]); t == "Lbl" {
			return true
		}
	}
	return false
}

// listNumberingValue reads the current ListNumbering attribute value from the L
// element's /A, which may be a single attribute dict or an array of them.
func listNumberingValue(ctx context.Context, doc *document.Document, lElem ast.PdfDict) string {
	aObj, ok := lElem["A"]
	if !ok {
		return ""
	}
	if d, err := document.ResolveDict(ctx, doc, aObj); err == nil && d != nil {
		if v, _ := document.ResolveName(ctx, doc, d["ListNumbering"]); v != "" {
			return string(v)
		}
	}
	if arr, err := document.ResolveArray(ctx, doc, aObj); err == nil {
		for _, item := range arr {
			d, err := document.ResolveDict(ctx, doc, item)
			if err != nil || d == nil {
				continue
			}
			if v, _ := document.ResolveName(ctx, doc, d["ListNumbering"]); v != "" {
				return string(v)
			}
		}
	}
	return ""
}

// appendListNumberingAttr returns a new /A value with a List-owned attribute
// dict carrying ListNumbering=value added. An absent /A becomes the single
// attribute dict; an existing dict or array is extended into an array.
func appendListNumberingAttr(ctx context.Context, doc *document.Document, existing ast.PdfObject, value ast.PdfName) ast.PdfObject {
	attr := ast.PdfDict{"O": ast.PdfName("List"), "ListNumbering": value}
	if existing == nil {
		return attr
	}
	if arr, err := document.ResolveArray(ctx, doc, existing); err == nil && arr != nil {
		out := make(ast.PdfArray, 0, len(arr)+1)
		out = append(out, arr...)
		out = append(out, attr)
		return out
	}
	return ast.PdfArray{existing, attr}
}
