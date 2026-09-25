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
//   - A UTF-16 entity is recognized from its bytes ([utf16Order]) and
//     transcoded to UTF-8 before parsing.
//   - An encoding declaration naming an IANA-registered encoding that is
//     ASCII-compatible, ISO-8859-1 or windows-1252 for instance, decodes the
//     rest of the entity. Per 4.3.3 names match case-insensitively, and an
//     IANA-registered name denotes the encoding registered for it.
//
// Whether an entity is UTF-16 is read off its bytes, never off its
// declaration. Deliberate leniency: 4.3.3 requires a UTF-16 entity to begin
// with a byte order mark, and makes it a fatal error for an entity to be in
// an encoding other than the one its declaration names. An entity declaring
// UTF-16 whose bytes are ASCII-compatible is read as those bytes, as if it
// declared no encoding. An entity whose bytes are UTF-16, with a byte order
// mark or without, is read as UTF-16 whatever encoding litho reads its
// declaration names. The XMP reader of the compliance package,
// xmpDetectEncoding, applies the same rule for the reason it gives:
// veraPDF's XMPCore reads a metadata packet's bytes whatever encoding it
// declares, and holds valid a UTF-8 packet declaring UTF-16. litho reads
// the XMP packet and the embedded invoice of one PDF alike.
//
// Everything else is an error: an encoding name outside production [81]
// EncName, which makes the declaration not well-formed; a name IANA does not
// register or x/text cannot decode, which 4.3.3 makes a fatal error ("an
// entity with an encoding that it is unable to process"); and a declaration
// of an ASCII-incompatible encoding, EBCDIC for instance, on an entity
// Appendix F.1 has already read as ASCII-compatible to reach the
// declaration, which contradicts its bytes.
//
// # Corpus evidence
//
//   - All corpora: none of the 159 files that embed XML declares UTF-16 or
//     begins with a byte order mark or a NUL byte, so the rule moves no
//     verdict.
func NewDecoder(payload []byte) *xml.Decoder {
	wide := false
	if order, ok := utf16Order(payload); ok {
		// UseBOM drops a byte order mark, which also overrides order.
		if transcoded, err := unicode.UTF16(order, unicode.UseBOM).NewDecoder().Bytes(payload); err == nil {
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

// utf16Order reports whether payload is UTF-16, and in which byte order: it
// begins with a byte order mark (XML 1.0, 4.3.3), or its first two code
// units are each a NUL byte and a non-NUL one, which XML 1.0, Appendix F.1
// reads as UTF-16 with no byte order mark from the "<?" an XML declaration
// opens with. No UTF-8 entity begins that way, as XML admits no NUL
// character (2.2).
//
// The byte test is the one xmpDetectEncoding in the compliance package
// applies to an XMP packet, so the two readers agree on what is UTF-16.
func utf16Order(payload []byte) (unicode.Endianness, bool) {
	switch {
	case bytes.HasPrefix(payload, []byte{0xFE, 0xFF}):
		return unicode.BigEndian, true
	case bytes.HasPrefix(payload, []byte{0xFF, 0xFE}):
		return unicode.LittleEndian, true
	case len(payload) < 4:
		return unicode.BigEndian, false
	case payload[0] == 0 && payload[1] != 0 && payload[2] == 0 && payload[3] != 0:
		return unicode.BigEndian, true
	case payload[0] != 0 && payload[1] == 0 && payload[2] != 0 && payload[3] == 0:
		return unicode.LittleEndian, true
	}
	return unicode.BigEndian, false
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
		// Transcoded already when the bytes are UTF-16, and read as they
		// are when they are not: the bytes decide, see [NewDecoder].
		return input, nil
	}
	if wide {
		// A UTF-16 entity declaring another encoding: the bytes decide.
		return input, nil
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
