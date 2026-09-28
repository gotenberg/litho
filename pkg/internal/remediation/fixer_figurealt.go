package remediation

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/mutations"
)

// defaultFigureAlt is the placeholder written into a Figure that lacks alternate
// text when aggressive remediation runs without a caller-supplied string. It is
// deliberately generic: an automated processor cannot describe an image, and the
// value exists only to satisfy the ISO 14289-1 §7.3 / ISO 14289-2 §8.2.5.28.2
// presence check.
const defaultFigureAlt = "Figure"

// matchFigureAlt matches the "Figure lacks alternate text" violations:
// ISO 14289-1 §7.3 t1 (PDF/UA-1) and ISO 14289-2 §8.2.5.28.2 t1 (PDF/UA-2). It
// keys on the exact clause and test number, like the table fixer, so it never
// over-claims a neighbouring rule.
func matchFigureAlt(v compliance.Violation) bool {
	return (v.Rule.Clause == "7.3" && v.Rule.TestNumber == 1) ||
		(v.Rule.Clause == "8.2.5.28.2" && v.Rule.TestNumber == 1)
}

// fixFigureAlt writes alternate text into Figure structure elements that lack the
// /Alt or /ActualText PDF/UA requires (ISO 14289-1 §7.3, ISO 14289-2
// §8.2.5.28.2). A processor cannot synthesize a meaningful description, and a
// generic placeholder satisfies the validator while masking the missing
// description from assistive technology, so the fixer never fabricates alt text
// by default: with opts.FigureAlt empty in the safe mode it leaves the figure
// for a human. It acts when the caller supplies opts.FigureAlt, or, in aggressive
// mode (opts.PreserveContent == false), falls back to a generic placeholder so
// the document passes the mechanical check. The /Alt edit changes no rendering;
// gating the placeholder on aggressive mode is a deliberate honesty restriction,
// not the literal content-preservation meaning. A synthesized placeholder is
// reported as still owing human review, so the residual stays auditable after the
// verdict flips green. A Figure written directly in a /K, with no object number
// of its own, takes the text in the object holding it (see
// [figureAltWriter.kids]). In aggressive mode without a caller string,
// [fixEmptyFigure] has already taken out the Figures that enclose nothing, so
// the placeholder goes to the Figures that stay.
//
// In safe mode without a caller string the finding stays in the report's
// residual, as every refusal does. What a Figure paints changes nothing: no
// drawing yields a description, so a Figure of vector art or images is left as
// surely as one that paints only text or nothing at all.
//
// # Corpus evidence
//
//   - All corpora: PDF/UA-2 remediation in safe mode leaves ISO 14289-2
//     8.2.5.28.2 t1 on 166 of 1127 tagged files, and at WTPDF-1.0
//     Accessibility too. Their failing Figures paint vector art, images and
//     forms, mostly mixed with text; no file's failing Figures paint only
//     text, and 8 files' paint nothing (empty elements, or an /MCID no content
//     stream marks). A caller string reaches every Figure on all 166, and
//     aggressive mode leaves none.
//   - verapdf PDF_A-1a/2a/2u/4 test files (9 sources, 18 cells at PDF/UA-2 and
//     WTPDF-1.0 Accessibility): their Figures were out of reach until the
//     single-doc-root repair stopped listing its Document as its own /K, which
//     had left the whole tree unreachable to this fixer and to both
//     validators.
//   - litho directfig_ua2.pdf: three Figures that enclose drawings, written
//     directly in an element's /K array, in an inline Div's and in a /K array
//     of its own, kept failing 8.2.5.28.2 after aggressive remediation. Now
//     they take the placeholder and the outputs pass litho and veraPDF 1.30.2.
func fixFigureAlt(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, opts Options, _ []compliance.Violation, profile string) ([]string, error) {
	altText := opts.FigureAlt
	synthesized := false
	if altText == "" {
		if opts.PreserveContent {
			return nil, nil
		}
		altText = defaultFigureAlt
		synthesized = true
	}

	catalogRef, ok := catalogRefForFixer(ctx, doc)
	if !ok {
		return nil, fmt.Errorf("trailer missing /Root")
	}
	catalog := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalog == nil {
		return nil, fmt.Errorf("resolving catalog")
	}
	stRef, ok := catalog["StructTreeRoot"].(ast.PdfIndirectRef)
	if !ok {
		return nil, nil
	}
	stDict := resolveFromAllocOrDoc(ctx, alloc, doc, stRef)
	if stDict == nil {
		return nil, nil
	}

	w := &figureAltWriter{
		alloc:   alloc,
		doc:     doc,
		roleMap: compliance.LoadRoleMap(ctx, doc, stDict),
		profile: profile,
		// /Alt is a text string (ISO 32000-1, 14.9.3). The caller supplies it
		// through WithFigureAltText, so it may hold anything.
		alt:     document.EncodePdfString(altText),
		resolve: mutations.AllocResolver(ctx, alloc, doc),
		visited: map[int]bool{stRef.ObjectNum: true},
	}
	if k, changed := w.kids(ctx, stDict["K"], 0); changed {
		newRoot := maps.Clone(stDict)
		newRoot["K"] = k
		alloc.AllocAt(stRef.ObjectNum, newRoot)
	}
	if w.count == 0 {
		return nil, nil
	}

	if synthesized {
		return []string{fmt.Sprintf("inserted placeholder /Alt on %d figure(s) (generic text; human review still owed)", w.count)}, nil
	}
	return []string{fmt.Sprintf("inserted /Alt on %d figure(s)", w.count)}, nil
}

// figureAltWriter writes alternate text into the Figures of one structure tree
// that lack it, judged by the validator's own predicate so the fixer repairs
// exactly what the check flags. It reads every object from alloc first. Not
// safe for concurrent use.
type figureAltWriter struct {
	alloc   *writer.ObjAllocator
	doc     *document.Document
	roleMap map[ast.PdfName]ast.PdfName
	profile string
	alt     ast.PdfObject
	resolve func(ast.PdfObject) ast.PdfObject
	// visited marks the elements, and the /K arrays written as objects of
	// their own, that the walk read. Each is read once: the direct elements
	// of a shared array can each name it again as their /K, a 2^depth
	// fan-out were it read anew wherever it is reached.
	visited map[int]bool
	count   int
}

// kids returns the /K value kObj with alternate text written into each Figure
// it holds that lacks it, and whether kObj changed. ISO 32000-1, 14.7.2, Table
// 323 lets a /K hold an element directly, and such an element has no object
// number to rewrite, so it changes in the value holding it: an element or a /K
// array written as an object of its own is rewritten at its number, and a
// reference to it comes back unchanged. The walk stops at
// [compliance.MaxStructTreeWalkDepth], the depth validation walks to.
func (w *figureAltWriter) kids(ctx context.Context, kObj ast.PdfObject, depth int) (ast.PdfObject, bool) {
	if depth > compliance.MaxStructTreeWalkDepth || ctx.Err() != nil {
		return kObj, false
	}
	switch v := kObj.(type) {
	case ast.PdfIndirectRef:
		if v.ObjectNum <= 0 || w.visited[v.ObjectNum] {
			return kObj, false
		}
		w.visited[v.ObjectNum] = true
		switch obj := w.resolve(v).(type) {
		case ast.PdfArray:
			if out, changed := w.array(ctx, obj, depth); changed {
				w.alloc.AllocAt(v.ObjectNum, out)
			}
		case ast.PdfDict:
			if out, changed := w.element(ctx, obj, depth); changed {
				w.alloc.AllocAt(v.ObjectNum, out)
			}
		}
		return kObj, false
	case ast.PdfArray:
		return w.array(ctx, v, depth)
	case ast.PdfDict:
		return w.element(ctx, v, depth)
	}
	return kObj, false
}

// array returns the entries of a /K array, each passed through
// [figureAltWriter.kids], and whether one changed.
func (w *figureAltWriter) array(ctx context.Context, arr ast.PdfArray, depth int) (ast.PdfArray, bool) {
	var out ast.PdfArray
	for i, item := range arr {
		if updated, changed := w.kids(ctx, item, depth); changed {
			if out == nil {
				out = slices.Clone(arr)
			}
			out[i] = updated
		}
	}
	if out == nil {
		return arr, false
	}
	return out, true
}

// element returns the structure element elem with alternate text when it is a
// Figure lacking it, and with its /K passed through [figureAltWriter.kids], and
// whether either changed. A marked-content or object reference is no element
// (ISO 32000-1, Tables 324 and 325) and comes back unchanged.
func (w *figureAltWriter) element(ctx context.Context, elem ast.PdfDict, depth int) (ast.PdfDict, bool) {
	if !isStructElement(elem) {
		return elem, false
	}
	var out ast.PdfDict
	if compliance.FigureNeedsAltText(ctx, w.doc, elem, w.roleMap, w.profile) {
		out = maps.Clone(elem)
		out["Alt"] = w.alt
		w.count++
	}
	if k, changed := w.kids(ctx, elem["K"], depth+1); changed {
		if out == nil {
			out = maps.Clone(elem)
		}
		out["K"] = k
	}
	if out == nil {
		return elem, false
	}
	return out, true
}
