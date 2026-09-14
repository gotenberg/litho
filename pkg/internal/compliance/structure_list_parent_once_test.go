package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestListContainmentStatedOncePerParent pins that the PDF/UA-1 list rules are
// stated once for the offending parent, however many of its children offend.
//
// veraPDF keys §7.2 t19 on the SEL and t20 on the SELI, and each test reads that
// element's whole kidsStandardTypes string, so one bad list element is one
// finding. litho visits children, so it needs the parent's identity to match:
// govdocs thread_400_400037.pdf holds 21 LI elements with exactly two offending
// children each, where litho reported 42 and veraPDF 21.
func TestListContainmentStatedOncePerParent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	count := func(vs []Violation, r Rule) int {
		n := 0
		for _, v := range vs {
			if v.Rule == r {
				n++
			}
		}
		return n
	}

	t.Run("two offending children of one LI report once", func(t *testing.T) {
		t.Parallel()
		var results []Violation
		state := &walkState{results: &results, profile: "PDF/UA-1", ua1ParentSeq: 7}
		for range 2 {
			validateListContainment(ctx, state, ast.PdfDict{}, "Span", "LI")
		}
		if n := count(results, Rule_14289_1_7_2_t20); n != 1 {
			t.Errorf("one LI must report §7.2 t20 once, got %d", n)
		}
	})

	t.Run("two different LI elements each report", func(t *testing.T) {
		t.Parallel()
		var results []Violation
		state := &walkState{results: &results, profile: "PDF/UA-1", ua1ParentSeq: 7}
		validateListContainment(ctx, state, ast.PdfDict{}, "Span", "LI")
		state.ua1ParentSeq = 8
		validateListContainment(ctx, state, ast.PdfDict{}, "Span", "LI")
		if n := count(results, Rule_14289_1_7_2_t20); n != 2 {
			t.Errorf("two LI elements must each report §7.2 t20, got %d", n)
		}
	})

	t.Run("two offending children of one L report once", func(t *testing.T) {
		t.Parallel()
		var results []Violation
		state := &walkState{results: &results, profile: "PDF/UA-1", ua1ParentSeq: 3}
		for range 2 {
			validateListContainment(ctx, state, ast.PdfDict{}, "P", "L")
		}
		if n := count(results, Rule_14289_1_7_2_t19); n != 1 {
			t.Errorf("one L must report §7.2 t19 once, got %d", n)
		}
	})

	t.Run("the two rules do not mask each other", func(t *testing.T) {
		t.Parallel()
		// Same parent sequence, different rules: both must still be stated.
		var results []Violation
		state := &walkState{results: &results, profile: "PDF/UA-1", ua1ParentSeq: 5}
		validateListContainment(ctx, state, ast.PdfDict{}, "P", "L")
		validateListContainment(ctx, state, ast.PdfDict{}, "Span", "LI")
		if n := count(results, Rule_14289_1_7_2_t19); n != 1 {
			t.Errorf("§7.2 t19 must still be stated, got %d", n)
		}
		if n := count(results, Rule_14289_1_7_2_t20); n != 1 {
			t.Errorf("§7.2 t20 must still be stated, got %d", n)
		}
	})
}
