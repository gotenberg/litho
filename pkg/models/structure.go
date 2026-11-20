package models

// PronunciationLexicon holds a W3C PLS (Pronunciation Lexicon Specification)
// XML document extracted from the catalog's /PronunciationLexicon array.
// Concurrent reads of a value are safe; a write needs synchronization.
type PronunciationLexicon struct {
	// XML is the lexicon document text.
	XML string `json:"xml"`
}

// FormulaElement represents a mathematical formula extracted from the
// structure tree. It carries the MathML markup from the /AF associated file.
// Concurrent reads of a value are safe; a write needs synchronization.
type FormulaElement struct {
	// MathML is the MathML markup of the formula.
	MathML string `json:"mathML"`
	// Content lists the marked-content sequences that draw the formula, in
	// structure tree order. Match each against the [TextElement] values of
	// its page whose MCID equals its MCID.
	Content []MarkedContentRef `json:"content,omitempty"`
}

// MarkedContentRef names one marked-content sequence of a page: its
// marked-content identifier, unique within the page's content (ISO 32000-1,
// 14.7.4.2), and the page that holds it.
// Concurrent reads of a value are safe; a write needs synchronization.
type MarkedContentRef struct {
	// Page is the 1-based page whose content holds the sequence. It is 0 when
	// the structure tree names no page of the document for it.
	Page int `json:"page"`
	// MCID is the marked-content identifier, as [TextElement.MCID] reports it.
	MCID int `json:"mcid"`
}
