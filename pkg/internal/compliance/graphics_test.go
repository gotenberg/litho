package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestCheckHTPKeyPartScoping pins which halftone key each part forbids.
// ISO 19005-2 and -3 §6.2.5 t3 read "containsHTP == false"; ISO 19005-4
// §6.2.5 t3 reads "containsHTO == false". Checking both keys under every
// part reported /HTP against part 4 and /HTO against parts 2 and 3, neither
// of which the governing rule constrains.
func TestCheckHTPKeyPartScoping(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	fires := func(key string, part int) bool {
		gs := ast.PdfDict{ast.PdfName(key): ast.PdfInt(1)}
		return len(checkHTPKey(ctx, nil, gs, "", part, 1, "GS0")) > 0
	}

	for _, part := range []int{2, 3} {
		if !fires("HTP", part) {
			t.Errorf("part %d: /HTP must fire §6.2.5 t3", part)
		}
		if fires("HTO", part) {
			t.Errorf("part %d: /HTO is not constrained, want no violation", part)
		}
	}
	if !fires("HTO", 4) {
		t.Error("part 4: /HTO must fire §6.2.5 t3")
	}
	if fires("HTP", 4) {
		t.Error("part 4: /HTP is not constrained, want no violation")
	}
	if fires("HTP", 1) {
		t.Error("part 1: the clause does not apply, want no violation")
	}
}

// TestDeviceNComponentRulePart4 pins that ISO 19005-4 states no DeviceN
// colourant limit. Returning the part-1 rule id there reported a construct
// that part permits.
func TestDeviceNComponentRulePart4(t *testing.T) {
	t.Parallel()

	if got := deviceNComponentRule(4); got.Specification != "" {
		t.Errorf("part 4 has no DeviceN colourant rule: got %v, want none", got)
	}
	if got := deviceNComponentRule(2); got.Specification == "" {
		t.Error("part 2 must keep §6.1.13 t9")
	}
	if got := deviceNComponentRule(1); got.Specification == "" {
		t.Error("part 1 must keep §6.1.12 t9")
	}
}
