package models

// Annotation represents a PDF annotation (e.g., Link, Text, Highlight).
// Concurrent reads of a value are safe; a write needs synchronization.
type Annotation struct {
	// Subtype is the annotation's /Subtype name, such as "Link" or "Text".
	Subtype string `json:"subtype"`
	// Rect is the annotation's /Rect as the file writes it, in default user
	// space. Its corners are not normalized.
	Rect [4]float64 `json:"rect"`
	// Contents is the annotation's /Contents text, decoded, or empty when it
	// has none.
	Contents string `json:"contents,omitempty"`
	// URI is the target of a Link annotation whose action is a URI action, or
	// empty otherwise.
	URI string `json:"uri,omitempty"`
	// DestPage is the 1-based page a Link annotation's GoTo action or
	// destination names, or 0 when it names none.
	DestPage int `json:"destPage,omitempty"`
	// QuadPoints holds the annotation's /QuadPoints, one entry of eight
	// numbers, four x and y pairs, per quadrilateral (ISO 32000-1, 12.5.6.10).
	QuadPoints [][8]float64 `json:"quadPoints,omitempty"`
}
