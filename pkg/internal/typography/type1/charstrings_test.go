package type1

import "testing"

func TestParseCharStringNames_Empty(t *testing.T) {
	if got := ParseCharStringNames(nil); got != nil {
		t.Errorf("nil input: got %v, want nil", got)
	}
	if got := ParseCharStringNames([]byte("no eexec here")); got != nil {
		t.Errorf("no-eexec input: got %v, want nil", got)
	}
}

func TestParseCharStringNames_Synthetic(t *testing.T) {
	// Build a minimal Type 1 font program: clear-text preamble plus a
	// plaintext "private section" the test eexec-encrypts on the way out.
	plaintext := []byte(
		"xxxx" + // 4-byte salt: decryption strips these
			"/Private 10 dict dup begin\n" +
			"/CharStrings 3 dict dup begin\n" +
			"/.notdef 4 RD AAAA ND\n" +
			"/asteriskmath 6 RD ABCDEF ND\n" +
			"/alpha 5 RD HELLO ND\n" +
			"end\n" +
			"end",
	)
	enc := encryptEexec(plaintext)
	font := append([]byte("%!PS-AdobeFont-1.0\ncurrentfile eexec\n"), enc...)

	got := ParseCharStringNames(font)
	want := []string{".notdef", "asteriskmath", "alpha"}

	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i, name := range want {
		if got[i] != name {
			t.Errorf("[%d]: got %q, want %q", i, got[i], name)
		}
	}
}

func TestParseCharStringNames_LenPrefixSkipsBinaryTokens(t *testing.T) {
	// The encrypted charstring bytes contain a byte sequence that
	// looks like "/fake 1 RD X ND". A length-prefix-driven parser
	// must ignore it; a naive regex scan would pick it up.
	fake := []byte("/fake 1 RD X ND ")
	plaintext := []byte(
		"xxxx" +
			"/Private 10 dict dup begin\n" +
			"/CharStrings 1 dict dup begin\n" +
			"/realglyph ")
	plaintext = append(plaintext, []byte(" ")...)
	lengthLine := []byte("16 RD ")
	plaintext = append(plaintext[:len(plaintext)-1], lengthLine...)
	plaintext = append(plaintext, fake...)
	plaintext = append(plaintext, []byte("ND\nend\nend")...)

	enc := encryptEexec(plaintext)
	font := append([]byte("currentfile eexec\n"), enc...)

	got := ParseCharStringNames(font)
	if len(got) != 1 || got[0] != "realglyph" {
		t.Errorf("got %v, want [realglyph]", got)
	}
}

// encryptEexec is the inverse of [decryptEexec]. It is test-only so
// the unit tests can construct synthetic encrypted fixtures without
// depending on external fonts.
func encryptEexec(plain []byte) []byte {
	out := make([]byte, len(plain))
	r := eexecKey
	for i, p := range plain {
		c := p ^ byte(r>>8)
		out[i] = c
		r = (uint32(c)+r)*eexecC1 + eexecC2
	}
	return out
}
