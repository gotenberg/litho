package odt

import (
	"bytes"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// TestConvert_LanguageMostTextStates locks the language of a document
// whose default style states none: the one most of its text states
// (layout.StatedLang) before the caller's default, so that only text in
// another language takes a Span (ISO 14289-1, 7.2).
func TestConvert_LanguageMostTextStates(t *testing.T) {
	auto := `<style:style style:name="NL" style:family="paragraph"><style:text-properties fo:language="nl" fo:country="NL"/></style:style>` +
		`<style:style style:name="FR" style:family="text"><style:text-properties fo:language="fr" fo:country="FR"/></style:style>`
	body := `<text:p text:style-name="NL">Een lange zin in het Nederlands. <text:span text:style-name="FR">Bonjour</text:span></text:p>`
	// The test document's default style states en-US: strip it.
	src := bytes.Replace(doc{contentAuto: auto, body: body}.flat(), []byte(` fo:language="en" fo:country="US"`), nil, 1)
	res := convertDoc(t, src, office.Options{DefaultLang: "de-DE"})
	if res.Metadata.Lang != "nl-NL" || res.Structure.Lang != "nl-NL" {
		t.Fatalf("document language %q, structure %q, want nl-NL", res.Metadata.Lang, res.Structure.Lang)
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
	walk(res.Structure)
	if len(spans) != 1 || spans[0] != "fr-FR" {
		t.Errorf("language Spans %q, want the French run's alone", spans)
	}
}
