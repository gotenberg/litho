package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// The remediate stage accepts the profiles the standalone command accepts,
// minus the one it has no flags for.
//
// WTPDF was wired into remediate.go and validate.go and not into this stage,
// so a level litho remediates was rejected here as "unknown profile" while the
// standalone command handled it. Factur-X is the deliberate exception: it
// needs the invoice XML and conformance level that a pipe stage cannot take,
// so rejecting it is the honest answer rather than a gap.

// TestPipeRemediate_AcceptsWTPDF closes the wiring gap. The assertion is on
// the stage being reached at all: the point is that the profile no longer
// bounces off an "unknown profile" error before remediation runs.
func TestPipeRemediate_AcceptsWTPDF(t *testing.T) {
	src := writeTempPDF(t, redactableCLIPDF())
	out := filepath.Join(t.TempDir(), "out.pdf")

	rc, _, stderr := runPipeCLI(t,
		"pipe", "-o", out, src, "remediate", "-profile", "WTPDF-1.0-Reuse")
	if strings.Contains(stderr, "unknown profile") {
		t.Errorf("the remediate stage rejected WTPDF-1.0-Reuse, which the standalone command accepts; stderr: %s", stderr)
	}
	if rc != exitOK {
		t.Errorf("pipe remediate WTPDF-1.0-Reuse: exit %d, want %d; stderr: %s", rc, exitOK, stderr)
	}
}

// TestPipeRemediate_StillRejectsFacturX pins the boundary as a decision rather
// than an oversight. Factur-X needs --invoice and --level, which this stage
// does not take, so accepting the name and remediating without an invoice
// would write a document claiming a conformance it cannot have.
func TestPipeRemediate_StillRejectsFacturX(t *testing.T) {
	src := writeTempPDF(t, redactableCLIPDF())
	out := filepath.Join(t.TempDir(), "out.pdf")

	rc, _, stderr := runPipeCLI(t,
		"pipe", "-o", out, src, "remediate", "-profile", "Factur-X")
	if rc != exitError {
		t.Errorf("pipe remediate Factur-X: exit %d, want %d", rc, exitError)
	}
	if !strings.Contains(stderr, "unknown profile") {
		t.Errorf("no explanation that the stage cannot take Factur-X; stderr: %s", stderr)
	}
}

// TestPipeRemediate_ParityWithTheStandaloneCommand states the contract the
// wiring gap broke: a profile one entry point remediates, the other reaches
// too. Factur-X is excluded for the reason above.
func TestPipeRemediate_ParityWithTheStandaloneCommand(t *testing.T) {
	for _, profile := range []string{"PDF/A-2b", "PDF/UA-1", "WTPDF-1.0-Reuse"} {
		t.Run(profile, func(t *testing.T) {
			src := writeTempPDF(t, redactableCLIPDF())

			standalone := filepath.Join(t.TempDir(), "standalone.pdf")
			standaloneRC, _, _ := runPipeCLI(t, "remediate", "--profile", profile, "-o", standalone, src)

			piped := filepath.Join(t.TempDir(), "piped.pdf")
			pipedRC, _, stderr := runPipeCLI(t, "pipe", "-o", piped, src, "remediate", "-profile", profile)

			if standaloneRC == exitOK && pipedRC != exitOK {
				t.Errorf("%s: standalone exits %d but pipe exits %d; stderr: %s",
					profile, standaloneRC, pipedRC, stderr)
			}
		})
	}
}

// TestPipeRemediate_WTPDFResidualsReachStrict: the new arm collects its
// residuals like the two beside it, or --strict would silently never fire for
// WTPDF while working for every other profile.
func TestPipeRemediate_WTPDFResidualsReachStrict(t *testing.T) {
	src := writeTempPDF(t, redactableCLIPDF())
	out := filepath.Join(t.TempDir(), "out.pdf")

	rc, _, stderr := runPipeCLI(t,
		"pipe", "--strict", "-o", out, src, "remediate", "-profile", "WTPDF-1.0-Reuse")

	// exitError means the stage broke, which is a different failure from the
	// document not conforming, so it is worth naming separately.
	if rc == exitError {
		t.Fatalf("pipe --strict remediate WTPDF failed as a command; stderr: %s", stderr)
	}

	// Measured: this fixture leaves WTPDF residuals, so the verdict is 2. An
	// earlier form of this accepted either 0 or 2, which could not fail if the
	// new arm stopped collecting residuals altogether, and that is the one
	// thing this test exists to catch.
	if rc != exitValidationFail {
		t.Errorf("exit %d, want %d: this fixture leaves WTPDF residuals, so they must reach the verdict",
			rc, exitValidationFail)
	}
}
