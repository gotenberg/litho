package compliance

import "errors"

// maxBERDepth bounds the nesting berToDER follows. Not spec-mandated: an
// anti-DoS guard. A CMS SignedData nests a certificate's fields about a dozen
// levels deep, so 64 leaves ample room while keeping a crafted blob of nested
// indefinite lengths from recursing without end.
const maxBERDepth = 64

// errBERMalformed reports a BER encoding that runs past its input, nests past
// [maxBERDepth], or uses a form BER does not allow.
var errBERMalformed = errors.New("malformed BER encoding")

// berToDER rewrites the first BER-encoded value in b with definite, minimal
// lengths, the length form encoding/asn1 requires. RFC 5652 lets a SignedData
// be written in BER, and a streaming signer writes indefinite lengths (ISO/IEC
// 8825-1, 8.1.3.6), which encoding/asn1 rejects; veraPDF reads them. Tags and
// contents are copied as they are, so a value already in DER comes back
// unchanged. Bytes after the first value, such as the zero padding a signature
// /Contents carries, are ignored.
func berToDER(b []byte) ([]byte, error) {
	out, _, err := berValue(b, 0)
	return out, err
}

// berValue re-encodes the value at the start of b and returns it with the
// bytes that follow it.
func berValue(b []byte, depth int) (out, rest []byte, err error) {
	if depth > maxBERDepth || len(b) < 2 {
		return nil, nil, errBERMalformed
	}
	// Identifier octets: a low-tag-number form takes one byte, a high one
	// continues while bit 8 is set (ISO/IEC 8825-1, 8.1.2).
	n := 1
	if b[0]&0x1f == 0x1f {
		for n < len(b) && b[n]&0x80 != 0 {
			n++
		}
		n++
		if n >= len(b) {
			return nil, nil, errBERMalformed
		}
	}
	tag, constructed := b[:n], b[0]&0x20 != 0
	b = b[n:]

	var content []byte
	switch b[0] {
	case 0x80:
		// Indefinite length: constructed only, contents end at two zero
		// octets (8.1.3.6).
		if !constructed {
			return nil, nil, errBERMalformed
		}
		b = b[1:]
		for {
			if len(b) >= 2 && b[0] == 0 && b[1] == 0 {
				b = b[2:]
				break
			}
			child, next, err := berValue(b, depth+1)
			if err != nil {
				return nil, nil, err
			}
			content = append(content, child...)
			b = next
		}
		rest = b
	default:
		size, hdr, err := berLength(b)
		if err != nil {
			return nil, nil, err
		}
		body := b[hdr : hdr+size]
		rest = b[hdr+size:]
		if !constructed {
			content = body
			break
		}
		// A definite constructed value can still hold indefinite children.
		for len(body) > 0 {
			child, next, err := berValue(body, depth+1)
			if err != nil {
				return nil, nil, err
			}
			content = append(content, child...)
			body = next
		}
	}
	out = append(append(append(make([]byte, 0, len(tag)+6+len(content)), tag...), derLength(len(content))...), content...)
	return out, rest, nil
}

// berLength reads a definite length at the start of b and returns it with the
// number of length octets, checking it against the bytes that follow.
func berLength(b []byte) (size, hdr int, err error) {
	if len(b) == 0 {
		return 0, 0, errBERMalformed
	}
	if b[0] < 0x80 {
		size, hdr = int(b[0]), 1
	} else {
		k := int(b[0] & 0x7f)
		// Four length octets reach 4 GiB, far past maxCMSBlobSize; more is
		// no length a real signature carries.
		if k == 0 || k > 4 || len(b) < 1+k {
			return 0, 0, errBERMalformed
		}
		for _, c := range b[1 : 1+k] {
			size = size<<8 | int(c)
		}
		hdr = 1 + k
	}
	if size > len(b)-hdr {
		return 0, 0, errBERMalformed
	}
	return size, hdr, nil
}

// derLength encodes n in the minimal definite form (ISO/IEC 8825-1, 8.1.3).
func derLength(n int) []byte {
	if n < 0x80 {
		return []byte{byte(n)} //nolint:gosec // n < 0x80 fits one octet.
	}
	var octets []byte
	for v := n; v > 0; v >>= 8 {
		octets = append([]byte{byte(v)}, octets...) //nolint:gosec // one octet of n at a time.
	}
	return append([]byte{0x80 | byte(len(octets))}, octets...) //nolint:gosec // at most 8 length octets.
}
