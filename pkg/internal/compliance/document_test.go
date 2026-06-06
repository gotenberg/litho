package compliance

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

func TestCheckNeedsRenderingKey(t *testing.T) {
	t.Parallel()
	present := ast.PdfDict{"NeedsRendering": ast.PdfBool(true)}

	if v := checkNeedsRenderingKey(present, 2); len(v) != 1 ||
		v[0].Rule.Clause != "6.4.2" || v[0].Rule.TestNumber != 2 ||
		v[0].Rule.Specification != "ISO_19005_2" {
		t.Errorf("part 2 with /NeedsRendering: got %v, want one ISO_19005_2 §6.4.2 t02 violation", v)
	}
	if v := checkNeedsRenderingKey(present, 4); len(v) != 1 || v[0].Rule.Specification != "ISO_19005_4" {
		t.Errorf("part 4 with /NeedsRendering: got %v, want a PDF/A-4 violation", v)
	}
	if v := checkNeedsRenderingKey(ast.PdfDict{}, 2); len(v) != 0 {
		t.Errorf("catalog without /NeedsRendering must not flag, got %v", v)
	}
}
