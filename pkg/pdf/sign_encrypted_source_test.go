package pdf

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

// TestWriter_SignEncryptedSource_ReturnsIncompatible covers the one arm of the
// sign-compatibility guard that no caller action produces.
//
// TestWriter_SignWithFullRewriteOps_ReturnsIncompatible exercises the five arms
// a caller reaches by doing something to a plaintext document: Encrypt,
// RemoveEncryption, ClearMetadata, WithFullRewrite, WithEncryption. This one is
// reached by the SOURCE alone. [Writer.WriteTo] also refuses when
// w.reader.doc.IsEncrypted, so signing an untouched encrypted file is rejected
// while the caller has requested no full-rewrite operation at all, and the
// error names operations they never asked for.
//
// The document must be AUTHENTICATED, and that is the precondition below rather
// than an assumption: an unauthenticated encrypted source is refused earlier by
// [ErrEncryptedNotAuthenticated], which would make this test pass against the
// wrong guard entirely.
func TestWriter_SignEncryptedSource_ReturnsIncompatible(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	const userPw = "secret"
	encrypted := encryptedWithUserPassword(t, userPw, "owner")

	r, err := Open(ctx, bytes.NewReader(encrypted), int64(len(encrypted)), WithPassword(userPw))
	if err != nil {
		t.Fatalf("Open with the user password: %v", err)
	}
	defer r.Close()

	if !r.IsEncrypted() {
		t.Fatal("precondition: the fixture is not encrypted, so this exercises no encryption arm")
	}

	w := NewWriter(r)
	w.Sign(&mockSigner{})

	var out bytes.Buffer
	err = w.WriteTo(ctx, &out)
	if !errors.Is(err, ErrSignFullRewriteIncompatible) {
		t.Errorf("WriteTo = %v, want errors.Is(err, ErrSignFullRewriteIncompatible)", err)
	}
	// Not the unauthenticated guard: that one fires earlier, and matching it
	// here would mean the fixture never reached the sign-compatibility check.
	if errors.Is(err, ErrEncryptedNotAuthenticated) {
		t.Errorf("WriteTo = %v, matched ErrEncryptedNotAuthenticated: the source was "+
			"authenticated, so this test did not reach the guard it targets", err)
	}
}

// TestWriter_SignPlaintextSource_IsAccepted is the control that keeps the test
// above honest.
//
// Without it, a guard that rejected every signed write would satisfy the
// assertion there. Signing a plaintext document with no other operation must
// still succeed, because that is the whole supported case for signing.
func TestWriter_SignPlaintextSource_IsAccepted(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := writerTestPDF()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	if r.IsEncrypted() {
		t.Fatal("precondition: the plaintext fixture reports as encrypted")
	}

	w := NewWriter(r)
	w.Sign(&mockSigner{})

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo on a plaintext source: %v, want it accepted", err)
	}
	if out.Len() == 0 {
		t.Error("WriteTo produced no output")
	}
}
