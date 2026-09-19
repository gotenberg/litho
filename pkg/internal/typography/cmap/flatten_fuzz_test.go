package cmap

import (
	"bytes"
	"context"
	"testing"
)

// FuzzFlattenUseCMap drives the rewrite with attacker-controlled CMap
// programs. It must never panic, and a rewrite it claims must re-read as the
// mapping it merged.
func FuzzFlattenUseCMap(f *testing.F) {
	full := cmapProgram("BaseCMap", rangeSection(0x0000, 0x01FF, 0))
	part := cmapProgram("UsingCMap", rangeSection(0x0000, 0x000F, 900))

	f.Add(part, full)
	f.Add(part, part)
	f.Add(part[:len(part)/2], full)     // truncated using program
	f.Add(part, full[:len(full)/2])     // truncated base
	f.Add([]byte("begincmap\n"), full)  // no codespace, no ranges
	f.Add(part, []byte("/X usecmap\n")) // a base that only defers again
	f.Add([]byte{}, []byte{})

	f.Fuzz(func(t *testing.T, using, base []byte) {
		if len(using) > 1<<20 || len(base) > 1<<20 {
			return
		}
		ctx := context.Background()
		out, changed, err := FlattenUseCMap(ctx, using, [][]byte{base})
		if err != nil {
			return
		}
		if !changed {
			if !bytes.Equal(out, using) {
				t.Fatal("declined but returned something other than the input")
			}
			return
		}
		if bytes.Contains(out, []byte("usecmap")) {
			t.Fatal("claimed a rewrite that still defers to another CMap")
		}
		if _, err := ParseCID(ctx, out); err != nil {
			t.Fatalf("claimed a rewrite that does not re-parse: %v", err)
		}
	})
}

// TestFlattenFuzzSeedsReachTheRewrite pins that at least one seed actually
// produces a rewrite. A target whose every seed declines would pass forever
// while exercising nothing.
func TestFlattenFuzzSeedsReachTheRewrite(t *testing.T) {
	t.Parallel()

	_, changed, err := FlattenUseCMap(context.Background(),
		cmapProgram("UsingCMap", rangeSection(0x0000, 0x000F, 900)),
		[][]byte{cmapProgram("BaseCMap", rangeSection(0x0000, 0x01FF, 0))})
	if err != nil || !changed {
		t.Fatalf("the seed corpus never reaches a rewrite: changed=%v err=%v", changed, err)
	}
}
