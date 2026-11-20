package models

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
