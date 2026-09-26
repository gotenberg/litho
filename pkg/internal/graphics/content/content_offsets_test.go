package content

import (
	"context"
	"strings"
	"testing"
)

func TestParseStreamDataWithOffsets(t *testing.T) {
	ctx := context.Background()
	data := []byte("BT /F1 12 Tf 72 700 Td (Hi) Tj ET")

	ops, ranges, err := ParseStreamDataWithOffsets(ctx, data)
	if err != nil {
		t.Fatalf("ParseStreamDataWithOffsets: %v", err)
	}
	if len(ops) != len(ranges) {
		t.Fatalf("ops/ranges length mismatch: %d vs %d", len(ops), len(ranges))
	}
	wantOps := []string{"BT", "Tf", "Td", "Tj", "ET"}
	if len(ops) != len(wantOps) {
		t.Fatalf("got %d ops, want %d: %+v", len(ops), len(wantOps), ops)
	}

	// Ranges must be contiguous, monotonic, and cover the operator keyword.
	if ranges[0].Start != 0 {
		t.Errorf("first range starts at %d, want 0", ranges[0].Start)
	}
	for i, op := range ops {
		if op.Operator != wantOps[i] {
			t.Errorf("op %d: got %q, want %q", i, op.Operator, wantOps[i])
		}
		r := ranges[i]
		if r.Start < 0 || r.End > len(data) || r.Start > r.End {
			t.Fatalf("range %d out of bounds: %+v (len %d)", i, r, len(data))
		}
		if i > 0 && r.Start != ranges[i-1].End {
			t.Errorf("range %d not contiguous: starts %d, prev ended %d", i, r.Start, ranges[i-1].End)
		}
		if seg := string(data[r.Start:r.End]); !strings.Contains(seg, op.Operator) {
			t.Errorf("range %d (%q) does not contain operator %q", i, seg, op.Operator)
		}
	}
	if last := ranges[len(ranges)-1]; last.End != len(data) {
		t.Errorf("last range ends at %d, want %d", last.End, len(data))
	}
}

// TestParseStreamDataWithOffsets_ErrorIsConsistent pins what a stream that
// ends inside an inline image dictionary, before the ID that ISO 32000-1,
// 8.9.7 ends it with, gives. The readings that read the image, veraPDF's, a
// repair's and the whole stream's, fail on it, and [ParseStreamDataWithOffsets]
// returns the error [ParseStreamData] returns, with both slices nil so they
// stay parallel. An image whose data runs to the end of the stream fails no
// reading: each ends the data there. The reading of PDF.js stops at the
// dictionary instead ([readInlineImage]) and keeps what it read before.
func TestParseStreamDataWithOffsets_ErrorIsConsistent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := []byte("q BI /W 1 /H 1 /BPC 8 /CS /G")
	for name, opts := range map[string][]Option{
		"veraPDF":      nil,
		"repair":       {ForRepair()},
		"whole stream": {WholeStream()},
	} {
		ops, ranges, err := ParseStreamDataWithOffsets(ctx, data, opts...)
		_, want := ParseStreamData(ctx, data, opts...)
		if err == nil || want == nil || err.Error() != want.Error() {
			t.Errorf("%s: ParseStreamDataWithOffsets error %v, want the error of ParseStreamData, %v", name, err, want)
		}
		if ops != nil || ranges != nil {
			t.Errorf("%s: on error want nil ops and ranges; got %d ops, %d ranges", name, len(ops), len(ranges))
		}
	}

	ops, ranges, err := ParseStreamDataWithOffsets(ctx, data, SplitGluedOperators())
	if err != nil || len(ops) != 1 || ops[0].Operator != "q" || len(ranges) != 1 {
		t.Errorf("read as PDF.js: %d ops, %d ranges, err %v, want q alone", len(ops), len(ranges), err)
	}

	unended := []byte("q BI /W 1 /H 1 /BPC 8 /CS /G ID \xff\xff\xff")
	for _, opts := range [][]Option{nil, {ForRepair()}, {WholeStream()}, {SplitGluedOperators()}} {
		if ops, ranges, err := ParseStreamDataWithOffsets(ctx, unended, opts...); err != nil || len(ops) != 2 || len(ranges) != 2 {
			t.Errorf("image data to the end of the stream: %d ops, %d ranges, err %v, want q and BI", len(ops), len(ranges), err)
		}
	}
}

func TestParseStreamDataWithOffsets_Empty(t *testing.T) {
	ops, ranges, err := ParseStreamDataWithOffsets(context.Background(), nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(ops) != 0 || len(ranges) != 0 {
		t.Errorf("empty input: got %d ops, %d ranges", len(ops), len(ranges))
	}
}
