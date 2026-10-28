package glyph

import (
	"context"
	"log/slog"
	"sync"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/intconv"
	"github.com/gotenberg/litho/pkg/internal/typography/cff"
	"github.com/gotenberg/litho/pkg/internal/typography/encodings"
	"github.com/gotenberg/litho/pkg/internal/typography/type1"
)

// nameTable maps each single-byte code to a glyph name, "" for none.
type nameTable = [256]string

// Predefined encodings of ISO 32000-1, Annex D, Table D.1, by the name an
// /Encoding or /BaseEncoding entry gives them (9.6.6.1, Table 114).
const (
	encStandard  = "StandardEncoding"
	encWinAnsi   = "WinAnsiEncoding"
	encMacRoman  = "MacRomanEncoding"
	encMacExpert = "MacExpertEncoding"
)

// predefinedNames builds the code-to-name tables of the predefined encodings
// once. The encodings package keeps WinAnsi, MacRoman and MacExpert as
// code-to-character tables; each character is named back by the Adobe Glyph
// List, which is how Annex D, Table D.2 names it.
var predefinedNames = sync.OnceValue(func() map[string]*nameTable {
	fromRunes := func(m map[byte]rune) *nameTable {
		var t nameTable
		for code, r := range m {
			t[code] = encodings.RuneToGlyph(r)
		}
		return &t
	}
	standard := encodings.StandardEncodingNames
	symbol := encodings.SymbolSetEncoding
	dingbats := encodings.ZapfDingbatsEncoding
	return map[string]*nameTable{
		encStandard:  &standard,
		encWinAnsi:   fromRunes(encodings.WinAnsiEncoding),
		encMacRoman:  fromRunes(encodings.MacRomanEncoding),
		encMacExpert: fromRunes(encodings.MacExpertEncoding),
		// The built-in encodings of the Symbol and ZapfDingbats standard
		// fonts (Annex D.5 and D.6), which 9.6.6.1 gives a font that names
		// no encoding of its own.
		"Symbol":       &symbol,
		"ZapfDingbats": &dingbats,
	}
})

// macRomanCodes maps a glyph name to its code in the Mac OS standard roman
// encoding, the step ISO 32000-1, 9.6.6.4 takes before a (1,0) cmap lookup.
var macRomanCodes = sync.OnceValue(func() map[string]byte {
	out := make(map[string]byte, 256)
	t := predefinedNames()[encMacRoman]
	for code, name := range t {
		if name != "" {
			out[name] = byte(code) //nolint:gosec // code indexes a 256-entry table.
		}
	}
	return out
})

// simpleEncoding is a simple font's code-to-name mapping (ISO 32000-1,
// 9.6.6.1) and whether the font states an /Encoding at all, which ISO
// 32000-1, 9.6.6.4 reads to choose a TrueType font's cmap route.
type simpleEncoding struct {
	names nameTable
	// stated reports an /Encoding entry that names an encoding or holds a
	// dictionary.
	stated bool
	// differences reports a /Differences array.
	differences bool
}

// resolveSimpleEncoding returns the glyph names a simple font's codes select:
// the /Encoding entry's predefined encoding, or its dictionary's
// /BaseEncoding with /Differences applied, over the implicit base ISO
// 32000-1, 9.6.6.1, Table 114 gives a dictionary without one. builtin is the
// font's built-in encoding, from its embedded program or, for the standard
// Symbol and ZapfDingbats fonts, Annex D; nil when it has none litho reads.
func resolveSimpleEncoding(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, builtin *nameTable, implicit string) simpleEncoding {
	var enc simpleEncoding
	base := builtin
	if base == nil {
		base = predefinedNames()[implicit]
	}
	encObj, ok := fontDict["Encoding"]
	if !ok {
		if base != nil {
			enc.names = *base
		}
		return enc
	}
	resolved, err := document.ResolveObject(ctx, doc, encObj)
	if err != nil {
		doc.Logger.Debug("glyph: resolving /Encoding", slog.Any("err", err))
	}
	switch v := resolved.(type) {
	case ast.PdfName:
		enc.stated = true
		if t, ok := predefinedNames()[string(v)]; ok && isPredefinedEncodingName(string(v)) {
			enc.names = *t
		} else if base != nil {
			// A name outside Table D.1 names no encoding; the font keeps
			// its base, as PDF.js reads it (evaluator.js,
			// extractDataStructures, which ignores an unknown name).
			enc.names = *base
		}
	case ast.PdfDict:
		enc.stated = true
		if bName, err := document.ResolveName(ctx, doc, v["BaseEncoding"]); err == nil && isPredefinedEncodingName(string(bName)) {
			enc.names = *predefinedNames()[string(bName)]
		} else if base != nil {
			enc.names = *base
		}
		if diffs, err := document.ResolveArray(ctx, doc, v["Differences"]); err == nil && diffs != nil {
			enc.differences = true
			applyDifferences(ctx, doc, &enc.names, diffs)
		}
	default:
		if base != nil {
			enc.names = *base
		}
	}
	return enc
}

// isPredefinedEncodingName reports one of the four encodings an /Encoding or
// /BaseEncoding entry may name (ISO 32000-1, 9.6.6.1, Table 114).
func isPredefinedEncodingName(name string) bool {
	switch name {
	case encStandard, encWinAnsi, encMacRoman, encMacExpert:
		return true
	}
	return false
}

// applyDifferences writes a /Differences array over names: each integer
// starts a run of codes the names after it fill in turn (ISO 32000-1,
// 9.6.6.1). A code past 255 names nothing, and an element that is neither an
// integer nor a name is skipped, as PDF.js skips it.
func applyDifferences(ctx context.Context, doc *document.Document, names *nameTable, diffs ast.PdfArray) {
	code := -1
	for _, el := range diffs {
		resolved, err := document.ResolveObject(ctx, doc, el)
		if err != nil {
			continue
		}
		switch v := resolved.(type) {
		case ast.PdfInt:
			code = intconv.Clamp(v)
		case ast.PdfReal:
			code = int(v)
		case ast.PdfName:
			if code >= 0 && code < len(names) {
				names[code] = string(v)
			}
			if code >= 0 {
				code++
			}
		}
	}
}

// type1Builtin returns the built-in encoding of a Type 1 program, its
// /Encoding vector (Adobe Type 1 Font Format, 2.3), or nil when it names a
// predefined one litho holds no table for. A program that names
// StandardEncoding uses Annex D's table.
func type1Builtin(data []byte) *nameTable {
	vec := type1.ParseEncodingPS(data)
	if vec[0] != "" && vec[0] != notdefName && vec[0] == vec[255] && vec[0] == vec[65] {
		// ParseEncodingPS repeats the name of a predefined encoding across
		// the vector.
		if t, ok := predefinedNames()[vec[0]]; ok {
			return t
		}
		return nil
	}
	for _, n := range vec {
		if n != "" {
			return &vec
		}
	}
	return nil
}

// cffBuiltin returns the built-in encoding of a name-keyed CFF program (Adobe
// Technical Note #5176, section 12): its predefined Standard or Expert
// encoding in full, or the custom encoding it carries.
func cffBuiltin(data []byte) *nameTable {
	m := cff.PredefinedEncodingNames(data)
	if m == nil {
		m = cff.ExtractEncoding(data)
	}
	if len(m) == 0 {
		return nil
	}
	var t nameTable
	for code, name := range m {
		t[code] = name
	}
	return &t
}
