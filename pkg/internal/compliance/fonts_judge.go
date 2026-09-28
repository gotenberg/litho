package compliance

import (
	"context"
	"maps"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/typography/encodings"
	"github.com/gotenberg/litho/pkg/models"
)

// judgedFontName is the resource name [JudgeFontShowing] selects the judged
// font through.
const judgedFontName = "F"

// JudgeFontShowing returns what the font rules of profile state against font,
// a font dictionary that need not be an object of doc, when a content stream
// shows each of shown with it visibly (rendering mode 0). The judgement is the
// one validation makes of a font a form XObject shows ([checkResourceFonts]):
// the rules of the font dictionary and of its descendant CIDFont, and the
// rules of every glyph the strings show. Objects font reaches by reference are
// read from doc, and the ones it holds directly are read as they are, so a
// repair can judge a font it has not written yet.
//
// A WTPDF level is judged as the PDF/UA-2 rules it adopts (see
// [ValidateWTPDF]).
func JudgeFontShowing(ctx context.Context, doc *document.Document, font ast.PdfDict, shown []ast.PdfString, profile string) []Violation {
	ops := make([]content.Operation, 0, len(shown)+3)
	ops = append(ops,
		content.Operation{Operator: "BT"},
		content.Operation{Operator: "Tf", Operands: []ast.PdfObject{ast.PdfName(judgedFontName), ast.PdfInt(1)}},
	)
	for _, s := range shown {
		ops = append(ops, content.Operation{Operator: "Tj", Operands: []ast.PdfObject{s}})
	}
	ops = append(ops, content.Operation{Operator: "ET"})
	res := ast.PdfDict{"Font": ast.PdfDict{judgedFontName: font}}
	return checkResourceFonts(ctx, doc, res, ops, showSite{}, 1, judgedProfile(profile), newFontJudgements())
}

// judgedProfile maps a WTPDF level onto the PDF/UA-2 identifier its checks run
// under.
func judgedProfile(profile string) string {
	if IsWTPDF(profile) {
		return string(models.PDFUA2)
	}
	return profile
}

// FontEmbeddingRule returns the rule requiring an embedded font program under
// profile: ISO 19005-1, 6.3.4 t1, ISO 19005-2 and 19005-3, 6.2.11.4.1 t1, ISO
// 19005-4, 6.2.10.4.1 t1, ISO 14289-1, 7.21.4.1 t1 or ISO 14289-2, 8.4.5.5.1
// t1. It is the zero Rule for an identifier naming none of them.
func FontEmbeddingRule(profile string) Rule {
	return fontEmbeddingRule(judgedProfile(profile))
}

// GlyphPresenceRule returns the rule requiring the embedded program to define
// every glyph shown ([glyphPresenceRule]).
func GlyphPresenceRule(profile string) Rule {
	return glyphPresenceRule(judgedProfile(profile))
}

// GlyphWidthRule returns the rule requiring the declared widths to agree with
// the embedded program's advances ([glyphWidthRule]).
func GlyphWidthRule(profile string) Rule {
	return glyphWidthRule(judgedProfile(profile))
}

// ShownStrings returns, per font resource name, the string operands the text
// operators of ops show, in order, as [ShownCharCodes] reads their codes.
func ShownStrings(ops []content.Operation) map[string][]ast.PdfString {
	usage := collectFontUsage(ops)
	out := make(map[string][]ast.PdfString, len(usage))
	for name, u := range usage {
		if len(u.rawStrings) > 0 {
			out[name] = u.rawStrings
		}
	}
	return out
}

// PredefinedEncodingNames returns the glyph name each code of a predefined
// simple-font encoding selects (ISO 32000-1, Annex D): WinAnsiEncoding,
// MacRomanEncoding, MacExpertEncoding or StandardEncoding. It is nil for any
// other name.
func PredefinedEncodingNames(name string) map[byte]string {
	return predefinedEncodingNames(name)
}

// UnembeddedFontGlyphNames returns the glyph name each code of a simple font
// selects while its program is not embedded, which is the name a conforming
// reader looks up in the font it substitutes (ISO 32000-1, 9.6.6.1 and 9.6.6.2).
// A predefined /Encoding names every code; an encoding dictionary starts from
// its /BaseEncoding or, without one, from the implicit base Table 114 gives
// the font, and /Differences overrides it. With no /Encoding the implicit base
// stands alone. The implicit base of the Symbol and ZapfDingbats standard
// fonts is their built-in encoding (ISO 32000-1, D.5 and D.6), and that of
// any other font is StandardEncoding, the built-in encoding of the Latin
// standard fonts, unless the descriptor flags the font symbolic: a symbolic
// font's built-in encoding lives in the program the file does not carry, so
// the result is nil and the codes are unknown.
func UnembeddedFontGlyphNames(ctx context.Context, doc *document.Document, font ast.PdfDict) map[byte]string {
	base := implicitUnembeddedEncoding(ctx, doc, font)
	encObj, ok := font["Encoding"]
	if !ok {
		return base
	}
	if name, err := document.ResolveName(ctx, doc, encObj); err == nil && name != "" {
		if names := predefinedEncodingNames(string(name)); len(names) > 0 {
			return names
		}
		// A name no predefined encoding carries states nothing (Table 114).
		return base
	}
	encDict, err := document.ResolveDict(ctx, doc, encObj)
	if err != nil || encDict == nil {
		return base
	}
	result := make(map[byte]string)
	if baseName, err := document.ResolveName(ctx, doc, encDict["BaseEncoding"]); err == nil && predefinedEncodingNames(string(baseName)) != nil {
		maps.Copy(result, predefinedEncodingNames(string(baseName)))
	} else {
		maps.Copy(result, base)
	}
	maps.Copy(result, differencesGlyphNames(ctx, doc, encDict))
	if len(result) == 0 {
		return nil
	}
	return result
}

// implicitUnembeddedEncoding returns the implicit base encoding of an
// unembedded simple font; see [UnembeddedFontGlyphNames].
func implicitUnembeddedEncoding(ctx context.Context, doc *document.Document, font ast.PdfDict) map[byte]string {
	base, _ := document.ResolveName(ctx, doc, font["BaseFont"])
	name := string(base)
	if _, suffix, ok := strings.Cut(name, "+"); ok {
		name = suffix
	}
	switch name {
	case "Symbol":
		return tableNames(&encodings.SymbolSetEncoding)
	case "ZapfDingbats":
		return tableNames(&encodings.ZapfDingbatsEncoding)
	}
	if isSymbolicFont(ctx, doc, document.ResolveFontDescriptor(ctx, doc, font)) {
		return nil
	}
	return predefinedEncodingNames("StandardEncoding")
}

// tableNames returns the named codes of a 256-entry encoding vector.
func tableNames(table *[256]string) map[byte]string {
	out := make(map[byte]string)
	for code, name := range table {
		if name != "" {
			out[byte(code)] = name //nolint:gosec // G115: code indexes a 256-entry array.
		}
	}
	return out
}

// Type0ShownCIDs returns the CIDs the strings select through a Type0 font's
// /Encoding (a predefined CMap, Identity-H or Identity-V, or an embedded CMap
// stream), as the glyph rules decode them. It is nil when the encoding cannot
// be decoded or selects nothing.
func Type0ShownCIDs(ctx context.Context, doc *document.Document, font ast.PdfDict, shown []ast.PdfString) map[uint16]bool {
	return type0UsedCIDs(ctx, doc, font, &fontUsageInfo{visible: true, rawStrings: shown})
}

// DeclaredCIDWidth returns the width a CIDFont dictionary declares for cid,
// from /W or else /DW (ISO 32000-1, 9.7.4.3), in glyph space units.
func DeclaredCIDWidth(ctx context.Context, doc *document.Document, cidFont ast.PdfDict, cid uint16) float64 {
	return lookupCIDWidth(ctx, doc, cidFont, cid)
}

// WidthsConsistent reports whether a declared width and a program advance,
// both in glyph space units, agree as the glyph width rules require: within
// one unit ([widthTolerance]).
func WidthsConsistent(declared, program float64) bool {
	return !widthsInconsistent(declared, program)
}
