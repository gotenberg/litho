package pdf

import (
	"context"
	"errors"
	"io"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/crypto"
	"github.com/gotenberg/litho/pkg/models"
)

// Pipeline chains write operations against a single PDF state that evolves
// from stage to stage. Call [Pipeline.Open] to seed the pipeline, queue
// stages in declaration order, then call [Pipeline.WriteTo] to execute the
// pipeline atomically (nothing is written to dst until the final stage).
//
// Pure transforms (SetMetadata, RemediatePDFA, etc.) accumulate on the
// current document. Restructuring stages (Split, Merge) materialize the
// current state to an intermediate temp file and reopen it as a new Reader
// for the next stage. Temp files are cleaned up automatically on both the
// success and failure paths.
//
// The first validation error recorded while building the pipeline is
// surfaced via [Pipeline.Err] and re-returned from [Pipeline.WriteTo].
// Callers who want eager validation can call [Pipeline.Validate] before
// executing.
type Pipeline struct {
	stages   []stage
	firstErr error
}

// PipelineResult aggregates per-stage execution info returned from
// [Pipeline.WriteTo].
type PipelineResult struct {
	// Stages reports one entry per executed stage in declaration order.
	Stages []StageReport
	// RemediationReports holds one entry per Remediate* stage that ran,
	// in declaration order.
	RemediationReports []*RemediationReport
}

// StageReport summarizes the execution of a single pipeline stage.
type StageReport struct {
	// Index is the 0-based position of the stage in declaration order.
	Index int
	// Name is the short stage identifier, e.g. "split", "remediate-pdfa".
	Name string
	// Materialized is true if the stage flushed the pipeline state to an
	// intermediate temp file or to the final destination.
	Materialized bool
	// BytesOut is the size of the intermediate file for restructuring
	// stages, or 0 for pure-transform stages and for the final stage.
	BytesOut int64
}

// NewPipeline creates an empty Pipeline. Seed it with [Pipeline.Open].
func NewPipeline() *Pipeline { return &Pipeline{} }

// Err returns the first build-time validation error recorded during chain
// building, or nil. Chain methods keep returning the receiver after an error
// so callers can defer error handling to [Pipeline.WriteTo].
func (p *Pipeline) Err() error { return p.firstErr }

// setErr records the first build-time error. Subsequent errors are ignored
// to preserve the earliest signal.
func (p *Pipeline) setErr(op string, index int, err error) {
	if p.firstErr != nil {
		return
	}
	p.firstErr = &PipelineStageError{Index: index, Name: op, Err: err}
}

// append adds a stage and returns the pipeline for chaining.
func (p *Pipeline) append(s stage) *Pipeline {
	p.stages = append(p.stages, s)
	return p
}

// Open sets the initial source document. Must be the first stage. The
// caller retains ownership of src and is responsible for closing it.
func (p *Pipeline) Open(src *Reader) *Pipeline {
	return p.append(&openStage{src: src})
}

// Split keeps the given 1-based pages (order preserved, duplicates allowed)
// as the new current document. Honors [WithBookmarks] on the stage itself;
// other [WriteOption] values on this stage are ignored (pass them to
// [Pipeline.WriteTo] instead).
func (p *Pipeline) Split(pages []int, opts ...WriteOption) *Pipeline {
	pagesCopy := append([]int(nil), pages...)
	return p.append(&splitStage{pages: pagesCopy, opts: opts})
}

// Merge appends others to the end of the current document. At least one
// other source is required. The caller retains ownership of each other
// reader. Honors [WithBookmarks] on the stage itself; other [WriteOption]
// values on this stage are ignored (pass them to [Pipeline.WriteTo] instead).
func (p *Pipeline) Merge(others []*Reader, opts ...WriteOption) *Pipeline {
	othersCopy := append([]*Reader(nil), others...)
	return p.append(&mergeStage{others: othersCopy, opts: opts})
}

// SetMetadata queues a metadata update.
func (p *Pipeline) SetMetadata(meta Metadata) *Pipeline {
	return p.append(&setMetadataStage{meta: meta})
}

// RotatePages queues a rotation for the given 1-based pages. Rotation must
// be a multiple of 90.
func (p *Pipeline) RotatePages(rotation int, pages ...int) *Pipeline {
	if rotation%90 != 0 {
		p.setErr("rotate-pages", len(p.stages), ErrPipelineInvalidRotation)
		return p
	}
	pagesCopy := append([]int(nil), pages...)
	return p.append(&rotatePagesStage{rotation: rotation, pages: pagesCopy})
}

// SetBookmarks queues a direct bookmark replacement.
func (p *Pipeline) SetBookmarks(bms []models.Bookmark) *Pipeline {
	bmsCopy := append([]models.Bookmark(nil), bms...)
	return p.append(&setBookmarksStage{bms: bmsCopy})
}

// GenerateBookmarks queues auto-generation of the outline.
func (p *Pipeline) GenerateBookmarks(opts BookmarkGenerationOptions, mode BookmarkMergeMode) *Pipeline {
	return p.append(&generateBookmarksStage{opts: opts, mode: mode})
}

// StampText queues a text overlay on the specified 1-based pages. If no
// pages are given, the stamp is applied to all pages.
func (p *Pipeline) StampText(stamp TextStamp, pages ...int) *Pipeline {
	pagesCopy := append([]int(nil), pages...)
	return p.append(&stampTextStage{stamp: stamp, pages: pagesCopy})
}

// AddAttachment queues an embedded file addition.
func (p *Pipeline) AddAttachment(name string, data []byte, mimeType string) *Pipeline {
	dataCopy := append([]byte(nil), data...)
	return p.append(&addAttachmentStage{fileName: name, data: dataCopy, mimeType: mimeType})
}

// SetFormField queues a form field value update.
func (p *Pipeline) SetFormField(name, value string) *Pipeline {
	return p.append(&setFormFieldStage{fieldName: name, value: value})
}

// FlattenForm queues a form flattening operation. Forces a full rewrite of
// the current document segment.
func (p *Pipeline) FlattenForm() *Pipeline {
	return p.append(&flattenFormStage{})
}

// Encrypt queues password-based encryption on the current document segment.
// Forces a full rewrite. Rejected by [Pipeline.Validate] when combined with
// any RemediatePDFA* stage in the same pipeline.
func (p *Pipeline) Encrypt(cfg crypto.EncryptConfig) *Pipeline {
	return p.append(&encryptStage{cfg: cfg})
}

// Decrypt queues removal of encryption on the current document segment.
// Forces a full rewrite.
func (p *Pipeline) Decrypt() *Pipeline {
	return p.append(&decryptStage{})
}

// Sign queues a digital signature. Must be the final stage in the pipeline.
// Rejected at validation time if combined with [Pipeline.Encrypt], and at
// execution time if a preceding stage forces a full rewrite.
func (p *Pipeline) Sign(signer Signer) *Pipeline {
	return p.append(&signStage{signer: signer})
}

// RemediatePDFA queues PDF/A remediation for the given profile. The report
// for this stage appears in [PipelineResult.RemediationReports] in
// declaration order after a successful run.
func (p *Pipeline) RemediatePDFA(profile models.PDFAProfile, opts ...RemediateOption) *Pipeline {
	return p.append(&remediatePDFAStage{profile: profile, opts: opts})
}

// RemediatePDFUA queues PDF/UA remediation for the given profile.
func (p *Pipeline) RemediatePDFUA(profile models.PDFUAProfile, opts ...RemediateOption) *Pipeline {
	return p.append(&remediatePDFUAStage{profile: profile, opts: opts})
}

// Validate runs all build-time checks without executing the pipeline. It
// returns nil on success or a joined error via [errors.Join]. Each element
// is a [*PipelineStageError] wrapping one of the pipeline sentinel errors.
// Callers can use [errors.Is] against the sentinels and [errors.As] against
// [*PipelineStageError] to inspect the stage index and name.
func (p *Pipeline) Validate() error {
	var errs []error
	if p.firstErr != nil {
		errs = append(errs, p.firstErr)
	}
	errs = append(errs, p.buildValidationErrors()...)
	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}

// WriteTo executes the pipeline and writes the final document to dst. The
// provided options are applied only at the final stage write. Nothing is
// written to dst until the last stage has produced output.
func (p *Pipeline) WriteTo(ctx context.Context, dst io.Writer, opts ...WriteOption) (*PipelineResult, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}

	cfg := writeOpts{}
	for _, o := range opts {
		o(&cfg)
	}
	logger := cfg.logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	ex := &executor{
		ctx:         ctx,
		tempDir:     cfg.pipelineTempDir,
		finalOpts:   opts,
		finalDst:    dst,
		totalStages: len(p.stages),
		logger:      logger,
		result:      &PipelineResult{},
	}
	defer ex.cleanup()

	for i, s := range p.stages {
		if err := ctx.Err(); err != nil {
			return nil, &PipelineStageError{Index: i, Name: s.name(), Err: err}
		}
		ex.stageIdx = i
		if err := s.apply(ctx, ex); err != nil {
			return nil, &PipelineStageError{Index: i, Name: s.name(), Err: err}
		}
	}

	// Terminal flush: if no restructuring stage already wrote the final
	// destination, apply any pending mutations (or copy the source as-is).
	if !ex.dstWritten {
		if ex.writer == nil {
			ex.writer = NewWriter(ex.cur)
		}
		if err := ex.writer.WriteTo(ctx, dst, opts...); err != nil {
			return nil, &PipelineStageError{
				Index: len(p.stages) - 1,
				Name:  "write",
				Err:   err,
			}
		}
	}

	return ex.result, nil
}
