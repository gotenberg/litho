package pdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// laterRoundPDF builds a one-page document whose PDF/A-1b remediation keeps a
// second round. Its page paints in DeviceCMYK and carries a square annotation
// with a /C colour array, and the document has no OutputIntent. The first
// round installs a CMYK intent for the content, and annot-color waits for the
// output to carry it (see fixAnnotColor): only the second round, run on the
// first round's output, drops the /C array ISO 19005-1, 6.5.3 forbids next to
// an intent that is not RGB.
//
// With binaryComment the file is otherwise well formed, so the first round
// writes an incremental update. Without it, the header lacks the comment ISO
// 19005-1, 6.1.2 requires, and the first round rewrites the file in full,
// renumbering every object before the second round reads it.
func laterRoundPDF(binaryComment bool) []byte {
	const xmp = `<?xpacket begin="` + "\xEF\xBB\xBF" + `" id="W5M0MpCehiHzreSzNTczkc9d"?>` +
		`<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` +
		`<rdf:Description rdf:about="" xmlns:dc="http://purl.org/dc/elements/1.1/"/>` +
		`</rdf:RDF></x:xmpmeta><?xpacket end="w"?>`
	const content = "0 0 1 0 k 20 20 100 100 re f BT /F1 12 Tf 20 700 Td (page) Tj ET\n"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /Metadata 6 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R" +
			" /Resources << /Font << /F1 5 0 R >> >>" +
			" /Annots [<< /Type /Annot /Subtype /Square /Rect [300 300 400 400] /F 4 /C [1 0 0] >>] >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fmt.Sprintf("<< /Type /Metadata /Subtype /XML /Length %d >>\nstream\n%s\nendstream", len(xmp), xmp),
		"<< /Title (Fixture) /Author (litho) >>",
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	if binaryComment {
		buf.WriteString("%\xE2\xE3\xCF\xD3\n")
	}
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R /Info 7 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

// keptRounds counts the remediation rounds after the first that the Writer
// keeps, read off its debug log, so a test can prove it reached one.
type keptRounds struct{ n atomic.Int32 }

func (k *keptRounds) Enabled(context.Context, slog.Level) bool { return true }
func (k *keptRounds) WithAttrs([]slog.Attr) slog.Handler       { return k }
func (k *keptRounds) WithGroup(string) slog.Handler            { return k }
func (k *keptRounds) Handle(_ context.Context, r slog.Record) error {
	if r.Message == "remediate: extra round kept" {
		k.n.Add(1)
	}
	return nil
}

// TestWriterEditBeforeLaterRoundsOfTheTestEngine pins, with the engine of
// [roundsEngine], whose rounds do not depend on what the real fixers happen to
// leave for a second one, that a remediation keeping later rounds keeps an
// edit queued before it: each of three rounds sets a catalog key, and the
// output holds all three and the rotation.
func TestWriterEditBeforeLaterRoundsOfTheTestEngine(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		data func(*testing.T) []byte
	}{
		{name: "an incremental first round", data: validXrefPDF},
		// The hand-written offsets of threePagePDF make the reader rebuild its
		// table, so every round rewrites the file in full.
		{name: "a rewritten first round", data: func(*testing.T) []byte { return threePagePDF() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			data := tc.data(t)
			r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer r.Close()

			e := &roundsEngine{steps: []roundsStep{
				{keys: []string{"PageLayout"}}, {keys: []string{"PageMode"}}, {keys: []string{"Lang"}},
			}}
			w := NewWriter(r)
			defer w.Close()
			if err := w.RotatePages(90); err != nil {
				t.Fatalf("RotatePages: %v", err)
			}
			if _, err := w.remediate(ctx, engineTarget, DefaultRegressionTolerance, e.revalidate, e.run); err != nil {
				t.Fatalf("remediate: %v", err)
			}
			if len(e.disabled) != 3 {
				t.Fatalf("the engine ran %d rounds, want 3", len(e.disabled))
			}

			var out bytes.Buffer
			if err := w.WriteTo(ctx, &out); err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
			got, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
			if err != nil {
				t.Fatalf("reopening the output: %v", err)
			}
			defer got.Close()
			if res, err := e.revalidate(ctx, got); err != nil || len(res) != 0 {
				t.Errorf("the rotated output misses a round's repair: %v %v", res, err)
			}
			for i, rot := range rotationsOf(t, out.Bytes()) {
				if rot != 90 {
					t.Errorf("page %d has rotation %d, want 90", i+1, rot)
				}
			}
		})
	}
}

// errRefusedWrite is what [refusingWriter] returns.
var errRefusedWrite = errors.New("destination refuses writes")

// refusingWriter is a destination every write to fails, as a full disk.
type refusingWriter struct{}

func (refusingWriter) Write([]byte) (int, error) { return 0, errRefusedWrite }

// laterRoundWriter opens [laterRoundPDF] and remediates it for PDF/A-1b,
// failing the test unless the remediation keeps a second round.
func laterRoundWriter(t *testing.T) (*Writer, *RemediationReport) {
	t.Helper()

	ctx := context.Background()
	data := laterRoundPDF(true)
	rounds := &keptRounds{}
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)), WithLogger(slog.New(rounds)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := r.Close(); closeErr != nil {
			t.Logf("closing reader: %v", closeErr)
		}
	})
	w := NewWriter(r)
	report, err := w.RemediatePDFA(ctx, models.PDFA1b)
	if err != nil {
		t.Fatalf("RemediatePDFA: %v", err)
	}
	if rounds.n.Load() == 0 {
		t.Fatal("the remediation kept no second round, so the test proves nothing")
	}
	return w, report
}

// TestWriterLaterRoundOutputIsReleased pins the life of the output a Writer
// writes after a remediation that kept a later round. The Writer reads that
// file, so a failed write keeps it for the retry; a successful write, or
// Close, removes it; and a second remediation, which is refused, adds none.
func TestWriterLaterRoundOutputIsReleased(t *testing.T) {
	ctx := context.Background()

	t.Run("a failed write, then a retry", func(t *testing.T) {
		dir := isolateTempDir(t)
		w, report := laterRoundWriter(t)
		if err := w.WriteTo(ctx, refusingWriter{}); !errors.Is(err, errRefusedWrite) {
			t.Fatalf("WriteTo into a refusing destination returned %v, want its error", err)
		}
		if got := lithoTempFiles(t, dir); len(got) != 1 {
			t.Errorf("after the failed write the temp dir holds %q, want the output the Writer edits", got)
		}

		var out bytes.Buffer
		if err := w.WriteTo(ctx, &out); err != nil {
			t.Fatalf("WriteTo retry: %v", err)
		}
		if got := lithoTempFiles(t, dir); len(got) != 0 {
			t.Errorf("WriteTo left %q behind", got)
		}
		if err := w.Close(); err != nil {
			t.Errorf("Close after WriteTo returned %v, want nil", err)
		}
		got, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
		if err != nil {
			t.Fatalf("reopening the output: %v", err)
		}
		defer got.Close()
		residual, err := got.ValidatePDFA(ctx, models.PDFA1b)
		if err != nil {
			t.Fatalf("ValidatePDFA: %v", err)
		}
		if gotV, wantV := violationTexts(residual), violationTexts(report.Residual); !slices.Equal(gotV, wantV) {
			t.Errorf("the retried output validates to %q, want %q", gotV, wantV)
		}
	})

	t.Run("a refused second remediation, then Close", func(t *testing.T) {
		dir := isolateTempDir(t)
		w, _ := laterRoundWriter(t)
		if _, err := w.RemediatePDFA(ctx, models.PDFA1b); !errors.Is(err, ErrEditAfterRemediation) {
			t.Fatalf("second RemediatePDFA returned %v, want ErrEditAfterRemediation", err)
		}
		if got := lithoTempFiles(t, dir); len(got) != 1 {
			t.Errorf("after the refusal the temp dir holds %q, want the output the Writer writes", got)
		}
		if err := w.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		if got := lithoTempFiles(t, dir); len(got) != 0 {
			t.Errorf("Close left %q behind", got)
		}
		if err := w.WriteTo(ctx, &bytes.Buffer{}); !errors.Is(err, ErrWriterClosed) {
			t.Errorf("WriteTo after Close returned %v, want ErrWriterClosed", err)
		}
	})
}

// violationTexts returns the text of each violation, sorted, so two
// validations compare as multisets.
func violationTexts(errs []error) []string {
	out := make([]string, len(errs))
	for i, e := range errs {
		out[i] = e.Error()
	}
	slices.Sort(out)
	return out
}

// TestWriterEditBeforeALaterRoundKeepsItsFixes pins that the output of edits
// queued before a remediation that keeps a later round holds the edits and the
// repairs of every round, and validates to the reported residual.
//
// The rounds after the first run on a reopened copy of the previous round's
// output and hand back only its file, so the Writer writes that file from
// then on (see [Writer.rebase]). An edit queued after such a remediation made
// the file stale, and the output lost what the later rounds repaired; no edit
// follows a remediation now, so the edits here come first.
func TestWriterEditBeforeALaterRoundKeepsItsFixes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name          string
		binaryComment bool
		before        func(*Writer) error
		wantRotate    int
		wantSubject   string
	}{
		{
			name:          "an incremental first round, rotated before",
			binaryComment: true,
			before:        func(w *Writer) error { return w.RotatePages(90) },
			wantRotate:    90,
		},
		{
			name:       "a rewritten first round, rotated before",
			before:     func(w *Writer) error { return w.RotatePages(90) },
			wantRotate: 90,
		},
		{
			name:          "rotated and metadata set before",
			binaryComment: true,
			before: func(w *Writer) error {
				w.SetMetadata(Metadata{Subject: "earlier edit"})
				return w.RotatePages(270)
			},
			wantRotate:  270,
			wantSubject: "earlier edit",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			data := laterRoundPDF(tc.binaryComment)
			rounds := &keptRounds{}
			r, err := Open(ctx, bytes.NewReader(data), int64(len(data)), WithLogger(slog.New(rounds)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer r.Close()

			w := NewWriter(r)
			defer w.Close()
			if err := tc.before(w); err != nil {
				t.Fatalf("edit before remediation: %v", err)
			}
			report, err := w.RemediatePDFA(ctx, models.PDFA1b)
			if err != nil {
				t.Fatalf("RemediatePDFA: %v", err)
			}
			if rounds.n.Load() == 0 {
				t.Fatal("the remediation kept no second round, so the test proves nothing")
			}

			var out bytes.Buffer
			if err := w.WriteTo(ctx, &out); err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
			got, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
			if err != nil {
				t.Fatalf("reopening the output: %v", err)
			}
			defer got.Close()

			residual, err := got.ValidatePDFA(ctx, models.PDFA1b)
			if err != nil {
				t.Fatalf("ValidatePDFA: %v", err)
			}
			if gotV, wantV := violationTexts(residual), violationTexts(report.Residual); !slices.Equal(gotV, wantV) {
				t.Errorf("the edited output validates to %q, want the remediation's residual %q", gotV, wantV)
			}
			for i, rot := range rotationsOf(t, out.Bytes()) {
				if rot != tc.wantRotate {
					t.Errorf("page %d has rotation %d, want %d", i+1, rot, tc.wantRotate)
				}
			}
			if tc.wantSubject != "" {
				meta, err := got.Metadata(ctx)
				if err != nil {
					t.Fatalf("Metadata: %v", err)
				}
				if meta["Subject"] != tc.wantSubject {
					t.Errorf("the output /Subject is %q, want %q", meta["Subject"], tc.wantSubject)
				}
			}
		})
	}
}
