package parser

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ioat"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
)

func lookaheadParser(src string) *Parser {
	return NewParser(lexer.NewLexer(ioat.NewReader(bytes.NewReader([]byte(src)), int64(len(src)), 0)))
}

// TestLookaheadSurvivesBufferRewind consumes far more tokens than the lookahead
// buffer ever holds, so the buffer drains and rewinds many times.
//
// [Parser.Consume] advances an index and resets to the front of the backing
// array once the buffer drains, rather than resliding the header forward. The
// rewind is where an off-by-one would hide: it must not repeat, skip or reorder
// a token, and Peek must keep reading past the boundary.
func TestLookaheadSurvivesBufferRewind(t *testing.T) {
	t.Parallel()

	const count = 5000
	nums := make([]string, count)
	for i := range nums {
		nums[i] = fmt.Sprint(i)
	}
	p := lookaheadParser(strings.Join(nums, " "))

	for i := range count {
		tok, err := p.Consume()
		if err != nil {
			t.Fatalf("token %d: %v", i, err)
		}
		if want := fmt.Sprint(i); tok.Value != want {
			t.Fatalf("token %d = %q, want %q", i, tok.Value, want)
		}
	}
	if tok, _ := p.Peek(0); tok.Type != lexer.TokenEOF {
		t.Errorf("after %d tokens, got %v, want EOF", count, tok.Type)
	}
}

// TestLookaheadPeekAheadAcrossRewind pins that a multi-token lookahead still
// reports the right tokens after the buffer has rewound.
//
// Peeking two ahead keeps the buffer non-empty across consumes, so the reset
// only fires between peeks; getting the index wrong would shift what Peek(1)
// and Peek(2) answer.
func TestLookaheadPeekAheadAcrossRewind(t *testing.T) {
	t.Parallel()

	const count = 600
	nums := make([]string, count)
	for i := range nums {
		nums[i] = fmt.Sprint(i)
	}
	p := lookaheadParser(strings.Join(nums, " "))

	for i := range count - 2 {
		for ahead := range 3 {
			tok, err := p.Peek(ahead)
			if err != nil {
				t.Fatalf("peek %d at %d: %v", ahead, i, err)
			}
			if want := fmt.Sprint(i + ahead); tok.Value != want {
				t.Fatalf("peek %d at %d = %q, want %q", ahead, i, tok.Value, want)
			}
		}
		if _, err := p.Consume(); err != nil {
			t.Fatalf("consume %d: %v", i, err)
		}
	}
}

// TestLookaheadOffsetsTrackTheRewind covers the parallel end-offset buffer,
// which is indexed by the same head and reset with it. Offsets must stay
// monotonic and keep pointing at the token Peek returns.
func TestLookaheadOffsetsTrackTheRewind(t *testing.T) {
	t.Parallel()

	const count = 500
	nums := make([]string, count)
	for i := range nums {
		nums[i] = fmt.Sprint(i)
	}
	p := lookaheadParser(strings.Join(nums, " ")).WithOffsetTracking()

	var prev int64
	for i := range count {
		tok, err := p.Peek(0)
		if err != nil {
			t.Fatalf("peek %d: %v", i, err)
		}
		end := p.PeekEnd(0)
		if end < 0 {
			t.Fatalf("token %d (%q): offset tracking reported no end", i, tok.Value)
		}
		if end < prev {
			t.Fatalf("token %d (%q): end %d went backwards from %d", i, tok.Value, end, prev)
		}
		prev = end
		if _, err = p.Consume(); err != nil {
			t.Fatalf("consume %d: %v", i, err)
		}
	}
}
