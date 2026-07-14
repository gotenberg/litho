package cmap

import "testing"

// TestPredefinedCMapCIDSystemInfo checks the CIDSystemInfo resolved for named
// predefined CMaps against veraPDF's CharacterCollections table, including the
// generation-dependent entries whose supplement (and sometimes ordering) widen
// under ISO 32000-2.
func TestPredefinedCMapCIDSystemInfo(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		pdf20      bool
		registry   string
		ordering   string
		supplement int
		ok         bool
	}{
		// ISO 32000-1 era (PDF/A-1/2/3): the Unicode CMaps carry the older,
		// lower supplements veraPDF validates against.
		{"UniKS-UTF16-H", false, "Adobe", "Korea1", 2, true},
		{"UniJIS-UTF16-H", false, "Adobe", "Japan1", 5, true},
		{"UniGB-UTF16-H", false, "Adobe", "GB1", 4, true},
		{"90ms-RKSJ-H", false, "Adobe", "Japan1", 2, true},
		{"GBpc-EUC-H", false, "Adobe", "GB1", 0, true},
		// ISO 32000-2 (PDF/A-4): UniKS-UTF16-H widens to the Adobe-KR
		// collection; UniJIS/UniGB advance to higher supplements.
		{"UniKS-UTF16-H", true, "Adobe", "KR", 9, true},
		{"UniJIS-UTF16-H", true, "Adobe", "Japan1", 7, true},
		{"UniGB-UTF16-H", true, "Adobe", "GB1", 5, true},
		// Not predefined encoding CMaps: Identity is handled separately, and a
		// CIDSystemInfo-shaped name is not a CMap name.
		{"Identity-H", false, "", "", 0, false},
		{"Adobe-Japan1-2", false, "", "", 0, false},
		{"NotACMap", false, "", "", 0, false},
	}
	for _, c := range cases {
		reg, ord, sup, ok := PredefinedCMapCIDSystemInfo(c.name, c.pdf20)
		if ok != c.ok || reg != c.registry || ord != c.ordering || sup != c.supplement {
			t.Errorf("PredefinedCMapCIDSystemInfo(%q, pdf20=%v) = (%q, %q, %d, %v); want (%q, %q, %d, %v)",
				c.name, c.pdf20, reg, ord, sup, ok, c.registry, c.ordering, c.supplement, c.ok)
		}
	}
}
