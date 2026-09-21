package parser

import (
	"bytes"
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ioat"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
)

// parseInput runs the parser over src the way every caller does.
func parseInput(t *testing.T, src string) error {
	t.Helper()
	data := []byte(src)
	r := ioat.NewReader(bytes.NewReader(data), int64(len(data)), 0)
	_, err := NewParser(lexer.NewLexer(r)).ParseObject(context.Background())
	return err
}

// TestParseObject_NestingIsBounded pins a depth bound on the three recursive
// paths through [Parser.ParseObject].
//
// Without one the recursion is limited only by the goroutine stack, and a
// stack overflow is a fatal runtime error that Go cannot recover from: the
// recover boundary every pkg/pdf entry point installs converts panics, not
// fatal errors, so the process dies. Measured before the bound existed: a
// dictionary nested 850,000 deep (a 7.3 MB file) killed the test binary, and
// the same file opened through pdf.Open killed it too.
//
// The same failure mode is why [maxDestIndirectionDepth] exists in
// core/document, where a 700-byte file killed the process through the public
// Bookmarks and Annotations calls.
//
// The three paths are separate because they recurse through different
// functions and instrumenting only the two container parsers would miss the
// third: an object header chain reaches ParseObject through
// parseIndirectObject with no dictionary or array anywhere in the input.
func TestParseObject_NestingIsBounded(t *testing.T) {
	t.Parallel()

	// Comfortably past the bound, far short of the depth that overflows the
	// stack, so a failure here means the bound is missing rather than that
	// the machine ran out of stack.
	const tooDeep = 20000

	cases := []struct {
		name string
		src  string
	}{
		{"dictionaries", strings.Repeat("<< /A ", tooDeep) + "null" + strings.Repeat(" >>", tooDeep)},
		{"arrays", strings.Repeat("[", tooDeep) + strings.Repeat("]", tooDeep)},
		{"object header chain", strings.Repeat("1 0 obj ", tooDeep) + "null"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := parseInput(t, tc.src); err == nil {
				t.Fatalf("%s nested %d deep parsed without error: the recursion is unbounded",
					tc.name, tooDeep)
			}
		})
	}
}

// TestParseObject_RealNestingStillParses is the control for the bound above.
// A guard that refused everything would satisfy that test while breaking every
// document, so the depths here are the ones real files actually reach.
//
// The deepest nesting measured across a sample of all seven corpora is 156
// (govdocs thread_200_200087.pdf), with a 99th percentile of 55 and a median
// of 3, so 200 sits above every observed document.
func TestParseObject_RealNestingStillParses(t *testing.T) {
	t.Parallel()

	for _, depth := range []int{1, 3, 55, 156, 200} {
		t.Run(strconv.Itoa(depth), func(t *testing.T) {
			t.Parallel()
			src := strings.Repeat("<< /A ", depth) + "null" + strings.Repeat(" >>", depth)
			if err := parseInput(t, src); err != nil {
				t.Fatalf("a dictionary nested %d deep is ordinary, but it failed: %v", depth, err)
			}
			arr := strings.Repeat("[", depth) + strings.Repeat("]", depth)
			if err := parseInput(t, arr); err != nil {
				t.Fatalf("an array nested %d deep is ordinary, but it failed: %v", depth, err)
			}
		})
	}
}
