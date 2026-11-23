package docx

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// TestConvert_LanguageMostRunsState locks the language of a document
// whose styles and settings state none: the one most of its runs state
// (layout.StatedLang) before the caller's default, so that only a run in
// another language takes a Span (ISO 14289-1, 7.2).
func TestConvert_LanguageMostRunsState(t *testing.T) {
	run := func(lang, s string) string {
		return `<w:r><w:rPr><w:lang w:val="` + lang + `"/></w:rPr><w:t xml:space="preserve">` + s + `</w:t></w:r>`
	}
	body := `<w:p>` + run("nl-NL", "Een lange zin in het Nederlands. ") + run("fr-FR", "Bonjour") + `</w:p>`
	out := convertBytes(t, docx(t, body, nil), office.Options{DefaultLang: "de-DE"})
	if out.Metadata.Lang != "nl-NL" || out.Structure.Lang != "nl-NL" {
		t.Fatalf("document language %q, structure %q, want nl-NL", out.Metadata.Lang, out.Structure.Lang)
	}
	var spans []string
	var walk func(*typeset.StructElem)
	walk = func(e *typeset.StructElem) {
		if e.Role == "Span" && e.Lang != "" {
			spans = append(spans, e.Lang)
		}
		for _, k := range e.Kids {
			walk(k)
		}
	}
	walk(out.Structure)
	if len(spans) != 1 || spans[0] != "fr-FR" {
		t.Errorf("language Spans %q, want the French run's alone", spans)
	}
}
