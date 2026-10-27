package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
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

// TestREADME_SignExampleRuns pins the `litho sign` invocation the README shows
// as one that works.
//
// The README once listed a sign example while the command always refused, and
// the example was removed. The command signs now, so the example is back, and
// this test runs it with a generated certificate and key in place of the
// README's file names: an example that stopped working fails here.
func TestREADME_SignExampleRuns(t *testing.T) {
	readme := readREADME(t)

	var example []string
	for line := range strings.SplitSeq(readme, "\n") {
		if fields := strings.Fields(line); len(fields) > 1 && fields[0] == "litho" && fields[1] == "sign" {
			example = fields[1:]
			break
		}
	}
	if example == nil {
		t.Fatal("README shows no `litho sign` invocation")
	}

	certPath, keyPath := writeSigningPEM(t, "ec")
	out := filepath.Join(t.TempDir(), "signed.pdf")
	names := map[string]string{
		"cert.pem": certPath, "key.pem": keyPath, "document.pdf": onePageTempPDF(t), "signed.pdf": out,
	}
	for i, arg := range example {
		if sub, ok := names[arg]; ok {
			example[i] = sub
			delete(names, arg)
		}
	}
	if len(names) > 0 {
		t.Fatalf("the README example %q no longer names the files this test substitutes: %v", example, names)
	}

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), example, &stdout, &stderr); rc != exitOK {
		t.Fatalf("the README's sign example exits %d: %s", rc, stderr.String())
	}
	if n := signatureCountOf(t, out); n != 1 {
		t.Errorf("the README's sign example wrote %d signatures, want 1", n)
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
	for line := range strings.SplitSeq(stdout.String()+stderr.String(), "\n") {
		if strings.HasPrefix(line, "  -") {
			n++
		}
	}
	return n
}
