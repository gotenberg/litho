package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestPipe_EditFailureNamesItsStage pins that an edit failing when a later
// stage writes the document names the stage that queued it. The pipeline's own
// error names only the stage that wrote it, the final write here, so the stamp
// of a page its one-page source lacks used to read "applying mutation 0".
func TestPipe_EditFailureNamesItsStage(t *testing.T) {
	src := writeTempPDF(t, redactableCLIPDF())
	out := filepath.Join(t.TempDir(), "out.pdf")

	rc, _, stderr := runPipeCLI(t,
		"pipe", "-o", out, src, "stamp-pdf", "-stamp", src, "-stamp-page", "9", "rotate", "-angle", "90")

	if rc == exitOK {
		t.Fatal("a stamp of a page the source lacks was written")
	}
	if !strings.Contains(stderr, `"stamp-pdf #1"`) || !strings.Contains(stderr, "Writer.StampPDF") {
		t.Errorf("the error does not name the stage that queued the stamp; stderr: %s", stderr)
	}
}
