package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

func TestCheckFormXObjectPart1_OPI(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc := nopDoc()

	t.Run("form with /OPI fires 6.2.4-t02", func(t *testing.T) {
		t.Parallel()
		got := checkFormXObjectPart1(ctx, doc, ast.PdfDict{"OPI": ast.PdfDict{}}, "Fm0", 1)
		if !hasRule(got, Rule_19005_1_6_2_4_t02) {
			t.Errorf("form /OPI: got %v, want 6.2.4-t02", got)
		}
	})

	t.Run("form without /OPI is clean", func(t *testing.T) {
		t.Parallel()
		got := checkFormXObjectPart1(ctx, doc, ast.PdfDict{}, "Fm0", 1)
		if hasRule(got, Rule_19005_1_6_2_4_t02) {
			t.Errorf("clean form: got %v, want no 6.2.4-t02", got)
		}
	})
}
