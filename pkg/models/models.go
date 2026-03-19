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
// other OCR backend into the pdf.Reader so the engine can automatically
// extract text from scanned image pages.
type OCREngine interface {
	Extract(img image.Image) (string, error)
}

// Page represents a decoupled, public view of a PDF page.
type Page struct {
	PageNumber int
	Width      float64
	Height     float64
	DPM        map[string]string // Document Part Metadata (PDF 2.0)
	MediaBox   [4]float64        // [llx, lly, urx, ury] physical sheet boundary
	CropBox    [4]float64        // [llx, lly, urx, ury] visible region (defaults to MediaBox)
	BleedBox   [4]float64        // [llx, lly, urx, ury] bleed region (defaults to CropBox)
	TrimBox    [4]float64        // [llx, lly, urx, ury] intended finished page (defaults to CropBox)
	ArtBox     [4]float64        // [llx, lly, urx, ury] meaningful content extent (defaults to CropBox)
}

// TextElement represents a spatially positioned piece of text on a page.
type TextElement struct {
	Text          string
	X             float64
	Y             float64
	Width         float64 // Advance width of this glyph in page-space units
	FontSize      float64
	MCID          int
	Hidden        bool
	Invisible     bool
	Vertical      bool    // True when the glyph was set with a WMode=1 (vertical) font
	Angle         float64 // Text direction angle in radians (0 = horizontal LTR, π/2 = vertical)
	Redacted      bool    // True when the element falls inside an unapplied /Redact annotation
	IsClipped     bool    // True when the element falls outside the /CropBox visible region
	PatternType   string  // "Tiling", "Shading", or "" when fill uses a Pattern color space
	Language      string  // BCP 47 language tag from span-level /Lang in marked content
	ExpansionText string  // Expansion text from /E in marked content (e.g., "World Wide Web" for "WWW")
	MathML        string  // MathML markup from a /Formula structure element's /AF entry
}

// Bookmark represents an item in the document outline.
type Bookmark struct {
	Title      string
	PageNumber int // 1-based destination page number
	Children   []Bookmark
}

// FormField represents an interactive AcroForm field.
type FormField struct {
	Name  string
	Value string
}

// Annotation represents a PDF annotation (e.g., Link, Text, Highlight).
type Annotation struct {
	Subtype    string
	Rect       [4]float64
	Contents   string
	URI        string       // Only for Links
	DestPage   int          // 1-based page number, only for internal Links
	QuadPoints [][8]float64 // Groups of 8 coordinates defining quadrilateral regions (Redact, Highlight, etc.)
}

// ImageMeta represents metadata for an extracted image XObject.
// The raw encoded stream can be accessed lazily via Open, which returns an
// io.ReadCloser that decodes through the PDF filter pipeline on read. The
// caller must close the returned reader.
type ImageMeta struct {
	Filename         string
	Width            int
	Height           int
	ColorSpace       string
	BitsPerComponent int
	Open             func(ctx context.Context) (io.ReadCloser, error) // Lazy accessor for decoded image stream data
}

// Line represents a stroked line segment on a page, in absolute page coordinates.
type Line struct {
	X1, Y1, X2, Y2 float64
}

// Table represents a table detected from spatial analysis of a PDF page.
type Table struct {
	Rows [][]string // Row-major 2D grid of cell text
	BBox [4]float64 // [minX, minY, maxX, maxY] bounding box
}

// EmbeddedFile represents a file embedded within a PDF document.
// Content is accessed lazily: call Open to obtain an io.ReadCloser that
// decodes the stream through the PDF filter pipeline on read. The caller
// must close the returned reader.
type EmbeddedFile struct {
	Name     string                                           // Original filename from the file specification
	MIMEType string                                           // MIME type from the /Subtype entry, if present
	Open     func(ctx context.Context) (io.ReadCloser, error) // Lazy accessor for decoded file content
}

// Signature represents a cryptographic signature found in the document.
type Signature struct {
	FieldName    string
	ByteRange    [4]int64
	Contents     string
	DocumentHash string
	SigningTime  string // /M entry from the signature dictionary (PDF date string or ISO 8601)
	MDPPolicy    int    // DocMDP /P value extracted from /Reference array (0 = not set)
}

// SignatureValidation holds the result of validating a PDF signature's
// X.509 certificate chain against a set of trusted root certificates.
type SignatureValidation struct {
	FieldName      string
	HashValid      bool   // True if the document hash matches the signed range
	CertChainValid bool   // True if the signer certificate chains to a trusted root
	SignerCN       string // Common Name of the leaf (signer) certificate
	Issuer         string // Common Name of the issuing CA
	NotBefore      string // Certificate validity start (RFC 3339)
	NotAfter       string // Certificate validity end (RFC 3339)
	SigningTime    string // Declared signing time from the PDF /M entry
	TimestampValid bool   // True if signing time falls within certificate validity
	Error          string // Non-empty if validation failed with an error

	// LTV / PAdES fields
	HasDSS          bool   // True if a Document Security Store exists
	HasTimestamp    bool   // True if an RFC 3161 timestamp token was found
	TimestampTime   string // Cryptographic timestamp from TSA (RFC 3339)
	TimestampIssuer string // Common Name of the Timestamp Authority
	LTVEnabled      bool   // True if enough embedded data exists for offline validation
	RevocationOK    bool   // True if OCSP/CRL data confirms the cert was not revoked at signing time
	RevocationInfo  string // Description of revocation check result

	// MDP (Modification Detection and Prevention) fields
	MDPPolicy int    // DocMDP /P value: 1=no changes, 2=forms+signing, 3=forms+signing+annotations; 0=not set
	MDPValid  bool   // True if no unauthorized modifications were detected after signing
	MDPInfo   string // Description of MDP evaluation result
}

// GeoTiePoint represents a single tie point mapping PDF coordinates to
// GPS coordinates (latitude/longitude).
type GeoTiePoint struct {
	PDFX float64 // X coordinate in PDF user space
	PDFY float64 // Y coordinate in PDF user space
	Lat  float64 // Latitude (WGS 84)
	Lon  float64 // Longitude (WGS 84)
}

// GeoData holds geospatial metadata extracted from a PDF page or image
// that contains a /Measure dictionary with /Subtype /GEO.
type GeoData struct {
	CoordinateSystem string        // Coordinate system type (e.g., "GEOGCS", "PROJCS")
	EPSG             string        // EPSG code if identified (e.g., "4326" for WGS 84)
	TiePoints        []GeoTiePoint // Mapping from PDF space to GPS coordinates
	Bounds           [4]float64    // [minLon, minLat, maxLon, maxLat] bounding box
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
type ScaleAxis struct {
	Unit             string  // Display unit label (e.g., "ft", "m", "mm", "in")
	ConversionFactor float64 // Multiplier from default user-space units to the display unit
	Decimals         int     // Number of decimal places for display (/D entry)
}

// Measurement holds the 2D measurement scale extracted from a page Viewport
// (/VP) or a Line/PolyLine annotation that carries a /Measure dictionary with
// /Subtype /RL (Rectilinear). Developers can use the scale axes to convert
// raw PDF point coordinates into real-world physical measurements.
type Measurement struct {
	Source   string     // "Viewport" or annotation subtype (e.g., "Line", "PolyLine")
	Name     string     // Viewport /Name or annotation /NM, if present
	BBox     [4]float64 // Viewport /BBox [llx, lly, urx, ury], or annotation /Rect
	ScaleX   ScaleAxis  // X-axis (horizontal) scale
	ScaleY   ScaleAxis  // Y-axis (vertical) scale
	AreaUnit string     // Area display unit from /A, if present (e.g., "sq ft")
}

// Model3D represents a 3D model stream embedded in a PDF annotation.
// Content is accessed lazily: call Open to obtain an io.ReadCloser that
// decodes the stream through the PDF filter pipeline on read. The caller
// must close the returned reader.
type Model3D struct {
	Name    string                                           // Annotation name or resource key
	Subtype string                                           // "U3D" (Universal 3D) or "PRC" (Product Representation Compact)
	Open    func(ctx context.Context) (io.ReadCloser, error) // Lazy accessor for decoded 3D model data
}

// DSSInfo represents the Document Security Store (/DSS) extracted from
// the document catalog. It contains the embedded validation materials
// needed for Long-Term Validation (LTV).
type DSSInfo struct {
	CertCount int // Number of certificates in /Certs
	CRLCount  int // Number of CRLs in /CRLs
	OCSPCount int // Number of OCSP responses in /OCSPs
	VRICount  int // Number of entries in /VRI
}

// PronunciationLexicon holds a W3C PLS (Pronunciation Lexicon Specification)
// XML document extracted from the catalog's /PronunciationLexicon array.
type PronunciationLexicon struct {
	XML string // Raw PLS XML content
}

// FormulaElement represents a mathematical formula extracted from the
// structure tree. It carries the MathML markup from the /AF associated file.
type FormulaElement struct {
	MathML string // Raw MathML XML string from the /AF entry
	MCIDs  []int  // Marked Content IDs covered by this formula's structure element
}

// LinearizationParams holds the parameters extracted from a Linearization
// Dictionary found in the first 1024 bytes of a linearized PDF.
type LinearizationParams struct {
	FileLength          int64
	HintStreamOffset    int64
	HintStreamLength    int64
	FirstPageObjNum     int
	NumPages            int
	FirstPageEndOffset  int64
	FirstPageXRefOffset int64
}

// PageOffsetEntry holds the per-page data decoded from the hint stream's
// Page Offset Hint Table.
type PageOffsetEntry struct {
	ObjCount            int
	PageLength          int64
	ContentStreamOffset int64
}
