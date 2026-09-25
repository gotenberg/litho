package xsd

import (
	"context"
	"errors"
	"regexp"
	"regexp/syntax"
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

// FuzzNewDecoder asserts the decoder [NewDecoder] builds never panics while
// it sniffs a byte order mark, transcodes UTF-16 and switches to a declared
// encoding. The seeds reach each of the three.
func FuzzNewDecoder(f *testing.F) {
	latin1 := `<?xml version="1.0" encoding="ISO-8859-1"?><a b="` + "\xE9" + `">` + "\xFF" + `</a>`
	f.Add([]byte(latin1))
	f.Add([]byte(latin1[:len(latin1)/2])) // Truncated inside the element.
	f.Add([]byte("\xFF\xFE<\x00?\x00x\x00m\x00l\x00 \x00"))
	f.Add([]byte("\xFE\xFF\x00<\x00a"))
	f.Add([]byte(`<?xml version="1.0" encoding="UTF-8-W/O-BOM"?><a/>`))

	f.Fuzz(func(_ *testing.T, payload []byte) {
		dec := NewDecoder(payload)
		for {
			if _, err := dec.Token(); err != nil {
				return
			}
		}
	})
}

// FuzzXSDLoadThenValidate drives a FUZZED schema through validation, which
// neither target above can do: FuzzXSDLoad stops at Load, and Load cannot reach
// the facet walk (collectAllFacets has exactly two callers, ValidateValue and
// itself, and ValidateValue runs only from Validate). FuzzXSDValidate holds the
// schema fixed and acyclic.
//
// The comment on FuzzXSDValidate reasons that fuzzing both halves "would mostly
// produce schemas that fail to load and leave the validator unreached". That is
// true of malformed schemas and exactly wrong for the dangerous ones: a
// simpleType restricting itself LOADS without error, and the validator then
// walked its base chain until the goroutine stack passed 1 GB and the process
// died. Loading cleanly is what made it dangerous.
//
// The cyclic schemas are seeded rather than left to mutation because a valid
// XSD carrying a self-referential base is not a shape byte mutation discovers.
func FuzzXSDLoadThenValidate(f *testing.F) {
	const payload = `<Root xmlns="urn:t">far too long to pass</Root>`

	f.Add([]byte(minimalXSD), []byte(validInstance))
	f.Add([]byte(selfRestrictingXSD), []byte(payload))     // Loads, then used to overflow.
	f.Add([]byte(mutuallyRestrictingXSD), []byte(payload)) // A two-step cycle.
	f.Add([]byte(mutuallyExtendingSimpleContentXSD), []byte(payload))
	f.Add([]byte(simpleContentXSD), []byte(`<Invoice xmlns="urn:t"><Small scale="x">1,5</Small></Invoice>`))
	f.Add([]byte(leafTextXSD), []byte(`<Invoice xmlns="urn:t" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"><Party xsi:nil="true"><Name/></Party></Invoice>`))
	f.Add([]byte(minimalXSD[:len(minimalXSD)/2]), []byte(payload))
	f.Add([]byte(""), []byte(""))

	f.Fuzz(func(_ *testing.T, schemaSrc, doc []byte) {
		if len(schemaSrc) > 1<<20 || len(doc) > 1<<20 {
			return
		}
		schema, err := Load(context.Background(), fstest.MapFS{
			"schema.xsd": &fstest.MapFile{Data: schemaSrc},
		}, "schema.xsd")
		if err != nil || schema == nil {
			return
		}
		_, _ = Validate(context.Background(), schema, doc)
	})
}

// FuzzTranslatePattern asserts the Appendix F translator never panics and
// never hands the compiler a malformed expression. A pattern is read from
// the schema, which FuzzXSDLoadThenValidate mutates as a whole; aiming at
// the translator alone reaches its grammar far faster.
func FuzzTranslatePattern(f *testing.F) {
	f.Add(`[a-z-[aeiou]]{2}(\d+|\p{Lu}.)?`)
	f.Add(`[a-z-[aeiou`) // Truncated inside the subtracted class.
	f.Add(`[^\w-[\s]]*\P{Cn}a{02,3}`)
	f.Add(`\i\c*[\I-[\p{IsBasicLatin}]]\P{IsGreek}[\C\p{IsSpecials}]`)
	f.Add(`((a|b)*`)
	f.Add(`\`)

	f.Fuzz(func(t *testing.T, pattern string) {
		if len(pattern) > 1<<12 {
			return
		}
		translated, err := translatePattern(pattern)
		if err != nil {
			return
		}
		// The only compile failures a translation may leave are Go's own
		// limits, which a valid Appendix F pattern can exceed: the 1000
		// repetitions of a count, the size of the compiled program, and
		// its nesting.
		_, err = regexp.Compile("^(?:" + translated + ")$")
		if err == nil {
			return
		}
		if se, ok := errors.AsType[*syntax.Error](err); ok {
			switch se.Code {
			case syntax.ErrInvalidRepeatSize, syntax.ErrLarge, syntax.ErrNestingDepth:
				return
			}
		}
		t.Fatalf("translatePattern(%q) = %q, which does not compile: %v", pattern, translated, err)
	})
}
