package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// remediate rewrites the whole file for most documents but not all, so it
// takes --compact the way pipe does rather than the way rotate does: the flag
// is accepted, and applies when a rewrite happens.
//
// PDF/A-1 is the one profile it must refuse. ISO 19005-1, 6.1.4 forbids
// cross-reference streams, which packing requires. Measured on one file with
// and without compaction, PDF/A-1b gains exactly one violation (clause 6.1.4)
// while PDF/A-2b, PDF/A-3b and PDF/UA-1 gain none. Remediating a document into
// conformance and then packing it back out of conformance would defeat the
// command, so the flag is refused there rather than honored.

const warnCompactProfile = "--compact does not apply to PDF/A-1b"

// runRemediateTo runs the remediate command and returns the output bytes and
// whatever it wrote to stderr.
func runRemediateTo(t *testing.T, src, profile string, extra ...string) ([]byte, string) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "out.pdf")
	args := append([]string{"remediate", "--profile", profile, "-o", out}, extra...)
	args = append(args, src)

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), args, &stdout, &stderr); rc != exitOK {
		t.Fatalf("remediate %s %v: exit %d, stderr: %s", profile, extra, rc, stderr.String())
	}
	data, err := os.ReadFile(out) //nolint:gosec // path is this test's own temp file
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	return data, stderr.String()
}

// TestRemediateCompact_PacksForAProfileThatPermitsIt is what the flag exists
// for. The precondition carries half the test: asserting the unflagged output
// carries no object stream is what stops the packed assertion passing on a
// document that was already packed, or on one remediation never rewrote.
func TestRemediateCompact_PacksForAProfileThatPermitsIt(t *testing.T) {
	src := compactFixtureFile(t)

	plain, _ := runRemediateTo(t, src, "PDF/A-2b")
	if bytes.Contains(plain, []byte("/Type /ObjStm")) {
		t.Fatal("precondition: remediation packed the output without the flag")
	}

	packed, stderr := runRemediateTo(t, src, "PDF/A-2b", "--compact")
	if !bytes.Contains(packed, []byte("/Type /ObjStm")) {
		t.Error("--compact produced no object stream on a profile that permits one")
	}
	if strings.Contains(stderr, "--compact does not apply") {
		t.Errorf("refused compaction on a profile that permits it; stderr: %q", stderr)
	}
	if len(packed) >= len(plain) {
		t.Errorf("--compact did not shrink the output: %d bytes vs %d", len(packed), len(plain))
	}
}

// TestRemediateCompact_RefusedForPDFA1 is the trap this refusal exists to
// avoid: a smaller file that no longer conforms to the profile the caller
// asked for is worse than the larger conformant one.
func TestRemediateCompact_RefusedForPDFA1(t *testing.T) {
	src := compactFixtureFile(t)

	out, stderr := runRemediateTo(t, src, "PDF/A-1b", "--compact")
	if !strings.Contains(stderr, warnCompactProfile) {
		t.Errorf("no warning that PDF/A-1 cannot be compacted; stderr: %q", stderr)
	}
	if bytes.Contains(out, []byte("/Type /ObjStm")) {
		t.Error("packed a PDF/A-1 output; ISO 19005-1, 6.1.4 forbids the xref stream that needs")
	}
	if bytes.Contains(out, []byte("/Type /XRef")) {
		t.Error("PDF/A-1 output carries a cross-reference stream, which 6.1.4 forbids")
	}
}

// TestRemediateCompact_SilentWithoutTheFlag: a caller who never asked for
// compaction must not be told about it, whatever profile they target.
func TestRemediateCompact_SilentWithoutTheFlag(t *testing.T) {
	src := compactFixtureFile(t)

	_, stderr := runRemediateTo(t, src, "PDF/A-1b")
	if strings.Contains(stderr, "--compact") {
		t.Errorf("mentioned --compact to a caller who did not pass it; stderr: %q", stderr)
	}
}

// TestRemediateCompact_OutputStaysReadable asks a reader rather than the
// bytes. A smaller file nothing can open is not a saving.
func TestRemediateCompact_OutputStaysReadable(t *testing.T) {
	src := compactFixtureFile(t)
	out := filepath.Join(t.TempDir(), "out.pdf")

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{
		"remediate", "--profile", "PDF/A-2b", "--compact", "-o", out, src,
	}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("remediate --compact: exit %d, stderr: %s", rc, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if rc := run(context.Background(), []string{"info", out}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("the compacted output does not open: exit %d, stderr: %s", rc, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Pages:") {
		t.Errorf("info reported no page count for the compacted output: %s", stdout.String())
	}
}

func TestCompactForbiddenByProfile(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		profile string
		want    bool
	}{
		// ISO 19005-1, 6.1.4 forbids cross-reference streams.
		{"PDF/A-1a", true},
		{"PDF/A-1b", true},
		// Parts 2 and later are written against PDF 1.7 and permit them.
		{"PDF/A-2a", false},
		{"PDF/A-2b", false},
		{"PDF/A-3b", false},
		{"PDF/A-4", false},
		{"PDF/UA-1", false},
		{"PDF/UA-2", false},
		{"WTPDF-1.0-Reuse", false},
		// Factur-X and every ZUGFeRD generation remediate to a PDF/A-3 base.
		{"Factur-X", false},
		{"ZUGFeRD-1.0", false},
		// A typo must not silently disable compaction: the command rejects an
		// unknown profile before the write is ever reached.
		{"PDF/A-9z", false},
		{"", false},
	} {
		t.Run(tc.profile, func(t *testing.T) {
			t.Parallel()
			if got := compactForbiddenByProfile(tc.profile); got != tc.want {
				t.Errorf("compactForbiddenByProfile(%q) = %v, want %v", tc.profile, got, tc.want)
			}
		})
	}
}
