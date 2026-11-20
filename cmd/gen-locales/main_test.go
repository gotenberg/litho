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
	} {
		sep := map[string]string{"de-DE": ", ", "en-US": ", "}[tt.tag]
		tr := translator{kw: keywordsFor(tt.tag), general: tt.general, decimal: tt.decimal, group: tt.group, dowSep: sep}
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
