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
	"github.com/gotenberg/litho/pkg/internal/mutations"
	"github.com/gotenberg/litho/pkg/internal/remediation"
	"github.com/gotenberg/litho/pkg/internal/semantics"
	"github.com/gotenberg/litho/pkg/internal/signature"
	"github.com/gotenberg/litho/pkg/internal/tagging"
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
	// drain into the Writer's copy of the document on the first successful
	// write, so a second write would re-apply an already-mutated state and
	// emit a corrupt document. Create a fresh Writer for each distinct
	// output: the [Reader] it is built on is left as it was.
	ErrWriterConsumed = errors.New("writer already consumed; create a new Writer")

	// ErrWriterClosed is returned by [Writer.WriteTo], [Writer.Redact] and
	// the remediation methods after [Writer.Close]. Close releases the
	// remediated output a Writer holds, and a later write cannot rebuild it
	// in full: the later remediation rounds ran against that output, not
	// against the Writer's queue.
	ErrWriterClosed = errors.New("writer closed; create a new Writer")

	// ErrSignFullRewriteIncompatible is returned by [Writer.WriteTo] when
	// [Writer.Sign] is combined with [Writer.Encrypt],
	// [Writer.RemoveEncryption] or [WithEncryption], or with an encrypted
	// source, a remediated one included, or one a [Pipeline] split or merged
	// and reopened in the clear. Signing covers a ByteRange of the
	// existing bytes via an incremental update, and the incremental update of
	// an encrypted file would append the signature dictionary's strings in
	// plaintext where its security handler requires ciphertext (ISO 32000-1,
	// 7.6.1). Encrypted-then-signed output is permitted by the spec but not yet
	// implemented.
	//
	// A full rewrite does not return it, despite the name: [WithFullRewrite],
	// an edit that forces one ([Writer.DeletePages], [Writer.InsertPages],
	// [Writer.FlattenForm], [Writer.ClearMetadata], [Writer.Redact]) and a
	// remediation whose repairs need one, such as the formatting a PDF/A
	// profile requires of the header and of every object. WriteTo writes the
	// rewrite and signs it in an incremental update on top.
	ErrSignFullRewriteIncompatible = errors.New("sign is incompatible with encryption and decryption")

	// ErrSignatureFieldUnavailable is returned by [Writer.WriteTo] when the
	// field [WithSignatureField] names cannot take the signature: a field of
	// that name exists and is not a signature field, is signed already, or
	// carries a /Lock or required seed values, or no field has the name and it
	// holds a period, which a new root field's name cannot (ISO 32000-1,
	// 12.7.3.2). Nothing is written.
	ErrSignatureFieldUnavailable = mutations.ErrSignatureFieldUnavailable

	// ErrSignerKeyUnsupported is returned by [NewSigner] and [ParseSignerPEM]
	// for a private key other than RSA or ECDSA on P-256, P-384 or P-521.
	ErrSignerKeyUnsupported = signature.ErrSignerKeyUnsupported

	// ErrSignerKeyMismatch is returned by [NewSigner] and [ParseSignerPEM] when
	// the private key is not the one of the signing certificate, or when either
	// is missing.
	ErrSignerKeyMismatch = signature.ErrSignerKeyMismatch

	// ErrSignerCertificateUsage is returned by [NewSigner] and
	// [ParseSignerPEM] for a certificate whose key usage extension allows
	// neither digitalSignature nor nonRepudiation (RFC 5280, 4.2.1.3).
	ErrSignerCertificateUsage = signature.ErrSignerCertificateUsage

	// ErrSignerKeyEncrypted is returned by [ParseSignerPEM] for a password-
	// protected private key, an ENCRYPTED PRIVATE KEY block (RFC 5958) or a
	// legacy PEM block with a Proc-Type ENCRYPTED header (RFC 1421). Decrypt
	// it first, or build the key and call [NewSigner].
	ErrSignerKeyEncrypted = errors.New("private key is encrypted")

	// ErrSignerPEM is returned by [ParseSignerPEM] for input that holds no
	// certificate, no private key or more than one, or a block that does not
	// parse.
	ErrSignerPEM = errors.New("invalid signer PEM")

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

	// ErrAutoTagAlreadyTagged is what [AutoTagReport.Skipped] wraps when
	// [Writer.AutoTag] left the document alone because it already declares a
	// logical structure: a catalog /StructTreeRoot (ISO 32000-1, 14.7.2) or
	// /MarkInfo with /Marked true (ISO 32000-1, 14.7.1). A second tree would
	// replace the producer's. Like every skip, it is reported, not returned:
	// the output is written without a new tree.
	ErrAutoTagAlreadyTagged = tagging.ErrAlreadyTagged

	// ErrAutoTagMarkedContent is what [AutoTagReport.Skipped] wraps when
	// [Writer.AutoTag] left the document alone because a page's content
	// already holds a marked-content sequence, a BDC or BMC operator (ISO
	// 32000-1, 14.6), and marking it again would nest or cross it. That
	// includes the /Artifact sequence a stamp queued before AutoTag wraps its
	// content in. [AutoTagReport.Page] names the first such page.
	ErrAutoTagMarkedContent = tagging.ErrMarkedContent

	// ErrAutoTagUnreadableContent is what [AutoTagReport.Skipped] wraps when
	// [Writer.AutoTag] left the document alone because a page's content
	// cannot be assembled, so it cannot be shown to be unmarked.
	// [AutoTagReport.Page] names the first such page.
	ErrAutoTagUnreadableContent = tagging.ErrUnreadableContent

	// ErrAutoTagNothingToTag is what [AutoTagReport.Skipped] wraps when
	// [Writer.AutoTag] left the document alone because no page draws text or
	// an image it places in a structure element. Text drawn only inside form
	// XObjects counts as none: AutoTag does not descend into them.
	ErrAutoTagNothingToTag = tagging.ErrNothingToTag

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

	// ErrFontNotSupplied is the reason of a [FontRefusal] for a font the
	// [FontProvider] supplied no program for.
	ErrFontNotSupplied = remediation.ErrFontNotSupplied

	// ErrFontProgramInvalid is the reason of a [FontRefusal] for a supplied
	// program litho cannot read as a TrueType, OpenType or CFF font, or one
	// larger than the document's stream budget ([Limits]).
	ErrFontProgramInvalid = remediation.ErrFontProgramInvalid

	// ErrFontEmbeddingRestricted is the reason of a [FontRefusal] for a
	// supplied program whose OS/2 fsType forbids embedding its outlines.
	ErrFontEmbeddingRestricted = remediation.ErrFontEmbeddingRestricted

	// ErrFontKind is the reason of a [FontRefusal] for a font the supplied
	// program cannot serve: a TrueType font given CFF outlines, a symbolic
	// Type 1 font given TrueType outlines, a multiple master font, or a
	// composite font whose CIDs name no characters, such as an Identity
	// ordering.
	ErrFontKind = remediation.ErrFontKind

	// ErrFontCoverage is the reason of a [FontRefusal] for a supplied program
	// lacking a glyph for a code the document shows.
	ErrFontCoverage = remediation.ErrFontCoverage

	// ErrFontWidths is the reason of a [FontRefusal] for a supplied program
	// whose advances disagree with the widths the font declares by more than
	// one unit, so text would move, or for a font that declares no widths.
	ErrFontWidths = remediation.ErrFontWidths

	// ErrFontRejected is the reason of a [FontRefusal] for a font that,
	// embedded, would fail a rule of the profile it did not fail before.
	ErrFontRejected = remediation.ErrFontRejected

	// ErrFontUnwritable is the reason of a [FontRefusal] for a font
	// dictionary written inline in a resource dictionary that no object
	// number reaches, so no embedded font can be written in its place.
	ErrFontUnwritable = remediation.ErrFontUnwritable

	// ErrFontRolledBack is the reason of a [FontRefusal] for a font whose
	// embedding was discarded with the other content repairs of its round,
	// when the output failed the integrity checks.
	ErrFontRolledBack = remediation.ErrFontRolledBack

	// errFontFileTooLarge marks a font file [WithFontFiles] skips, one past
	// [maxFontFileBytes].
	errFontFileTooLarge = errors.New("font file too large")

	// ErrEditAfterRemediation is returned, wrapped with the names of the
	// refused call and of the remediation, when a [Writer] is asked for an edit
	// after a remediation ([Writer.RemediatePDFA], [Writer.RemediatePDFUA],
	// [Writer.RemediateWTPDF], [Writer.RemediateFacturX] or
	// [Writer.RepairFacturX]), a second remediation included.
	//
	// A remediation validates and repairs the document as edited when it runs,
	// and its [RemediationReport] describes the output only if nothing edits
	// that document afterwards. After one, a Writer accepts [Writer.Sign],
	// [Writer.WriteTo] and [Writer.Close], and refuses everything else: queue
	// the edit before the remediation. A refused call queues nothing. A
	// method that has an error return hands the refusal back at once, and
	// every refusal, whatever the method, is also recorded: [Writer.WriteTo]
	// returns the first one and writes nothing. [Pipeline.WriteTo] returns it too for a
	// [Pipeline.Split] or [Pipeline.Merge] stage that follows a remediation.
	ErrEditAfterRemediation = errors.New("edit after a remediation; queue it before the remediation")

	// ErrPDFAForbidsEncryption is returned, naming the clause, when the output
	// of a PDF/A remediation would be encrypted. Every part of ISO 19005 forbids
	// the Encrypt key in the trailer: ISO 19005-1, 6.1.3, ISO 19005-2, 6.1.3,
	// ISO 19005-3, 6.1.3 and ISO 19005-4, 6.1.3. [Writer.RemediatePDFA],
	// [Writer.RemediateFacturX] and [Writer.RepairFacturX], whose Factur-X
	// output is a PDF/A-3 file, return it when [Writer.Encrypt] was called
	// before them or [NewWriter] was given [WithEncryption], and
	// [Writer.WriteTo] and [Pipeline.WriteTo] return it when WithEncryption
	// asks to encrypt what such a remediation produced. Nothing is written.
	ErrPDFAForbidsEncryption = errors.New("PDF/A forbids encryption: the trailer shall not contain the Encrypt key")

	// ErrPDFUAForbidsBarringAccessibility is returned, naming the clause, when
	// the output of a PDF/UA-1 remediation would be encrypted with permissions
	// that clear bit 10 of the P key, the permission to extract content for
	// accessibility (ISO 32000-1, 7.6.3.2, Table 22). ISO 14289-1, 7.16
	// requires an encrypted conforming file to set it.
	// [Writer.RemediatePDFUA] returns it when [Writer.Encrypt] or the
	// [WithEncryption] given to [NewWriter] asked for such permissions before
	// it, and [Writer.WriteTo] and [Pipeline.WriteTo] return it when
	// [WithEncryption] asks for them after it. Nothing is written.
	ErrPDFUAForbidsBarringAccessibility = errors.New("PDF/UA-1 requires an encrypted file to permit content extraction for accessibility: bit 10 of the P key shall be set")

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
// Sign combined with encryption or decryption is enforced by
// [Writer.WriteTo] at execution time and surfaces as
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
