package document

import (
	"sync/atomic"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// objectCache holds the objects [Document.resolveWith] parsed, by object
// number, up to a count the caller gives at each insertion. Once full, an
// insertion replaces one object, chosen by a clock: each object carries a bit
// a lookup sets, and the hand passes over the objects in insertion order,
// clearing the bits it finds set and replacing the first object whose bit is
// clear. An object read since the hand last passed it therefore stays for
// another turn, and one nothing read leaves first.
//
// The callers walk a document in two ways. Validation, rewriting and
// splitting sweep every object in object-number order, several times for a
// validation, and between and during the sweeps they walk the page tree, the
// structure tree, fonts and resources, which a few objects many of the
// others reference. A sweep larger than the cache gains nothing from any
// order that keeps what was read last, since each object comes back only
// after every other one: emptying the cache when full, or replacing the least
// recently read object, then parse the same objects again. What an order can
// keep is the objects the walks read again and again. The clock keeps them
// as a least-recently-read order would, and a lookup that hits only sets a
// bit, so hits stay under the document's read lock while concurrent check
// groups resolve.
//
// The zero value is empty and ready. The caller holds [Document.mu]: a read
// lock for [objectCache.lookup], the write lock for the other methods.
//
// # Corpus evidence
//
//   - pdfjs pdf.pdf: 334,092 objects. Its resolutions, recorded and
//     replayed against a cache of 50,000 objects: emptying the cache when
//     full parses 3,167,812 objects for a PDF/A-2b validation, 2,686,207 for
//     PDF/UA-1, 588,374 for splitting half the pages and 356,689 for a full
//     rewrite. The clock parses 3,115,799, 2,633,694, 575,606 and 356,689; a
//     least-recently-read order 3,126,714, 2,650,217, 576,355 and 356,689.
//     An order that knew every future lookup would still parse 2,581,729,
//     2,098,580, 372,439 and 334,089. Admitting an object only when it was
//     read more often than the one it replaces parses more on both
//     validations, 3,226,417 and 3,136,215, and a segmented order that
//     protects objects read twice 387,320 on the rewrite.
type objectCache struct {
	index map[int]int32 // slot of each cached object number
	slots []cacheSlot
	hand  int // next slot the clock examines
}

// cacheSlot is one cached object and its clock bit.
type cacheSlot struct {
	num int
	obj ast.PdfObject
	// read is set by a lookup and cleared by the passing hand. It is atomic
	// because lookups set it under the read lock, concurrently.
	read atomic.Bool
}

// lookup returns the object cached for num and marks it read. The caller
// holds at least the read lock.
func (c *objectCache) lookup(num int) (ast.PdfObject, bool) {
	i, ok := c.index[num]
	if !ok {
		return nil, false
	}
	s := &c.slots[i]
	if !s.read.Load() {
		s.read.Store(true)
	}
	return s.obj, true
}

// insert caches obj for num, which the cache does not hold, replacing an
// object when the cache already holds limit objects. A limit of zero or less
// bounds nothing. The caller holds the write lock.
func (c *objectCache) insert(num int, obj ast.PdfObject, limit int) {
	if c.index == nil {
		c.index = make(map[int]int32)
	}
	if limit <= 0 || len(c.slots) < limit {
		c.index[num] = int32(len(c.slots)) //nolint:gosec // fits int32: below limit, at most MaxCachedObjectsCeiling
		c.slots = append(c.slots, cacheSlot{num: num, obj: obj})
		return
	}
	if c.hand >= len(c.slots) {
		c.hand = 0
	}
	// The hand passes each slot at most once with its bit set, so it stops
	// within one turn.
	for c.slots[c.hand].read.Load() {
		c.slots[c.hand].read.Store(false)
		c.hand = (c.hand + 1) % len(c.slots)
	}
	s := &c.slots[c.hand]
	delete(c.index, s.num)
	s.num, s.obj = num, obj
	c.index[num] = int32(c.hand) //nolint:gosec // fits int32: a slot index, below limit
	c.hand = (c.hand + 1) % len(c.slots)
}
