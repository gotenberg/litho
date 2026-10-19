package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// wantCompactIncremental is the notice pipe prints when --compact reached a
// final write that appended an incremental update.
const wantCompactIncremental = "--compact does not apply to an incremental update"

// TestPipe_CompactWarnsOnAnIncrementalUpdate pins that pipe reports a
// --compact it could not apply. A pipeline whose stages leave an incremental
// update of the caller's file, a rotation here, appends to the source's bytes,
// and packing, dedup and image recompression run on a full rewrite only: the
// output came back uncompacted, exit 0 and silent, byte-identical to the one
// written without the flag.
func TestPipe_CompactWarnsOnAnIncrementalUpdate(t *testing.T) {
	src := writeTempPDF(t, compactFixturePDF())
	plain, _ := runPipeCompactProfile(t, []string{
		"pipe", src, "rotate", "-angle", "90", "-o", filepath.Join(t.TempDir(), "plain.pdf"),
	})
	out, stderr := runPipeCompactProfile(t, []string{
		"pipe", src, "rotate", "-angle", "90", "--compact", "-o", filepath.Join(t.TempDir(), "compact.pdf"),
	})
	if !bytes.Equal(out, plain) {
		t.Fatal("precondition: --compact changed the output, so the write was not an incremental update")
	}
	if !strings.Contains(stderr, wantCompactIncremental) {
		t.Errorf("pipe applied no compaction and did not say so; stderr: %q", stderr)
	}
}

// TestPipe_CompactDoesNotWarnOnARewrite is the control: a pipeline whose
// output is a full rewrite, a split then a rotation here, is compacted and
// prints no such notice.
func TestPipe_CompactDoesNotWarnOnARewrite(t *testing.T) {
	src := writeTempPDF(t, compactFixturePDF())
	out, stderr := runPipeCompactProfile(t, []string{
		"pipe", src, "split", "-pages", "2", "rotate", "-angle", "90", "--compact", "-o", filepath.Join(t.TempDir(), "compact.pdf"),
	})
	if !bytes.Contains(out, []byte("/ObjStm")) {
		t.Fatal("precondition: the rewritten output packs no object")
	}
	if strings.Contains(stderr, wantCompactIncremental) {
		t.Errorf("pipe warned on a compacted output; stderr: %q", stderr)
	}
}

// TestPipe_CompactWarnsOnAnIncrementalRemediateStage covers the remediate
// stage: a document already conformant to the profile needs no structural
// repair, so the stage leaves an update, and the warning reaches it too. The
// PDF/A-1b case names no `decrypt --compact` remedy, which would pack objects
// that ISO 19005-1, 6.1.4 forbids.
func TestPipe_CompactWarnsOnAnIncrementalRemediateStage(t *testing.T) {
	for _, tc := range []struct {
		profile    string
		wantRemedy bool
	}{
		{profile: "PDF/A-2b", wantRemedy: true},
		{profile: "PDF/A-1b", wantRemedy: false},
	} {
		t.Run(tc.profile, func(t *testing.T) {
			remediated, _ := runRemediateTo(t, writeTempPDF(t, compactFixturePDF()), tc.profile)
			src := writeTempPDF(t, remediated)
			out, stderr := runPipeCompactProfile(t, []string{
				"pipe", src, "remediate", "--profile", tc.profile, "--compact", "-o", filepath.Join(t.TempDir(), "out.pdf"),
			})
			if !bytes.HasPrefix(out, remediated) {
				t.Fatal("precondition: the second remediation rewrote the file")
			}
			if !strings.Contains(stderr, wantCompactIncremental) {
				t.Errorf("pipe applied no compaction and did not say so; stderr: %q", stderr)
			}
			if got := strings.Contains(stderr, "litho decrypt --compact`, which rewrites"); got != tc.wantRemedy {
				t.Errorf("the warning names the decrypt --compact remedy: %v, want %v; stderr: %q", got, tc.wantRemedy, stderr)
			}
		})
	}
}

// TestRemediate_CompactWarnsOnAnIncrementalUpdate is the standalone half: a
// remediation that repairs nothing structural leaves an update, and
// remediate --compact said nothing about the compaction it could not apply.
func TestRemediate_CompactWarnsOnAnIncrementalUpdate(t *testing.T) {
	remediated, _ := runRemediateTo(t, writeTempPDF(t, compactFixturePDF()), "PDF/A-2b")
	out, stderr := runRemediateTo(t, writeTempPDF(t, remediated), "PDF/A-2b", "--compact", "--recompress-images")
	if !bytes.HasPrefix(out, remediated) {
		t.Fatal("precondition: the second remediation rewrote the file")
	}
	if want := "--compact and --recompress-images do not apply to an incremental update"; !strings.Contains(stderr, want) {
		t.Errorf("stderr lacks %q: %q", want, stderr)
	}
	if want := "litho decrypt --compact --recompress-images"; !strings.Contains(stderr, want) {
		t.Errorf("stderr lacks the remedy %q: %q", want, stderr)
	}
}

// TestPipe_RecompressImagesWarnsOnAnIncrementalUpdate pins that the warning
// names --recompress-images alone when it is the flag that went unapplied.
func TestPipe_RecompressImagesWarnsOnAnIncrementalUpdate(t *testing.T) {
	src := writeTempPDF(t, compactFixturePDF())
	_, stderr := runPipeCompactProfile(t, []string{
		"pipe", src, "rotate", "-angle", "90", "--recompress-images", "-o", filepath.Join(t.TempDir(), "out.pdf"),
	})
	if want := "--recompress-images does not apply to an incremental update"; !strings.Contains(stderr, want) {
		t.Errorf("stderr lacks %q: %q", want, stderr)
	}
	if strings.Contains(stderr, "decrypt --compact") {
		t.Errorf("the remedy asks for --compact, which the caller did not: %q", stderr)
	}
}
