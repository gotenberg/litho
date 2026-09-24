package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestWalkFormFields_FieldAAJudgesFormFieldsOnly pins which dictionaries ISO
// 19005-2 §6.4.1 t2 judges as form fields when it reports a field /AA.
func TestWalkFormFields_FieldAAJudgesFormFieldsOnly(t *testing.T) {
	t.Parallel()

	// §6.4.1 t2 judges veraPDF's form fields: every dictionary the /Fields array
	// lists, and below a field only the kids that carry a non-null /T. A kid
	// widget with /FT and no /T, or with a null /T, carries a field's appearance,
	// not its identity (ISO 32000-1 §12.7.3.1); its /AA is a t1 (widget)
	// violation only. A top-level widget is a field whatever it carries, and a
	// null /AA is absent (7.3.7). Measured against veraPDF.
	fields := ast.PdfArray{
		ast.PdfDict{"Subtype": ast.PdfName("Widget"), "AA": ast.PdfDict{}}, // top-level widget: t2
		ast.PdfDict{"FT": ast.PdfName("Tx"), "T": ast.PdfString("p"), "AA": ast.PdfDict{}, "Kids": ast.PdfArray{ // field: t2
			ast.PdfDict{"Subtype": ast.PdfName("Widget"), "FT": ast.PdfName("Btn"), "AA": ast.PdfDict{}}, // no /T: no t2
			ast.PdfDict{"Subtype": ast.PdfName("Widget"), "T": ast.PdfNull{}, "AA": ast.PdfDict{}},       // null /T: no t2
			ast.PdfDict{"T": ast.PdfString("k"), "AA": ast.PdfDict{}},                                    // kid field: t2
		}},
		ast.PdfDict{"FT": ast.PdfName("Tx"), "T": ast.PdfString("f"), "AA": ast.PdfNull{}}, // null /AA: no t2
	}

	var results []Violation
	walkFormFields(context.Background(), nopDoc(), fields, "PDF/A-2b", 2, &results, 0, make(map[int]bool), make(map[int]bool), true)

	t2 := 0
	for _, v := range results {
		if v.Rule == Rule_19005_2_6_4_1_t02 {
			t2++
		}
	}
	if t2 != 3 {
		t.Errorf("expected 3 field /AA (t2) violations (top-level widget, parent field, kid field), got %d", t2)
	}
}

// TestCheckPageActions_ForbiddenAAStillChecksActionTypes pins that a forbidden
// page /AA is reported and its actions are still judged for their type.
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

// TestIsForbiddenAction pins the per-profile action-type whitelists of
// [isForbiddenAction].
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

// TestIsForbiddenActionType pins that the exported verdict is the validator's
// own for every PDF/A profile, and that a profile outside PDF/A forbids no
// action type.
func TestIsForbiddenActionType(t *testing.T) {
	t.Parallel()

	types := []ast.PdfName{"GoTo", "GoToE", "JavaScript", "SetOCGState", "GoTo3DView", "Launch", "RichMediaExecute"}
	for _, prof := range []string{"PDF/A-1b", "PDF/A-2b", "PDF/A-3u", "PDF/A-4", "PDF/A-4e", "PDF/A-4f"} {
		for _, at := range types {
			if got, want := IsForbiddenActionType(at, prof), isForbiddenAction(at, prof); got != want {
				t.Errorf("IsForbiddenActionType(%q, %q) = %v, want the validator's %v", at, prof, got, want)
			}
		}
	}
	for _, prof := range []string{"PDF/UA-1", "PDF/UA-2", "WTPDF-1.0-Reuse", ""} {
		for _, at := range types {
			if IsForbiddenActionType(at, prof) {
				t.Errorf("IsForbiddenActionType(%q, %q) = true, want false: no action-type rule outside PDF/A", at, prof)
			}
		}
	}
}

// TestAllowedNamedActions pins that [AllowedNamedActions] holds the four page
// navigation actions and not Print.
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

// TestActionKeysPresent pins the key list [actionKeysPresent] formats.
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
