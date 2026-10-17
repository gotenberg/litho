package mutations

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// signatureLock is the field lock a signature the document holds records in
// its FieldMDP transform (ISO 32000-1, 12.8.2.4, Table 256): the action and
// the fields its signing locked. field is the signed field.
type signatureLock struct {
	field  string
	action ast.PdfName
	names  []string
}

// covers reports whether the lock reaches the field named name. ISO 32000-1,
// Table 256 locks every field for All, the fields named for Include, and every
// field not named for Exclude. A name reaches the fields under it, as
// [lockFields] turns a named non-terminal field's kids read-only.
func (l signatureLock) covers(name string) bool {
	named := slices.ContainsFunc(l.names, func(n string) bool {
		return name == n || strings.HasPrefix(name, n+".")
	})
	switch l.action {
	case "All":
		return true
	case "Include":
		return named
	case "Exclude":
		return !named
	}
	return false
}

// refuseLockedField refuses, with [ErrSignatureFieldLocked], a signature in
// the field named name when a signature the document holds locked it. signed
// says the field exists, so signing it sets its /V; a new field takes the
// name otherwise. docTimeStamp says the signature is a document timestamp.
//
// ISO 32000-1, 12.8.2.4, Table 256: a change to a field the FieldMDP
// transform locks invalidates the signature that recorded it, and signing a
// field changes its value (12.7.4.5). A new signature field is a field the
// lock reaches as well when it locks All or Exclude, so a new signature is
// refused there too.
//
// A document timestamp in a new field is not: it is the incremental update
// ETSI EN 319 142-1, 5.4 (PAdES B-LTA) and ISO 32000-2, 12.8.5 add after a
// signature, it leaves every field of the signed revision as it was, and its
// field, invisible and of zero size, holds no value a form fills. pyHanko's
// diff policy reads it the same way: its SigFieldCreationRule and
// SigFieldModificationRule clear an invisible /DocTimeStamp in a new field
// even under an All or Exclude lock, and a /Sig in the same place only at
// form filling, which a lock forbids.
//
// # Corpus evidence
//
//   - All corpora: 9 signatures in 5242 files record a FieldMDP lock; one,
//     pdfjs issue15813.pdf, locks All, so it alone refuses a new signature.
//   - litho: siglock_2b.pdf, signed in a locked field and timestamped, draws
//     "the form field name is locked" from pyHanko 0.37, which is
//     pyHanko-side. The update writes only the catalog, the DocTimeStamp
//     dictionary and its field, and pyHanko's own timestamp of the same
//     signed file, and of the file signed by pyHanko, draws the same report.
//     Its _allow_in_place_appearance_update compares the last revision that
//     wrote a locked text field's appearance stream with the signed revision
//     by ==, so a stream written before it, and not since, reads as rewritten
//     in place; with <= every timestamp after a lock passes at LTA_UPDATES,
//     and only the signatures this function refuses still fail.
func refuseLockedField(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, fields ast.PdfArray, name string, signed, docTimeStamp bool) error {
	if docTimeStamp && !signed {
		return nil
	}
	locks, err := signatureLocks(ctx, alloc, doc, fields)
	if err != nil {
		return err
	}
	for _, l := range locks {
		if l.covers(name) {
			return fmt.Errorf("%w: %w: field %q is under the /%s lock the signature in field %q records (ISO 32000-1, 12.8.2.4, Table 256)",
				ErrSignatureFieldUnavailable, ErrSignatureFieldLocked, name, string(l.action), l.field)
		}
	}
	return nil
}

// signatureLocks returns the field locks the signatures of the form's root
// fields record: the FieldMDP transform of each signed signature field's
// signature dictionary (ISO 32000-1, 12.8.2.4, Tables 253 and 256). A
// transform whose action Table 256 does not define locks nothing a verifier
// can name, and is passed over. The /Lock of a signed field is not read: the
// transform is the record a verifier holds the document to, and litho writes
// one for every /Lock it honours ([lockFields]).
//
// The walk visits each indirect field and /Kids array once
// ([document.VisitedBefore]) and stops at [maxLockDepth] levels.
func signatureLocks(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, fields ast.PdfArray) ([]signatureLock, error) {
	type node struct {
		obj    ast.PdfObject
		prefix string
		depth  int
	}
	visited := make(map[int]bool)
	stack := make([]node, 0, len(fields))
	for _, f := range slices.Backward(fields) {
		stack = append(stack, node{obj: f})
	}
	var locks []signatureLock
	for steps := 0; len(stack) > 0; steps++ {
		if steps%100 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, fmt.Errorf("reading the form's field locks: %w", err)
			}
		}
		top := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if document.VisitedBefore(top.obj, visited) {
			continue
		}
		field, err := resolveDictAllocFirst(ctx, alloc, doc, top.obj)
		if err != nil || field == nil {
			continue
		}
		name := top.prefix
		if s, err := document.ResolveString(ctx, doc, field["T"]); err == nil && s != "" {
			if partial := document.DecodePdfString(s); name == "" {
				name = partial
			} else {
				name += "." + partial
			}
		}
		if l, ok := recordedLock(ctx, alloc, doc, field["V"]); ok {
			l.field = name
			locks = append(locks, l)
		}
		if top.depth >= maxLockDepth {
			continue
		}
		kidsObj, ok := field["Kids"]
		if !ok || document.VisitedBefore(kidsObj, visited) {
			continue
		}
		kids, err := resolveArrayAllocFirst(ctx, alloc, doc, kidsObj)
		if err != nil {
			continue
		}
		for _, kid := range slices.Backward(kids) {
			stack = append(stack, node{obj: kid, prefix: name, depth: top.depth + 1})
		}
	}
	return locks, nil
}

// recordedLock returns the lock the FieldMDP transform of the signature
// dictionary value records, ok false when value is no signature dictionary
// or records none (ISO 32000-1, 12.8.2.4, Tables 253 and 256).
func recordedLock(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, value ast.PdfObject) (signatureLock, bool) {
	if value == nil {
		return signatureLock{}, false
	}
	sig, err := resolveDictAllocFirst(ctx, alloc, doc, value)
	if err != nil || sig == nil {
		return signatureLock{}, false
	}
	refs, err := resolveArray(ctx, doc, sig["Reference"])
	if err != nil {
		return signatureLock{}, false
	}
	for _, r := range refs {
		ref, err := document.ResolveDict(ctx, doc, r)
		if err != nil || ref == nil {
			continue
		}
		if method, err := document.ResolveName(ctx, doc, ref["TransformMethod"]); err != nil || method != "FieldMDP" {
			continue
		}
		params, err := document.ResolveDict(ctx, doc, ref["TransformParams"])
		if err != nil || params == nil {
			continue
		}
		action, err := document.ResolveName(ctx, doc, params["Action"])
		if err != nil {
			action = ""
		}
		switch action {
		case "All":
			return signatureLock{action: action}, true
		case "Include", "Exclude":
			return signatureLock{action: action, names: seedStrings(ctx, doc, params["Fields"])}, true
		}
		doc.Logger.Debug("signing: a FieldMDP transform names an action Table 256 does not define, so it locks no field", slog.String("action", string(action)))
	}
	return signatureLock{}, false
}
