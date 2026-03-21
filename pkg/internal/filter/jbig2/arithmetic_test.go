package jbig2

import "testing"

func TestCX_Basic(t *testing.T) {
	cx := NewCX(256, 0)

	// Initial state.
	if cx.cxVal() != 0 {
		t.Errorf("initial cx = %d, want 0", cx.cxVal())
	}
	if cx.mpsVal() != 0 {
		t.Errorf("initial mps = %d, want 0", cx.mpsVal())
	}

	// Set and toggle.
	cx.setCx(5)
	if cx.cxVal() != 5 {
		t.Errorf("after setCx(5): cx = %d, want 5", cx.cxVal())
	}

	cx.toggleMps()
	if cx.mpsVal() != 1 {
		t.Errorf("after toggleMps: mps = %d, want 1", cx.mpsVal())
	}

	cx.toggleMps()
	if cx.mpsVal() != 0 {
		t.Errorf("after double toggleMps: mps = %d, want 0", cx.mpsVal())
	}
}

func TestCX_IndexSwitch(t *testing.T) {
	cx := NewCX(16, 0)
	cx.SetIndex(5)
	cx.setCx(10)

	cx.SetIndex(0)
	if cx.cxVal() != 0 {
		t.Errorf("index 0 cx = %d, want 0", cx.cxVal())
	}

	cx.SetIndex(5)
	if cx.cxVal() != 10 {
		t.Errorf("index 5 cx = %d, want 10", cx.cxVal())
	}
}

func TestArithmeticIntegerDecoder_SetPrev(t *testing.T) {
	// Test the setPrev method's two branches (< 256, >= 256).
	aid := &ArithmeticIntegerDecoder{}

	aid.prev = 100
	aid.setPrev(1)
	if aid.prev != 201 {
		t.Errorf("setPrev(1) from 100: got %d, want 201", aid.prev)
	}

	aid.prev = 300
	aid.setPrev(0)
	// (((300 << 1) | 0) & 511) | 256) & 0x1ff
	// (600 & 511) | 256 = 89 | 256 = 345 & 0x1ff = 345
	want := (((300<<1 | 0) & 511) | 256) & 0x1ff
	if aid.prev != want {
		t.Errorf("setPrev(0) from 300: got %d, want %d", aid.prev, want)
	}
}
