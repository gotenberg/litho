package xsd

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/ianaindex"
	"golang.org/x/text/encoding/unicode"
)

// NewDecoder returns a strict [xml.Decoder] over payload that reads the
// character encodings XML 1.0 (Fifth Edition), 4.3.3 has a processor read,
// and reports as an error, wrapping [ErrEncoding], an entity it cannot read.
//
// encoding/xml reads UTF-8 alone and refuses any other encoding declaration
// unless given a [xml.Decoder.CharsetReader]. Per 4.3.3 every processor reads
// UTF-8 and UTF-16, and a processor may read the other encodings IANA
// registers:
//
//   - A UTF-16 entity begins with a byte order mark (4.3.3), from which it is
//     recognized and transcoded to UTF-8 before parsing.
//   - An encoding declaration naming an IANA-registered encoding that is
//     ASCII-compatible, ISO-8859-1 or windows-1252 for instance, decodes the
//     rest of the entity. Per 4.3.3 names match case-insensitively, and an
//     IANA-registered name denotes the encoding registered for it.
//
// Everything else is an error: an encoding name outside production [81]
// EncName, which makes the declaration not well-formed; a name IANA does not
// register or x/text cannot decode, which 4.3.3 makes a fatal error ("an
// entity with an encoding that it is unable to process"); and a declaration
// that contradicts the bytes, which is also a fatal error there, such as
// UTF-16 declared on an entity with no byte order mark, which Appendix F.1
// has already read as ASCII-compatible to reach the declaration.
func NewDecoder(payload []byte) *xml.Decoder {
	wide := false
	if bytes.HasPrefix(payload, []byte{0xFE, 0xFF}) || bytes.HasPrefix(payload, []byte{0xFF, 0xFE}) {
		// ExpectBOM reads the byte order from the mark and drops it.
		if transcoded, err := unicode.UTF16(unicode.BigEndian, unicode.ExpectBOM).NewDecoder().Bytes(payload); err == nil {
			payload, wide = transcoded, true
		}
	}
	dec := xml.NewDecoder(bytes.NewReader(payload))
	dec.Strict = true
	dec.CharsetReader = func(label string, input io.Reader) (io.Reader, error) {
		return charsetReader(label, input, wide)
	}
	return dec
}

// charsetReader decodes input under the encoding label names. encoding/xml
// calls it for every declared encoding other than UTF-8, after reading the
// declaration itself as UTF-8. wide reports that [NewDecoder] transcoded the
// entity from UTF-16.
func charsetReader(label string, input io.Reader, wide bool) (io.Reader, error) {
	if !isEncName(label) {
		return nil, fmt.Errorf("%w: %q is not an encoding name (XML 1.0, 4.3.3, production [81])", ErrEncoding, label)
	}
	enc, err := ianaindex.IANA.Encoding(label)
	if err != nil || enc == nil {
		return nil, fmt.Errorf("%w: %q is not an encoding litho reads", ErrEncoding, label)
	}
	canonical, err := ianaindex.IANA.Name(enc)
	if err != nil {
		return nil, fmt.Errorf("%w: %q: %w", ErrEncoding, label, err)
	}
	switch canonical {
	case "UTF-8":
		// An alias such as csUTF8: the bytes are what encoding/xml reads.
		return input, nil
	case "UTF-16", "UTF-16BE", "UTF-16LE":
		if wide {
			return input, nil
		}
		return nil, fmt.Errorf("%w: %q declared on an entity with no UTF-16 byte order mark", ErrEncoding, label)
	}
	if wide {
		return nil, fmt.Errorf("%w: %q declared on an entity that begins with a UTF-16 byte order mark", ErrEncoding, label)
	}
	if !asciiCompatible(enc) {
		return nil, fmt.Errorf("%w: %q declared on an entity read as ASCII-compatible", ErrEncoding, label)
	}
	return enc.NewDecoder().Reader(input), nil
}

// isEncName reports whether name matches XML 1.0, 4.3.3, production [81]:
// EncName ::= [A-Za-z] ([A-Za-z0-9._] | '-')*.
func isEncName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z':
		case i > 0 && (c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-'):
		default:
			return false
		}
	}
	return true
}

// asciiCompatible reports whether enc writes the characters of an XML
// declaration as their ASCII bytes, the family XML 1.0, Appendix F.1 detects
// from an entity that begins with "<?xm" and no byte order mark. EBCDIC and
// the wide encodings do not.
func asciiCompatible(enc encoding.Encoding) bool {
	const probe = `<?xml version="1.0" encoding=''?>`
	out, err := enc.NewEncoder().Bytes([]byte(probe))
	return err == nil && string(out) == probe
}
