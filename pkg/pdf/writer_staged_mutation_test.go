package pdf

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// oneRoundRemediatedWriter opens data, remediates it at PDF/A-2b in a Writer and
// requires the remediation to keep its first round alone, so its repairs stay
// queued in the Writer rather than written into a file it edits.
func oneRoundRemediatedWriter(t *testing.T, data []byte) (*Writer, func()) {
	t.Helper()

	ctx := context.Background()
	rounds := &keptRounds{}
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)), WithLogger(slog.New(rounds)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	w := NewWriter(r)
	cleanup := func() {
		if err := w.Close(); err != nil {
			t.Errorf("Writer.Close: %v", err)
		}
		if err := r.Close(); err != nil {
			t.Errorf("Reader.Close: %v", err)
		}
	}
	report, err := w.RemediatePDFA(ctx, models.PDFA2b)
	if err != nil {
		cleanup()
		t.Fatalf("RemediatePDFA: %v", err)
	}
	if rounds.n.Load() != 0 {
		cleanup()
		t.Fatalf("the remediation kept %d later rounds, so the Writer edits its output and the test proves nothing", rounds.n.Load())
	}
	if len(report.Fixed) == 0 {
		cleanup()
		t.Fatal("the remediation fixed nothing, so the test proves nothing")
	}
	return w, cleanup
}

// writeAndReopen writes w and opens the output.
func writeAndReopen(t *testing.T, w *Writer) *Reader {
	t.Helper()

	ctx := context.Background()
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	r, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("reopening the output: %v", err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Errorf("Reader.Close: %v", err)
		}
	})
	return r
}

// TestWriterMetadataEditAfterOneRemediationRoundReachesTheXMP pins that a
// metadata edit queued after a remediation that keeps one round moves the XMP
// packet the remediation wrote, as it does when it edits the remediated file.
//
// A PDF/A remediation of a document with no XMP writes a packet at a new object
// number, queued in the Writer, and the edit synchronised XMP from the packet
// the source document holds at that number: none, so the output carried the
// new title in /Info only and the remediation's packet kept the old one.
func TestWriterMetadataEditAfterOneRemediationRoundReachesTheXMP(t *testing.T) {
	t.Parallel()

	w, cleanup := oneRoundRemediatedWriter(t, signablePDF())
	defer cleanup()
	w.SetMetadata(Metadata{Title: "Edited title", Author: "Edited author"})

	r := writeAndReopen(t, w)
	packet := readXMPPacket(t, r)
	if packet == "" {
		t.Fatal("the output carries no XMP packet")
	}
	for _, want := range []string{"Edited title", "Edited author"} {
		if !strings.Contains(packet, want) {
			t.Errorf("the XMP packet lacks %q, which the /Info dictionary now states", want)
		}
	}
}

// TestWriterEditQueuedBeforeAStageIsAppliedOnce pins that an edit queued before
// a remediation that keeps one round, or before a redaction, reaches the output
// once.
//
// Both stages replay the edits queued before them, so that they see the
// document as staged, and keep what the edits staged in their overrides. The
// edits also stayed queued and were applied again on top of those overrides
// when the Writer was written: a stamp, which appends to the page each time it
// is applied, was drawn twice.
func TestWriterEditQueuedBeforeAStageIsAppliedOnce(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		stage func(ctx context.Context, t *testing.T, w *Writer)
	}{
		{
			name: "a remediation",
			stage: func(ctx context.Context, t *testing.T, w *Writer) {
				if _, err := w.RemediatePDFA(ctx, models.PDFA2b); err != nil {
					t.Fatalf("RemediatePDFA: %v", err)
				}
			},
		},
		{
			name: "a redaction",
			stage: func(ctx context.Context, t *testing.T, w *Writer) {
				report, err := w.Redact(ctx, []RedactRegion{{Page: 1, Rect: [4]float64{10, 690, 100, 720}}})
				if err != nil {
					t.Fatalf("Redact: %v", err)
				}
				if len(report.RemovedText) == 0 {
					t.Fatal("the redaction removed nothing, so the test proves nothing")
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			data := signablePDF()
			rounds := &keptRounds{}
			r, err := Open(ctx, bytes.NewReader(data), int64(len(data)), WithLogger(slog.New(rounds)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer r.Close()
			w := NewWriter(r)
			defer w.Close()
			if err := w.StampText(TextStamp{Text: "STAMPED", X: 50, Y: 50}); err != nil {
				t.Fatalf("StampText: %v", err)
			}
			tc.stage(ctx, t, w)
			if rounds.n.Load() != 0 {
				t.Fatalf("the remediation kept %d later rounds, which write the edits into a file, so the test proves nothing", rounds.n.Load())
			}

			out := writeAndReopen(t, w)
			text, err := out.ExtractText(ctx, 1, WithArtifacts())
			if err != nil {
				t.Fatalf("ExtractText: %v", err)
			}
			if n := strings.Count(text, "STAMPED"); n != 1 {
				t.Errorf("the page shows the stamp %d times, want once: %q", n, text)
			}
		})
	}
}
