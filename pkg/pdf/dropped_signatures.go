package pdf

import (
	"context"
	"fmt"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/mutations"
	"github.com/gotenberg/litho/pkg/models"
)

// WithDroppedSignatures stores in *dst, once the output is written, the
// signatures of the inputs that the output does not carry (see
// [models.DroppedSignature]). It applies to [Split], [Merge], [Writer.WriteTo]
// and [Pipeline.WriteTo]. A write that fails leaves *dst as it was.
//
// [Split] and [Merge] build a new file, so they drop every signature of every
// source. [Writer.WriteTo] reports what [Writer.DroppedSignatures] reports, and
// [Pipeline.WriteTo] what [models.PipelineResult.DroppedSignatures] holds.
func WithDroppedSignatures(dst *[]models.DroppedSignature) WriteOption {
	return func(o *writeOpts) {
		o.droppedSignatures = dst
	}
}

// WithSignatureRemoval allows an output to leave out the signatures of its
// inputs, as [DroppedSignature] describes. Without it, [Writer.WriteTo],
// [Split], [Merge], [Compose] and [Pipeline.WriteTo] return
// [ErrSignaturesDropped] rather than write an output that drops one. Given to
// [Pipeline.WriteTo], it allows every stage of the pipeline; given to
// [Pipeline.Split], [Pipeline.Merge] or [Pipeline.Compose], that stage only.
func WithSignatureRemoval() WriteOption {
	return func(o *writeOpts) {
		o.signatureRemoval = true
	}
}

// refuseDropping returns [ErrSignaturesDropped] when dropped, the signatures
// an output leaves out, is not empty and cfg does not allow it.
func (cfg writeOpts) refuseDropping(dropped []DroppedSignature) error {
	if len(dropped) == 0 || cfg.signatureRemoval {
		return nil
	}
	return fmt.Errorf("%w: %d signature(s); pass WithSignatureRemoval to drop them", ErrSignaturesDropped, len(dropped))
}

// DroppedSignatures returns the signatures the output of [Writer.WriteTo]
// does not carry: those of the document the Writer edits, and of each
// document [Writer.InsertPages] took pages from, when the output is a full
// rewrite. It returns nil before WriteTo has written the output, and for an
// output written as an incremental update, which keeps every signature.
//
// The edits that force a full rewrite are listed on [Writer.WriteTo]:
// [Writer.DeletePages], [Writer.InsertPages], [Writer.FlattenForm],
// [Writer.Encrypt], [Writer.RemoveEncryption], [Writer.Redact],
// [WithFullRewrite], a remediation whose repairs need one, and any edit to a
// source litho repaired on reading or an encrypted one. Each signed signature
// widget's appearance is drawn into its page, in a tagged document under the
// structure element that enclosed the widget, and the field leaves the form
// (see [models.DroppedSignature]). A Writer that also signs ([Writer.Sign])
// keeps the signature it adds.
//
// Entries come in the order of the Writer's document, then of the inserted
// documents: the signed fields its form lists, those only a page presents,
// then the signatures /Perms names that no field holds. An entry repeated
// across documents is listed once.
func (w *Writer) DroppedSignatures() []models.DroppedSignature {
	if !w.consumed {
		return nil
	}
	return slices.Clone(w.droppedSignatures)
}

// recordDroppedSignatures adds to the Writer's record the signatures a full
// rewrite of its document drops (see [mutations.BakeSignaturesForRewrite]),
// with those of the documents its insertions took pages from, whose copies
// leave them out (see [Writer.InsertPages]).
func (w *Writer) recordDroppedSignatures(ctx context.Context) {
	docs := append([]*document.Document{w.reader.doc}, w.pageSources()...)
	w.droppedSignatures = appendDroppedSignatures(w.droppedSignatures, droppedSignaturesOf(ctx, docs...)...)
}

// droppedSignaturesOf returns the signatures of docs, in order, each listed
// once: what an output built from them drops.
func droppedSignaturesOf(ctx context.Context, docs ...*document.Document) []models.DroppedSignature {
	var out []models.DroppedSignature
	for _, doc := range docs {
		for _, sig := range mutations.SignedSignatures(ctx, doc) {
			out = appendDroppedSignatures(out, models.DroppedSignature{Field: sig.Field, Perms: sig.Perms})
		}
	}
	return out
}

// appendDroppedSignatures appends to list each of add it does not hold yet.
func appendDroppedSignatures(list []models.DroppedSignature, add ...models.DroppedSignature) []models.DroppedSignature {
	for _, sig := range add {
		if !slices.Contains(list, sig) {
			list = append(list, sig)
		}
	}
	return list
}
