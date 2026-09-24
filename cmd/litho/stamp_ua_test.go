package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// uaViolations runs validate against PDF/UA-1 and returns its report.
//
// The exit code is deliberately ignored. A fixture built from untaggedTextPDF
// stays non-conformant for reasons this test is not about, an unembedded
// standard font, a missing ViewerPreferences and a missing /Metadata, so
// validate keeps reporting failure either way. Asserting exitOK here would
// encode a falsehood; the report text is what carries the answer.
func uaViolations(t *testing.T, path string) string {
	t.Helper()

	var stdout, stderr bytes.Buffer
	run(context.Background(), []string{"validate", "--profile", "PDF/UA-1", path}, &stdout, &stderr)
	if stdout.Len() == 0 {
		t.Fatalf("validate %s produced no report; stderr: %s", path, stderr.String())
	}
	return stdout.String()
}

// TestStampText_DoesNotBreakContentMarking pins that stamping a tagged document
// leaves it as conformant as it found it, for the one clause stamping controls.
//
// ISO 14289-1, 7.1 test 3 requires every piece of page content to be either
// tagged as real content or marked as an artifact. A stamp is neither: it is
// appended to the content stream after the structure tree was built, so it
// belongs to no marked-content sequence at all. ISO 32000-1, 14.8.2.2 names
// watermarks as artifacts, which is what the stamp has to declare itself to be.
//
// Measured before the fix, stamping an autotagged page took PDF/UA-1 from 4
// violations to 7: this clause, the natural-language clause reporting "text
// outside the structure tree", and an unembedded stamp font. The first two are
// the missing artifact marking. The third is a separate defect that artifact
// marking does not address, so this test does not assert it away.
//
// This test is the only thing standing between the repair and a silent
// regression.
//
// # Corpus evidence
//
//   - All corpora: the corpus gate scores neither stamping nor this clause.
func TestStampText_DoesNotBreakContentMarking(t *testing.T) {
	dir := t.TempDir()
	src := writeTempPDF(t, untaggedTextPDF())
	tagged := filepath.Join(dir, "tagged.pdf")
	stamped := filepath.Join(dir, "stamped.pdf")

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{"autotag", "-o", tagged, src}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("autotag: exit %d, stderr: %s", rc, stderr.String())
	}

	// Precondition: the tagged document is clean on this clause. Without it a
	// passing test could mean the stamp was fine, or that the clause never
	// fired here at all.
	const clause = "clause 7.1 test 3"
	if before := uaViolations(t, tagged); strings.Contains(before, clause) {
		t.Fatalf("precondition: the autotagged document already violates %s:\n%s", clause, before)
	}

	stdout.Reset()
	stderr.Reset()
	if rc := run(context.Background(), []string{
		"stamp-text", "-text", "WATERMARK", "-x", "100", "-y", "400", "-o", stamped, tagged,
	}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("stamp-text: exit %d, stderr: %s", rc, stderr.String())
	}

	if after := uaViolations(t, stamped); strings.Contains(after, clause) {
		t.Errorf("stamping introduced %s: the stamp content is neither tagged nor marked as an artifact.\n%s",
			clause, after)
	}
}
