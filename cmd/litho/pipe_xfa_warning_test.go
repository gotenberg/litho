package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// outputHasXFAForm answers the XFA question the way the library does.
//
// A byte scan for "/XFA" would agree on today's output and stop agreeing the
// moment the writer packs the AcroForm into an object stream, so the assertion
// is keyed on the predicate the warning itself uses.
func outputHasXFAForm(t *testing.T, path string) bool {
	t.Helper()

	ctx := context.Background()
	r, closer, err := openPDF(ctx, path, "")
	if err != nil {
		t.Fatalf("opening %s: %v", path, err)
	}
	defer closer()

	return r.HasXFAForm(ctx)
}

// TestPipe_WarnsWhenAStageDropsXFA pins the entry-point parity gap.
//
// `8ef706c8` wired warnXFADropped into the standalone split and merge, the two
// verbs measured to discard /XFA. pipe runs the identical operations and said
// nothing, so `litho split` on an XFA document reported the loss while
// `litho pipe ... split` wrote the same output in silence. This is the third
// time the same divergence has appeared at this seam: the encryption warning
// needed `10c8949f` to reach pipe at all and `c9c20cd8` to reach every stage.
//
// The merge row puts the XFA document SECOND on purpose. Checking only the
// piped input would pass a test that put it first, and a stage source that
// loses its form is exactly the case the variadic warning exists for.
func TestPipe_WarnsWhenAStageDropsXFA(t *testing.T) {
	for _, tc := range []struct {
		desc  string
		build func(xfa, plain, out string) []string
	}{
		{"split", func(xfa, _, out string) []string {
			return []string{"pipe", xfa, "split", "-pages", "1", "-o", out}
		}},
		{"merge", func(xfa, plain, out string) []string {
			return []string{"pipe", xfa, "merge", plain, "-o", out}
		}},
		{"merge, only the stage source carries XFA", func(xfa, plain, out string) []string {
			return []string{"pipe", plain, "merge", xfa, "-o", out}
		}},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			xfa := writeTempPDF(t, xfaFormFixturePDF())
			plain := writeTempPDF(t, untaggedTextPDF())
			out := filepath.Join(t.TempDir(), "out.pdf")

			rc, errOut := runPipeArgs(t, tc.build(xfa, plain, out))
			if rc != exitOK {
				t.Fatalf("exit %d, stderr: %s", rc, errOut)
			}
			if !strings.Contains(errOut, xfaWarning) {
				t.Errorf("no XFA warning: pipe dropped the input's form data silently, "+
					"while the standalone command reports it; stderr: %q", errOut)
			}
			// Without this the assertion above would still pass if the output
			// had kept its XFA, in which case there was nothing to warn about
			// and the row would prove nothing.
			if outputHasXFAForm(t, out) {
				t.Error("the output still carries XFA, so no loss happened here")
			}
		})
	}
}

// TestPipe_NoXFAWarningWhenTheStageKeepsIt is the control that stops the
// warning from being unconditional, and it is why xfaDroppingStages is an allow
// list.
//
// rotate rewrites the document and MEASURABLY keeps /XFA, so warning here would
// report a loss that did not happen. A deny list modelled on silentStages would
// do exactly that for fifteen stages.
func TestPipe_NoXFAWarningWhenTheStageKeepsIt(t *testing.T) {
	xfa := writeTempPDF(t, xfaFormFixturePDF())
	out := filepath.Join(t.TempDir(), "out.pdf")

	rc, errOut := runPipeArgs(t, []string{"pipe", xfa, "rotate", "-angle", "90", "-o", out})
	if rc != exitOK {
		t.Fatalf("exit %d, stderr: %s", rc, errOut)
	}
	if strings.Contains(errOut, xfaWarning) {
		t.Errorf("warned although the stage keeps the form data; stderr: %q", errOut)
	}
	if !outputHasXFAForm(t, out) {
		t.Error("rotate lost the XFA through pipe, so the silence is wrong " +
			"and this control proves nothing")
	}
}

// TestPipe_NoXFAWarningForAnOrdinaryForm is the control that matters most, and
// it mirrors the one guarding the standalone warning.
//
// formFixturePDF is a real /AcroForm with widget fields and no /XFA, and those
// fields survive a split. A predicate keyed on /AcroForm presence instead of
// /XFA would fire here while every other test still passed.
func TestPipe_NoXFAWarningForAnOrdinaryForm(t *testing.T) {
	form := writeTempPDF(t, formFixturePDF())
	out := filepath.Join(t.TempDir(), "out.pdf")

	rc, errOut := runPipeArgs(t, []string{"pipe", form, "split", "-pages", "1", "-o", out})
	if rc != exitOK {
		t.Fatalf("exit %d, stderr: %s", rc, errOut)
	}
	if strings.Contains(errOut, xfaWarning) {
		t.Errorf("warned on an ordinary AcroForm that survives the split; stderr: %q", errOut)
	}
}
