package xsd

import (
	"context"
	"testing"
	"testing/fstest"
)

// This package had NO fuzz target. Validate is handed the decoded embedded
// CII invoice stream (compliance/cii.go:347 passes doc.DecodeStream output),
// so its payload is as attacker-controlled as any font or filter input, and it
// is reachable from the public Reader.ValidateFacturX.
//
// The earlier coverage sweep listed parseRawSchema as the remainder here and
// stopped. That is the SCHEMA half, which ships bundled and trusted; the
// PAYLOAD half went unlisted because the criterion counted a []byte in the
// FIRST non-ctx position and Validate carries it third.

// validInstance matches minimalXSD from load_test.go: an Invoice with the
// required currency attribute, an ID passing the [A-Z0-9]+ pattern, and a date.
const validInstance = `<?xml version="1.0"?>` +
	`<Invoice xmlns="urn:test" currency="EUR"><ID>ABC123</ID><Date>2026-01-01</Date></Invoice>`

// FuzzXSDLoad asserts schema loading never panics. A schema is normally
// bundled, so this is the lower-risk half, but parseRawSchema, the prefix
// extractor and the link pass all run on raw bytes and the census named them.
func FuzzXSDLoad(f *testing.F) {
	f.Add([]byte(minimalXSD))
	f.Add([]byte(minimalXSD[:len(minimalXSD)/2])) // Truncated mid-element.
	f.Add([]byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">`))
	f.Add([]byte(""))

	f.Fuzz(func(_ *testing.T, schemaSrc []byte) {
		fsys := fstest.MapFS{"schema.xsd": &fstest.MapFile{Data: schemaSrc}}
		_, _ = Load(context.Background(), fsys, "schema.xsd")
	})
}

// FuzzXSDValidate asserts validation never panics on a crafted payload. The
// schema is held fixed and valid so every mutation lands on the document side,
// which is the side a PDF actually controls: fuzzing both at once would mostly
// produce schemas that fail to load and leave the validator unreached.
func FuzzXSDValidate(f *testing.F) {
	f.Add([]byte(validInstance))
	f.Add([]byte(validInstance[:len(validInstance)/2])) // Truncated.
	f.Add([]byte(`<Invoice xmlns="urn:test"></Invoice>`))
	f.Add([]byte(`<Unknown/>`))
	f.Add([]byte(""))

	schema, err := Load(context.Background(), fstest.MapFS{
		"schema.xsd": &fstest.MapFile{Data: []byte(minimalXSD)},
	}, "schema.xsd")
	if err != nil {
		f.Fatalf("the fixed schema must load, else every exec returns early and the target is inert: %v", err)
	}

	f.Fuzz(func(_ *testing.T, payload []byte) {
		_, _ = Validate(context.Background(), schema, payload)
	})
}
