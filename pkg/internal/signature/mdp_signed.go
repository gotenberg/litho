package signature

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// signedRevision is the revision a signature covers, read from doc once and
// only when a check needs it, along with the object numbers it references.
// It is used by one evaluation on one goroutine.
type signedRevision struct {
	doc *document.Document
	end int64

	rev     *document.Document
	revErr  error
	revRead bool

	refs     map[int]bool
	refsErr  error
	refsRead bool
}

// newSignedRevision returns the revision of doc that ends at byte end, not yet
// read.
func newSignedRevision(doc *document.Document, end int64) *signedRevision {
	return &signedRevision{doc: doc, end: end}
}

// revision returns the signed revision, reading it on the first call.
func (s *signedRevision) revision(ctx context.Context) (*document.Document, error) {
	if !s.revRead {
		s.rev, s.revErr = s.doc.Revision(ctx, s.end)
		s.revRead = true
	}
	return s.rev, s.revErr
}

// held reports whether object num already existed when the document was
// signed: the signed revision rev defines it, or refers to it from an object
// its trailer reaches. rev is what [signedRevision.revision] returned.
//
// ISO 32000-1, 7.3.10 reads a reference to an object that is not defined as
// the null object, so the signed document meant something by that reference.
// An update that defines the number later changes what the signed document
// means, which is a modification, not an addition. The exemptions for new
// objects (validation data, a new XFA datasets packet, a new appearance
// stream) therefore apply only to a number the signed revision neither
// defines nor refers to.
//
// The references are collected on the first call that needs them. When they
// cannot be, every number counts as held, so nothing passes as new; the note
// [signedRevision.note] returns says why.
func (s *signedRevision) held(ctx context.Context, rev *document.Document, num int) bool {
	if rev.DefinesObject(num) {
		return true
	}
	if !s.refsRead {
		s.refs, s.refsErr = references(ctx, rev)
		s.refsRead = true
	}
	return s.refsErr != nil || s.refs[num]
}

// note returns the MDPInfo note that says the signed revision's references
// could not be collected, or "" when they were or were never needed.
func (s *signedRevision) note() string {
	if s.refsErr == nil {
		return ""
	}
	return fmt.Sprintf("; objects the signed revision refers to not collected, so none counts as new: %v", s.refsErr)
}

// references returns every object number rev refers to from an object its
// trailer reaches (ISO 32000-1, 7.5.5), defined or not. The walk is bounded by
// [maxReferenceWalk].
func references(ctx context.Context, rev *document.Document) (map[int]bool, error) {
	roots := make([]ast.PdfObject, 0, len(rev.Index.Trailer))
	for _, value := range rev.Index.Trailer {
		roots = append(roots, value)
	}
	refs := make(map[int]bool)
	err := walkReferences(ctx, rev, roots, func(ast.PdfObject) bool { return true }, make(map[int]bool), func(num int) {
		refs[num] = true
	})
	if err != nil {
		return nil, fmt.Errorf("walking the signed revision from its trailer: %w", err)
	}
	return refs, nil
}
