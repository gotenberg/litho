package models

import (
	"context"
	"fmt"
	"image"
	"io"
	"strings"
)

// OCREngine is an integration interface for Optical Character Recognition.
// Developers can inject a wrapper for Tesseract, Google Cloud Vision, or any
// other OCR backend via [pdf.WithOCREngine] so the engine can automatically
// extract text from scanned image pages.
// Implementations must be safe for concurrent use.
type OCREngine interface {
	Extract(img image.Image) (string, error)
}

// Page represents a decoupled, public view of a PDF page.
// Width and Height are effective dimensions in PDF points (1/72 inch),
// consistent with all other coordinates in the API. They account for both
// /Rotate (swapping width/height at 90°/270°) and /UserUnit (scaling factor;
// default 1.0). A portrait MediaBox with /Rotate 90 reports landscape dimensions.
// Values must not be shared across goroutines without synchronization.
type Page struct {
	PageNumber  int     `json:"pageNumber"`
	Width       float64 `json:"width"`
	Height      float64 `json:"height"`
	Rotate      int     `json:"rotate"`
	UserUnit    float64 `json:"userUnit"`
	IsLandscape bool    `json:"isLandscape"`
	// DPM aliases the reader's cached page metadata rather than copying it:
	// the page tree is resolved once per reader and every Page handed out
	// carries that same map. Treat it as read-only, and copy it before
	// changing anything, since writing through it edits the reader's own
	// state for every later caller. The rest of this struct is values.
	DPM      map[string]string `json:"dpm,omitempty"`
	MediaBox [4]float64        `json:"mediaBox"`
	CropBox  [4]float64        `json:"cropBox"`
	BleedBox [4]float64        `json:"bleedBox"`
	TrimBox  [4]float64        `json:"trimBox"`
	ArtBox   [4]float64        `json:"artBox"`
}

// TextElement represents a spatially positioned piece of text on a page.
// Values must not be shared across goroutines without synchronization.
type TextElement struct {
	Text       string  `json:"text"`
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	Width      float64 `json:"width"`
	FontSize   float64 `json:"fontSize"`
	SpaceWidth float64 `json:"-"` // font's space char advance in page-space units (internal, not serialized)
	// Chunk is the content-stream chunk ID: a counter that
	// increments when the content stream crosses a text-object or
	// text-positioning operator (BT, ET, Td, TD, Tm, Tf, T*). Glyphs
	// emitted by the same show-text operator chain share one Chunk
	// value. Reconstruction uses the ID to suppress gap-based space
	// insertion inside a single chunk, matching the operator-level
	// segmentation a PDF viewer applies.
	Chunk         int     `json:"chunk,omitempty"`
	MCID          int     `json:"mcid"`
	Hidden        bool    `json:"hidden,omitempty"`
	Invisible     bool    `json:"invisible,omitempty"`
	Vertical      bool    `json:"vertical,omitempty"`
	Angle         float64 `json:"angle,omitempty"`
	Redacted      bool    `json:"redacted,omitempty"`
	IsClipped     bool    `json:"isClipped,omitempty"`
	PatternType   string  `json:"patternType,omitempty"`
	Language      string  `json:"language,omitempty"`
	ExpansionText string  `json:"expansionText,omitempty"`
	MathML        string  `json:"mathML,omitempty"`
}

// Bookmark represents an item in the document outline.
// Values must not be shared across goroutines without synchronization.
type Bookmark struct {
	Title      string     `json:"title"`
	PageNumber int        `json:"pageNumber"`
	Children   []Bookmark `json:"children,omitempty"`
}

// FormField represents an interactive AcroForm field.
// Values must not be shared across goroutines without synchronization.
type FormField struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Annotation represents a PDF annotation (e.g., Link, Text, Highlight).
// Values must not be shared across goroutines without synchronization.
type Annotation struct {
	Subtype    string       `json:"subtype"`
	Rect       [4]float64   `json:"rect"`
	Contents   string       `json:"contents,omitempty"`
	URI        string       `json:"uri,omitempty"`
	DestPage   int          `json:"destPage,omitempty"`
	QuadPoints [][8]float64 `json:"quadPoints,omitempty"`
}

// ImageMeta represents metadata for an extracted Image XObject.
// The raw encoded stream can be accessed lazily via Open, which returns an
// [io.ReadCloser] that decodes through the PDF filter pipeline on read. The
// caller must close the returned reader.
// Values must not be shared across goroutines without synchronization.
type ImageMeta struct {
	Filename         string                                           `json:"filename"`
	Width            int                                              `json:"width"`
	Height           int                                              `json:"height"`
	ColorSpace       string                                           `json:"colorSpace"`
	BitsPerComponent int                                              `json:"bitsPerComponent"`
	Open             func(ctx context.Context) (io.ReadCloser, error) `json:"-"`
}

// Line represents a stroked line segment on a page, in absolute page coordinates.
// Values must not be shared across goroutines without synchronization.
type Line struct {
	X1 float64 `json:"x1"`
	Y1 float64 `json:"y1"`
	X2 float64 `json:"x2"`
	Y2 float64 `json:"y2"`
}

// Table represents a table detected from spatial analysis of a PDF page.
// Values must not be shared across goroutines without synchronization.
type Table struct {
	Rows [][]string `json:"rows"`
	BBox [4]float64 `json:"bbox"`
}

// EmbeddedFile represents a file embedded within a PDF document.
// Content is accessed lazily: call Open to obtain an [io.ReadCloser] that
// decodes the stream through the PDF filter pipeline on read. The caller
// must close the returned reader.
// Values must not be shared across goroutines without synchronization.
type EmbeddedFile struct {
	Name     string                                           `json:"name"`
	MIMEType string                                           `json:"mimeType,omitempty"`
	Open     func(ctx context.Context) (io.ReadCloser, error) `json:"-"`
}

// Signature represents a cryptographic signature found in the document.
// Values must not be shared across goroutines without synchronization.
type Signature struct {
	FieldName    string   `json:"fieldName"`
	ByteRange    [4]int64 `json:"byteRange"`
	Contents     string   `json:"contents"`
	DocumentHash string   `json:"documentHash"`
	SigningTime  string   `json:"signingTime,omitempty"`
	MDPPolicy    int      `json:"mdpPolicy,omitempty"`
}

// SignatureValidation holds the result of validating a PDF signature's
// X.509 certificate chain against a set of trusted root certificates.
// Values must not be shared across goroutines without synchronization.
type SignatureValidation struct {
	FieldName      string `json:"fieldName"`
	HashValid      bool   `json:"hashValid"`
	CertChainValid bool   `json:"certChainValid"`
	SignerCN       string `json:"signerCN"`
	Issuer         string `json:"issuer"`
	NotBefore      string `json:"notBefore"`
	NotAfter       string `json:"notAfter"`
	SigningTime    string `json:"signingTime,omitempty"`
	TimestampValid bool   `json:"timestampValid"`
	ErrorMessage   string `json:"errorMessage,omitempty"`

	// LTV / PAdES fields
	HasDSS          bool   `json:"hasDSS,omitempty"`
	HasTimestamp    bool   `json:"hasTimestamp,omitempty"`
	TimestampTime   string `json:"timestampTime,omitempty"`
	TimestampIssuer string `json:"timestampIssuer,omitempty"`
	LTVEnabled      bool   `json:"ltvEnabled,omitempty"`
	RevocationOK    bool   `json:"revocationOK,omitempty"`
	RevocationInfo  string `json:"revocationInfo,omitempty"`

	// MDP (Modification Detection and Prevention) fields
	MDPPolicy int    `json:"mdpPolicy,omitempty"`
	MDPValid  bool   `json:"mdpValid,omitempty"`
	MDPInfo   string `json:"mdpInfo,omitempty"`
}

// GeoTiePoint represents a single tie point mapping PDF coordinates to
// GPS coordinates (latitude/longitude).
// Values must not be shared across goroutines without synchronization.
type GeoTiePoint struct {
	PDFX float64 `json:"pdfX"`
	PDFY float64 `json:"pdfY"`
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
}

// GeoData holds geospatial metadata extracted from a PDF page or image
// that contains a /Measure dictionary with /Subtype /GEO.
// Values must not be shared across goroutines without synchronization.
type GeoData struct {
	CoordinateSystem string        `json:"coordinateSystem"`
	EPSG             string        `json:"epsg,omitempty"`
	TiePoints        []GeoTiePoint `json:"tiePoints"`
	Bounds           [4]float64    `json:"bounds"`
}

// ToGeoJSON converts the geospatial data to a GeoJSON FeatureCollection
// string containing tie points as Point features and the bounding box as
// a Polygon feature.
func (g *GeoData) ToGeoJSON() string {
	var features []string

	for i, tp := range g.TiePoints {
		feature := fmt.Sprintf(
			`{"type":"Feature","id":%d,"geometry":{"type":"Point","coordinates":[%g,%g]},"properties":{"pdfX":%g,"pdfY":%g}}`,
			i, tp.Lon, tp.Lat, tp.PDFX, tp.PDFY,
		)
		features = append(features, feature)
	}

	if len(g.TiePoints) > 0 {
		b := g.Bounds
		bbox := fmt.Sprintf(
			`{"type":"Feature","id":"bbox","geometry":{"type":"Polygon","coordinates":[[`+
				`[%g,%g],[%g,%g],[%g,%g],[%g,%g],[%g,%g]`+
				`]]},"properties":{"type":"bounds","crs":"%s","epsg":"%s"}}`,
			b[0], b[1], b[2], b[1], b[2], b[3], b[0], b[3], b[0], b[1],
			g.CoordinateSystem, g.EPSG,
		)
		features = append(features, bbox)
	}

	return fmt.Sprintf(`{"type":"FeatureCollection","features":[%s]}`, strings.Join(features, ","))
}

// ScaleAxis describes the unit and conversion factor for one axis of a
// rectilinear measurement scale extracted from a /Measure /RL dictionary.
// Values must not be shared across goroutines without synchronization.
type ScaleAxis struct {
	Unit             string  `json:"unit"`
	ConversionFactor float64 `json:"conversionFactor"`
	Decimals         int     `json:"decimals"`
}

// Measurement holds the 2D measurement scale extracted from a page Viewport
// (/VP) or a Line/PolyLine annotation that carries a /Measure dictionary with
// /Subtype /RL (Rectilinear). Developers can use the scale axes to convert
// raw PDF point coordinates into real-world physical measurements.
// Values must not be shared across goroutines without synchronization.
type Measurement struct {
	Source   string     `json:"source"`
	Name     string     `json:"name,omitempty"`
	BBox     [4]float64 `json:"bbox"`
	ScaleX   ScaleAxis  `json:"scaleX"`
	ScaleY   ScaleAxis  `json:"scaleY"`
	AreaUnit string     `json:"areaUnit,omitempty"`
}

// Model3D represents a 3D model stream embedded in a PDF annotation.
// Content is accessed lazily: call Open to obtain an [io.ReadCloser] that
// decodes the stream through the PDF filter pipeline on read. The caller
// must close the returned reader.
// Values must not be shared across goroutines without synchronization.
type Model3D struct {
	Name    string                                           `json:"name"`
	Subtype string                                           `json:"subtype"`
	Open    func(ctx context.Context) (io.ReadCloser, error) `json:"-"`
}

// DSSInfo represents the Document Security Store (/DSS) extracted from
// the document catalog. It contains the embedded validation materials
// needed for Long-Term Validation (LTV).
// Values must not be shared across goroutines without synchronization.
type DSSInfo struct {
	CertCount int `json:"certCount"`
	CRLCount  int `json:"crlCount"`
	OCSPCount int `json:"ocspCount"`
	VRICount  int `json:"vriCount"`
}

// PronunciationLexicon holds a W3C PLS (Pronunciation Lexicon Specification)
// XML document extracted from the catalog's /PronunciationLexicon array.
// Values must not be shared across goroutines without synchronization.
type PronunciationLexicon struct {
	XML string `json:"xml"`
}

// FormulaElement represents a mathematical formula extracted from the
// structure tree. It carries the MathML markup from the /AF associated file.
// Values must not be shared across goroutines without synchronization.
type FormulaElement struct {
	MathML string `json:"mathML"`
	MCIDs  []int  `json:"mcids,omitempty"`
}

// LinearizationParams holds the parameters extracted from a Linearization
// Dictionary found in the first 1024 bytes of a linearized PDF.
// Values must not be shared across goroutines without synchronization.
type LinearizationParams struct {
	FileLength          int64 `json:"fileLength"`
	HintStreamOffset    int64 `json:"hintStreamOffset"`
	HintStreamLength    int64 `json:"hintStreamLength"`
	FirstPageObjNum     int   `json:"firstPageObjNum"`
	NumPages            int   `json:"numPages"`
	FirstPageEndOffset  int64 `json:"firstPageEndOffset"`
	FirstPageXRefOffset int64 `json:"firstPageXRefOffset"`
	// Hints is the decoded Page Offset Hint Table, one entry per page,
	// or nil when the hint stream could not be parsed.
	Hints []PageOffsetEntry `json:"hints,omitempty"`
}

// PageOffsetEntry holds the per-page data decoded from the hint stream's
// Page Offset Hint Table.
// Values must not be shared across goroutines without synchronization.
type PageOffsetEntry struct {
	ObjCount            int   `json:"objCount"`
	PageLength          int64 `json:"pageLength"`
	ContentStreamOffset int64 `json:"contentStreamOffset"`
}

// StreamDiagnostic identifies an in-use stream litho cannot fully decode,
// reported by [pdf.Reader.UndecodableStreams]. Filters lists the
// undecodable filter names in the stream's /Filter chain (e.g.
// "JBIG2Decode" when the jbig2 plugin is not registered).
// Values must not be shared across goroutines without synchronization.
type StreamDiagnostic struct {
	ObjectNum int      `json:"objectNum"`
	Filters   []string `json:"filters"`
}
