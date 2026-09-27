package mutations

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// annotTextKeys are the annotation entries holding plain text that can quote
// what a redaction removed: /Contents is the note's own text, /RC its rich-text
// form, and /URI a link target that routinely spells the heading it points at
// (ISO 32000-1, Table 164 and 12.5.6).
//
// /V and /DV are deliberately absent. A field's value and type are inheritable
// (ISO 32000-1, 12.7.3.1), so they can sit on an ancestor field dictionary that
// appears in no page's /Annots at all; walking annotations would silently miss
// it. That needs the ancestor walk [fieldHoldsText] does, and it is not this
// pass.
var annotTextKeys = []ast.PdfName{"Contents", "RC", "URI"}

// ScrubAnnotationText clears annotation text anywhere in the document that
// quotes one of tokens, and returns the object numbers it rewrote, ascending.
//
// Document-wide on purpose. An annotation on page five can quote text redacted
// on page one, so scrubbing only the redacted page's annotations would leave
// the copy that matters. Pass [RedactReport.RemovedText] as tokens.
//
// It refuses rather than half-clean:
//
//   - An annotation whose appearance stream still paints text reports
//     [ErrAnnotationAppearanceHoldsText]. The words are drawn by the /AP's own
//     content stream, so clearing /Contents leaves them on screen. Only a
//     widget's appearance regenerates from its field value; a markup
//     annotation's does not.
//   - An annotation written directly into /Annots reports
//     [ErrAnnotationNotAddressable], having no object number to replace.
func ScrubAnnotationText(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	tokens []string,
) ([]int, error) {
	// Annotations are rewritten one at a time, so refusing on a later one would
	// otherwise leave the earlier ones queued: a caller that handled the error
	// would still write a document scrubbed halfway. Restore is lossless here
	// because every edit is a fresh dictionary handed to AllocAt rather than an
	// AST mutated in place, the condition writer.ObjAllocator.Snapshot
	// documents.
	snap := alloc.Snapshot()
	scrubbed, err := scrubAnnotationText(ctx, alloc, doc, tokens)
	if err != nil {
		alloc.Restore(snap)
		return nil, err
	}
	return scrubbed, nil
}

func scrubAnnotationText(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	tokens []string,
) ([]int, error) {
	return scrubDocumentPages(ctx, doc, tokens, func(page *document.Page, scrubbed map[int]bool) error {
		return scrubPageAnnotations(ctx, alloc, doc, page, tokens, scrubbed)
	})
}

// scrubPageAnnotations clears the annotations one page carries.
func scrubPageAnnotations(
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
		annot, err := document.ResolveDict(ctx, doc, entry)
		if err != nil || annot == nil {
			continue
		}
		hits := annotTextHits(ctx, doc, annot, tokens)
		if len(hits) == 0 {
			continue
		}

		ref, isRef := entry.(ast.PdfIndirectRef)
		if !isRef {
			return fmt.Errorf("%w: an annotation on a page carries the text in %s", ErrAnnotationNotAddressable, hits[0])
		}
		if paints, err := appearancePaintsText(ctx, doc, annot); err != nil {
			return err
		} else if paints {
			return fmt.Errorf("%w: annotation %d carries the text in %s", ErrAnnotationAppearanceHoldsText, ref.ObjectNum, hits[0])
		}

		newAnnot := make(ast.PdfDict, len(annot))
		maps.Copy(newAnnot, annot)
		for _, key := range hits {
			delete(newAnnot, key)
		}
		if !alloc.AllocAt(ref.ObjectNum, newAnnot) {
			return fmt.Errorf("%w: annotation %d", ErrAnnotationNotAddressable, ref.ObjectNum)
		}
		scrubbed[ref.ObjectNum] = true
	}
	return nil
}

// annotTextHits returns the text entries of annot that quote a token, in the
// order of [annotTextKeys] so a refusal names the same one every run.
func annotTextHits(ctx context.Context, doc *document.Document, annot ast.PdfDict, tokens []string) []ast.PdfName {
	var hits []ast.PdfName
	for _, key := range annotTextKeys {
		raw, present := annot[key]
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
				hits = append(hits, key)
				break
			}
		}
	}
	return hits
}

// appearancePaintsText reports whether the annotation's normal appearance draws
// glyphs of its own.
//
// Mirrors the test [valueOutlivesAppearance] makes, inverted: there it proves an
// appearance paints nothing so baking it would lose the value, here it proves an
// appearance paints something so clearing the value would leave it on screen.
// An appearance litho cannot read counts as painting, because an appearance it
// cannot prove empty is one it cannot prove clean.
func appearancePaintsText(ctx context.Context, doc *document.Document, annot ast.PdfDict) (bool, error) {
	apObj, ok := annot["AP"]
	if !ok {
		return false, nil
	}
	apDict, err := document.ResolveDict(ctx, doc, apObj)
	if err != nil || apDict == nil {
		return false, nil
	}
	normal, ok := apDict["N"]
	if !ok {
		return false, nil
	}
	stream, err := document.ResolveStream(ctx, doc, normal)
	if err != nil || stream.Dict == nil {
		// A /N that is a sub-dictionary of states rather than a stream, or one
		// that will not resolve: unread, so not provably clean.
		return true, nil
	}
	data, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		return true, nil
	}
	return bytes.Contains(data, []byte("Tj")) || bytes.Contains(data, []byte("TJ")), nil
}
