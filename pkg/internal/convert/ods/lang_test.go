package ods

import "testing"

// TestRead_LanguageOfTheDefaultCellStyle locks the language of a
// spreadsheet whose metadata names none: its default cell style's
// fo:language and fo:country (ODF 1.3, Part 3, 20.204 and 20.211), the
// language every cell's text takes unless its own style says otherwise;
// the metadata's dc:language when it names one, and none for zxx.
//
// # Corpus evidence
//
//   - libreoffice/ods: 226 spreadsheets state a language in their default
//     cell style and none in their metadata; their conversions left all
//     their text in no language (ISO 14289-1, 7.2).
func TestRead_LanguageOfTheDefaultCellStyle(t *testing.T) {
	body := `<table:table table:name="S"><table:table-row><table:table-cell office:value-type="string"><text:p>x</text:p></table:table-cell></table:table-row></table:table>`
	def := func(lang string) string {
		return `<style:default-style style:family="table-cell"><style:text-properties ` + lang + `/></style:default-style>`
	}
	for _, tc := range []struct{ name, styles, meta, want string }{
		{"default cell style", def(`fo:language="de" fo:country="DE"`), "", "de-DE"},
		{"a language alone", def(`fo:language="fr"`), "", "fr"},
		{"the metadata's", def(`fo:language="de" fo:country="DE"`), `<dc:language>en-GB</dc:language>`, "en-GB"},
		{"no language", def(`fo:language="zxx" fo:country="none"`), "", ""},
	} {
		doc, _ := read(t, flat{styles: tc.styles, meta: tc.meta, body: body}.bytes())
		if doc.Meta.Lang != tc.want {
			t.Errorf("%s: language %q, want %q", tc.name, doc.Meta.Lang, tc.want)
		}
	}
}
