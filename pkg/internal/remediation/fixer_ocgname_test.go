package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// TestEnsureOCGConfigNameReturnsDirectReplacement pins that a configuration
// dictionary written inline is handed back for the caller to install.
//
// An optional-content configuration may be a DIRECT dictionary inside
// /OCProperties, which no object number addresses. Writing the fix only through
// the allocator dropped that case while still reporting success, so the catalog
// was rewritten with /Name still missing and the next run found the same
// defect, so the file grew on every pass and never gained the /Name.
//
// # Corpus evidence
//
//   - litho complex_1b.pdf: remediating it grew it 1125 bytes per pass,
//     forever, and never added the /Name.
func TestEnsureOCGConfigNameReturnsDirectReplacement(t *testing.T) {
	alloc := writer.NewObjAllocator(10)
	direct := ast.PdfDict{"Order": ast.PdfArray{}}

	replacement, fixed := ensureOCGConfigName(context.Background(), alloc, nil, direct, "D")

	if !fixed {
		t.Fatal("a config without /Name should be fixed, got fixed=false")
	}
	if replacement == nil {
		t.Fatal("a direct dictionary must be returned for the caller to install, got nil")
	}
	name, ok := replacement["Name"].(ast.PdfString)
	if !ok {
		t.Fatalf("/Name is %T, want ast.PdfString", replacement["Name"])
	}
	if string(name) != "D" {
		t.Errorf("/Name = %q, want \"D\"", name)
	}
	// The original must be left alone: callers copy before installing.
	if _, leaked := direct["Name"]; leaked {
		t.Error("the source dictionary was mutated in place")
	}
}

// TestEnsureOCGConfigNameLeavesNamedConfigsAlone checks the fixer is a no-op
// once the name is there, which is what makes remediation idempotent.
func TestEnsureOCGConfigNameLeavesNamedConfigsAlone(t *testing.T) {
	alloc := writer.NewObjAllocator(10)
	named := ast.PdfDict{"Name": ast.PdfString("D"), "Order": ast.PdfArray{}}

	replacement, fixed := ensureOCGConfigName(context.Background(), alloc, nil, named, "D")

	if fixed {
		t.Error("a config that already has /Name should report no fix")
	}
	if replacement != nil {
		t.Error("no replacement should be returned when nothing changed")
	}
}
