// Command gen-saslprep generates the stringprep tables of
// pkg/internal/crypto/saslprep from RFC 3454, Appendices A to D.
//
// ISO 32000-2, 7.6.4.3.3 prepares an AES-256 password with SASLprep (RFC
// 4013), the stringprep profile that names tables A.1, B.1, C.1.2 to C.9, D.1
// and D.2 of RFC 3454. The generator reads the RFC text, pinned by its
// SHA-256, transcribes every table those sections name into sorted code point
// ranges, and refuses a table line that is not a code point or a code point
// range, so a misread page cannot emit a partial table.
//
// Usage:
//
//	go run ./cmd/gen-saslprep                    # download the pinned RFC text
//	go run ./cmd/gen-saslprep --src rfc3454.txt  # read a local copy
//	go generate ./pkg/internal/crypto/saslprep/...
package main
