package pdf

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
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
// closure contents; incompatible-operation checks (e.g., Sign +
// encryption) are enforced by [Writer.WriteTo] at execution time.
//
// Every intermediate file is written in the clear, and the encryption a stage
// asks for, [Writer.Encrypt] in an Apply stage or [WithEncryption] on a Split
// or Merge, goes on the pipeline's output instead. The [Writer] of each later
// Apply stage starts with it, as if the stage had called [Writer.Encrypt]
// first, so a later [Writer.Sign] or PDF/A remediation is refused as it is on
// one Writer. A later request replaces it: [Writer.Encrypt] in a later stage,
// or [WithEncryption] on a later Split or Merge or on [Pipeline.WriteTo].
//
// A Split or Merge stage reopens its output in the clear, and that document
// still counts as encrypted when a [Reader] it was made from was: the source,
// a Reader merged into it, or an earlier stage's output made from one. A
// [Writer.Sign] after it fails with [ErrSignFullRewriteIncompatible], as it
// does on the encrypted source itself. To sign the result, write the
// pipeline, reopen its output and sign that.
//
// A remediation is the last edit, as on any [Writer]: an Apply stage after
// the one that remediates may only call [Writer.Sign], and a Split or Merge
// stage after it is refused with [ErrEditAfterRemediation] before it runs.
// Nothing is written then.
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
	// DroppedSignatures lists the signatures of the pipeline's documents that
	// its output does not carry, each once, in the order the stages dropped
	// them: a Split or a Merge drops every signature of the documents it
	// reads, and an Apply stage those its Writer's full rewrite drops (see
	// [Writer.DroppedSignatures]).
	DroppedSignatures []DroppedSignature
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
// source is required. Honors [WithBookmarks] on the stage itself. An
// encrypted Reader among others makes the merged document one a later
// [Writer.Sign] refuses (see [Pipeline]).
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
// page lists, and empty merge sources. Cross-stage compatibility (Sign
// combined with encryption, a stage after a remediation)
// depends on what the Apply closures call, and is enforced at execution time:
// it does not appear here.
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
//
// The options reach the final write as they reach [Writer.WriteTo], which
// refuses [WithEncryption] after a PDF/A remediation with
// [ErrPDFAForbidsEncryption], and after a PDF/UA-1 remediation, when its
// permissions bar extraction for accessibility, with
// [ErrPDFUAForbidsBarringAccessibility]. A stage after a remediation fails
// with [ErrEditAfterRemediation] (see [Pipeline]).
//
// [WithObjectStreams], [WithDedup], [WithImageRecompression] and
// [WithVersion] reach the output after a Split or a Merge too: when a later stage edits the file a
// Split or a Merge wrote, the output is a full rewrite of it, and a signature
// goes on top of that rewrite. On the source itself they apply only when the
// write rewrites it in full, as on [Writer.WriteTo].
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
	if cfg.pageSizeErr != nil {
		return nil, cfg.pageSizeErr
	}

	ex := &executor{
		tempDir:     cfg.tempDir,
		pageSize:    cfg.pageSize,
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
		ex.recordDropped(ex.writer.DroppedSignatures()...)
	}
	if cfg.droppedSignatures != nil {
		*cfg.droppedSignatures = slices.Clone(ex.result.DroppedSignatures)
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
	curFile  *os.File
	writer   *Writer
	tmpFiles []*os.File
	tempDir  string
	// pageSize is the default page size the options of [Pipeline.WriteTo]
	// name (see [WithDefaultPageSize]), nil for none. Every stage writes
	// with it, not only the last: an intermediate file that gave a page a
	// media box gave it for good.
	pageSize    ast.PdfArray
	finalOpts   []WriteOption
	finalDst    io.Writer
	dstWritten  bool
	stageIdx    int
	totalStages int
	logger      *slog.Logger
	result      *PipelineResult
	// encrypt is the encryption an earlier stage asked for, which the
	// pipeline's output owes the caller although every intermediate file is
	// written in the clear (see [executor.carryEncryption]). nil when none did.
	encrypt *EncryptConfig
}

func (ex *executor) isLastStage() bool { return ex.stageIdx == ex.totalStages-1 }

// ensureWriter binds a Writer to the current document, placing its temp
// files where the pipeline places its own. The Writer is asked for the
// encryption an earlier stage asked for, as if the stage that binds it had
// called [Writer.Encrypt] first (see [executor.carryEncryption]). A Writer
// over an intermediate file knows it, so the compaction and the version the
// output asks for rewrite that file rather than update it (see
// [Writer.rewritesForOptions]).
func (ex *executor) ensureWriter() {
	if ex.writer == nil {
		ex.writer = NewWriter(ex.cur)
		ex.writer.tempDir = ex.tempDir
		ex.writer.intermediate = ex.curFile != nil
		ex.writer.defaults.pageSize = ex.pageSize
		if ex.encrypt != nil {
			ex.writer.Encrypt(*ex.encrypt)
		}
	}
}

// carryEncryption records cfg, the encryption a stage asked for its output,
// as the one the pipeline's output owes the caller, and returns the option
// that writes the stage's output, an intermediate file, in the clear.
//
// An intermediate file is reopened as the next stage's document, and the
// pipeline holds no password to reopen an encrypted one with. Written under
// the encryption an Apply stage asked for, it failed to reopen as soon as that
// encryption set a user password, and under an owner password alone it
// reopened, and the Split or Merge after it wrote the output in the clear.
// The caller asked for an encrypted output either way, so the encryption waits
// for the write that produces it: [executor.ensureWriter] asks each later
// Writer for it, and a Split or Merge that is the last stage writes with it.
// A later request replaces it, as [WithEncryption] on [Writer.WriteTo]
// replaces [Writer.Encrypt]. The remediation writes its temp files in the
// clear for the same reason (see [Writer.writeTempFile]).
func (ex *executor) carryEncryption(cfg *EncryptConfig) WriteOption {
	if cfg != nil {
		carried := *cfg
		ex.encrypt = &carried
	}
	return inTheClear()
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

// flushToTempFile writes the current Writer to an intermediate file and makes
// that file the current document, before stage, a split or a merge, restructures
// it. A remediated Writer is refused: restructuring its output would leave the
// remediation's report describing a document that is not the one written.
func (ex *executor) flushToTempFile(ctx context.Context, stage string) error {
	if ex.writer == nil {
		return nil
	}
	if ex.writer.remediation != "" {
		return fmt.Errorf("%w: a %s stage after Writer.%s", ErrEditAfterRemediation, stage, ex.writer.remediation)
	}
	f, err := os.CreateTemp(ex.tempDir, "litho-pipeline-*.pdf")
	if err != nil {
		return fmt.Errorf("creating intermediate temp file: %w", err)
	}
	ex.tmpFiles = append(ex.tmpFiles, f)
	var encrypt *EncryptConfig
	if ex.writer.encryptCfg != nil {
		cfg := encryptConfigFromInternal(*ex.writer.encryptCfg)
		encrypt = &cfg
	}
	if err := ex.writer.WriteTo(ctx, f, ex.carryEncryption(encrypt)); err != nil {
		return fmt.Errorf("flushing pending mutations: %w", err)
	}
	ex.recordDropped(ex.writer.DroppedSignatures()...)
	return ex.reopenAsCurrent(ctx, f, []*Reader{ex.cur})
}

// recordDropped adds to the result the signatures a stage's output does not
// carry, each once.
func (ex *executor) recordDropped(sigs ...DroppedSignature) {
	ex.result.DroppedSignatures = appendDroppedSignatures(ex.result.DroppedSignatures, sigs...)
}

// recordRestructured records the signatures of sources, the documents a Split
// or a Merge read, which the new file it builds drops.
func (ex *executor) recordRestructured(ctx context.Context, sources ...*Reader) {
	for _, src := range sources {
		ex.recordDropped(droppedSignaturesOf(ctx, src.doc)...)
	}
}

// restructureOptions returns the options a Split or a Merge stage writes with,
// given the ones the stage carries. The last stage writes the pipeline's
// output: the encryption an earlier stage asked for, then the options of
// [Pipeline.WriteTo], then the stage's own, so a later request wins. Any other
// writes an intermediate file, in the clear, and an encryption among its
// options is carried to the output (see [executor.carryEncryption]).
func (ex *executor) restructureOptions(stageOpts []WriteOption) []WriteOption {
	var opts []WriteOption
	if ex.isLastStage() {
		if ex.encrypt != nil {
			opts = append(opts, WithEncryption(*ex.encrypt))
		}
		opts = append(opts, ex.finalOpts...)
		return append(opts, stageOpts...)
	}
	cfg := writeOpts{}
	for _, o := range stageOpts {
		o(&cfg)
	}
	if ex.pageSize != nil {
		pageSize := ex.pageSize
		opts = append(opts, func(o *writeOpts) { o.pageSize = pageSize })
	}
	opts = append(opts, stageOpts...)
	return append(opts, ex.carryEncryption(cfg.encrypt))
}

// reopenAsCurrent makes f, an intermediate file the pipeline wrote from the
// documents of from, the current document.
//
// The Reader over f counts as encrypted when a document it was written from
// was (see [Reader.derivesFromEncrypted]), although f is written in the clear
// (see [executor.carryEncryption]): [Writer.WriteTo] refuses to sign an
// encrypted document, and a signature asked for after a Split or a Merge of
// one would otherwise go on a decrypted copy the caller never asked for, where
// the same Writer on the source refuses it.
// A Merge brings the pages of every Reader it is given into f, so each of them
// counts.
//
// # Corpus evidence
//
//   - All corpora: litho opens 86 encrypted files. A pipeline signing after a
//     Split of one signed 84 of them, and one signing after a Merge of one
//     into a plaintext document signed 84. Both refuse all 84 now.
//   - pdfjs issue19484_1.pdf, issue19484_2.pdf: neither splits nor merges, on
//     an object stream that does not inflate.
func (ex *executor) reopenAsCurrent(ctx context.Context, f *os.File, from []*Reader) error {
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
	next.encryptedOrigin = slices.ContainsFunc(from, (*Reader).derivesFromEncrypted)
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
	if err := ex.flushToTempFile(ctx, s.name()); err != nil {
		return err
	}
	opts := ex.restructureOptions(s.opts)

	if ex.isLastStage() {
		if err := Split(ctx, ex.cur, s.pages, ex.finalDst, opts...); err != nil {
			return err
		}
		ex.recordRestructured(ctx, ex.cur)
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
	ex.recordRestructured(ctx, ex.cur)
	if err := ex.reopenAsCurrent(ctx, f, []*Reader{ex.cur}); err != nil {
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
	if err := ex.flushToTempFile(ctx, s.name()); err != nil {
		return err
	}
	sources := make([]*Reader, 0, len(s.others)+1)
	sources = append(sources, ex.cur)
	sources = append(sources, s.others...)

	opts := ex.restructureOptions(s.opts)

	if ex.isLastStage() {
		if err := Merge(ctx, sources, ex.finalDst, opts...); err != nil {
			return err
		}
		ex.recordRestructured(ctx, sources...)
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
	ex.recordRestructured(ctx, sources...)
	if err := ex.reopenAsCurrent(ctx, f, sources); err != nil {
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
