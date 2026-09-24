package document

import (
	"context"
	"fmt"
	"io"
)

// Revision returns the document as it stood when the file ended at byte end:
// the revision that incremental updates (ISO 32000-1, 7.5.6) appended after
// end leave untouched. A signature's /ByteRange ends at such an offset, and
// 12.8.2.2.2 compares "the signed and current versions of the document".
//
// The revision is read from the last startxref before end, through the /Prev
// chain, exactly as [NewDocument] reads a file. It reports [ErrNoRevision] when
// that fails and only a raw-scan rebuild could read the bytes: a rebuild
// guesses at objects, and a caller comparing revisions needs the one the bytes
// state.
//
// The revision reads the same underlying file under the same budgets, logger,
// filter plugins and CMYK profile. When it is encrypted and the empty user
// password does not open it, it takes this document's security handler if
// both carry the same parameters ([crypto.Handler.SameParameters]): the key
// then derives from the same password, so it is the key the revision needs.
//
// The returned [Document] is separate, with its own caches, and honours the
// same concurrency contract.
func (d *Document) Revision(ctx context.Context, end int64) (*Document, error) {
	if d.r == nil || end <= 0 || end > d.size {
		return nil, fmt.Errorf("%w: byte %d is outside the %d-byte file", ErrNoRevision, end, d.size)
	}
	rev, err := NewDocument(ctx, io.NewSectionReader(d.r, 0, end), end, d.Logger, WithLimits(d.Limits()))
	if err != nil {
		return nil, fmt.Errorf("reading the revision that ends at byte %d: %w", end, err)
	}
	if rev.Rebuilt {
		return nil, fmt.Errorf("%w: no cross-reference section ends the first %d bytes", ErrNoRevision, end)
	}
	rev.Filters = d.Filters
	rev.CMYKProfile = d.CMYKProfile
	if rev.IsEncrypted && !rev.Authenticated && d.Authenticated &&
		d.Crypto != nil && rev.Crypto != nil && d.Crypto.SameParameters(rev.Crypto) {
		rev.Crypto = d.Crypto
		rev.Authenticated = true
		rev.resetParsedObjectCaches()
	}
	return rev, nil
}
