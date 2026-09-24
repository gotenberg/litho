package postscript

import (
	"math"
	"testing"
)

// FuzzExecute feeds arbitrary Type 4 programs to the parser and evaluator.
//
// The program is a decoded stream straight from the file. Both halves must
// never panic, and a program that runs must leave no more than the 100-entry
// operand stack ISO 32000-1, 7.10.5.1 allows.
func FuzzExecute(f *testing.F) {
	f.Add([]byte("{ dup 0.78 mul exch 0.55 mul 0 exch 0 exch }"), 0.5)
	f.Add([]byte("{ dup 0.5 gt { 1 } { 0 } ifelse"), 0.25)
	f.Add([]byte("{ 1 -1 copy }"), 0.0)

	f.Fuzz(func(t *testing.T, program []byte, x float64) {
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return
		}
		p, err := Compile(program)
		if err != nil {
			return
		}
		out, err := p.Execute([]float64{x})
		if err != nil {
			return
		}
		if len(out) > maxStackDepth {
			t.Fatalf("Execute left %d entries, more than the %d-entry stack", len(out), maxStackDepth)
		}
	})
}
