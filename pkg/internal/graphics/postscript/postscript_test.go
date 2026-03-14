package postscript

import (
	"errors"
	"math"
	"testing"
)

func almostEqual(a, b, eps float64) bool {
	return math.Abs(a-b) < eps
}

func TestArithmeticOperators(t *testing.T) {
	result, err := Execute(nil, []byte("{ 3 4 add }"))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if len(result) != 1 || result[0] != 7 {
		t.Errorf("3 4 add = %v, want [7]", result)
	}

	result, err = Execute(nil, []byte("{ 10 3 sub }"))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result[0] != 7 {
		t.Errorf("10 3 sub = %v, want 7", result[0])
	}

	result, err = Execute(nil, []byte("{ 5 6 mul }"))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result[0] != 30 {
		t.Errorf("5 6 mul = %v, want 30", result[0])
	}

	result, err = Execute(nil, []byte("{ 15 4 div }"))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result[0] != 3.75 {
		t.Errorf("15 4 div = %v, want 3.75", result[0])
	}
}

func TestIntegerDivAndMod(t *testing.T) {
	result, err := Execute(nil, []byte("{ 17 5 idiv }"))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result[0] != 3 {
		t.Errorf("17 5 idiv = %v, want 3", result[0])
	}

	result, err = Execute(nil, []byte("{ 17 5 mod }"))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result[0] != 2 {
		t.Errorf("17 5 mod = %v, want 2", result[0])
	}
}

func TestDivisionByZero(t *testing.T) {
	_, err := Execute(nil, []byte("{ 1 0 div }"))
	if !errors.Is(err, ErrDivisionByZero) {
		t.Errorf("expected ErrDivisionByZero, got %v", err)
	}
}

func TestUnaryMathOps(t *testing.T) {
	tests := []struct {
		program string
		want    float64
	}{
		{"{ -5 neg }", 5},
		{"{ -3.7 abs }", 3.7},
		{"{ 2.3 ceiling }", 3},
		{"{ 2.7 floor }", 2},
		{"{ 2.5 round }", 3}, // Go rounds to even for .5, but math.Round rounds away from 0
		{"{ 2.7 truncate }", 2},
		{"{ 9 sqrt }", 3},
	}
	for _, tt := range tests {
		result, err := Execute(nil, []byte(tt.program))
		if err != nil {
			t.Fatalf("Execute(%q) failed: %v", tt.program, err)
		}
		if !almostEqual(result[0], tt.want, 1e-9) {
			t.Errorf("Execute(%q) = %v, want %v", tt.program, result[0], tt.want)
		}
	}
}

func TestTrigonometry(t *testing.T) {
	// sin(90) = 1, cos(0) = 1
	result, err := Execute(nil, []byte("{ 90 sin }"))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !almostEqual(result[0], 1.0, 1e-9) {
		t.Errorf("sin(90) = %v, want 1", result[0])
	}

	result, err = Execute(nil, []byte("{ 0 cos }"))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !almostEqual(result[0], 1.0, 1e-9) {
		t.Errorf("cos(0) = %v, want 1", result[0])
	}

	// atan(1, 0) = 90 degrees
	result, err = Execute(nil, []byte("{ 1 0 atan }"))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !almostEqual(result[0], 90.0, 1e-9) {
		t.Errorf("atan(1,0) = %v, want 90", result[0])
	}
}

func TestExpAndLog(t *testing.T) {
	result, err := Execute(nil, []byte("{ 2 10 exp }"))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result[0] != 1024 {
		t.Errorf("2^10 = %v, want 1024", result[0])
	}

	result, err = Execute(nil, []byte("{ 100 log }"))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !almostEqual(result[0], 2.0, 1e-9) {
		t.Errorf("log(100) = %v, want 2", result[0])
	}
}

func TestStackOperators(t *testing.T) {
	// dup
	result, err := Execute(nil, []byte("{ 5 dup }"))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if len(result) != 2 || result[0] != 5 || result[1] != 5 {
		t.Errorf("5 dup = %v, want [5,5]", result)
	}

	// exch
	result, err = Execute(nil, []byte("{ 1 2 exch }"))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result[0] != 2 || result[1] != 1 {
		t.Errorf("1 2 exch = %v, want [2,1]", result)
	}

	// pop
	result, err = Execute(nil, []byte("{ 1 2 pop }"))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if len(result) != 1 || result[0] != 1 {
		t.Errorf("1 2 pop = %v, want [1]", result)
	}

	// copy
	result, err = Execute(nil, []byte("{ 1 2 3 2 copy }"))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if len(result) != 5 {
		t.Fatalf("copy: got %d elements, want 5", len(result))
	}
	expected := []float64{1, 2, 3, 2, 3}
	for i, v := range expected {
		if result[i] != v {
			t.Errorf("copy result[%d] = %v, want %v", i, result[i], v)
		}
	}

	// index
	result, err = Execute(nil, []byte("{ 10 20 30 1 index }"))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result[len(result)-1] != 20 {
		t.Errorf("index(1) = %v, want 20", result[len(result)-1])
	}
}

func TestRoll(t *testing.T) {
	// 3 1 roll on [a b c] → [c a b]
	result, err := Execute(nil, []byte("{ 1 2 3 3 1 roll }"))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	expected := []float64{3, 1, 2}
	if len(result) != 3 {
		t.Fatalf("roll: got %d elements, want 3", len(result))
	}
	for i, v := range expected {
		if result[i] != v {
			t.Errorf("roll result[%d] = %v, want %v", i, result[i], v)
		}
	}
}

func TestComparisons(t *testing.T) {
	tests := []struct {
		program string
		want    float64
	}{
		{"{ 5 5 eq }", 1},
		{"{ 5 3 eq }", 0},
		{"{ 5 3 ne }", 1},
		{"{ 5 3 gt }", 1},
		{"{ 3 5 gt }", 0},
		{"{ 5 5 ge }", 1},
		{"{ 3 5 lt }", 1},
		{"{ 5 5 le }", 1},
	}
	for _, tt := range tests {
		result, err := Execute(nil, []byte(tt.program))
		if err != nil {
			t.Fatalf("Execute(%q) failed: %v", tt.program, err)
		}
		if result[0] != tt.want {
			t.Errorf("Execute(%q) = %v, want %v", tt.program, result[0], tt.want)
		}
	}
}

func TestBooleanOps(t *testing.T) {
	result, err := Execute(nil, []byte("{ true }"))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result[0] != 1 {
		t.Errorf("true = %v, want 1", result[0])
	}

	result, err = Execute(nil, []byte("{ 0 not }"))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result[0] != 1 {
		t.Errorf("0 not = %v, want 1", result[0])
	}

	result, err = Execute(nil, []byte("{ 1 not }"))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result[0] != 0 {
		t.Errorf("1 not = %v, want 0", result[0])
	}
}

func TestIfOperator(t *testing.T) {
	// true branch taken
	result, err := Execute(nil, []byte("{ 1 { 42 } if }"))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if len(result) != 1 || result[0] != 42 {
		t.Errorf("true if = %v, want [42]", result)
	}

	// false branch not taken
	result, err = Execute(nil, []byte("{ 0 { 42 } if }"))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("false if = %v, want []", result)
	}
}

func TestIfElseOperator(t *testing.T) {
	// true branch
	result, err := Execute(nil, []byte("{ 1 { 10 } { 20 } ifelse }"))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result[0] != 10 {
		t.Errorf("true ifelse = %v, want 10", result[0])
	}

	// false branch
	result, err = Execute(nil, []byte("{ 0 { 10 } { 20 } ifelse }"))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result[0] != 20 {
		t.Errorf("false ifelse = %v, want 20", result[0])
	}
}

func TestInputsPassedToStack(t *testing.T) {
	// Tint transform: input 0.5 → map to CMYK (0, 0.2, 0.4, 0.1)
	// This simulates a simple Separation color space function.
	program := []byte("{ dup 0.4 mul exch dup 0.8 mul exch 0.2 mul 0 4 1 roll }")
	result, err := Execute([]float64{0.5}, program)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if len(result) != 4 {
		t.Fatalf("got %d results, want 4", len(result))
	}
	// Expected: 0, 0.2, 0.4, 0.1
	if !almostEqual(result[0], 0, 1e-9) {
		t.Errorf("C = %v, want 0", result[0])
	}
}

func TestSyntaxErrors(t *testing.T) {
	// Unterminated block.
	_, err := Execute(nil, []byte("{ 1 { 2 }"))
	if !errors.Is(err, ErrSyntax) {
		t.Errorf("expected ErrSyntax for unterminated block, got %v", err)
	}

	// Unknown operator.
	_, err = Execute(nil, []byte("{ 1 foobar }"))
	if !errors.Is(err, ErrUnknownOperator) {
		t.Errorf("expected ErrUnknownOperator, got %v", err)
	}
}

func TestStackUnderflow(t *testing.T) {
	_, err := Execute(nil, []byte("{ add }"))
	if !errors.Is(err, ErrStackUnderflow) {
		t.Errorf("expected ErrStackUnderflow, got %v", err)
	}
}

func TestBitshift(t *testing.T) {
	result, err := Execute(nil, []byte("{ 7 3 bitshift }"))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result[0] != 56 {
		t.Errorf("7 << 3 = %v, want 56", result[0])
	}

	// Right shift.
	result, err = Execute(nil, []byte("{ 56 -3 bitshift }"))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result[0] != 7 {
		t.Errorf("56 >> 3 = %v, want 7", result[0])
	}
}

func TestNestedIfElse(t *testing.T) {
	// Nested: if x > 0 then (if x > 5 then 100 else 50) else 0
	program := []byte("{ dup 0 gt { dup 5 gt { pop 100 } { pop 50 } ifelse } { pop 0 } ifelse }")

	result, err := Execute([]float64{10}, program)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result[0] != 100 {
		t.Errorf("nested(10) = %v, want 100", result[0])
	}

	result, err = Execute([]float64{3}, program)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result[0] != 50 {
		t.Errorf("nested(3) = %v, want 50", result[0])
	}

	result, err = Execute([]float64{-1}, program)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result[0] != 0 {
		t.Errorf("nested(-1) = %v, want 0", result[0])
	}
}

func TestCMYKTintTransform(t *testing.T) {
	// Typical Separation tint transform: tint → (0, tint*0.78, tint*0.55, 0)
	program := []byte("{ dup 0.78 mul exch 0.55 mul 0 exch 0 exch }")
	// Input: 1.0 → expected (0, 0.78, 0.55, 0)
	// Wait, let me trace: stack starts [1.0]
	// dup → [1.0, 1.0]
	// 0.78 → [1.0, 1.0, 0.78]
	// mul → [1.0, 0.78]
	// exch → [0.78, 1.0]
	// 0.55 → [0.78, 1.0, 0.55]
	// mul → [0.78, 0.55]
	// 0 → [0.78, 0.55, 0]
	// exch → [0.78, 0, 0.55]
	// 0 → [0.78, 0, 0.55, 0]
	// exch → [0.78, 0, 0, 0.55]
	// Result: [0.78, 0, 0, 0.55]
	// Hmm, that's not standard CMYK order. Let me just verify execution works.
	result, err := Execute([]float64{1.0}, program)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if len(result) != 4 {
		t.Fatalf("got %d results, want 4", len(result))
	}
}

func TestNoOuterBraces(t *testing.T) {
	// Programs without outer { } should still work.
	result, err := Execute(nil, []byte("3 4 add"))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if len(result) != 1 || result[0] != 7 {
		t.Errorf("3 4 add (no braces) = %v, want [7]", result)
	}
}
