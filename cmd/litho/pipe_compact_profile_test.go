package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pipeA1bViolations counts what the output violates against PDF/A-1b, asking
// the library rather than reading the bytes. The whole point of the refusal is
// conformance, so the assertion is keyed on a verdict, not on a construct.
func pipeA1bViolations(t *testing.T, path string) []string {
	t.Helper()

	ctx := context.Background()
	r, closer, err := openPDF(ctx, path, "")
	if err != nil {
		t.Fatalf("opening %s: %v", path, err)
	}
	defer closer()

	violations, err := r.ValidatePDFAReport(ctx, pdfaProfiles["PDF/A-1b"])
	if err != nil {
		t.Fatalf("validating %s: %v", path, err)
	}
	msgs := make([]string, 0, len(violations))
	for _, v := range violations {
		msgs = append(msgs, v.Error())
	}
	return msgs
}

// runPipeCompactProfile runs a pipeline and returns its output and stderr.
func runPipeCompactProfile(t *testing.T, args []string) ([]byte, string) {
	t.Helper()

	rc, errOut := runPipeArgs(t, args)
	if rc != exitOK {
		t.Fatalf("pipe %v: exit %d, stderr: %s", args, rc, errOut)
	}
	out := args[len(args)-1]
	data, err := os.ReadFile(out) //nolint:gosec // the test's own temp path
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	return data, errOut
}

// TestPipe_CompactRefusedForAPDFA1RemediateStage pins a conformance defect, not
// a missing notice.
//
// `b6ced949` made the standalone remediate refuse --compact for PDF/A-1,
// because ISO 19005-1, 6.1.4 forbids the cross-reference stream that object
// streams require. pipe accepted the same pipeline and packed it anyway, so
// `litho pipe doc remediate --profile PDF/A-1b --compact` returned 0 and wrote
// a document that no longer conformed to the profile it had just been
// remediated to.
//
// MEASURED before the fix, pipe against pipe so the difference is the flag
// alone: without --compact the output validated clean at PDF/A-1b, with it the
// output carried exactly one violation, "clause 6.1.4 test 3: Xref streams
// shall not be used". The standalone command refused and stayed clean.
func TestPipe_CompactRefusedForAPDFA1RemediateStage(t *testing.T) {
	src := writeTempPDF(t, compactFixturePDF())

	// The precondition carries half the test: if the uncompacted pipeline were
	// already non-conformant, a violation below would prove nothing about the
	// flag.
	plainOut := filepath.Join(t.TempDir(), "plain.pdf")
	plain, _ := runPipeCompactProfile(t, []string{
		"pipe", src, "remediate", "--profile", "PDF/A-1b", "-o", plainOut,
	})
	if v := pipeA1bViolations(t, plainOut); len(v) != 0 {
		t.Fatalf("precondition: the uncompacted pipeline is already non-conformant: %v", v)
	}
	if bytes.Contains(plain, []byte("/Type /ObjStm")) {
		t.Fatal("precondition: the pipeline packed without the flag")
	}

	packedOut := filepath.Join(t.TempDir(), "packed.pdf")
	packed, errOut := runPipeCompactProfile(t, []string{
		"pipe", src, "remediate", "--profile", "PDF/A-1b", "--compact", "-o", packedOut,
	})

	if v := pipeA1bViolations(t, packedOut); len(v) != 0 {
		t.Errorf("--compact cost the pipeline the profile it remediated to: %v", v)
	}
	if bytes.Contains(packed, []byte("/Type /ObjStm")) {
		t.Error("packed a PDF/A-1 output; ISO 19005-1, 6.1.4 forbids the xref stream it needs")
	}
	if bytes.Contains(packed, []byte("/Type /XRef")) {
		t.Error("PDF/A-1 output carries a cross-reference stream, which 6.1.4 forbids")
	}
	if !strings.Contains(errOut, warnCompactProfile) {
		t.Errorf("refused compaction without saying so; stderr: %q", errOut)
	}
}

// TestPipe_CompactRefusalReadsTheEqualsForm covers the spelling the group scan
// could miss.
//
// runPipe reads the profile out of the raw stage tokens rather than the stage's
// FlagSet, so it has to handle `--profile=X` as well as `--profile X`. A scan
// that only understood the spaced form would refuse nothing here and pack a
// non-conformant file, which is exactly the defect this file exists to stop.
func TestPipe_CompactRefusalReadsTheEqualsForm(t *testing.T) {
	src := writeTempPDF(t, compactFixturePDF())
	out := filepath.Join(t.TempDir(), "out.pdf")

	packed, errOut := runPipeCompactProfile(t, []string{
		"pipe", src, "remediate", "--profile=PDF/A-1b", "--compact", "-o", out,
	})
	if bytes.Contains(packed, []byte("/Type /ObjStm")) {
		t.Error("the equals form escaped the refusal and packed a PDF/A-1 output")
	}
	if !strings.Contains(errOut, warnCompactProfile) {
		t.Errorf("no refusal for the equals form; stderr: %q", errOut)
	}
}

// TestPipe_CompactStillPacksForAPermittedProfile is the control that stops the
// refusal becoming blanket. Parts 2 and later permit cross-reference streams,
// so a remediate stage targeting them must still compact.
func TestPipe_CompactStillPacksForAPermittedProfile(t *testing.T) {
	src := writeTempPDF(t, compactFixturePDF())
	out := filepath.Join(t.TempDir(), "out.pdf")

	packed, errOut := runPipeCompactProfile(t, []string{
		"pipe", src, "remediate", "--profile", "PDF/A-2b", "--compact", "-o", out,
	})
	if !bytes.Contains(packed, []byte("/Type /ObjStm")) {
		t.Error("refused compaction on a profile that permits it, so the flag is inert")
	}
	if strings.Contains(errOut, "--compact does not apply") {
		t.Errorf("refused compaction on PDF/A-2b, which permits it; stderr: %q", errOut)
	}
}

// TestPipe_NoCompactRefusalWithoutTheFlag: a caller who never asked for
// compaction must not be told about it, whatever profile a stage targets.
func TestPipe_NoCompactRefusalWithoutTheFlag(t *testing.T) {
	src := writeTempPDF(t, compactFixturePDF())
	out := filepath.Join(t.TempDir(), "out.pdf")

	_, errOut := runPipeCompactProfile(t, []string{
		"pipe", src, "remediate", "--profile", "PDF/A-1b", "-o", out,
	})
	if strings.Contains(errOut, "--compact") {
		t.Errorf("mentioned --compact to a caller who did not pass it; stderr: %q", errOut)
	}
}
