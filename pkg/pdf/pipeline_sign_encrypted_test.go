package pdf

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

// TestPipelineRefusesToSignAfterRestructuringAnEncryptedSource pins that a
// [Pipeline] refuses to sign a document derived from an encrypted source, as
// [Writer.WriteTo] refuses to sign an encrypted source, however many Split and
// Merge stages lie between the source and the signature.
//
// A Split or a Merge writes the current document to an intermediate file in the
// clear and reopens it as the next stage's document, and the refusal read that
// document alone: a signature asked for on the encrypted source went on a
// decrypted copy the caller never asked for, where a Writer on the same source
// refuses. A Merge brings the pages of every Reader it is given into the
// document, so an encrypted Reader merged into a plaintext one counts too.
func TestPipelineRefusesToSignAfterRestructuringAnEncryptedSource(t *testing.T) {
	t.Parallel()

	const userPw = "secret"
	encrypted := encryptedWithUserPassword(t, userPw, "owner")
	plain := threePagePDF()
	sign := func(signer Signer) func(context.Context, *Writer) error {
		return func(_ context.Context, w *Writer) error {
			w.Sign(signer)
			return nil
		}
	}

	for _, tc := range []struct {
		name  string
		build func(enc, other *Reader, signer Signer) *Pipeline
	}{
		{
			name: "a split",
			build: func(enc, _ *Reader, signer Signer) *Pipeline {
				return NewPipeline().Open(enc).Split([]int{1, 2}).Apply(sign(signer))
			},
		},
		{
			name: "two splits",
			build: func(enc, _ *Reader, signer Signer) *Pipeline {
				return NewPipeline().Open(enc).Split([]int{1, 2}).Split([]int{1}).Apply(sign(signer))
			},
		},
		{
			name: "an edit, then a split",
			build: func(enc, _ *Reader, signer Signer) *Pipeline {
				return NewPipeline().Open(enc).
					Apply(func(_ context.Context, w *Writer) error {
						w.SetMetadata(Metadata{Title: "edited"})
						return nil
					}).
					Split([]int{1}).Apply(sign(signer))
			},
		},
		{
			name: "a decryption, then a split",
			build: func(enc, _ *Reader, signer Signer) *Pipeline {
				return NewPipeline().Open(enc).
					Apply(func(_ context.Context, w *Writer) error {
						w.RemoveEncryption()
						return nil
					}).
					Split([]int{1}).Apply(sign(signer))
			},
		},
		{
			name: "a merge of a plaintext document into it",
			build: func(enc, other *Reader, signer Signer) *Pipeline {
				return NewPipeline().Open(enc).Merge([]*Reader{other}).Apply(sign(signer))
			},
		},
		{
			name: "a merge of it into a plaintext document",
			build: func(enc, other *Reader, signer Signer) *Pipeline {
				return NewPipeline().Open(other).Merge([]*Reader{enc}).Apply(sign(signer))
			},
		},
		{
			name: "a split, then a merge of a plaintext document",
			build: func(enc, other *Reader, signer Signer) *Pipeline {
				return NewPipeline().Open(enc).Split([]int{1}).Merge([]*Reader{other}).Apply(sign(signer))
			},
		},
		{
			name: "a split, then Sign and an edit in one stage",
			build: func(enc, _ *Reader, signer Signer) *Pipeline {
				return NewPipeline().Open(enc).Split([]int{1}).
					Apply(func(_ context.Context, w *Writer) error {
						w.Sign(signer)
						w.SetMetadata(Metadata{Title: "edited"})
						return nil
					})
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			enc, err := Open(ctx, bytes.NewReader(encrypted), int64(len(encrypted)), WithPassword(userPw))
			if err != nil {
				t.Fatalf("Open with the user password: %v", err)
			}
			defer enc.Close()
			if !enc.IsEncrypted() {
				t.Fatal("precondition: the fixture is not encrypted")
			}
			other, err := Open(ctx, bytes.NewReader(plain), int64(len(plain)))
			if err != nil {
				t.Fatalf("Open the plaintext document: %v", err)
			}
			defer other.Close()

			signer := newCMSTestSigner(t)
			var out bytes.Buffer
			_, err = tc.build(enc, other, signer).WriteTo(ctx, &out)
			if !errors.Is(err, ErrSignFullRewriteIncompatible) {
				t.Fatalf("WriteTo returned %v, want ErrSignFullRewriteIncompatible", err)
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

// TestPipelineSignsAfterRestructuringAPlaintextSource is the control that keeps
// the test above honest: a guard that refused every signature after a Split or
// a Merge would satisfy it. Signing a plaintext document the pipeline split or
// merged stays the supported case.
func TestPipelineSignsAfterRestructuringAPlaintextSource(t *testing.T) {
	t.Parallel()

	plain := threePagePDF()
	for _, tc := range []struct {
		name  string
		pages int
		build func(src, other *Reader, signer Signer) *Pipeline
	}{
		{
			name:  "a split",
			pages: 2,
			build: func(src, _ *Reader, signer Signer) *Pipeline {
				return NewPipeline().Open(src).Split([]int{1, 2}).
					Apply(func(_ context.Context, w *Writer) error { w.Sign(signer); return nil })
			},
		},
		{
			name:  "a merge",
			pages: 6,
			build: func(src, other *Reader, signer Signer) *Pipeline {
				return NewPipeline().Open(src).Merge([]*Reader{other}).
					Apply(func(_ context.Context, w *Writer) error { w.Sign(signer); return nil })
			},
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

			signer := newCMSTestSigner(t)
			var out bytes.Buffer
			if _, err := tc.build(src, other, signer).WriteTo(ctx, &out); err != nil {
				t.Fatalf("WriteTo on a plaintext source: %v, want it signed", err)
			}
			if n := signatureCount(t, out.Bytes()); n != 1 {
				t.Errorf("the output holds %d signatures, want 1", n)
			}
			if n := signer.calls.Load(); n != 1 {
				t.Errorf("the signer ran %d times, want once", n)
			}
			got, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
			if err != nil {
				t.Fatalf("reopening the output: %v", err)
			}
			defer got.Close()
			if n, err := got.PageCount(ctx); err != nil || n != tc.pages {
				t.Errorf("the output has %d pages (%v), want %d", n, err, tc.pages)
			}
		})
	}
}
