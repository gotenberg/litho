package main

import (
	"bytes"
	"context"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// runCapturingStderr dispatches a command and returns its exit code and
// stderr. pipe goes through runPipeArgs, which the pipe tests already use.
func runCapturingStderr(t *testing.T, args []string) (int, string) {
	t.Helper()

	if args[0] == "pipe" {
		return runPipeArgs(t, args)
	}

	var stdout, stderr bytes.Buffer
	rc := run(context.Background(), args, &stdout, &stderr)
	return rc, stderr.String()
}

// encryptionWarningLine returns the warning the command printed, failing the
// test when there is none. Without this guard a row that stopped warning would
// pass every assertion below by having nothing left to inspect.
func encryptionWarningLine(t *testing.T, stderr string) string {
	t.Helper()

	for _, line := range strings.Split(stderr, "\n") {
		if strings.Contains(line, encryptionWarning) {
			return line
		}
	}
	t.Fatalf("no encryption warning to inspect; stderr = %q", stderr)
	return ""
}

var warningFlagPattern = regexp.MustCompile(`--[a-z][a-z0-9-]*`)

// TestEncryptionWarning_NamesNoFlagTheCommandRejects is the invariant behind
// the per-family remedies: a warning may only name a flag its own command
// accepts.
//
// Measured on the single-message version: `80742794` widened the warning from
// the five page operations to every output-producing command without changing
// its text, so fourteen of the seventeen emitters advised --user-password while
// rejecting it. `litho rotate -p pw --user-password new` exits 1 with "flag
// provided but not defined: -user-password", which means the answer to a
// confidentiality downgrade was a command that fails.
//
// The probe runs the advised flag rather than reading usage text, so the test
// asserts the remedy WORKS rather than that it is documented. Rows whose
// warning names no flag pass the loop trivially and are pinned by
// TestEncryptionWarning_EachFamilyGetsItsOwnRemedy instead.
func TestEncryptionWarning_NamesNoFlagTheCommandRejects(t *testing.T) {
	for _, tc := range encryptionRemedyRows() {
		t.Run(tc.desc, func(t *testing.T) {
			f := newRemedyFixture(t)
			out := filepath.Join(t.TempDir(), "out.pdf")

			rc, stderr := runCapturingStderr(t, tc.args(f, out))
			if rc != exitOK {
				t.Fatalf("exit %d, stderr: %s", rc, stderr)
			}

			for _, flagName := range warningFlagPattern.FindAllString(encryptionWarningLine(t, stderr), -1) {
				probeOut := filepath.Join(t.TempDir(), "probe.pdf")
				probe := append(tc.args(f, probeOut), flagName, "probe-pw")

				rc, stderr := runCapturingStderr(t, probe)
				if strings.Contains(stderr, "flag provided but not defined") {
					t.Errorf("the warning advises %s, which this command rejects: %s",
						flagName, strings.TrimSpace(stderr))
					continue
				}
				if rc != exitOK {
					t.Errorf("the warning advises %s, and following it exits %d: %s",
						flagName, rc, strings.TrimSpace(stderr))
				}
			}
		})
	}
}

// TestEncryptionWarning_EachFamilyGetsItsOwnRemedy pins the three remedies
// README.md documents: split and merge take --user-password, pipe takes an
// encrypt stage, everything else is followed by litho encrypt.
//
// The negative half carries the defect. A command with no encryption flag must
// not name one, which is what the widened single message did.
func TestEncryptionWarning_EachFamilyGetsItsOwnRemedy(t *testing.T) {
	for _, tc := range encryptionRemedyRows() {
		t.Run(tc.desc, func(t *testing.T) {
			f := newRemedyFixture(t)
			out := filepath.Join(t.TempDir(), "out.pdf")

			rc, stderr := runCapturingStderr(t, tc.args(f, out))
			if rc != exitOK {
				t.Fatalf("exit %d, stderr: %s", rc, stderr)
			}

			line := encryptionWarningLine(t, stderr)
			if !strings.Contains(line, tc.wantRemedy) {
				t.Errorf("remedy %q missing from the warning; got %q", tc.wantRemedy, line)
			}
			if tc.wantNoRemedy != "" && strings.Contains(line, tc.wantNoRemedy) {
				t.Errorf("the warning names %q, which this command does not accept; got %q",
					tc.wantNoRemedy, line)
			}
		})
	}
}

type remedyFixture struct {
	encrypted string
	plain     string
}

// newRemedyFixture builds a two-page encrypted document and a plain one. Two
// pages because delete refuses to remove the only page of a document.
func newRemedyFixture(t *testing.T) remedyFixture {
	t.Helper()

	return remedyFixture{
		encrypted: encryptTempPDF(t, twoPageTempPDF(t), "pw"),
		plain:     onePageTempPDF(t),
	}
}

type encryptionRemedyRow struct {
	desc string
	args func(f remedyFixture, out string) []string
	// wantRemedy is the advice this command's family gets, wantNoRemedy the
	// advice it must not get.
	wantRemedy   string
	wantNoRemedy string
}

// encryptionRemedyRows covers all three families: two commands that accept
// --user-password, pipe, and three that accept no encryption flag at all.
func encryptionRemedyRows() []encryptionRemedyRow {
	return []encryptionRemedyRow{
		{
			desc: "split accepts the flag",
			args: func(f remedyFixture, out string) []string {
				return []string{"split", "-p", "pw", "-pages", "1", "-o", out, f.encrypted}
			},
			wantRemedy: "--user-password",
		},
		{
			desc: "merge accepts the flag",
			args: func(f remedyFixture, out string) []string {
				return []string{"merge", "-p", "pw", f.plain, f.encrypted, "-o", out}
			},
			wantRemedy: "--user-password",
		},
		{
			desc: "pipe takes an encrypt stage, not a flag of its own",
			args: func(f remedyFixture, out string) []string {
				return []string{"pipe", "-p", "pw", f.encrypted, "rotate", "-angle", "90", "-o", out}
			},
			wantRemedy:   "Add an encrypt stage",
			wantNoRemedy: "--user-password",
		},
		{
			desc: "rotate has no encryption flag",
			args: func(f remedyFixture, out string) []string {
				return []string{"rotate", "-p", "pw", "-angle", "90", "-o", out, f.encrypted}
			},
			wantRemedy:   "Run litho encrypt on the result",
			wantNoRemedy: "--user-password",
		},
		{
			desc: "delete has no encryption flag",
			args: func(f remedyFixture, out string) []string {
				return []string{"delete", "-p", "pw", "-pages", "1", "-o", out, f.encrypted}
			},
			wantRemedy:   "Run litho encrypt on the result",
			wantNoRemedy: "--user-password",
		},
		{
			desc: "set-metadata has no encryption flag",
			args: func(f remedyFixture, out string) []string {
				return []string{"set-metadata", "-p", "pw", "-title", "T", "-o", out, f.encrypted}
			},
			wantRemedy:   "Run litho encrypt on the result",
			wantNoRemedy: "--user-password",
		},
	}
}

// TestEncryptionWarning_TheAdvisedFlagIsRejectedByCommandsThatLackIt is the
// control that keeps the invariant test honest.
//
// TestEncryptionWarning_NamesNoFlagTheCommandRejects only probes flags the
// warning actually names, so it would pass vacuously against a message that
// named nothing at all. This proves the probe detects a real rejection: rotate
// genuinely refuses --user-password, which is why its warning must not offer
// it.
func TestEncryptionWarning_TheAdvisedFlagIsRejectedByCommandsThatLackIt(t *testing.T) {
	f := newRemedyFixture(t)
	out := filepath.Join(t.TempDir(), "out.pdf")

	rc, stderr := runCapturingStderr(t, []string{
		"rotate", "-p", "pw", "-angle", "90", "-o", out, f.encrypted, "--user-password", "x",
	})
	if rc == exitOK {
		t.Fatalf("rotate accepted --user-password, so the probe cannot detect a rejected flag; stderr: %s", stderr)
	}
	if !strings.Contains(stderr, "flag provided but not defined") {
		t.Errorf("rotate failed for some other reason, so the probe proves nothing; stderr: %q", stderr)
	}
}
