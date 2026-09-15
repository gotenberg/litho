package type1

import (
	"bytes"
	"testing"
)

// type1FontProgram builds a whole Type 1 program the readers decrypt end to
// end: a clear-text header, the eexec keyword, and an encrypted private section
// holding /lenIV and a /CharStrings entry whose charstring is itself encrypted.
//
// It reuses the package's existing test encryptors. What it adds over
// [TestParseCharStringWidths_Synthetic] is the surrounding font: that fixture
// starts at "currentfile eexec", so it never exercises the clear-text scan that
// locates the keyword, and it is always binary, so the ASCII-hex branch of
// [normaliseEexec] goes unreached.
//
// A seed like this cannot arise by mutation: the cipher makes every byte of the
// section depend on all the ones before it, so random edits decrypt to noise
// and the readers past eexec never see structure.
func type1FontProgram(hexEncoded bool) []byte {
	charstring := []byte{139, 248, 136, csOpHsbw} // sbx 0, width 500, hsbw
	enc := encryptCharString(charstring, defaultLenIV)

	// The first eexecSalt bytes are discarded by the decryptor, so the private
	// section proper starts after them.
	body := []byte("xxxx" +
		"dup /Private 8 dict dup begin\n" +
		"/lenIV 4 def\n" +
		"/CharStrings 1 dict dup begin\n" +
		"/A ")
	body = append(body, []byte(itoa(len(enc))+" RD ")...)
	body = append(body, enc...)
	body = append(body, []byte(" ND\nend\nend\n")...)

	section := encryptEexec(body)
	if hexEncoded {
		const hexDigits = "0123456789abcdef"
		hexed := make([]byte, 0, len(section)*2)
		for _, b := range section {
			hexed = append(hexed, hexDigits[b>>4], hexDigits[b&0x0F])
		}
		section = hexed
	}

	var out bytes.Buffer
	out.WriteString("%!PS-AdobeFont-1.0: Fuzz 001.001\n")
	out.WriteString("/FontMatrix [0.001 0 0 0.001 0 0] readonly def\n")
	out.WriteString("/Encoding 256 array\ndup 65 /A put\nreadonly def\n")
	out.WriteString("currentfile eexec\n")
	out.Write(section)
	out.WriteString("\ncleartomark\n")
	return out.Bytes()
}

// FuzzType1Eexec feeds arbitrary bytes to the Type 1 readers through the
// encrypted path and asserts crash-freedom.
//
// The program comes from a /FontFile stream, so the eexec section, the lengths
// declared inside its decrypted text and the charstrings it names are all
// file-controlled. Decryption is what makes this worth fuzzing separately from
// [FuzzType1]: it turns arbitrary bytes into arbitrary plaintext, so the
// readers downstream parse input no producer ever wrote, with lengths that
// need not match anything.
func FuzzType1Eexec(f *testing.F) {
	f.Add(type1FontProgram(false))
	f.Add(type1FontProgram(true))
	f.Add(type1FontProgram(false)[:len(type1FontProgram(false))/2])
	f.Add([]byte("currentfile eexec\n"))
	// Exactly the salt and nothing else: the decryptor strips eexecSalt bytes
	// and must not read past them.
	f.Add([]byte("currentfile eexec\n\x00\x00\x00\x00"))
	f.Add([]byte{})

	f.Fuzz(func(_ *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		_ = ParseCharStringNames(data)
		_ = ParseCharStringWidths(data)
		_ = ParseEncoding(data)
		_, _ = FontMatrixScale(data)
	})
}

// TestType1FontProgramDecrypts pins that the seed survives both decryption
// layers, in both eexec encodings, and yields the width its charstring
// declares. A seed that failed to decrypt would leave every reader past eexec
// untouched while the target looked well seeded.
func TestType1FontProgramDecrypts(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		hex  bool
	}{
		{"binary eexec", false},
		{"ASCII-hex eexec", true},
	} {
		data := type1FontProgram(tc.hex)

		if names := ParseCharStringNames(data); len(names) == 0 {
			t.Errorf("%s: no charstring names recovered", tc.name)
		}
		if got, ok := ParseCharStringWidths(data)["A"]; !ok || got != 500 {
			t.Errorf("%s: width of /A = %v (present %v), want 500", tc.name, got, ok)
		}
		if scale, ok := FontMatrixScale(data); !ok || scale <= 0 {
			t.Errorf("%s: FontMatrixScale = %v, %v; want a positive scale", tc.name, scale, ok)
		}
	}
}
