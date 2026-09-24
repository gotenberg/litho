package compliance

import (
	"context"
	"testing"
)

// TestRuleByPart pins that [ruleByPart] picks the rule for parts 1 to 4 and
// the zero Rule for any other part.
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

// TestViolationError pins the message [Violation.Error] formats, with and
// without a context.
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

// TestViolationsToErrors pins that [violationsToErrors] returns nil for no
// violations and one error per violation otherwise.
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

// TestViolationString pins that [Violation.String] matches [Violation.Error].
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

// TestFailHelpers pins the single-violation slices [fail] and [failf] build.
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

// TestPageCtxParsedOps pins that [pageCtx.parsedOps] returns nil for empty
// content and the same result on repeated calls.
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
