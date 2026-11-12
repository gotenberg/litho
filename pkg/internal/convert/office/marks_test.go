package office

import "testing"

// TestMoveKey pairs texts of one author equal once trimmed, at least six
// characters with an inner space, case kept.
func TestMoveKey(t *testing.T) {
	t.Parallel()
	key := func(author, s string) string {
		k, ok := MoveKey(author, s)
		if !ok {
			return "-"
		}
		return k
	}
	for _, tc := range []struct {
		a, b string
		pair bool
	}{
		{"abc de", "  abc de ", true},
		{"ab cd", "ab cd", false},
		{"Sentencemoves", "Sentencemoves", false},
		{"Case Words", "case words", false},
	} {
		ka, kb := key("Ada", tc.a), key("Ada", tc.b)
		if got := ka != "-" && ka == kb; got != tc.pair {
			t.Errorf("%q and %q pair %v, want %v", tc.a, tc.b, got, tc.pair)
		}
	}
	if key("Ada", "abc de") == key("Grace", "abc de") {
		t.Error("two authors pair")
	}
}

// TestMarkColor draws a move in green whatever its author, and cycles the
// author colours past the ninth.
func TestMarkColor(t *testing.T) {
	t.Parallel()
	if c := MarkColor(MarkMoveTo, 3); c != MovedColor {
		t.Errorf("move colour %06X", c)
	}
	if MarkColor(MarkInsert, 9) != MarkColor(MarkDelete, 0) || Hex(AuthorColor(0)) != "C69200" {
		t.Error("author colours do not cycle from C69200")
	}
	if c := RGB(0xE1F2FA); c.R != 225.0/255 || c.G != 242.0/255 || c.B != 250.0/255 || c.A != 1 {
		t.Errorf("RGB %v", c)
	}
}
