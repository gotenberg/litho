package pdf

import (
	"errors"
	"fmt"
)

// Exported errors for granular error handling by consumers.
var (
	// ErrInvalidPassword is returned when the provided password does not
	// unlock the encrypted document.
	ErrInvalidPassword = errors.New("invalid PDF password")

	// ErrNotAPDF is returned when the file does not start with a valid
	// %PDF- header.
	ErrNotAPDF = errors.New("not a valid PDF document (missing %PDF- header)")

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
)

// Pipeline sentinel errors surface structural problems with a [Pipeline]
// declaration. Each is wrapped in a [PipelineStageError] so callers can
// match on the sentinel via [errors.Is] while still inspecting the offending
// stage via [errors.As].
var (
	// ErrPipelineEmpty indicates a pipeline has no source stage.
	ErrPipelineEmpty = errors.New("pipeline has no source")

	// ErrPipelineNoSource indicates a stage was declared before [Pipeline.Open].
	ErrPipelineNoSource = errors.New("pipeline stage requires a loaded source")

	// ErrPipelineSignNotLast indicates [Pipeline.Sign] is not the final stage.
	ErrPipelineSignNotLast = errors.New("sign must be the final pipeline stage")

	// ErrPipelineSignWithEncrypt indicates [Pipeline.Sign] is combined with
	// [Pipeline.Encrypt] in the same pipeline.
	ErrPipelineSignWithEncrypt = errors.New("sign is incompatible with encrypt in the same pipeline")

	// ErrPipelineSignWithFullRewrite indicates a remediation stage forced a
	// full rewrite, which is incompatible with the incremental signing flow.
	ErrPipelineSignWithFullRewrite = errors.New("sign is incompatible with stages that force full rewrite")

	// ErrPipelineEncryptWithPDFA indicates [Pipeline.Encrypt] is combined with
	// a PDF/A remediation stage. PDF/A forbids encryption.
	ErrPipelineEncryptWithPDFA = errors.New("encrypt is incompatible with PDF/A remediation")

	// ErrPipelineFormAfterFlatten indicates a form field mutation follows
	// [Pipeline.FlattenForm] without an intervening restructure.
	ErrPipelineFormAfterFlatten = errors.New("form field mutation after FlattenForm")

	// ErrPipelineNothingToMerge indicates [Pipeline.Merge] was called with
	// an empty slice of other sources.
	ErrPipelineNothingToMerge = errors.New("merge requires at least one other source")

	// ErrPipelineEmptyPageList indicates [Pipeline.Split] was called with
	// an empty page list.
	ErrPipelineEmptyPageList = errors.New("split requires at least one page")

	// ErrPipelineInvalidRotation indicates [Pipeline.RotatePages] was called
	// with a rotation that is not a multiple of 90.
	ErrPipelineInvalidRotation = errors.New("rotation must be a multiple of 90")
)

// PipelineStageError locates a validation or execution failure to a specific
// pipeline stage. Use [errors.As] to extract it; the wrapped Err matches one
// of the pipeline sentinel errors for structural problems, or an execution
// error for runtime failures.
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
