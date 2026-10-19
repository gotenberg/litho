package type1

import (
	"bytes"
	"context"
	"testing"
)

// FuzzSubsetInPlace feeds arbitrary bytes to the Type 1 subsetter, which
// splits a /FontFile program, decrypts it and rewrites the charstrings its
// decrypted text declares. Every length it reads is file-controlled. The
// contract is crash-freedom and, when it subsets, lengths that partition the
// result.
func FuzzSubsetInPlace(f *testing.F) {
	whole, _ := subsetTestProgram(true, false)
	f.Add(whole, "Aacute")
	f.Add(whole[:len(whole)/2], "A")
	plain, _ := subsetTestProgram(false, true)
	f.Add(plain, "")
	f.Add(type1FontProgram(false), "A")
	f.Add([]byte("currentfile eexec\n\x00\x00\x00\x00"), "")

	f.Fuzz(func(t *testing.T, data []byte, keep string) {
		if len(data) > 1<<20 {
			return
		}
		out, lens, err := SubsetInPlace(context.Background(), data, []string{keep})
		if err != nil {
			return
		}
		if lens.Clear+lens.Encrypted+lens.Trailer != len(out) {
			t.Fatalf("lengths %+v do not partition %d bytes", lens, len(out))
		}
		if !bytes.Equal(out[:lens.Clear], data[:lens.Clear]) {
			t.Fatal("the clear text changed")
		}
	})
}
