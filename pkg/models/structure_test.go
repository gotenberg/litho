package models

import "testing"

func TestPronunciationLexicon_JSONRoundTrip(t *testing.T) {
	assertRoundTrip(t, PronunciationLexicon{XML: "<lexicon/>"})
}

func TestFormulaElement_JSONRoundTrip(t *testing.T) {
	assertRoundTrip(t, FormulaElement{
		MathML:  "<mrow><mi>x</mi></mrow>",
		Content: []MarkedContentRef{{Page: 1, MCID: 2}, {Page: 3, MCID: 0}},
	})
}
