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
// Concurrent reads of a value are safe; a write needs synchronization.
type Page struct {
	// PageNumber is the 1-based position of the page in the document.
	PageNumber int `json:"pageNumber"`
	// Width is the effective width in PDF points, after /Rotate and /UserUnit.
	Width float64 `json:"width"`
	// Height is the effective height in PDF points, after /Rotate and
	// /UserUnit.
	Height float64 `json:"height"`
	// Rotate is the page's /Rotate in degrees clockwise, inherited from the
	// page tree and not normalized, so it reads as the file writes it (ISO
	// 32000-1, 7.7.3.3, Table 30).
	Rotate int `json:"rotate"`
	// UserUnit is the page's /UserUnit, the size of one user space unit in
	// points. It is 1 when the file states none (ISO 32000-1, Table 30).
	UserUnit float64 `json:"userUnit"`
	// IsLandscape reports that Width is greater than Height.
	IsLandscape bool `json:"isLandscape"`
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
	// It is the zero rectangle when the file declares none. The US Letter
	// fallback reaches Width and Height, and the intersection that reduces
	// the other boxes, but never this field.
	MediaBox [4]float64 `json:"mediaBox"`
	// CropBox is the effective crop box. CropBox, BleedBox, TrimBox and ArtBox are
	// the effective page boundaries, not the entries as written, read as viewers
	// read them:
	//
	//   - A box that is not four numbers enclosing an area is no box to a viewer
	//     and reads as absent.
	//   - An absent box takes its ISO 32000-1, Table 30 default: the media box for
	//     the crop box, the crop box for the bleed, trim and art boxes.
	//   - A box that extends beyond the media box is reduced to its intersection
	//     with it, as ISO 32000-1, 14.11.2.1 requires, or with US Letter where the
	//     media box is no box.
	//   - A box whose intersection encloses no area takes its default.
	//   - None is clipped to the crop box, which has no defined relationship with
	//     the other boundaries.
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
// Concurrent reads of a value are safe; a write needs synchronization.
type TextElement struct {
	// Text is the characters the glyph maps to.
	Text string `json:"text"`
	// X is the horizontal position of the glyph origin in default user space,
	// in points. The origin sits on the baseline, lifted by any text rise.
	X float64 `json:"x"`
	// Y is the vertical position of the glyph origin, in the same space as X.
	Y float64 `json:"y"`
	// Width is the advance of the glyph in default user space, in points. It
	// leaves out character and word spacing, which sit in the gap between
	// elements.
	Width float64 `json:"width"`
	// FontSize is the font size as drawn, in points, once the text matrix and
	// the CTM scale it.
	FontSize float64 `json:"fontSize"`
	// SpaceWidth is the advance of the font's space glyph at the size and
	// horizontal scaling the element is drawn at, in the units of Width. It is
	// left out of the JSON form.
	SpaceWidth float64 `json:"-"`
	// Chunk numbers the run of the content stream the element comes from.
	// Elements that share a Chunk value come from one chain of show-text
	// operators, with no text object or text positioning operator between
	// them. The value is zero when omitted from the JSON form.
	Chunk int `json:"chunk,omitempty"`
	// MCID is the marked-content identifier of the innermost enclosing
	// marked-content sequence that carries one, or -1 when the text sits in
	// none.
	MCID int `json:"mcid"`
	// Hidden reports a glyph whose origin falls outside the clipping path the
	// page set with W or W*. A page that sets none never marks an element
	// hidden.
	Hidden bool `json:"hidden,omitempty"`
	// Invisible reports text drawn in rendering mode 3 or painted only in
	// white (ISO 32000-1, 9.3.6, Table 106).
	Invisible bool `json:"invisible,omitempty"`
	// Vertical reports a font in vertical writing mode (ISO 32000-1, 9.7.4.2).
	Vertical bool `json:"vertical,omitempty"`
	// Angle is the direction the glyph advances along, in radians, measured
	// from the positive X axis of default user space.
	Angle float64 `json:"angle,omitempty"`
	// InkRight reports that horizontal text inks the right of its advance, the side
	// a quarter turn clockwise from it, rather than the left. On the left, glyphs
	// rise to their ascenders as the rendering matrix of ISO 32000-1, 9.4.4 sets
	// them upright.
	//
	// A negative horizontal scaling, a mirrored text matrix and a mirrored CTM each
	// reflect the glyphs onto the right, and any two of them cancel. So does a Type
	// 3 font that draws its glyphs below its text-space baseline (9.6.5). A
	// negative font size turns the glyphs a half turn without moving them across
	// the advance, which Angle already carries.
	InkRight bool `json:"inkRight,omitempty"`
	// InkBox is the axis-aligned box [minX minY maxX maxY], in default user space,
	// holding the ink the element paints and its origin. Redaction tests it against
	// a region. It is all zeros on an element that does not come from extraction.
	//
	// A run's box is the union of its glyphs'. Each glyph's bounds are carried
	// through the text rendering matrix of ISO 32000-1, 9.4.4,
	// [Tfs×Th 0 0 Tfs 0 Trise] × Tm × CTM, so a skewed, turned or mirrored matrix
	// gives the box of the parallelogram it draws. The bounds depend on the glyph:
	//
	//   - A Type 3 glyph is bounded by what its description paints and the box its
	//     d1 states (9.6.5), mapped by /FontMatrix. A glyph drawn in pixels and
	//     shown at Tf 1 is bounded where it inks, however small its font size.
	//   - A Type 3 glyph whose description paints what no box holds, a shading over
	//     no clip of its own, is bounded by the clipping path it is shown under.
	//   - Any other glyph is bounded by its advance along the baseline and by the
	//     font's /FontBBox across it, never less than an ascender of one em and a
	//     descender of a fifth of one.
	//   - When the text rendering mode strokes the glyph (9.3.6, Table 106), the
	//     box grows by the reach of the pen: half the line width through the CTM,
	//     √2 times that under a projecting square cap (8.4.3.3). A miter join's tip
	//     at a sharp corner may reach past it (8.4.3.5).
	InkBox [4]float64 `json:"inkBox,omitzero"`
	// Redacted reports an element whose ink overlaps an unapplied /Redact
	// annotation. Only Reader.ExtractRedactedText sets it.
	Redacted bool `json:"redacted,omitempty"`
	// IsClipped reports a glyph whose origin falls outside the page's crop
	// box, the region a viewer shows (ISO 32000-1, 14.11.2). It stays false
	// when the crop box lies wholly outside the media box.
	IsClipped bool `json:"isClipped,omitempty"`
	// PatternType is "Tiling" or "Shading" when the glyph is painted with a
	// pattern of that type (ISO 32000-1, 8.7.3.1, Table 75), and empty
	// otherwise.
	PatternType string `json:"patternType,omitempty"`
	// Language is the BCP 47 tag of the nearest enclosing marked-content
	// sequence that carries a /Lang entry, or empty when none does.
	Language string `json:"language,omitempty"`
	// ExpansionText is the /E expansion of the nearest enclosing
	// marked-content sequence that carries one, the full form of an
	// abbreviation or acronym (ISO 32000-1, 14.9.5). It is empty when none
	// does.
	ExpansionText string `json:"expansionText,omitempty"`
}

// Bookmark represents an item in the document outline.
// Concurrent reads of a value are safe; a write needs synchronization.
type Bookmark struct {
	// Title is the text of the outline item.
	Title string `json:"title"`
	// PageNumber is the 1-based page the item leads to, or 0 when it names no
	// page of the document.
	PageNumber int `json:"pageNumber"`
	// Children are the items nested under this one, in outline order.
	Children []Bookmark `json:"children,omitempty"`
}

// FormField represents an interactive AcroForm field.
// Concurrent reads of a value are safe; a write needs synchronization.
type FormField struct {
	// Name is the fully qualified field name, the partial names of the field
	// and its ancestors joined by periods (ISO 32000-1, 12.7.3.2).
	Name string `json:"name"`
	// Value is the field's value as text. A multi-select list box joins its
	// selections into one string.
	Value string `json:"value"`
}

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

// ImageMeta represents metadata for an extracted Image XObject.
//
// Open returns an [io.ReadCloser] over the image stream's data, decoded lazily
// through its filters on read. It yields the samples, packed at
// BitsPerComponent in ColorSpace, rather than the stored bytes or pixels. A
// filter litho cannot decode is skipped, its input handed on still encoded. The
// caller must close the returned reader.
//
// Decode turns this image's samples into pixels on demand. It decodes as
// Reader.DecodeImage in package pdf does, an opacity the JPEG 2000 data carries
// included.
//
// Filename can repeat on one page, since two form XObjects may each name an
// image Im0. Decode is the way to decode this entry rather than another image
// of the same name.
//
// Concurrent reads of a value are safe; a write needs synchronization.
// Open and Decode may be called from several goroutines at once, and alongside
// other reads of the Reader that returned the value, while that Reader stays
// open.
type ImageMeta struct {
	// Filename is the name the image has in its resource dictionary, such as
	// Im0. It is not a file name and can repeat on one page.
	Filename string `json:"filename"`
	// Width is the image width in samples.
	Width int `json:"width"`
	// Height is the image height in samples.
	Height int `json:"height"`
	// ColorSpace names the image's colour space.
	ColorSpace string `json:"colorSpace"`
	// BitsPerComponent is the number of bits of each colour component. It is
	// 8 when the image states none or is JPXDecode data.
	BitsPerComponent int `json:"bitsPerComponent"`
	// Open reads the image's decoded samples. Reader.ExtractImages sets it on
	// every entry, and each call returns a new reader.
	Open func(ctx context.Context) (io.ReadCloser, error) `json:"-"`
	// Decode returns the image's pixels. Reader.ExtractImages sets it on every
	// entry.
	Decode func(ctx context.Context) (image.Image, error) `json:"-"`
}

// Line represents a stroked line segment on a page, in absolute page coordinates.
// Concurrent reads of a value are safe; a write needs synchronization.
type Line struct {
	// X1 is the x coordinate of the start point, in points.
	X1 float64 `json:"x1"`
	// Y1 is the y coordinate of the start point, in points.
	Y1 float64 `json:"y1"`
	// X2 is the x coordinate of the end point, in points.
	X2 float64 `json:"x2"`
	// Y2 is the y coordinate of the end point, in points.
	Y2 float64 `json:"y2"`
}

// Table represents a table detected from spatial analysis of a PDF page.
// Concurrent reads of a value are safe; a write needs synchronization.
type Table struct {
	// Rows holds the cell text, top row first and left cell first in each
	// row.
	Rows [][]string `json:"rows"`
	// BBox is the box [minX minY maxX maxY] around the table, in default user
	// space.
	BBox [4]float64 `json:"bbox"`
}

// EmbeddedFile represents a file embedded within a PDF document.
// Content is accessed lazily: call Open to obtain an [io.ReadCloser] that
// decodes the stream through the PDF filter pipeline on read. The caller
// must close the returned reader.
// Concurrent reads of a value are safe; a write needs synchronization.
// Open may be called from several goroutines at once, and alongside
// other reads of the Reader that returned the value, while that Reader stays
// open.
type EmbeddedFile struct {
	// Name is the base name of the file, read from the file specification's
	// /UF or /F entry. It is "embedded" when the specification has neither.
	Name string `json:"name"`
	// MIMEType is the stream's /Subtype name, such as application/pdf, or
	// empty when the stream has none.
	MIMEType string `json:"mimeType,omitempty"`
	// Open reads the file's decoded content. Each call returns a new reader.
	Open func(ctx context.Context) (io.ReadCloser, error) `json:"-"`
}

// Signature represents a cryptographic signature found in the document.
// Concurrent reads of a value are safe; a write needs synchronization.
type Signature struct {
	// FieldName is the fully qualified name of the signature field.
	FieldName string `json:"fieldName"`
	// ByteRange is the signature's /ByteRange: the offset and length, in bytes,
	// of each of the two file ranges the signature covers (ISO 32000-1,
	// 12.8.1).
	ByteRange [4]int64 `json:"byteRange"`
	// Contents is the signature's /Contents: the raw bytes of the PKCS#7 or
	// CMS container, not hex-encoded.
	Contents string `json:"contents"`
	// DocumentHash is the hex digest of the bytes ByteRange covers, taken with
	// the digest algorithm the signature declares. It is empty when ByteRange
	// names bytes the file does not hold.
	DocumentHash string `json:"documentHash"`
	// SigningTime is the signature's /M entry as the file writes it, a PDF
	// date string, or empty when it has none.
	SigningTime string `json:"signingTime,omitempty"`
	// MDPPolicy is the /P permission of the signature's DocMDP or FieldMDP
	// transform (ISO 32000-1, 12.8.1, Tables 252 and 253), or 0 when it has none.
	MDPPolicy int `json:"mdpPolicy,omitempty"`
	// SubFilter is the signature dictionary's /SubFilter, such as
	// adbe.pkcs7.detached, ETSI.CAdES.detached or, for a document timestamp,
	// ETSI.RFC3161 (ISO 32000-2, 12.8.3 and 12.8.5).
	SubFilter string `json:"subFilter,omitempty"`
}

// SignatureValidation holds the result of validating a PDF signature's
// X.509 certificate chain against a set of trusted root certificates.
// Concurrent reads of a value are safe; a write needs synchronization.
type SignatureValidation struct {
	// FieldName is the fully qualified name of the signature field.
	FieldName string `json:"fieldName"`
	// HashValid reports that the signature verifies over the bytes its
	// /ByteRange covers, independent of trust in the certificate chain.
	HashValid bool `json:"hashValid"`
	// CertChainValid reports that the signer certificate chains to a trusted
	// root, checked at the signing time when one is known.
	CertChainValid bool `json:"certChainValid"`
	// SignerCN is the common name of the signer certificate.
	SignerCN string `json:"signerCN"`
	// Issuer is the common name of the signer certificate's issuer.
	Issuer string `json:"issuer"`
	// NotBefore is the start of the signer certificate's validity, in RFC 3339
	// form.
	NotBefore string `json:"notBefore"`
	// NotAfter is the end of the signer certificate's validity, in RFC 3339
	// form.
	NotAfter string `json:"notAfter"`
	// SigningTime is the signature's /M entry as the file writes it, or empty
	// when it has none.
	SigningTime string `json:"signingTime,omitempty"`
	// TimestampValid reports that the signing time falls within the signer
	// certificate's validity. It is false when no signing time is known.
	TimestampValid bool `json:"timestampValid"`
	// ErrorMessage says why a check failed, or is empty when none did.
	ErrorMessage string `json:"errorMessage,omitempty"`

	// LTV / PAdES fields

	// HasDSS reports that the document carries a Document Security Store.
	HasDSS bool `json:"hasDSS,omitempty"`
	// HasTimestamp reports that the signature holds an RFC 3161 timestamp
	// token.
	HasTimestamp bool `json:"hasTimestamp,omitempty"`
	// TimestampTime is the time the timestamp token states, in RFC 3339 form.
	TimestampTime string `json:"timestampTime,omitempty"`
	// TimestampIssuer is the common name of the timestamp authority's
	// certificate.
	TimestampIssuer string `json:"timestampIssuer,omitempty"`
	// TimestampError says why the RFC 3161 token found does not verify over
	// what it timestamps; its time is then not trusted as the signing time.
	TimestampError string `json:"timestampError,omitempty"`
	// SubFilter is the signature dictionary's /SubFilter.
	SubFilter string `json:"subFilter,omitempty"`
	// LTVEnabled reports that the document holds enough data to validate the
	// signature offline: a Document Security Store with revocation data and a
	// timestamp that verifies.
	LTVEnabled bool `json:"ltvEnabled,omitempty"`
	// RevocationOK reports that the embedded OCSP responses or CRLs show the
	// signer certificate was not revoked at the signing time.
	RevocationOK bool `json:"revocationOK,omitempty"`
	// RevocationInfo describes the revocation outcome, or says that the
	// Document Security Store holds no OCSP or CRL data.
	RevocationInfo string `json:"revocationInfo,omitempty"`

	// MDP (Modification Detection and Prevention) fields

	// MDPPolicy is the /P permission of the signature's DocMDP or FieldMDP
	// transform, or 0 when it has none.
	MDPPolicy int `json:"mdpPolicy,omitempty"`
	// MDPValid reports that the changes made after signing comply with
	// MDPPolicy.
	MDPValid bool `json:"mdpValid,omitempty"`
	// MDPInfo explains the MDP outcome.
	MDPInfo string `json:"mdpInfo,omitempty"`
}

// GeoTiePoint represents a single tie point mapping PDF coordinates to
// GPS coordinates (latitude/longitude).
// Concurrent reads of a value are safe; a write needs synchronization.
type GeoTiePoint struct {
	// PDFX is the x coordinate of the point in the page's /LPTS array, or 0
	// when the array has no entry for it.
	PDFX float64 `json:"pdfX"`
	// PDFY is the y coordinate of the point in the page's /LPTS array, or 0
	// when the array has no entry for it.
	PDFY float64 `json:"pdfY"`
	// Lat is the latitude of the point, from the /GPTS array.
	Lat float64 `json:"lat"`
	// Lon is the longitude of the point, from the /GPTS array.
	Lon float64 `json:"lon"`
}

// GeoData holds geospatial metadata extracted from a PDF page or image
// that contains a /Measure dictionary with /Subtype /GEO.
// Concurrent reads of a value are safe; a write needs synchronization.
type GeoData struct {
	// CoordinateSystem is the /Type of the /GCS dictionary, such as GEOGCS or
	// PROJCS.
	CoordinateSystem string `json:"coordinateSystem"`
	// EPSG is the EPSG code of the coordinate system, from the /GCS /EPSG
	// entry or its /WKT text, or empty when neither gives one.
	EPSG string `json:"epsg,omitempty"`
	// TiePoints are the points of the /GPTS array, in file order.
	TiePoints []GeoTiePoint `json:"tiePoints"`
	// Bounds is the box [minLon minLat maxLon maxLat] around TiePoints. It is
	// all zeros when there are none.
	Bounds [4]float64 `json:"bounds"`
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
// Concurrent reads of a value are safe; a write needs synchronization.
type ScaleAxis struct {
	// Unit is the unit label, the /U entry of the number format.
	Unit string `json:"unit"`
	// ConversionFactor is the /C entry of the number format, the factor that
	// converts a value into Unit.
	ConversionFactor float64 `json:"conversionFactor"`
	// Decimals is the /D entry of the number format, the number of decimal
	// places to show. It is 0 when the entry is absent.
	Decimals int `json:"decimals"`
}

// Measurement holds the 2D measurement scale extracted from a page Viewport
// (/VP) or a Line/PolyLine annotation that carries a /Measure dictionary with
// /Subtype /RL (Rectilinear). Developers can use the scale axes to convert
// raw PDF point coordinates into real-world physical measurements.
// Concurrent reads of a value are safe; a write needs synchronization.
type Measurement struct {
	// Source says where the scale comes from: "Viewport" for a page viewport,
	// or the annotation subtype, "Line" or "PolyLine".
	Source string `json:"source"`
	// Name is the viewport's /Name or the annotation's /NM, or empty when it
	// has none.
	Name string `json:"name,omitempty"`
	// BBox is the viewport's /BBox or the annotation's /Rect, in default user
	// space.
	BBox [4]float64 `json:"bbox"`
	// ScaleX is the scale along the x axis.
	ScaleX ScaleAxis `json:"scaleX"`
	// ScaleY is the scale along the y axis. It equals ScaleX when the
	// dictionary has no /Y entry.
	ScaleY ScaleAxis `json:"scaleY"`
	// AreaUnit is the unit label for areas, or empty when the dictionary
	// gives none.
	AreaUnit string `json:"areaUnit,omitempty"`
}

// Model3D represents a 3D model stream embedded in a PDF annotation.
// Content is accessed lazily: call Open to obtain an [io.ReadCloser] that
// decodes the stream through the PDF filter pipeline on read. The caller
// must close the returned reader.
// Concurrent reads of a value are safe; a write needs synchronization.
// Open may be called from several goroutines at once, and alongside
// other reads of the Reader that returned the value, while that Reader stays
// open.
type Model3D struct {
	// Name is the annotation's /NM, or model_N, N counting the page's models
	// from 0, when it has none.
	Name string `json:"name"`
	// Subtype is the model format, "U3D" or "PRC".
	Subtype string `json:"subtype"`
	// Open reads the model's decoded stream. Each call returns a new reader.
	Open func(ctx context.Context) (io.ReadCloser, error) `json:"-"`
}

// DSSInfo represents the Document Security Store (/DSS) extracted from
// the document catalog. It contains the embedded validation materials
// needed for Long-Term Validation (LTV).
// Concurrent reads of a value are safe; a write needs synchronization.
type DSSInfo struct {
	// CertCount is the number of certificates in the store.
	CertCount int `json:"certCount"`
	// CRLCount is the number of certificate revocation lists in the store.
	CRLCount int `json:"crlCount"`
	// OCSPCount is the number of OCSP responses in the store.
	OCSPCount int `json:"ocspCount"`
	// VRICount is the number of per-signature validation entries in the
	// store.
	VRICount int `json:"vriCount"`
}

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

// LinearizationParams holds the parameters extracted from a Linearization
// Dictionary found in the first 1024 bytes of a linearized PDF.
// Concurrent reads of a value are safe; a write needs synchronization.
type LinearizationParams struct {
	// FileLength is the /L value: the length of the file in bytes.
	FileLength int64 `json:"fileLength"`
	// HintStreamOffset is the byte offset of the primary hint stream, the
	// first value of /H.
	HintStreamOffset int64 `json:"hintStreamOffset"`
	// HintStreamLength is the length of the primary hint stream in bytes, the
	// second value of /H.
	HintStreamLength int64 `json:"hintStreamLength"`
	// FirstPageObjNum is the /O value: the object number of the first page.
	FirstPageObjNum int `json:"firstPageObjNum"`
	// NumPages is the /N value: the number of pages in the document.
	NumPages int `json:"numPages"`
	// FirstPageEndOffset is the /E value: the byte offset of the end of the
	// first page.
	FirstPageEndOffset int64 `json:"firstPageEndOffset"`
	// FirstPageXRefOffset is the byte offset of the first-page cross-reference
	// section.
	FirstPageXRefOffset int64 `json:"firstPageXRefOffset"`
	// Hints is the decoded Page Offset Hint Table, one entry per page,
	// or nil when the hint stream could not be parsed.
	Hints []PageOffsetEntry `json:"hints,omitempty"`
}

// PageOffsetEntry holds the per-page data decoded from the hint stream's
// Page Offset Hint Table.
// Concurrent reads of a value are safe; a write needs synchronization.
type PageOffsetEntry struct {
	// ObjCount is the number of objects in the page's group.
	ObjCount int `json:"objCount"`
	// PageLength is the length in bytes of the page's objects.
	PageLength int64 `json:"pageLength"`
	// ContentStreamOffset is the byte offset of the page's content stream,
	// relative to the start of the page's group.
	ContentStreamOffset int64 `json:"contentStreamOffset"`
}

// StreamDiagnostic identifies an in-use stream litho cannot fully decode,
// reported by [github.com/gotenberg/litho/pkg/pdf.Reader.UndecodableStreams],
// or whose data litho salvages, reported by
// [github.com/gotenberg/litho/pkg/pdf.Reader.SalvagedStreams]. Filters lists
// the reported filter names in the stream's /Filter chain: for the first, a
// filter name no decoder knows, or a filter whose built-in decoder refuses a
// feature the stream uses; for the second, a filter whose data is damaged.
// Concurrent reads of a value are safe; a write needs synchronization.
type StreamDiagnostic struct {
	// ObjectNum is the object number of the stream.
	ObjectNum int      `json:"objectNum"`
	Filters   []string `json:"filters"`
}

// ColorSpaceUse records a colour space a page's content paints in, reported by
// [github.com/gotenberg/litho/pkg/pdf.Reader.ExtractColorSpaces].
//
// Family is the family of ISO 32000-1, 8.6.3, Table 62, such as "DeviceRGB",
// "ICCBased" or "Separation". It is "" for an image that takes its colour space
// from its JPXDecode data (8.9.5, Table 89) or a space litho cannot read.
//
// Components is how many numbers a colour in the space carries. Alternate is
// nil except in three cases: the base of an Indexed space, the alternate of a
// Separation, DeviceN or ICCBased space when it states one, and the underlying
// space of a Pattern space. Colorants names the colorants of a Separation or
// DeviceN space.
//
// Source is what paints in the space:
//
//   - "operator" for a colour operator. Operator names it: cs, CS, g, G, rg,
//     RG, k or K.
//   - "image" for an image XObject (Do).
//   - "inline-image" (BI).
//   - "shading" for the sh operator or a shading pattern (sh, scn or SCN).
//   - "group" for the blending space of a transparency group (Operator
//     "Group").
//
// Name is the resource the operator named, "" for a family named directly.
// Object is the object number of the image XObject, shading or group dictionary
// it reads, or else of the content stream the operator sits in. It is 0 for the
// page's own content and for a direct object. Count is how many times the
// page's content uses it so.
//
// Concurrent reads of a value are safe; a write needs synchronization.
type ColorSpaceUse struct {
	Family     string         `json:"family"`
	Components int            `json:"components,omitempty"`
	Alternate  *ColorSpaceUse `json:"alternate,omitempty"`
	Colorants  []string       `json:"colorants,omitempty"`
	Source     string         `json:"source"`
	Operator   string         `json:"operator"`
	Name       string         `json:"name,omitempty"`
	Object     int            `json:"object,omitempty"`
	Count      int            `json:"count"`
}
