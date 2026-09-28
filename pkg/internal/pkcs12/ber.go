package pkcs12

import (
	"encoding/asn1"
	"fmt"
)

// maxBERDepth caps how deeply [toDER] and [octets] descend into nested
// constructed values, so a crafted chain of constructed encodings cannot
// exhaust the stack. Anti-DoS guard, not spec-mandated: the deepest path a
// PKCS #12 structure takes (PFX, ContentInfo, SafeBag, EncryptedPrivateKeyInfo,
// PBES2 parameters, PBKDF2 parameters, PRF identifier) is about ten levels,
// and a constructed string splits into chunks one level deep in practice.
const maxBERDepth = 64

// maxTagBytes caps the bytes of a high-tag-number identifier. Anti-DoS guard,
// not spec-mandated: no tag PKCS #12 uses needs more than one byte, and four
// bytes already encode tag numbers past 2^21.
const maxTagBytes = 4

// toDER re-encodes a BER value (ITU-T X.690, 8) in the definite, minimal
// lengths DER requires (X.690, 10.1), which is all [asn1.Unmarshal] rejects
// in the BER some exporters write: indefinite lengths from streaming
// encoders, long-form lengths a short form would hold. Primitive contents are
// copied unchanged, and a constructed string stays constructed, for
// [octets] to join. data must hold exactly one value.
func toDER(data []byte) ([]byte, error) {
	out, rest, err := derValue(data, 0)
	if err != nil {
		return nil, err
	}
	if len(rest) != 0 {
		return nil, fmt.Errorf("%w: %d bytes after the value", ErrMalformed, len(rest))
	}
	return out, nil
}

// derValue re-encodes the first value of in (see [toDER]) and returns it and
// the bytes after it.
func derValue(in []byte, depth int) (out, rest []byte, err error) {
	if depth > maxBERDepth {
		return nil, nil, fmt.Errorf("%w: values nest deeper than %d", ErrLimit, maxBERDepth)
	}
	if len(in) < 2 {
		return nil, nil, fmt.Errorf("%w: truncated value", ErrMalformed)
	}
	tagLen := 1
	if in[0]&0x1f == 0x1f {
		// X.690, 8.1.2.4: the tag number continues while bit 8 is set.
		for tagLen < len(in) && in[tagLen]&0x80 != 0 {
			tagLen++
			if tagLen > maxTagBytes {
				return nil, nil, fmt.Errorf("%w: identifier longer than %d bytes", ErrLimit, maxTagBytes)
			}
		}
		tagLen++
		if tagLen >= len(in) {
			return nil, nil, fmt.Errorf("%w: truncated identifier", ErrMalformed)
		}
	}
	tag := in[:tagLen]
	constructed := in[0]&0x20 != 0
	in = in[tagLen:]

	lb := in[0]
	in = in[1:]
	if lb == 0x80 {
		// X.690, 8.1.3.6: the indefinite form, constructed values only,
		// closed by the end-of-contents octets 00 00.
		if !constructed {
			return nil, nil, fmt.Errorf("%w: indefinite length on a primitive value", ErrMalformed)
		}
		var content []byte
		for {
			if len(in) >= 2 && in[0] == 0 && in[1] == 0 {
				return appendHeader(tag, content), in[2:], nil
			}
			var child []byte
			child, in, err = derValue(in, depth+1)
			if err != nil {
				return nil, nil, err
			}
			content = append(content, child...)
		}
	}
	n := int(lb)
	if lb&0x80 != 0 {
		// X.690, 8.1.3.5: the long form, lb&0x7f length bytes. Four bytes
		// already exceed any input [Decode] accepts.
		k := int(lb & 0x7f)
		if k > 4 || k > len(in) {
			return nil, nil, fmt.Errorf("%w: length of %d bytes", ErrMalformed, k)
		}
		n = 0
		for _, b := range in[:k] {
			n = n<<8 | int(b)
		}
		in = in[k:]
	}
	if n > len(in) {
		return nil, nil, fmt.Errorf("%w: value of %d bytes, %d left", ErrMalformed, n, len(in))
	}
	content, rest := in[:n], in[n:]
	if !constructed {
		return appendHeader(tag, content), rest, nil
	}
	var joined []byte
	for len(content) > 0 {
		var child []byte
		child, content, err = derValue(content, depth+1)
		if err != nil {
			return nil, nil, err
		}
		joined = append(joined, child...)
	}
	return appendHeader(tag, joined), rest, nil
}

// appendHeader returns tag, content's minimal definite length (X.690,
// 10.1) and content.
func appendHeader(tag, content []byte) []byte {
	out := make([]byte, 0, len(tag)+5+len(content))
	out = append(out, tag...)
	n := len(content)
	switch {
	case n < 0x80:
		out = append(out, byte(n))
	default:
		var lb []byte
		for v := n; v > 0; v >>= 8 {
			lb = append([]byte{byte(v)}, lb...)
		}
		out = append(out, 0x80|byte(len(lb))) //nolint:gosec // len(lb) is at most 8, the bytes of an int.
		out = append(out, lb...)
	}
	return append(out, content...)
}

// octets returns the value of a string, joining the chunks of a constructed
// one (X.690, 8.7.3), whatever its tag, so it reads an OCTET STRING and the
// implicitly tagged [0] of an EncryptedContentInfo alike.
func octets(v asn1.RawValue) ([]byte, error) {
	return joinOctets(v, 0)
}

// joinOctets is [octets] at nesting depth.
func joinOctets(v asn1.RawValue, depth int) ([]byte, error) {
	if !v.IsCompound {
		return v.Bytes, nil
	}
	if depth > maxBERDepth {
		return nil, fmt.Errorf("%w: string chunks nest deeper than %d", ErrLimit, maxBERDepth)
	}
	var out []byte
	for rest := v.Bytes; len(rest) > 0; {
		var chunk asn1.RawValue
		var err error
		rest, err = asn1.Unmarshal(rest, &chunk)
		if err != nil {
			return nil, fmt.Errorf("%w: string chunk: %w", ErrMalformed, err)
		}
		// X.690, 8.7.3.2: each chunk of a constructed OCTET STRING is itself
		// an OCTET STRING.
		if chunk.Class != asn1.ClassUniversal || chunk.Tag != asn1.TagOctetString {
			return nil, fmt.Errorf("%w: string chunk of tag %d", ErrMalformed, chunk.Tag)
		}
		b, err := joinOctets(chunk, depth+1)
		if err != nil {
			return nil, err
		}
		out = append(out, b...)
	}
	return out, nil
}
