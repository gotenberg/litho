package postscript

import (
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// nestedProgram returns a program whose body nests n empty blocks.
func nestedProgram(n int) []byte {
	return []byte("{ " + strings.Repeat("{", n) + strings.Repeat("}", n) + " }")
}

// TestCompile_BoundsBlockNesting pins the nesting guard at its boundary.
//
// The parser recursed once per '{' with no bound, so a program of a few
// million braces (a few KB once Flate-compressed) overflowed the goroutine
// stack: a fatal error, not a panic, which no recover can turn into an error.
func TestCompile_BoundsBlockNesting(t *testing.T) {
	t.Parallel()

	if _, err := Compile(nestedProgram(maxBlockDepth)); err != nil {
		t.Fatalf("Compile at the %d-level limit: %v", maxBlockDepth, err)
	}
	if _, err := Compile(nestedProgram(maxBlockDepth + 1)); !errors.Is(err, ErrBlockTooDeep) {
		t.Fatalf("Compile one level past the limit = %v, want ErrBlockTooDeep", err)
	}

	// Far past the limit the guard must stop the descent early rather than
	// only report once the recursion unwinds.
	if _, err := Compile(nestedProgram(5_000_000)); !errors.Is(err, ErrBlockTooDeep) {
		t.Fatalf("Compile of 5M nested blocks = %v, want ErrBlockTooDeep", err)
	}
}

// TestExecute_BoundsOperandStack pins the 100-entry stack of ISO 32000-1,
// 7.10.5.1 at its boundary, and pins that doubling `copy` steps cannot run
// away with memory.
func TestExecute_BoundsOperandStack(t *testing.T) {
	t.Parallel()

	full := []byte("{ " + strings.Repeat("1 ", maxStackDepth) + "}")
	out, err := Execute(nil, full)
	if err != nil {
		t.Fatalf("Execute with %d entries: %v", maxStackDepth, err)
	}
	if len(out) != maxStackDepth {
		t.Fatalf("Execute left %d entries, want %d", len(out), maxStackDepth)
	}

	over := []byte("{ " + strings.Repeat("1 ", maxStackDepth+1) + "}")
	if _, err := Execute(nil, over); !errors.Is(err, ErrStackOverflow) {
		t.Fatalf("Execute with %d entries = %v, want ErrStackOverflow", maxStackDepth+1, err)
	}

	// Each step doubles the stack: unbounded, 30 steps ask for 8 GB. Stopping
	// at 128 overflows the cap without endangering an unguarded build.
	var doubling strings.Builder
	doubling.WriteString("{ 1")
	for k := 1; k <= 128; k <<= 1 {
		doubling.WriteString(" " + strconv.Itoa(k) + " copy")
	}
	doubling.WriteString(" }")
	if _, err := Execute(nil, []byte(doubling.String())); !errors.Is(err, ErrStackOverflow) {
		t.Fatalf("Execute of doubling copy steps = %v, want ErrStackOverflow", err)
	}

	inputs := make([]float64, maxStackDepth+1)
	if _, err := Execute(inputs, []byte("{ }")); !errors.Is(err, ErrStackOverflow) {
		t.Fatalf("Execute with %d inputs = %v, want ErrStackOverflow", len(inputs), err)
	}
}

// TestCopy_RejectsUnusableCounts pins that `copy` refuses a count it cannot
// honor. A negative count passed the old length check and sliced past the end
// of the stack, which panicked.
func TestCopy_RejectsUnusableCounts(t *testing.T) {
	t.Parallel()

	for _, program := range []string{
		"{ 1 2 -1 copy }",      // negative
		"{ 1 2 -1 sqrt copy }", // NaN
		"{ 1 2 3 copy }",       // more than the stack holds
		"{ 1 2 1e300 copy }",   // too large for int
	} {
		if _, err := Execute(nil, []byte(program)); !errors.Is(err, ErrStackUnderflow) {
			t.Errorf("Execute(%q) = %v, want ErrStackUnderflow", program, err)
		}
	}

	out, err := Execute(nil, []byte("{ 1 2 2 copy }"))
	if err != nil {
		t.Fatalf("Execute of a valid copy: %v", err)
	}
	if len(out) != 4 || out[2] != 1 || out[3] != 2 {
		t.Fatalf("1 2 2 copy = %v, want [1 2 1 2]", out)
	}
}

// TestProgram_ExecuteIsReusableAndConcurrent pins that a compiled program is
// evaluated many times, concurrently, without re-parsing or shared state.
func TestProgram_ExecuteIsReusableAndConcurrent(t *testing.T) {
	t.Parallel()

	p, err := Compile([]byte("{ dup 1 exch sub }"))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	var wg sync.WaitGroup
	for i := range 16 {
		wg.Add(1)
		go func(x float64) {
			defer wg.Done()
			for range 100 {
				out, err := p.Execute([]float64{x})
				if err != nil {
					t.Errorf("Execute(%v): %v", x, err)
					return
				}
				if len(out) != 2 || out[0] != x || out[1] != 1-x {
					t.Errorf("Execute(%v) = %v, want [%v %v]", x, out, x, 1-x)
					return
				}
			}
		}(float64(i) / 16)
	}
	wg.Wait()
}
