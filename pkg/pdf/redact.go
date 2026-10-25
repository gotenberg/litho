package pdf

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sort"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/flatten"
	"github.com/gotenberg/litho/pkg/internal/mutations"
	"github.com/gotenberg/litho/pkg/internal/render/glyph"
)

// RedactRegion names an area of one page whose content is to be removed.
//
// Rect is [llx, lly, urx, ury] in page space, the same shape and order as
// [models.Annotation.Rect] and an annotation /Rect in ISO 32000-1, Table 166.
// Page is 1-based, like every other page argument in this package.
// RedactRegion values are immutable and safe for concurrent use.
type RedactRegion struct {
	// Page is the 1-based page the region applies to.
	Page int
	// Rect is [llx, lly, urx, ury] in page space.
	Rect [4]float64
}

// RedactionHit names a place a redacted string still appears.
//
// It is reported rather than silently tolerated: a redaction that cannot
// account for every copy has not redacted the document, so these accompany
// [ErrRedactionIncomplete] to say where the survivor is.
// RedactionHit values are immutable and safe for concurrent use.
type RedactionHit struct {
	// ObjectNum is the object the copy was found in.
	ObjectNum int
	// Token is the string that survived.
	Token string
	// Where is "string" for a string, "stream" for a stream's data, decoded or
	// as stored, or "name" for a destination name.
	Where string
	// Pages lists the 1-based pages whose content draws the object, ascending.
	// It is set only on [RedactReport.CopiesOnOtherPages] and
	// [RedactReport.CopiesOnRedactedPages], which names the one redacted page
	// whose content stream holds the copy.
	Pages []int
	// Boxes are the ink boxes [llx lly urx ury], in default user space, of the
	// text runs that spell the copy on the page. It is set only on
	// [RedactReport.CopiesOnRedactedPages], and empty there when the page's
	// content spells the string only in the numbers of operators that draw no
	// text, a coordinate matching a removed date.
	Boxes [][4]float64
}

// RedactOption configures [Writer.Redact].
type RedactOption func(*redactOpts)

// redactOpts holds what [Writer.Redact] and its [RedactOption] values set.
type redactOpts struct {
	strict bool
	raster bool
	// noClipAnnots refuses the annotations that paint nothing reaching
	// outside the regions ([WithoutRedactionAnnotationClipping]).
	noClipAnnots bool
	// noPixelCheck skips the pixel check ([WithoutRedactionPixelCheck]).
	noPixelCheck bool
	// jpegQuality is [WithRedactionJPEGQuality]'s, 0 for lossless, and
	// jpegQualitySet whether it was given.
	jpegQuality    int
	jpegQualitySet bool
	// outlines selects glyphs by their outlines too
	// ([WithRedactionGlyphOutlines]), nil by their advance boxes alone.
	outlines *redactGlyphOutlines
	// outlineProviders are the substitute fonts [WithRedactionGlyphOutlines]
	// was given.
	outlineProviders []FontProvider
}

// redactGlyphOutlines is what [WithRedactionGlyphOutlines] sets: the
// programs that draw the fonts a document does not embed, nil for none.
type redactGlyphOutlines struct {
	fonts glyph.Provider
}

// glyphOutlines returns the bounder a redaction selects with under s, nil
// when s is nil.
func (s *redactGlyphOutlines) glyphOutlines(doc *document.Document) *mutations.GlyphOutlines {
	if s == nil {
		return nil
	}
	return mutations.NewGlyphOutlines(doc, s.fonts)
}

// WithStrictRedaction makes [Writer.Redact] refuse with
// [ErrRedactionIncomplete] when a removed string is still drawn by a page the
// regions do not cover, or as visible text outside the regions of a page they
// do, instead of reporting it in [RedactReport.CopiesOnOtherPages] or
// [RedactReport.CopiesOnRedactedPages]. Use it when the regions select every
// occurrence of what must go, so a copy elsewhere means the selection missed
// one.
func WithStrictRedaction() RedactOption {
	return func(o *redactOpts) { o.strict = true }
}

// WithRedactionRasterFallback makes [Writer.Redact] rasterise what it would
// otherwise refuse because it cannot remove it exactly: content a form
// XObject draws, an image among it, and a shading paint (sh) whose clip a
// region meets. Those units, and the units drawn among them that an opaque
// image over them must take with it, are drawn instead as opaque images of
// the page at 300 dpi, clipped to the area they cover less the regions, and
// removed from the content; [RedactReport.RasterisedUnits] counts them. The
// images paint nothing inside the regions, and their samples there are the
// paper's colour, so the file keeps nothing of what lay there. Text is never
// rasterised: a form showing text keeps its text as text, the glyphs the
// regions cover removed from it as from the page's own, and everything the
// page draws outside those units stays as it was.
//
// An annotation reaching outside the regions, refused otherwise since an
// annotation is atomic (ISO 32000-1, 12.5.2), is drawn into its page first
// as a viewer draws it (12.5.5, Algorithm 8.1) and removed, with its pop-up
// and the text and geometry it carries: its look is then page content the
// regions cut, and nothing of it marks what lay inside them.
// [RedactReport.BakedAnnotations] counts them. A widget is not, its look
// being its field's value (12.7.3.3), nor a pop-up, an annotation under
// optional content, a NoRotate one on a turned page or one a viewer does not
// display: those still refuse.
//
// Opt-in, because the fallback changes the page outside the regions: the
// rasterised units lose their vector form there, at the image's resolution,
// and show as litho's renderer draws them, and an annotation drawn into the
// page loses its behaviour, its comment and its pop-up. Without it such a page is refused
// with [ErrRedactionRefused], as a redaction that would change more than the
// regions has to be asked for.
//
// An image a form XObject draws inside a region rasterises its form too:
// blanked, it would change every other placement of the form, which the
// redaction refuses. The renderer reads the document, not the edits queued on
// the Writer, so a redaction with the fallback first writes the edits queued
// before it to a temporary file and reopens it, as
// [Writer.FlattenTransparency] does. A later stage that brings back removed
// text makes [Writer.WriteTo] apply the regions again without the fallback,
// refusing such a page rather than compositing a document the renderer cannot
// read.
func WithRedactionRasterFallback() RedactOption {
	return func(o *redactOpts) { o.raster = true }
}

// WithoutRedactionAnnotationClipping makes [Writer.Redact] refuse, with
// [ErrRedactionRefused], every annotation reaching outside the regions,
// instead of keeping those a viewer draws nothing for.
//
// By default a redaction keeps such an annotation. A Link with no appearance
// stream and a border of width 0 loses the part of its activation area the
// regions cover (ISO 32000-1, 12.5.6.5, Table 173): left as one rectangle, it
// becomes the link's /Rect; left as several, they become its /QuadPoints, one
// quadrilateral each, with /Rect their bounding box, which a reader that
// ignores /QuadPoints activates whole. A pop-up that is not open and has no
// appearance stream, whose /Rect only places the window it opens over its
// parent (12.5.6.14), stays as it is. [RedactReport.ClippedAnnotations] lists
// the links cut.
//
// Keeping such an annotation keeps no content of the regions: it paints
// nothing there or anywhere, and a Redact annotation removes the content of
// its area (ISO 32000-2, 12.5.6.23), not the links that lead out of it. The
// text a link names in /A or /Contents is scrubbed of the removed strings as
// every annotation's is, an action written as an object of its own included.
// An annotation that draws anything, a widget, a visible border, a markup
// appearance, still refuses, and so does a link cut on one page that another
// page also lists, since every page listing it would show the cut.
//
// Use it when a link must keep its whole area or go with the region: the
// redaction then refuses rather than change what a link outside the regions
// does.
//
// # Corpus evidence
//
//   - All corpora, the 409 scenarios of the F13 sweep that refused over an
//     annotation reaching outside the region: 48 succeed with the clipping,
//     every one passing the pixel check and the redact-render cell's inside
//     and outside checks by litho's and poppler 26.09.0's rendering; 85
//     with the raster fallback too. 77 reach a later refusal, a structure
//     element or a copy of the removed text.
func WithoutRedactionAnnotationClipping() RedactOption {
	return func(o *redactOpts) { o.noClipAnnots = true }
}

// WithRedactionAnnotationClipping asks for the annotation clipping
// [Writer.Redact] does by default, and changes nothing. It is kept so callers
// written when the clipping was opt-in still compile.
//
// Deprecated: the clipping is the default;
// [WithoutRedactionAnnotationClipping] turns it off.
func WithRedactionAnnotationClipping() RedactOption {
	return func(*redactOpts) {}
}

// WithRedactionGlyphOutlines makes [Writer.Redact] select a glyph by its
// outline as well as by its advance box. By default a glyph is selected when
// the box of its advance and its font's ascent and descent meets a region
// (see [models.TextElement.InkBox]), and an outline reaching past that box,
// a script face's swash or descender, an italic overhang, a combining accent,
// keeps painting inside a region when its glyph lies outside it: ISO 32000-1,
// 9.2.4 lets a glyph's outline paint anywhere in glyph space, its advance
// only moving the text position. With this option a glyph whose outline's
// bounds meet a region is removed too, so nothing a kept glyph paints
// reaches into a region.
//
// The outline is the one litho's renderer draws for the code, through the
// font matrix and the text rendering matrix (9.4.4), its bounds holding the
// outline's control points: from the program the document embeds, or, for a
// font it does not embed, from the substitute fonts supplies, given as
// [WithRenderFontProvider], [WithRenderFontFiles] and [WithRenderSystemFonts]
// give them to [Reader.RenderPage]; any other [RenderOption] among fonts
// changes nothing. A font the document does not embed is drawn by each viewer
// with a program of its own choosing (9.6.2.2), so its outline is the
// substitute's, the one a caller renders with, and with no substitute the
// glyph keeps its advance box. So does a glyph the program cannot draw. A
// Type 3 glyph is bounded by what its description paints either way. The
// removed glyphs' boxes in [RedactReport.RemovedGlyphBoxes] hold their
// outlines, and [Writer.WriteTo], applying the regions again, selects the
// same way.
//
// Opt-in, because it removes more than the default does: a glyph whose
// advance box lies outside a region, and whose text the caller may not have
// meant to select, goes when its outline reaches in.
func WithRedactionGlyphOutlines(fonts ...RenderOption) RedactOption {
	var ro renderOpts
	for _, opt := range fonts {
		if opt != nil {
			opt(&ro)
		}
	}
	providers := ro.fonts
	if ro.systemFonts != nil {
		providers = append(slices.Clip(providers), ro.systemFonts)
	}
	return func(o *redactOpts) {
		// The provider is built at Redact, against the Writer's logger.
		o.outlines = &redactGlyphOutlines{}
		o.outlineProviders = providers
	}
}

// WithRedactionJPEGQuality encodes as baseline JPEG (ITU-T T.81) at quality
// q, 1 to 100, on the scale of the Independent JPEG Group's libjpeg, the
// photographic parts of two kinds of image a redaction writes: the
// photographs it blanks, and the images [WithRedactionRasterFallback] draws.
// Either is cut on a grid of its pixels as [WithFlattenJPEGQuality] cuts a
// flattening's composite: a run of cells whose pixels hold 256 colours and
// that no region reaches is encoded as a JPEG tile unless Flate holds it in
// no more bytes, and the JPEG tiles are drawn over one lossless image of the
// whole, so the image outside the JPEG tiles draws as it does without this
// option. A cell a region reaches, widened by a pixel of a fallback image
// and by two samples of a photograph, always stays lossless, so the samples
// inside the regions are the blank or the paper's
// colour exactly, in the file as on the page: a JPEG spreads each 8 by 8
// block's error over the whole block, and would decode traces of the ink
// beside a region into it.
//
// A blanked photograph is re-encoded only when the source held it as JPEG
// or JPEG 2000, so blanking never turns a lossless picture lossy, and only
// when that saves bytes; [RedactReport.LossyBlankedImages] counts them. The
// picture becomes a form XObject at the image's own object number, so every
// placement and reference finds it there, drawing the lossless image of the
// whole and the JPEG tiles in the picture's colour space. A picture with a
// soft mask, in a colour space JPEG does not code, or drawn so deep in forms
// that the two forms its tiles add would pass the form depth budget, stays
// one lossless image. A later redaction that reaches a JPEG tile blanks it
// in a copy, as it blanks any image a form nested in another form draws (see
// [RedactReport.ImagesBlankedInCopy]), and [WithStrictRedaction] refuses it.
//
// By default every image a redaction writes is lossless under FlateDecode,
// at the cost of size: a blanked JPEG 2000 photograph weighs several times
// its source, and a page of photographs the fallback draws several times its
// JPEG source. A q outside 1 to 100 makes [Writer.Redact] fail with
// [ErrInvalidRedactOptions].
//
// Opt-in, because outside the regions a JPEG tile shows the picture with
// JPEG's loss, which a lossless image does not.
//
// # Corpus evidence
//
//   - litho (all), pdfcpu (all), govdocs (1 in 8) and pdfjs (1 in 16, and
//     issue17289), the four redact-render scenarios with the fallback: 106
//     rasterise, 3 of them reach a photographic tile at q 90, and the
//     outputs weigh 47.7 MB against 55.5 MB lossless (46.2 MB at q 75).
//   - pdfjs issue17289, the band with the fallback, before blanked
//     photographs were re-encoded: 28.8 MB lossless, 21.0 MB at q 90; most
//     of the rest was a JPX image the band blanks. Re-encoded too: 27.0 MB
//     lossless, 11.5 MB at q 90, against a 7.8 MB source.
//   - litho (5 PDFs), pdfcpu (21), pdfjs (190) and govdocs (386), every PDF
//     holding JPEG or JPEG 2000 data, the four redact-render scenarios
//     without the fallback, at q 90: 247 redactions blank 565 photographs
//     that are re-encoded (pdfcpu 8, pdfjs 84, govdocs 155, litho none),
//     and their outputs weigh 491.0 MB against 685.1 MB lossless, from
//     404.2 MB of sources (pdfjs 259.2 against 404.1 from 196.6, govdocs
//     222.6 against 269.4 from 202.4). pdfjs issue17289, the band: 9.0 MB
//     against 16.7 MB.
//   - pdfcpu, pdfjs, govdocs, the 148 PDFs those redactions reach, the
//     redact-render cell, both halves, with and without the fallback, at
//     q 90 against lossless: no outcome changes, no inside leak by either
//     renderer that lossless lacks. Poppler's outside check counts more
//     changed pixels on 156 scenarios, and litho's, which reads every pixel
//     an image paints on a fallback scenario, on 5 of pdfjs issue12752 and
//     govdocs thread_000_000372 and thread_400_400358 (1 to 24 pixels).
//     Of 27 of them, those 5, the 12 poppler counts most on and 10 at
//     random, poppler 26.09.0, PDFium 4.30.0 and litho draw every pixel
//     changed at 72 and 300 dpi inside a JPEG tile or within 2 pixels of
//     its edge. Under the pixel check every redaction that succeeds
//     lossless succeeds at q 90.
//   - pdfjs issue16395 (PDF/A-2b) and issue19494 (PDF/A-3a), the three of
//     their redactions that re-encode a photograph: veraPDF 1.30.2 reports
//     the same rules lossy as lossless.
//   - litho, pdfcpu, pdfjs, govdocs, redact-render with the fallback, before
//     blanked photographs were re-encoded, at q 90 against lossless: no
//     inside leak by either renderer that lossless lacks, and
//     litho's outside check unchanged. Poppler's outside check fails on 2
//     more scenarios, of pdfjs bug1140761 (158 and 177 pixels), and counts
//     more changed pixels on 6 that fail lossless already, of issue12752,
//     issue17289 and issue2642.
//   - pdfjs bug1140761, the quarter and band at q 90 against lossless:
//     PDFium 4.30.0 and poppler 26.09.0 both draw every changed pixel
//     inside a JPEG tile or within 2 pixels of its edge, 140 to 213 at 72
//     dpi, JPEG's loss at the sharp edges of a scanned page, and none in the
//     lossless parts.
func WithRedactionJPEGQuality(q int) RedactOption {
	return func(o *redactOpts) { o.jpegQuality, o.jpegQualitySet = q, true }
}

// RedactReport describes what a redaction removed, per vector.
//
// Removing content from a page does not remove the copies of it held elsewhere
// in the file, so the report covers both halves: what the pages lost, and what
// the document-wide passes cleared afterwards.
// Values must not be shared across goroutines without synchronization.
type RedactReport struct {
	// RemovedText holds the text of each show-text operator removed, one
	// string per operator, in the order the pages drew them. An operator split
	// at its glyphs gives one string per run of removed glyphs.
	RemovedText []string
	// RemovedGlyphBoxes maps a 1-based page number to the box, in user space,
	// each glyph removed from it paints, in the order the page drew them:
	// every glyph, whatever its character, a space among them. A glyph is
	// removed when this box meets a region, so its ink may reach past the
	// region, and these boxes are where the page changed for it.
	RemovedGlyphBoxes map[int][][4]float64
	// BlankedImages names the images whose covered samples were overwritten:
	// an image XObject by its resource name, an inline image as "(inline)".
	BlankedImages []string
	// ImagesBlankedInCopy maps a 1-based page number to the images of
	// BlankedImages that page blanked in a copy, by the resource name it drew
	// them with. Each is also painted, or named, where the regions do not
	// cover the same samples, so the covered placement was repointed at a
	// blanked copy of its own and the image itself kept its samples for the
	// others: it is still in the file. A covered placement that lay wholly
	// inside a region was removed instead of repointed (see RemovedImages),
	// and its image is listed here all the same, since it too stays in the
	// file. [WithStrictRedaction] refuses such an image instead.
	ImagesBlankedInCopy map[int][]string
	// FormsCopied maps a 1-based page number to the form XObjects it now
	// draws a copy of, because an image of ImagesBlankedInCopy was painted
	// inside them: the form the page draws, and each form between it and
	// the image, once per covered placement. The copy names the blanked
	// image where the form named the image; the form itself is unchanged,
	// and every other placement still draws it.
	FormsCopied map[int]int
	// RemovedPaths counts the painted paths removed.
	RemovedPaths int
	// ClippedPaths counts the painted paths that reached outside the regions:
	// their geometry inside the regions was removed and the rest is painted
	// under a clip that excludes the regions.
	ClippedPaths int
	// RemovedImages counts the image placements a page's own content drew
	// wholly inside a region. They are removed, not only blanked: the blanked
	// samples of a picture drawn as many small images, a logo one run of
	// pixels at a time, would keep its outline on the page in black. An image
	// XObject among them is also in BlankedImages, its samples overwritten in
	// the file; an inline image goes with its operator, and is not.
	RemovedImages int
	// RemovedAnnotations lists the object numbers of annotations dropped
	// because their rectangle sat inside a region, ascending.
	RemovedAnnotations []int
	// ClippedAnnotations lists the object numbers of annotations kept with
	// the part inside the regions cut away, ascending (see
	// [WithoutRedactionAnnotationClipping]).
	ClippedAnnotations []int
	// DroppedPageEntries names page entries dropped because they held a copy
	// of what was removed, such as /Thumb.
	DroppedPageEntries []string
	// RasterisedUnits counts the painting elements that
	// [WithRedactionRasterFallback] drew as images with the regions painted
	// out instead of removing them: the form XObjects and shading paints
	// whose content the regions selected, and those drawn among them that the
	// images took with them.
	RasterisedUnits int
	// BakedAnnotations counts the annotations reaching outside the regions
	// that [WithRedactionRasterFallback] drew into their pages and removed:
	// their look is page content the regions cut, and the annotations, with
	// their pop-ups and the text they carry, are gone. They are counted, not
	// named, since the document is written out and reopened, renumbered,
	// once they are drawn.
	BakedAnnotations int
	// FontsFullyRemovedOnPage maps a 1-based page number to the object
	// numbers of fonts whose every glyph on that page was removed.
	//
	// Reported rather than refused, and page-scoped on purpose: the font may
	// still be drawn elsewhere, and its embedded program carries the outlines
	// either way. litho cannot yet re-subset across the three embedded font
	// formats, so a caller that must also strip those outlines is told which
	// programs to deal with.
	FontsFullyRemovedOnPage map[int][]int
	// ScrubbedAnnotations, ScrubbedFormFields and ScrubbedOutlineItems list
	// the object numbers the document-wide passes rewrote, ascending.
	ScrubbedAnnotations  []int
	ScrubbedFormFields   []int
	ScrubbedOutlineItems []int
	// RemovedScripts counts the JavaScript the redaction removed: each script
	// action cut out of an action entry, and the document-level
	// /Names/JavaScript tree as one.
	RemovedScripts int
	// ScrubbedPrivateData names the applications whose private data the
	// redaction removed from the catalog's /PieceInfo, sorted.
	ScrubbedPrivateData []string
	// SurvivingCopies names every place a removed string was still found by
	// the verification scan. Non-empty only alongside
	// [ErrRedactionIncomplete].
	SurvivingCopies []RedactionHit
	// CopiesOnOtherPages names the removed strings the content of pages
	// outside the regions still draws, each with the pages that draw it. These
	// are warnings: the redaction succeeded, and the output keeps them. Empty
	// under [WithStrictRedaction], which refuses them instead.
	CopiesOnOtherPages []RedactionHit
	// CopiesOnRedactedPages names the removed strings a redacted page still
	// draws as visible text outside every region, each with the page and the
	// boxes of the runs that spell it. These are warnings: the redaction
	// succeeded, and the output keeps them. Empty under
	// [WithStrictRedaction], which refuses them instead.
	CopiesOnRedactedPages []RedactionHit
	// UnreadableObjects lists objects the verification scan could not decode.
	// A stream it could not read is one it has not proved clean, so these
	// fail the redaction as surely as a hit does.
	UnreadableObjects []int

	// LossyBlankedImages counts the images of BlankedImages that
	// [WithRedactionJPEGQuality] redrew with JPEG tiles outside the regions:
	// each is now a form XObject drawing a lossless image of the whole
	// picture with JPEG tiles over it.
	LossyBlankedImages int

	// clippedByPage maps a 0-based page index to the annotations cut on that
	// page, which [mutations.CheckRewrittenAnnotationUses] reads.
	clippedByPage map[int][]int
	// removedGlyphOrigins maps a 1-based page number to the origin, in user
	// space, of each glyph removed from the page's own content stream, which
	// the pixel check of [Writer.Redact] finds the drawn glyph by.
	removedGlyphOrigins map[int][][2]float64
	// blankedPhotographs maps the object number of each blanked picture the
	// source held as JPEG or JPEG 2000 to what [WithRedactionJPEGQuality]
	// re-encodes it with.
	blankedPhotographs map[int]mutations.BlankedPhotograph
}

// Redact removes what the given regions cover and then clears the copies of
// that text held elsewhere in the document, refusing rather than half-redacting.
//
// This is the cure for what [Reader.ExtractRedactedText] only diagnoses. Drawing
// a black rectangle over text leaves the bytes in the file and any extractor
// recovers them; true redaction is content removal plus document-wide
// sanitization. Selection is geometric and VISIBILITY-INDEPENDENT: a glyph
// counts when the box it paints meets a region, whatever its colour, its text
// rendering mode, or the optional-content group it sits in, because invisible
// text is exactly what a failed redaction leaves behind.
//
// It runs eagerly rather than queueing, because the caller must learn whether
// the document may be written at all before writing it. It forces a full
// rewrite: verification answers for the file a full rewrite would produce, and
// an incremental update would append the edits while leaving every superseded
// object, the original content stream among them, in the file.
//
// Three ways it refuses, all leaving the [Writer] exactly as it was:
//
//   - [ErrRedactionRefused] when something cannot be removed exactly: a region
//     covering only some of a text run that cannot be split at its glyphs
//     (a run a region covers only partly is otherwise split, the covered
//     glyphs removed and the rest kept where they were), text in a clipping
//     text rendering mode, whose removal would widen the clip for what
//     follows, text showing a code no glyph was read for anywhere on the page,
//     whose glyph cannot be placed, text a Q inside its text object moves
//     for some viewers and not others, a shading paint (sh)
//     whose clip the region meets, a clipping path, a straddling path it
//     cannot cut and clip at the region, a stroke whose width cannot be read,
//     content drawn by a form XObject, an image whose samples cannot be
//     rewritten, or an annotation reaching outside the regions that draws
//     anything (one that draws nothing is cut to the part outside, see
//     [WithoutRedactionAnnotationClipping]). Removing more than was asked, or reporting success over
//     content still in the file, are both worse than refusing.
//     [WithRedactionRasterFallback] rasterises the form XObjects and shading
//     paints instead of refusing them.
//   - [ErrRedactionIncomplete] when the passes succeed but the verification
//     scan still finds a copy it does not tolerate (see below), or could not
//     read an object. The report names
//     every survivor in [RedactReport.SurvivingCopies] so the caller can act on
//     it. The scan looks for each removed run holding at least three letters
//     or digits, and for each such word drawn across shorter runs, as a
//     producer that positions every glyph draws one. A run too short to
//     identify anything, a lone space, letter or digit, is not looked for: it
//     survives in nearly every document whatever was removed. Nor does it
//     read the samples of images, inline ones included, embedded font
//     programs, CMaps or ICC profiles: no reader extracts text from them,
//     and their bytes spell a short word by chance. A stream a page's content, an attachment, a
//     metadata stream, JavaScript or an XFA form also names is read.
//   - [ErrRedactionPixelCheck] when a rendering of a redacted page does not
//     show what the regions promise (see below).
//
// A copy that only pages outside the regions draw does not fail the redaction.
// A region redaction removes what the region covers on its own page, as a
// Redact annotation applies to the page it sits on (ISO 32000-2, 12.5.6.23),
// and the same text drawn on another page is content the caller did not
// select. Such a copy, in a content stream, a form XObject, a tiling pattern,
// a Type 3 glyph or an annotation appearance that another page draws and no
// redacted page draws or names, is reported in [RedactReport.CopiesOnOtherPages]
// with the pages that draw it, and the output keeps it. A copy anywhere else,
// the redacted page's own resources and annotations, the structure tree, the
// metadata or any other object, still refuses. [WithStrictRedaction] refuses
// copies on other pages too.
//
// Nor does a copy the redacted page draws as visible text outside every
// region. The region is the content the redaction removes (ISO 32000-2,
// 12.5.6.23), and the same characters shown again elsewhere on the page, a
// running head or a repeated word, are text the caller did not select. Such a
// copy is reported in [RedactReport.CopiesOnRedactedPages] with the page and
// the boxes of the runs that spell it, and the output keeps it. So is a
// removed run of digits the page's content spells only in the numbers of
// operators that draw no text (a path, a matrix, a colour): ISO 32000-1,
// 9.10.1 reads text from the strings of show-text operators alone. It counts
// only when the page's own content stream holds the string in nothing but
// show-text operators whose every glyph paints (no text rendering mode 3 or
// 7, no paint only in device white), lies inside the clipping path and the
// crop box, sits in no optional-content group that is off, and meets no
// region: a copy no viewer shows, or one behind a region, is what a failed
// redaction leaves, and still refuses, as does a copy a form XObject or an
// annotation of the redacted page draws. The page is also drawn as written,
// at 72 dpi with and without its text over the copies' boxes, and a copy
// that changes no pixel of its own, one a later fill, image or text paints
// over, refuses as well; so does every copy on a page that cannot be drawn.
// That drawing runs under [WithoutRedactionPixelCheck] too.
// [WithStrictRedaction] refuses these copies too.
//
// The verification compares text, not pixels, so a pixel check follows it.
// Redact renders each redacted page before and after at [RedactionCheckDPI],
// and refuses with a [*RedactionPixelError] unless the regions show only
// paper and blanked images and nothing outside them changed; see
// [WithoutRedactionPixelCheck], which skips it, for what it proves and costs.
//
// The document holds copies outside its pages too. Each home is cleared,
// kept for the scan to judge, or left to the rewrite, as follows:
//
//   - Annotation values, form field values, outline titles and the structure
//     tree's text are scrubbed of the removed strings. Document metadata goes:
//     /Info is emptied and the catalog's XMP removed.
//   - The redacted page's /Thumb, /PieceInfo and /Metadata go: a thumbnail, an
//     application's copy of the page, and an XMP packet that can carry a page
//     thumbnail as base64 (ISO 32000-1, 12.3.4, 14.5 and 14.3.2). A blanked
//     image is rewritten without its /Alternates, /OPI, /ID, /Metadata, /AF
//     and /PtData, which picture or describe the samples it had, and keeps
//     its /OC, /Interpolate, /Intent, /Name and /Measure, which say where
//     and how it paints, so a hidden image stays hidden. A blanked image
//     keeps its /StructParent where it is blanked in place, and a copy
//     blanked for one placement carries none: its structure element names
//     the original (14.7.4.4).
//   - JavaScript goes, wherever an action entry or the /Names/JavaScript tree
//     holds it (ISO 32000-1, 12.6.4.16): a script builds a word at run time
//     from pieces no comparison of bytes finds. Form calculations and custom
//     navigation go with it; see [RedactReport.RemovedScripts].
//   - The private data of the catalog's /PieceInfo goes, its dictionaries and
//     their /LastModified staying (ISO 32000-1, 14.5): an application's own
//     format, an embedded search index among them, that the scan cannot read.
//     See [RedactReport.ScrubbedPrivateData].
//   - Signatures leave the form, /Perms and the /DSS, their appearances drawn
//     into the pages, as every full rewrite does (see
//     [Writer.DroppedSignatures]), so a copy in a signature dictionary or a
//     certificate never reaches the output.
//   - Embedded files and their /Collection, named destinations, and every
//     other object stay, and the scan compares them: a copy there refuses.
//     Removing an attachment or renaming a destination would delete structure
//     the caller can see, and an attachment holding no copy is kept. The scan
//     reads text spelled in UTF-16 either way round, and into an attached PDF,
//     zip or gzip; a container it cannot open, encrypted or nested past its
//     bound, refuses as an unreadable object does.
//
// Regions on several pages are applied together. Passing none is a no-op.
//
// Everything outside the regions stays where it was. Text after a removed run
// on the same line keeps its position, because the run's advance is put back
// in its place. A painted path reaching outside the regions loses its geometry
// inside them, and the rest is painted under a clip that excludes them (see
// [RedactReport.ClippedPaths]). An image is blanked by rewriting it, and an
// image also painted somewhere the regions do not cover the same samples, on
// another page, twice on one page, or by an annotation or a form, is blanked
// in a copy instead: the covered placement paints the copy under a new
// resource name, and every other placement keeps the image as it was (see
// [RedactReport.ImagesBlankedInCopy]). [WithStrictRedaction] refuses such an
// image. An image a form XObject draws on the page is blanked in place when
// every placement of it had the same samples covered. One also painted
// elsewhere is blanked in a copy too: the form, and each form nested between
// it and the image, is copied for the covered placement, the copy names the
// blanked image, and the page draws the copy, while every other placement
// draws the form as it was (see [RedactReport.FormsCopied]). A placement the
// page's own content draws wholly inside a region is removed, with the
// q ... Q group that placed it when that group paints nothing else, so the
// paper shows where it was (see [RedactReport.RemovedImages]): blanked, it
// would paint its outline in black, and a picture drawn as many small
// images would keep its shape. Its image is still blanked in place when no
// placement the regions do not cover paints it, since the file keeps it.
//
// The redaction holds whatever else the Writer does before or after it:
// another Redact, auto-tagging, and a remediation, which comes after it since a
// remediation is the last edit on a Writer (a Redact after one returns
// [ErrEditAfterRemediation]). Each builds on the others' edits,
// and [Writer.WriteTo] scans the output it is about to write for every removed
// string. A stage can still rebuild a page from the source document
// (auto-tagging marks up the original content stream), so when a removed
// string, or an image blanked in a copy, is back, WriteTo applies the regions
// again on top of everything else and scans once more. If a copy still survives, WriteTo refuses with
// [ErrRedactionIncomplete] and writes nothing.
//
// An inline image is blanked in place: a new inline image with the covered
// samples overwritten replaces the bytes between BI and EI (ISO 32000-1,
// 8.9.7), and [RedactReport.BlankedImages] names it "(inline)". One whose
// data readers end at different bytes, whose dictionary writes a key both
// abbreviated and in full or holds one ISO 32000-1, Table 93 does not list,
// or whose colour space only an image XObject could name is refused instead.
//
// What this does NOT do: re-subset embedded fonts whose glyphs now describe
// only removed text (see [RedactReport.FontsFullyRemovedOnPage]), and it
// cannot remove content a form XObject draws, which it refuses instead. Nor
// does it find a copy no comparison of text recognises: text an image or a
// thumbnail inside an attachment draws, or an attachment in
// a format whose compression the scan does not know.
func (w *Writer) Redact(ctx context.Context, regions []RedactRegion, opts ...RedactOption) (_ *RedactReport, err error) {
	defer recoverToError(w.logger(), "Writer.Redact", &err)

	if w.closed {
		return nil, ErrWriterClosed
	}
	if w.consumed {
		return nil, ErrWriterConsumed
	}
	if err := w.refuseAfterRemediation("Redact"); err != nil {
		return nil, err
	}
	if len(regions) == 0 {
		return &RedactReport{}, nil
	}
	var o redactOpts
	for _, opt := range opts {
		opt(&o)
	}
	if o.jpegQualitySet && (o.jpegQuality < 1 || o.jpegQuality > 100) {
		return nil, fmt.Errorf("%w: JPEG quality %d, outside 1 to 100", ErrInvalidRedactOptions, o.jpegQuality)
	}

	if !o.raster {
		return w.redact(ctx, regions, o)
	}
	// The renderer the fallback composites with reads the document, not the
	// edits queued on it, so those are written out first.
	undo, err := w.materialize(ctx)
	if err != nil {
		return nil, err
	}
	baked, undoBake, err := w.bakeRedactedAnnotations(ctx, regions)
	if err != nil {
		undo()
		return nil, err
	}
	report, err := w.redact(ctx, regions, o)
	if err != nil {
		undoBake()
		undo()
		return report, err
	}
	report.BakedAnnotations = baked
	return report, nil
}

// bakeRedactedAnnotations draws into the pages the regions reach the
// annotations reaching outside them that [flatten.RedactionAnnotations]
// draws, removes those annotations, and, when it drew any, writes the
// document out and reopens it, so the renderer the raster fallback composites
// with reads the drawing. It returns how many it removed; undo restores the
// Writer as it was before.
func (w *Writer) bakeRedactedAnnotations(ctx context.Context, regions []RedactRegion) (int, func(), error) {
	noop := func() {}
	if err := w.promotePageTree(ctx); err != nil {
		return 0, noop, err
	}
	doc := w.reader.doc
	pages, err := w.reader.resolvePages(ctx)
	if err != nil {
		return 0, noop, fmt.Errorf("resolving pages: %w", err)
	}
	byPage, order, err := groupRedactRegions(regions, len(pages))
	if err != nil {
		return 0, noop, err
	}
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))
	if err := ctx.Err(); err != nil {
		return 0, noop, err
	}
	seedAllocator(alloc, w.rewriteOverrides)
	baked := 0
	rewritten := make(map[int][]int)
	for _, pageNum := range order {
		bake, bakeErr := flatten.RedactionAnnotations(ctx, alloc, doc, pages[pageNum-1], byPage[pageNum])
		if bakeErr != nil {
			if ctxErr := ctx.Err(); ctxErr != nil && errors.Is(bakeErr, ctxErr) {
				return 0, noop, bakeErr
			}
			return 0, noop, fmt.Errorf("%w: page %d: %w", ErrRedactionRefused, pageNum, bakeErr)
		}
		baked += bake.Removed
		if len(bake.Rewritten) > 0 {
			rewritten[pageNum-1] = bake.Rewritten
		}
	}
	if baked == 0 {
		return 0, noop, nil
	}
	// An annotation drawn into one page is emptied or removed, which every
	// other page listing it would show.
	if useErr := mutations.CheckRewrittenAnnotationUses(ctx, alloc, doc, pages, rewritten); useErr != nil {
		return 0, noop, fmt.Errorf("%w: %w", ErrRedactionRefused, useErr)
	}
	prevOverrides, prevFolded, prevForce := w.rewriteOverrides, w.folded, w.forceRewrite
	w.rewriteOverrides = alloc.Pending()
	w.folded = len(w.mutations)
	w.forceRewrite = true
	undo, err := w.materializeNow(ctx)
	if err != nil {
		w.rewriteOverrides, w.folded, w.forceRewrite = prevOverrides, prevFolded, prevForce
		return 0, noop, err
	}
	return baked, func() {
		undo()
		w.rewriteOverrides, w.folded, w.forceRewrite = prevOverrides, prevFolded, prevForce
	}, nil
}

// redact applies regions as [Writer.Redact] describes, with the raster
// fallback when o asks for it.
func (w *Writer) redact(ctx context.Context, regions []RedactRegion, o redactOpts) (*RedactReport, error) {
	// The pixel check compares the redacted pages with the pages as staged
	// before the redaction, which must be rendered from a document the
	// redaction's passes have not started on.
	var before *Reader
	if !o.noPixelCheck {
		staged, release, stagedErr := w.stagedReader(ctx)
		if stagedErr != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, &RedactionPixelError{Err: fmt.Errorf("writing the staged document: %w", stagedErr)}
		}
		defer release()
		before = staged
	}

	if err := w.promotePageTree(ctx); err != nil {
		return nil, err
	}
	doc := w.reader.doc
	pages, err := w.reader.resolvePages(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolving pages: %w", err)
	}

	byPage, order, err := groupRedactRegions(regions, len(pages))
	if err != nil {
		return nil, err
	}

	alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))
	// A done ctx can stop the raw-byte augmentation short, and the allocator
	// then starts below objects the file defines.
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Start from what earlier stages produced, remediation or an earlier
	// Redact, and replay whatever an earlier stage queued, so the passes below
	// read the document as the caller has staged it rather than the untouched
	// source. Starting empty replaced those stages' work: a second Redact
	// dropped the first, and a Redact after remediation undid its fixes. The
	// mutations the overrides already hold are not applied again, and the rest
	// stay queued, marked folded once the overrides hold them (see
	// [Writer.folded]): a refusal has to be able to put the Writer back as it
	// was.
	seedAllocator(alloc, w.rewriteOverrides)
	for i, m := range w.unfoldedMutations() {
		if applyErr := m.apply(ctx, alloc, doc); applyErr != nil {
			return nil, fmt.Errorf("applying staged mutation %d before redaction: %w", w.folded+i, applyErr)
		}
	}
	// What the passes below queue is told apart from what the stages above
	// staged, which is pending too.
	staged := alloc.Writes()

	// ClearMetadata empties /Info in place on the document the Writer edits,
	// so a refusal after it has run would otherwise persist on a Writer the
	// caller was told to treat as untouched.
	preTrailer := make(ast.PdfDict, len(doc.Index.Trailer))
	maps.Copy(preTrailer, doc.Index.Trailer)
	restoreTrailer := func() { doc.Index.Trailer = preTrailer }

	pageOpts := mutations.RedactOptions{ClipAnnotations: !o.noClipAnnots}
	if o.outlines != nil {
		o.outlines.fonts = renderFontProvider(w.logger(), o.outlineProviders)
		pageOpts.GlyphOutlines = o.outlines.glyphOutlines(doc)
	}
	if o.raster {
		pageOpts.Rasterise = flatten.Redaction
		if o.jpegQuality > 0 {
			pageOpts.Rasterise = flatten.RedactionJPEG(o.jpegQuality)
		}
	}
	report, copied, err := redactPages(ctx, alloc, doc, pages, byPage, order, o.strict, pageOpts)
	if err != nil {
		restoreTrailer()
		return nil, err
	}
	// An annotation is one object however many pages list it, so removing it
	// from one page is exact only when no other page still shows it. That
	// also needs every page's regions applied first.
	if useErr := mutations.CheckRemovedAnnotationUses(ctx, alloc, doc, pages, report.RemovedAnnotations); useErr != nil {
		restoreTrailer()
		return nil, fmt.Errorf("%w: %w", ErrRedactionRefused, useErr)
	}
	if useErr := mutations.CheckRewrittenAnnotationUses(ctx, alloc, doc, pages, report.clippedByPage); useErr != nil {
		restoreTrailer()
		return nil, fmt.Errorf("%w: %w", ErrRedactionRefused, useErr)
	}
	// Once every placement is blanked: a picture blanked in place on two
	// pages is one object, whose tiles must avoid what either region reaches.
	if o.jpegQuality > 0 && len(report.blankedPhotographs) > 0 {
		lossy, encErr := mutations.EncodeBlankedPhotographs(ctx, alloc, doc, report.blankedPhotographs, o.jpegQuality)
		if encErr != nil {
			restoreTrailer()
			return nil, fmt.Errorf("re-encoding blanked photographs: %w", encErr)
		}
		report.LossyBlankedImages = lossy
	}

	// Nothing was selected, so there is nothing to sanitize and nothing to
	// write. Leaving the Writer untouched keeps a redaction that removed
	// nothing from forcing a rewrite of the whole file.
	//
	// The passes queued nothing when the allocator took no write after the
	// staged edits. Testing for an empty pending map instead read every staged
	// edit as a removal: a redaction that selected nothing after a remediation
	// went on to clear the metadata, and the output lost the XMP packet ISO
	// 19005-2, 6.6.2.1 requires.
	if len(report.RemovedText) == 0 && alloc.Writes() == staged {
		restoreTrailer()
		// Removing nothing is no proof the regions show nothing: content the
		// passes do not see, a Type 3 glyph painting a shading far past the
		// box its font states among it, still paints there. The page as
		// staged is the page the caller writes, so it is checked against
		// itself.
		if !o.noPixelCheck {
			if checkErr := checkRedactionPixels(ctx, before, before, regions, order, redactedGlyphs{}); checkErr != nil {
				return nil, checkErr
			}
		}
		return report, nil
	}

	// Metadata needs no scrubber of its own: ClearMetadata already empties
	// /Info and drops the catalog's XMP. Composing it here is what the
	// verification below depends on, since /Info is the usual home no
	// token-driven pass reaches.
	if metaErr := mutations.ClearMetadata(ctx, alloc, doc); metaErr != nil {
		restoreTrailer()
		return nil, fmt.Errorf("%w: clearing metadata: %w", ErrRedactionRefused, metaErr)
	}
	if vecErr := clearOpaqueCopies(ctx, alloc, doc, report); vecErr != nil {
		restoreTrailer()
		return nil, fmt.Errorf("%w: %w", ErrRedactionRefused, vecErr)
	}

	// The scrubs and the scan look for the removed runs that can identify
	// what was removed, not for every run: a lone space or digit survives in
	// nearly every document, and refusing over it refuses over nothing (see
	// mutations.RedactionNeedles).
	needles := mutations.RedactionNeedles(report.RemovedText)
	sanitized, sanErr := mutations.SanitizeDocument(ctx, alloc, doc, needles)
	report.ScrubbedAnnotations = sanitized.Annotations
	report.ScrubbedFormFields = sanitized.FormFields
	report.ScrubbedOutlineItems = sanitized.OutlineItems
	for _, hit := range sanitized.Scan.Hits {
		report.SurvivingCopies = append(report.SurvivingCopies,
			RedactionHit{ObjectNum: hit.ObjectNum, Token: hit.Token, Where: hit.Where})
	}
	report.UnreadableObjects = sanitized.Scan.Unreadable

	if sanErr != nil && !o.strict && errors.Is(sanErr, mutations.ErrSanitizationIncomplete) {
		others, samePage, tolerable, splitErr := w.tolerableCopies(ctx, alloc, pages, byPage, sanitized.Scan)
		if splitErr != nil {
			restoreTrailer()
			return nil, fmt.Errorf("telling copies on other pages apart: %w", splitErr)
		}
		if tolerable {
			report.SurvivingCopies = nil
			report.CopiesOnOtherPages = others
			report.CopiesOnRedactedPages = samePage
			sanErr = nil
		}
	}

	if sanErr != nil {
		// Fail closed at the boundary. SanitizeDocument leaves correct edits
		// queued so a survivor can be named, but a caller that ignored the
		// error would then write a half-sanitized document, so the Writer is
		// restored here and the report carries the diagnosis instead.
		restoreTrailer()
		return report, fmt.Errorf("%w: %w", ErrRedactionIncomplete, sanErr)
	}

	// Seed via rewriteOverrides only. Queueing a mutation that replayed the
	// pending map worked incrementally, where source and destination numbering
	// agree, but stomped the copier's destination allocations during a full
	// rewrite and produced files whose trailer pointed at the wrong object.
	prevOverrides, prevFolded, prevForce := w.rewriteOverrides, w.folded, w.forceRewrite
	prevRedactions, prevTokens, prevTolerances := len(w.redactions), len(w.redactedTokens), len(w.redactionTolerances)
	prevOutlines := len(w.redactionOutlines)
	w.rewriteOverrides = alloc.Pending()
	w.folded = len(w.mutations)
	w.forceRewrite = true
	w.recordRedaction(regions, needles, o, copied, report.CopiesOnRedactedPages)
	w.redactionOutlines = append(w.redactionOutlines, o.outlines)
	judgeCopies := slices.ContainsFunc(report.CopiesOnRedactedPages, func(h RedactionHit) bool { return len(h.Boxes) > 0 })
	if judgeCopies || !o.noPixelCheck {
		var check *redactedOutputCheck
		if !o.noPixelCheck {
			check = &redactedOutputCheck{
				before: before, regions: regions, order: order,
				removed: redactedGlyphs{boxes: report.RemovedGlyphBoxes, origins: report.removedGlyphOrigins},
			}
		}
		unseen, checkErr := w.checkRedactedOutput(ctx, report.CopiesOnRedactedPages, check)
		if checkErr != nil {
			w.rewriteOverrides, w.folded, w.forceRewrite = prevOverrides, prevFolded, prevForce
			w.redactions = w.redactions[:prevRedactions]
			w.redactedTokens = w.redactedTokens[:prevTokens]
			w.redactionTolerances = w.redactionTolerances[:prevTolerances]
			w.redactionCopies = w.redactionCopies[:prevRedactions]
			w.redactionClips = w.redactionClips[:prevRedactions]
			w.redactionOutlines = w.redactionOutlines[:prevOutlines]
			restoreTrailer()
			if len(unseen) > 0 {
				report.SurvivingCopies = unseen
				report.CopiesOnOtherPages, report.CopiesOnRedactedPages = nil, nil
				return report, checkErr
			}
			return nil, checkErr
		}
	}
	return report, nil
}

// redactedOutputCheck is what the pixel check of [Writer.Redact] compares the
// written pages against.
type redactedOutputCheck struct {
	before  *Reader
	regions []RedactRegion
	order   []int
	removed redactedGlyphs
}

// checkRedactedOutput writes the Writer as [Writer.WriteTo] would and
// reopens it. It refuses with [ErrRedactionIncomplete] when a copy of
// samePage that the drawing of its page does not show remains, returning
// those copies (see [unseenSamePageCopies]), and then, when pixels is not
// nil, runs the pixel check of [Writer.Redact] on the pages of pixels.order
// against pixels.before.
func (w *Writer) checkRedactedOutput(ctx context.Context, samePage []RedactionHit, pixels *redactedOutputCheck) ([]RedactionHit, error) {
	after, release, err := w.writeAndReopen(ctx, "litho-redact-after-*.pdf")
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if pixels == nil {
			// The copies cannot be drawn, so none is proved seen.
			return slices.Clone(samePage), fmt.Errorf("%w: writing the redacted document to draw the copies left on its pages: %w", ErrRedactionIncomplete, err)
		}
		return nil, &RedactionPixelError{Err: fmt.Errorf("writing the redacted document: %w", err)}
	}
	defer release()
	unseen, err := unseenSamePageCopies(ctx, after, samePage)
	if err != nil {
		return nil, err
	}
	if len(unseen) > 0 {
		return unseen, fmt.Errorf("%w: %d copies of removed text on the redacted pages that no drawing of the page shows", ErrRedactionIncomplete, len(unseen))
	}
	if pixels == nil {
		return nil, nil
	}
	return nil, checkRedactionPixels(ctx, pixels.before, after, pixels.regions, pixels.order, pixels.removed)
}

// redactPages applies the regions of byPage to each page of order, and
// returns the report and the images it blanked in a copy rather than in place.
//
// Blanking rewrites an image in place, so it is exact only when every place
// that paints the image had the same samples covered. That needs every page's
// regions at once, so it is checked after all pages are redacted. An image
// that fails it is blanked in a copy instead: the pages are redacted again
// from the state before the first pass, with the covered placements of those
// images repointed at copies of their own (see
// mutations.RedactOptions.PrivateCopies). The image keeps its samples wherever
// else it is painted, which is content no region selected.
//
// Under strict, a shared image refuses instead: [WithStrictRedaction] says the
// regions select every occurrence of what must go, and the same samples
// painted where no region covers them are one the selection missed.
func redactPages(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	pages []*document.Page,
	byPage map[int][][4]float64,
	order []int,
	strict bool,
	pageOpts mutations.RedactOptions,
) (*RedactReport, map[int]bool, error) {
	snap := alloc.Snapshot()
	var copied map[int]bool
	for {
		report := &RedactReport{}
		var blanked, removed []int
		for _, pageNum := range order {
			opts := pageOpts
			opts.PrivateCopies = copied
			pageReport, redactErr := mutations.RedactPageRegionsWith(
				ctx, alloc, doc, pages[pageNum-1], byPage[pageNum], opts)
			if redactErr != nil {
				alloc.Restore(snap)
				return nil, nil, fmt.Errorf("%w: page %d: %w", ErrRedactionRefused, pageNum, redactErr)
			}
			mergePageRedaction(report, pageNum, pageReport)
			blanked = appendNewObjects(blanked, pageReport.BlankedImageObjects)
			removed = append(removed, pageReport.RemovedImageObjects...)
		}

		// A second pass blanks in place only the images the first found
		// exclusive, on the same pages and regions, so a failure there is one
		// a copy cannot lift and refuses as before.
		if strict || copied != nil {
			if useErr := mutations.CheckBlankedImageUses(ctx, alloc, doc, pages, byPage, blanked, removed); useErr != nil {
				alloc.Restore(snap)
				return nil, nil, fmt.Errorf("%w: %w", ErrRedactionRefused, useErr)
			}
			return report, copied, nil
		}
		shared, useErr := mutations.SharedBlankedImages(ctx, alloc, doc, pages, byPage, blanked, removed)
		if useErr != nil {
			alloc.Restore(snap)
			return nil, nil, fmt.Errorf("%w: %w", ErrRedactionRefused, useErr)
		}
		if len(shared) == 0 {
			return report, nil, nil
		}
		alloc.Restore(snap)
		copied = make(map[int]bool, len(shared))
		for _, objNum := range shared {
			copied[objNum] = true
		}
	}
}

// tolerableCopies reports whether every hit of scan is a copy the redaction
// tolerates, and names those copies when it is: a copy only pages outside
// byPage draw (see mutations.SplitOtherPageCopies), and a copy a redacted page
// draws as visible text outside every region (see
// mutations.SplitSamePageCopies). An object the scan could not read is not
// proved to be either, so any makes it false.
func (w *Writer) tolerableCopies(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	pages []*document.Page,
	byPage map[int][][4]float64,
	scan mutations.ScanResult,
) (others, samePage []RedactionHit, ok bool, err error) {
	if len(scan.Unreadable) > 0 || len(scan.Hits) == 0 {
		return nil, nil, false, nil
	}
	redacted := make(map[int]bool, len(byPage))
	for pageNum := range byPage {
		redacted[pageNum] = true
	}
	copies, rest, err := mutations.SplitOtherPageCopies(ctx, alloc, w.reader.doc, pages, redacted, scan.Hits)
	if err != nil {
		return nil, nil, false, err
	}
	same, rest, err := mutations.SplitSamePageCopies(ctx, alloc, w.reader.doc, pages, byPage, rest)
	if err != nil {
		return nil, nil, false, fmt.Errorf("telling copies on the redacted pages apart: %w", err)
	}
	if len(rest) > 0 {
		return nil, nil, false, nil
	}
	for _, c := range copies {
		others = append(others, RedactionHit{ObjectNum: c.Hit.ObjectNum, Token: c.Hit.Token, Where: c.Hit.Where, Pages: c.Pages})
	}
	for _, c := range same {
		samePage = append(samePage, RedactionHit{ObjectNum: c.Hit.ObjectNum, Token: c.Hit.Token, Where: c.Hit.Where, Pages: []int{c.Page}, Boxes: c.Boxes})
	}
	return others, samePage, true, nil
}

// seedAllocator queues every override at its own object number, so a stage
// allocating on top of them builds on the work an earlier stage produced.
// Object numbers are visited in order so the same Writer state always drives
// the same sequence of edits.
func seedAllocator(alloc *writer.ObjAllocator, overrides map[int]ast.PdfObject) {
	for _, objNum := range slices.Sorted(maps.Keys(overrides)) {
		alloc.AllocAt(objNum, overrides[objNum])
	}
}

// recordRedaction remembers a successful redaction so [Writer.WriteTo] can
// check the composed output against it. needles are the removed runs the
// output is verified against (see mutations.RedactionNeedles).
//
// A redaction that tolerated copies on other pages also records the pages it
// covered and its needles, so WriteTo tolerates the same copies and no others.
//
// copied holds the images the redaction blanked in a copy rather than in
// place, which WriteTo checks the composed pages for.
func (w *Writer) recordRedaction(regions []RedactRegion, needles []string, o redactOpts, copied map[int]bool, samePage []RedactionHit) {
	w.redactions = append(w.redactions, slices.Clone(regions))
	w.redactionCopies = append(w.redactionCopies, copied)
	w.redactionClips = append(w.redactionClips, !o.noClipAnnots)
	w.redactedTokens = appendNewTokens(w.redactedTokens, needles)
	if o.strict {
		return
	}
	tolerance := redactionTolerance{
		pages:   make(map[int]bool, len(regions)),
		regions: make(map[int][][4]float64, len(regions)),
		tokens:  make(map[string]bool, len(needles)),
		seen:    make(map[string]bool, len(samePage)),
	}
	for _, c := range samePage {
		if len(c.Boxes) > 0 && len(c.Pages) > 0 {
			tolerance.seen[seenCopyKey(c.Pages[0], c.Token, c.Boxes)] = true
		}
	}
	for _, region := range regions {
		tolerance.pages[region.Page] = true
		tolerance.regions[region.Page] = append(tolerance.regions[region.Page], region.Rect)
	}
	for _, tok := range needles {
		tolerance.tokens[tok] = true
	}
	w.redactionTolerances = append(w.redactionTolerances, tolerance)
}

// redactionTolerance is what one non-strict [Writer.Redact] call lets survive:
// a copy of one of its tokens that only pages outside its pages draw, and one
// its pages draw as visible text outside its regions. seen holds the copies of
// the second kind Redact drew and found shown ([seenCopyKey]): WriteTo cannot
// draw the document it is writing, so it tolerates the same copies at the
// same places and no others.
type redactionTolerance struct {
	pages   map[int]bool
	regions map[int][][4]float64
	tokens  map[string]bool
	seen    map[string]bool
}

// untolerated drops from scan the hits some recorded redaction tolerates (see
// [redactionTolerance]), and returns what is left.
func (w *Writer) untolerated(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	pages []*document.Page,
	scan mutations.ScanResult,
) (mutations.ScanResult, error) {
	rest := scan.Hits
	for _, tolerance := range w.redactionTolerances {
		var mine, others []mutations.TokenHit
		for _, hit := range rest {
			if tolerance.tokens[hit.Token] {
				mine = append(mine, hit)
			} else {
				others = append(others, hit)
			}
		}
		if len(mine) == 0 {
			continue
		}
		_, kept, err := mutations.SplitOtherPageCopies(ctx, alloc, w.reader.doc, pages, tolerance.pages, mine)
		if err != nil {
			return mutations.ScanResult{}, err
		}
		same, kept, err := mutations.SplitSamePageCopies(ctx, alloc, w.reader.doc, pages, tolerance.regions, kept)
		if err != nil {
			return mutations.ScanResult{}, err
		}
		for _, c := range same {
			if len(c.Boxes) > 0 && !tolerance.seen[seenCopyKey(c.Page, c.Hit.Token, c.Boxes)] {
				kept = append(kept, c.Hit)
			}
		}
		others = append(others, kept...)
		rest = others
	}
	return mutations.ScanResult{Hits: rest, Unreadable: scan.Unreadable}, nil
}

// appendNewObjects appends each object number of add that objs does not hold
// yet.
func appendNewObjects(objs, add []int) []int {
	for _, objNum := range add {
		if !slices.Contains(objs, objNum) {
			objs = append(objs, objNum)
		}
	}
	return objs
}

// appendNewTokens appends each token of add that tokens does not hold yet.
func appendNewTokens(tokens, add []string) []string {
	for _, tok := range add {
		if !slices.Contains(tokens, tok) {
			tokens = append(tokens, tok)
		}
	}
	return tokens
}

// enforceRedactions checks the output [Writer.WriteTo] is about to write
// against every recorded redaction, and returns the overrides to write.
//
// A redaction lives in the rewrite overrides, but a stage can still rebuild a
// page from the source document around it: auto-tagging marks up the original
// content stream, whether queued before or after Redact or run as a
// remediation fixer, and its /Contents then carries the text Redact removed.
// So the composed output is scanned for every removed token. When one is
// back, the recorded regions are applied again on top of the composed state
// and the output is sanitized and scanned once more. A copy that survives that
// is refused with [ErrRedactionIncomplete], and nothing is written: reporting
// success over content still in the file is the failure redaction exists to
// prevent.
//
// An image a redaction blanked in a copy comes back the same way, with no text
// for the scan to find: the rebuilt content names the image, whose samples
// were never touched. So each page such a redaction covered is also checked
// for a placement of the image under its regions (see
// [Writer.copiedImagesRestored]), which re-applies the redactions as a token
// does, and refuses when the re-application leaves one.
func (w *Writer) enforceRedactions(ctx context.Context, overrides map[int]ast.PdfObject) (map[int]ast.PdfObject, error) {
	if len(w.redactedTokens) == 0 && !w.hasRedactionCopies() {
		return overrides, nil
	}
	doc := w.reader.doc
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))
	// A done ctx can stop the raw-byte augmentation short, and the allocator
	// then starts below objects the file defines.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	seedAllocator(alloc, overrides)

	var pages []*document.Page
	resolvePages := func() error {
		if pages != nil {
			return nil
		}
		resolved, err := w.reader.resolvePages(ctx)
		if err != nil {
			return fmt.Errorf("resolving pages: %w", err)
		}
		pages = resolved
		return nil
	}

	var hits, unreadable int
	if len(w.redactedTokens) > 0 {
		scan, err := mutations.ScanOutputForTokens(ctx, alloc, doc, w.redactedTokens)
		if err != nil {
			return nil, fmt.Errorf("%w: scanning the composed output: %w", ErrRedactionIncomplete, err)
		}
		if !scan.Clean() {
			if err := resolvePages(); err != nil {
				return nil, err
			}
			if scan, err = w.untolerated(ctx, alloc, pages, scan); err != nil {
				return nil, fmt.Errorf("%w: telling copies on other pages apart: %w", ErrRedactionIncomplete, err)
			}
			hits, unreadable = len(scan.Hits), len(scan.Unreadable)
		}
	}
	imagesBack := false
	if w.hasRedactionCopies() {
		if err := resolvePages(); err != nil {
			return nil, err
		}
		back, err := w.copiedImagesRestored(ctx, alloc, pages)
		if err != nil {
			return nil, err
		}
		imagesBack = back
	}
	if hits == 0 && unreadable == 0 && !imagesBack {
		return overrides, nil
	}
	doc.Logger.Debug("writer: a later stage restored redacted content, re-applying the redactions",
		slog.Int("hits", hits), slog.Int("unreadable", unreadable), slog.Bool("images", imagesBack))

	tokens := slices.Clone(w.redactedTokens)
	for i, regions := range w.redactions {
		byPage, order, err := groupRedactRegions(regions, len(pages))
		if err != nil {
			return nil, err
		}
		opts := mutations.RedactOptions{PrivateCopies: w.redactionCopies[i], ClipAnnotations: w.redactionClips[i]}
		if i < len(w.redactionOutlines) {
			opts.GlyphOutlines = w.redactionOutlines[i].glyphOutlines(doc)
		}
		for _, pageNum := range order {
			pageReport, redactErr := mutations.RedactPageRegionsWith(
				ctx, alloc, doc, pages[pageNum-1], byPage[pageNum], opts)
			if redactErr != nil {
				return nil, fmt.Errorf("%w: re-applying page %d at write time: %w", ErrRedactionRefused, pageNum, redactErr)
			}
			tokens = appendNewTokens(tokens, mutations.RedactionNeedles(pageReport.RemovedText))
		}
	}
	if w.hasRedactionCopies() {
		back, err := w.copiedImagesRestored(ctx, alloc, pages)
		if err != nil {
			return nil, err
		}
		if back {
			return nil, fmt.Errorf("%w: a page still paints an image a redaction blanked in a copy", ErrRedactionIncomplete)
		}
	}
	if len(tokens) == 0 {
		return alloc.Pending(), nil
	}
	sanitized, sanErr := mutations.SanitizeDocument(ctx, alloc, doc, tokens)
	if sanErr != nil && errors.Is(sanErr, mutations.ErrSanitizationIncomplete) {
		// The copies Redact reported on other pages are still there, and still
		// tolerated: only what remains past them refuses.
		left, splitErr := w.untolerated(ctx, alloc, pages, sanitized.Scan)
		if splitErr != nil {
			return nil, fmt.Errorf("%w: telling copies on other pages apart: %w", ErrRedactionIncomplete, splitErr)
		}
		if left.Clean() {
			sanErr = nil
		}
	}
	if sanErr != nil {
		return nil, fmt.Errorf("%w: %w", ErrRedactionIncomplete, sanErr)
	}
	return alloc.Pending(), nil
}

// hasRedactionCopies reports whether a recorded redaction blanked an image in
// a copy.
func (w *Writer) hasRedactionCopies() bool {
	for _, copied := range w.redactionCopies {
		if len(copied) > 0 {
			return true
		}
	}
	return false
}

// copiedImagesRestored reports whether a page, as alloc stages it, paints an
// image a recorded redaction blanked in a copy where that redaction's regions
// meet it (see mutations.CopiedImagesPaintedInRegions).
func (w *Writer) copiedImagesRestored(ctx context.Context, alloc *writer.ObjAllocator, pages []*document.Page) (bool, error) {
	for i, regions := range w.redactions {
		copied := w.redactionCopies[i]
		if len(copied) == 0 {
			continue
		}
		byPage, order, err := groupRedactRegions(regions, len(pages))
		if err != nil {
			return false, err
		}
		for _, pageNum := range order {
			back, err := mutations.CopiedImagesPaintedInRegions(ctx, alloc, w.reader.doc, pages[pageNum-1], byPage[pageNum], copied)
			if err != nil {
				return false, fmt.Errorf("checking page %d for a restored image: %w", pageNum, err)
			}
			if back {
				return true, nil
			}
		}
	}
	return false, nil
}

// groupRedactRegions collects the regions of each page, and returns the pages
// in ascending order so the same input always drives the same sequence of
// edits.
func groupRedactRegions(regions []RedactRegion, pageCount int) (map[int][][4]float64, []int, error) {
	byPage := make(map[int][][4]float64, len(regions))
	for _, region := range regions {
		if region.Page < 1 || region.Page > pageCount {
			return nil, nil, fmt.Errorf("%w: page %d of %d", ErrInvalidPageNumber, region.Page, pageCount)
		}
		byPage[region.Page] = append(byPage[region.Page], region.Rect)
	}

	order := make([]int, 0, len(byPage))
	for pageNum := range byPage {
		order = append(order, pageNum)
	}
	sort.Ints(order)
	return byPage, order, nil
}

// mergePageRedaction folds one page's result into the document-wide report.
func mergePageRedaction(out *RedactReport, pageNum int, in mutations.RedactReport) {
	out.RemovedText = append(out.RemovedText, in.RemovedText...)
	if len(in.RemovedGlyphBoxes) > 0 {
		if out.RemovedGlyphBoxes == nil {
			out.RemovedGlyphBoxes = make(map[int][][4]float64)
		}
		out.RemovedGlyphBoxes[pageNum] = append(out.RemovedGlyphBoxes[pageNum], in.RemovedGlyphBoxes...)
	}
	if len(in.RemovedGlyphOrigins) > 0 {
		if out.removedGlyphOrigins == nil {
			out.removedGlyphOrigins = make(map[int][][2]float64)
		}
		out.removedGlyphOrigins[pageNum] = append(out.removedGlyphOrigins[pageNum], in.RemovedGlyphOrigins...)
	}
	out.BlankedImages = append(out.BlankedImages, in.BlankedImages...)
	out.blankedPhotographs = mutations.MergeBlankedPhotographs(out.blankedPhotographs, in.BlankedPhotographs)
	if len(in.ImagesBlankedInCopy) > 0 {
		if out.ImagesBlankedInCopy == nil {
			out.ImagesBlankedInCopy = make(map[int][]string)
		}
		out.ImagesBlankedInCopy[pageNum] = append(out.ImagesBlankedInCopy[pageNum], in.ImagesBlankedInCopy...)
	}
	if in.FormsCopied > 0 {
		if out.FormsCopied == nil {
			out.FormsCopied = make(map[int]int)
		}
		out.FormsCopied[pageNum] += in.FormsCopied
	}
	out.RemovedPaths += in.RemovedPaths
	out.ClippedPaths += in.ClippedPaths
	out.RemovedImages += in.RemovedImages
	out.RasterisedUnits += in.RasterisedUnits
	out.RemovedAnnotations = append(out.RemovedAnnotations, in.RemovedAnnotations...)
	if len(in.ClippedAnnotations) > 0 {
		out.ClippedAnnotations = append(out.ClippedAnnotations, in.ClippedAnnotations...)
		if out.clippedByPage == nil {
			out.clippedByPage = make(map[int][]int)
		}
		out.clippedByPage[pageNum-1] = append(out.clippedByPage[pageNum-1], in.ClippedAnnotations...)
	}
	out.DroppedPageEntries = append(out.DroppedPageEntries, in.DroppedPageEntries...)
	if len(in.FontsFullyRemovedOnPage) > 0 {
		if out.FontsFullyRemovedOnPage == nil {
			out.FontsFullyRemovedOnPage = make(map[int][]int)
		}
		out.FontsFullyRemovedOnPage[pageNum] = in.FontsFullyRemovedOnPage
	}
}
