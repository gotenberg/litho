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

// TextContrast is a run of text whose contrast against what the page shows
// under it falls short of the ratio WCAG 2.x requires of it, as
// pdf.Reader.CheckTextContrast measures it.
// Values must not be shared across goroutines without synchronization.
type TextContrast struct {
	// Text is the run's text as text extraction reads it.
	Text string `json:"text"`
	// BBox is the box [minX minY maxX maxY], in default user space, holding
	// the ink boxes of the run's glyphs (see [TextElement.InkBox]).
	BBox [4]float64 `json:"bbox"`
	// FontSize is the size, in points, the run is drawn at, read through the
	// text matrix and the CTM.
	FontSize float64 `json:"fontSize"`
	// Bold reports a bold font, by its descriptor or its name.
	Bold bool `json:"bold,omitempty"`
	// Large reports large-scale text as WCAG 2.x defines it: at least 18
	// points, or 14 points and bold.
	Large bool `json:"large,omitempty"`
	// Ratio is the measured contrast ratio, (L1 + 0.05) / (L2 + 0.05) of the
	// relative luminances of the lighter and the darker colour, and Required
	// the ratio the run falls short of.
	Ratio    float64 `json:"ratio"`
	Required float64 `json:"required"`
	// TextColor is the colour the content stream paints the run in, and
	// BackgroundColor the mean colour the page shows under it, both sRGB.
	TextColor       [3]uint8 `json:"textColor"`
	BackgroundColor [3]uint8 `json:"backgroundColor"`
	// BackgroundVaries reports a background whose luminance spreads widely
	// under the run, an image or a gradient, where the mean is a summary and
	// parts of the run may read better or worse.
	BackgroundVaries bool `json:"backgroundVaries,omitempty"`
}

// ContrastReport is what pdf.Reader.CheckTextContrast found on a page.
// Values must not be shared across goroutines without synchronization.
type ContrastReport struct {
	// Failures are the runs short of their required ratio, in content
	// order.
	Failures []TextContrast `json:"failures,omitempty"`
	// Judged counts the runs of visible text measured, failures included.
	Judged int `json:"judged"`
	// Undetermined counts the runs of visible text whose colour the content
	// stream does not settle: painted with a pattern, with transparency, a
	// blend mode or a soft mask, by a Type 3 glyph that may set its own
	// colour, or in a colour space litho cannot convert.
	Undetermined int `json:"undetermined"`
}
