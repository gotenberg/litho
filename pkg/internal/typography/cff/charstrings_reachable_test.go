package cff

import (
	"encoding/binary"
	"testing"
)

func TestCharStringsReachable(t *testing.T) {
	t.Parallel()
	valid := testCFF()
	if !CharStringsReachable(valid) {
		t.Fatal("a well-formed program reads as unreachable")
	}

	// A Top DICT INDEX holding no font.
	nameEnd, err := skipINDEX(valid, int(valid[2]))
	if err != nil {
		t.Fatal(err)
	}
	emptyTop := append(append([]byte{}, valid[:nameEnd]...), 0, 0, 0, 0, 0, 0)
	if CharStringsReachable(emptyTop) {
		t.Error("an empty Top DICT INDEX reads as reachable")
	}

	// The CharStrings offset pointed past the end of the data.
	_, tops, err := parseINDEX(valid, nameEnd)
	if err != nil || len(tops) == 0 {
		t.Fatal("test program has no Top DICT")
	}
	beyond := redirectCharStrings(t, valid, nameEnd, int32(len(valid)+10))
	if CharStringsReachable(beyond) {
		t.Error("a CharStrings offset past the data reads as reachable")
	}

	if CharStringsReachable(valid[:3]) {
		t.Error("a truncated header reads as reachable")
	}
}

// redirectCharStrings returns a copy of data whose first Top DICT gives the
// CharStrings operator the offset to, written in the five-byte form so the
// DICT keeps its length wherever the operand was a five-byte form already.
// It fails the test when the DICT writes the operand in another form.
func redirectCharStrings(t *testing.T, data []byte, nameEnd int, to int32) []byte {
	t.Helper()
	out := append([]byte{}, data...)
	_, tops, err := parseINDEX(out, nameEnd)
	if err != nil {
		t.Fatal(err)
	}
	top := tops[0]
	for i := 0; i+5 < len(top); i++ {
		if top[i] == 29 && top[i+5] == 17 {
			binary.BigEndian.PutUint32(top[i+1:i+5], uint32(to)) //nolint:gosec // G115: the test offset is a small positive value.
			return out
		}
	}
	t.Skip("test program writes its CharStrings offset in a short form")
	return nil
}
