package mutations

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

func TestPlacementSpan(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, data, want string
	}{
		{"group of state alone", "BT ET q /GS0 gs 0 g 2 0 0 1 5 5 cm /Im1 Do Q 1 w", " q /GS0 gs 0 g 2 0 0 1 5 5 cm /Im1 Do Q"},
		{"group that paints more", "q 0 0 5 5 re f 2 0 0 1 5 5 cm /Im1 Do Q", " /Im1 Do"},
		{"group marking content", "q /P BMC 2 0 0 1 5 5 cm /Im1 Do EMC Q", " /Im1 Do"},
		{"nested group", "q q 2 0 0 1 5 5 cm Q /Im1 Do Q", " /Im1 Do"},
		{"no group", "2 0 0 1 5 5 cm /Im1 Do", " /Im1 Do"},
		{"group never closed", "q 2 0 0 1 5 5 cm /Im1 Do", " /Im1 Do"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data := []byte(tc.data)
			ops, ranges, err := content.ParseStreamDataWithOffsets(context.Background(), data)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			op := -1
			for i, o := range ops {
				if o.Operator == "Do" {
					op = i
				}
			}
			span := placementSpan(ops, ranges, op)
			if got := string(data[span.Start:span.End]); got != tc.want {
				t.Errorf("span = %q, want %q", got, tc.want)
			}
		})
	}
}
