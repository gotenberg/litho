package pdf

import (
	"errors"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/filter"
	pdfimage "github.com/gotenberg/litho/pkg/internal/graphics/image"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/semantics"
)

// Exported errors for granular error handling by consumers.
var (
	// ErrInvalidPassword is returned when the provided password does not
	// unlock the encrypted document.
	ErrInvalidPassword = errors.New("invalid PDF password")

	// ErrNotAPDF is returned when the file does not start with a valid
	// %PDF- header.
	ErrNotAPDF = errors.New("not a valid PDF document (missing %PDF- header)")

	// ErrInternalPanic is returned when an unexpected panic was recovered
	// while processing document bytes, at a public API boundary or on a
	// worker goroutine such as a compliance check group. Internal
	// code is not supposed to panic; this sentinel exists so a crafted or
	// corrupt input cannot crash the host process through a panic. A fatal
	// runtime error is not recoverable in Go, so a stack overflow or an
	// out-of-memory kill ends the process without ever producing this
	// error. The recovered value and a stack trace are logged at Error via
	// the document logger.
	ErrInternalPanic = document.ErrInternalPanic

	// ErrDamagedXRef is returned when the cross-reference table is
	// corrupted or missing and could not be reconstructed.
	ErrDamagedXRef = errors.New("damaged or missing cross-reference table")

	// ErrUnsupportedEncryption is returned when the document uses an
	// encryption algorithm or revision that this engine does not support.
	ErrUnsupportedEncryption = errors.New("unsupported encryption algorithm or revision")

	// ErrFontMissing is returned when a required font could not be
	// resolved or decoded during text extraction.
	ErrFontMissing = errors.New("required font is missing or failed to decode")

	// ErrRequiresOCR is returned when a page contains no digital text
	// and requires optical character recognition.
	ErrRequiresOCR = errors.New("page contains no digital text and requires optical character recognition")

	// ErrOCRFailed is returned when the OCR engine returns an error
	// during text extraction. Callers can use [errors.Is] to distinguish
	// engine failures from other extraction errors and decide whether to
	// retry with a different engine or skip the page.
	ErrOCRFailed = errors.New("OCR engine failed")

	// ErrImageNotFound is returned by [Reader.DecodeImage] when the page shows
	// no image under the requested name.
	ErrImageNotFound = errors.New("image not found on the page")

	// ErrImageNotDecodable is returned by [Reader.DecodeImage] when litho
	// cannot turn an image's samples into pixels: a filter it does not know, a
	// colour space or depth it does not handle, or damaged data. The message
	// carries the reason.
	ErrImageNotDecodable = errors.New("image cannot be decoded")

	// ErrInvalidPageNumber is returned when a page number is outside the
	// valid range [1, pageCount].
	ErrInvalidPageNumber = errors.New("page number out of bounds")

	// ErrInvalidRotation is returned when a rotation value is not a
	// multiple of 90.
	ErrInvalidRotation = errors.New("rotation must be a multiple of 90")

	// ErrEmptyPageList is returned by [Writer.DeletePages] when it receives an
	// empty page list. It is the only page operation that returns it: the
	// others document an empty list as every page.
	ErrEmptyPageList = errors.New("empty page list")

	// ErrNilSource is returned when a nil [Reader] is passed where a
	// source document is required.
	ErrNilSource = errors.New("source reader is nil")

	// ErrEmptyText is returned when a text stamp has an empty string.
	ErrEmptyText = errors.New("empty text")

	// ErrEmptyData is returned when image stamp data is empty.
	ErrEmptyData = errors.New("empty data")

	// ErrUnsupportedMimeType is returned when an image stamp uses an
	// unsupported MIME type.
	ErrUnsupportedMimeType = errors.New("unsupported MIME type")

	// ErrInvalidFont is returned when font data is too short or
	// structurally invalid.
	ErrInvalidFont = errors.New("invalid font data")

	// ErrSourceIndexOutOfRange guards the text-element callback that
	// [Writer.Merge] hands to bookmark generation, which is the only place
	// it is produced. No exported method returns it: the callback's caller
	// logs the failure at Debug through the document logger and skips the
	// page, so the value never reaches a caller. The index it checks is a
	// loop variable over the same sources the callback captured, so it is
	// in range by construction. Split does not carry this guard at all,
	// because its own callbacks ignore the index.
	ErrSourceIndexOutOfRange = errors.New("source index out of range")

	// ErrWriterConsumed is returned by [Writer.WriteTo] when the Writer
	// has already produced output. A Writer is single-use: its mutations
	// drain into the backing [Reader]'s in-memory state on the first
	// successful write, so a second write would re-apply an
	// already-mutated state and emit a corrupt document. Create a fresh
	// Writer (and Reader) for each distinct output.
	ErrWriterConsumed = errors.New("writer already consumed; create a new Writer")

	// ErrWriterClosed is returned by [Writer.WriteTo], [Writer.Redact] and
	// the remediation methods after [Writer.Close]. Close releases the
	// remediated output a Writer holds, and a later write cannot rebuild it
	// in full: the later remediation rounds ran against that output, not
	// against the Writer's queue.
	ErrWriterClosed = errors.New("writer closed; create a new Writer")

	// ErrSignFullRewriteIncompatible is returned by [Writer.WriteTo] when
	// [Writer.Sign] is combined with an operation that requires a full
	// rewrite: [Writer.Encrypt], [Writer.RemoveEncryption],
	// [Writer.DeletePages], [Writer.InsertPages], [Writer.FlattenForm],
	// [Writer.ClearMetadata], or [WithFullRewrite]. Signing covers a
	// ByteRange of the existing bytes via an incremental update; a full
	// rewrite renumbers objects and invalidates that ByteRange, so the
	// two cannot be combined. Encrypted-then-signed output is permitted
	// by the spec but not yet implemented.
	ErrSignFullRewriteIncompatible = errors.New("sign is incompatible with full-rewrite operations (encrypt, decrypt, delete/insert pages, flatten, clear metadata, full rewrite)")

	// ErrEncryptedNotAuthenticated is returned by [Writer.WriteTo] when the
	// source document is encrypted but was never authenticated, so its
	// strings and streams cannot be decrypted for re-serialization. Reopen
	// the document with [WithPassword] before writing.
	ErrEncryptedNotAuthenticated = errors.New("source document is encrypted and not authenticated; reopen with WithPassword before writing")

	// ErrRedactionRefused is returned by [Writer.Redact] when a region
	// selects content that cannot be removed exactly: an operator the region
	// covers only partly, a painted path that straddles the region or was
	// stroked or took part in a clip, content drawn by a form XObject, an
	// image whose samples cannot be rewritten, an image also painted where
	// the regions do not cover the same samples (blanking rewrites the image,
	// so that other place would lose them too), or a removed run whose advance
	// later text depends on and cannot be kept. Removing more than was asked
	// changes the page outside the region, and reporting success over content
	// still in the file is worse than refusing, so litho refuses and names the
	// construct. The [Writer] is left exactly as it was. [Writer.WriteTo]
	// returns it too when a region it re-applies at write time, over a page a
	// later stage rebuilt, can no longer be removed exactly.
	ErrRedactionRefused = errors.New("redaction refused: the region covers content that cannot be removed exactly")

	// ErrRedactionIncomplete is returned by [Writer.Redact] when the
	// document-wide passes ran but the verification scan still found a copy of
	// a removed string, or could not read an object and so cannot prove it
	// clean. Clearing a page is not clearing a document: the same text lives
	// in the structure tree, annotation values, outlines and metadata.
	// [RedactReport.SurvivingCopies] and [RedactReport.UnreadableObjects] name
	// where, and the [Writer] is left untouched so a caller that ignores the
	// error cannot write a half-redacted document. [Writer.WriteTo] returns it,
	// writing nothing, when a removed string survives in the output it is about
	// to write, for example one a later stage put back.
	ErrRedactionIncomplete = errors.New("redaction incomplete: a copy of the removed text survives in the document")

	// ErrPageDeletionIncomplete is returned by [Writer.WriteTo] when
	// [Writer.DeletePages] could not walk one of the document's reference
	// structures to the bottom, because it nests deeper than that prune's depth
	// bound.
	//
	// Deleting a page means unhooking every reference that reaches it: the
	// structure tree through /Pg, the outline through /Dest and /A, the named
	// destination trees, and a field's /Kids through its widgets' /P. The
	// rewrite copies whatever the catalog still reaches, so a reference left
	// behind keeps the deleted page and its content stream alive. Writing that
	// file would report one fewer page while still carrying the page's bytes,
	// which is worse than refusing for a caller deleting a page to remove what
	// is on it. Nothing is written. Real documents nest a few dozen levels in
	// any of these structures, so none of them reaches this.
	ErrPageDeletionIncomplete = errors.New("page deletion incomplete: a prune stopped at its depth bound, so the deleted page would survive in the output")

	// ErrAttachmentTreeIncomplete is returned by [Writer.WriteTo],
	// [Split] and [Merge] when the document's /EmbeddedFiles name tree nests
	// deeper than the attachment walk reads.
	//
	// Adding, removing, splitting and merging all rebuild that tree from what
	// the walk collected, so an entry below the bound would be absent from the
	// rebuilt tree: adding one attachment would destroy the others, and a split
	// or merge would carry none of them across. The same truncation makes
	// [Writer.RemoveAttachment] match nothing and report success having removed
	// nothing. Every outcome is silent data loss or a false success, so litho
	// refuses and writes nothing. Real trees nest two or three levels, so no
	// ordinary document reaches this.
	ErrAttachmentTreeIncomplete = errors.New("attachment tree incomplete: the embedded-files tree nests deeper than the attachment walk reads, so rebuilding it would drop entries")

	// ErrNameHoldsNull is returned, wrapped with the name, by
	// [Writer.WriteTo], [Split] and [Merge] when an object they write holds
	// a name with a NUL byte in it. ISO 32000-1, 7.3.5 excludes NUL from
	// every name, so no spelling of that name conforms. Only a "#00" escape
	// in the source produces one: writing it back repeats the fault, and
	// dropping the byte renames it, which can collide with a name the
	// document already uses or orphan a content stream that still spells it
	// the old way. litho refuses rather than write either. The destination
	// then holds an incomplete file, as it does after any write error.
	ErrNameHoldsNull = writer.ErrNameHoldsNull

	// ErrRemediationBrokePDF is returned by [Writer.RemediatePDFA] and
	// [Writer.RemediatePDFUA] when applied fixes cause the output to
	// regress beyond the configured [RegressionTolerance] and rolling back
	// content-modifying fixers did not recover the document. The source
	// [Reader] is left untouched; the caller can retry with a looser
	// tolerance, disable aggressive fixes, or report the document as
	// unfixable.
	ErrRemediationBrokePDF = errors.New("remediation broke the PDF beyond the configured regression tolerance")

	// ErrRemediationUnverified is returned, wrapping the file-system error,
	// by [Writer.RemediatePDFA] and the other remediation methods when the
	// remediation cannot create, write or close the temp file it reopens to
	// revalidate its output. Without that file litho can neither check the
	// output for regressions nor list what it leaves unrepaired, so the
	// remediation is undone rather than reported. Point the temp files at a
	// writable directory with [WithTempDir].
	ErrRemediationUnverified = errors.New("remediation output could not be written to a temp file to verify it")

	// ErrEmptyInvoiceXML is returned by [Writer.RemediateFacturX] when
	// [FacturXConfig.InvoiceXML] is nil or zero-length. Factur-X
	// requires an embedded Cross-Industry Invoice payload.
	ErrEmptyInvoiceXML = errors.New("factur-x invoice XML is empty")

	// ErrNoInvoiceAttachment is returned by [Writer.RepairFacturX] when
	// the document embeds no attachment named after a known hybrid
	// invoice convention (factur-x.xml, zugferd-invoice.xml,
	// ZUGFeRD-invoice.xml).
	ErrNoInvoiceAttachment = errors.New("no hybrid invoice attachment found")

	// ErrInvalidFacturXLevel is returned by [Writer.RemediateFacturX]
	// when [FacturXConfig.Level] does not match one of the documented
	// [models.FacturXLevel] constants.
	ErrInvalidFacturXLevel = errors.New("factur-x conformance level is invalid")

	// ErrInvalidFacturXProfile is returned by [Writer.RemediateFacturX]
	// when [FacturXConfig.Profile] is not a PDF/A-3 conformance level.
	// Factur-X 1.0.07 mandates a PDF/A-3 archival base.
	ErrInvalidFacturXProfile = errors.New("factur-x requires a PDF/A-3 profile")

	// ErrInvalidZUGFeRDVersion is returned by [Reader.ValidateZUGFeRD]
	// when the version argument does not match one of the documented
	// [models.ZUGFeRDVersion] constants.
	ErrInvalidZUGFeRDVersion = errors.New("zugferd version is invalid")

	// ErrInvalidCMYKProfile is returned by [Open] and [OpenFile] when the
	// profile given to [WithDefaultCMYKProfile] is not a CMYK ICC profile
	// litho can convert colours through.
	ErrInvalidCMYKProfile = errors.New("invalid default CMYK profile")

	// ErrInvalidLimits is returned, wrapped in an error naming the field, by
	// [Limits.Validate], and by [Open] and [OpenFile] when a budget given to
	// [WithLimits] is negative or above its ceiling. See [Limits].
	ErrInvalidLimits = limits.ErrInvalid

	// ErrStreamTooLarge is returned, wrapped, when decoding a stream would
	// produce more than [Limits.MaxStreamBytes] bytes: by [Reader.DecodeImage]
	// and an image's Decode function alongside [ErrImageNotDecodable], and by
	// the Open function of an image or an embedded file.
	ErrStreamTooLarge = filter.ErrDecompressionBomb

	// ErrPageContentTooLarge is returned, wrapped, when the decoded content of
	// a page, summed across the streams of its /Contents array, takes more
	// than a quarter of [Limits.MaxStreamBytes]: by [Reader.ExtractText] and
	// the other calls that read a page's operators, by [Writer.Redact]
	// alongside [ErrRedactionRefused], and by [Writer.WriteTo] for a
	// [Writer.StampPDF] whose source page is that heavy. Validation and
	// remediation read such a page as they read a damaged one instead.
	ErrPageContentTooLarge = limits.ErrPageContentTooLarge

	// ErrImageTooLarge is returned, wrapped alongside [ErrImageNotDecodable],
	// by [Reader.DecodeImage] and an image's Decode function when the image
	// declares more than [Limits.MaxImagePixels] pixels, or its decoded
	// samples take more than four bytes a pixel of that budget.
	ErrImageTooLarge = pdfimage.ErrImageTooLarge

	// ErrSchematronRuleSkipped is returned by [Reader.ValidateFacturX]
	// and [Reader.ValidateZUGFeRD] as an advisory entry in the
	// violations slice whenever a Schematron rule's XPath could not
	// be compiled or evaluated by the bundled engine. The rule's
	// verdict is unknown; the document neither passes nor fails it.
	//
	// Use [errors.Is] to separate advisories from real violations:
	//
	//	for _, e := range errs {
	//	    if errors.Is(e, pdf.ErrSchematronRuleSkipped) { continue }
	//	    // real violation
	//	}
	//
	// The wrapped error's string identifies the rule id and the
	// XPath construct that triggered the skip (typically an
	// XPath 2.0 function the engine does not implement, e.g.
	// "tokenize", "for $x in ... return", "string-to-codepoints").
	ErrSchematronRuleSkipped = compliance.ErrSchematronRuleSkipped

	// ErrNoMetadata is returned by [Reader.Metadata] when the document
	// carries neither an /Info dictionary nor an XMP packet. Absence is
	// not a failure for most callers: match it with [errors.Is] to tell
	// "no metadata" from a genuine extraction error.
	ErrNoMetadata = semantics.ErrNoMetadata
)

// Pipeline sentinel errors surface structural problems with a [Pipeline]
// declaration. Each is wrapped in a [PipelineStageError] so callers can
// match on the sentinel via [errors.Is] while still inspecting the
// offending stage via [errors.As].
//
// Sign combined with encryption or any other full-rewrite operation
// is enforced by [Writer.WriteTo] at execution time and surfaces as
// [ErrSignFullRewriteIncompatible], a regular error from
// [Pipeline.WriteTo], not a pipeline sentinel.
var (
	// ErrPipelineEmpty indicates a pipeline has no source stage.
	ErrPipelineEmpty = errors.New("pipeline has no source")

	// ErrPipelineNoSource indicates a stage was declared before
	// [Pipeline.Open].
	ErrPipelineNoSource = errors.New("pipeline stage requires a loaded source")

	// ErrPipelineNothingToMerge indicates [Pipeline.Merge] was called
	// with an empty slice of other sources.
	ErrPipelineNothingToMerge = errors.New("merge requires at least one other source")

	// ErrPipelineEmptyPageList indicates [Pipeline.Split] was called
	// with an empty page list.
	ErrPipelineEmptyPageList = errors.New("split requires at least one page")
)

// PipelineStageError locates a validation or execution failure to a specific
// pipeline stage. Use [errors.As] to extract it; the wrapped Err matches one
// of the pipeline sentinel errors for structural problems, or an execution
// error for runtime failures.
// Values must not be shared across goroutines without synchronization.
type PipelineStageError struct {
	// Index is the 0-based position of the stage in declaration order.
	Index int
	// Name is the short stage identifier, e.g. "split", "remediate-pdfa".
	Name string
	// Err is the underlying error.
	Err error
}

// Error implements the error interface.
func (e *PipelineStageError) Error() string {
	if e == nil || e.Err == nil {
		return "pipeline stage error"
	}
	return fmt.Sprintf("pipeline stage %d (%s): %s", e.Index, e.Name, e.Err.Error())
}

// Unwrap returns the wrapped error so callers can use [errors.Is].
func (e *PipelineStageError) Unwrap() error { return e.Err }
