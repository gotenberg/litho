package pdf

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/gotenberg/litho/pkg/models"
)

// Pipeline chains write operations against a single PDF state that evolves
// from stage to stage. Call [Pipeline.Open] to seed the pipeline, queue
// stages in declaration order, then call [Pipeline.WriteTo] to execute the
// pipeline. Nothing is written to dst until the last stage has produced
// output.
//
// Pure transforms ([Pipeline.SetMetadata], [Pipeline.RemediatePDFA], ...)
// accumulate on the current document. Restructuring stages ([Pipeline.Split],
// [Pipeline.Merge]) materialize the current state to an intermediate temp
// file and reopen it as a new [Reader] for the next stage. Temp files are
// backed by [os.CreateTemp] and cleaned up automatically on both the
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
	// RemediationReports holds one entry per [Pipeline.RemediatePDFA] or
	// [Pipeline.RemediatePDFUA] stage that ran, in declaration order.
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

// NewPipeline creates an empty [Pipeline]. Seed it with [Pipeline.Open].
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
// caller retains ownership of src and is responsible for calling
// [Reader.Close] on it.
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

// SetMetadata queues a metadata update on the current document. Mirrors
// [Writer.SetMetadata].
func (p *Pipeline) SetMetadata(meta Metadata) *Pipeline {
	return p.append(&setMetadataStage{meta: meta})
}

// RotatePages queues a rotation for the given 1-based pages. Rotation must
// be a multiple of 90; otherwise [Pipeline.Err] reports
// [ErrPipelineInvalidRotation].
func (p *Pipeline) RotatePages(rotation int, pages ...int) *Pipeline {
	if rotation%90 != 0 {
		p.setErr(stageNameRotatePages, len(p.stages), ErrPipelineInvalidRotation)
		return p
	}
	pagesCopy := append([]int(nil), pages...)
	return p.append(&rotatePagesStage{rotation: rotation, pages: pagesCopy})
}

// SetBookmarks queues a direct bookmark replacement. Mirrors
// [Writer.SetBookmarks].
func (p *Pipeline) SetBookmarks(bms []models.Bookmark) *Pipeline {
	bmsCopy := append([]models.Bookmark(nil), bms...)
	return p.append(&setBookmarksStage{bms: bmsCopy})
}

// GenerateBookmarks queues auto-generation of the outline. Mirrors
// [Writer.GenerateBookmarks].
func (p *Pipeline) GenerateBookmarks(opts BookmarkGenerationOptions, mode BookmarkMergeMode) *Pipeline {
	return p.append(&generateBookmarksStage{opts: opts, mode: mode})
}

// StampText queues a text overlay on the specified 1-based pages. If no
// pages are given, the stamp is applied to all pages. Mirrors
// [Writer.StampText].
func (p *Pipeline) StampText(stamp TextStamp, pages ...int) *Pipeline {
	pagesCopy := append([]int(nil), pages...)
	return p.append(&stampTextStage{stamp: stamp, pages: pagesCopy})
}

// StampImage queues an image overlay on the specified 1-based pages.
// Mirrors [Writer.StampImage].
func (p *Pipeline) StampImage(stamp ImageStamp, pages ...int) *Pipeline {
	pagesCopy := append([]int(nil), pages...)
	return p.append(&stampImageStage{stamp: stamp, pages: pagesCopy})
}

// StampPDF queues a PDF-page overlay on the specified 1-based pages.
// Mirrors [Writer.StampPDF].
func (p *Pipeline) StampPDF(stamp PDFStamp, pages ...int) *Pipeline {
	pagesCopy := append([]int(nil), pages...)
	return p.append(&stampPDFStage{stamp: stamp, pages: pagesCopy})
}

// AddAttachment queues an embedded file addition. Mirrors
// [Writer.AddAttachment].
func (p *Pipeline) AddAttachment(name string, data []byte, mimeType string) *Pipeline {
	dataCopy := append([]byte(nil), data...)
	return p.append(&addAttachmentStage{fileName: name, data: dataCopy, mimeType: mimeType})
}

// SetFormField queues a form field value update. Mirrors
// [Writer.SetFormField].
func (p *Pipeline) SetFormField(name, value string) *Pipeline {
	return p.append(&setFormFieldStage{fieldName: name, value: value})
}

// FlattenForm queues a form flattening operation. Forces a full rewrite of
// the current document segment. Mirrors [Writer.FlattenForm].
func (p *Pipeline) FlattenForm() *Pipeline {
	return p.append(&flattenFormStage{})
}

// Encrypt queues password-based encryption on the current document segment.
// Forces a full rewrite. Rejected by [Pipeline.Validate] when combined with
// any [Pipeline.RemediatePDFA] stage in the same pipeline.
func (p *Pipeline) Encrypt(cfg EncryptConfig) *Pipeline {
	return p.append(&encryptStage{cfg: cfg})
}

// Decrypt queues removal of encryption on the current document segment.
// Forces a full rewrite. Mirrors [Writer.Decrypt].
func (p *Pipeline) Decrypt() *Pipeline {
	return p.append(&decryptStage{})
}

// Sign queues a digital signature. Must be the final stage in the pipeline.
// Rejected by [Pipeline.Validate] if combined with [Pipeline.Encrypt], and
// at execution time if a preceding stage forces a full rewrite (the sentinel
// in that case is [ErrPipelineSignWithFullRewrite]).
func (p *Pipeline) Sign(signer Signer) *Pipeline {
	return p.append(&signStage{signer: signer})
}

// RemediatePDFA queues PDF/A remediation for the given profile. The report
// for this stage appears in [PipelineResult.RemediationReports] in
// declaration order after a successful run. Mirrors [Writer.RemediatePDFA].
func (p *Pipeline) RemediatePDFA(profile models.PDFAProfile, opts ...RemediateOption) *Pipeline {
	return p.append(&remediatePDFAStage{profile: profile, opts: opts})
}

// RemediatePDFUA queues PDF/UA remediation for the given profile. Mirrors
// [Writer.RemediatePDFUA].
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

// buildValidationErrors evaluates the compatibility matrix against the
// declared stages and returns one [*PipelineStageError] per violated rule.
func (p *Pipeline) buildValidationErrors() []error {
	var errs []error

	if len(p.stages) == 0 {
		errs = append(errs, &PipelineStageError{
			Index: 0,
			Name:  stageNamePipeline,
			Err:   ErrPipelineEmpty,
		})
		return errs
	}

	// Structural flags accumulated while walking the stages.
	var (
		hasSource  bool
		hasFlatten bool
		hasEncrypt bool
		hasPDFA    bool
		signIdx    = -1
	)

	for i, s := range p.stages {
		if s.kind() == kindOpen {
			if i != 0 {
				// Open is only valid as the first stage.
				errs = append(errs, &PipelineStageError{
					Index: i,
					Name:  s.name(),
					Err:   ErrPipelineNoSource,
				})
			}
			hasSource = true
		} else if !hasSource {
			errs = append(errs, &PipelineStageError{
				Index: i,
				Name:  s.name(),
				Err:   ErrPipelineNoSource,
			})
		}

		switch st := s.(type) {
		case *splitStage:
			if len(st.pages) == 0 {
				errs = append(errs, &PipelineStageError{
					Index: i,
					Name:  s.name(),
					Err:   ErrPipelineEmptyPageList,
				})
			}
			// A restructure resets the form-flatten flag because the new
			// document can carry its own form state.
			hasFlatten = false
		case *mergeStage:
			if len(st.others) == 0 {
				errs = append(errs, &PipelineStageError{
					Index: i,
					Name:  s.name(),
					Err:   ErrPipelineNothingToMerge,
				})
			}
			hasFlatten = false
		case *flattenFormStage:
			hasFlatten = true
		case *setFormFieldStage:
			if hasFlatten {
				errs = append(errs, &PipelineStageError{
					Index: i,
					Name:  s.name(),
					Err:   ErrPipelineFormAfterFlatten,
				})
			}
		case *encryptStage:
			hasEncrypt = true
			if hasPDFA {
				errs = append(errs, &PipelineStageError{
					Index: i,
					Name:  s.name(),
					Err:   ErrPipelineEncryptWithPDFA,
				})
			}
		case *remediatePDFAStage:
			hasPDFA = true
			if hasEncrypt {
				errs = append(errs, &PipelineStageError{
					Index: i,
					Name:  s.name(),
					Err:   ErrPipelineEncryptWithPDFA,
				})
			}
		case *signStage:
			signIdx = i
		}
	}

	if signIdx >= 0 && signIdx != len(p.stages)-1 {
		errs = append(errs, &PipelineStageError{
			Index: signIdx,
			Name:  stageNameSign,
			Err:   ErrPipelineSignNotLast,
		})
	}
	if signIdx >= 0 && hasEncrypt {
		errs = append(errs, &PipelineStageError{
			Index: signIdx,
			Name:  stageNameSign,
			Err:   ErrPipelineSignWithEncrypt,
		})
	}

	return errs
}

// WriteTo executes the pipeline and writes the final document to dst. The
// provided options are applied only at the final stage write. Nothing is
// written to dst until the last stage has produced output.
//
// Pass [WithWriteLogger] to receive structured debug logs for every stage
// transition, temp-file flush, and cleanup action. Pass [WithPipelineTempDir]
// to redirect intermediate temp files away from [os.TempDir].
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

	logger.Debug("pipeline started",
		slog.Int("stages", len(p.stages)),
		slog.String("temp_dir", cfg.pipelineTempDir),
	)

	for i, s := range p.stages {
		if err := ctx.Err(); err != nil {
			logger.Debug("pipeline canceled",
				slog.Int("stage", i),
				slog.String("name", s.name()),
				slog.Any("err", err),
			)
			return nil, &PipelineStageError{Index: i, Name: s.name(), Err: err}
		}
		ex.stageIdx = i
		logger.Debug("stage starting",
			slog.Int("stage", i),
			slog.String("name", s.name()),
		)
		if err := s.apply(ctx, ex); err != nil {
			logger.Debug("stage failed",
				slog.Int("stage", i),
				slog.String("name", s.name()),
				slog.Any("err", err),
			)
			return nil, &PipelineStageError{Index: i, Name: s.name(), Err: err}
		}
	}

	// Terminal flush: if no restructuring stage already wrote the final
	// destination, apply any pending mutations (or copy the source as-is).
	if !ex.dstWritten {
		if ex.writer == nil {
			ex.writer = NewWriter(ex.cur)
		}
		logger.Debug("terminal flush to dst")
		if err := ex.writer.WriteTo(ctx, dst, opts...); err != nil {
			logger.Debug("terminal flush failed", slog.Any("err", err))
			return nil, &PipelineStageError{
				Index: len(p.stages) - 1,
				Name:  stageNameWrite,
				Err:   err,
			}
		}
	}

	logger.Debug("pipeline completed",
		slog.Int("stages_executed", len(ex.result.Stages)),
		slog.Int("remediation_reports", len(ex.result.RemediationReports)),
	)
	return ex.result, nil
}

// --- Internal: stages and executor ---

// stageKind identifies the category of a pipeline stage. Used by
// [Pipeline.buildValidationErrors] to apply the compatibility matrix.
type stageKind int

const (
	kindOpen stageKind = iota
	kindSplit
	kindMerge
	kindSetMetadata
	kindRotatePages
	kindSetBookmarks
	kindGenerateBookmarks
	kindStampText
	kindStampImage
	kindStampPDF
	kindAddAttachment
	kindSetFormField
	kindFlattenForm
	kindEncrypt
	kindDecrypt
	kindSign
	kindRemediatePDFA
	kindRemediatePDFUA
)

// Stage name constants. Used as the [StageReport.Name] and
// [PipelineStageError.Name] values so callers can match on them without
// hard-coding string literals.
const (
	stageNamePipeline          = "pipeline"
	stageNameWrite             = "write"
	stageNameOpen              = "open"
	stageNameSplit             = "split"
	stageNameMerge             = "merge"
	stageNameSetMetadata       = "set-metadata"
	stageNameRotatePages       = "rotate-pages"
	stageNameSetBookmarks      = "set-bookmarks"
	stageNameGenerateBookmarks = "generate-bookmarks"
	stageNameStampText         = "stamp-text"
	stageNameStampImage        = "stamp-image"
	stageNameStampPDF          = "stamp-pdf"
	stageNameAddAttachment     = "add-attachment"
	stageNameSetFormField      = "set-form-field"
	stageNameFlattenForm       = "flatten-form"
	stageNameEncrypt           = "encrypt"
	stageNameDecrypt           = "decrypt"
	stageNameSign              = "sign"
	stageNameRemediatePDFA     = "remediate-pdfa"
	stageNameRemediatePDFUA    = "remediate-pdfua"
)

// stage is the internal unit of work in a [Pipeline]. Concrete stages are
// small value types that hold the arguments of the call that created them.
type stage interface {
	kind() stageKind
	name() string
	apply(ctx context.Context, ex *executor) error
}

// executor drives the sequential execution of a pipeline's stages. It owns
// the current in-flight [Reader], the active pending-mutation [Writer], and
// the intermediate temp files that back restructuring boundaries.
type executor struct {
	ctx context.Context

	// cur is the current document state. Either the original source (owned
	// by the caller) or a reader the executor opened over an intermediate
	// temp file.
	cur *Reader
	// curOwned indicates whether the executor must close cur on cleanup.
	// True for every reader opened from a temp file; false for the initial
	// source passed by the caller.
	curOwned bool

	// writer holds mutations queued against cur. Created lazily on the first
	// pure-transform stage and reset to nil after every [executor.flushToTempFile].
	writer *Writer

	// tmpFiles are the intermediate PDF temp files created by flushes and
	// restructuring stages. Retained so the reopened readers stay valid for
	// the lifetime of the run. Cleaned up by [executor.cleanup] regardless
	// of outcome.
	tmpFiles []*os.File
	tempDir  string

	// finalOpts are the options passed to [Pipeline.WriteTo]. Applied only
	// at the final stage write (either a pure-transform terminal flush or a
	// restructuring stage that happens to be last).
	finalOpts []WriteOption
	finalDst  io.Writer

	// dstWritten indicates a stage has already written the final output.
	// Set by restructuring stages that target finalDst directly. When true,
	// the terminal flush at the end of [Pipeline.WriteTo] is skipped.
	dstWritten bool

	// stageIdx and totalStages track progress for logging and error wrapping.
	stageIdx    int
	totalStages int

	logger *slog.Logger
	result *PipelineResult
}

// isLastStage reports whether the currently running stage is the last one
// in the pipeline.
func (ex *executor) isLastStage() bool { return ex.stageIdx == ex.totalStages-1 }

// ensureWriter creates a [Writer] on the current reader if none exists.
// Called by every pure-transform stage before it queues a mutation.
func (ex *executor) ensureWriter() {
	if ex.writer == nil {
		ex.writer = NewWriter(ex.cur)
	}
}

// flushToTempFile materializes any pending mutations to a new temp file and
// reopens it as the current reader. Called immediately before a restructuring
// stage that is not the terminal stage. If there are no pending mutations,
// it is a no-op.
func (ex *executor) flushToTempFile() error {
	if ex.writer == nil {
		return nil
	}
	f, err := os.CreateTemp(ex.tempDir, "litho-pipeline-*.pdf")
	if err != nil {
		return fmt.Errorf("creating intermediate temp file: %w", err)
	}
	// Track the file even on error so [executor.cleanup] removes it.
	ex.tmpFiles = append(ex.tmpFiles, f)
	ex.logger.Debug("flushing pending mutations to temp file",
		slog.String("path", f.Name()),
		slog.Int("stage", ex.stageIdx),
	)
	if err := ex.writer.WriteTo(ex.ctx, f); err != nil {
		return fmt.Errorf("flushing pending mutations: %w", err)
	}
	return ex.reopenAsCurrent(f)
}

// reopenAsCurrent takes a temp file that just received a PDF write, seeks
// it to zero, opens it as a new [Reader], and installs it as cur. The
// previous owned reader is closed.
func (ex *executor) reopenAsCurrent(f *os.File) error {
	stat, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat intermediate file: %w", err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("seeking intermediate file: %w", err)
	}
	next, err := Open(ex.ctx, f, stat.Size())
	if err != nil {
		return fmt.Errorf("reopening intermediate file: %w", err)
	}
	if ex.curOwned && ex.cur != nil {
		if closeErr := ex.cur.Close(); closeErr != nil {
			ex.logger.Debug("closing previous intermediate reader failed",
				slog.Any("err", closeErr),
			)
		}
	}
	ex.cur = next
	ex.curOwned = true
	ex.writer = nil
	ex.logger.Debug("reopened intermediate file as current reader",
		slog.String("path", f.Name()),
		slog.Int64("size", stat.Size()),
	)
	return nil
}

// newTempFile allocates and tracks a new temp file without opening it.
func (ex *executor) newTempFile() (*os.File, error) {
	f, err := os.CreateTemp(ex.tempDir, "litho-pipeline-*.pdf")
	if err != nil {
		return nil, fmt.Errorf("creating intermediate temp file: %w", err)
	}
	ex.tmpFiles = append(ex.tmpFiles, f)
	return f, nil
}

// cleanup releases every resource the executor owns. Safe to call on both
// the success and failure paths.
func (ex *executor) cleanup() {
	if ex.curOwned && ex.cur != nil {
		if err := ex.cur.Close(); err != nil {
			ex.logger.Debug("closing current reader failed",
				slog.Any("err", err),
			)
		}
	}
	for _, f := range ex.tmpFiles {
		name := f.Name()
		if err := f.Close(); err != nil {
			ex.logger.Debug("closing temp file failed",
				slog.String("path", name),
				slog.Any("err", err),
			)
		}
		if err := os.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
			ex.logger.Debug("removing temp file failed",
				slog.String("path", name),
				slog.Any("err", err),
			)
		}
	}
}

// recordStage appends a [StageReport] to the result.
func (ex *executor) recordStage(name string, materialized bool, bytesOut int64) {
	ex.result.Stages = append(ex.result.Stages, StageReport{
		Index:        ex.stageIdx,
		Name:         name,
		Materialized: materialized,
		BytesOut:     bytesOut,
	})
}

// openStage seeds the pipeline with its initial source. Must be the first
// stage; validated by [Pipeline.buildValidationErrors].
type openStage struct {
	src *Reader
}

func (s *openStage) kind() stageKind { return kindOpen }
func (s *openStage) name() string    { return stageNameOpen }
func (s *openStage) apply(_ context.Context, ex *executor) error {
	ex.cur = s.src
	ex.curOwned = false
	ex.writer = nil
	ex.recordStage(s.name(), false, 0)
	return nil
}

// splitStage extracts the given 1-based pages. See [Pipeline.Split].
type splitStage struct {
	pages []int
	opts  []WriteOption
}

func (s *splitStage) kind() stageKind { return kindSplit }
func (s *splitStage) name() string    { return stageNameSplit }
func (s *splitStage) apply(ctx context.Context, ex *executor) error {
	if err := ex.flushToTempFile(); err != nil {
		return err
	}
	// Combine per-stage opts with finalOpts when this stage is the terminal
	// one. Stage opts come last so they win on conflicts.
	var opts []WriteOption
	if ex.isLastStage() {
		opts = append(opts, ex.finalOpts...)
	}
	opts = append(opts, s.opts...)

	if ex.isLastStage() {
		if err := Split(ctx, ex.cur, s.pages, ex.finalDst, opts...); err != nil {
			return err
		}
		ex.dstWritten = true
		ex.recordStage(s.name(), true, 0)
		return nil
	}
	f, err := ex.newTempFile()
	if err != nil {
		return err
	}
	if err := Split(ctx, ex.cur, s.pages, f, opts...); err != nil {
		return err
	}
	if err := ex.reopenAsCurrent(f); err != nil {
		return err
	}
	var size int64
	if stat, statErr := f.Stat(); statErr == nil {
		size = stat.Size()
	}
	ex.recordStage(s.name(), true, size)
	return nil
}

// mergeStage appends other sources after the current document. See
// [Pipeline.Merge].
type mergeStage struct {
	others []*Reader
	opts   []WriteOption
}

func (s *mergeStage) kind() stageKind { return kindMerge }
func (s *mergeStage) name() string    { return stageNameMerge }
func (s *mergeStage) apply(ctx context.Context, ex *executor) error {
	if err := ex.flushToTempFile(); err != nil {
		return err
	}
	sources := make([]*Reader, 0, len(s.others)+1)
	sources = append(sources, ex.cur)
	sources = append(sources, s.others...)

	var opts []WriteOption
	if ex.isLastStage() {
		opts = append(opts, ex.finalOpts...)
	}
	opts = append(opts, s.opts...)

	if ex.isLastStage() {
		if err := Merge(ctx, sources, ex.finalDst, opts...); err != nil {
			return err
		}
		ex.dstWritten = true
		ex.recordStage(s.name(), true, 0)
		return nil
	}
	f, err := ex.newTempFile()
	if err != nil {
		return err
	}
	if err := Merge(ctx, sources, f, opts...); err != nil {
		return err
	}
	if err := ex.reopenAsCurrent(f); err != nil {
		return err
	}
	var size int64
	if stat, statErr := f.Stat(); statErr == nil {
		size = stat.Size()
	}
	ex.recordStage(s.name(), true, size)
	return nil
}

// setMetadataStage queues a metadata update. See [Pipeline.SetMetadata].
type setMetadataStage struct {
	meta Metadata
}

func (s *setMetadataStage) kind() stageKind { return kindSetMetadata }
func (s *setMetadataStage) name() string    { return stageNameSetMetadata }
func (s *setMetadataStage) apply(_ context.Context, ex *executor) error {
	ex.ensureWriter()
	ex.writer.SetMetadata(s.meta)
	ex.recordStage(s.name(), false, 0)
	return nil
}

// rotatePagesStage queues a page rotation mutation. See [Pipeline.RotatePages].
type rotatePagesStage struct {
	rotation int
	pages    []int
}

func (s *rotatePagesStage) kind() stageKind { return kindRotatePages }
func (s *rotatePagesStage) name() string    { return stageNameRotatePages }
func (s *rotatePagesStage) apply(_ context.Context, ex *executor) error {
	ex.ensureWriter()
	if err := ex.writer.RotatePages(s.rotation, s.pages...); err != nil {
		return err
	}
	ex.recordStage(s.name(), false, 0)
	return nil
}

// setBookmarksStage queues a direct bookmark replacement. See
// [Pipeline.SetBookmarks].
type setBookmarksStage struct {
	bms []models.Bookmark
}

func (s *setBookmarksStage) kind() stageKind { return kindSetBookmarks }
func (s *setBookmarksStage) name() string    { return stageNameSetBookmarks }
func (s *setBookmarksStage) apply(_ context.Context, ex *executor) error {
	ex.ensureWriter()
	ex.writer.SetBookmarks(s.bms)
	ex.recordStage(s.name(), false, 0)
	return nil
}

// generateBookmarksStage queues auto-generation of the outline. See
// [Pipeline.GenerateBookmarks].
type generateBookmarksStage struct {
	opts BookmarkGenerationOptions
	mode BookmarkMergeMode
}

func (s *generateBookmarksStage) kind() stageKind { return kindGenerateBookmarks }
func (s *generateBookmarksStage) name() string    { return stageNameGenerateBookmarks }
func (s *generateBookmarksStage) apply(_ context.Context, ex *executor) error {
	ex.ensureWriter()
	ex.writer.GenerateBookmarks(s.opts, s.mode)
	ex.recordStage(s.name(), false, 0)
	return nil
}

// stampTextStage queues a text overlay. See [Pipeline.StampText].
type stampTextStage struct {
	stamp TextStamp
	pages []int
}

func (s *stampTextStage) kind() stageKind { return kindStampText }
func (s *stampTextStage) name() string    { return stageNameStampText }
func (s *stampTextStage) apply(_ context.Context, ex *executor) error {
	ex.ensureWriter()
	ex.writer.StampText(s.stamp, s.pages...)
	ex.recordStage(s.name(), false, 0)
	return nil
}

// stampImageStage queues an image overlay. See [Pipeline.StampImage].
type stampImageStage struct {
	stamp ImageStamp
	pages []int
}

func (s *stampImageStage) kind() stageKind { return kindStampImage }
func (s *stampImageStage) name() string    { return stageNameStampImage }
func (s *stampImageStage) apply(_ context.Context, ex *executor) error {
	ex.ensureWriter()
	if err := ex.writer.StampImage(s.stamp, s.pages...); err != nil {
		return err
	}
	ex.recordStage(s.name(), false, 0)
	return nil
}

// stampPDFStage queues a PDF-page overlay. See [Pipeline.StampPDF].
type stampPDFStage struct {
	stamp PDFStamp
	pages []int
}

func (s *stampPDFStage) kind() stageKind { return kindStampPDF }
func (s *stampPDFStage) name() string    { return stageNameStampPDF }
func (s *stampPDFStage) apply(_ context.Context, ex *executor) error {
	ex.ensureWriter()
	if err := ex.writer.StampPDF(s.stamp, s.pages...); err != nil {
		return err
	}
	ex.recordStage(s.name(), false, 0)
	return nil
}

// addAttachmentStage queues an embedded file addition. See
// [Pipeline.AddAttachment].
type addAttachmentStage struct {
	fileName string
	data     []byte
	mimeType string
}

func (s *addAttachmentStage) kind() stageKind { return kindAddAttachment }
func (s *addAttachmentStage) name() string    { return stageNameAddAttachment }
func (s *addAttachmentStage) apply(_ context.Context, ex *executor) error {
	ex.ensureWriter()
	ex.writer.AddAttachment(s.fileName, s.data, s.mimeType)
	ex.recordStage(s.name(), false, 0)
	return nil
}

// setFormFieldStage queues a form field value update. See
// [Pipeline.SetFormField].
type setFormFieldStage struct {
	fieldName string
	value     string
}

func (s *setFormFieldStage) kind() stageKind { return kindSetFormField }
func (s *setFormFieldStage) name() string    { return stageNameSetFormField }
func (s *setFormFieldStage) apply(_ context.Context, ex *executor) error {
	ex.ensureWriter()
	ex.writer.SetFormField(s.fieldName, s.value)
	ex.recordStage(s.name(), false, 0)
	return nil
}

// flattenFormStage queues a form flattening operation. See
// [Pipeline.FlattenForm].
type flattenFormStage struct{}

func (s *flattenFormStage) kind() stageKind { return kindFlattenForm }
func (s *flattenFormStage) name() string    { return stageNameFlattenForm }
func (s *flattenFormStage) apply(_ context.Context, ex *executor) error {
	ex.ensureWriter()
	ex.writer.FlattenForm()
	ex.recordStage(s.name(), false, 0)
	return nil
}

// encryptStage queues an encryption operation. See [Pipeline.Encrypt].
type encryptStage struct {
	cfg EncryptConfig
}

func (s *encryptStage) kind() stageKind { return kindEncrypt }
func (s *encryptStage) name() string    { return stageNameEncrypt }
func (s *encryptStage) apply(_ context.Context, ex *executor) error {
	ex.ensureWriter()
	ex.writer.Encrypt(s.cfg)
	ex.recordStage(s.name(), false, 0)
	return nil
}

// decryptStage queues a decryption operation. See [Pipeline.Decrypt].
type decryptStage struct{}

func (s *decryptStage) kind() stageKind { return kindDecrypt }
func (s *decryptStage) name() string    { return stageNameDecrypt }
func (s *decryptStage) apply(_ context.Context, ex *executor) error {
	ex.ensureWriter()
	ex.writer.Decrypt()
	ex.recordStage(s.name(), false, 0)
	return nil
}

// signStage queues a digital signature. Must be the terminal stage; rejected
// at execution time if the current writer has forced a full rewrite. See
// [Pipeline.Sign].
type signStage struct {
	signer Signer
}

func (s *signStage) kind() stageKind { return kindSign }
func (s *signStage) name() string    { return stageNameSign }
func (s *signStage) apply(_ context.Context, ex *executor) error {
	ex.ensureWriter()
	if ex.writer.fullRewriteForced() {
		return ErrPipelineSignWithFullRewrite
	}
	ex.writer.Sign(s.signer)
	ex.recordStage(s.name(), false, 0)
	return nil
}

// remediatePDFAStage runs PDF/A remediation at execution time. See
// [Pipeline.RemediatePDFA].
type remediatePDFAStage struct {
	profile models.PDFAProfile
	opts    []RemediateOption
}

func (s *remediatePDFAStage) kind() stageKind { return kindRemediatePDFA }
func (s *remediatePDFAStage) name() string    { return stageNameRemediatePDFA }
func (s *remediatePDFAStage) apply(ctx context.Context, ex *executor) error {
	ex.ensureWriter()
	report, err := ex.writer.RemediatePDFA(ctx, s.profile, s.opts...)
	if err != nil {
		return err
	}
	ex.result.RemediationReports = append(ex.result.RemediationReports, report)
	ex.recordStage(s.name(), false, 0)
	return nil
}

// remediatePDFUAStage runs PDF/UA remediation at execution time. See
// [Pipeline.RemediatePDFUA].
type remediatePDFUAStage struct {
	profile models.PDFUAProfile
	opts    []RemediateOption
}

func (s *remediatePDFUAStage) kind() stageKind { return kindRemediatePDFUA }
func (s *remediatePDFUAStage) name() string    { return stageNameRemediatePDFUA }
func (s *remediatePDFUAStage) apply(ctx context.Context, ex *executor) error {
	ex.ensureWriter()
	report, err := ex.writer.RemediatePDFUA(ctx, s.profile, s.opts...)
	if err != nil {
		return err
	}
	ex.result.RemediationReports = append(ex.result.RemediationReports, report)
	ex.recordStage(s.name(), false, 0)
	return nil
}
