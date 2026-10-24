package models

// UnseenReason says why a run of text a page draws shows nowhere on it.
type UnseenReason string

const (
	// UnseenRenderMode is text in rendering mode 3 or 7, which neither fills
	// nor strokes its glyphs (ISO 32000-1, 9.3.6, Table 106): the mode an OCR
	// layer over a scan uses.
	UnseenRenderMode UnseenReason = "render-mode"
	// UnseenOutsidePage is text whose glyphs start outside the page's crop
	// box, the region a viewer shows (14.11.2).
	UnseenOutsidePage UnseenReason = "outside-page"
	// UnseenPaintsNothing is text that marks no pixel even drawn alone on a
	// white page: glyphs that describe no shape, text a clipping path
	// removes, text fully transparent, or text in white.
	UnseenPaintsNothing UnseenReason = "paints-nothing"
	// UnseenCovered is text that marks pixels drawn alone and none on the
	// page: content painted after it covers it, or it is painted in the
	// colour of what lies under it.
	UnseenCovered UnseenReason = "covered"
)

// UnseenText is a run of text a page draws that shows nowhere on it.
// Values must not be shared across goroutines without synchronization.
type UnseenText struct {
	// Text is the run's text as text extraction reads it.
	Text string `json:"text"`
	// BBox is the box [minX minY maxX maxY], in default user space, holding
	// the ink boxes of the run's glyphs (see [TextElement.InkBox]).
	BBox [4]float64 `json:"bbox"`
	// FontSize is the size of the run's first glyph as it is drawn.
	FontSize float64 `json:"fontSize"`
	// Reason says why the run shows nowhere.
	Reason UnseenReason `json:"reason"`
}
