package models

// RedactRegion names an area of one page whose content is to be removed.
//
// Rect is [llx, lly, urx, ury] in page space, the same shape and order as
// [Annotation.Rect] and an annotation /Rect in ISO 32000-1, Table 166.
// Page is 1-based, like every other page argument in this package.
// RedactRegion values are immutable and safe for concurrent use.
type RedactRegion struct {
	// Page is the 1-based page the region applies to.
	Page int `json:"page"`
	// Rect is [llx, lly, urx, ury] in page space.
	Rect [4]float64 `json:"rect"`
}

// RedactionHit names a place a redacted string still appears.
//
// It is reported rather than silently tolerated: a redaction that cannot
// account for every copy has not redacted the document, so these accompany
// [github.com/gotenberg/litho/pkg/pdf.ErrRedactionIncomplete] to say where the
// survivor is. RedactionHit values are immutable and safe for concurrent use.
type RedactionHit struct {
	// ObjectNum is the object the copy was found in.
	ObjectNum int `json:"objectNum"`
	// Token is the string that survived.
	Token string `json:"token"`
	// Where is "string" for a string, "stream" for a stream's data, decoded or
	// as stored, or "name" for a destination name.
	Where string `json:"where"`
	// Pages lists the 1-based pages whose content draws the object, ascending.
	// It is set only on [RedactReport.CopiesOnOtherPages] and
	// [RedactReport.CopiesOnRedactedPages], which names the one redacted page
	// whose content stream holds the copy.
	Pages []int `json:"pages"`
	// Boxes are the ink boxes [llx lly urx ury], in default user space, of the
	// text runs that spell the copy on the page. It is set only on
	// [RedactReport.CopiesOnRedactedPages], and empty there when the page's
	// content spells the string only in the numbers of operators that draw no
	// text, a coordinate matching a removed date.
	Boxes [][4]float64 `json:"boxes"`
}

// RedactReport describes what a redaction removed, per vector.
//
// Removing content from a page does not remove the copies of it held elsewhere
// in the file, so the report covers both halves: what the pages lost, and what
// the document-wide passes cleared afterwards.
// Concurrent reads of a value are safe; a write needs synchronization.
type RedactReport struct {
	// RemovedText holds the text of each show-text operator removed, one
	// string per operator, in the order the pages drew them. An operator split
	// at its glyphs gives one string per run of removed glyphs.
	RemovedText []string `json:"removedText"`
	// RemovedGlyphBoxes maps a 1-based page number to the box, in user space,
	// each glyph removed from it paints, in the order the page drew them. It
	// lists every glyph, whatever its character, a space among them.
	//
	// A glyph is removed when this box meets a region, so its ink may reach
	// past the region. These boxes are where the page changed for it.
	RemovedGlyphBoxes map[int][][4]float64 `json:"removedGlyphBoxes"`
	// BlankedImages names the images whose covered samples were overwritten:
	// an image XObject by its resource name, an inline image as "(inline)".
	BlankedImages []string `json:"blankedImages"`
	// ImagesBlankedInCopy maps a 1-based page number to the images of
	// BlankedImages that page blanked in a copy, by the resource name it drew
	// them with.
	//
	// Each is also painted, or named, where the regions do not cover the same
	// samples. The covered placement was therefore repointed at a blanked copy
	// of its own, and the image itself kept its samples for the others, so it
	// is still in the file.
	//
	// A covered placement that lay wholly inside a region was removed instead
	// of repointed (see RemovedImages). Its image is listed here all the same,
	// since it too stays in the file.
	//
	// [github.com/gotenberg/litho/pkg/pdf.WithStrictRedaction] refuses such an
	// image instead.
	ImagesBlankedInCopy map[int][]string `json:"imagesBlankedInCopy"`
	// FormsCopied maps a 1-based page number to the form XObjects it now draws
	// a copy of, because an image of ImagesBlankedInCopy was painted inside
	// them. The count covers the form the page draws and each form between it
	// and the image, once per covered placement.
	//
	// The copy names the blanked image where the form named the image. The form
	// itself is unchanged, and every other placement still draws it.
	FormsCopied map[int]int `json:"formsCopied"`
	// RemovedPaths counts the painted paths removed.
	RemovedPaths int `json:"removedPaths"`
	// ClippedPaths counts the painted paths that reached outside the regions:
	// their geometry inside the regions was removed and the rest is painted
	// under a clip that excludes the regions.
	ClippedPaths int `json:"clippedPaths"`
	// RemovedImages counts the image placements a page's own content drew
	// wholly inside a region.
	//
	// They are removed, not only blanked. The blanked samples of a picture
	// drawn as many small images, a logo one run of pixels at a time, would
	// keep its outline on the page in black.
	//
	// An image XObject among them is also in BlankedImages, its samples
	// overwritten in the file. An inline image goes with its operator, and is
	// not.
	RemovedImages int `json:"removedImages"`
	// RemovedAnnotations lists the object numbers of annotations dropped
	// because their rectangle sat inside a region, ascending.
	RemovedAnnotations []int `json:"removedAnnotations"`
	// ClippedAnnotations lists the object numbers of annotations kept with
	// the part inside the regions cut away, ascending (see
	// [github.com/gotenberg/litho/pkg/pdf.WithoutRedactionAnnotationClipping]).
	ClippedAnnotations []int `json:"clippedAnnotations"`
	// DroppedPageEntries names page entries dropped because they held a copy
	// of what was removed, such as /Thumb.
	DroppedPageEntries []string `json:"droppedPageEntries"`
	// RasterisedUnits counts the painting elements that
	// [github.com/gotenberg/litho/pkg/pdf.WithRedactionRasterFallback] drew as
	// images with the regions painted out instead of removing them: the form
	// XObjects and shading paints whose content the regions selected, and
	// those drawn among them that the images took with them.
	RasterisedUnits int `json:"rasterisedUnits"`
	// BakedAnnotations counts the annotations reaching outside the regions
	// that [github.com/gotenberg/litho/pkg/pdf.WithRedactionRasterFallback]
	// drew into their pages and removed.
	//
	// Their look is page content the regions cut. The annotations, with their
	// pop-ups and the text they carry, are gone. They are counted, not named,
	// since the document is written out and reopened, renumbered, once they are
	// drawn.
	BakedAnnotations int `json:"bakedAnnotations"`
	// FontsFullyRemovedOnPage maps a 1-based page number to the object numbers
	// of fonts whose every glyph on that page was removed.
	//
	// Redact reports these fonts rather than refusing, and the report is
	// page-scoped on purpose. The font may still be drawn elsewhere, and its
	// embedded program carries the outlines either way. litho cannot yet
	// re-subset across the three embedded font formats, so a caller that must
	// also strip those outlines is told which programs to deal with.
	FontsFullyRemovedOnPage map[int][]int `json:"fontsFullyRemovedOnPage"`
	// ScrubbedAnnotations, ScrubbedFormFields and ScrubbedOutlineItems list
	// the object numbers the document-wide passes rewrote, ascending.
	ScrubbedAnnotations  []int `json:"scrubbedAnnotations"`
	ScrubbedFormFields   []int `json:"scrubbedFormFields"`
	ScrubbedOutlineItems []int `json:"scrubbedOutlineItems"`
	// RemovedScripts counts the JavaScript the redaction removed: each script
	// action cut out of an action entry, and the document-level
	// /Names/JavaScript tree as one.
	RemovedScripts int `json:"removedScripts"`
	// ScrubbedPrivateData names the applications whose private data the
	// redaction removed from the catalog's /PieceInfo, sorted.
	ScrubbedPrivateData []string `json:"scrubbedPrivateData"`
	// SurvivingCopies names every place a removed string was still found by
	// the verification scan. Non-empty only alongside
	// [github.com/gotenberg/litho/pkg/pdf.ErrRedactionIncomplete].
	SurvivingCopies []RedactionHit `json:"survivingCopies"`
	// CopiesOnOtherPages names the removed strings the content of pages outside
	// the regions still draws, each with the pages that draw it.
	//
	// These are warnings: the redaction succeeded, and the output keeps them.
	// The slice is empty under
	// [github.com/gotenberg/litho/pkg/pdf.WithStrictRedaction], which refuses
	// them instead.
	CopiesOnOtherPages []RedactionHit `json:"copiesOnOtherPages"`
	// CopiesOnRedactedPages names the removed strings a redacted page still
	// draws as visible text outside every region, each with the page and the
	// boxes of the runs that spell it.
	//
	// These are warnings: the redaction succeeded, and the output keeps them.
	// The slice is empty under
	// [github.com/gotenberg/litho/pkg/pdf.WithStrictRedaction], which refuses
	// them instead.
	CopiesOnRedactedPages []RedactionHit `json:"copiesOnRedactedPages"`
	// UnreadableObjects lists objects the verification scan could not decode.
	//
	// A stream it could not read is one it has not proved clean, so these fail
	// the redaction as surely as a hit does.
	UnreadableObjects []int `json:"unreadableObjects"`

	// LossyBlankedImages counts the images of BlankedImages that
	// [github.com/gotenberg/litho/pkg/pdf.WithRedactionJPEGQuality] redrew
	// with JPEG tiles outside the regions: each is now a form XObject drawing
	// a lossless image of the whole picture with JPEG tiles over it.
	LossyBlankedImages int `json:"lossyBlankedImages"`
}
