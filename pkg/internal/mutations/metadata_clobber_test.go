package mutations

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// Mutations queued on one writer share a single allocator and run in order with
// no flush between them, so [SetMetadata] must read /Info through the allocator:
// its contract is that an empty field preserves "the existing value", and after
// [ClearMetadata] the existing value is the cleared one, not what the file
// carried on disk.
//
// Clearing metadata is a privacy action. Reading /Info from the document put
// every field the caller asked to remove straight back into the output, which
// is worse than losing an edit: it restores exactly what was meant to be
// destroyed, and reports success doing it.
//
// Nothing else catches this. Mutations have no fix-to-fixpoint loop to notice a
// discarded write, the corpus gate scores no metadata-set cell, and every
// existing ClearMetadata test calls it alone rather than alongside SetMetadata.
func TestSetMetadata_DoesNotResurrectClearedFields(t *testing.T) {
	ctx := context.Background()
	doc := metadataFixture(t, true, true)
	alloc := writer.NewObjAllocator(20)

	// The fixture's /Info (object 4) carries /Title and /Author.
	if err := ClearMetadata(ctx, alloc, doc); err != nil {
		t.Fatalf("ClearMetadata: %v", err)
	}
	if cleared := pendingDictAt(t, alloc, 4); len(cleared) != 0 {
		t.Fatalf("precondition: ClearMetadata left %v, want an empty /Info", cleared)
	}

	if err := SetMetadata(ctx, alloc, doc, Metadata{Title: "New Title"}); err != nil {
		t.Fatalf("SetMetadata: %v", err)
	}

	info := pendingDictAt(t, alloc, 4)

	// SetMetadata's own job must still be done: this is a composition test, not
	// an assertion that it writes nothing.
	if _, ok := info["Title"]; !ok {
		t.Errorf("/Title missing, SetMetadata did not write the field it was given: %v", info)
	}

	// One field was set on a cleared dictionary, so one field is all there
	// should be. Asserting the count rather than /Author alone catches every
	// resurrected key, not just the one this fixture happens to carry.
	if len(info) != 1 {
		t.Errorf("/Info has %d entries, want exactly 1: SetMetadata rebuilt it from the "+
			"on-disk copy and resurrected fields ClearMetadata removed; /Info = %v",
			len(info), info)
	}
	if _, back := info["Author"]; back {
		t.Errorf("/Author came back after ClearMetadata removed it: %v", info)
	}
}
