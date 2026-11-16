package pdf

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestSignatureRemoval_RefusedByDefault pins that every output that would drop
// a signature of its input returns [ErrSignaturesDropped] and writes nothing,
// unless [WithSignatureRemoval] allows it.
func TestSignatureRemoval_RefusedByDefault(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, tc := range []struct {
		name  string
		write func(t *testing.T, src *Reader, dst *bytes.Buffer, opts ...WriteOption) error
	}{
		{"Writer.WriteTo, a full rewrite", func(t *testing.T, src *Reader, dst *bytes.Buffer, opts ...WriteOption) error {
			w := NewWriter(src)
			defer w.Close()
			if err := w.DeletePages(2); err != nil {
				t.Fatalf("DeletePages: %v", err)
			}
			return w.WriteTo(ctx, dst, opts...)
		}},
		{"Writer.WriteTo, a rewrite to sign", func(t *testing.T, src *Reader, dst *bytes.Buffer, opts ...WriteOption) error {
			w := NewWriter(src)
			defer w.Close()
			w.Sign(newCMSTestSigner(t))
			return w.WriteTo(ctx, dst, append(opts, WithFullRewrite())...)
		}},
		{"Writer.WriteTo, after a remediation that rewrote", func(t *testing.T, src *Reader, dst *bytes.Buffer, opts ...WriteOption) error {
			w := NewWriter(src)
			defer w.Close()
			if err := w.DeletePages(2); err != nil {
				t.Fatalf("DeletePages: %v", err)
			}
			if _, err := w.RemediatePDFA(ctx, models.PDFA2b); err != nil {
				t.Fatalf("RemediatePDFA: %v", err)
			}
			return w.WriteTo(ctx, dst, opts...)
		}},
		{"Split", func(_ *testing.T, src *Reader, dst *bytes.Buffer, opts ...WriteOption) error {
			return Split(ctx, src, []int{1}, dst, opts...)
		}},
		{"Merge", func(_ *testing.T, src *Reader, dst *bytes.Buffer, opts ...WriteOption) error {
			return Merge(ctx, []*Reader{src, src}, dst, opts...)
		}},
		{"Compose", func(_ *testing.T, src *Reader, dst *bytes.Buffer, opts ...WriteOption) error {
			return Compose(ctx, 612, 792, []Placement{{Source: src}}, dst, opts...)
		}},
		{"Pipeline.WriteTo, an edit flushed before a split", func(_ *testing.T, src *Reader, dst *bytes.Buffer, opts ...WriteOption) error {
			_, err := NewPipeline().Open(src).Apply(func(_ context.Context, w *Writer) error { return w.DeletePages(2) }).Split([]int{1}).WriteTo(ctx, dst, opts...)
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			src := openDeclaration(t, certifiedPDF(), false)
			defer src.Close()
			var refused bytes.Buffer
			err := tc.write(t, src, &refused)
			if !errors.Is(err, ErrSignaturesDropped) {
				t.Fatalf("without WithSignatureRemoval: err = %v, want ErrSignaturesDropped", err)
			}
			if !IsInvalidInput(err) {
				t.Errorf("IsInvalidInput(%v) = false, want true", err)
			}
			if refused.Len() != 0 {
				t.Errorf("a refused write wrote %d bytes, want none", refused.Len())
			}

			allowed := openDeclaration(t, certifiedPDF(), false)
			defer allowed.Close()
			var out bytes.Buffer
			if err := tc.write(t, allowed, &out, WithSignatureRemoval()); err != nil {
				t.Fatalf("with WithSignatureRemoval: %v", err)
			}
			if out.Len() == 0 {
				t.Error("with WithSignatureRemoval: wrote nothing")
			}
		})
	}
}

// TestSignatureRemoval_StageOption pins that the option given to a pipeline
// stage allows that stage, and an incremental update needs none.
func TestSignatureRemoval_StageOption(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	src := openDeclaration(t, certifiedPDF(), false)
	defer src.Close()
	var split bytes.Buffer
	result, err := NewPipeline().Open(src).Split([]int{1}, WithSignatureRemoval()).WriteTo(ctx, &split)
	if err != nil {
		t.Fatalf("a split stage given WithSignatureRemoval: %v", err)
	}
	if len(result.DroppedSignatures) == 0 {
		t.Error("the split reported no dropped signature")
	}

	incremental := openDeclaration(t, certifiedPDF(), false)
	defer incremental.Close()
	w := NewWriter(incremental)
	defer w.Close()
	if err := w.RotatePages(90, 1); err != nil {
		t.Fatalf("RotatePages: %v", err)
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("an incremental update: %v", err)
	}
	if got := w.DroppedSignatures(); got != nil {
		t.Errorf("an incremental update dropped %v, want none", got)
	}
}
