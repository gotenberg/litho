package mq

import "testing"

// maxFuzzDecisions bounds how many decisions one fuzz input decodes. Past the
// end of the data the decoder feeds marker 1-bits forever, so without a bound
// a single input would never finish. Test-only, not a decoder limit.
const maxFuzzDecisions = 1 << 14

// FuzzDecoder feeds arbitrary bytes to the decoder. The contract is
// crash-freedom and termination: every read stays inside the data and every
// decision returns 0 or 1, whatever the bytes.
func FuzzDecoder(f *testing.F) {
	f.Add(t88TestCoded)
	f.Add(t88TestCoded[:11])
	f.Add([]byte{})
	f.Add([]byte{0xFF})
	f.Add([]byte{0xFF, 0x8F, 0xFF, 0x90})

	f.Fuzz(func(t *testing.T, data []byte) {
		d := NewDecoder(data)
		// Several contexts, chosen by the previous decisions, so the fuzzer
		// reaches states a single context adapting in one direction would not.
		contexts := make([]Context, 4)
		prev := 0
		for range maxFuzzDecisions {
			bit := d.Decode(&contexts[prev])
			if bit != 0 && bit != 1 {
				t.Fatalf("Decode returned %d", bit)
			}
			prev = (prev<<1 | bit) & 3
		}
	})
}
