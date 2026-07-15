package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

func TestWalkFormFields_PureWidgetAANotFieldViolation(t *testing.T) {
	t.Parallel()

	// A pure widget-annotation kid (/Subtype /Widget, no /FT, no /T) carries a
	// field's appearance, not its identity (ISO 32000-1 §12.7.3.1); its /AA is a
	// §6.4.1 t1 (widget) violation only, never t2 (PDFormField). A terminal field
	// merged with its widget keeps /FT (or /T) and stays a field.
	fields := ast.PdfArray{
		ast.PdfDict{"Subtype": ast.PdfName("Widget"), "AA": ast.PdfDict{}},                           // pure widget: no t2
		ast.PdfDict{"Subtype": ast.PdfName("Widget"), "FT": ast.PdfName("Btn"), "AA": ast.PdfDict{}}, // merged terminal: t2
		ast.PdfDict{"FT": ast.PdfName("Tx"), "T": ast.PdfString("f"), "AA": ast.PdfDict{}},           // plain field: t2
	}

	var results []Violation
	walkFormFields(context.Background(), nil, fields, "PDF/A-2b", 2, &results, 0, make(map[int]bool))

	t2 := 0
	for _, v := range results {
		if v.Rule == Rule_19005_2_6_4_1_t02 {
			t2++
		}
	}
	if t2 != 2 {
		t.Errorf("expected 2 field /AA (t2) violations (merged terminal + plain field), got %d", t2)
	}
}

func TestCheckPageActions_ForbiddenAAStillChecksActionTypes(t *testing.T) {
	t.Parallel()

	// A page /AA is forbidden outright for parts 2/3 (§6.5.2 t02), but the
	// action-type restrictions (§6.5.1) still apply to the actions it holds:
	// veraPDF fires both. The /O action is a forbidden Launch (§6.5.1 t01) and
	// the /C action is a disallowed named action (§6.5.1 t02).
	doc, cat := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /AA << /O 4 0 R /C 5 0 R >> >>",
		"<< /Type /Action /S /Launch /F (setup.exe) >>",
		"<< /Type /Action /S /Named /N /ShowBookmarks >>",
	)

	got := checkPageActions(context.Background(), doc, cat, "PDF/A-2b", 2)
	for _, want := range []Rule{
		Rule_19005_2_6_5_2_t02, // page /AA forbidden
		Rule_19005_2_6_5_1_t01, // Launch action type
		Rule_19005_2_6_5_1_t02, // disallowed named action
	} {
		if !hasRule(got, want) {
			t.Errorf("page /AA with forbidden actions: missing %v (got %v)", want, got)
		}
	}
}

func TestIsForbiddenAction(t *testing.T) {
	t.Parallel()

	// PDF/A permits only a whitelist of action types per profile (§6.6.1/§6.5.1).
	// Any /S value outside the allowed set is non-conformant, including action
	// types the prose never enumerated, since the whitelist is closed.

	t.Run("non-whitelisted types forbidden for every profile", func(t *testing.T) {
		t.Parallel()
		forbidden := []ast.PdfName{
			"Launch", "Sound", "Movie", "ResetForm", "ImportData",
			"Hide", "SetState", "NoOp", "NOP",
		}
		for _, prof := range []string{"PDF/A-1b", "PDF/A-2b", "PDF/A-3b", "PDF/A-4", "PDF/A-4e", "PDF/A-4f"} {
			for _, at := range forbidden {
				if !isForbiddenAction(at, prof) {
					t.Errorf("isForbiddenAction(%q, %q) = false, want true", at, prof)
				}
			}
		}
	})

	t.Run("Rendition Trans GoToE forbidden under part 1 whitelist", func(t *testing.T) {
		t.Parallel()
		// The PDF/A-1 whitelist forbids these: a blacklist would miss them, but the
		// closed whitelist admits only the action types part 1 explicitly allows.
		for _, at := range []ast.PdfName{"Rendition", "Trans", "SetOCGState", "GoTo3DView", "GoToE"} {
			if !isForbiddenAction(at, "PDF/A-1b") {
				t.Errorf("isForbiddenAction(%q, PDF/A-1b) = false, want true", at)
			}
		}
	})

	t.Run("GoToE allowed for parts 2/3/4 but not part 1", func(t *testing.T) {
		t.Parallel()
		if isForbiddenAction("GoToE", "PDF/A-2b") || isForbiddenAction("GoToE", "PDF/A-4") {
			t.Error("GoToE should be allowed for parts 2-4")
		}
		if !isForbiddenAction("GoToE", "PDF/A-1b") {
			t.Error("GoToE should be forbidden for part 1")
		}
	})

	t.Run("JavaScript allowed only for part 4", func(t *testing.T) {
		t.Parallel()
		for _, prof := range []string{"PDF/A-1b", "PDF/A-2b", "PDF/A-3b"} {
			if !isForbiddenAction("JavaScript", prof) {
				t.Errorf("JavaScript should be forbidden for %q", prof)
			}
		}
		for _, prof := range []string{"PDF/A-4", "PDF/A-4e", "PDF/A-4f"} {
			if isForbiddenAction("JavaScript", prof) {
				t.Errorf("JavaScript should be allowed for %q", prof)
			}
		}
	})

	t.Run("SetOCGState and GoTo3DView allowed only for A-4e", func(t *testing.T) {
		t.Parallel()
		for _, at := range []ast.PdfName{"SetOCGState", "GoTo3DView"} {
			if isForbiddenAction(at, "PDF/A-4e") {
				t.Errorf("%q should be allowed for PDF/A-4e", at)
			}
			if !isForbiddenAction(at, "PDF/A-4") {
				t.Errorf("%q should be forbidden for PDF/A-4", at)
			}
		}
	})

	t.Run("GoTo is always allowed", func(t *testing.T) {
		t.Parallel()
		for _, prof := range []string{"PDF/A-1b", "PDF/A-2b", "PDF/A-3b", "PDF/A-4", "PDF/A-4e", "PDF/A-4f"} {
			if isForbiddenAction("GoTo", prof) {
				t.Errorf("isForbiddenAction(GoTo, %q) = true, want false", prof)
			}
		}
	})
}

func TestAllowedNamedActions(t *testing.T) {
	t.Parallel()
	expected := []ast.PdfName{"NextPage", "PrevPage", "FirstPage", "LastPage"}
	for _, n := range expected {
		if !AllowedNamedActions[n] {
			t.Errorf("AllowedNamedActions missing %q", n)
		}
	}
	if AllowedNamedActions["Print"] {
		t.Error("Print should not be an allowed named action")
	}
}

func TestActionKeysPresent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		hasA, hasAA bool
		want        string
	}{
		{"both", true, true, "A, AA"},
		{"only A", true, false, "A"},
		{"only AA", false, true, "AA"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := actionKeysPresent(tc.hasA, tc.hasAA); got != tc.want {
				t.Errorf("actionKeysPresent(%v, %v) = %q, want %q", tc.hasA, tc.hasAA, got, tc.want)
			}
		})
	}
}
