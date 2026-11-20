package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestGenerate_MatchesCommitted fails when the committed Go files are not
// what the generate step writes from the committed extract: run
// go run ./cmd/gen-locales after a change to the extract or the generator.
func TestGenerate_MatchesCommitted(t *testing.T) {
	root := filepath.Join("..", "..")
	data, err := os.ReadFile(filepath.Join(root, extractPath))
	if err != nil {
		t.Fatal(err)
	}
	table, models, err := generate(data)
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string][]byte{tablePath: table, modelsPath: models} {
		got, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s is not what the generator writes: run go run ./cmd/gen-locales", path)
		}
	}
}

// TestTranslate checks the translation of locale format codes into the
// SpreadsheetML syntax: keywords, separators, colours and the literals the
// syntax would read otherwise.
func TestTranslate(t *testing.T) {
	for _, tt := range []struct {
		tag, decimal, group, general, code, want string
	}{
		{"de-DE", ",", ".", "Standard", "#.##0,00 [$€-407];[ROT]-#.##0,00 [$€-407]", "#,##0.00 [$€-407];[RED]-#,##0.00 [$€-407]"},
		{"de-DE", ",", ".", "Standard", "NNNNT. MMMM JJJJ", `DDDD", "D. MMMM YYYY`},
		{"de-DE", ",", ".", "Standard", "[HH]:MM:SS,00", "[HH]:MM:SS.00"},
		{"fr-FR", ",", " ", "Standard", "JJ/MM/AAAA", "DD/MM/YYYY"},
		{"fr-FR", ",", " ", "Standard", "# ##0,00 €", "#,##0.00 €"},
		{"fi-FI", ",", " ", "Yleinen", "PP.KK.VVVV TT:MM", "DD.MM.YYYY HH:MM"},
		{"nl-NL", ",", ".", "Standaard", "DD-MM-JJJJ UU:MM", "DD-MM-YYYY HH:MM"},
		{"it-IT", ",", ".", "Standard", "OOOO GG MMMM AAAA", "DDDD DD MMMM YYYY"},
		{"en-US", ".", ",", "General", "NNNNMMMM D, YYYY", `DDDD", "MMMM D, YYYY`},
		{"en-US", ".", ",", "General", "0.00E+00", "0.00E+00"},
		{"de-CH", ".", "'", "Standard", "#'##0.00", "#,##0.00"},
		{"de-DE", ",", ".", "Standard", "Standard", "General"},
		{"sv-SE", ",", "\u00a0", "Standard", "[TT]:MM:SS,00", "[HH]:MM:SS.00"},
	} {
		sep := map[string]string{"de-DE": ", ", "en-US": ", "}[tt.tag]
		tr := translator{kw: keywordsFor(tt.tag), general: tt.general, decimal: tt.decimal, group: tt.group, time100: tt.decimal, dowSep: sep}
		got, err := tr.translate(tt.code)
		if err != nil || got != tt.want {
			t.Errorf("%s %q: %q %v, want %q", tt.tag, tt.code, got, err, tt.want)
		}
	}
	tr := translator{kw: keywordsFor("en-US"), decimal: ".", group: ","}
	if _, err := tr.translate("NNNNDD MMMM GGGE"); err == nil {
		t.Error("an era keyword translated")
	}
}

// TestCalcTables checks the reading of Calc's tables: macros expanded
// with their arguments, string literals joined, the three kinds of entry,
// and the resolution of a locale's table over its parents, an id reused
// taking the final format of the id it names.
func TestCalcTables(t *testing.T) {
	cxx := `
#define SYM "\"kr\""
#define NUMFMT_STRING( INDEX, FORMATCODE ) \
    { INDEX, u"" FORMATCODE ""_ustr, -1, -1 }
#define NUMFMT_PREDEF( INDEX, PREDEFINED ) \
    { INDEX, {}, css::i18n::NumberFormatIndex::PREDEFINED, -1 }
#define NUMFMT_REUSE( INDEX, REUSED_INDEX ) \
    { INDEX, {}, -1, REUSED_INDEX }
#define TWO( INDEX, CODE ) NUMFMT_STRING( INDEX + 0, CODE ), NUMFMT_STRING( INDEX + 1, CODE ";" CODE )
const BuiltinFormat spBuiltinFormats_BASE[] =
{
    NUMFMT_PREDEF( 14, DATE_SYS_DDMMYYYY ), // a comment
    NUMFMT_REUSE( 27, 14 ),
    NUMFMT_STRING( 45, "mm:ss" ),
};
/** A locale. */
const BuiltinFormat spBuiltinFormats_xx_XX[] =
{
    NUMFMT_STRING( 14, "D.M.YYYY" ),
    TWO( 5, SYM " #,##0" ),
    NUMFMT_STRING( 46, "?\?/?\?" ),
};
constexpr BuiltinFormatTable spBuiltinFormatTables[] =
{
    { u"*"_ustr,     u""_ustr,  spBuiltinFormats_BASE },
    { u"xx-XX"_ustr, u"*"_ustr, spBuiltinFormats_xx_XX },
};
int f() { return '"'; }
`
	idl := "const short DATE_START = 18; const short DATE_SYS_DDMMYYYY = DATE_START+3;"
	tables, err := calcTables(cxx, idl)
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) != 2 || tables[1].Locale != "xx-XX" || tables[1].Parent != "*" {
		t.Fatalf("tables %+v", tables)
	}
	got := tables[1].Formats
	if len(got) != 4 || got[1].ID != 5 || got[1].Code != `"kr" #,##0` || got[2].ID != 6 || got[2].Code != `"kr" #,##0;"kr" #,##0` || got[3].Code != "??/??" {
		t.Errorf("xx-XX formats %+v", got)
	}
	if f := tables[0].Formats[0]; f.Predef == nil || *f.Predef != 21 {
		t.Errorf("base 14 %+v", f)
	}
	cb := &calcBuiltins{tables: map[string]calcTable{}}
	for _, tb := range tables {
		cb.tables[tb.Locale] = tb
	}
	res, err := cb.resolve("xx-XX")
	if err != nil {
		t.Fatal(err)
	}
	if res[27].Code != "D.M.YYYY" || res[45].Code != "mm:ss" {
		t.Errorf("xx-XX resolves 27 to %+v, 45 to %+v", res[27], res[45])
	}
	if res, err = cb.resolve("yy-YY"); err != nil || res[27].Predef == nil || *res[27].Predef != 21 {
		t.Errorf("a locale without a table resolves 27 to %+v, %v", res[27], err)
	}
}
