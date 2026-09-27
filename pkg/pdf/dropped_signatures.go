package pdf

import (
	"context"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/mutations"
)

// DroppedSignature names a signature of an input that an output does not
// carry.
//
// A signature's digest covers the bytes of the file it was made in, as its
// /ByteRange names them (ISO 32000-1, 12.8.1). An output that rewrites the
// file, or builds a new one, moves every one of those bytes, so a signature
// carried into it could never verify. litho leaves it out instead: a signed
// signature field leaves the form and its widget's appearance is drawn into
// the page, so the page looks as it did, and a /Perms entry naming a signature
// goes. The input's document security store goes with its signatures: the
// catalog /DSS holds their certificates, OCSP responses, CRLs and /VRI entries
// (ISO 32000-2, 12.8.4.3), which validate nothing the output holds. An
// incremental update appends to the signed bytes and keeps every signature,
// and the store.
//
// DroppedSignature values are immutable and safe for concurrent use.
type DroppedSignature struct {
	// Field is the fully qualified name of the signed signature field that
	// held the signature (ISO 32000-1, 12.7.3.2), "" for a signature no field
	// holds: the usage rights signature /Perms /UR3 names (ISO 32000-1,
	// 12.8.2.3).
	Field string
	// Perms names the catalog /Perms entry that referred to the signature,
	// "DocMDP" for a certification signature and "UR3" for a usage rights
	// signature (ISO 32000-1, 12.8.4, Table 258), and is "" when none did.
	Perms string
}

// WithDroppedSignatures stores in *dst, once the output is written, the
// signatures of the inputs that the output does not carry (see
// [DroppedSignature]). It applies to [Split], [Merge], [Writer.WriteTo] and
// [Pipeline.WriteTo]. A write that fails leaves *dst as it was.
//
// [Split] and [Merge] build a new file, so they drop every signature of every
// source. [Writer.WriteTo] reports what [Writer.DroppedSignatures] reports, and
// [Pipeline.WriteTo] what [PipelineResult.DroppedSignatures] holds.
func WithDroppedSignatures(dst *[]DroppedSignature) WriteOption {
	return func(o *writeOpts) {
		o.droppedSignatures = dst
	}
}

// DroppedSignatures returns the signatures the output of [Writer.WriteTo]
// does not carry: those of the document the Writer edits, and of each
// document [Writer.InsertPages] took pages from, when the output is a full
// rewrite. It returns nil before WriteTo has written the output, and for an
// output written as an incremental update, which keeps every signature.
//
// The edits that force a full rewrite are listed on [Writer.WriteTo]:
// [Writer.DeletePages], [Writer.InsertPages], [Writer.FlattenForm],
// [Writer.Encrypt], [Writer.RemoveEncryption], [Writer.Redact], [WithFullRewrite],
// a remediation whose repairs need one, and any edit to a source litho repaired
// on reading or an encrypted one. Each signed signature widget's appearance is
// drawn into its page, in a tagged document under the structure element that
// enclosed the widget, and the field leaves the form (see [DroppedSignature]).
// A Writer that also signs ([Writer.Sign]) keeps the signature it adds.
//
// Entries come in the order of the Writer's document, then of the inserted
// documents: the signed fields its form lists, those only a page presents,
// then the signatures /Perms names that no field holds. An entry repeated
// across documents is listed once.
func (w *Writer) DroppedSignatures() []DroppedSignature {
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
func droppedSignaturesOf(ctx context.Context, docs ...*document.Document) []DroppedSignature {
	var out []DroppedSignature
	for _, doc := range docs {
		for _, sig := range mutations.SignedSignatures(ctx, doc) {
			out = appendDroppedSignatures(out, DroppedSignature{Field: sig.Field, Perms: sig.Perms})
		}
	}
	return out
}

// appendDroppedSignatures appends to list each of add it does not hold yet.
func appendDroppedSignatures(list []DroppedSignature, add ...DroppedSignature) []DroppedSignature {
	for _, sig := range add {
		if !slices.Contains(list, sig) {
			list = append(list, sig)
		}
	}
	return list
}
