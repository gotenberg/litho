package remediation

import (
	"context"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestRemediateDisabledFixers pins the contract a writer rolling back the
// content tier relies on: the report names the content-tier fixers a rollback
// to its snapshot discards, and a run that disables them leaves their repairs
// unmade and runs the others.
func TestRemediateDisabledFixers(t *testing.T) {
	t.Parallel()

	// Object 4 of the document is the page content, whose "-" operands the
	// unknown-ops repair respells.
	const contentObj = 4
	ctx := context.Background()
	doc, alloc := newTestDoc(t, malformedNumberPDF("-"))
	rep, err := RemediatePDFA(ctx, doc, alloc, models.PDFA1b, Options{PreserveContent: true})
	if err != nil {
		t.Fatalf("RemediatePDFA: %v", err)
	}
	if !slices.Contains(rep.ContentFixers, "unknown-ops") || rep.ContentSnapshot == nil {
		t.Fatalf("ContentFixers = %q, want unknown-ops among them, with a snapshot", rep.ContentFixers)
	}
	if _, rewritten := alloc.Pending()[contentObj]; !rewritten {
		t.Fatal("the run did not repair the page content")
	}

	doc, alloc = newTestDoc(t, malformedNumberPDF("-"))
	off, err := RemediatePDFA(ctx, doc, alloc, models.PDFA1b, Options{PreserveContent: true}.WithDisabled(rep.ContentFixers))
	if err != nil {
		t.Fatalf("RemediatePDFA with fixers disabled: %v", err)
	}
	if len(off.ContentFixers) != 0 || off.ContentSnapshot != nil || len(off.ContentFixed) != 0 {
		t.Errorf("a disabled content-tier fixer ran: %q, %q", off.ContentFixers, off.ContentFixed)
	}
	if _, rewritten := alloc.Pending()[contentObj]; rewritten {
		t.Error("a disabled fixer repaired the page content")
	}
	if len(off.Fixed) == 0 {
		t.Error("disabling the content tier stopped the other fixers")
	}
}
