package pdf

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gotenberg/litho/internal/pkitest"
	"github.com/gotenberg/litho/pkg/models"
)

// editTestPDF builds a one-page document with an exact cross-reference table,
// which a signature goes on in an incremental update.
func editTestPDF() []byte {
	const content = "BT /F1 12 Tf 20 700 Td (page) Tj ET"
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	})
}

// badStampPDF queues on w a stamp of page 9 of a one-page source, which fails
// with ErrInvalidPageNumber when the Writer applies it.
func badStampPDF(t *testing.T, w *Writer) error {
	t.Helper()
	src := openBytes(t, editTestPDF())
	t.Cleanup(func() { _ = src.Close() })
	return w.StampPDF(PDFStamp{Source: src, Page: 9})
}

// assertStampEditError fails t unless err holds the [EditError] of the bad
// stamp queued under label, and still matches ErrInvalidPageNumber and
// IsInvalidInput through it.
func assertStampEditError(t *testing.T, err error, label string) {
	t.Helper()
	editErr, ok := errors.AsType[*EditError](err)
	if !ok {
		t.Fatalf("error = %v, want an EditError", err)
	}
	if editErr.Label != label || editErr.Method != "StampPDF" {
		t.Errorf("EditError{Label: %q, Method: %q}, want {%q, %q}", editErr.Label, editErr.Method, label, "StampPDF")
	}
	if !errors.Is(err, ErrInvalidPageNumber) {
		t.Errorf("errors.Is(%v, ErrInvalidPageNumber) = false", err)
	}
	if !IsInvalidInput(err) {
		t.Errorf("IsInvalidInput(%v) = false", err)
	}
}

// TestEditErrorNamesTheStageThatQueuedTheEdit pins that an edit failing in a
// later stage of a pipeline reports the Apply stage that queued it, whichever
// stage applies it: the final write, a Split, a Redact with and without its
// pixel check, a colour conversion, a transparency flattening, a remediation or
// a signature.
func TestEditErrorNamesTheStageThatQueuedTheEdit(t *testing.T) {
	t.Parallel()

	pki := pkitest.New(t)
	signer, err := NewSigner(pki.Leaf, pki.LeafKey, pki.Intermediate)
	if err != nil {
		t.Fatal(err)
	}
	region := []RedactRegion{{Page: 1, Rect: [4]float64{0, 0, 100, 100}}}

	sign := func(p *Pipeline) (string, string) {
		p.Apply(func(_ context.Context, w *Writer) error {
			w.Sign(signer)
			return nil
		}, WithEditLabel("sign"))
		return stageNameWrite, ""
	}
	for _, tc := range []struct {
		name string
		// then adds the stages after the labeled stamp and rotation, and
		// returns the name and the label of the stage the failure surfaces in.
		then func(p *Pipeline) (stageName, stageLabel string)
		opts []WriteOption
	}{
		{"final write", func(*Pipeline) (string, string) { return stageNameWrite, "" }, nil},
		{"Split", func(p *Pipeline) (string, string) {
			p.Split([]int{1})
			return stageNameSplit, ""
		}, nil},
		{"Redact with the pixel check", func(p *Pipeline) (string, string) {
			p.Apply(func(ctx context.Context, w *Writer) error {
				_, err := w.Redact(ctx, region)
				return err
			}, WithEditLabel("redact"))
			return stageNameApply, "redact"
		}, nil},
		{"Redact without the pixel check", func(p *Pipeline) (string, string) {
			p.Apply(func(ctx context.Context, w *Writer) error {
				_, err := w.Redact(ctx, region, WithoutRedactionPixelCheck())
				return err
			}, WithEditLabel("redact"))
			return stageNameApply, "redact"
		}, nil},
		{"ConvertColors", func(p *Pipeline) (string, string) {
			p.Apply(func(ctx context.Context, w *Writer) error {
				_, err := w.ConvertColors(ctx, GrayColorTarget())
				return err
			}, WithEditLabel("gray"))
			return stageNameApply, "gray"
		}, nil},
		{"FlattenTransparency", func(p *Pipeline) (string, string) {
			p.Apply(func(ctx context.Context, w *Writer) error {
				_, err := w.FlattenTransparency(ctx)
				return err
			}, WithEditLabel("flatten"))
			return stageNameApply, "flatten"
		}, nil},
		{"RemediatePDFA", func(p *Pipeline) (string, string) {
			p.Apply(func(ctx context.Context, w *Writer) error {
				_, err := w.RemediatePDFA(ctx, models.PDFA2b)
				return err
			}, WithEditLabel("pdfa"))
			return stageNameApply, "pdfa"
		}, nil},
		{"Sign", sign, nil},
		{"Sign over a full rewrite", sign, []WriteOption{WithFullRewrite()}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			p := NewPipeline().Open(openBytes(t, editTestPDF()))
			p.Apply(func(_ context.Context, w *Writer) error {
				return badStampPDF(t, w)
			}, WithEditLabel("stamp"))
			p.Apply(func(_ context.Context, w *Writer) error {
				return w.RotatePages(90)
			}, WithEditLabel("rotate"))
			wantName, wantLabel := tc.then(p)

			_, err := p.WriteTo(context.Background(), io.Discard, tc.opts...)
			assertStampEditError(t, err, "stamp")
			stageErr, ok := errors.AsType[*PipelineStageError](err)
			if !ok {
				t.Fatalf("error = %v, want a PipelineStageError", err)
			}
			if stageErr.Name != wantName || stageErr.Label != wantLabel {
				t.Errorf("PipelineStageError{Name: %q, Label: %q}, want {%q, %q}", stageErr.Name, stageErr.Label, wantName, wantLabel)
			}
		})
	}
}

// TestEditLabelOfTheStageOnly pins that a stage's label covers its own edits:
// an unlabeled stage after a labeled one queues edits with no label.
func TestEditLabelOfTheStageOnly(t *testing.T) {
	t.Parallel()

	p := NewPipeline().Open(openBytes(t, editTestPDF()))
	p.Apply(func(_ context.Context, w *Writer) error {
		w.SetMetadata(Metadata{Title: "Labeled"})
		return nil
	}, WithEditLabel("metadata"))
	p.Apply(func(_ context.Context, w *Writer) error {
		return badStampPDF(t, w)
	})

	_, err := p.WriteTo(context.Background(), io.Discard)
	assertStampEditError(t, err, "")
}

// TestSetEditLabelWithoutAPipeline pins the label on a Writer used directly.
func TestSetEditLabelWithoutAPipeline(t *testing.T) {
	t.Parallel()

	w := NewWriter(openBytes(t, editTestPDF()))
	defer w.Close()
	w.SetEditLabel("watermark")
	if err := badStampPDF(t, w); err != nil {
		t.Fatalf("StampPDF: %v", err)
	}
	w.SetEditLabel("")
	if err := w.RotatePages(90); err != nil {
		t.Fatalf("RotatePages: %v", err)
	}

	assertStampEditError(t, w.WriteTo(context.Background(), io.Discard), "watermark")
}

// TestSetMetadataLabelsBothEdits pins that the two edits SetMetadata queues,
// the /Info entries and the XMP packet that has to agree with them, both carry
// the label and the method.
func TestSetMetadataLabelsBothEdits(t *testing.T) {
	t.Parallel()

	w := NewWriter(openBytes(t, editTestPDF()))
	defer w.Close()
	w.SetEditLabel("meta")
	w.SetMetadata(Metadata{Title: "Labeled"})

	if len(w.mutations) != 2 {
		t.Fatalf("SetMetadata queued %d edits, want 2", len(w.mutations))
	}
	for i, m := range w.mutations {
		if m.label != "meta" || m.method != "SetMetadata" {
			t.Errorf("edit %d: label %q, method %q, want %q, %q", i, m.label, m.method, "meta", "SetMetadata")
		}
	}
}

// TestSignFailureNamesSign pins that a signature whose timestamp authority
// fails is reported as Sign's, not as the document timestamp's that follows
// it, under the label in effect when Sign was called.
func TestSignFailureNamesSign(t *testing.T) {
	t.Parallel()

	tsa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer tsa.Close()

	pki := pkitest.New(t)
	signer, err := NewSigner(pki.Leaf, pki.LeafKey, pki.Intermediate)
	if err != nil {
		t.Fatal(err)
	}

	w := NewWriter(openBytes(t, editTestPDF()))
	defer w.Close()
	w.SetEditLabel("sign")
	w.Sign(signer, WithTimestampAuthority(tsa.URL))
	w.SetEditLabel("timestamp")
	w.Timestamp(tsa.URL)

	err = w.WriteTo(context.Background(), io.Discard)
	editErr, ok := errors.AsType[*EditError](err)
	if !ok {
		t.Fatalf("error = %v, want an EditError", err)
	}
	if editErr.Method != "Sign" || editErr.Label != "sign" {
		t.Errorf("EditError{Label: %q, Method: %q}, want {%q, %q}", editErr.Label, editErr.Method, "sign", "Sign")
	}
}

// TestEditErrorMessage pins the message with and without a label.
func TestEditErrorMessage(t *testing.T) {
	t.Parallel()

	cause := errors.New("boom")
	for _, tc := range []struct {
		err  *EditError
		want string
	}{
		{&EditError{Label: "watermark", Method: "StampText", Err: cause}, `edit "watermark", Writer.StampText: boom`},
		{&EditError{Method: "StampText", Err: cause}, "Writer.StampText: boom"},
	} {
		if got := tc.err.Error(); got != tc.want {
			t.Errorf("Error() = %q, want %q", got, tc.want)
		}
	}
}
