package pdf

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestWriterRefusesToPackAPDFA1Remediation pins that the output of a PDF/A-1
// remediation is never written with a cross-reference stream. ISO 19005-1,
// 6.1.4 forbids one, and packing objects into object streams requires one,
// so a packed output fails the profile the remediation was asked to meet while
// its report said otherwise. The refusal names the clause, and nothing is
// written.
func TestWriterRefusesToPackAPDFA1Remediation(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		// atNew are the options given to NewWriter, refused by the
		// remediation itself.
		atNew     []WriteOption
		profile   models.PDFAProfile
		writeOpts []WriteOption
		wantOpt   string
	}{
		{
			name:      "WithObjectStreams after a PDF/A-1b remediation",
			profile:   models.PDFA1b,
			writeOpts: []WriteOption{WithObjectStreams(), WithDedup()},
			wantOpt:   "WithObjectStreams",
		},
		{
			name:      "WithXRefStream after a PDF/A-1a remediation",
			profile:   models.PDFA1a,
			writeOpts: []WriteOption{WithXRefStream()},
			wantOpt:   "WithXRefStream",
		},
		{
			name:    "WithObjectStreams given to NewWriter before a PDF/A-1b remediation",
			profile: models.PDFA1b,
			atNew:   []WriteOption{WithObjectStreams()},
			wantOpt: "WithObjectStreams",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			data := signablePDF()
			r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer r.Close()
			w := NewWriter(r, tc.atNew...)
			defer w.Close()
			_, err = w.RemediatePDFA(ctx, tc.profile)
			check := func(what string, err error) {
				t.Helper()
				if !errors.Is(err, ErrPDFA1ForbidsObjectStreams) {
					t.Fatalf("%s returned %v, want ErrPDFA1ForbidsObjectStreams", what, err)
				}
				for _, want := range []string{"ISO 19005-1, 6.1.4", tc.wantOpt} {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("the refusal %q does not name %s", err, want)
					}
				}
			}
			if len(tc.atNew) > 0 {
				check("the remediation", err)
				return
			}
			if err != nil {
				t.Fatalf("RemediatePDFA: %v", err)
			}
			var out bytes.Buffer
			check("WriteTo", w.WriteTo(ctx, &out, tc.writeOpts...))
			if out.Len() != 0 {
				t.Errorf("WriteTo wrote %d bytes of a packed PDF/A-1 output", out.Len())
			}
			// A refused write leaves the Writer usable: the caller retries
			// without the option.
			if err := w.WriteTo(ctx, &out); err != nil {
				t.Fatalf("WriteTo without the option: %v", err)
			}
			if bytes.Contains(out.Bytes(), []byte(objStmMarker)) {
				t.Error("the retried output packs objects")
			}
		})
	}
}

// TestWriterDedupsAPDFA1Remediation is the control: dedup writes no
// cross-reference stream, so a PDF/A-1 output takes it, and a PDF/A-2b output
// still takes packing.
func TestWriterDedupsAPDFA1Remediation(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		profile models.PDFAProfile
		opts    []WriteOption
	}{
		{name: "WithDedup after PDF/A-1b", profile: models.PDFA1b, opts: []WriteOption{WithDedup(), WithImageRecompression()}},
		{name: "WithObjectStreams after PDF/A-2b", profile: models.PDFA2b, opts: []WriteOption{WithObjectStreams(), WithXRefStream()}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			data := signablePDF()
			r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer r.Close()
			w := NewWriter(r)
			defer w.Close()
			if _, err := w.RemediatePDFA(ctx, tc.profile); err != nil {
				t.Fatalf("RemediatePDFA: %v", err)
			}
			var out bytes.Buffer
			if err := w.WriteTo(ctx, &out, tc.opts...); err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
		})
	}
}

// TestPipelineRefusesToPackAPDFA1Remediation pins that the refusal reaches
// the final write of a [Pipeline] whose last stage remediates to PDF/A-1b.
func TestPipelineRefusesToPackAPDFA1Remediation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := signablePDF()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	p := NewPipeline().Open(r).Apply(func(ctx context.Context, w *Writer) error {
		_, err := w.RemediatePDFA(ctx, models.PDFA1b)
		return err
	})
	var out bytes.Buffer
	if _, err := p.WriteTo(ctx, &out, WithObjectStreams()); !errors.Is(err, ErrPDFA1ForbidsObjectStreams) {
		t.Fatalf("Pipeline.WriteTo returned %v, want ErrPDFA1ForbidsObjectStreams", err)
	}
	if out.Len() != 0 {
		t.Errorf("Pipeline.WriteTo wrote %d bytes", out.Len())
	}
}
