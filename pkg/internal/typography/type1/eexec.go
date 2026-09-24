package type1

import "bytes"

// Adobe Type 1 eexec stream cipher constants. See Appendix A of the
// Type 1 Font Format specification (Adobe Systems, 1993).
const (
	eexecKey  uint32 = 55665
	eexecC1   uint32 = 52845
	eexecC2   uint32 = 22719
	eexecSalt        = 4
)

// decryptEexec returns the plaintext PostScript code that follows the
// "currentfile eexec" keyword in a Type 1 font program. Both encoded
// forms the spec permits are handled: ASCII-hex (the portion before
// the encrypted bytes is a sequence of 0-9a-fA-F) and raw binary
// (anything else is treated as binary). The 4-byte random salt that
// prefixes every eexec stream is stripped from the returned plaintext.
//
// An all-zero salt or a stream shorter than the salt returns nil so
// callers can treat the absence of an eexec body as non-fatal.
func decryptEexec(data []byte) []byte {
	if len(data) == 0 {
		return nil
	}
	binary := normaliseEexec(data)
	if len(binary) <= eexecSalt {
		return nil
	}
	plain := make([]byte, len(binary))
	r := eexecKey
	for i, c := range binary {
		plain[i] = c ^ byte(r>>8)
		r = (uint32(c)+r)*eexecC1 + eexecC2
	}
	return plain[eexecSalt:]
}

// normaliseEexec inspects the leading bytes and returns a binary
// eexec payload. An ASCII-hex encoded section is converted to binary;
// raw binary input passes through unchanged. Decoding stops at the
// first non-hex byte so stray whitespace and section terminators do
// not corrupt the stream.
func normaliseEexec(data []byte) []byte {
	// Skip leading newlines/spaces that PDF producers often include
	// between "currentfile eexec" and the payload.
	i := 0
	for i < len(data) && isEexecWhitespace(data[i]) {
		i++
	}
	if i >= len(data) {
		return nil
	}
	if !isHexEncoded(data[i:]) {
		return data[i:]
	}
	// ASCII-hex. Two hex digits per output byte.
	var buf bytes.Buffer
	buf.Grow(len(data) / 2) //nolint:mnd // approximate final size, exact is 2 hex chars per byte
	var nib byte
	haveNib := false
	for j := i; j < len(data); j++ {
		c := data[j]
		if isEexecWhitespace(c) {
			continue
		}
		v, ok := hexNibble(c)
		if !ok {
			break
		}
		if !haveNib {
			nib = v
			haveNib = true
			continue
		}
		buf.WriteByte(nib<<4 | v)
		haveNib = false
	}
	return buf.Bytes()
}

// isHexEncoded reports whether an eexec payload is written in ASCII-hex rather
// than raw binary. The Type 1 Font Format specification (Adobe Systems, 1993),
// 7.2, settles the two apart on the first four bytes: the encrypted binary
// begins with a random salt, so a payload whose first four bytes are all
// hexadecimal digits is hex and any other is binary. Reading one byte is not
// enough, because a binary salt byte is a hexadecimal digit better than one
// time in twelve.
//
// # Corpus evidence
//
//   - govdocs thread_300_300035.pdf: starts its binary section with 0x32
//     ('2'), which made litho hex-decode the cipher text into noise and
//     read no glyph at all from the program.
func isHexEncoded(data []byte) bool {
	const probe = 4
	seen := 0
	for _, c := range data {
		if isEexecWhitespace(c) {
			continue
		}
		if !isHexDigit(c) {
			return false
		}
		if seen++; seen == probe {
			return true
		}
	}
	return false
}

func isEexecWhitespace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '\f'
}

func isHexDigit(c byte) bool {
	_, ok := hexNibble(c)
	return ok
}

func hexNibble(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}
