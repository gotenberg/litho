package writer

import (
	"context"
	"crypto/sha256"
	"fmt"
	"maps"
	"sort"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// DedupStreams merges byte-identical stream objects in the allocator, pointing
// every reference at one survivor and dropping the rest. It reports how many
// objects it removed.
//
// Only STREAMS are merged. A stream is pure data, so two that serialize
// identically are interchangeable wherever either is named. Dictionaries are
// deliberately excluded even when identical: structure elements, form fields
// and annotations carry identity that a reader resolves by object number, and
// collapsing two of those changes what the document means rather than how it
// is stored.
//
// The caller must not run this on an encrypted document. Encryption derives a
// per-object key from the object number (ISO 32000-1, 7.6.2), so two identical
// plaintext streams already hold different ciphertext by the time the write
// engine sees them, and merging them would hand a reader bytes decrypted under
// the wrong key. [WriteRewrite] refuses when the trailer carries /Encrypt,
// which also makes the no-op explicit rather than silent.
//
// Survivors are chosen by lowest object number so the result does not depend
// on map iteration order, as the reproducible-output rule requires.
func DedupStreams(ctx context.Context, alloc *ObjAllocator, trailer ast.PdfDict) (int, error) {
	pending := alloc.Pending()

	nums := make([]int, 0, len(pending))
	for num, obj := range pending {
		if _, isStream := obj.(ast.PdfStream); isStream {
			nums = append(nums, num)
		}
	}
	if len(nums) < 2 {
		return 0, nil
	}
	sort.Ints(nums)

	canonical := make(map[[sha256.Size]byte]int, len(nums))
	remap := make(map[int]int)
	for _, num := range nums {
		if err := ctx.Err(); err != nil {
			return 0, fmt.Errorf("hashing streams for dedup: %w", err)
		}
		// Serialized straight into the digest. Hashing a stream necessarily
		// reads its bytes, since serializeStream calls Bytes, so this does
		// not avoid loading the payload; what it avoids is a SECOND copy
		// held only to be hashed and discarded. Serialize emits dictionary
		// keys in sorted order, so two streams differing only in key order
		// still hash alike.
		h := sha256.New()
		if _, err := Serialize(ctx, h, pending[num]); err != nil {
			return 0, fmt.Errorf("serializing object %d for dedup: %w", num, err)
		}
		var sum [sha256.Size]byte
		copy(sum[:], h.Sum(nil))

		if first, seen := canonical[sum]; seen {
			remap[num] = first
			continue
		}
		canonical[sum] = num
	}
	if len(remap) == 0 {
		return 0, nil
	}

	// Repoint every surviving object, then the trailer. Missing the trailer
	// would leave /Root or /Info naming an object this function just deleted.
	for num, obj := range pending {
		if _, gone := remap[num]; gone {
			continue
		}
		if rewritten, changed := remapRefs(obj, remap); changed {
			alloc.AllocAt(num, rewritten)
		}
	}
	for key, val := range trailer {
		if rewritten, changed := remapRefs(val, remap); changed {
			trailer[key] = rewritten
		}
	}

	for num := range remap {
		delete(pending, num)
	}
	return len(remap), nil
}

// remapRefs rebuilds obj with every indirect reference in remap repointed at
// its survivor, reporting whether anything changed.
//
// The changed flag keeps the common case allocation-free: most objects name no
// duplicate, and rebuilding every dictionary in the document to discover that
// would cost more than the dedup saves.
func remapRefs(obj ast.PdfObject, remap map[int]int) (ast.PdfObject, bool) {
	switch v := obj.(type) {
	case ast.PdfIndirectRef:
		if to, ok := remap[v.ObjectNum]; ok {
			// A merged object is always written at generation 0, as every
			// object in a full rewrite is.
			return ast.PdfIndirectRef{ObjectNum: to, GenNum: 0}, true
		}
		return v, false

	case ast.PdfArray:
		var out ast.PdfArray
		changed := false
		for i, elem := range v {
			rewritten, elemChanged := remapRefs(elem, remap)
			if elemChanged && !changed {
				out = make(ast.PdfArray, len(v))
				copy(out, v)
				changed = true
			}
			if changed {
				out[i] = rewritten
			}
		}
		if !changed {
			return v, false
		}
		return out, true

	case ast.PdfDict:
		var out ast.PdfDict
		changed := false
		for key, val := range v {
			rewritten, valChanged := remapRefs(val, remap)
			if valChanged && !changed {
				out = make(ast.PdfDict, len(v))
				maps.Copy(out, v)
				changed = true
			}
			if changed {
				out[key] = rewritten
			}
		}
		if !changed {
			return v, false
		}
		return out, true

	case ast.PdfStream:
		// A stream dictionary can name another stream, an /SMask or an
		// ICC-based /ColorSpace among them, so skipping these would leave a
		// dangling reference to an object this pass deletes.
		rewritten, changed := remapRefs(v.Dict, remap)
		if !changed {
			return v, false
		}
		dict, ok := rewritten.(ast.PdfDict)
		if !ok {
			return v, false
		}
		// Rebuilt with the ORIGINAL loader rather than by reading the bytes:
		// PdfStream keeps its payload behind an unexported lazy loader, and
		// s.Bytes is a method value of exactly the signature NewLazyStream
		// takes. Materializing here instead would pull every embedded image
		// into memory, which the resource-streaming rule forbids. Same idiom
		// as [Copier.copyStream].
		return ast.NewLazyStream(dict, v.Bytes), true

	default:
		// Scalars and null are immutable and carry no reference.
		return obj, false
	}
}
