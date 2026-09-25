package signature

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// earlierDefinition names one appended definition of an object: its number
// and where the incremental update that wrote it ends.
type earlierDefinition struct {
	num int
	end int64
}

// withoutEarlierAppearances returns kept without the objects an update wrote
// to redraw an annotation it changed within policy, judged in the revision
// that update made, and a note for MDPInfo when an update could not be
// judged. objects are the appended objects [withoutFormData] was given, kept
// those it left, updates the file offsets where each incremental update after
// the signed revision ends ([updateEndOffsets]), and policy is 2 or 3.
//
// ISO 32000-1, 12.8.2.2, Table 254 permits filling in forms and signing from
// P=2 up, and P=3 adds annotation changes. Each form fill redraws the widgets
// it fills in (12.5.6.19, 12.7.3.3), and a later fill that redraws the same
// widget with a new appearance stream leaves the earlier one unreferenced.
// [withoutFormData] reads the current document, which reaches only the last
// appearance, so every earlier one would otherwise stay a change.
//
// Each update is judged against the revision just before it, the one the
// signed revision rev ends for the first update:
//
//   - its annotations are those it wrote that the policy permits: a widget
//     whose change, compared with the revision before, is filling in or
//     signing ([fieldDiff]), and at P=3 any other annotation;
//   - an object of one of the [appearanceTypes] it wrote is exempt when the
//     revision it made reaches it from those annotations' /AP ([drawnObjects])
//     and it is new, neither held by the signed revision nor by the revision
//     before ([signedRevision.held]), or was the own appearance stream of one
//     of those annotations in the revision before ([ownAppearances]) and was
//     new since signing or already that annotation's own in the signed
//     revision.
//
// The last condition keeps an update from pointing a widget at an object the
// signed document uses elsewhere, a comment's appearance, and letting the next
// update rewrite it as that widget's own. An object no permitted update
// explains stays a change. An update whose revision, or the one before it,
// cannot be read is not judged, so what it wrote stays a change. At most
// [maxEarlierRevisions] updates are judged, each reading two revisions.
//
// # Corpus evidence
//
//   - pdfjs issue18801.pdf: the 14 form fills Acrobat saved after the
//     signature leave 1,209 XObjects, 12 fonts and 12 font descriptors that no
//     current appearance reaches, and each is part of the appearance of a
//     widget the fill that wrote it filled in. Two more are the appearances of
//     comments a later update redrew, which P=3 permits.
func withoutEarlierAppearances(
	ctx context.Context,
	doc *document.Document,
	signed *signedRevision,
	rev *document.Document,
	objects, kept []appendedObject,
	updates []int64,
	policy int,
) (_ []appendedObject, note string) {
	pending := make(map[int64]bool)
	for _, obj := range kept {
		if obj.UpdateEnd > 0 && !obj.IsAnnot && appearanceTypes[obj.Type] {
			pending[obj.UpdateEnd] = true
		}
	}
	if len(pending) == 0 {
		return kept, ""
	}
	if len(pending) > maxEarlierRevisions {
		return kept, fmt.Sprintf("; appearances of earlier updates not recognized: %d updates hold objects to check, more than the %d compared", len(pending), maxEarlierRevisions)
	}

	exempt := make(map[earlierDefinition]bool)
	before := signed
	for _, end := range updates {
		if err := ctx.Err(); err != nil {
			return kept, fmt.Sprintf("; appearances of earlier updates not recognized: %v", err)
		}
		if pending[end] {
			if err := exemptUpdateAppearances(ctx, doc, signed, rev, before, end, objects, kept, policy, exempt); err != nil {
				// A revision that cannot be read explains nothing, so what
				// its update wrote stays a change.
				doc.Logger.Debug("judging an earlier update's appearances", slog.Int64("end", end), slog.Any("err", err))
			}
			if n := before.note(); n != "" && before != signed {
				note = "; appearances of earlier updates not recognized" + n
			}
		}
		before = newSignedRevision(doc, end)
	}

	rest := make([]appendedObject, 0, len(kept))
	for _, obj := range kept {
		if !exempt[earlierDefinition{obj.ObjNum, obj.UpdateEnd}] {
			rest = append(rest, obj)
		}
	}
	return rest, note
}

// exemptUpdateAppearances adds to exempt the appearance objects of kept that
// the update ending at end wrote, following the rules of
// [withoutEarlierAppearances]. before is the revision the update was made
// on, signed with rev the signed revision.
func exemptUpdateAppearances(
	ctx context.Context,
	doc *document.Document,
	signed *signedRevision,
	rev *document.Document,
	before *signedRevision,
	end int64,
	objects, kept []appendedObject,
	policy int,
	exempt map[earlierDefinition]bool,
) error {
	made, err := doc.Revision(ctx, end)
	if err != nil {
		return err
	}
	prior, err := before.revision(ctx)
	if err != nil {
		return err
	}
	priorHeld := func(num int) bool { return before.held(ctx, prior, num) }

	changes := &fieldDiff{doc: made, rev: prior, held: priorHeld}
	var annots []int
	for _, obj := range objects {
		if obj.UpdateEnd != end || !obj.IsAnnot || !permitted(obj, policy) {
			continue
		}
		if obj.Subtype == "Widget" {
			dict, err := document.ResolveDict(ctx, made, ast.PdfIndirectRef{ObjectNum: obj.ObjNum})
			if err != nil || dict == nil || changes.judge(ctx, obj.ObjNum, dict) != "" {
				continue
			}
		}
		annots = append(annots, obj.ObjNum)
	}
	if len(annots) == 0 {
		return nil
	}
	reached, err := drawnObjects(ctx, made, annots)
	if err != nil {
		return err
	}
	own := ownAppearances(ctx, prior, annots)
	signedOwn := ownAppearances(ctx, rev, annots)

	for _, obj := range kept {
		num := obj.ObjNum
		if obj.UpdateEnd != end || obj.IsAnnot || !appearanceTypes[obj.Type] || !reached[num] {
			continue
		}
		signedHeld := signed.held(ctx, rev, num)
		isNew := !signedHeld && !priorHeld(num)
		if isNew || (own[num] && (!signedHeld || signedOwn[num])) {
			exempt[earlierDefinition{num, end}] = true
		}
	}
	return nil
}
