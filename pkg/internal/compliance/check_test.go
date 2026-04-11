package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

func TestRuleByPart(t *testing.T) {
	t.Parallel()

	r1 := Rule{Specification: "r1"}
	r2 := Rule{Specification: "r2"}
	r3 := Rule{Specification: "r3"}
	r4 := Rule{Specification: "r4"}

	tests := []struct {
		name string
		part int
		want string
	}{
		{"part 1", 1, "r1"},
		{"part 2", 2, "r2"},
		{"part 3", 3, "r3"},
		{"part 4", 4, "r4"},
		{"part 0", 0, ""},
		{"part 5", 5, ""},
		{"part -1", -1, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ruleByPart(tc.part, r1, r2, r3, r4)
			if got.Specification != tc.want {
				t.Errorf("ruleByPart(%d, ...) = %q, want %q", tc.part, got.Specification, tc.want)
			}
		})
	}
}

func TestViolationError(t *testing.T) {
	t.Parallel()

	t.Run("with context", func(t *testing.T) {
		t.Parallel()
		v := Violation{
			Rule:    Rule{Clause: "6.1", TestNumber: 1, Description: "test desc"},
			Context: "page 1",
		}
		got := v.Error()
		if got != "clause 6.1 test 1: test desc [page 1]" {
			t.Errorf("unexpected Error(): %s", got)
		}
	})

	t.Run("without context", func(t *testing.T) {
		t.Parallel()
		v := Violation{
			Rule: Rule{Clause: "6.1", TestNumber: 1, Description: "test desc"},
		}
		got := v.Error()
		if got != "clause 6.1 test 1: test desc" {
			t.Errorf("unexpected Error(): %s", got)
		}
	})
}

func TestViolationsToErrors(t *testing.T) {
	t.Parallel()

	t.Run("nil on empty", func(t *testing.T) {
		t.Parallel()
		if got := violationsToErrors(nil); got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})

	t.Run("converts violations", func(t *testing.T) {
		t.Parallel()
		vs := []Violation{
			{Rule: Rule{Clause: "1"}, Context: "a"},
			{Rule: Rule{Clause: "2"}, Context: "b"},
		}
		errs := violationsToErrors(vs)
		if len(errs) != 2 {
			t.Fatalf("expected 2 errors, got %d", len(errs))
		}
	})
}

func TestViolationString(t *testing.T) {
	t.Parallel()
	v := Violation{
		Rule:    Rule{Clause: "6.1", TestNumber: 1, Description: "test desc"},
		Context: "page 1",
	}
	if v.String() != v.Error() {
		t.Errorf("String() = %q, want same as Error() = %q", v.String(), v.Error())
	}
}

func TestFailHelpers(t *testing.T) {
	t.Parallel()

	t.Run("fail", func(t *testing.T) {
		t.Parallel()
		r := Rule{Clause: "6.1", TestNumber: 1}
		vs := fail(r, "ctx")
		if len(vs) != 1 || vs[0].Context != "ctx" || vs[0].Rule.Clause != "6.1" {
			t.Errorf("unexpected fail result: %v", vs)
		}
	})

	t.Run("failf", func(t *testing.T) {
		t.Parallel()
		r := Rule{Clause: "6.2", TestNumber: 2}
		vs := failf(r, "page %d", 5)
		if len(vs) != 1 || vs[0].Context != "page 5" {
			t.Errorf("unexpected failf result: %v", vs)
		}
	})
}

func TestOpsReferenceResources(t *testing.T) {
	t.Parallel()

	t.Run("empty ops", func(t *testing.T) {
		t.Parallel()
		if opsReferenceResources(nil) {
			t.Error("expected false for nil ops")
		}
	})

	t.Run("no resource references", func(t *testing.T) {
		t.Parallel()
		ops := []content.Operation{
			{Operator: "q"},
			{Operator: "Q"},
			{Operator: "m", Operands: []ast.PdfObject{ast.PdfReal(0), ast.PdfReal(0)}},
		}
		if opsReferenceResources(ops) {
			t.Error("expected false for non-resource ops")
		}
	})

	t.Run("has Tf reference", func(t *testing.T) {
		t.Parallel()
		ops := []content.Operation{
			{Operator: "Tf", Operands: []ast.PdfObject{ast.PdfName("F1"), ast.PdfInt(12)}},
		}
		if !opsReferenceResources(ops) {
			t.Error("expected true for Tf with name operand")
		}
	})

	t.Run("has Do reference", func(t *testing.T) {
		t.Parallel()
		ops := []content.Operation{
			{Operator: "Do", Operands: []ast.PdfObject{ast.PdfName("Im1")}},
		}
		if !opsReferenceResources(ops) {
			t.Error("expected true for Do with name operand")
		}
	})

	t.Run("has cs reference", func(t *testing.T) {
		t.Parallel()
		ops := []content.Operation{
			{Operator: "cs", Operands: []ast.PdfObject{ast.PdfName("DeviceRGB")}},
		}
		if !opsReferenceResources(ops) {
			t.Error("expected true for cs with name operand")
		}
	})
}

func TestPageCtxParsedOps(t *testing.T) {
	t.Parallel()

	t.Run("nil content", func(t *testing.T) {
		t.Parallel()
		p := &pageCtx{}
		ops := p.parsedOps(context.Background())
		if ops != nil {
			t.Errorf("expected nil ops for empty content, got %d", len(ops))
		}
	})

	t.Run("cached result", func(t *testing.T) {
		t.Parallel()
		p := &pageCtx{content: []byte("q Q")}
		ops1 := p.parsedOps(context.Background())
		ops2 := p.parsedOps(context.Background())
		if len(ops1) != len(ops2) {
			t.Error("expected same result on repeated calls")
		}
	})
}
