// Package export provides serialization utilities for converting litho's
// public model types into standard interchange formats (JSON, CSV, Markdown).
package export

import (
	"encoding/json"
	"io"

	"github.com/gotenberg/litho/pkg/models"
)

// PageData holds all extracted data for a single page.
type PageData struct {
	Page         models.Page          `json:"page"`
	Text         string               `json:"text,omitempty"`
	TextElements []models.TextElement `json:"textElements,omitempty"`
	Annotations  []models.Annotation  `json:"annotations,omitempty"`
	Tables       []models.Table       `json:"tables,omitempty"`
	Images       []models.ImageMeta   `json:"images,omitempty"`
	Lines        []models.Line        `json:"lines,omitempty"`
	Measurements []models.Measurement `json:"measurements,omitempty"`
	GeoData      []models.GeoData     `json:"geoData,omitempty"`
	Models3D     []models.Model3D     `json:"models3d,omitempty"`
}

// Document holds all extracted data for a complete PDF document.
type Document struct {
	Metadata      map[string]string             `json:"metadata,omitempty"`
	Bookmarks     []models.Bookmark             `json:"bookmarks,omitempty"`
	FormData      map[string]string             `json:"formData,omitempty"`
	Pages         []PageData                    `json:"pages"`
	Signatures    []models.SignatureValidation  `json:"signatures,omitempty"`
	DSS           *models.DSSInfo               `json:"dss,omitempty"`
	Lexicons      []models.PronunciationLexicon `json:"pronunciationLexicons,omitempty"`
	Formulas      []models.FormulaElement       `json:"formulas,omitempty"`
	EmbeddedFiles []models.EmbeddedFile         `json:"embeddedFiles,omitempty"`
	Linearization *models.LinearizationParams   `json:"linearization,omitempty"`
	HintTable     []models.PageOffsetEntry      `json:"hintTable,omitempty"`
	IsPortfolio   bool                          `json:"isPortfolio,omitempty"`
	IsLinearized  bool                          `json:"isLinearized,omitempty"`
}

// JSON writes the document data as formatted JSON to w.
func JSON(w io.Writer, doc *Document) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(doc)
}

// JSONCompact writes the document data as compact (non-indented) JSON to w.
func JSONCompact(w io.Writer, doc *Document) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(doc)
}

// MarshalJSON returns the document data as a formatted JSON byte slice.
func MarshalJSON(doc *Document) ([]byte, error) {
	return json.MarshalIndent(doc, "", "  ")
}

// MarshalJSONCompact returns the document data as a compact JSON byte slice.
func MarshalJSONCompact(doc *Document) ([]byte, error) {
	return json.Marshal(doc)
}
