package pdf

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
)

// Pipeline chains write operations against a single PDF state that
// evolves from stage to stage. [Pipeline.Open] seeds the state,
// [Pipeline.Apply] queues mutations through a [Writer] closure,
// [Pipeline.Split] and [Pipeline.Merge] restructure the document, and
// [Pipeline.WriteTo] executes the pipeline.
//
// Restructuring stages materialize the current state to an intermediate
// temp file and reopen it as a new [Reader]. A temp file is removed as soon
// as the next one replaces it as the current state, and every one left, the
// output of a remediation no stage wrote included, is removed before
// [Pipeline.WriteTo] returns, on success and failure alike.
//
// [Pipeline.Apply] receives a [Writer] bound to the current document.
// The closure can call any [Writer] method, including
// [Writer.RemediatePDFA]. Results are captured by the caller through
// variables in the enclosing scope. The pipeline does not inspect
// closure contents; incompatible-operation checks (e.g., Sign + an
// edit forcing a full rewrite) are enforced by [Writer.WriteTo] at
// execution time.
// Pipeline values must not be modified after being passed to [Pipeline.WriteTo].
//
// A Pipeline is not safe for concurrent use. Every stage method appends to the
// same slice, so building one from several goroutines races; declare the
// pipeline on one goroutine and hand the finished value to [Pipeline.WriteTo].
// [PipelineResult] carries the same requirement.
type Pipeline struct {
	stages   []stage
	firstErr error
}

// PipelineResult holds per-stage execution info returned from
// [Pipeline.WriteTo].
// Values must not be shared across goroutines without synchronization.
type PipelineResult struct {
	// Stages reports one entry per executed stage in declaration order.
	Stages []StageReport
}

// StageReport summarizes a single pipeline stage.
// Values must not be shared across goroutines without synchronization.
type StageReport struct {
	// Index is the 0-based position of the stage in declaration order.
	Index int
	// Name is the short stage identifier: "open", "split", "merge",
	// "apply", or "write".
	Name string
	// Materialized is true if the stage flushed the pipeline state to
	// an intermediate temp file or to the final destination.
	Materialized bool
	// BytesOut is the size of the intermediate file for restructuring
	// stages, or 0 otherwise.
	BytesOut int64
}

// NewPipeline creates an empty [Pipeline]. Seed it with [Pipeline.Open].
func NewPipeline() *Pipeline { return &Pipeline{} }

// Err returns the first build-time validation error, or nil.
func (p *Pipeline) Err() error { return p.firstErr }

func (p *Pipeline) add(s stage) *Pipeline {
	p.stages = append(p.stages, s)
	return p
}

// Open sets the initial source document. Must be the first stage.
func (p *Pipeline) Open(src *Reader) *Pipeline {
	return p.add(&openStage{src: src})
}

// Split keeps the given 1-based pages as the new current document.
// Honors [WithBookmarks] on the stage itself.
func (p *Pipeline) Split(pages []int, opts ...WriteOption) *Pipeline {
	cp := append([]int(nil), pages...)
	return p.add(&splitStage{pages: cp, opts: opts})
}

// Merge appends others to the current document. At least one other
// source is required. Honors [WithBookmarks] on the stage itself.
func (p *Pipeline) Merge(others []*Reader, opts ...WriteOption) *Pipeline {
	cp := append([]*Reader(nil), others...)
	return p.add(&mergeStage{others: cp, opts: opts})
}

// Apply queues a mutation closure that runs against the current
// [Writer]. The closure can call any Writer method (SetMetadata,
// Encrypt, RemediatePDFA, Sign, etc.). Errors returned from the
// closure surface as [PipelineStageError] from [Pipeline.WriteTo].
//
// The [context.Context] passed to the closure is the one the caller
// provided to [Pipeline.WriteTo].
func (p *Pipeline) Apply(fn func(ctx context.Context, w *Writer) error) *Pipeline {
	return p.add(&applyStage{fn: fn})
}

// Validate runs build-time structural checks without executing the
// pipeline. It catches empty pipelines, missing [Pipeline.Open], empty
// page lists, and empty merge sources. Cross-stage compatibility
// (Sign combined with an edit forcing a full rewrite) is enforced by
// [Writer.WriteTo] at execution time and does not appear here.
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

func (p *Pipeline) buildValidationErrors() []error {
	var errs []error
	if len(p.stages) == 0 {
		return append(errs, &PipelineStageError{
			Index: 0, Name: stageNamePipeline, Err: ErrPipelineEmpty,
		})
	}

	hasSource := false
	for i, s := range p.stages {
		if s.kind() == kindOpen {
			if i != 0 {
				errs = append(errs, &PipelineStageError{
					Index: i, Name: s.name(), Err: ErrPipelineNoSource,
				})
			}
			hasSource = true
		} else if !hasSource {
			errs = append(errs, &PipelineStageError{
				Index: i, Name: s.name(), Err: ErrPipelineNoSource,
			})
		}

		switch st := s.(type) {
		case *splitStage:
			if len(st.pages) == 0 {
				errs = append(errs, &PipelineStageError{
					Index: i, Name: s.name(), Err: ErrPipelineEmptyPageList,
				})
			}
		case *mergeStage:
			if len(st.others) == 0 {
				errs = append(errs, &PipelineStageError{
					Index: i, Name: s.name(), Err: ErrPipelineNothingToMerge,
				})
			}
		}
	}
	return errs
}

// WriteTo executes the pipeline and writes the final document to dst.
// Pass [WithWriteLogger] for structured debug logs; pass [WithTempDir] or
// [WithPipelineTempDir] to redirect intermediate temp files.
func (p *Pipeline) WriteTo(ctx context.Context, dst io.Writer, opts ...WriteOption) (_ *PipelineResult, err error) {
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
	defer recoverToError(logger, "Pipeline.WriteTo", &err)

	ex := &executor{
		tempDir:     cfg.tempDir,
		finalOpts:   opts,
		finalDst:    dst,
		totalStages: len(p.stages),
		logger:      logger,
		result:      &PipelineResult{},
	}
	defer ex.cleanup()

	logger.Debug("pipeline started",
		slog.Int("stages", len(p.stages)),
	)

	for i, s := range p.stages {
		if err := ctx.Err(); err != nil {
			return nil, &PipelineStageError{Index: i, Name: s.name(), Err: err}
		}
		ex.stageIdx = i
		if err := s.apply(ctx, ex); err != nil {
			logger.Debug("stage failed",
				slog.Int("stage", i),
				slog.String("name", s.name()),
				slog.Any("err", err),
			)
			return nil, &PipelineStageError{Index: i, Name: s.name(), Err: err}
		}
	}

	// Terminal flush.
	if !ex.dstWritten {
		ex.ensureWriter()
		if err := ex.writer.WriteTo(ctx, dst, opts...); err != nil {
			return nil, &PipelineStageError{
				Index: len(p.stages) - 1, Name: stageNameWrite, Err: err,
			}
		}
	}

	logger.Debug("pipeline completed",
		slog.Int("stages_executed", len(ex.result.Stages)),
	)
	return ex.result, nil
}

const (
	stageNamePipeline = "pipeline"
	stageNameWrite    = "write"
	stageNameOpen     = "open"
	stageNameSplit    = "split"
	stageNameMerge    = "merge"
	stageNameApply    = "apply"
)

type stageKind int

const (
	kindOpen stageKind = iota
	kindSplit
	kindMerge
	kindApply
)

type stage interface {
	kind() stageKind
	name() string
	apply(ctx context.Context, ex *executor) error
}

// executor drives the sequential execution of pipeline stages.
type executor struct {
	cur      *Reader
	curOwned bool
	// curFile is the intermediate temp file cur reads, nil while cur is the
	// caller's source.
	curFile     *os.File
	writer      *Writer
	tmpFiles    []*os.File
	tempDir     string
	finalOpts   []WriteOption
	finalDst    io.Writer
	dstWritten  bool
	stageIdx    int
	totalStages int
	logger      *slog.Logger
	result      *PipelineResult
}

func (ex *executor) isLastStage() bool { return ex.stageIdx == ex.totalStages-1 }

// ensureWriter binds a Writer to the current document, placing its temp
// files where the pipeline places its own.
func (ex *executor) ensureWriter() {
	if ex.writer == nil {
		ex.writer = NewWriter(ex.cur)
		ex.writer.tempDir = ex.tempDir
	}
}

// releaseWriter closes the current Writer, removing the remediation output
// it holds when no stage wrote it, and forgets it.
func (ex *executor) releaseWriter() {
	if ex.writer == nil {
		return
	}
	if err := ex.writer.Close(); err != nil {
		ex.logger.Debug("closing writer failed", slog.Any("err", err))
	}
	ex.writer = nil
}

func (ex *executor) flushToTempFile(ctx context.Context) error {
	if ex.writer == nil {
		return nil
	}
	f, err := os.CreateTemp(ex.tempDir, "litho-pipeline-*.pdf")
	if err != nil {
		return fmt.Errorf("creating intermediate temp file: %w", err)
	}
	ex.tmpFiles = append(ex.tmpFiles, f)
	if err := ex.writer.WriteTo(ctx, f); err != nil {
		return fmt.Errorf("flushing pending mutations: %w", err)
	}
	return ex.reopenAsCurrent(ctx, f)
}

func (ex *executor) reopenAsCurrent(ctx context.Context, f *os.File) error {
	stat, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat intermediate file: %w", err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("seeking intermediate file: %w", err)
	}
	next, err := Open(ctx, f, stat.Size(), ex.cur.reopenOptions()...)
	if err != nil {
		return fmt.Errorf("reopening intermediate file: %w", err)
	}
	next.adoptConfiguration(ex.cur)
	if ex.curOwned && ex.cur != nil {
		if closeErr := ex.cur.Close(); closeErr != nil {
			ex.logger.Debug("closing previous reader failed", slog.Any("err", closeErr))
		}
	}
	// Nothing reads the previous intermediate file any more: its Reader is
	// closed and its Writer was written into f.
	if ex.curFile != nil {
		ex.removeTempFile(ex.curFile)
	}
	ex.cur = next
	ex.curOwned = true
	ex.curFile = f
	ex.releaseWriter()
	return nil
}

func (ex *executor) newTempFile() (*os.File, error) {
	f, err := os.CreateTemp(ex.tempDir, "litho-pipeline-*.pdf")
	if err != nil {
		return nil, fmt.Errorf("creating intermediate temp file: %w", err)
	}
	ex.tmpFiles = append(ex.tmpFiles, f)
	return f, nil
}

// removeTempFile closes and removes f, one of the intermediate files, and
// drops it from the ones cleanup removes.
func (ex *executor) removeTempFile(f *os.File) {
	name := f.Name()
	if err := f.Close(); err != nil {
		ex.logger.Debug("closing temp file failed", slog.String("path", name), slog.Any("err", err))
	}
	if err := os.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
		ex.logger.Debug("removing temp file failed", slog.String("path", name), slog.Any("err", err))
	}
	ex.tmpFiles = slices.DeleteFunc(ex.tmpFiles, func(g *os.File) bool { return g == f })
}

func (ex *executor) cleanup() {
	// A Writer a failed or cancelled pipeline never wrote may hold the output
	// of a remediation.
	ex.releaseWriter()
	if ex.curOwned && ex.cur != nil {
		if err := ex.cur.Close(); err != nil {
			ex.logger.Debug("closing current reader failed", slog.Any("err", err))
		}
	}
	for len(ex.tmpFiles) > 0 {
		ex.removeTempFile(ex.tmpFiles[0])
	}
}

func (ex *executor) recordStage(name string, materialized bool, bytesOut int64) {
	ex.result.Stages = append(ex.result.Stages, StageReport{
		Index: ex.stageIdx, Name: name, Materialized: materialized, BytesOut: bytesOut,
	})
}

type openStage struct{ src *Reader }

func (s *openStage) kind() stageKind { return kindOpen }
func (s *openStage) name() string    { return stageNameOpen }
func (s *openStage) apply(_ context.Context, ex *executor) error {
	ex.cur = s.src
	ex.curOwned = false
	ex.releaseWriter()
	ex.recordStage(s.name(), false, 0)
	return nil
}

type splitStage struct {
	pages []int
	opts  []WriteOption
}

func (s *splitStage) kind() stageKind { return kindSplit }
func (s *splitStage) name() string    { return stageNameSplit }
func (s *splitStage) apply(ctx context.Context, ex *executor) error {
	if err := ex.flushToTempFile(ctx); err != nil {
		return err
	}
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
	if err := ex.reopenAsCurrent(ctx, f); err != nil {
		return err
	}
	var size int64
	if stat, statErr := f.Stat(); statErr == nil {
		size = stat.Size()
	}
	ex.recordStage(s.name(), true, size)
	return nil
}

type mergeStage struct {
	others []*Reader
	opts   []WriteOption
}

func (s *mergeStage) kind() stageKind { return kindMerge }
func (s *mergeStage) name() string    { return stageNameMerge }
func (s *mergeStage) apply(ctx context.Context, ex *executor) error {
	if err := ex.flushToTempFile(ctx); err != nil {
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
	if err := ex.reopenAsCurrent(ctx, f); err != nil {
		return err
	}
	var size int64
	if stat, statErr := f.Stat(); statErr == nil {
		size = stat.Size()
	}
	ex.recordStage(s.name(), true, size)
	return nil
}

type applyStage struct {
	fn func(ctx context.Context, w *Writer) error
}

func (s *applyStage) kind() stageKind { return kindApply }
func (s *applyStage) name() string    { return stageNameApply }
func (s *applyStage) apply(ctx context.Context, ex *executor) error {
	ex.ensureWriter()
	if err := s.fn(ctx, ex.writer); err != nil {
		return err
	}
	ex.recordStage(s.name(), false, 0)
	return nil
}
