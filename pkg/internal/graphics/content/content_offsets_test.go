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

func TestParseStreamDataWithOffsets_ErrorIsConsistent(t *testing.T) {
	// A truncated inline image (no EI terminator) makes parsing fail; on error
	// both returned slices must be nil so they stay parallel.
	data := []byte("q BI /W 1 /H 1 /BPC 8 /CS /G ID \xff\xff\xff")
	ops, ranges, err := ParseStreamDataWithOffsets(context.Background(), data)
	if err == nil {
		t.Skip("inline image parsed leniently; no error to check")
	}
	if ops != nil || ranges != nil {
		t.Errorf("on error want nil ops and ranges; got %d ops, %d ranges", len(ops), len(ranges))
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
