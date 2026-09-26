package compliance

import (
	"bytes"
	"testing"
)

// FuzzUA1HeaderLine asserts that picking the header line out of a file's
// leading bytes never panics and returns a line of the input that holds no
// EOL marker. The bytes are attacker-controlled: they are the start of an
// untrusted document.
func FuzzUA1HeaderLine(f *testing.F) {
	f.Add([]byte("junk\r\n%PDF-1.7\n%\xe2\xe3\xcf\xd3\n1 0 obj"))
	f.Add([]byte("junk\r\n%PD"))
	f.Add([]byte(""))

	f.Fuzz(func(t *testing.T, data []byte) {
		line := ua1HeaderLine(data)
		if bytes.ContainsAny(line, "\r\n") || !bytes.Contains(data, line) {
			t.Errorf("ua1HeaderLine(%q) = %q, not one line of the input", data, line)
		}
	})
}
