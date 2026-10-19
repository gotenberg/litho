package pdf

import (
	"bytes"
	"context"
	"testing"
)

// TestWriterCompactsTheOutputOfAKeptRemediationRound pins that the compaction
// options reach the output of a remediation that kept more than one round,
// signed or not.
//
// A round after the first repairs a reopened copy of the previous round's
// output, so the Writer then edits that file, and an unsigned write appended
// its last repairs to it in an incremental update, as a signed one appended
// its signature. Neither carries compaction: the output came back uncompacted,
// the options dropped with only a Debug line. Measured on pdfcpu
// read.go.pdf at PDF/A-2b, `remediate --compact` wrote a rewrite with an
// update on top and no object stream.
func TestWriterCompactsTheOutputOfAKeptRemediationRound(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := threeTextPagesPDF()
	steps := []roundsStep{{keys: []string{"PageLayout"}}, {keys: []string{"PageMode"}}, {keys: []string{"Lang"}}}

	write := func(t *testing.T, signed bool, opts ...WriteOption) []byte {
		t.Helper()
		r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		defer r.Close()
		e := &roundsEngine{steps: steps}
		w := NewWriter(r)
		defer w.Close()
		if _, err := w.remediate(ctx, engineTarget, DefaultRegressionTolerance, e.revalidate, e.run); err != nil {
			t.Fatalf("remediate: %v", err)
		}
		if w.basePath == "" {
			t.Fatal("precondition: the remediation kept no later round, so the Writer edits its source")
		}
		if signed {
			w.Sign(newCMSTestSigner(t))
		}
		var out bytes.Buffer
		if err := w.WriteTo(ctx, &out, opts...); err != nil {
			t.Fatalf("WriteTo: %v", err)
		}
		got, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
		if err != nil {
			t.Fatalf("reopening the output: %v", err)
		}
		defer got.Close()
		if res, err := e.revalidate(ctx, got); err != nil || len(res) != 0 {
			t.Errorf("the output misses a repair: %v %v", res, err)
		}
		return out.Bytes()
	}

	for _, signed := range []bool{false, true} {
		name := "unsigned"
		if signed {
			name = "signed"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if plain := write(t, signed); bytes.Contains(plain, []byte("/ObjStm")) {
				t.Fatal("precondition: the uncompacted output packed objects")
			}
			packed := write(t, signed, WithObjectStreams(), WithDedup())
			if !bytes.Contains(packed, []byte("/ObjStm")) {
				t.Error("the compaction options did not reach the output")
			}
			if signed {
				if n := signatureCount(t, packed); n != 1 {
					t.Errorf("the output holds %d signatures, want 1", n)
				}
				return
			}
			if again := write(t, signed, WithObjectStreams(), WithDedup()); !bytes.Equal(again, packed) {
				t.Error("the compacted output differs between two runs")
			}
		})
	}
}

// TestWriterKeepsTheUpdateOfASignedIntermediate pins the exception: a file
// litho wrote for the Writer that still carries a signature is updated, not
// rewritten for compaction, since the rewrite would drop the signature (ISO
// 32000-1, 12.8.1). The unsigned file is the control that shows the rule
// applies at all.
func TestWriterKeepsTheUpdateOfASignedIntermediate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	for _, tc := range []struct {
		name       string
		signed     bool
		wantUpdate bool
	}{
		{name: "signed", signed: true, wantUpdate: true},
		{name: "unsigned", signed: false, wantUpdate: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			data := signedFormPDF(tc.signed)
			r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer r.Close()
			w := NewWriter(r)
			defer w.Close()
			w.intermediate = true
			if err := w.RotatePages(90); err != nil {
				t.Fatalf("RotatePages: %v", err)
			}
			var out bytes.Buffer
			if err := w.WriteTo(ctx, &out, WithObjectStreams(), WithDedup()); err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
			if got := bytes.HasPrefix(out.Bytes(), data); got != tc.wantUpdate {
				t.Errorf("the output keeps the file's bytes: %v, want %v", got, tc.wantUpdate)
			}
			if got := w.DroppedSignatures(); len(got) != 0 {
				t.Errorf("the write dropped %v", got)
			}
		})
	}
}
