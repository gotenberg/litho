package text

import (
	"slices"
	"strconv"
	"strings"
	"testing"
)

// familyCacheLen returns how many lists the process-wide table holds.
func familyCacheLen() int {
	familyCache.mu.RLock()
	defer familyCache.mu.RUnlock()
	return len(familyCache.m)
}

// TestResolvedFamilies_BoundsWhatADocumentParks checks the two bounds of
// the table that outlives a conversion: a list whose names pass
// maxFamilyKeyBytes resolves to the same keys and is not kept, and a table
// that is full is emptied, never grown past maxFamilyCache.
//
// It is not parallel: it fills the table every other test reads.
func TestResolvedFamilies_BoundsWhatADocumentParks(t *testing.T) {
	long := Style{Families: []string{strings.Repeat("x", maxFamilyKeyBytes), "serif"}}
	before := familyCacheLen()
	first := resolvedFamilies(long)
	second := resolvedFamilies(long)
	if familyCacheLen() != before {
		t.Errorf("a list of %d bytes of names was kept", maxFamilyKeyBytes+len("serif"))
	}
	if !slices.Equal(first.match, second.match) || !slices.Equal(first.requested, second.requested) || len(first.match) == 0 {
		t.Errorf("a list too long to keep resolves to %v then %v", first.match, second.match)
	}

	for i := range 2*maxFamilyCache + 10 {
		resolvedFamilies(Style{Families: []string{"family-" + strconv.Itoa(i)}})
		if n := familyCacheLen(); n > maxFamilyCache {
			t.Fatalf("the table holds %d lists, past its cap of %d", n, maxFamilyCache)
		}
	}
	kept := Style{Families: []string{"Liberation Sans", "sans-serif"}, Lang: "en"}
	want := resolvedFamilies(kept)
	if got := resolvedFamilies(kept); !slices.Equal(got.match, want.match) {
		t.Errorf("a kept list resolves to %v then %v", want.match, got.match)
	}
}
