package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestSymbolicTrueTypeEncodingEntry pins which /Encoding entries of a symbolic
// TrueType font fail ISO 14289-1, 7.21.6 t3 and its restatements (ISO
// 19005-1, 6.3.7 t2; ISO 19005-2 and -3, 6.2.11.6 t3; ISO 19005-4, 6.2.10.6
// t3; ISO 14289-2, 8.4.5.7 t3).
//
// veraPDF tests "Encoding == null", where Encoding is a name /Encoding names,
// or the /BaseEncoding of an encoding dictionary (GFPDSimpleFont.getEncoding),
// so a dictionary that only lists /Differences passes. Measured on veraPDF
// 1.30.2 at all five profiles, embedded and not, on a file drawing one font
// per case. An /Encoding that is neither a name, a dictionary nor null stops
// veraPDF with a NullPointerException, so no verdict stands against stating
// the rule.
func TestSymbolicTrueTypeEncodingEntry(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	for _, tc := range []struct {
		name     string
		encoding string
		extra    string
		fails    bool
	}{
		{name: "no entry"},
		{name: "a name", encoding: "/Encoding /WinAnsiEncoding", fails: true},
		{name: "differences only", encoding: "/Encoding << /Type /Encoding /Differences [65 /A] >>"},
		{name: "differences only, indirect", encoding: "/Encoding 5 0 R", extra: "<< /Differences [65 /A] >>"},
		{name: "empty dictionary", encoding: "/Encoding << >>"},
		{name: "base encoding and differences", encoding: "/Encoding << /BaseEncoding /WinAnsiEncoding /Differences [65 /A] >>", fails: true},
		{name: "base encoding written as a string", encoding: "/Encoding << /BaseEncoding (WinAnsiEncoding) >>", fails: true},
		{name: "base encoding that names nothing", encoding: "/Encoding << /BaseEncoding 7 /Differences [65 /A] >>"},
		{name: "null", encoding: "/Encoding null"},
		{name: "neither a name nor a dictionary", encoding: "/Encoding 5", fails: true},
	} {
		objs := []string{
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [] /Count 0 >>",
			"<< /Type /Font /Subtype /TrueType /BaseFont /Sym /FirstChar 65 /LastChar 65 /Widths [722] /FontDescriptor 4 0 R " + tc.encoding + " >>",
			"<< /Type /FontDescriptor /FontName /Sym /Flags 4 /ItalicAngle 0 /Ascent 718 /Descent -207 /CapHeight 718 /StemV 88 /FontBBox [0 0 1000 1000] >>",
		}
		if tc.extra != "" {
			objs = append(objs, tc.extra)
		}
		doc, _ := objsDocPDF(t, objs...)
		font, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 3})
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		for _, profile := range []string{"PDF/A-1b", "PDF/A-2b", "PDF/A-4", "PDF/UA-1", "PDF/UA-2"} {
			rule := trueTypeEncodingSymbolicNoEncodingRule(profile)
			got := hasRule(checkTrueTypeEncoding(ctx, doc, font, "Sym", 1, profile), rule)
			if got != tc.fails {
				t.Errorf("%s, %s: %s t%d fired = %v, want %v", tc.name, profile, rule.Clause, rule.TestNumber, got, tc.fails)
			}
		}
	}
}
