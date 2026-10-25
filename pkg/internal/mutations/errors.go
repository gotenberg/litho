package mutations

import "errors"

// ErrPageNotAddressable reports a page that cannot be rewritten in place
// because it has no indirect reference of its own.
//
// A page may be inlined directly in a /Kids array (ISO 32000-1, 7.7.3.2), in
// which case there is no object number for an incremental update to replace.
// Copying mutations handle this by allocating a new object for the page; the
// ones that edit a page where it stands cannot, and report this instead of
// leaving the caller believing the edit landed.
var ErrPageNotAddressable = errors.New("page has no indirect reference")

// ErrInvalidPageNumber reports a page number outside [1, pageCount], or an
// insertion position outside [0, pageCount], of the document a page operation
// edits. pdf.ErrInvalidPageNumber is this value, so a caller matches it with
// errors.Is without importing pkg/internal.
var ErrInvalidPageNumber = errors.New("page number out of bounds")

// ErrCatalogUnreadable reports a catalog that neither the trailer reference nor
// the reader's recovery could produce entries for.
//
// A catalog with no entries is not a catalog: ISO 32000-1, 7.7.2 requires
// /Type and /Pages. Editing one in place would write those zero entries back
// over the real catalog, so the mutation reports this instead.
var ErrCatalogUnreadable = errors.New("document catalog could not be read")

// ErrPageTreeNotReproduced reports a full rewrite whose source catalog named a
// page tree the output could not reproduce.
//
// The rewrite copies the object graph from the catalog down, so a page tree
// litho failed to read is simply absent from the copy. Substituting an empty
// one (ISO 32000-1, 7.7.3.2 requires the catalog to name a page tree) keeps the
// output structurally valid but silently discards every page. Report the loss
// instead, and reserve the empty tree for a catalog that never named one.
//
// # Corpus evidence
//
//   - pdfjs: `litho attach` on an encrypted file turned a 47 KB one-page
//     document into a 1.9 KB zero-page one and exited 0, while `litho rotate`
//     on the same file failed.
var ErrPageTreeNotReproduced = errors.New("source page tree could not be reproduced in the rewrite")

// ErrSpanOutOfRange reports a byte span that does not lie within the content it
// addresses.
//
// A span names offsets in one parse of a content stream, so a span reaching
// past the bytes being rewritten means the two have diverged. Excising it would
// cut whatever occupies those offsets now, so the mutation reports the span
// rather than clamping it to fit.
var ErrSpanOutOfRange = errors.New("byte span lies outside the content")

// ErrPartialOperatorCoverage reports a region that covers some but not all of
// what one content-stream operator draws, and an operator that cannot be split
// at its glyphs exactly.
//
// A show-text operator whose run only partly meets the region is split: the
// covered glyphs go, and each removed run becomes one TJ number standing for
// its whole width, kerning included, which answers the glyph-advance leak
// (arXiv 2206.02285) since the next kept glyph shows that width anyway. A split
// is refused when litho cannot place the kept glyphs where they were: a code
// the extractor read at a length the font's encoding does not give it, text
// in a rendering mode that adds it to the clipping path, or a TJ number under
// a horizontal scaling other than 100. Dropping the whole operator would take
// the rest of its text with it, so refusing states the problem instead of
// removing more than the caller asked for.
var ErrPartialOperatorCoverage = errors.New("region covers an operator only partly")

// ErrImageShared is returned by [CheckBlankedImageUses] when a blanked image
// is also painted somewhere the regions do not cover the same samples. A
// blanking rewrites the image object in place, so that other placement would
// lose samples no region selected. Removing more than was asked changes the
// page outside the region, so the check refuses and names the image. A
// caller that can blank the image in a copy instead asks
// [SharedBlankedImages] which images to copy (see
// [RedactOptions.PrivateCopies]), and keeps this refusal for a strict
// redaction, or for an image a copy cannot place.
var ErrImageShared = errors.New("blanked image is also painted where the regions do not cover it")

// ErrTextAdvanceUnknown is returned when a show-text operator selected for
// removal has an advance that later text on its line depends on, and that
// advance cannot be put back: it was not recorded, or it was drawn at a font
// size, horizontal scale or text matrix that no TJ number can reproduce.
// Removing the operator anyway would move text outside the region.
var ErrTextAdvanceUnknown = errors.New("advance of the removed text cannot be preserved")

// ErrRegionCoversAnImage reported a region that meets an image the page paints.
//
// SUPERSEDED, and nothing returns it. It dates from when removal could only
// delete whole content-stream operators, so an image was taken entirely or not
// at all and the honest answer was to refuse. Blanking the covered pixels is
// the operation it said litho could not do yet, and [RedactPageRegions] does it
// now: see [blankImage], which rewrites the covered samples in place and
// records the XObject in [RedactReport.BlankedImages].
//
// The cases where an image still cannot be cleared each have their own
// sentinel, so this one covers nothing the others do not:
// [ErrImageNotAddressable], [ErrImageNotDecodable],
// [ErrImagePlacementDegenerate], [ErrImageRegionCoversNoSample],
// [ErrImageCarriesSampleEntries], [ErrImageIsStencilMask] and
// [ErrInlineImageInRegion].
//
// Kept rather than deleted because unused code here may be deliberate; removing
// it is a maintainer's call.
var ErrRegionCoversAnImage = errors.New("region covers an image")

// ErrXObjectUnresolved reports a Do the machine could not follow to an XObject.
//
// An XObject it cannot read is content it cannot place, so it cannot rule out
// that the thing covers the region being cleared. Removal has to be provable,
// and "I could not tell what this draws" is not a proof.
var ErrXObjectUnresolved = errors.New("page invokes an XObject that could not be resolved")

// ErrRegionInFormXObject reports a region that selects content a form XObject
// draws, whether glyphs or a painted path.
//
// The content belongs to the form's own content stream, so no byte range in the
// page's stream addresses it. Removing the Do that invoked the form would
// erase everything else the form draws, so the mutation reports the region
// rather than reaching for the only span it could name, unless the raster
// fallback is asked for ([RedactOptions.Rasterise]).
var ErrRegionInFormXObject = errors.New("region selects content drawn by a form XObject")

// ErrPathStraddlesRegion reports a painted path that lies only partly inside
// the region being cleared and cannot be rewritten exactly.
//
// Such a path is ordinarily rewritten: its geometry inside the region removed
// and the rest painted under a clip excluding the region. That needs operands
// it can read, a place for the clip (ISO 32000-1, 8.2 allows no q inside a
// text object), and, for a subpath both filled and stroked but left open, ends
// and a closing edge outside the region, since the fill closes it and the
// stroke does not. Without them, removing the whole path would take ink the
// caller never selected and keeping it would leave ink inside the region, so
// the redaction is refused.
var ErrPathStraddlesRegion = errors.New("painted path lies only partly inside the region")

// ErrRegionCoversAStrokedPath reports a region meeting a path painted by a
// stroking operator whose ink cannot be cleared exactly.
//
// A stroke is centred on the path and spreads half the line width to either
// side (ISO 32000-1, 8.4.3.2), more at a miter join, so its ink covers more than
// the path geometry the machine records. A stroke is cleared by the reach the
// line width, the joins and the CTM give it, and refused when that reach is
// unknown: the width or join comes from a graphics state that does not
// resolve, or an operand that is no number. A dashed stroke whose geometry
// would have to be cut is refused too, since the chord replacing the removed
// part moves every dash after it (8.4.3.6).
var ErrRegionCoversAStrokedPath = errors.New("region covers a stroked path")

// ErrRegionCoversAShading reports a region that a shading paint (sh) may reach.
//
// sh fills the whole current clipping region with the shading (ISO 32000-1,
// 8.7.4.2): it has no geometry of its own to cut, and removing the operator
// would clear the shading outside the region too. The region is checked
// against the bounds of the clip in force, which contain all the shading can
// paint; one that meets them is refused rather than reported clear while
// shading ink stays inside it, unless the raster fallback is asked for
// ([RedactOptions.Rasterise]).
var ErrRegionCoversAShading = errors.New("region covers a shading paint")

// ErrRegionCoversAClippedPath reports a region meeting a path that a W or W*
// took part in.
//
// Such a path does double duty: it paints, and it narrows the clipping region
// for everything drawn after it (ISO 32000-1, 8.5.4). Removing it would widen
// the clip and change how later content renders, which is a change well
// outside the region the caller named. Refusing beats editing the rest of the
// page as a side effect of clearing part of it.
var ErrRegionCoversAClippedPath = errors.New("region covers a path that also clips")

// ErrRegionCoversClippingText reports a region selecting a show-text operator
// whose text rendering mode adds its glyphs to the clipping path.
//
// Modes 4 to 7 make the glyph outlines part of the clip for everything drawn
// after the text object (ISO 32000-1, 9.3.6, Table 106). Removing the operator
// widens that clip, so what it masked paints outside the region too: text set
// in mode 7 over a filled rectangle turns into the whole rectangle. It is the
// text counterpart of [ErrRegionCoversAClippedPath].
var ErrRegionCoversClippingText = errors.New("region covers text that also clips")

// ErrTextNotPlaceable reports a page whose content shows a code the text
// extraction read no glyph for, outside the operators the redaction removes
// whole.
//
// Selection is by the glyph boxes extraction places, and such a code has none:
// its length or its width is one the font does not state (a CMap name outside
// ISO 32000-1, Table 118, or a Type 0 font without a CIDFont), so where a
// viewer draws its glyph and the codes after it, extraction cannot say.
// Keeping it could keep a glyph inside the region, so the page is refused
// instead.
var ErrTextNotPlaceable = errors.New("page shows text whose glyphs cannot be placed")

// ErrTextPositionAmbiguous reports a page showing text after a Q, inside a
// text object, that undoes a q saved after the text object began, when the
// text matrix moved between them.
//
// ISO 32000-1, 8.2, Figure 9 allows no q or Q inside a text object, so no
// clause says whether Q restores the text matrix there, and viewers read it
// both ways: PDF.js and litho restore it, poppler keeps it. The text after
// the Q then sits in two places, and a region clearing the one litho reads
// leaves the one poppler draws (litho corpus qshyphen_2b, whose `q 0 -30 Td Q`
// puts a word a line lower in poppler).
var ErrTextPositionAmbiguous = errors.New("page shows text whose position viewers read two ways")

// ErrImageNotAddressable reports an image the mutation cannot replace because
// it has no indirect reference of its own.
//
// Blanking rewrites the image's own stream object, so an image written directly
// into a resource dictionary has no object number for the replacement to take.
// Reporting it beats leaving the picture in place while telling the caller the
// region was cleared.
var ErrImageNotAddressable = errors.New("image has no indirect reference")

// ErrInlineImageInRegion reports an inline image the region covers that the
// mutation cannot rewrite in place.
//
// An inline image carries its samples in the content stream between BI and EI
// (ISO 32000-1, 8.9.7), so blanking one splices a new inline image over those
// bytes, which has to name the same colour space through the same resources.
// An image whose data the readers end at different bytes, whose dictionary
// writes a key both abbreviated and in full or holds one Table 93 does not
// list, or whose colour space only an image XObject could name once rewritten
// is reported rather than left with its pixels in place.
var ErrInlineImageInRegion = errors.New("region covers an inline image")

// ErrImageNotDecodable reports an image whose samples the mutation could not
// read.
//
// Blanking decodes the samples, overwrites the covered ones and re-encodes. A
// filter litho cannot decode (an unknown filter), a sample
// depth it does not unpack, or an indexed image whose colour table it cannot
// read (a base colour space litho assigns no component count, or a /Lab base
// missing the /WhitePoint ISO 32000-1, Table 65 requires) leaves nothing to
// overwrite, and an image it cannot read is one it cannot prove it cleared.
var ErrImageNotDecodable = errors.New("image samples could not be decoded")

// ErrImagePlacementDegenerate reports an image whose placement matrix cannot be
// inverted.
//
// The matrix collapsed the image's unit square onto a line or a point, so no
// pixel corresponds to a page-space point and the covered samples cannot be
// identified at all.
var ErrImagePlacementDegenerate = errors.New("image placement matrix cannot be inverted")

// ErrImageRegionCoversNoSample reports a region that meets an image but falls
// between its sample centres, so blanking clears nothing.
//
// The region still covers part of what the image paints: a single sample can be
// stretched across a large area, and a region inside that area covers picture
// the caller asked to clear. Blanking the nearest samples would clear more of
// the picture than was asked, and returning success would leave that content in
// the file while telling the caller it was cleared. Reporting it is the only
// honest answer, and a higher-resolution image does not hit this at all.
//
// A region meeting only samples that hold the blank already, as an earlier
// redaction leaves them, shows no picture, so it is not refused: the image is
// left as it is (see [touchedSamplesAlreadyBlank]).
var ErrImageRegionCoversNoSample = errors.New("region covers an image but none of its samples")

// errSamplesAlreadyBlank tells the redaction that a region meets an image only
// over samples holding the blank already, so the image stays as it is. It
// never leaves the package: see [touchedSamplesAlreadyBlank].
var errSamplesAlreadyBlank = errors.New("region covers only samples already blank")

// ErrImageCarriesSampleEntries reports an image whose dictionary holds entries
// the blanked replacement cannot carry forward.
//
// Blanking re-encodes the samples at 8 bits under a fresh dictionary, in the
// image's own colour space or in DeviceRGB, so /Decode, /Mask and
// /ColorKeyMask are dropped rather than retained. What
// matters is whether the decoded pixels already carry what the dropped entry
// said.
//
// /Mask and /ColorKeyMask say which samples are painted at all. The decoder
// does not apply them, so blanking one would make masked-out picture opaque
// outside the region as well as inside it. Both are always refused.
//
// /Decode is refused only when the decode did not apply it, which
// [pdfimage.AppliesDecodeArray] answers. The decoder now applies it to every
// image it returns pixels for, an indexed one's indices included, so the
// mapping is already in the pixels and the entry is redundant: the image is
// blanked rather than refused. A JPEG was refused here until DecodeImage
// stopped double-decoding, and an indexed image until the decoder mapped its
// indices.
var ErrImageCarriesSampleEntries = errors.New("image carries decode or mask entries")

// ErrImageIsStencilMask reports an image mask the region covers that blanking
// cannot rewrite, which is now only one carrying /SMask.
//
// An ordinary stencil is no longer refused. A mask supplies a shape rather than
// a picture, and ISO 32000-1, 8.9.6.2 paints its unmasked samples with the
// current fill colour, so [blankImage] clears one by keeping it a mask: the
// replacement is still /ImageMask at one bit a sample, and the covered samples
// are set UNPAINTED. Blanking it the way a picture is blanked would be wrong
// twice over. Re-encoding to 8-bit DeviceRGB would stop it being a mask and
// stop it taking the fill colour, and painting the covered samples black, which
// is what clearing a picture does, would mark the page in whatever colour
// happened to be in force instead of removing the shape.
//
// A mask carrying /SMask is what remains. No clause in ISO 32000-1 gives a soft
// mask a meaning on a stencil, so dropping it would change rendering for a
// viewer that honours it, and carrying it onto the one-bit replacement would
// assert a meaning the spec does not define. Refusing beats guessing, which is
// the judgement [ErrImageCarriesSampleEntries] records for /Mask.
var ErrImageIsStencilMask = errors.New("region covers an image mask carrying a soft mask")

// ErrStructElementPartlyRemoved reports a structure element whose /ActualText,
// /Alt or /E describes both content a redaction removed and content it kept.
//
// One element can cover several marked-content sequences, and its descendants'
// too, and those entries describe all of it together (ISO 32000-1, 14.9.3 to
// 14.9.5). Clearing them would take the description of what stayed, and
// leaving them keeps a copy of the removed text in the structure tree. An
// element holding none of them is no reason to refuse: its marked content
// stays in the page, holding what survived.
var ErrStructElementPartlyRemoved = errors.New("structure element describes both removed and surviving content")

// ErrStructElementNotAddressable reports a structure element the mutation
// cannot rewrite because it has no indirect reference of its own.
//
// Scrubbing replaces the element's own object, so an element written directly
// into the /ParentTree array has no object number for the replacement to take.
// Reporting it beats leaving the structure tree holding a copy of text the page
// no longer draws.
var ErrStructElementNotAddressable = errors.New("structure element has no indirect reference")

// ErrAnnotationAppearanceHoldsText reports an annotation whose appearance
// stream still paints text of its own.
//
// An annotation's words can be drawn twice: once as the /Contents a viewer may
// show, and again by the content stream in its /AP, which is what actually
// appears on the page. Clearing /Contents alone leaves the appearance drawing
// them. Dropping the appearance is only recoverable for a widget, whose
// appearance litho can regenerate from the field value; a markup annotation's
// appearance has nothing to rebuild it from, so removing it would destroy the
// annotation's visual instead of cleaning it.
var ErrAnnotationAppearanceHoldsText = errors.New("annotation appearance stream still paints text")

// ErrAnnotationNotAddressable reports an annotation the mutation cannot rewrite
// because it has no indirect reference of its own.
//
// Scrubbing replaces the annotation's own object, so one written directly into
// a page's /Annots array has no object number for the replacement to take.
// Reporting it beats leaving an annotation quoting text the page no longer
// draws.
var ErrAnnotationNotAddressable = errors.New("annotation has no indirect reference")

// ErrFormNotAddressable reports an interactive form the mutation cannot
// rewrite.
//
// Pruning a root field means replacing the AcroForm dictionary, or the catalog
// carrying it inline. Neither can take a replacement without an object number
// of its own, and reporting it beats leaving /Fields naming a field whose
// widget the page no longer carries.
var ErrFormNotAddressable = errors.New("interactive form has no indirect reference")

// ErrAnnotationStraddlesRegion reports an annotation reaching outside the
// region that selected it.
//
// An annotation is atomic: there is no way to remove the part inside the region
// and keep the rest. Removing one that extends past the region would take link
// or widget behaviour the caller never selected, and any appearance it draws
// outside with it. Refusing matches [ErrPathStraddlesRegion], which exists for
// the same reason.
var ErrAnnotationStraddlesRegion = errors.New("annotation reaches outside the region")

// ErrAnnotationIsFormWidget reports a widget annotation inside the region that
// cannot be detached from its form.
//
// Most widgets are detachable and are not refused. A parentless one is the
// merged field-and-annotation of ISO 32000-1, 12.5.6.19 and leaves AcroForm
// /Fields with the page; one carrying /Parent is dropped from that field's
// /Kids. What remains here is the case with nothing to rewrite: a /Parent
// written directly into the widget, or a widget written directly into /Annots,
// neither of which has an object number for the detachment to take. Removing
// the widget anyway would leave a field naming an annotation no page carries,
// still holding its value, which is [ScrubFormFieldValues]' to clear.
var ErrAnnotationIsFormWidget = errors.New("annotation is a form widget")

// ErrAnnotationNotPlaceable reports an annotation whose rectangle cannot be
// read.
//
// /Rect is required (ISO 32000-1, Table 164), so one missing or malformed means
// the annotation cannot be placed on the page at all. An annotation that cannot
// be placed cannot be ruled outside the region either, which is the same reason
// [ErrXObjectUnresolved] refuses rather than stepping over content it could not
// follow.
var ErrAnnotationNotPlaceable = errors.New("annotation rectangle cannot be read")

// ErrFieldValueNotAddressable reports a form field whose value the mutation
// cannot clear because the dictionary stating it has no indirect reference.
//
// A field's /V is inheritable (ISO 32000-1, 12.7.3.1), so the value a widget
// shows is often stated by an ancestor field rather than by the widget itself.
// Clearing it means rewriting that ancestor's own object, and one written
// directly into its parent's /Kids has no object number for the replacement to
// take. Reporting it beats leaving the value in the field tree while telling
// the caller the document was cleared.
var ErrFieldValueNotAddressable = errors.New("form field value has no indirect reference")

// ErrOutlineItemNotAddressable reports an outline item quoting removed text
// that the mutation cannot rewrite, because the entry naming it is not an
// indirect reference.
//
// Scrubbing replaces the item's own object, so one written directly into its
// parent's /First or a sibling's /Next has no object number for the replacement
// to take. Walking past it is what a destination prune does, because an
// unpruned destination is merely stale; here it would leave a bookmark whose
// title still spells the text the page no longer draws.
var ErrOutlineItemNotAddressable = errors.New("outline item has no indirect reference")

// ErrScanTokenEmpty reports an empty token handed to a document scan.
//
// An empty string is contained in every other string, so scanning for one would
// report every object in the file as still holding the token. That reads as a
// catastrophic leak and means nothing, so the scan reports the caller's mistake
// instead of returning an answer it cannot support.
var ErrScanTokenEmpty = errors.New("scan token is empty")

// ErrSanitizationIncomplete reports a document that still holds removed text
// after every document-wide pass has run.
//
// It is the point of the gate rather than a defect in it. The passes clear the
// homes they know: annotation values, form field values, outline titles. A
// token surviving past them sits somewhere none of them reaches, most often
// /Info, which [ClearMetadata] owns and which no token-driven pass touches. The
// scan's own caveat applies: a hit may be an innocent coincidence in unrelated
// data, so this says the document cannot be declared clean, not that it
// certainly leaks.
//
// The edits already queued are left in place, and [SanitizeReport.Scan] names
// the objects, so a caller can clear what remains and ask again rather than
// starting over.
var ErrSanitizationIncomplete = errors.New("document still holds removed text after sanitization")

// ErrNestedContainerUnread reports a container inside a stream, an attached
// PDF or archive, that the output scan could not read through: encrypted,
// past the scan's budget or nesting bound, or with an object the scan of the
// nested PDF could not read. The scan reports the carrying stream unreadable,
// which fails a redaction closed.
var ErrNestedContainerUnread = errors.New("nested container could not be read through")

// ErrSpansOverlap reports two byte spans that cover some of the same bytes.
//
// Overlapping spans stop having a single meaning once one of them is removed,
// and merging them quietly would excise bytes no caller selected. Removal has
// to be provable, so the mutation reports the overlap instead.
var ErrSpansOverlap = errors.New("byte spans overlap")

// ErrPagePruneIncomplete reports a delete-time prune that stopped at its depth
// bound with part of the graph unread.
//
// Deleting a page means unhooking every reference that reaches it, and each
// prune owns one kind: the structure tree through /Pg, the outline through
// /Dest and /A, the named-destination trees, and a field's /Kids through its
// widgets' /P. Each walk is bounded, and each bound stops the walk without
// telling anyone. The rewrite then copies whatever the catalog still reaches,
// so a reference the walk never visited keeps the page and its content stream
// in the output: the result is a document reporting one fewer page while still
// carrying that page's bytes, which is the shape a caller deleting a page to
// remove what is on it least expects.
//
// Reporting beats performing the deletion halfway, the same judgement
// [ErrSanitizationIncomplete] records: the error reaches the writer before
// anything is written, so the caller gets no document rather than one that
// silently kept what it asked to remove. Real documents nest a few dozen levels
// in any of these structures, so none of them reaches this.
var ErrPagePruneIncomplete = errors.New("a page-delete prune stopped at its depth bound, so the deleted page would survive in the output")

// ErrAttachmentTreeIncomplete reports an EmbeddedFiles name tree the walk could
// not read to the bottom, because it nests deeper than maxAttachmentTreeDepth.
//
// Adding, removing, splitting and merging all rebuild the whole tree from what
// the walk collected, so an entry below the bound is absent from the collection
// and therefore absent from the rebuilt tree: it is destroyed, not kept. The
// same truncation makes a removal match nothing and report success having
// removed nothing.
//
// Reporting beats performing the edit halfway, the judgement
// [ErrSanitizationIncomplete] already records. Every measured outcome here is
// silent data loss or a false success, so a caller that gets an error can
// decide what to do, where one that gets neither cannot. Real trees nest two or
// three levels, so no ordinary document reaches this.
var ErrAttachmentTreeIncomplete = errors.New("embedded-files tree nests deeper than the attachment walk can read, so rebuilding it would drop entries")

// ErrSignatureFieldUnavailable reports a signature field name that cannot take
// the signature: a field of that name exists and is not a signature field, is
// signed already, or asks for more than the signature litho writes, or the name
// is not one a new root field can take.
var ErrSignatureFieldUnavailable = errors.New("signature field unavailable")

// ErrSignatureFieldLocked reports a signature field a signature the document
// holds locked (ISO 32000-1, 12.8.2.4, Table 256): signing it would change a
// field that signature's FieldMDP transform forbids changing. It is returned
// with [ErrSignatureFieldUnavailable].
var ErrSignatureFieldLocked = errors.New("signature field locked by an earlier signature")

// ErrSeedValueUnmet reports a signature field whose seed value dictionary
// (ISO 32000-1, 12.7.4.5, Table 234) requires what the signature cannot be:
// another signature handler, subfilter or digest, a reason other than the
// one asked for, a certification signature, revocation information inside
// the SignedData, a timestamp with no authority named, or a signing
// certificate other than the signer's.
var ErrSeedValueUnmet = errors.New("signature field seed value cannot be met")

// ErrSubFilterUnwritable reports a [Signer] whose /Contents does not suit the
// /SubFilter [SignParams] asked for: a caller's SignedData that is not the
// CAdES signature ETSI.CAdES.detached requires (ETSI EN 319 142-1, 6.3). A
// subfilter a seed value dictionary chose then gives way to the next one it
// accepts (see [WriteWithSignature]).
var ErrSubFilterUnwritable = errors.New("the signer cannot write the signature's subfilter")

// ErrInvalidAnnotation reports an annotation [BakeAnnotations] was asked to
// bake that the page's /Annots array does not hold as a dictionary.
var ErrInvalidAnnotation = errors.New("annotation is not a dictionary of the page")
