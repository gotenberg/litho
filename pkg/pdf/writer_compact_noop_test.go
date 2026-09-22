package pdf

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

// Compaction is a full-rewrite feature. Packing and dedup run inside the
// rewrite engine, and writer.IncrementalConfig carries no field for either, so
// an incremental write drops both. The output is then byte-identical to one
// written without the options, which a caller cannot tell apart from
// compaction that ran and found nothing to save. WriteTo reports it at Debug.
//
// The CLI depends on this being visible: pipe and remediate both accept
// --compact, rewrite the whole file for most inputs, and append an incremental
// update for the rest.
const compactIgnoredNotice = "compaction ignored"

// debugLogger returns a logger that records every Debug line into buf.
func debugLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// rotatedIncrementalWriter opens data and queues a rotation, which is a
// mutation the incremental engine can append.
func rotatedIncrementalWriter(t *testing.T, data []byte) *Writer {
	t.Helper()
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { r.Close() })

	w := NewWriter(r)
	if err := w.RotatePages(90); err != nil {
		t.Fatalf("RotatePages: %v", err)
	}
	return w
}

// TestWriteTo_ReportsCompactionIgnoredOnAnIncrementalWrite pins the notice.
//
// The fixture choice carries the test. threePagePDF cannot be used here: its
// hand-written xref makes the source a rebuild candidate, and WriteTo forces a
// full rewrite for those, so the write would compact and the notice would
// correctly never fire. validXrefPDF computes its offsets, which is what keeps
// this write on the incremental path.
func TestWriteTo_ReportsCompactionIgnoredOnAnIncrementalWrite(t *testing.T) {
	t.Parallel()
	data := validXrefPDF(t)
	w := rotatedIncrementalWriter(t, data)

	var logs, out bytes.Buffer
	if err := w.WriteTo(context.Background(), &out,
		WithObjectStreams(), WithDedup(), WithWriteLogger(debugLogger(&logs))); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	// An incremental update appends to the original bytes and leaves them
	// intact. Without this the assertions below would pass or fail for the
	// wrong reason on a fixture that had quietly become a rebuild candidate.
	if !bytes.HasPrefix(out.Bytes(), data) {
		t.Fatal("this write was a full rewrite, so compaction applied and no notice is due: " +
			"the test no longer covers the incremental path it names")
	}
	if bytes.Contains(out.Bytes(), []byte(objStmMarker)) {
		t.Error("an appended section carries a packed object stream, which the incremental engine cannot produce")
	}
	if !strings.Contains(logs.String(), compactIgnoredNotice) {
		t.Errorf("compaction was dropped without a word; logs:\n%s", logs.String())
	}
}

// TestWriteTo_SilentAboutCompactionOnAFullRewrite is the control. Reporting a
// no-op where the options do take effect would teach a caller to ignore the
// notice.
func TestWriteTo_SilentAboutCompactionOnAFullRewrite(t *testing.T) {
	t.Parallel()
	data := validXrefPDF(t)
	w := rotatedIncrementalWriter(t, data)

	var logs, out bytes.Buffer
	if err := w.WriteTo(context.Background(), &out,
		WithFullRewrite(), WithObjectStreams(), WithDedup(),
		WithWriteLogger(debugLogger(&logs))); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	// Precondition: the options really did take effect, or an absent notice
	// would prove nothing at all.
	if !bytes.Contains(out.Bytes(), []byte(objStmMarker)) {
		t.Fatal("the full rewrite packed nothing, so this control has no compaction to stay silent about")
	}
	if strings.Contains(logs.String(), compactIgnoredNotice) {
		t.Errorf("called compaction ignored on a write that packed; logs:\n%s", logs.String())
	}
}

// TestWriteTo_SilentWhenCompactionWasNeverRequested: a caller who passed
// neither option must not be told they were dropped.
func TestWriteTo_SilentWhenCompactionWasNeverRequested(t *testing.T) {
	t.Parallel()
	data := validXrefPDF(t)
	w := rotatedIncrementalWriter(t, data)

	var logs, out bytes.Buffer
	if err := w.WriteTo(context.Background(), &out, WithWriteLogger(debugLogger(&logs))); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if strings.Contains(logs.String(), compactIgnoredNotice) {
		t.Errorf("raised compaction with a caller who never asked for it; logs:\n%s", logs.String())
	}
}
