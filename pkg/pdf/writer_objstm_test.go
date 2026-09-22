package pdf

import (
	"bytes"
	"context"
	"testing"
)

// objStmMarker is how a packed object stream appears once serialized: the
// dictionary keys are emitted in sorted order, so /Type is last.
const objStmMarker = "/Type /ObjStm"

// TestWriter_WithObjectStreams_PacksAndReopens is the end-to-end contract.
// The unit tests drive the rewrite engine directly, so only this one proves
// that the public option produces a file litho can read back: a packed output
// no reader accepts would be worse than no packing at all.
func TestWriter_WithObjectStreams_PacksAndReopens(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := threePagePDF()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	var out bytes.Buffer
	if err := NewWriter(r).WriteTo(ctx, &out, WithFullRewrite(), WithObjectStreams()); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if !bytes.Contains(out.Bytes(), []byte(objStmMarker)) {
		t.Fatal("output carries no object stream, so the option did nothing")
	}
	// Packing implies a cross-reference stream: a traditional table cannot
	// name a compressed object.
	if !bytes.Contains(out.Bytes(), []byte("/Type /XRef")) {
		t.Error("packed output did not switch to a cross-reference stream")
	}

	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("reopening the packed output: %v", err)
	}
	defer r2.Close()

	got, err := r2.PageCount(ctx)
	if err != nil {
		t.Fatalf("PageCount on the packed output: %v", err)
	}
	if got != 3 {
		t.Errorf("packed output has %d pages, want 3", got)
	}
}

// TestWriter_WithObjectStreams_SurvivesEitherWritePath checks what actually
// holds whichever engine runs: the output reopens and keeps its pages, and
// packing appears only in a full rewrite.
//
// An earlier version asserted that a rotate goes through the incremental
// engine and therefore must not pack. It failed, and the premise was wrong
// twice over. WriteTo forces a full rewrite when the source was rebuilt, has a
// non-zero header offset, or is encrypted, and this fixture's hand-written
// xref makes it a rebuild candidate, so the rotate never reached the
// incremental path. More importantly the claim was unfalsifiable:
// IncrementalConfig carries no packing field, so the incremental engine has no
// code path to pack through. The test now distinguishes the paths by whether
// the source bytes survive as a prefix, which only an append produces, and
// says which one it exercised.
func TestWriter_WithObjectStreams_SurvivesEitherWritePath(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := threePagePDF()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	w := NewWriter(r)
	if err := w.RotatePages(90); err != nil {
		t.Fatalf("RotatePages: %v", err)
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out, WithObjectStreams()); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	// An incremental update copies the original bytes and appends to them;
	// a full rewrite re-derives every object, so the prefix is gone.
	incremental := bytes.HasPrefix(out.Bytes(), data)
	packed := bytes.Contains(out.Bytes(), []byte(objStmMarker))
	t.Logf("write path: incremental=%v packed=%v", incremental, packed)

	if incremental && packed {
		t.Error("an appended section cannot describe objects packed into a stream it does not contain")
	}

	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("reopening the output: %v", err)
	}
	defer r2.Close()
	got, err := r2.PageCount(ctx)
	if err != nil {
		t.Fatalf("PageCount: %v", err)
	}
	if got != 3 {
		t.Errorf("output has %d pages, want 3", got)
	}
}

// TestSplit_WithObjectStreams_Packs covers the second full-rewrite entry
// point. Split takes the option through a separate parameter chain from
// WriteTo, so passing it there silently doing nothing is exactly the failure
// this test exists to catch.
func TestSplit_WithObjectStreams_Packs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := threePagePDF()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	var out bytes.Buffer
	if err := Split(ctx, r, []int{1}, &out, WithObjectStreams()); err != nil {
		t.Fatalf("Split: %v", err)
	}
	if !bytes.Contains(out.Bytes(), []byte(objStmMarker)) {
		t.Fatal("Split ignored WithObjectStreams")
	}

	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("reopening the split output: %v", err)
	}
	defer r2.Close()
	got, err := r2.PageCount(ctx)
	if err != nil {
		t.Fatalf("PageCount: %v", err)
	}
	if got != 1 {
		t.Errorf("split output has %d pages, want 1", got)
	}
}
