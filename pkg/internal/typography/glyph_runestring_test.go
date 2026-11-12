package typography

import "testing"

func TestRuneStringMatchesStringConversion(t *testing.T) {
	for _, r := range []rune{0, 'A', ' ', 0x7f, 0x80, 0xe9, 0x20ac, 0x1f600, -1, 0x110000} {
		if got, want := runeString(r), string(r); got != want {
			t.Errorf("runeString(%#x) = %q, want %q", r, got, want)
		}
	}
}

func TestRuneStringSharesASCII(t *testing.T) {
	if n := testing.AllocsPerRun(100, func() { _ = runeString('a') }); n != 0 {
		t.Errorf("runeString('a') allocates %v times, want 0", n)
	}
}
