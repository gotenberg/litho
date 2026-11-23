package odp

import "testing"

// TestRead_LanguageOfTheDefaultGraphicStyle locks the language of a
// presentation whose metadata names none: its default graphic style's
// fo:language and fo:country (ODF 1.3, Part 3, 20.204 and 20.211), the
// language the text of every shape takes unless its own style says
// otherwise; the metadata's dc:language when it names one.
//
// # Corpus evidence
//
//   - libreoffice/odp: 74 presentations state a language in their default
//     graphic style and none in their metadata; their conversions left
//     the text their styles do not give a language in none (ISO 14289-1,
//     7.2).
func TestRead_LanguageOfTheDefaultGraphicStyle(t *testing.T) {
	def := `<style:default-style style:family="graphic"><style:text-properties fo:language="pt" fo:country="BR"/></style:default-style>`
	for _, tc := range []struct{ name, styles, meta, want string }{
		{"default graphic style", def, "", "pt-BR"},
		{"the metadata's", def, `<dc:language>en-GB</dc:language>`, "en-GB"},
		{"none", "", "", ""},
	} {
		doc, _ := read(t, flat{styles: tc.styles, meta: tc.meta, body: page("")}.bytes())
		if doc.Meta.Lang != tc.want {
			t.Errorf("%s: language %q, want %q", tc.name, doc.Meta.Lang, tc.want)
		}
	}
}
