package type1

import "testing"

func TestParseEncoding_ExplicitVector(t *testing.T) {
	data := []byte(`%!PS-AdobeFont-1.0: CMSY8 003.002
/FontInfo 9 dict dup begin
/version (003.002) readonly def
end readonly def
/FontName /CMSY8 def
/Encoding 256 array
0 1 255 {1 index exch /.notdef put} for
dup 3 /asteriskmath put
dup 102 /braceleft put
dup 103 /braceright put
dup 1 /periodcentered put
readonly def
currentdict end
currentfile eexec
` + "\x80\x81\x82\x83binary-garbage-that-should-be-skipped")

	enc := ParseEncoding(data)

	cases := map[int]string{
		3:   "asteriskmath",
		102: "braceleft",
		103: "braceright",
		1:   "periodcentered",
	}
	for idx, want := range cases {
		if enc[idx] != want {
			t.Errorf("enc[%d]: got %q, want %q", idx, enc[idx], want)
		}
	}
	if enc[0] != "" {
		t.Errorf("enc[0]: got %q, want empty", enc[0])
	}
}

func TestParseEncoding_NamedReference(t *testing.T) {
	data := []byte(`%!PS-AdobeFont-1.0
/Encoding StandardEncoding def
currentfile eexec
binary...`)

	enc := ParseEncoding(data)

	for _, idx := range []int{0, 33, 128, 255} {
		if enc[idx] != "StandardEncoding" {
			t.Errorf("enc[%d]: got %q, want StandardEncoding", idx, enc[idx])
		}
	}
}

func TestParseEncoding_Empty(t *testing.T) {
	enc := ParseEncoding(nil)
	for idx, name := range enc {
		if name != "" {
			t.Errorf("enc[%d]: got %q, want empty", idx, name)
		}
	}
}

func TestParseEncoding_NoEncodingKeyword(t *testing.T) {
	data := []byte("%!PS-AdobeFont-1.0\n/FontName /NoEnc def\ncurrentfile eexec\n")
	enc := ParseEncoding(data)
	for idx, name := range enc {
		if name != "" {
			t.Errorf("enc[%d]: got %q, want empty", idx, name)
		}
	}
}

func TestParseEncoding_StopsAtEexec(t *testing.T) {
	// A "dup N /fake put" after the eexec marker must be ignored:
	// anything there belongs to the encrypted binary blob.
	data := []byte(`%!PS-AdobeFont-1.0
/Encoding 256 array
dup 5 /realglyph put
readonly def
currentfile eexec
dup 99 /fakeglyph put
`)
	enc := ParseEncoding(data)
	if enc[5] != "realglyph" {
		t.Errorf("enc[5] = %q", enc[5])
	}
	if enc[99] != "" {
		t.Errorf("enc[99] leaked from encrypted section: %q", enc[99])
	}
}
