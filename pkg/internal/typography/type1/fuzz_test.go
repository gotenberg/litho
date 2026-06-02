package type1

import "testing"

// FuzzType1 feeds arbitrary bytes to the Type 1 font readers, which
// scan the decrypted, attacker-controlled charstring and encoding
// sections. The contract is crash-freedom, not a successful parse.
func FuzzType1(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte("/Encoding 256 array\n0 1 255 {1 index exch /.notdef put} for\ndup 65 /A put\n"))
	f.Add([]byte("2 dict dup begin\n/A 5 RD ????? ND\n/B 5 RD ????? ND\nend\n"))

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		_ = ParseCharStringNames(data)
		_ = ParseEncoding(data)
		_ = ParseCharStringWidths(data)
	})
}
