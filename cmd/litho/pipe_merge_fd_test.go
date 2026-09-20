package main

import (
	"os"
	"path/filepath"
	"testing"
)

// openFDs counts the process's open file descriptors.
//
// /dev/fd is measured reliable on this platform: opening two files raises the
// count by exactly two and closing them returns it, with no drift. A test that
// asserts on it therefore needs no tolerance, and a difference of one is a real
// descriptor rather than sampling noise.
func openFDs(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/dev/fd")
	if err != nil {
		t.Skipf("/dev/fd unavailable, cannot count descriptors: %v", err)
	}
	return len(entries)
}

// TestPipe_MergeClosesItsSources pins that the merge stage releases every
// document it opens.
//
// addStage's merge case opened each positional argument with openPDF and threw
// the cleanup function away (`r, _, err := openPDF(...)`), the only one of the
// package's openPDF call sites to do so. A path that is not "-" goes through
// pdf.OpenFile, which owns the *os.File and stores it as Reader.closer, so the
// discarded function was the only thing that would ever close it: one leaked
// descriptor per merge source, held until the process exited.
//
// The fix cannot close them inside addStage. Pipeline.Merge keeps the *Reader
// pointers (it copies the slice, not the documents) and the merge stage reads
// them when Pipeline.WriteTo executes, so releasing them at the end of addStage
// would be a use-after-close. They belong to runPipe, released after the write,
// exactly as the standalone merge command does with its own closers.
func TestPipe_MergeClosesItsSources(t *testing.T) {
	src := onePageTempPDF(t)
	a := onePageTempPDF(t)
	b := onePageTempPDF(t)
	c := onePageTempPDF(t)
	out := filepath.Join(t.TempDir(), "merged.pdf")

	before := openFDs(t)
	rc, errOut := runPipeArgs(t, []string{
		"pipe", src, "merge", a, b, c, "-o", out,
	})
	if rc != exitOK {
		t.Fatalf("pipe merge: exit %d, stderr: %s", rc, errOut)
	}
	after := openFDs(t)

	if after > before {
		t.Errorf("pipe leaked %d descriptor(s) across a 3-source merge (%d -> %d)",
			after-before, before, after)
		t.Log("each merge source is opened with openPDF and its closer discarded, " +
			"so the *os.File behind Reader.closer is never released")
	}
	if got := pageCountOf(t, out); got != 4 {
		t.Errorf("merged output has %d page(s), want 4: the merge did not actually run, "+
			"so the descriptor count above proves nothing", got)
	}
}

// TestPipe_WithoutMergeLeaksNothing is the discrimination control.
//
// It runs a pipe of the same shape with no merge stage. If this also leaked,
// the test above would be measuring something general about pipe or about the
// descriptor counter rather than the merge path, and its failure would not
// point at the discarded closer.
func TestPipe_WithoutMergeLeaksNothing(t *testing.T) {
	src := onePageTempPDF(t)
	out := filepath.Join(t.TempDir(), "rotated.pdf")

	before := openFDs(t)
	rc, errOut := runPipeArgs(t, []string{
		"pipe", src, "rotate", "-angle", "90", "-o", out,
	})
	if rc != exitOK {
		t.Fatalf("pipe rotate: exit %d, stderr: %s", rc, errOut)
	}
	after := openFDs(t)

	if after > before {
		t.Errorf("control leaked %d descriptor(s) with NO merge stage (%d -> %d): "+
			"the merge test is not isolating the merge path", after-before, before, after)
	}
}
