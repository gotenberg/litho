package compliance

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

func TestIsForbiddenAction(t *testing.T) {
	t.Parallel()

	// Core forbidden types should be forbidden for all parts.
	coreTypes := []ast.PdfName{
		"Launch", "Sound", "Movie", "ResetForm", "ImportData",
		"JavaScript", "Hide", "SetState", "NoOp", "NOP",
	}

	t.Run("base types forbidden for part 1", func(t *testing.T) {
		t.Parallel()
		for _, at := range coreTypes {
			if !isForbiddenAction(at, 1) {
				t.Errorf("isForbiddenAction(%q, 1) = false, want true", at)
			}
		}
	})

	t.Run("part 2/3 extras forbidden", func(t *testing.T) {
		t.Parallel()
		for _, at := range coreTypes {
			if !isForbiddenAction(at, 2) {
				t.Errorf("isForbiddenAction(%q, 2) = false, want true", at)
			}
		}
		extras := []ast.PdfName{"SetOCGState", "Rendition", "Trans", "GoTo3DView"}
		for _, at := range extras {
			if !isForbiddenAction(at, 2) {
				t.Errorf("isForbiddenAction(%q, 2) = false, want true", at)
			}
			if !isForbiddenAction(at, 3) {
				t.Errorf("isForbiddenAction(%q, 3) = false, want true", at)
			}
		}
	})

	t.Run("part 4 extras forbidden", func(t *testing.T) {
		t.Parallel()
		for _, at := range coreTypes {
			if !isForbiddenAction(at, 4) {
				t.Errorf("isForbiddenAction(%q, 4) = false, want true", at)
			}
		}
		extras := []ast.PdfName{"Rendition", "Trans", "SetOCGState", "GoTo3DView"}
		for _, at := range extras {
			if !isForbiddenAction(at, 4) {
				t.Errorf("isForbiddenAction(%q, 4) = false, want true", at)
			}
		}
	})

	t.Run("GoTo is allowed", func(t *testing.T) {
		t.Parallel()
		for _, part := range []int{1, 2, 3, 4} {
			if isForbiddenAction("GoTo", part) {
				t.Errorf("isForbiddenAction(GoTo, %d) = true, want false", part)
			}
		}
	})

	t.Run("part 2 extras not forbidden for part 1", func(t *testing.T) {
		t.Parallel()
		extras := []ast.PdfName{"SetOCGState", "Rendition", "Trans", "GoTo3DView"}
		for _, at := range extras {
			if isForbiddenAction(at, 1) {
				t.Errorf("isForbiddenAction(%q, 1) = true, want false", at)
			}
		}
	})
}

func TestAllowedNamedActions(t *testing.T) {
	t.Parallel()
	expected := []ast.PdfName{"NextPage", "PrevPage", "FirstPage", "LastPage"}
	for _, n := range expected {
		if !AllowedNamedActions[n] {
			t.Errorf("AllowedNamedActions missing %q", n)
		}
	}
	if AllowedNamedActions["Print"] {
		t.Error("Print should not be an allowed named action")
	}
}

func TestActionKeysPresent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		hasA, hasAA bool
		want        string
	}{
		{"both", true, true, "A, AA"},
		{"only A", true, false, "A"},
		{"only AA", false, true, "AA"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := actionKeysPresent(tc.hasA, tc.hasAA); got != tc.want {
				t.Errorf("actionKeysPresent(%v, %v) = %q, want %q", tc.hasA, tc.hasAA, got, tc.want)
			}
		})
	}
}
