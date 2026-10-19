package content

import (
	"context"
	"testing"
)

// TestUnclosedMarkedContent pins where a stream's never-closed marked content
// starts and how many sequences it leaves open (ISO 32000-1, 14.6).
func TestUnclosedMarkedContent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	for _, tc := range []struct {
		name, data string
		tail, open int
	}{
		{"balanced", "/P BMC 0 0 m S EMC", 4, 0},
		{"no marked content", "0 0 m S", 2, 0},
		{"one open", "/P BMC S EMC /Artifact BMC 0 0 m S", 3, 1},
		{"open around a closed one", "/A BMC /B BMC S EMC", 0, 1},
		{"two open", "S /A BMC /B BMC S", 0, 2},
		{"stray EMC", "EMC S", 2, 0},
		{"stray EMC then open", "EMC /A BMC S", 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ops, err := ParseStreamData(ctx, []byte(tc.data))
			if err != nil {
				t.Fatalf("ParseStreamData: %v", err)
			}
			tail, open := UnclosedMarkedContent(ops)
			if tail != tc.tail || open != tc.open {
				t.Errorf("UnclosedMarkedContent(%q) = (%d, %d), want (%d, %d)", tc.data, tail, open, tc.tail, tc.open)
			}
		})
	}
}
