package compliance

import (
	"bytes"
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// runGroupsFixture opens the minimal compliance fixture and resolves its
// catalog, the two inputs [runCheckGroups] takes besides the groups.
func runGroupsFixture(t *testing.T) (*document.Document, ast.PdfDict) {
	t.Helper()
	data := minimalPDFForCompliance()
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := resolveCatalog(context.Background(), doc)
	if err != nil {
		t.Fatal(err)
	}
	return doc, catalog
}

// TestRunCheckGroups_PanicBecomesError pins that a panicking check group is
// recovered on its own goroutine and surfaces as an error wrapping
// [document.ErrInternalPanic]. The public API's recover runs on the calling
// goroutine, so before this the panic ended the process.
func TestRunCheckGroups_PanicBecomesError(t *testing.T) {
	t.Parallel()
	doc, catalog := runGroupsFixture(t)

	ok := func(context.Context, *validationCtx) []Violation {
		return []Violation{{Rule: Rule_19005_4_6_1_12_t01, Context: "ok"}}
	}
	boom := func(context.Context, *validationCtx) []Violation {
		panic("boom")
	}

	violations, err := runCheckGroups(context.Background(), doc, catalog, "PDF/A-2b", []checkGroup{ok, boom})
	if !errors.Is(err, document.ErrInternalPanic) {
		t.Fatalf("err = %v, want one wrapping document.ErrInternalPanic", err)
	}
	if violations != nil {
		t.Errorf("violations = %v, want nil alongside the error", violations)
	}
}

// TestRunCheckGroups_CancelWaitsAndReturnsContextError pins the cancellation
// contract: the run returns the context error rather than the partial
// violations the groups stopped at, and only after every group has returned,
// so no goroutine outlives the call.
func TestRunCheckGroups_CancelWaitsAndReturnsContextError(t *testing.T) {
	t.Parallel()
	doc, catalog := runGroupsFixture(t)

	ctx, cancel := context.WithCancel(context.Background())
	const n = 4
	var started, returned atomic.Int32
	group := func(ctx context.Context, _ *validationCtx) []Violation {
		defer returned.Add(1)
		if started.Add(1) == n {
			cancel()
		}
		<-ctx.Done()
		return []Violation{{Rule: Rule_19005_4_6_1_12_t01, Context: "partial"}}
	}
	groups := make([]checkGroup, n)
	for i := range groups {
		groups[i] = group
	}

	violations, err := runCheckGroups(ctx, doc, catalog, "PDF/A-2b", groups)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if violations != nil {
		t.Errorf("violations = %v, want nil: partial results read as a verdict", violations)
	}
	if got := returned.Load(); got != n {
		t.Errorf("%d of %d groups had returned when runCheckGroups did", got, n)
	}
}

// TestRunCheckGroups_OrderIndependentOfScheduling pins that violations come
// back in group order whatever order the groups finish in. Group 0 waits for
// group 1 to finish, so the results arrive reversed.
func TestRunCheckGroups_OrderIndependentOfScheduling(t *testing.T) {
	t.Parallel()
	doc, catalog := runGroupsFixture(t)

	secondDone := make(chan struct{})
	first := func(context.Context, *validationCtx) []Violation {
		<-secondDone
		return []Violation{{Rule: Rule_19005_4_6_1_12_t01, Context: "first"}}
	}
	second := func(context.Context, *validationCtx) []Violation {
		defer close(secondDone)
		return []Violation{{Rule: Rule_19005_4_6_1_12_t01, Context: "second"}}
	}

	violations, err := runCheckGroups(context.Background(), doc, catalog, "PDF/A-2b", []checkGroup{first, second})
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 2 || violations[0].Context != "first" || violations[1].Context != "second" {
		t.Errorf("violations = %v, want [first second]", violations)
	}
}

// TestRunCheckGroups_GroupsIgnoringCancellationStillError pins that a run
// whose context is already cancelled returns the context error even when every
// group ignores ctx and delivers a complete-looking result. The select in
// [runCheckGroups] usually catches it; the check after collection covers the
// receives that win the race against ctx.Done().
func TestRunCheckGroups_GroupsIgnoringCancellationStillError(t *testing.T) {
	t.Parallel()
	doc, catalog := runGroupsFixture(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	groups := make([]checkGroup, 16)
	for i := range groups {
		groups[i] = func(context.Context, *validationCtx) []Violation {
			return []Violation{{Rule: Rule_19005_4_6_1_12_t01, Context: "complete-looking"}}
		}
	}

	violations, err := runCheckGroups(ctx, doc, catalog, "PDF/A-2b", groups)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if violations != nil {
		t.Errorf("violations = %v, want nil", violations)
	}
}
