// Package annot decides which annotation appearances a viewer draws on a page,
// and where (ISO 32000-1, 12.5.5).
//
// [Collect] walks a page's /Annots array in order and returns one
// [Appearance] per annotation a viewer paints: the normal appearance stream
// (/AP /N, or the entry of its subdictionary that /AS selects), the resources
// it runs with, and the matrix of Algorithm 8.1 that places its /BBox on the
// annotation's /Rect. Annotations it leaves out come back as a [Skip] with a
// [Reason]. The package paints nothing: the renderer runs each appearance as
// a form XObject.
//
// An annotation with no appearance stream is skipped, never synthesised.
// Building an appearance from /DA, /V or /C is a remediation matter (see the
// appearance fixers of package remediation), not something a viewer of the
// file as written does.
//
// Optional content (/OC) and the /NoRotate and /NoZoom flags are reported, not
// applied: the caller knows the optional-content configuration and the page's
// device transform.
//
// [Collect] holds no state between calls and is safe for concurrent use with
// distinct or shared documents, as far as [document.Document] reads are.
package annot
