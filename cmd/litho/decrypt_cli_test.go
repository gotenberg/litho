package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

// opensWithoutAPassword reports whether the document can be read with no -p,
// which is the only sound way to ask whether encryption is gone.
//
// Scanning the output bytes for /Encrypt would not do it. litho writes
// incremental updates, so a superseded encryption dictionary can still be
// present in a file that no longer uses one, and the token may sit inside an
// object stream where a byte search never finds it either way. What a reader
// experiences is the contract, so a reader is what this asks.
func opensWithoutAPassword(t *testing.T, path string) bool {
	t.Helper()

	var stdout, stderr bytes.Buffer
	return run(context.Background(), []string{"metadata", path}, &stdout, &stderr) == exitOK
}

// TestDecrypt_RemovesThePasswordRequirement pins what the command is for.
//
// RemoveEncryption itself is covered at pkg/pdf level; what had nothing behind
// it is that the command reaches it. The fixture is built through the encrypt
// command rather than by hand, so the input really is encrypted by the same
// path a user would use.
//
// The precondition is half the test: asserting the encrypted file CANNOT be
// read without a password is what stops the final assertion passing vacuously
// on a document that was never encrypted.
func TestDecrypt_RemovesThePasswordRequirement(t *testing.T) {
	enc := encryptTempPDF(t, onePageTempPDF(t), "secret")
	out := filepath.Join(t.TempDir(), "decrypted.pdf")

	if opensWithoutAPassword(t, enc) {
		t.Fatal("precondition: the encrypted fixture opened with no password")
	}

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{
		"decrypt", "-p", "secret", "-o", out, enc,
	}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("decrypt: exit %d, stderr: %s", rc, stderr.String())
	}

	if !opensWithoutAPassword(t, out) {
		t.Error("the decrypted document still demands a password")
	}
}

// TestDecrypt_RejectsAWrongPassword covers the openPDF arm of
// runSimpleMutation: the document exists and is well formed, but the password
// does not open it, so the failure happens before anything is written.
func TestDecrypt_RejectsAWrongPassword(t *testing.T) {
	enc := encryptTempPDF(t, onePageTempPDF(t), "secret")
	out := filepath.Join(t.TempDir(), "decrypted.pdf")

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{
		"decrypt", "-p", "wrong", "-o", out, enc,
	}, &stdout, &stderr); rc == exitOK {
		t.Fatal("decrypt accepted a wrong password")
	}
	if stderr.Len() == 0 {
		t.Error("decrypt failed silently: nothing on stderr")
	}
	if _, err := os.Stat(out); err == nil {
		t.Errorf("%s exists: a failed decrypt must not leave an output file", out)
	}
}
