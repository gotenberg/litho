package cff

import "testing"

func TestEmptyCharStrings(t *testing.T) {
	t.Parallel()
	valid := testCFF()
	if _, _, ok := EmptyCharStrings(valid); ok {
		t.Error("a program with charstrings reads as empty")
	}

	// Point the CharStrings operator at a two-byte INDEX of count 0 appended
	// to the program.
	nameEnd, err := skipINDEX(valid, int(valid[2]))
	if err != nil {
		t.Fatal(err)
	}
	empty := redirectCharStrings(t, append(append([]byte{}, valid...), 0, 0), nameEnd, int32(len(valid)))
	dw, cid, ok := EmptyCharStrings(empty)
	if !ok || cid {
		t.Fatalf("EmptyCharStrings = %v, %v, %v; want a name-keyed empty program", dw, cid, ok)
	}
	if dw < 0 {
		t.Errorf("defaultWidthX = %v", dw)
	}

	if _, _, ok := EmptyCharStrings(valid[:3]); ok {
		t.Error("a truncated header reads as empty")
	}
	beyond := redirectCharStrings(t, valid, nameEnd, int32(len(valid)+10))
	if _, _, ok := EmptyCharStrings(beyond); ok {
		t.Error("a CharStrings offset past the data reads as empty")
	}
}
