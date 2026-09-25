package xsd

import (
	"context"
	"errors"
	"testing"

	"golang.org/x/text/encoding/unicode"
)

// encodingXSD holds a Name to at most three characters, so a value decoded
// from a single-byte encoding passes where its raw UTF-8 reading of a
// multi-byte sequence would not, and the reverse.
const encodingXSD = `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
           targetNamespace="urn:t" elementFormDefault="qualified">
  <xs:simpleType name="ShortName">
    <xs:restriction base="xs:string">
      <xs:maxLength value="3"/>
    </xs:restriction>
  </xs:simpleType>
  <xs:element name="Name" type="ShortName"/>
</xs:schema>`

// utf16 encodes s as UTF-16 in the given byte order, byte order mark first.
func utf16(t *testing.T, s string, order unicode.Endianness) []byte {
	t.Helper()
	out, err := unicode.UTF16(order, unicode.UseBOM).NewEncoder().Bytes([]byte(s))
	if err != nil {
		t.Fatalf("encoding UTF-16: %v", err)
	}
	return out
}

// TestValidate_ReadsTheEncodingsXMLAllows pins that the encodings XML 1.0,
// 4.3.3 has a processor read parse and validate: a declaration naming UTF-8
// in any case, an IANA-registered single-byte encoding, and UTF-16 behind a
// byte order mark in either order.
func TestValidate_ReadsTheEncodingsXMLAllows(t *testing.T) {
	s := loadTestSchema(t, encodingXSD)
	const body = `<Name xmlns="urn:t">Zoë</Name>`
	for name, payload := range map[string][]byte{
		"no declaration": []byte(body),
		"UTF-8":          []byte(`<?xml version="1.0" encoding="UTF-8"?>` + body),
		"utf-8":          []byte(`<?xml version="1.0" encoding="utf-8"?>` + body),
		"csUTF8":         []byte(`<?xml version="1.0" encoding="csUTF8"?>` + body),
		"UTF-8 BOM":      []byte("\xEF\xBB\xBF" + `<?xml version="1.0" encoding="UTF-8"?>` + body),
		"ISO-8859-1":     []byte(`<?xml version="1.0" encoding="ISO-8859-1"?><Name xmlns="urn:t">Zo` + "\xEB" + `</Name>`),
		"windows-1252":   []byte(`<?xml version="1.0" encoding="windows-1252"?><Name xmlns="urn:t">Zo` + "\xEB" + `</Name>`),
		"UTF-16BE":       utf16(t, `<?xml version="1.0" encoding="UTF-16"?>`+body, unicode.BigEndian),
		"UTF-16LE":       utf16(t, `<?xml version="1.0" encoding="UTF-16"?>`+body, unicode.LittleEndian),
		"UTF-16 undecl.": utf16(t, body, unicode.LittleEndian),
	} {
		t.Run(name, func(t *testing.T) {
			vs, err := Validate(context.Background(), s, payload)
			if err != nil {
				t.Fatalf("Validate: %v", err)
			}
			if len(vs) != 0 {
				t.Errorf("violations: %v, want none", vs)
			}
		})
	}
}

// TestValidate_ISO88591DecodesTheBytes pins that a single-byte declaration
// decodes what follows it: "Zo" and the two bytes C3 AB read as ISO-8859-1
// are four characters, one more than the Name admits, where their UTF-8
// reading would be the three of "Zoë".
func TestValidate_ISO88591DecodesTheBytes(t *testing.T) {
	s := loadTestSchema(t, encodingXSD)
	payload := []byte(`<?xml version="1.0" encoding="ISO-8859-1"?><Name xmlns="urn:t">Zo` + "\xC3\xAB" + `</Name>`)
	vs, err := Validate(context.Background(), s, payload)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(vs) != 1 || vs[0].Rule != RuleFacet {
		t.Errorf("violations: %v, want one maxLength facet violation", vs)
	}
}

// TestValidate_RejectsAnEncodingItCannotRead pins that every encoding
// declaration XML 1.0, 4.3.3 makes an error fails the parse with
// [ErrEncoding]: a name outside production [81], a name IANA does not
// register or x/text cannot decode, and a declaration the bytes contradict.
func TestValidate_RejectsAnEncodingItCannotRead(t *testing.T) {
	s := loadTestSchema(t, encodingXSD)
	const body = `<Name xmlns="urn:t">Zoe</Name>`
	for name, payload := range map[string][]byte{
		"not an EncName":        []byte(`<?xml version="1.0" encoding="UTF-8-W/O-BOM"?>` + body),
		"leading digit":         []byte(`<?xml version="1.0" encoding="8859-1"?>` + body),
		"unregistered":          []byte(`<?xml version="1.0" encoding="x-litho-test"?>` + body),
		"registered, no codec":  []byte(`<?xml version="1.0" encoding="UTF-32"?>` + body),
		"UTF-16 without a BOM":  []byte(`<?xml version="1.0" encoding="UTF-16"?>` + body),
		"EBCDIC on ASCII bytes": []byte(`<?xml version="1.0" encoding="IBM037"?>` + body),
		"Latin-1 behind a BOM":  utf16(t, `<?xml version="1.0" encoding="ISO-8859-1"?>`+body, unicode.BigEndian),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Validate(context.Background(), s, payload)
			if !errors.Is(err, ErrEncoding) {
				t.Errorf("Validate error: %v, want ErrEncoding", err)
			}
		})
	}
}

// TestIsEncName pins production [81] of XML 1.0, 4.3.3.
func TestIsEncName(t *testing.T) {
	for name, want := range map[string]bool{
		"UTF-8":         true,
		"ISO-8859-1":    true,
		"x_foo.bar-1":   true,
		"":              false,
		"8859":          false,
		"-utf":          false,
		"UTF-8-W/O-BOM": false,
		"UTF 8":         false,
	} {
		if got := isEncName(name); got != want {
			t.Errorf("isEncName(%q) = %v, want %v", name, got, want)
		}
	}
}
