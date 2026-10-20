package graphics

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

func testMachine() *Machine {
	return NewMachine([4]float64{0, 0, 612, 792})
}

func TestUnknownOperators_SilentByDefault(t *testing.T) {
	m := testMachine()
	ops := []content.Operation{
		{Operator: "Do"},
		{Operator: "sh"},
		{Operator: "q"},
	}

	if err := m.Execute(context.Background(), ops); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if got := m.UnknownOperators(); got != nil {
		t.Errorf("UnknownOperators() = %v, want nil while recording is off", got)
	}
}

func TestUnknownOperators_RecordedWhenAsked(t *testing.T) {
	m := testMachine()
	m.RecordUnknownOperators = true
	// BX rather than Do: Do gained a handler once the machine started recording
	// image placement, so it is modelled now and must not be reported here.
	ops := []content.Operation{
		{Operator: "sh"},
		{Operator: "BX"},
		{Operator: "q"}, // handled, must not be reported
		{Operator: "BX"},
		{Operator: "gs"}, // handled since the machine models ExtGState
		{Operator: "sh"},
	}

	if err := m.Execute(context.Background(), ops); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// Ordered by keyword, so the result does not depend on map iteration.
	want := []UnknownOperator{
		{Operator: "BX", Count: 2},
		{Operator: "sh", Count: 2},
	}
	got := m.UnknownOperators()
	if len(got) != len(want) {
		t.Fatalf("UnknownOperators() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("UnknownOperators()[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestUnknownOperators_HandledOperatorsNeverRecorded(t *testing.T) {
	m := testMachine()
	m.RecordUnknownOperators = true

	ops := make([]content.Operation, 0, len(opHandlers))
	for name := range opHandlers {
		ops = append(ops, content.Operation{Operator: name})
	}

	if err := m.Execute(context.Background(), ops); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if got := m.UnknownOperators(); got != nil {
		t.Errorf("UnknownOperators() = %v, want nil: every operator executed has a handler", got)
	}
}

func TestUnknownOperators_CapBoundsDistinctKeywords(t *testing.T) {
	m := testMachine()
	m.RecordUnknownOperators = true

	// A crafted stream can invent unlimited keywords, so the retained set is
	// bounded while occurrences of a keyword already held keep counting.
	ops := make([]content.Operation, 0, maxRecordedUnknownOperators+11)
	for i := range maxRecordedUnknownOperators + 10 {
		ops = append(ops, content.Operation{Operator: fmt.Sprintf("zz%03d", i)})
	}
	ops = append(ops, content.Operation{Operator: "zz000"})

	if err := m.Execute(context.Background(), ops); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	got := m.UnknownOperators()
	if len(got) != maxRecordedUnknownOperators {
		t.Errorf("retained %d distinct keywords, want the cap of %d", len(got), maxRecordedUnknownOperators)
	}
	if got[0].Operator != "zz000" {
		t.Fatalf("first keyword = %q, want zz000 (the result must be ordered)", got[0].Operator)
	}
	if got[0].Count != 2 {
		t.Errorf("zz000 counted %d times, want 2: the cap bounds distinct keywords, not occurrences", got[0].Count)
	}
}

func TestUnknownOperators_EmptyKeywordIgnored(t *testing.T) {
	m := testMachine()
	m.RecordUnknownOperators = true

	if err := m.Execute(context.Background(), []content.Operation{{Operator: ""}}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if got := m.UnknownOperators(); got != nil {
		t.Errorf("UnknownOperators() = %v, want nil for an empty keyword", got)
	}
}
