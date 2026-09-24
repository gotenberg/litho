package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pipeTextOf reads a document's text back through the text command, optionally
// including artifact-marked content.
//
// Through the command rather than pkg/pdf directly, for the same reason
// listFormFields and pageOneImages do: litho writes an incremental update, so
// the superseded objects are still in the file and a byte scan answers neither
// direction.
func pipeTextOf(t *testing.T, path string, artifacts bool) string {
	t.Helper()

	args := []string{"text"}
	if artifacts {
		args = append(args, "-artifacts")
	}
	args = append(args, path)

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), args, &stdout, &stderr); rc != exitOK {
		t.Fatalf("text %v: exit %d, stderr: %s", args, rc, stderr.String())
	}
	return stdout.String()
}

// The four pipe stages that never executed through the command.
//
// Measured by GROUP HEAD across the existing CLI tests rather than by counting
// stage names as substrings: the substring count is wrong because the stage
// names appear in doc comments and as flag VALUES (a blast-radius test passes
// `-title=rotate` precisely to prove a flag value is not a stage boundary).
// Nine of the thirteen stages that existed when this was written already ran;
// decrypt, flatten-form, stamp-image and stamp-text did not, so a defect in the
// code they share with the others would sit unobserved.
//
// The set has since grown to eighteen. The five output-producing stages added
// later are covered in pipe_output_stages_cli_test.go.
//
// Each stage is asserted through a SIBLING SUBCOMMAND rather than by scanning
// the output bytes. litho writes an incremental update, so a superseded object
// still carries the old value and a byte scan answers neither direction.

// TestPipe_DecryptStageRemovesEncryption also pins the plumbing that carries
// the password, which no other stage test covers.
//
// decrypt takes no flags of its own: the stage body is a bare Apply calling
// RemoveEncryption, and the password reaches the source through the GLOBAL -p,
// parsePipeGlobals -> openPDF. A stage that opened the document without it
// would fail before RemoveEncryption ever ran.
//
// # Corpus evidence
//
//   - All corpora: no corpus gate reaches cmd/litho, so nothing but these
//     tests watches the decrypt, flatten-form, stamp-image and stamp-text
//     stages.
func TestPipe_DecryptStageRemovesEncryption(t *testing.T) {
	src := onePageTempPDF(t)
	enc := encryptTempPDF(t, src, "s3cret")
	out := filepath.Join(t.TempDir(), "decrypted.pdf")

	if opensWithoutAPassword(t, enc) {
		t.Fatal("the fixture opens with no password, so the stage would have nothing to remove")
	}

	rc, errOut := runPipeArgs(t, []string{
		"pipe", "-p", "s3cret", enc, "decrypt", "-o", out,
	})
	if rc != exitOK {
		t.Fatalf("pipe decrypt: exit %d, stderr: %s", rc, errOut)
	}
	if !opensWithoutAPassword(t, out) {
		t.Error("the output still needs a password: the decrypt stage did not run, " +
			"or the global -p never reached openPDF")
	}
}

// TestPipe_FlattenFormStageRemovesTheFields mirrors the standalone command's
// contract: after flattening, the document reports no form fields.
func TestPipe_FlattenFormStageRemovesTheFields(t *testing.T) {
	src := writeTempPDF(t, formFixturePDF())
	out := filepath.Join(t.TempDir(), "flattened.pdf")

	if before := listFormFields(t, src); len(before) != 2 {
		t.Fatalf("fixture reports %d field(s), want 2", len(before))
	}

	rc, errOut := runPipeArgs(t, []string{
		"pipe", src, "flatten-form", "-o", out,
	})
	if rc != exitOK {
		t.Fatalf("pipe flatten-form: exit %d, stderr: %s", rc, errOut)
	}
	if after := listFormFields(t, out); len(after) != 0 {
		t.Errorf("output still reports %d field(s): the flatten-form stage did not run", len(after))
	}
}

// TestPipe_StampImageStageEmbedsTheImage reads the image XObjects back through
// the images command, the way the standalone stamp-image test does.
func TestPipe_StampImageStageEmbedsTheImage(t *testing.T) {
	src := onePageTempPDF(t)
	img := filepath.Join(t.TempDir(), "stamp.png")
	if err := os.WriteFile(img, onePixelPNG(t), 0o600); err != nil {
		t.Fatalf("writing the PNG fixture: %v", err)
	}
	out := filepath.Join(t.TempDir(), "stamped.pdf")

	if before := pageOneImages(t, src); len(before) != 0 {
		t.Fatalf("fixture already carries %d image(s), so the assertion below proves nothing", len(before))
	}

	rc, errOut := runPipeArgs(t, []string{
		"pipe", src, "stamp-image", "-image", img, "-o", out,
	})
	if rc != exitOK {
		t.Fatalf("pipe stamp-image: exit %d, stderr: %s", rc, errOut)
	}
	if after := pageOneImages(t, out); len(after) != 1 {
		t.Errorf("output carries %d image(s), want 1: the stamp-image stage did not run", len(after))
	}
}

// TestPipe_StampTextStageMarksTheStampAsAnArtifact asserts through `text
// -artifacts`, and the flag is the point rather than a detail.
//
// Stamps declare themselves artifacts (ISO 32000-1, 14.8.2.2: a watermark
// carries no meaning for a reader), so ExtractText OMITS them unless
// WithArtifacts is passed. Asserting with a plain `text` would report the stamp
// missing and pass only when the stage silently did nothing; checking BOTH
// directions is what makes this test able to fail for the right reason.
func TestPipe_StampTextStageMarksTheStampAsAnArtifact(t *testing.T) {
	const stamp = "CONFIDENTIAL"

	src := onePageTempPDF(t)
	out := filepath.Join(t.TempDir(), "stamped.pdf")

	rc, errOut := runPipeArgs(t, []string{
		"pipe", src, "stamp-text", "-text", stamp, "-o", out,
	})
	if rc != exitOK {
		t.Fatalf("pipe stamp-text: exit %d, stderr: %s", rc, errOut)
	}

	withArtifacts := pipeTextOf(t, out, true)
	if !strings.Contains(withArtifacts, stamp) {
		t.Errorf("text -artifacts does not report %q: the stamp-text stage did not run", stamp)
	}
	plain := pipeTextOf(t, out, false)
	if strings.Contains(plain, stamp) {
		t.Errorf("plain text reports %q: the stamp is not marked as an artifact", stamp)
	}
}
