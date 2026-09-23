package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
)

// readREADME returns the repository README, which sits two levels above the
// package directory the test runs in.
func readREADME(t *testing.T) string {
	t.Helper()

	data, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatalf("reading README.md: %v", err)
	}
	return string(data)
}

// TestREADME_DoesNotAdvertiseTheSignCLI pins a claim the CLI cannot honour.
//
// `litho sign` ALWAYS refuses: it takes a pdf.Signer the CLI cannot supply
// without crypto dependencies, prints an explanation and exits 1, which
// TestSign_RefusesWithAnExplanation already pins. The README nonetheless listed
//
//	litho sign --cert cert.pem --key key.pem -o signed.pdf document.pdf
//
// among its CLI examples, beside redact and encrypt, which do work. Measured:
// running it exits 1 with "signing requires a custom Signer implementation".
// Documenting an invocation that always fails is the same defect as advising a
// flag the command rejects.
//
// The Features list still claims signing, correctly: the LIBRARY signs. Only
// the CLI example was false, so only it was removed.
func TestREADME_DoesNotAdvertiseTheSignCLI(t *testing.T) {
	readme := readREADME(t)

	// The control: without it a README that lost its example block entirely,
	// or a path that read the wrong file, would pass this test silently.
	if !strings.Contains(readme, "litho encrypt --user-password secret") {
		t.Fatal("the CLI example block is missing, so the assertion below proves nothing")
	}

	for _, line := range strings.Split(readme, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "litho sign") {
			t.Errorf("README shows a `litho sign` invocation, which always exits 1: %q", line)
		}
	}
}

// TestSign_TheCLIStillRefuses is the other half of the claim above. The README
// says signing is library-only; if the CLI ever gains it, this test fails and
// the documentation has to be revisited rather than quietly drifting again.
func TestSign_TheCLIStillRefuses(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := run(context.Background(), []string{"sign", "-o", "out.pdf", "in.pdf"}, &stdout, &stderr)

	if rc == exitOK {
		t.Fatalf("sign succeeded, so the README's library-only note is now wrong; stderr: %s",
			stderr.String())
	}
	if !strings.Contains(stderr.String(), "Signer") {
		t.Errorf("sign failed without explaining why; stderr: %q", stderr.String())
	}
}

// TestREADME_FlagsHintNamesAnInvocationThatShowsFlags pins the second claim.
//
// The README said "run `litho <command>` to see its flags". Measured, that
// prints one Usage line and NO flags: a command with no arguments hits its own
// argument guard, and nothing in the package calls flag.PrintDefaults. The flag
// list appears only on the help path. So the sentence named the one invocation
// that does not do what it promised.
func TestREADME_FlagsHintNamesAnInvocationThatShowsFlags(t *testing.T) {
	readme := readREADME(t)

	if !strings.Contains(readme, "`litho <command> -h` to see its flags") {
		t.Error("README no longer points at the invocation that actually lists flags")
	}

	// The measurement behind the wording, asserted rather than trusted: bare
	// prints no flags, -h does.
	bare := countFlagLines(t, "rotate")
	withHelp := countFlagLines(t, "rotate", "-h")

	if bare != 0 {
		t.Errorf("a bare command now lists %d flag line(s); the README wording can be relaxed", bare)
	}
	if withHelp == 0 {
		t.Error("`-h` lists no flags, so the README now points at the wrong invocation")
	}
}

// countFlagLines runs a command and counts the flag entries flag.PrintDefaults
// emits, which are indented and start with a dash.
func countFlagLines(t *testing.T, args ...string) int {
	t.Helper()

	var stdout, stderr bytes.Buffer
	run(context.Background(), args, &stdout, &stderr)

	n := 0
	for _, line := range strings.Split(stdout.String()+stderr.String(), "\n") {
		if strings.HasPrefix(line, "  -") {
			n++
		}
	}
	return n
}
