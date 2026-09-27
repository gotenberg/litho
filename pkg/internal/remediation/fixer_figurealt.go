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

// figureNode pairs a Figure structure element with its indirect reference.
type figureNode struct {
	ref  ast.PdfIndirectRef
	dict ast.PdfDict
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
// verdict flips green. Only indirect Figure elements are touched; an inline
// Figure has no object number to rewrite in place and is left alone.
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
	roleMap := compliance.LoadRoleMap(ctx, doc, stDict)

	var figures []figureNode
	visited := map[int]bool{stRef.ObjectNum: true}
	collectFiguresNeedingAlt(ctx, alloc, doc, stDict["K"], roleMap, profile, visited, 0, &figures)
	if len(figures) == 0 {
		return nil, nil
	}

	for _, f := range figures {
		newFig := make(ast.PdfDict, len(f.dict)+1)
		maps.Copy(newFig, f.dict)
		// /Alt is a text string (ISO 32000-1, 14.9.3). The caller supplies it
		// through WithFigureAltText, so it may hold anything.
		newFig["Alt"] = document.EncodePdfString(altText)
		alloc.AllocAt(f.ref.ObjectNum, newFig)
	}

	if synthesized {
		return []string{fmt.Sprintf("inserted placeholder /Alt on %d figure(s) (generic text; human review still owed)", len(figures))}, nil
	}
	return []string{fmt.Sprintf("inserted /Alt on %d figure(s)", len(figures))}, nil
}

// collectFiguresNeedingAlt walks the structure tree and gathers every indirect
// Figure element that lacks required alternate text, using the validator's own
// predicate so the fixer repairs exactly what the check flags. A visited set
// breaks reference cycles and the depth bound caps a pathological tree, mirroring
// the table and /Lang repair walks.
func collectFiguresNeedingAlt(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, kObj ast.PdfObject, roleMap map[ast.PdfName]ast.PdfName, profile string, visited map[int]bool, depth int, out *[]figureNode) {
	if depth > compliance.MaxStructTreeWalkDepth || ctx.Err() != nil {
		return
	}
	switch v := kObj.(type) {
	case ast.PdfArray:
		for _, item := range v {
			collectFiguresNeedingAlt(ctx, alloc, doc, item, roleMap, profile, visited, depth, out)
		}
	case ast.PdfIndirectRef:
		if v.ObjectNum == 0 || visited[v.ObjectNum] {
			return
		}
		visited[v.ObjectNum] = true
		elem := resolveFromAllocOrDoc(ctx, alloc, doc, v)
		if elem == nil {
			return
		}
		if compliance.FigureNeedsAltText(ctx, doc, elem, roleMap, profile) {
			*out = append(*out, figureNode{ref: v, dict: elem})
		}
		collectFiguresNeedingAlt(ctx, alloc, doc, elem["K"], roleMap, profile, visited, depth+1, out)
	case ast.PdfDict:
		// Inline structure element: recurse into its children but never collect
		// it, since an inline figure has no object number to rewrite in place.
		collectFiguresNeedingAlt(ctx, alloc, doc, v["K"], roleMap, profile, visited, depth+1, out)
	}
}
