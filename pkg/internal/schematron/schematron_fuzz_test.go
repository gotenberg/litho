package schematron

import (
	"context"
	"testing"
	"testing/fstest"
)

// This package had NO fuzz target. Validate is handed the decoded embedded CII
// invoice stream (compliance/cii.go:599 passes doc.DecodeStream output) and
// runs XPath over it, so the payload is attacker-controlled and reachable from
// the public Reader.ValidateFacturX.
//
// The earlier coverage sweep listed parseRawSchema and peekRoot here and
// stopped. Those are the SCHEMA half, which ships bundled; the PAYLOAD half
// went unlisted because the criterion counted a []byte in the FIRST non-ctx
// position and Validate carries it third.

// validCII matches the namespaces en16931ish (load_test.go) binds, so the
// patterns select real nodes instead of leaving every rule vacuous.
const validCII = `<?xml version="1.0"?>` +
	`<rsm:CrossIndustryInvoice ` +
	`xmlns:rsm="urn:un:unece:uncefact:data:standard:CrossIndustryInvoice:100" ` +
	`xmlns:ram="urn:un:unece:uncefact:data:standard:ReusableAggregateBusinessInformationEntity:100">` +
	`<rsm:ExchangedDocument><ram:ID>INV-1</ram:ID></rsm:ExchangedDocument>` +
	`</rsm:CrossIndustryInvoice>`

// FuzzSchematronLoad asserts schema loading never panics: parseRawSchema,
// peekRoot and the phase-reference validation all read raw bytes.
func FuzzSchematronLoad(f *testing.F) {
	f.Add([]byte(en16931ish))
	f.Add([]byte(en16931ish[:len(en16931ish)/2])) // Truncated mid-element.
	f.Add([]byte(`<sch:schema xmlns:sch="http://purl.oclc.org/dsdl/schematron">`))
	f.Add([]byte(""))

	f.Fuzz(func(_ *testing.T, schemaSrc []byte) {
		fsys := fstest.MapFS{"s.sch": &fstest.MapFile{Data: schemaSrc}}
		_, _ = Load(context.Background(), fsys, "s.sch")
	})
}

// FuzzSchematronValidate asserts evaluation never panics on a crafted payload.
// The schema is held fixed and valid so every mutation lands on the document
// side, driving the XML parse, the pattern and rule walk, and the XPath
// rewriter that runs per assertion.
func FuzzSchematronValidate(f *testing.F) {
	f.Add([]byte(validCII))
	f.Add([]byte(validCII[:len(validCII)/2])) // Truncated.
	f.Add([]byte(`<rsm:ExchangedDocument/>`)) // Undeclared prefix.
	f.Add([]byte(`<a><b/></a>`))
	f.Add([]byte(""))

	schema, err := Load(context.Background(), fstest.MapFS{
		"s.sch": &fstest.MapFile{Data: []byte(en16931ish)},
	}, "s.sch")
	if err != nil {
		f.Fatalf("the fixed schema must load, else every exec returns early and the target is inert: %v", err)
	}

	f.Fuzz(func(_ *testing.T, payload []byte) {
		_, _, _ = Validate(context.Background(), schema, payload)
	})
}
