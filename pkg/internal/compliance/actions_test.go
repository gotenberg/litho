package compliance

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

func TestForbiddenActionTypeMaps(t *testing.T) {
	t.Parallel()

	// Verify core forbidden types present in all maps.
	coreTypes := []ast.PdfName{
		"Launch", "Sound", "Movie", "ResetForm", "ImportData",
		"JavaScript", "Hide", "SetState", "NoOp", "NOP",
	}

	t.Run("base map", func(t *testing.T) {
		t.Parallel()
		for _, at := range coreTypes {
			if !forbiddenActionTypes[at] {
				t.Errorf("forbiddenActionTypes missing %q", at)
			}
		}
	})

	t.Run("part 2/3 map extends base", func(t *testing.T) {
		t.Parallel()
		for _, at := range coreTypes {
			if !forbiddenActionTypesPart23[at] {
				t.Errorf("forbiddenActionTypesPart23 missing %q", at)
			}
		}
		extras := []ast.PdfName{"SetOCGState", "Rendition", "Trans", "GoTo3DView"}
		for _, at := range extras {
			if !forbiddenActionTypesPart23[at] {
				t.Errorf("forbiddenActionTypesPart23 missing %q", at)
			}
		}
	})

	t.Run("part 4 map extends base", func(t *testing.T) {
		t.Parallel()
		for _, at := range coreTypes {
			if !forbiddenActionTypesPart4[at] {
				t.Errorf("forbiddenActionTypesPart4 missing %q", at)
			}
		}
		extras := []ast.PdfName{"Rendition", "Trans", "SetOCGState", "GoTo3DView"}
		for _, at := range extras {
			if !forbiddenActionTypesPart4[at] {
				t.Errorf("forbiddenActionTypesPart4 missing %q", at)
			}
		}
	})

	t.Run("GoTo is allowed", func(t *testing.T) {
		t.Parallel()
		for _, m := range []map[ast.PdfName]bool{forbiddenActionTypes, forbiddenActionTypesPart23, forbiddenActionTypesPart4} {
			if m["GoTo"] {
				t.Error("GoTo should not be forbidden")
			}
		}
	})
}

func TestAllowedNamedActions(t *testing.T) {
	t.Parallel()
	expected := []ast.PdfName{"NextPage", "PrevPage", "FirstPage", "LastPage"}
	for _, n := range expected {
		if !allowedNamedActions[n] {
			t.Errorf("allowedNamedActions missing %q", n)
		}
	}
	if allowedNamedActions["Print"] {
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
