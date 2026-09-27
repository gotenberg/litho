package pdf

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestPipelineEncryptsItsOutputAcrossARestructuringStage pins that an
// encryption a stage asks for survives the Split and Merge stages after it:
// the pipeline's output opens under that password, and only under it.
//
// A Split or a Merge reopens the current document from an intermediate file.
// That file was written under the encryption an earlier Apply stage asked for,
// and reopened with no password, so a pipeline encrypting with a user password
// failed with "reopening intermediate file: invalid PDF password"; with an
// owner password alone the reopen succeeded and the Split or the Merge wrote
// the output in the clear, dropping the encryption the caller asked for.
func TestPipelineEncryptsItsOutputAcrossARestructuringStage(t *testing.T) {
	t.Parallel()

	const (
		userPw  = "user-secret"
		ownerPw = "owner-secret"
	)
	plain := threePagePDF()
	encrypt := func(user, owner string) func(context.Context, *Writer) error {
		return func(_ context.Context, w *Writer) error {
			w.Encrypt(EncryptConfig{UserPassword: user, OwnerPassword: owner, Algorithm: EncryptAES256, Permissions: -1})
			return nil
		}
	}
	setTitle := func(_ context.Context, w *Writer) error {
		w.SetMetadata(Metadata{Title: "after the split"})
		return nil
	}

	for _, tc := range []struct {
		name  string
		build func(src, other *Reader) *Pipeline
		// open is the password the output opens under; "" when only the
		// owner password was set, so the output opens without one.
		open  string
		pages int
		title string
	}{
		{
			name: "an encryption, then a split as the last stage",
			build: func(src, _ *Reader) *Pipeline {
				return NewPipeline().Open(src).Apply(encrypt(userPw, ownerPw)).Split([]int{1, 2})
			},
			open:  userPw,
			pages: 2,
		},
		{
			name: "an encryption, then a merge as the last stage",
			build: func(src, other *Reader) *Pipeline {
				return NewPipeline().Open(src).Apply(encrypt(userPw, ownerPw)).Merge([]*Reader{other})
			},
			open:  userPw,
			pages: 6,
		},
		{
			name: "an encryption, a split, then an edit",
			build: func(src, _ *Reader) *Pipeline {
				return NewPipeline().Open(src).Apply(encrypt(userPw, ownerPw)).Split([]int{1}).Apply(setTitle)
			},
			open:  userPw,
			pages: 1,
			title: "after the split",
		},
		{
			name: "an encryption, a split, then a merge",
			build: func(src, other *Reader) *Pipeline {
				return NewPipeline().Open(src).Apply(encrypt(userPw, ownerPw)).Split([]int{1}).Merge([]*Reader{other})
			},
			open:  userPw,
			pages: 4,
		},
		{
			name: "an encryption under an owner password alone, then a split",
			build: func(src, _ *Reader) *Pipeline {
				return NewPipeline().Open(src).Apply(encrypt("", ownerPw)).Split([]int{1, 2})
			},
			pages: 2,
		},
		{
			name: "a split asking for encryption, then an edit",
			build: func(src, _ *Reader) *Pipeline {
				return NewPipeline().Open(src).
					Split([]int{2, 3}, WithEncryption(EncryptConfig{UserPassword: userPw, OwnerPassword: ownerPw, Algorithm: EncryptAES128, Permissions: -1})).
					Apply(setTitle)
			},
			open:  userPw,
			pages: 2,
			title: "after the split",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			src, err := Open(ctx, bytes.NewReader(plain), int64(len(plain)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer src.Close()
			other, err := Open(ctx, bytes.NewReader(plain), int64(len(plain)))
			if err != nil {
				t.Fatalf("Open the other document: %v", err)
			}
			defer other.Close()

			var out bytes.Buffer
			if _, err := tc.build(src, other).WriteTo(ctx, &out); err != nil {
				t.Fatalf("WriteTo: %v", err)
			}

			if tc.open != "" {
				if _, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len())); !errors.Is(err, ErrInvalidPassword) {
					t.Fatalf("the output opens without its user password (%v)", err)
				}
			}
			got, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()), WithPassword(tc.open))
			if err != nil {
				t.Fatalf("the output does not open under its user password: %v", err)
			}
			defer got.Close()
			if !got.IsEncrypted() {
				t.Fatal("the output is not encrypted")
			}
			if n, err := got.PageCount(ctx); err != nil || n != tc.pages {
				t.Errorf("the output has %d pages (%v), want %d", n, err, tc.pages)
			}
			if tc.title != "" {
				meta, err := got.Metadata(ctx)
				if err != nil {
					t.Fatalf("Metadata: %v", err)
				}
				if meta["Title"] != tc.title {
					t.Errorf("Title = %q, want %q", meta["Title"], tc.title)
				}
			}
		})
	}
}

// TestPipelineReencryptsAnEncryptedSourceAcrossASplit pins the intermediate
// file of an encrypted source that a stage re-encrypts: it is written in the
// clear, not passed through as the source's ciphertext, which the pipeline
// could not reopen, and the output opens under the new password only.
func TestPipelineReencryptsAnEncryptedSourceAcrossASplit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	const oldPw, newPw = "old-secret", "new-secret"
	encrypted := encryptedWithUserPassword(t, oldPw, "owner")
	src, err := Open(ctx, bytes.NewReader(encrypted), int64(len(encrypted)), WithPassword(oldPw))
	if err != nil {
		t.Fatalf("Open with the user password: %v", err)
	}
	defer src.Close()

	var out bytes.Buffer
	_, err = NewPipeline().Open(src).
		Apply(func(_ context.Context, w *Writer) error {
			w.Encrypt(EncryptConfig{UserPassword: newPw, OwnerPassword: "owner", Algorithm: EncryptAES256, Permissions: -1})
			return nil
		}).
		Split([]int{2}).
		WriteTo(ctx, &out)
	if err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if _, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()), WithPassword(oldPw)); !errors.Is(err, ErrInvalidPassword) {
		t.Errorf("the output opens under the source's password (%v)", err)
	}
	got, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()), WithPassword(newPw))
	if err != nil {
		t.Fatalf("the output does not open under the new password: %v", err)
	}
	defer got.Close()
	if n, err := got.PageCount(ctx); err != nil || n != 1 {
		t.Errorf("the output has %d pages (%v), want 1", n, err)
	}
}

// TestPipelineOutputEncryptionFollowsTheLastRequest pins which encryption the
// output carries when several stages ask for one: the last request wins, as
// [WithEncryption] on [Writer.WriteTo] overrides [Writer.Encrypt].
func TestPipelineOutputEncryptionFollowsTheLastRequest(t *testing.T) {
	t.Parallel()

	first := EncryptConfig{UserPassword: "first", OwnerPassword: "owner", Algorithm: EncryptAES256, Permissions: -1}
	second := EncryptConfig{UserPassword: "second", OwnerPassword: "owner", Algorithm: EncryptAES128, Permissions: -1}
	encrypt := func(cfg EncryptConfig) func(context.Context, *Writer) error {
		return func(_ context.Context, w *Writer) error {
			w.Encrypt(cfg)
			return nil
		}
	}

	for _, tc := range []struct {
		name  string
		build func(src *Reader) *Pipeline
		opts  []WriteOption
	}{
		{
			name:  "WithEncryption on WriteTo after a split",
			build: func(src *Reader) *Pipeline { return NewPipeline().Open(src).Apply(encrypt(first)).Split([]int{1}) },
			opts:  []WriteOption{WithEncryption(second)},
		},
		{
			name: "WithEncryption on WriteTo after a split and an edit",
			build: func(src *Reader) *Pipeline {
				return NewPipeline().Open(src).Apply(encrypt(first)).Split([]int{1}).
					Apply(func(_ context.Context, w *Writer) error { w.SetMetadata(Metadata{Title: "t"}); return nil })
			},
			opts: []WriteOption{WithEncryption(second)},
		},
		{
			name: "a second Encrypt after a split",
			build: func(src *Reader) *Pipeline {
				return NewPipeline().Open(src).Apply(encrypt(first)).Split([]int{1}).Apply(encrypt(second))
			},
		},
		{
			name: "WithEncryption on a later split",
			build: func(src *Reader) *Pipeline {
				return NewPipeline().Open(src).Apply(encrypt(first)).Split([]int{1, 2}).Split([]int{1}, WithEncryption(second))
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			plain := threePagePDF()
			src, err := Open(ctx, bytes.NewReader(plain), int64(len(plain)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer src.Close()

			var out bytes.Buffer
			if _, err := tc.build(src).WriteTo(ctx, &out, tc.opts...); err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
			if _, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()), WithPassword(first.UserPassword)); !errors.Is(err, ErrInvalidPassword) {
				t.Errorf("the output opens under the first user password (%v), want only the second", err)
			}
			got, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()), WithPassword(second.UserPassword))
			if err != nil {
				t.Fatalf("the output does not open under the second user password: %v", err)
			}
			defer got.Close()
			if n, err := got.PageCount(ctx); err != nil || n != 1 {
				t.Errorf("the output has %d pages (%v), want 1", n, err)
			}
		})
	}
}

// TestPipelineRefusesWhatAnEncryptedWriterRefusesAcrossARestructuringStage
// pins that an encryption carried past a Split or a Merge is refused beside
// what [Writer.Encrypt] is refused beside on one Writer: a signature
// ([ErrSignFullRewriteIncompatible]) and a PDF/A remediation
// ([ErrPDFAForbidsEncryption]), rather than one of the two being dropped. A
// signature asked for beside the encryption, in the stage before the Split, is
// refused too, although the intermediate file it would go on is written in the
// clear.
func TestPipelineRefusesWhatAnEncryptedWriterRefusesAcrossARestructuringStage(t *testing.T) {
	t.Parallel()

	cfg := EncryptConfig{UserPassword: "user", OwnerPassword: "owner", Algorithm: EncryptAES256, Permissions: -1}
	encrypt := func(_ context.Context, w *Writer) error { w.Encrypt(cfg); return nil }
	for _, tc := range []struct {
		name  string
		build func(src *Reader, signer Signer) *Pipeline
		want  error
	}{
		{
			name: "a signature after the split",
			build: func(src *Reader, signer Signer) *Pipeline {
				return NewPipeline().Open(src).Apply(encrypt).Split([]int{1}).
					Apply(func(_ context.Context, w *Writer) error { w.Sign(signer); return nil })
			},
			want: ErrSignFullRewriteIncompatible,
		},
		{
			name: "a signature beside the encryption, before the split",
			build: func(src *Reader, signer Signer) *Pipeline {
				return NewPipeline().Open(src).
					Apply(func(ctx context.Context, w *Writer) error { w.Sign(signer); return encrypt(ctx, w) }).
					Split([]int{1})
			},
			want: ErrSignFullRewriteIncompatible,
		},
		{
			name: "a PDF/A remediation after the split",
			build: func(src *Reader, _ Signer) *Pipeline {
				return NewPipeline().Open(src).Apply(encrypt).Split([]int{1}).
					Apply(func(ctx context.Context, w *Writer) error {
						_, err := w.RemediatePDFA(ctx, models.PDFA2b)
						return err
					})
			},
			want: ErrPDFAForbidsEncryption,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			plain := threePagePDF()
			src, err := Open(ctx, bytes.NewReader(plain), int64(len(plain)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer src.Close()

			signer := newCMSTestSigner(t)
			var out bytes.Buffer
			_, err = tc.build(src, signer).WriteTo(ctx, &out)
			if !errors.Is(err, tc.want) {
				t.Fatalf("WriteTo returned %v, want %v", err, tc.want)
			}
			if out.Len() != 0 {
				t.Errorf("the refused pipeline wrote %d bytes", out.Len())
			}
			if n := signer.calls.Load(); n != 0 {
				t.Errorf("the signer ran %d times for a refused pipeline", n)
			}
		})
	}
}
