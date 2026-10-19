package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// remediate rewrites the whole file for most documents but not all, so it
// takes --compact the way pipe does rather than the way rotate does: the flag
// is accepted, and applies when a rewrite happens.
//
// PDF/A-1 is the one profile where it must withhold packing. ISO 19005-1,
// 6.1.4 forbids cross-reference streams, which packing requires. Measured on
// one file with and without compaction, PDF/A-1b gains exactly one violation
// (clause 6.1.4) while PDF/A-2b, PDF/A-3b and PDF/UA-1 gain none. Remediating a
// document into conformance and then packing it back out of conformance would
// defeat the command, so packing is withheld there and dedup still runs.

const warnCompactProfile = "--compact does not pack objects into object streams for PDF/A-1b"

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
	if strings.Contains(stderr, "does not pack objects") {
		t.Errorf("refused compaction on a profile that permits it; stderr: %q", stderr)
	}
	if len(packed) >= len(plain) {
		t.Errorf("--compact did not shrink the output: %d bytes vs %d", len(packed), len(plain))
	}
}

// TestRemediateCompact_WithholdsPackingForPDFA1 is the trap the withholding
// exists to avoid: a smaller file that no longer conforms to the profile the
// caller asked for is worse than the larger conformant one.
func TestRemediateCompact_WithholdsPackingForPDFA1(t *testing.T) {
	src := compactFixtureFile(t)

	out, stderr := runRemediateTo(t, src, "PDF/A-1b", "--compact")
	if !strings.Contains(stderr, warnCompactProfile) {
		t.Errorf("no warning that PDF/A-1 cannot be packed; stderr: %q", stderr)
	}
	if bytes.Contains(out, []byte("/Type /ObjStm")) {
		t.Error("packed a PDF/A-1 output; ISO 19005-1, 6.1.4 forbids the xref stream that needs")
	}
	if bytes.Contains(out, []byte("/Type /XRef")) {
		t.Error("PDF/A-1 output carries a cross-reference stream, which 6.1.4 forbids")
	}
}

// twinStreamsPDF builds two pages whose content streams are separate objects
// holding the same bytes, which dedup merges into one.
func twinStreamsPDF() []byte {
	content := "0 0 m 300 300 l S\n"
	stream := fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(content), content)
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 5 0 R >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 6 0 R >>",
		stream,
		stream,
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

// streamCount counts the stream objects data carries.
func streamCount(data []byte) int {
	return bytes.Count(data, []byte("endstream"))
}

// TestRemediateCompact_DedupsForPDFA1 pins that withholding packing at PDF/A-1
// withholds nothing else. --compact was refused outright there, so the twin
// content streams of a PDF/A-1b output stayed two although merging them writes
// no construct ISO 19005-1 forbids: the output keeps a traditional
// cross-reference table and the PDF 1.4 header.
func TestRemediateCompact_DedupsForPDFA1(t *testing.T) {
	src := writeTempPDF(t, twinStreamsPDF())

	plain, _ := runRemediateTo(t, src, "PDF/A-1b")
	deduped, stderr := runRemediateTo(t, src, "PDF/A-1b", "--compact")

	if got, want := streamCount(deduped), streamCount(plain)-1; got != want {
		t.Errorf("the PDF/A-1b output carries %d streams with --compact and %d without, want %d", got, streamCount(plain), want)
	}
	if bytes.Contains(deduped, []byte("/Type /XRef")) || bytes.Contains(deduped, []byte("/Type /ObjStm")) {
		t.Error("the PDF/A-1b output carries a construct ISO 19005-1, 6.1.4 forbids")
	}
	if !bytes.HasPrefix(deduped, []byte("%PDF-1.4")) {
		t.Errorf("the PDF/A-1b output declares %q, want PDF 1.4", deduped[:8])
	}
	if !strings.Contains(stderr, warnCompactProfile) {
		t.Errorf("no notice that packing was withheld; stderr: %q", stderr)
	}
	if !strings.Contains(stderr, "Duplicate streams are still merged") {
		t.Errorf("the notice does not say dedup still runs; stderr: %q", stderr)
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
			if got := packingForbiddenByProfile(tc.profile); got != tc.want {
				t.Errorf("packingForbiddenByProfile(%q) = %v, want %v", tc.profile, got, tc.want)
			}
		})
	}
}
