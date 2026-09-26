package pdf

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestWriterRefusesToSignAnEncryptedSourceAfterARemediation pins that a
// remediation does not lift the refusal to sign an encrypted source, whatever
// was queued before it and however many rounds it kept.
//
// The signature of an encrypted document is written in that document: its
// ByteRange covers the bytes of the file (ISO 32000-1, 12.8.1), and the strings
// of its signature dictionary are encrypted under the file's key like every
// other string (ISO 32000-1, 7.6.1). litho cannot write that update. A
// remediation reads a decrypted copy of the source when an edit is queued
// before it or when it keeps a later round, and the refusal read the document
// the Writer edits, so it fired on the source alone and the signed output was
// the decrypted copy: a signature the caller asked for on the encrypted
// document went on a document they never asked to decrypt.
func TestWriterRefusesToSignAnEncryptedSourceAfterARemediation(t *testing.T) {
	t.Parallel()

	const userPw = "secret"
	encrypted := encryptedWithUserPassword(t, userPw, "owner")
	edit := func(w *Writer) { w.SetMetadata(Metadata{Title: "edited"}) }
	pdfa := func(ctx context.Context, w *Writer) error {
		_, err := w.RemediatePDFA(ctx, models.PDFA2b)
		return err
	}

	for _, tc := range []struct {
		name  string
		write func(ctx context.Context, t *testing.T, r *Reader, signer Signer, out *bytes.Buffer) error
	}{
		{
			name: "a PDF/A-2b remediation",
			write: func(ctx context.Context, t *testing.T, r *Reader, signer Signer, out *bytes.Buffer) error {
				w := NewWriter(r)
				defer w.Close()
				if err := pdfa(ctx, w); err != nil {
					t.Fatalf("RemediatePDFA: %v", err)
				}
				w.Sign(signer)
				return w.WriteTo(ctx, out)
			},
		},
		{
			name: "an edit then a PDF/A-2b remediation",
			write: func(ctx context.Context, t *testing.T, r *Reader, signer Signer, out *bytes.Buffer) error {
				w := NewWriter(r)
				defer w.Close()
				edit(w)
				if err := pdfa(ctx, w); err != nil {
					t.Fatalf("RemediatePDFA: %v", err)
				}
				w.Sign(signer)
				return w.WriteTo(ctx, out)
			},
		},
		{
			name: "Sign, an edit, then a PDF/UA-1 remediation",
			write: func(ctx context.Context, t *testing.T, r *Reader, signer Signer, out *bytes.Buffer) error {
				w := NewWriter(r)
				defer w.Close()
				w.Sign(signer)
				edit(w)
				if _, err := w.RemediatePDFUA(ctx, models.PDFUA1); err != nil {
					t.Fatalf("RemediatePDFUA: %v", err)
				}
				return w.WriteTo(ctx, out)
			},
		},
		{
			name: "a remediation keeping a later round",
			write: func(ctx context.Context, t *testing.T, r *Reader, signer Signer, out *bytes.Buffer) error {
				w := NewWriter(r)
				defer w.Close()
				e := &roundsEngine{steps: []roundsStep{{keys: []string{"PageLayout"}}, {keys: []string{"PageMode"}}}}
				if _, err := w.remediate(ctx, engineTarget, DefaultRegressionTolerance, e.revalidate, e.run); err != nil {
					t.Fatalf("remediate: %v", err)
				}
				if len(e.disabled) != 2 {
					t.Fatalf("the engine ran %d rounds, want 2", len(e.disabled))
				}
				w.Sign(signer)
				return w.WriteTo(ctx, out)
			},
		},
		{
			name: "a pipeline signing after an edit and a remediation",
			write: func(ctx context.Context, _ *testing.T, r *Reader, signer Signer, out *bytes.Buffer) error {
				_, err := NewPipeline().Open(r).
					Apply(func(ctx context.Context, w *Writer) error {
						edit(w)
						return pdfa(ctx, w)
					}).
					Apply(func(_ context.Context, w *Writer) error {
						w.Sign(signer)
						return nil
					}).
					WriteTo(ctx, out)
				return err
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			r, err := Open(ctx, bytes.NewReader(encrypted), int64(len(encrypted)), WithPassword(userPw))
			if err != nil {
				t.Fatalf("Open with the user password: %v", err)
			}
			defer r.Close()
			signer := newCMSTestSigner(t)
			var out bytes.Buffer
			err = tc.write(ctx, t, r, signer, &out)
			if !errors.Is(err, ErrSignFullRewriteIncompatible) {
				t.Fatalf("the write returned %v, want ErrSignFullRewriteIncompatible", err)
			}
			if out.Len() != 0 {
				t.Errorf("the refused write wrote %d bytes", out.Len())
			}
			if n := signer.calls.Load(); n != 0 {
				t.Errorf("the signer ran %d times for a refused write", n)
			}
		})
	}
}

// TestWriter_SignEncryptedSource_ReturnsIncompatible covers the one arm of the
// sign-compatibility guard that no caller action produces.
//
// TestWriter_SignWithFullRewriteOps_ReturnsIncompatible exercises the arms a
// caller reaches by doing something to a plaintext document: Encrypt,
// RemoveEncryption, WithEncryption. This one is reached by the SOURCE alone.
// [Writer.WriteTo] also refuses when the source is encrypted, so signing an
// untouched encrypted file is rejected while the caller has requested no
// encryption or decryption at all, and the error names operations they never
// asked for.
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
