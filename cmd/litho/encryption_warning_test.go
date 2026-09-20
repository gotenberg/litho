package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// twoPageTempPDF builds a two-page document by merging the one-page fixture
// with itself.
//
// Every PDF helper in this package is /Count 1, and delete needs a document it
// can remove a page from without emptying it: "cannot delete all 1 pages" is a
// real refusal that would fail the test for a reason unrelated to encryption.
func twoPageTempPDF(t *testing.T) string {
	t.Helper()

	plain := writeTempPDF(t, untaggedTextPDF())
	merged := filepath.Join(t.TempDir(), "two-page.pdf")

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(),
		[]string{"merge", "-o", merged, plain, plain}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("merge: exit %d, stderr: %s", rc, stderr.String())
	}
	return merged
}

// onePageTempPDF is the plain single-page fixture, as a path.
func onePageTempPDF(t *testing.T) string {
	t.Helper()
	return writeTempPDF(t, untaggedTextPDF())
}

// encryptTempPDF runs the encrypt command over src and returns the encrypted
// path. Building the fixture through the CLI keeps the test honest: the input
// really is encrypted by the same code path a user would use, rather than by a
// hand-built dictionary the reader might treat loosely.
func encryptTempPDF(t *testing.T, src, password string) string {
	t.Helper()

	enc := filepath.Join(t.TempDir(), "encrypted.pdf")

	var stdout, stderr bytes.Buffer
	rc := run(context.Background(),
		[]string{"encrypt", "-user-password", password, "-o", enc, src}, &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("encrypt: exit %d, stderr: %s", rc, stderr.String())
	}
	return enc
}

const encryptionWarning = "the input is encrypted but the output will not be"

// TestSplit_WarnsWhenEncryptionIsDropped pins the silent half of the
// confidentiality downgrade.
//
// Measured: an encrypted PDF put through split, merge, delete, insert or rotate
// comes out readable with no password, because the reader decrypts every object
// as it resolves it and the copied graph is plaintext. That default is
// deliberate, but the user supplied a password to READ the file and gets an
// unprotected one back with nothing said. The warning is the whole fix; no
// default changes.
func TestSplit_WarnsWhenEncryptionIsDropped(t *testing.T) {
	enc := encryptTempPDF(t, onePageTempPDF(t), "pw")
	out := filepath.Join(t.TempDir(), "split.pdf")

	var stdout, stderr bytes.Buffer
	rc := run(context.Background(),
		[]string{"split", "-p", "pw", "-pages", "1", "-o", out, enc}, &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("split: exit %d, stderr: %s", rc, stderr.String())
	}
	if !strings.Contains(stderr.String(), encryptionWarning) {
		t.Errorf("no warning that encryption was dropped; stderr = %q", stderr.String())
	}
}

// TestSplit_NoWarningWhenTheOutputIsEncrypted is the control that stops the
// warning becoming noise: asking for an encrypted output drops nothing, so
// there is nothing to report.
func TestSplit_NoWarningWhenTheOutputIsEncrypted(t *testing.T) {
	enc := encryptTempPDF(t, onePageTempPDF(t), "pw")
	out := filepath.Join(t.TempDir(), "split.pdf")

	var stdout, stderr bytes.Buffer
	rc := run(context.Background(),
		[]string{"split", "-p", "pw", "-pages", "1", "-user-password", "out-pw", "-o", out, enc},
		&stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("split: exit %d, stderr: %s", rc, stderr.String())
	}
	if strings.Contains(stderr.String(), encryptionWarning) {
		t.Errorf("warned about dropping encryption while encrypting; stderr = %q", stderr.String())
	}
}

// TestSplit_NoWarningForAPlainInput is the second control. Without it the
// warning could fire unconditionally and both tests above would still pass.
func TestSplit_NoWarningForAPlainInput(t *testing.T) {
	plain := onePageTempPDF(t)
	out := filepath.Join(t.TempDir(), "split.pdf")

	var stdout, stderr bytes.Buffer
	rc := run(context.Background(),
		[]string{"split", "-pages", "1", "-o", out, plain}, &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("split: exit %d, stderr: %s", rc, stderr.String())
	}
	if strings.Contains(stderr.String(), encryptionWarning) {
		t.Errorf("warned on an unencrypted input; stderr = %q", stderr.String())
	}
}

// TestPageOperations_WarnWhenEncryptionIsDropped covers the commands that have
// no encryption flag at all, which is where the warning matters most: their
// only remedy is to re-encrypt the result, so they must at least say so.
//
// Each case supplies its own input because the page count matters: delete needs
// two pages to remove one.
func TestPageOperations_WarnWhenEncryptionIsDropped(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  func(t *testing.T) string
		args func(in, out string) []string
	}{
		{"delete", twoPageTempPDF, func(in, out string) []string {
			return []string{"delete", "-p", "pw", "-pages", "1", "-o", out, in}
		}},
		{"rotate", onePageTempPDF, func(in, out string) []string {
			return []string{"rotate", "-p", "pw", "-angle", "90", "-o", out, in}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			enc := encryptTempPDF(t, tc.src(t), "pw")
			out := filepath.Join(t.TempDir(), "out.pdf")

			var stdout, stderr bytes.Buffer
			rc := run(context.Background(), tc.args(enc, out), &stdout, &stderr)
			if rc != exitOK {
				t.Fatalf("%s: exit %d, stderr: %s", tc.name, rc, stderr.String())
			}
			if !strings.Contains(stderr.String(), encryptionWarning) {
				t.Errorf("%s: no warning that encryption was dropped; stderr = %q",
					tc.name, stderr.String())
			}
		})
	}
}
