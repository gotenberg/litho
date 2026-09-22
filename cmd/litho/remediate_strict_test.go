package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/pdf"
)

// Remediation writes the profile's identification metadata whatever else
// remains, so a document it could not fully repair still claims the profile.
// Exit 0 then tells a pipeline the file is ready when it is not, and only the
// printed report says otherwise. --strict is what lets a build refuse it.
//
// The status follows validate rather than inventing one: exitValidationFail
// means the document did not conform, exitError stays reserved for the command
// itself failing.
//
// The fixture is load-bearing. minimalPDF is repaired completely, three fixes
// and no residual, so asserting --strict on it exercises nothing and passes
// for the wrong reason. redactableCLIPDF paints text through a non-embedded
// Helvetica, and litho ships no font program, so that embedding violation
// survives remediation under both PDF/A-2b and PDF/UA-1. Measured across seven
// fixtures before choosing, and every one of those runs exited 0 whatever it
// left behind, which is the behaviour this flag exists to change.

// runRemediateStrict runs remediate against src and returns the exit code,
// stdout, and whatever landed at the output path.
func runRemediateStrict(t *testing.T, src string, extra ...string) (int, string, []byte) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "out.pdf")
	args := append([]string{"remediate", "--profile", "PDF/A-2b", "-o", out}, extra...)
	args = append(args, src)

	var stdout, stderr bytes.Buffer
	rc := run(context.Background(), args, &stdout, &stderr)
	data, err := os.ReadFile(out) //nolint:gosec // path is this test's own temp file
	if err != nil {
		data = nil
	}
	return rc, stdout.String(), data
}

// TestRemediateStrict_ExitsNonZeroOnFatalResiduals carries its own
// precondition. The unflagged run asserts both that the default status is
// still 0 and that this fixture genuinely leaves residuals; without that, a
// passing strict assertion could equally mean the flag fires unconditionally.
func TestRemediateStrict_ExitsNonZeroOnFatalResiduals(t *testing.T) {
	src := writeTempPDF(t, redactableCLIPDF())

	rc, stdout, _ := runRemediateStrict(t, src)
	if rc != exitOK {
		t.Fatalf("without --strict: exit %d, want %d; stdout: %s", rc, exitOK, stdout)
	}
	if !strings.Contains(stdout, "Residual:") {
		t.Fatalf("the fixture left no residuals, so --strict would have nothing to refuse; stdout: %s", stdout)
	}

	rcStrict, _, data := runRemediateStrict(t, src, "--strict")
	if rcStrict != exitValidationFail {
		t.Errorf("with --strict: exit %d, want %d", rcStrict, exitValidationFail)
	}
	// The exit status is the verdict, not the product. A caller who asked for
	// a repaired file still gets one, and decides what to do with it.
	if len(data) == 0 {
		t.Error("--strict withheld the output file")
	}
}

// TestRemediateStrict_JSONHonorsTheFlag covers the branch that used to return
// the moment it had printed. A verdict that applied to the text format and not
// to JSON would be worse than no verdict, because the JSON caller is the one
// most likely to be a pipeline.
func TestRemediateStrict_JSONHonorsTheFlag(t *testing.T) {
	src := writeTempPDF(t, redactableCLIPDF())

	rc, stdout, _ := runRemediateStrict(t, src, "--strict", "-f", "json")
	if rc != exitValidationFail {
		t.Errorf("json with --strict: exit %d, want %d", rc, exitValidationFail)
	}

	// Refusing must not cost the caller the report that explains the refusal.
	var got remediateResult
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, stdout)
	}
	if len(got.Residual) == 0 {
		t.Error("the JSON carries no residual list, so the caller cannot see what failed")
	}
}

// TestFatalResiduals_TreatsAnUnstructuredErrorAsFatal pins the mapping the
// public boundary documents: an error that is not a compliance violation
// becomes a fatal Violation carrying its message, so nothing is lost and
// nothing is quietly downgraded out of the strict verdict.
//
// The warning grade is deliberately not exercised here. It is reachable only
// through four Factur-X and CII rules, and constructing one needs the internal
// compliance package that this command cannot import.
func TestFatalResiduals_TreatsAnUnstructuredErrorAsFatal(t *testing.T) {
	t.Parallel()
	report := &pdf.RemediationReport{Residual: []error{errors.New("something went wrong")}}
	if got := fatalResiduals(report); len(got) != 1 {
		t.Errorf("fatalResiduals = %d, want 1", len(got))
	}
}

// TestFatalResiduals_EmptyReportIsClean is what keeps --strict from failing a
// run that fixed everything.
func TestFatalResiduals_EmptyReportIsClean(t *testing.T) {
	t.Parallel()
	if got := fatalResiduals(&pdf.RemediationReport{}); len(got) != 0 {
		t.Errorf("fatalResiduals on a clean report = %d, want 0", len(got))
	}
}
