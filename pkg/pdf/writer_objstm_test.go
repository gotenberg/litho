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

// TestMerge_WithDedup_MergesDuplicateStreams exercises dedup end to end.
//
// A self-merge is the fixture that makes this discriminating: it puts two
// copies of every content stream in one document, so dedup has something real
// to merge. threePagePDF on its own carries three DISTINCT content streams and
// would let the test pass while merging nothing.
func TestMerge_WithDedup_MergesDuplicateStreams(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := threePagePDF()

	selfMerge := func(opts ...WriteOption) []byte {
		t.Helper()
		a, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		defer a.Close()
		b, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		defer b.Close()

		var out bytes.Buffer
		if err := Merge(ctx, []*Reader{a, b}, &out, opts...); err != nil {
			t.Fatalf("Merge: %v", err)
		}
		return out.Bytes()
	}

	plain := selfMerge()
	deduped := selfMerge(WithDedup())

	if len(deduped) >= len(plain) {
		t.Errorf("dedup did not shrink a self-merge: %d bytes vs %d", len(deduped), len(plain))
	}

	r, err := Open(ctx, bytes.NewReader(deduped), int64(len(deduped)))
	if err != nil {
		t.Fatalf("reopening the deduplicated merge: %v", err)
	}
	defer r.Close()
	got, err := r.PageCount(ctx)
	if err != nil {
		t.Fatalf("PageCount: %v", err)
	}
	if got != 6 {
		t.Errorf("deduplicated self-merge has %d pages, want 6", got)
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

// TestWriter_WithObjectStreams_AppliesToEncryptedOutput pins that packing runs
// before encryption, so an encrypted output is packed too: its object streams
// are encrypted as whole streams, and the file reopens under its password
// with every page and the same text as the plain packed output.
func TestWriter_WithObjectStreams_AppliesToEncryptedOutput(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := threePagePDF()

	write := func(opts ...WriteOption) []byte {
		t.Helper()
		r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		defer r.Close()
		var out bytes.Buffer
		if err := NewWriter(r).WriteTo(ctx, &out, append([]WriteOption{WithFullRewrite(), WithObjectStreams()}, opts...)...); err != nil {
			t.Fatalf("WriteTo: %v", err)
		}
		return out.Bytes()
	}
	packed := write()
	encrypted := write(WithEncryption(EncryptConfig{UserPassword: "u", OwnerPassword: "o"}))

	if !bytes.Contains(encrypted, []byte(objStmMarker)) {
		t.Fatal("the encrypted output carries no object stream")
	}
	if _, err := Open(ctx, bytes.NewReader(encrypted), int64(len(encrypted))); err == nil {
		t.Fatal("the output opens without its password, so it is not encrypted")
	}

	want, err := openRecompressSource(t, packed).ExtractAllText(ctx)
	if err != nil {
		t.Fatalf("ExtractAllText on the plain packed output: %v", err)
	}
	r := openRecompressSource(t, encrypted, WithPassword("u"))
	if !r.IsEncrypted() {
		t.Error("the output does not report itself encrypted")
	}
	if got, err := r.PageCount(ctx); err != nil || got != 3 {
		t.Errorf("the encrypted packed output has %d pages (%v), want 3", got, err)
	}
	if got, err := r.ExtractAllText(ctx); err != nil || got != want {
		t.Errorf("text %q (%v), want %q", got, err, want)
	}
}

// TestMerge_WithDedup_AppliesToEncryptedOutput covers dedup on the Merge
// path with encryption: the duplicate streams merge before the survivor is
// encrypted under its own number, so the output is smaller and reopens.
func TestMerge_WithDedup_AppliesToEncryptedOutput(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := threePagePDF()
	encrypt := WithEncryption(EncryptConfig{UserPassword: "u", OwnerPassword: "o"})

	selfMerge := func(opts ...WriteOption) []byte {
		t.Helper()
		a, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		defer a.Close()
		b, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		defer b.Close()
		var out bytes.Buffer
		if err := Merge(ctx, []*Reader{a, b}, &out, opts...); err != nil {
			t.Fatalf("Merge: %v", err)
		}
		return out.Bytes()
	}
	plain, deduped := selfMerge(encrypt), selfMerge(encrypt, WithDedup())
	if len(deduped) >= len(plain) {
		t.Errorf("dedup did not shrink an encrypted self-merge: %d bytes vs %d", len(deduped), len(plain))
	}
	r := openRecompressSource(t, deduped, WithPassword("u"))
	if got, err := r.PageCount(ctx); err != nil || got != 6 {
		t.Errorf("deduplicated encrypted self-merge has %d pages (%v), want 6", got, err)
	}
}

// TestSplit_WithObjectStreams_AppliesToEncryptedOutput covers packing on the
// Split path with encryption, the third write path sharing the order.
func TestSplit_WithObjectStreams_AppliesToEncryptedOutput(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := threePagePDF()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	var out bytes.Buffer
	if err := Split(ctx, r, []int{1}, &out, WithObjectStreams(), WithEncryption(EncryptConfig{UserPassword: "u", OwnerPassword: "o"})); err != nil {
		t.Fatalf("Split: %v", err)
	}
	if !bytes.Contains(out.Bytes(), []byte(objStmMarker)) {
		t.Fatal("the encrypted split output carries no object stream")
	}
	r2 := openRecompressSource(t, out.Bytes(), WithPassword("u"))
	if got, err := r2.PageCount(ctx); err != nil || got != 1 {
		t.Errorf("encrypted split output has %d pages (%v), want 1", got, err)
	}
}
