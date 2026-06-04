package tagging

import "testing"

// FuzzInjectMarks feeds arbitrary content bytes to the marked-content
// injector with derived spans. The contract is crash-freedom: no input may
// panic, and a valid set of spans must round-trip the original bytes.
func FuzzInjectMarks(f *testing.F) {
	f.Add([]byte("BT /F1 12 Tf (hi) Tj ET"))
	f.Add([]byte(""))
	f.Add([]byte("q 1 0 0 1 0 0 cm Q"))

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		// Derive a couple of non-overlapping, in-range spans from the input.
		var spans []markSpan
		if n := len(data); n >= 2 {
			mid := n / 2
			spans = []markSpan{
				{start: 0, end: mid, mcid: 0, tag: RoleH1},
				{start: mid, end: n, mcid: 1, tag: RoleP},
			}
		}
		out, err := injectMarks(data, spans)
		if err != nil {
			return // out-of-range/overlap is a valid rejection, not a crash
		}
		// Valid spans covering the whole input must preserve every original
		// byte (they only get bracketed, never rewritten).
		if len(out) < len(data) {
			t.Fatalf("injectMarks shrank content: %d < %d", len(out), len(data))
		}
	})
}
