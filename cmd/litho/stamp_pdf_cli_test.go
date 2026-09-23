package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stampPDFFails runs stamp-pdf and asserts it refused: non-zero exit,
// something on stderr, and no output file left behind.
//
// The output-file check is the part a usage assertion would miss. createOutput
// runs before WriteTo, so any failure after it has to reach abandonOutput or
// the caller keeps a truncated file that looks like a result.
func stampPDFFails(t *testing.T, out string, args []string) {
	t.Helper()

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), args, &stdout, &stderr); rc == exitOK {
		t.Fatalf("stamp-pdf %v succeeded; stderr: %s", args, stderr.String())
	}
	if stderr.Len() == 0 {
		t.Errorf("stamp-pdf %v failed silently: nothing on stderr", args)
	}
	if _, err := os.Stat(out); err == nil {
		t.Errorf("%s exists: a failed stamp-pdf must not leave an output file", out)
	}
}

// TestStampPDF_OverlaysAndKeepsThePageCount pins the success path.
//
// The overlay itself is covered at pkg/pdf level; what has never been covered
// is that the command reaches it. Page count is the read-back because it is
// what a stamp must NOT change: StampPDF draws the source page as a Form
// XObject on each target page, so gaining or losing a page means the command
// did something other than overlay.
func TestStampPDF_OverlaysAndKeepsThePageCount(t *testing.T) {
	src := writeTempPDF(t, untaggedTextPDF())
	stamp := writeTempPDF(t, untaggedTextPDF())
	out := filepath.Join(t.TempDir(), "stamped.pdf")

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{
		"stamp-pdf", "-stamp", stamp, "-x", "50", "-y", "50", "-o", out, src,
	}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("stamp-pdf: exit %d, stderr: %s", rc, stderr.String())
	}

	before := pageCountOf(t, src)
	after := pageCountOf(t, out)
	if before != after {
		t.Errorf("page count changed from %d to %d: stamp-pdf overlays, it does not add pages", before, after)
	}
}

// pageCountOf reports how many page rows the pages command lists.
func pageCountOf(t *testing.T, path string) int {
	t.Helper()

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{"pages", path}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("pages %s: exit %d, stderr: %s", path, rc, stderr.String())
	}
	n := 0
	for line := range strings.SplitSeq(stdout.String(), "\n") {
		f := strings.Fields(line)
		if len(f) >= 5 && f[0] != "Page" {
			n++
		}
	}
	if n == 0 {
		t.Fatalf("pages %s reported no page rows: %q", path, stdout.String())
	}
	return n
}

// TestStampPDF_RejectsAnUnreadableStampFile covers the -stamp open path, which
// is a separate openPDF call from the target document's and reports its own
// message.
func TestStampPDF_RejectsAnUnreadableStampFile(t *testing.T) {
	src := writeTempPDF(t, untaggedTextPDF())
	dir := t.TempDir()
	out := filepath.Join(dir, "stamped.pdf")

	stampPDFFails(t, out, []string{
		"stamp-pdf", "-stamp", filepath.Join(dir, "absent.pdf"), "-o", out, src,
	})
}

// TestStampPDF_RejectsAStampFileThatIsNotAPDF is the second half of that path.
// The file exists and reads cleanly, so it reaches the parser rather than the
// open: a handler that only checked for existence would pass the test above
// and fail this one.
func TestStampPDF_RejectsAStampFileThatIsNotAPDF(t *testing.T) {
	src := writeTempPDF(t, untaggedTextPDF())
	dir := t.TempDir()
	out := filepath.Join(dir, "stamped.pdf")

	notAPDF := filepath.Join(dir, "notapdf.pdf")
	if err := os.WriteFile(notAPDF, []byte("this is not a PDF"), 0o600); err != nil {
		t.Fatalf("writing the decoy stamp: %v", err)
	}

	stampPDFFails(t, out, []string{"stamp-pdf", "-stamp", notAPDF, "-o", out, src})
}

// TestStampPDF_RejectsAnOutOfRangeStampPage covers -stamp-page, which selects a
// page inside the stamp document and defaults to 1.
func TestStampPDF_RejectsAnOutOfRangeStampPage(t *testing.T) {
	src := writeTempPDF(t, untaggedTextPDF())
	stamp := writeTempPDF(t, untaggedTextPDF())
	out := filepath.Join(t.TempDir(), "stamped.pdf")

	stampPDFFails(t, out, []string{
		"stamp-pdf", "-stamp", stamp, "-stamp-page", "99", "-o", out, src,
	})
}

// TestStampPDF_MissingStampIsAUsageError pins the required-flag check: -stamp,
// -o and the input file are all required.
func TestStampPDF_MissingStampIsAUsageError(t *testing.T) {
	src := writeTempPDF(t, untaggedTextPDF())
	out := filepath.Join(t.TempDir(), "stamped.pdf")

	stampPDFFails(t, out, []string{"stamp-pdf", "-o", out, src})
}

// An encrypted stamp was unusable, and no flag could reach it.
//
// The stamp was opened with a hardcoded empty password while insert and merge
// open their own second documents with the command's -p. There is no
// --stamp-password, so `stamp-pdf -p pw -stamp enc.pdf` answered "invalid PDF
// password" to a caller who had supplied the right one, and the capability was
// unreachable rather than merely awkward. Measured before the change on both
// surfaces: rc=1, no output.
//
// A password is a no-op on an unencrypted document, so the ordinary plaintext
// stamp is unaffected; the rows below cover that direction too.

// TestStampPDF_OpensAnEncryptedStampWithTheSamePassword pins the capability.
func TestStampPDF_OpensAnEncryptedStampWithTheSamePassword(t *testing.T) {
	src := writeTempPDF(t, untaggedTextPDF())
	stamp := encryptTempPDF(t, writeTempPDF(t, untaggedTextPDF()), "s3cret")
	out := filepath.Join(t.TempDir(), "stamped.pdf")

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{
		"stamp-pdf", "-p", "s3cret", "-stamp", stamp, "-o", out, src,
	}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("an encrypted stamp with the right password was refused: exit %d, stderr: %s",
			rc, stderr.String())
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("no output file: %v", err)
	}
}

// TestStampPDF_WarnsWhenOnlyTheStampIsEncrypted pins the consequence of the
// change: the stamp is now an input that can carry protection, so it belongs in
// the variadic warning beside the target.
//
// The target here is PLAINTEXT, so the warning can only come from the stamp. A
// version that passed just the target would stay silent and hand back an
// unprotected document without saying so.
func TestStampPDF_WarnsWhenOnlyTheStampIsEncrypted(t *testing.T) {
	src := writeTempPDF(t, untaggedTextPDF())
	stamp := encryptTempPDF(t, writeTempPDF(t, untaggedTextPDF()), "s3cret")
	out := filepath.Join(t.TempDir(), "stamped.pdf")

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{
		"stamp-pdf", "-p", "s3cret", "-stamp", stamp, "-o", out, src,
	}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("exit %d, stderr: %s", rc, stderr.String())
	}
	if !strings.Contains(stderr.String(), encryptionWarning) {
		t.Errorf("the stamp's protection was dropped silently; stderr: %q", stderr.String())
	}
	if !opensWithoutAPassword(t, out) {
		t.Error("the output is encrypted, so nothing was dropped and this row proves nothing")
	}
}

// TestStampPDF_StillRefusesAWrongStampPassword is the control. Honouring -p
// must not mean accepting any stamp: a password that does not open the file
// still fails, and leaves nothing behind.
func TestStampPDF_StillRefusesAWrongStampPassword(t *testing.T) {
	src := writeTempPDF(t, untaggedTextPDF())
	stamp := encryptTempPDF(t, writeTempPDF(t, untaggedTextPDF()), "s3cret")
	out := filepath.Join(t.TempDir(), "stamped.pdf")

	stampPDFFails(t, out, []string{
		"stamp-pdf", "-p", "wrong", "-stamp", stamp, "-o", out, src,
	})
}
