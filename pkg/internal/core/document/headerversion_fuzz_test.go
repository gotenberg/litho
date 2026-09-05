package document

import (
	"strings"
	"testing"
)

// FuzzHeaderVersion feeds arbitrary bytes to the header version reader. The
// bytes come straight from the file, so the function must never panic and must
// never report something that is not a version number: the value is compared
// against profile requirements, and a caller that receives "1.3%" or a
// half-parsed string cannot tell it apart from a real answer.
func FuzzHeaderVersion(f *testing.F) {
	for _, seed := range []string{
		"1.4\n",
		"1.7\r%\xe2\xe3\xcf\xd3\r\n",
		"1.3%\r\n",
		"2.0",
		"1.",
		"",
		".",
		"17\n",
		"1.10 extra text\n",
		"99999999999999999999.0\n",
		"\x00\x00\x00",
		"1.4\x00trailing",
	} {
		f.Add([]byte(seed))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		got := headerVersion(data)
		if got == "" {
			return
		}
		major, minor, found := strings.Cut(got, ".")
		if !found || major == "" || minor == "" {
			t.Fatalf("headerVersion(%q) = %q, which is not major.minor", data, got)
		}
		for _, part := range []string{major, minor} {
			for _, c := range part {
				if c < '0' || c > '9' {
					t.Fatalf("headerVersion(%q) = %q, which holds a non-digit", data, got)
				}
			}
		}
		if !strings.HasPrefix(string(data), got) {
			t.Fatalf("headerVersion(%q) = %q, which is not a prefix of the input", data, got)
		}
		if !isPDFVersion(got) {
			t.Fatalf("headerVersion(%q) = %q, which isPDFVersion rejects", data, got)
		}
	})
}
