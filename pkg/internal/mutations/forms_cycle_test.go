package mutations

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// cyclicFieldParentPDF is [formPDF] with the field's /Parent pointing back at
// its own widget, so the chain 6 -> 5 -> 6 never reaches a root.
//
// A /Parent cycle is not hypothetical: one corpus file carries 16 of them.
func cyclicFieldParentPDF() []byte {
	return []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [5 0 R] /DA (0 g) >> >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Annots [6 0 R] >>\nendobj\n" +
		"4 0 obj\n<< /Length 1 >>\nstream\n \nendstream\nendobj\n" +
		"5 0 obj\n<< /FT /Tx /T (name) /V (value) /Parent 6 0 R >>\nendobj\n" +
		"6 0 obj\n<< /Type /Annot /Subtype /Widget /Parent 5 0 R /Rect [0 0 10 10] >>\nendobj\n" +
		"trailer\n<< /Size 7 /Root 1 0 R >>\n%%EOF\n")
}

func cyclicFieldParentDoc(t *testing.T) (context.Context, *document.Document) {
	t.Helper()
	data := cyclicFieldParentPDF()
	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	return ctx, doc
}

// TestWidgetFieldIdentity_CycleDoesNotFabricateAQualifiedName pins the one
// place a /Parent cycle produced a wrong answer rather than merely a slow one.
//
// The walk appends /T at every hop, so revisiting the field repeats its partial
// name: bounded at 64 hops the chain 6 -> 5 -> 6 yielded "name.name.name..."
// with about 32 parts. A fully qualified name is what [findAndSetField] matches
// on, so filling that field silently stopped working. The depth bound did not
// help, because it stops a cycle without detecting one.
func TestWidgetFieldIdentity_CycleDoesNotFabricateAQualifiedName(t *testing.T) {
	ctx, doc := cyclicFieldParentDoc(t)

	_, _, fieldFT, qualified, ok := widgetFieldIdentity(ctx, doc, ast.PdfIndirectRef{ObjectNum: 6})
	if !ok {
		t.Fatal("widgetFieldIdentity refused a widget whose field states /FT and /T")
	}
	if qualified != "name" {
		t.Errorf("qualified name = %q, want %q", qualified, "name")
	}
	if strings.Contains(qualified, ".") {
		t.Errorf("qualified name %q repeats a partial name, so the cycle was walked more than once", qualified)
	}
	if fieldFT != "Tx" {
		t.Errorf("field type = %q, want Tx", fieldFT)
	}
}

// TestFieldValueOwner_CycleRefuses pins the contract rather than catching a
// regression: a cycle with no value on it refused before the visited set was
// added too, by exhausting maxFieldAncestorDepth instead of detecting the loop.
// The result is the same and only the work differs, so this test would not have
// failed against the unguarded walk.
func TestFieldValueOwner_CycleRefuses(t *testing.T) {
	ctx, doc := cyclicFieldParentDoc(t)

	annotRef := ast.PdfIndirectRef{ObjectNum: 6}
	annot, err := document.ResolveDict(ctx, doc, annotRef)
	if err != nil || annot == nil {
		t.Fatalf("resolving the widget: %v", err)
	}

	// The widget states no value key of its own, so the walk has to climb.
	owner, _, addressable := fieldValueOwner(ctx, doc, annotRef, annot)
	if owner == nil {
		t.Fatal("no owner found for a field that states /V one hop up")
	}
	if !addressable {
		t.Error("the owner is an indirect object, so it is addressable")
	}
}

// TestRootFieldRef_CycleTerminates pins termination only. The guarded and
// unguarded walks both end on object 6 for this fixture, one by detecting the
// repeat and one by running the bound out to an even number of hops, so this
// test also would not have failed before the visited set was added.
func TestRootFieldRef_CycleTerminates(t *testing.T) {
	ctx, doc := cyclicFieldParentDoc(t)

	if _, ok := rootFieldRef(ctx, doc, ast.PdfIndirectRef{ObjectNum: 6}); !ok {
		t.Error("rootFieldRef found no reference to return")
	}
}
