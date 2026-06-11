package pdf

import (
	"errors"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/compliance"
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
	// at a public API boundary while processing document bytes. Internal
	// code is not supposed to panic; this sentinel exists so a crafted or
	// corrupt input cannot crash the host process. The recovered value
	// and a stack trace are logged at Error via the document logger.
	ErrInternalPanic = errors.New("internal panic recovered while processing the document")

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

	// ErrInvalidPageNumber is returned when a page number is outside the
	// valid range [1, pageCount].
	ErrInvalidPageNumber = errors.New("page number out of bounds")

	// ErrInvalidRotation is returned when a rotation value is not a
	// multiple of 90.
	ErrInvalidRotation = errors.New("rotation must be a multiple of 90")

	// ErrEmptyPageList is returned when a page operation receives an
	// empty page list.
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

	// ErrSourceIndexOutOfRange is returned when a source index exceeds
	// the number of available sources.
	ErrSourceIndexOutOfRange = errors.New("source index out of range")

	// ErrWriterConsumed is returned by [Writer.WriteTo] when the Writer
	// has already produced output. A Writer is single-use: its mutations
	// drain into the backing [Reader]'s in-memory state on the first
	// successful write, so a second write would re-apply an
	// already-mutated state and emit a corrupt document. Create a fresh
	// Writer (and Reader) for each distinct output.
	ErrWriterConsumed = errors.New("writer already consumed; create a new Writer")

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

	// ErrRemediationBrokePDF is returned by [Writer.RemediatePDFA] and
	// [Writer.RemediatePDFUA] when applied fixes cause the output to
	// regress beyond the configured [RegressionTolerance] and rolling back
	// content-modifying fixers did not recover the document. The source
	// [Reader] is left untouched; the caller can retry with a looser
	// tolerance, disable aggressive fixes, or report the document as
	// unfixable.
	ErrRemediationBrokePDF = errors.New("remediation broke the PDF beyond the configured regression tolerance")

	// ErrEmptyInvoiceXML is returned by [Writer.RemediateFacturX] when
	// [FacturXConfig.InvoiceXML] is nil or zero-length. Factur-X
	// requires an embedded Cross-Industry Invoice payload.
	ErrEmptyInvoiceXML = errors.New("factur-x invoice XML is empty")

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
