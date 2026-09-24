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
// what one content-stream operator draws.
//
// An operator is removed whole or not at all, because excision deletes byte
// ranges and cannot rewrite one: dropping a show-text operator whose run only
// partly meets the region would take the rest of its text with it. Splitting
// the run at glyph boundaries is the alternative, and it has to answer the
// glyph-advance leak first (arXiv 2206.02285), where the advances of the
// surviving glyphs still spell the removed ones. Refusing states the problem
// instead of removing more than the caller asked for.
var ErrPartialOperatorCoverage = errors.New("region covers an operator only partly")

// ErrImageShared is returned by [CheckBlankedImageUses] when a blanked image
// is also painted somewhere the regions do not cover the same samples. A
// blanking rewrites the image object in place, so that other placement would
// lose samples no region selected. Removing more than was asked changes the
// page outside the region, so the redaction refuses and names the image.
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
// rather than reaching for the only span it could name.
var ErrRegionInFormXObject = errors.New("region selects content drawn by a form XObject")

// ErrPathStraddlesRegion reports a painted path that lies only partly inside
// the region being cleared.
//
// Removing a path takes its whole run, from the first construction operator
// through the paint operator that ended it, because the construction operators
// left behind would merge into the next path and be painted by its operator
// instead. A path reaching outside the region would therefore lose ink the
// caller never selected. Refusing states the problem rather than clearing more
// of the page than was asked.
var ErrPathStraddlesRegion = errors.New("painted path lies only partly inside the region")

// ErrRegionCoversAStrokedPath reports a region meeting a path painted by a
// stroking operator.
//
// A stroke is centred on the path and spreads half the line width to either
// side (ISO 32000-1, 8.4.3.2), so its ink covers more than the path geometry
// the machine records. A stroked path whose recorded bounds sit inside the
// region can still mark the page outside it, which makes containment
// unprovable from the bounds alone. Refusing beats removing ink outside the
// region; inflating the bounds by the line width in force would lift this.
var ErrRegionCoversAStrokedPath = errors.New("region covers a stroked path")

// ErrRegionCoversAClippedPath reports a region meeting a path that a W or W*
// took part in.
//
// Such a path does double duty: it paints, and it narrows the clipping region
// for everything drawn after it (ISO 32000-1, 8.5.4). Removing it would widen
// the clip and change how later content renders, which is a change well
// outside the region the caller named. Refusing beats editing the rest of the
// page as a side effect of clearing part of it.
var ErrRegionCoversAClippedPath = errors.New("region covers a path that also clips")

// ErrImageNotAddressable reports an image the mutation cannot replace because
// it has no indirect reference of its own.
//
// Blanking rewrites the image's own stream object, so an image written directly
// into a resource dictionary has no object number for the replacement to take.
// Reporting it beats leaving the picture in place while telling the caller the
// region was cleared.
var ErrImageNotAddressable = errors.New("image has no indirect reference")

// ErrInlineImageInRegion reports an inline image the region covers.
//
// An inline image carries its samples in the content stream between BI and EI
// (ISO 32000-1, 8.9.7), so there is no object to replace: blanking one means
// splicing new sample bytes into the stream itself. That path does not exist
// yet, so the mutation reports it rather than leaving the pixels in place.
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
var ErrImageRegionCoversNoSample = errors.New("region covers an image but none of its samples")

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

// ErrStructElementPartlyRemoved reports a structure element that describes both
// marked content a redaction removed and marked content it kept.
//
// One element can cover several marked-content sequences, and its /ActualText,
// /Alt and /E describe all of them together. Clearing those for the sequence
// that went would take the description of the ones that stayed, and leaving
// them keeps a copy of the removed text in the structure tree. Splitting the
// element is the operation this needs, and it does not exist yet.
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
