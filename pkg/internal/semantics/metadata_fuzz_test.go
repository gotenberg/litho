package semantics

import (
	"context"
	"testing"
)

// The metadata readers below all consume document bytes directly: parseXMP gets
// an XMP packet carved out of the file, and the raw /Info recovery scan walks
// the whole file looking for field values a broken Info dictionary could not
// supply. Both must report nothing rather than panic or read past their input.
//
// pkg/internal/compliance fuzzes its own XMP helpers (FuzzXMPWellFormed,
// FuzzXMPChecks), but it does NOT import this package: those are a separate
// implementation and cannot reach these functions. The earlier fuzz census
// enumerated EXPORTED []byte entry points and so missed every one of these,
// which are unexported behind Metadata(ctx, doc).
//
// The raw scan is bounded by maxRawInfoScanBytes (10 MB, documented at its
// declaration), so these targets probe shape, not size.

// infoSeed wraps a field value in an Info-dictionary shape, with a /Producer
// sibling alongside the key.
//
// The sibling is belt and braces rather than a requirement, and measuring that
// is what corrected this comment. scanRawInfoField gates on finding one of
// /Producer, /Author, /Creator, /Title or /CreationDate within 500 bytes of the
// hit, but four of those five are themselves keys it is asked to scan for, so
// the gate admits its own key and is a no-op for them. A seed carrying only
// "/Title (Hello)" still reaches the extractor. The gate bites only for
// /ModDate and /Subject, which are not in its own list.
func infoSeed(key, value string) []byte {
	return []byte("<< /Producer (litho) " + key + " " + value + " >>")
}

// FuzzScanRawInfoField drives the raw Info-field recovery scan, which runs on
// every Metadata call whose file is under the size cap.
//
// Non-panic only. The scan deliberately skips anything it cannot read, so
// "found a value" is not a property worth asserting: a fuzzer producing no
// recoveries is the scanner working, not failing.
func FuzzScanRawInfoField(f *testing.F) {
	f.Add(infoSeed("/ModDate", "(D:20240101120000Z)"))
	f.Add(infoSeed("/Title", "<48656c6c6f>"))
	f.Add(infoSeed("/Title", "<48656c6c6f"))    // unterminated hex
	f.Add(infoSeed("/Author", "(unterminated")) // unterminated literal
	f.Add(infoSeed("/Author", `(nested (parens) ok)`))
	f.Add(infoSeed("/Author", `(escaped \) paren)`))
	f.Add(infoSeed("/Author", `(trailing backslash \`)) // backslash at the end
	f.Add(infoSeed("/Subject", "<0>"))                  // unpaired nibble
	f.Add(infoSeed("/Subject", "<zz>"))                 // non-hex
	f.Add([]byte("/ModDate (no sibling key nearby)"))   // must not reach an extractor
	f.Add([]byte{})

	f.Fuzz(func(_ *testing.T, data []byte) {
		for _, key := range []string{"ModDate", "Title", "Author", "Subject"} {
			scanRawInfoField(context.Background(), data, key, func(string) {})
		}
	})
}

// FuzzFindBalancedParen asserts the contract its only caller depends on.
//
// scanRawInfoField slices after[i+1 : i+1+end] with the returned value, so a
// result outside [-1, len(s)) is not a wrong answer but an immediate panic one
// frame up. That makes the range a requirement of the production call site
// rather than an invariant invented here.
func FuzzFindBalancedParen(f *testing.F) {
	f.Add([]byte("simple)"))
	f.Add([]byte("nested (inner) outer)"))
	f.Add([]byte(`escaped \) still open)`))
	f.Add([]byte("unterminated"))
	f.Add([]byte(`trailing backslash \`))
	f.Add([]byte(")"))
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		got := findBalancedParen(data)
		if got == -1 {
			return
		}
		if got < 0 || got >= len(data) {
			t.Fatalf("findBalancedParen returned %d for a %d-byte input: "+
				"scanRawInfoField slices with this and would panic", got, len(data))
		}
		if data[got] != ')' {
			t.Fatalf("findBalancedParen pointed at %q, not a closing paren", data[got])
		}
	})
}

// FuzzUnescapePdfLiteralString drives the escape decoder, whose octal branch
// consumes up to three following bytes and is the classic overrun site.
func FuzzUnescapePdfLiteralString(f *testing.F) {
	f.Add([]byte(`plain`))
	f.Add([]byte(`\n\r\t\b\f`))
	f.Add([]byte(`\(\)\\`))
	f.Add([]byte(`\101\102`)) // octal
	f.Add([]byte(`\7`))       // octal, truncated
	f.Add([]byte(`\`))        // lone trailing backslash
	f.Add([]byte("\\\r\n"))   // escaped CRLF
	f.Add([]byte("\\\r"))     // escaped CR at the very end
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		out := unescapePdfLiteralString(data)
		// Unescaping only ever collapses sequences, so it cannot grow.
		if len(out) > len(data) {
			t.Fatalf("unescapePdfLiteralString grew %d bytes into %d", len(data), len(out))
		}
	})
}

// FuzzDecodeHexString asserts the length promise its doc comment makes: an
// unpaired trailing nibble is treated as if followed by '0', so a successful
// decode yields ceil(digits/2) bytes.
func FuzzDecodeHexString(f *testing.F) {
	f.Add([]byte("48656c6c6f"))
	f.Add([]byte("48 65 6c"))  // whitespace is ignored
	f.Add([]byte("4"))         // unpaired nibble
	f.Add([]byte("zz"))        // non-hex
	f.Add([]byte("\t\r\n\f ")) // whitespace only
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		out, ok := decodeHexString(data)
		if !ok {
			return
		}
		digits := 0
		for _, c := range data {
			switch {
			case c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '\f':
			default:
				digits++
			}
		}
		if want := (digits + 1) / 2; len(out) != want {
			t.Fatalf("decodeHexString returned %d bytes for %d hex digits, want %d",
				len(out), digits, want)
		}
	})
}

// FuzzParseXMP drives the XMP reader with arbitrary bytes. The packet is carved
// out of the file by a window scan before it gets here, so its shape is fully
// attacker controlled.
func FuzzParseXMP(f *testing.F) {
	const packet = `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>` +
		`<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF ` +
		`xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` +
		`<rdf:Description xmlns:dc="http://purl.org/dc/elements/1.1/">` +
		`<dc:title><rdf:Alt><rdf:li xml:lang="x-default">T</rdf:li></rdf:Alt></dc:title>` +
		`</rdf:Description></rdf:RDF></x:xmpmeta><?xpacket end="w"?>`

	f.Add([]byte(packet))
	f.Add([]byte(packet[:len(packet)/2]))
	f.Add([]byte("<x:xmpmeta"))
	f.Add([]byte{})

	f.Fuzz(func(_ *testing.T, data []byte) {
		_ = parseXMP(data)
	})
}
