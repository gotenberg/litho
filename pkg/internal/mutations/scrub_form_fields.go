package mutations

import (
	"context"
	"fmt"
	"maps"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// fieldValueKeys are the field entries holding the text a widget shows: /V is
// the current value and /DV the default it reverts to (ISO 32000-1, Table 220).
// Both survive a page edit, and /DV survives a reset that clears /V.
var fieldValueKeys = []ast.PdfName{"V", "DV"}

// ScrubFormFieldValues clears the form field values anywhere in the document
// that quote one of tokens, and returns the object numbers it rewrote,
// ascending.
//
// This is the half [ScrubAnnotationText] deliberately leaves alone. A field's
// /V is inheritable (ISO 32000-1, 12.7.3.1), so the value a widget displays is
// frequently stated by an ancestor field dictionary that appears in no page's
// /Annots at all. Walking annotations and clearing what they carry looks
// complete and silently leaves the value sitting in the parent, which is why
// this pass walks up to the dictionary that actually states the value and
// rewrites that one.
//
// The widget's /AP goes with it. An appearance stream draws the old value, so
// clearing /V alone leaves it on screen; unlike a markup annotation, a widget's
// appearance is one litho can regenerate from the field value
// (see formNeedsAppearances and generatedAppearance), so dropping it is
// recoverable rather than destructive.
//
// It refuses when the dictionary stating the value has no indirect reference,
// reporting [ErrFieldValueNotAddressable]: there is no object to replace.
func ScrubFormFieldValues(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	tokens []string,
) ([]int, error) {
	// Fields are rewritten one at a time, so a refusal on a later one would
	// otherwise leave the earlier ones queued. Restore is lossless here because
	// every edit is a fresh dictionary handed to AllocAt rather than an AST
	// mutated in place.
	snap := alloc.Snapshot()
	scrubbed, err := scrubFormFieldValues(ctx, alloc, doc, tokens)
	if err != nil {
		alloc.Restore(snap)
		return nil, err
	}
	return scrubbed, nil
}

func scrubFormFieldValues(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	tokens []string,
) ([]int, error) {
	return scrubDocumentPages(ctx, doc, tokens, func(page *document.Page, scrubbed map[int]bool) error {
		return scrubPageWidgets(ctx, alloc, doc, page, tokens, scrubbed)
	})
}

// scrubPageWidgets clears the values behind one page's widget annotations.
func scrubPageWidgets(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	page *document.Page,
	tokens []string,
	scrubbed map[int]bool,
) error {
	annotsObj, ok := page.Dict["Annots"]
	if !ok {
		return nil
	}
	annots, err := document.ResolveArray(ctx, doc, annotsObj)
	if err != nil || annots == nil {
		return nil
	}

	for _, entry := range annots {
		annotRef, isRef := entry.(ast.PdfIndirectRef)
		if !isRef {
			// A direct widget cannot have its /AP dropped in place. It is
			// reported by [ScrubAnnotationText] when it carries text of its
			// own; here there is nothing to rewrite.
			continue
		}
		annot, err := document.ResolveDict(ctx, doc, annotRef)
		if err != nil || annot == nil {
			continue
		}
		if subtype, _ := document.ResolveName(ctx, doc, annot["Subtype"]); subtype != "Widget" {
			continue
		}

		owner, ownerRef, addressable := fieldValueOwner(ctx, doc, annotRef, annot)
		if owner == nil {
			continue
		}
		if !fieldValueQuotes(ctx, doc, owner, tokens) {
			continue
		}
		if !addressable {
			return fmt.Errorf("%w: the value behind widget %d", ErrFieldValueNotAddressable, annotRef.ObjectNum)
		}

		newOwner := make(ast.PdfDict, len(owner))
		maps.Copy(newOwner, owner)
		for _, key := range fieldValueKeys {
			delete(newOwner, key)
		}
		if !alloc.AllocAt(ownerRef.ObjectNum, newOwner) {
			return fmt.Errorf("%w: object %d", ErrFieldValueNotAddressable, ownerRef.ObjectNum)
		}
		scrubbed[ownerRef.ObjectNum] = true

		// The appearance still draws the old value. A widget's regenerates from
		// the field value, so dropping it loses nothing that cannot be rebuilt.
		if _, hasAP := annot["AP"]; hasAP {
			newAnnot := make(ast.PdfDict, len(annot))
			maps.Copy(newAnnot, annot)
			delete(newAnnot, "AP")
			if alloc.AllocAt(annotRef.ObjectNum, newAnnot) {
				scrubbed[annotRef.ObjectNum] = true
			}
		}
	}
	return nil
}

// fieldValueOwner returns the dictionary that actually states /V for this
// widget, the reference naming it, and whether that reference is usable.
//
// It parallels [inheritedFieldTypeAndValue] deliberately rather than by
// accident: that one answers what the value IS and discards the dictionary it
// came from, while clearing the value needs the dictionary itself and the
// object number to write it back to. Resolving a /Parent throws the reference
// away, so the walk carries it alongside.
func fieldValueOwner(
	ctx context.Context,
	doc *document.Document,
	annotRef ast.PdfIndirectRef,
	annotDict ast.PdfDict,
) (ast.PdfDict, ast.PdfIndirectRef, bool) {
	dict := annotDict
	ref := annotRef
	addressable := true

	for depth := 0; depth < maxFieldAncestorDepth && dict != nil; depth++ {
		for _, key := range fieldValueKeys {
			if _, ok := dict[key]; ok {
				return dict, ref, addressable
			}
		}
		parentObj, ok := dict["Parent"]
		if !ok {
			return nil, ast.PdfIndirectRef{}, false
		}
		// Reassigned per hop, never latched: the flag describes the dictionary
		// about to be examined, not a link the walk passed through. A direct
		// dictionary partway up does not make an addressable ancestor above it
		// unreachable, and treating it that way refused scrubs that were
		// perfectly possible.
		if parentRef, isRef := parentObj.(ast.PdfIndirectRef); isRef {
			ref = parentRef
			addressable = true
		} else {
			// Written straight into its child, so there is no object to replace
			// if the value turns out to live here.
			addressable = false
		}
		parent, err := document.ResolveDict(ctx, doc, parentObj)
		if err != nil || parent == nil {
			return nil, ast.PdfIndirectRef{}, false
		}
		dict = parent
	}
	return nil, ast.PdfIndirectRef{}, false
}

// fieldValueQuotes reports whether the field's stored value or default value
// contains one of the tokens.
//
// Only string values are compared: a token is text a redaction removed, so a
// button's /V name state cannot quote one.
func fieldValueQuotes(ctx context.Context, doc *document.Document, owner ast.PdfDict, tokens []string) bool {
	for _, key := range fieldValueKeys {
		raw, present := owner[key]
		if !present {
			continue
		}
		resolved, err := document.ResolveObject(ctx, doc, raw)
		if err != nil {
			continue
		}
		str, isStr := resolved.(ast.PdfString)
		if !isStr {
			continue
		}
		text := document.DecodePdfString(str)
		for _, tok := range tokens {
			if strings.Contains(text, tok) {
				return true
			}
		}
	}
	return false
}
