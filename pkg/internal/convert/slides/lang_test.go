package slides

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// TestConvert_LanguageMostRunsState locks the language of a deck whose
// metadata and default text style state none: the one most of its runs
// state, by characters (statedLang), which the document declares; a run
// in it takes no Span, a run in another takes a Span stating its own
// (ISO 14289-1, 7.2).
//
// # Corpus evidence
//
//   - probe, pptx: the 500-slide cost deck states en-US on every run and
//     no deck language; each run took a Span stating en-US, 6.5 % more
//     bytes and 1.3 % more allocations than without.
func TestConvert_LanguageMostRunsState(t *testing.T) {
	en, fr := arial(20), arial(20)
	en.Lang, fr.Lang = "en-US", "fr-FR"
	d := deck(&Slide{Shapes: []*Shape{
		box(50, 50, 600, 100, para("A sentence in English, long enough to win.", en)),
		box(50, 200, 600, 100, para("Bonjour", fr)),
	}})
	out := convert(t, d, office.Options{DefaultLang: "de-DE"})
	if out.Metadata.Lang != "en-US" || out.Structure.Lang != "en-US" {
		t.Fatalf("document language %q, structure %q, want en-US", out.Metadata.Lang, out.Structure.Lang)
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
