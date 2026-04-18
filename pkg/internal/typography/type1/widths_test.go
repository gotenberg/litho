package type1

import "testing"

func TestParseLenIV_Default(t *testing.T) {
	if got := parseLenIV([]byte("no lenIV here")); got != 4 {
		t.Errorf("got %d, want default 4", got)
	}
}

func TestParseLenIV_Explicit(t *testing.T) {
	for _, tc := range []struct {
		data string
		want int
	}{
		{"/Private 5 dict dup begin\n/lenIV 0 def", 0},
		{"/lenIV 8 def", 8},
		{"/lenIV -1 def", 4}, // negative falls back to default
	} {
		if got := parseLenIV([]byte(tc.data)); got != tc.want {
			t.Errorf("parseLenIV(%q) = %d, want %d", tc.data, got, tc.want)
		}
	}
}

func TestExtractAdvanceWidth_Hsbw(t *testing.T) {
	// Encode lsb=50, width=500, then hsbw (operator 13).
	// Integer 50: 50 + 139 = 189 → single byte 189
	// Integer 500: not in -107..107; not in 108..1131 easily either.
	//   500 = 247..250 first byte + second byte.
	//   500 - 108 = 392 = 1 * 256 + 136, so first byte 247+1=248, second 136.
	cs := []byte{189, 248, 136, csOpHsbw}
	w, ok := extractAdvanceWidth(cs)
	if !ok || w != 500 {
		t.Errorf("got (%v, %v), want (500, true)", w, ok)
	}
}

func TestExtractAdvanceWidth_Sbw(t *testing.T) {
	// Sbw takes 4 operands: lsbx, lsby, wx, wy.
	// Encode lsbx=0, lsby=0, wx=750, wy=0; then escape (12) + sbw (7).
	// 0 as -107..107: byte = 0 + 139 = 139.
	// 750 - 108 = 642 = 2*256 + 130, first byte 249, second 130.
	cs := []byte{139, 139, 249, 130, 139, csOpEscape, csOpEscapeSbw}
	w, ok := extractAdvanceWidth(cs)
	if !ok || w != 750 {
		t.Errorf("got (%v, %v), want (750, true)", w, ok)
	}
}

func TestExtractAdvanceWidth_NegativeInt(t *testing.T) {
	// lsb=-50, width=-200, hsbw.
	// -50 = 251..254 first byte + second.
	//   -50 fits in -107..107: byte = -50 + 139 = 89.
	// -200: not in -107..107; use 251..254 encoding.
	//   -(-200) = 200; 200 - 108 = 92. first byte 251+0=251, second 92. No wait:
	//   For bytes 251-254: value = -((b - 251) * 256) - next - 108.
	//   So b = 251, next = N gives value = -N - 108. For -200: N = 92.
	cs := []byte{89, 251, 92, csOpHsbw}
	w, ok := extractAdvanceWidth(cs)
	if !ok || w != -200 {
		t.Errorf("got (%v, %v), want (-200, true)", w, ok)
	}
}

func TestExtractAdvanceWidth_Empty(t *testing.T) {
	if _, ok := extractAdvanceWidth(nil); ok {
		t.Error("empty input: expected false")
	}
}

func TestExtractAdvanceWidth_NoMetricOp(t *testing.T) {
	// Charstring that starts with hstem (op 1) instead of hsbw: invalid
	// for Type 1 per spec, skipped by our extractor.
	cs := []byte{189, 248, 136, csOpHstem}
	if _, ok := extractAdvanceWidth(cs); ok {
		t.Error("expected false when leading operator is not hsbw/sbw")
	}
}

func TestDecryptCharString_StripsLenIV(t *testing.T) {
	// Round-trip: encrypt then decrypt.
	plain := []byte{139, 139, 201, csOpHsbw} // lsb=0, wx=62 (201-139), hsbw
	enc := encryptCharString(plain, 4)
	got := decryptCharString(enc, 4)
	if len(got) != len(plain) {
		t.Fatalf("length: got %d, want %d", len(got), len(plain))
	}
	for i := range plain {
		if got[i] != plain[i] {
			t.Errorf("byte %d: got 0x%02X, want 0x%02X", i, got[i], plain[i])
		}
	}
}

func TestParseCharStringWidths_Synthetic(t *testing.T) {
	cs1 := []byte{189, 248, 136, csOpHsbw} // width 500
	cs2 := []byte{139, 165, csOpHsbw}      // lsb=0, width=26 (165-139), hsbw
	enc1 := encryptCharString(cs1, 4)
	enc2 := encryptCharString(cs2, 4)

	body := []byte("xxxx" +
		"/Private 5 dict dup begin\n" +
		"/lenIV 4 def\n" +
		"/CharStrings 2 dict dup begin\n" +
		"/alpha ")
	body = append(body, []byte(itoa(len(enc1))+" RD ")...)
	body = append(body, enc1...)
	body = append(body, []byte(" ND\n/beta ")...)
	body = append(body, []byte(itoa(len(enc2))+" RD ")...)
	body = append(body, enc2...)
	body = append(body, []byte(" ND\nend\nend")...)

	font := append([]byte("currentfile eexec\n"), encryptEexec(body)...)
	widths := ParseCharStringWidths(font)

	if widths["alpha"] != 500 {
		t.Errorf("alpha: got %v, want 500", widths["alpha"])
	}
	if widths["beta"] != 26 {
		t.Errorf("beta: got %v, want 26", widths["beta"])
	}
}

// encryptCharString is the test-only inverse of decryptCharString.
func encryptCharString(plain []byte, lenIV int) []byte {
	body := make([]byte, lenIV+len(plain))
	for i := 0; i < lenIV; i++ {
		body[i] = 0x00
	}
	copy(body[lenIV:], plain)
	out := make([]byte, len(body))
	r := charStringKey
	for i, p := range body {
		c := p ^ byte(r>>8)
		out[i] = c
		r = (uint32(c)+r)*eexecC1 + eexecC2
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
