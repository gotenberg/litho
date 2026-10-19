package cmap

import (
	"context"
	"slices"
	"testing"
)

// TestNotdefCIDs pins the CIDs the notdef mappings of an embedded CMap and of
// the base its stream dictionary names select, each once and in order.
func TestNotdefCIDs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	base := ParseEmbeddedCIDMap(ctx, []byte(unmappedTestHead+
		"1 beginnotdefrange <0000> <00FF> 5 endnotdefrange\n"+unmappedTestTail))
	if base == nil {
		t.Fatal("the base CMap did not parse")
	}
	m := ParseEmbeddedCIDMapOver(ctx, []byte(unmappedTestHead+
		"2 beginnotdefrange <0100> <01FF> 9 <0200> <02FF> 5 endnotdefrange\n1 beginnotdefchar <0300> 3 endnotdefchar\n"+unmappedTestTail),
		Base{Map: base})
	if m == nil {
		t.Fatal("the CMap did not parse")
	}
	if got := m.NotdefCIDs(); !slices.Equal(got, []int{3, 5, 9}) {
		t.Errorf("NotdefCIDs = %v, want [3 5 9]", got)
	}
	var none *CIDMap
	if got := none.NotdefCIDs(); got != nil {
		t.Errorf("a nil map's NotdefCIDs = %v", got)
	}
}
