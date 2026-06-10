package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
)

func TestMatchAnnotArtifact(t *testing.T) {
	t.Parallel()
	for _, rule := range []compliance.Rule{
		compliance.Rule_14289_2_8_9_2_4_9_t01,  // Popup in structure
		compliance.Rule_14289_2_8_9_2_4_14_t01, // PrinterMark not artifact
		compliance.Rule_14289_2_8_9_2_2_t01,    // Invisible in structure
		compliance.Rule_14289_2_8_9_2_2_t02,    // NoView in structure
	} {
		if !matchAnnotArtifact(compliance.Violation{Rule: rule}) {
			t.Errorf("must match artifact rule %s/%s t%d", rule.Specification, rule.Clause, rule.TestNumber)
		}
	}
	// Must not claim the forbidden-Sound rule (adjacent clause family).
	if matchAnnotArtifact(compliance.Violation{Rule: compliance.Rule_14289_2_8_9_2_4_11_t01}) {
		t.Error("must not claim the forbidden Sound-annotation rule")
	}
}

func TestFixAnnotArtifact_RemovesStructParent(t *testing.T) {
	// A Popup (4) and a PrinterMark (5), both in the structure tree, plus a Link
	// (6) that legitimately stays. Only the first two lose /StructParent.
	pdf := annotsPDF("[4 0 R 5 0 R 6 0 R]",
		"<< /Type /Annot /Subtype /Popup /Rect [0 0 10 10] /StructParent 0 >>",
		"<< /Type /Annot /Subtype /PrinterMark /Rect [0 0 10 10] /StructParent 1 >>",
		"<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] /StructParent 2 >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixAnnotArtifact(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-2")
	if err != nil {
		t.Fatalf("fixAnnotArtifact: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 fix description, got %v", descs)
	}
	if _, has := pendingDict(t, alloc, 4)["StructParent"]; has {
		t.Error("Popup must lose /StructParent")
	}
	if _, has := pendingDict(t, alloc, 5)["StructParent"]; has {
		t.Error("PrinterMark must lose /StructParent")
	}
	if _, queued := alloc.Pending()[6]; queued {
		t.Error("Link must not be modified")
	}
}

func TestFixAnnotArtifact_InvisibleFlag(t *testing.T) {
	// A Text annotation with the Invisible flag (1) set and /StructParent loses it;
	// a plain Text annotation without forbidden flags is untouched.
	pdf := annotsPDF("[4 0 R 5 0 R]",
		"<< /Type /Annot /Subtype /Text /Rect [0 0 10 10] /F 1 /StructParent 0 >>",
		"<< /Type /Annot /Subtype /Text /Rect [0 0 10 10] /F 4 /StructParent 1 >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	if _, err := fixAnnotArtifact(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-2"); err != nil {
		t.Fatalf("fixAnnotArtifact: %v", err)
	}
	if _, has := pendingDict(t, alloc, 4)["StructParent"]; has {
		t.Error("Invisible annotation must lose /StructParent")
	}
	if _, queued := alloc.Pending()[5]; queued {
		t.Error("a visible (Print-only) annotation must not be modified")
	}
}

func TestFixAnnotArtifact_NoViewWithToggleUntouched(t *testing.T) {
	// NoView (32) set but ToggleNoView (256) also set: §8.9.2.2 t2 does not apply,
	// so /StructParent is kept.
	pdf := annotsPDF("[4 0 R]",
		"<< /Type /Annot /Subtype /Text /Rect [0 0 10 10] /F 288 /StructParent 0 >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixAnnotArtifact(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-2")
	if err != nil {
		t.Fatalf("fixAnnotArtifact: %v", err)
	}
	if len(descs) != 0 {
		t.Errorf("NoView+ToggleNoView annotation: got %v, want no fixes", descs)
	}
	if _, queued := alloc.Pending()[4]; queued {
		t.Error("annotation with ToggleNoView set must not be modified")
	}
}

func TestFixAnnotArtifact_NoStructParentUntouched(t *testing.T) {
	// A Popup that is already an artifact (no /StructParent) is left alone.
	pdf := annotsPDF("[4 0 R]",
		"<< /Type /Annot /Subtype /Popup /Rect [0 0 10 10] >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixAnnotArtifact(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-2")
	if err != nil {
		t.Fatalf("fixAnnotArtifact: %v", err)
	}
	if len(descs) != 0 {
		t.Errorf("already-artifact Popup: got %v, want no fixes", descs)
	}
	if _, queued := alloc.Pending()[4]; queued {
		t.Error("Popup already an artifact must not be modified")
	}
}
