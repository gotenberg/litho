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
// other OCR backend via [github.com/gotenberg/litho/pkg/pdf.WithOCREngine] so
// the engine can automatically extract text from scanned image pages.
// Implementations must be safe for concurrent use.
type OCREngine interface {
	// Extract returns the text recognized in img.
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
	// state for every later caller. The rest of this struct is values, and
	// each declared box points at an array the call allocates for this Page
	// alone.
	DPM map[string]string `json:"dpm,omitempty"`
	// MediaBox is the media box as the page, or the page tree node it inherits
	// from, writes it, in default user space units before /UserUnit applies.
	// It is the zero rectangle when the file declares none: the US Letter
	// fallback reaches only Width and Height, and the page the other boxes
	// are reduced to.
	MediaBox [4]float64 `json:"mediaBox"`
	// CropBox is the effective crop box. CropBox, BleedBox, TrimBox and
	// ArtBox are the effective page boundaries, not the entries as written,
	// read as pdf.js and poppler read them. A box that is not four numbers
	// enclosing an area is no box to them and reads as absent. An absent box
	// takes its ISO 32000-1, Table 30 default: the media box for the crop box,
	// the crop box for the bleed, trim and art boxes. A box that extends beyond
	// the media box is reduced to its intersection with it, as ISO 32000-1,
	// 14.11.2.1 requires, or with US Letter where the media box is no box, and
	// a box whose intersection encloses no area takes its default. None is
	// clipped to the crop box, which has no defined relationship with the other
	// boundaries.
	CropBox [4]float64 `json:"cropBox"`
	// BleedBox is the effective bleed box. See CropBox for the rules.
	BleedBox [4]float64 `json:"bleedBox"`
	// TrimBox is the effective trim box. See CropBox for the rules.
	TrimBox [4]float64 `json:"trimBox"`
	// ArtBox is the effective art box. See CropBox for the rules.
	ArtBox [4]float64 `json:"artBox"`
	// DeclaredCropBox is the crop box as the file writes it, before the
	// default and the intersection that make CropBox. DeclaredCropBox,
	// DeclaredBleedBox, DeclaredTrimBox and DeclaredArtBox are the entries as
	// written, in the units and corner order of the file, so a box that
	// extends beyond the media box, which ISO 32000-1, 14.11.2.1 reduces in
	// the effective box, reads here at its declared size. Each is nil when the
	// entry is absent, or is not an array of four numbers, which the
	// effective box also reads as absent (ISO 32000-1, 7.9.5). A declared box
	// has no default: nil tells a missing entry apart from one that equals
	// its default. MediaBox needs no declared counterpart, since it already
	// reports the entry as written.
	//
	// The crop box is inheritable (ISO 32000-1, 7.7.3.3, Table 30), so
	// DeclaredCropBox is the entry of the page or, when the page has none, of
	// the nearest page tree node above it that has one.
	DeclaredCropBox *[4]float64 `json:"declaredCropBox,omitempty"`
	// DeclaredBleedBox is the bleed box as the file writes it on the page, nil
	// when absent. It is not inheritable (ISO 32000-1, 7.7.3.3, Table 30). See
	// DeclaredCropBox for the rules.
	DeclaredBleedBox *[4]float64 `json:"declaredBleedBox,omitempty"`
	// DeclaredTrimBox is the trim box as the file writes it on the page, nil
	// when absent. It is not inheritable (ISO 32000-1, 7.7.3.3, Table 30). See
	// DeclaredCropBox for the rules.
	DeclaredTrimBox *[4]float64 `json:"declaredTrimBox,omitempty"`
	// DeclaredArtBox is the art box as the file writes it on the page, nil
	// when absent. It is not inheritable (ISO 32000-1, 7.7.3.3, Table 30). See
	// DeclaredCropBox for the rules.
	DeclaredArtBox *[4]float64 `json:"declaredArtBox,omitempty"`
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
	Chunk     int     `json:"chunk,omitempty"`
	MCID      int     `json:"mcid"`
	Hidden    bool    `json:"hidden,omitempty"`
	Invisible bool    `json:"invisible,omitempty"`
	Vertical  bool    `json:"vertical,omitempty"`
	Angle     float64 `json:"angle,omitempty"`
	// InkRight reports that horizontal text inks the right of its advance,
	// the side a quarter turn clockwise from it, rather than the left, where
	// glyphs rise to their ascenders as the rendering matrix of ISO 32000-1,
	// 9.4.4 sets them upright. A negative horizontal scaling, a mirrored text
	// matrix and a mirrored CTM each reflect the glyphs onto the right, and
	// any two of them cancel; so does a Type 3 font that draws its glyphs
	// below its text-space baseline (9.6.5). A negative font size turns the
	// glyphs a half turn without moving them across the advance, which Angle
	// already carries.
	InkRight      bool   `json:"inkRight,omitempty"`
	Redacted      bool   `json:"redacted,omitempty"`
	IsClipped     bool   `json:"isClipped,omitempty"`
	PatternType   string `json:"patternType,omitempty"`
	Language      string `json:"language,omitempty"`
	ExpansionText string `json:"expansionText,omitempty"`
	MathML        string `json:"mathML,omitempty"`
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
// Open returns an [io.ReadCloser] over the image stream's data decoded
// lazily through its filters on read: the samples, packed at BitsPerComponent
// in ColorSpace, rather than the stored bytes or pixels. A filter litho cannot
// decode is skipped, its input handed on still encoded. The caller must close
// the returned reader. Decode turns this image's samples into pixels on
// demand. Filename can repeat on one page, since two form XObjects may each
// name an image Im0, so Decode is the way to decode this entry rather than
// another image of the same name.
// Values must not be shared across goroutines without synchronization.
type ImageMeta struct {
	Filename         string                                           `json:"filename"`
	Width            int                                              `json:"width"`
	Height           int                                              `json:"height"`
	ColorSpace       string                                           `json:"colorSpace"`
	BitsPerComponent int                                              `json:"bitsPerComponent"`
	Open             func(ctx context.Context) (io.ReadCloser, error) `json:"-"`
	Decode           func(ctx context.Context) (image.Image, error)   `json:"-"`
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
	// SubFilter is the signature dictionary's /SubFilter, such as
	// adbe.pkcs7.detached, ETSI.CAdES.detached or, for a document timestamp,
	// ETSI.RFC3161 (ISO 32000-2, 12.8.3 and 12.8.5).
	SubFilter string `json:"subFilter,omitempty"`
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
	// TimestampError says why the RFC 3161 token found does not verify over
	// what it timestamps; its time is then not trusted as the signing time.
	TimestampError string `json:"timestampError,omitempty"`
	// SubFilter is the signature dictionary's /SubFilter.
	SubFilter      string `json:"subFilter,omitempty"`
	LTVEnabled     bool   `json:"ltvEnabled,omitempty"`
	RevocationOK   bool   `json:"revocationOK,omitempty"`
	RevocationInfo string `json:"revocationInfo,omitempty"`

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
// reported by [github.com/gotenberg/litho/pkg/pdf.Reader.UndecodableStreams],
// or whose data litho salvages, reported by
// [github.com/gotenberg/litho/pkg/pdf.Reader.SalvagedStreams]. Filters lists
// the reported filter names in the stream's /Filter chain: for the first, a
// filter name no decoder knows, or a filter whose built-in decoder refuses a
// feature the stream uses; for the second, a filter whose data is damaged.
// Values must not be shared across goroutines without synchronization.
type StreamDiagnostic struct {
	ObjectNum int      `json:"objectNum"`
	Filters   []string `json:"filters"`
}
