package pdf

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/gotenberg/litho/pkg/internal/crypto"
	"github.com/gotenberg/litho/pkg/models"
)

// stageKind identifies the category of a pipeline stage. Used by the
// validation engine to apply the compatibility matrix.
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
	kindAddAttachment
	kindSetFormField
	kindFlattenForm
	kindEncrypt
	kindDecrypt
	kindSign
	kindRemediatePDFA
	kindRemediatePDFUA
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
	// pure-transform stage and reset to nil after every flushToTempFile.
	writer *Writer

	// tmpFiles are the intermediate PDF temp files created by flushes and
	// restructuring stages. Retained so the reopened readers stay valid for
	// the lifetime of the run. Cleaned up by cleanup() regardless of outcome.
	tmpFiles []*os.File
	tempDir  string

	// finalOpts are the options passed to [Pipeline.WriteTo]. Applied only
	// at the final stage write (either a pure-transform terminal flush or a
	// restructuring stage that happens to be last).
	finalOpts []WriteOption
	finalDst  io.Writer

	// dstWritten indicates a stage has already written the final output.
	// Set by restructuring stages that target finalDst directly. When true,
	// the terminal flush at the end of run() is skipped.
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
// Called by every pure-transform stage.
func (ex *executor) ensureWriter() {
	if ex.writer == nil {
		ex.writer = NewWriter(ex.cur)
	}
}

// flushToTempFile materializes any pending mutations to a new temp file
// and reopens it as the current reader. Called immediately before a
// restructuring stage that is not the terminal stage. If there are no
// pending mutations, it is a no-op.
func (ex *executor) flushToTempFile() error {
	if ex.writer == nil {
		return nil
	}
	f, err := os.CreateTemp(ex.tempDir, "litho-pipeline-*.pdf")
	if err != nil {
		return fmt.Errorf("creating intermediate temp file: %w", err)
	}
	// Track the file even on error so cleanup() removes it.
	ex.tmpFiles = append(ex.tmpFiles, f)
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
		_ = ex.cur.Close()
	}
	ex.cur = next
	ex.curOwned = true
	ex.writer = nil
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
		_ = ex.cur.Close()
	}
	for _, f := range ex.tmpFiles {
		name := f.Name()
		_ = f.Close()
		_ = os.Remove(name)
	}
}

// recordStage appends a StageReport to the result.
func (ex *executor) recordStage(name string, materialized bool, bytesOut int64) {
	ex.result.Stages = append(ex.result.Stages, StageReport{
		Index:        ex.stageIdx,
		Name:         name,
		Materialized: materialized,
		BytesOut:     bytesOut,
	})
}

// --- Concrete stages ---

// openStage seeds the pipeline with its initial source. Must be first.
type openStage struct {
	src *Reader
}

func (s *openStage) kind() stageKind { return kindOpen }
func (s *openStage) name() string    { return "open" }
func (s *openStage) apply(_ context.Context, ex *executor) error {
	ex.cur = s.src
	ex.curOwned = false
	ex.writer = nil
	ex.recordStage(s.name(), false, 0)
	return nil
}

// splitStage extracts the given 1-based pages.
type splitStage struct {
	pages []int
	opts  []WriteOption
}

func (s *splitStage) kind() stageKind { return kindSplit }
func (s *splitStage) name() string    { return "split" }
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
	stat, _ := f.Stat()
	var size int64
	if stat != nil {
		size = stat.Size()
	}
	ex.recordStage(s.name(), true, size)
	return nil
}

// mergeStage appends other sources after the current document.
type mergeStage struct {
	others []*Reader
	opts   []WriteOption
}

func (s *mergeStage) kind() stageKind { return kindMerge }
func (s *mergeStage) name() string    { return "merge" }
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
	stat, _ := f.Stat()
	var size int64
	if stat != nil {
		size = stat.Size()
	}
	ex.recordStage(s.name(), true, size)
	return nil
}

// setMetadataStage queues a metadata update.
type setMetadataStage struct {
	meta Metadata
}

func (s *setMetadataStage) kind() stageKind { return kindSetMetadata }
func (s *setMetadataStage) name() string    { return "set-metadata" }
func (s *setMetadataStage) apply(_ context.Context, ex *executor) error {
	ex.ensureWriter()
	ex.writer.SetMetadata(s.meta)
	ex.recordStage(s.name(), false, 0)
	return nil
}

// rotatePagesStage queues a page rotation mutation.
type rotatePagesStage struct {
	rotation int
	pages    []int
}

func (s *rotatePagesStage) kind() stageKind { return kindRotatePages }
func (s *rotatePagesStage) name() string    { return "rotate-pages" }
func (s *rotatePagesStage) apply(_ context.Context, ex *executor) error {
	ex.ensureWriter()
	if err := ex.writer.RotatePages(s.rotation, s.pages...); err != nil {
		return err
	}
	ex.recordStage(s.name(), false, 0)
	return nil
}

// setBookmarksStage queues a direct bookmark replacement.
type setBookmarksStage struct {
	bms []models.Bookmark
}

func (s *setBookmarksStage) kind() stageKind { return kindSetBookmarks }
func (s *setBookmarksStage) name() string    { return "set-bookmarks" }
func (s *setBookmarksStage) apply(_ context.Context, ex *executor) error {
	ex.ensureWriter()
	ex.writer.SetBookmarks(s.bms)
	ex.recordStage(s.name(), false, 0)
	return nil
}

// generateBookmarksStage queues auto-generation of the outline.
type generateBookmarksStage struct {
	opts BookmarkGenerationOptions
	mode BookmarkMergeMode
}

func (s *generateBookmarksStage) kind() stageKind { return kindGenerateBookmarks }
func (s *generateBookmarksStage) name() string    { return "generate-bookmarks" }
func (s *generateBookmarksStage) apply(_ context.Context, ex *executor) error {
	ex.ensureWriter()
	ex.writer.GenerateBookmarks(s.opts, s.mode)
	ex.recordStage(s.name(), false, 0)
	return nil
}

// stampTextStage queues a text overlay.
type stampTextStage struct {
	stamp TextStamp
	pages []int
}

func (s *stampTextStage) kind() stageKind { return kindStampText }
func (s *stampTextStage) name() string    { return "stamp-text" }
func (s *stampTextStage) apply(_ context.Context, ex *executor) error {
	ex.ensureWriter()
	ex.writer.StampText(s.stamp, s.pages...)
	ex.recordStage(s.name(), false, 0)
	return nil
}

// addAttachmentStage queues an embedded file addition.
type addAttachmentStage struct {
	fileName string
	data     []byte
	mimeType string
}

func (s *addAttachmentStage) kind() stageKind { return kindAddAttachment }
func (s *addAttachmentStage) name() string    { return "add-attachment" }
func (s *addAttachmentStage) apply(_ context.Context, ex *executor) error {
	ex.ensureWriter()
	ex.writer.AddAttachment(s.fileName, s.data, s.mimeType)
	ex.recordStage(s.name(), false, 0)
	return nil
}

// setFormFieldStage queues a form field value update.
type setFormFieldStage struct {
	fieldName string
	value     string
}

func (s *setFormFieldStage) kind() stageKind { return kindSetFormField }
func (s *setFormFieldStage) name() string    { return "set-form-field" }
func (s *setFormFieldStage) apply(_ context.Context, ex *executor) error {
	ex.ensureWriter()
	ex.writer.SetFormField(s.fieldName, s.value)
	ex.recordStage(s.name(), false, 0)
	return nil
}

// flattenFormStage queues a form flattening operation.
type flattenFormStage struct{}

func (s *flattenFormStage) kind() stageKind { return kindFlattenForm }
func (s *flattenFormStage) name() string    { return "flatten-form" }
func (s *flattenFormStage) apply(_ context.Context, ex *executor) error {
	ex.ensureWriter()
	ex.writer.FlattenForm()
	ex.recordStage(s.name(), false, 0)
	return nil
}

// encryptStage queues an encryption operation.
type encryptStage struct {
	cfg crypto.EncryptConfig
}

func (s *encryptStage) kind() stageKind { return kindEncrypt }
func (s *encryptStage) name() string    { return "encrypt" }
func (s *encryptStage) apply(_ context.Context, ex *executor) error {
	ex.ensureWriter()
	ex.writer.Encrypt(s.cfg)
	ex.recordStage(s.name(), false, 0)
	return nil
}

// decryptStage queues a decryption operation.
type decryptStage struct{}

func (s *decryptStage) kind() stageKind { return kindDecrypt }
func (s *decryptStage) name() string    { return "decrypt" }
func (s *decryptStage) apply(_ context.Context, ex *executor) error {
	ex.ensureWriter()
	ex.writer.Decrypt()
	ex.recordStage(s.name(), false, 0)
	return nil
}

// signStage queues a digital signature. Must be the terminal stage; rejected
// at execution time if the current writer has forced a full rewrite.
type signStage struct {
	signer Signer
}

func (s *signStage) kind() stageKind { return kindSign }
func (s *signStage) name() string    { return "sign" }
func (s *signStage) apply(_ context.Context, ex *executor) error {
	ex.ensureWriter()
	if ex.writer.fullRewriteForced() {
		return ErrPipelineSignWithFullRewrite
	}
	ex.writer.Sign(s.signer)
	ex.recordStage(s.name(), false, 0)
	return nil
}

// remediatePDFAStage runs PDF/A remediation at execution time.
type remediatePDFAStage struct {
	profile models.PDFAProfile
	opts    []RemediateOption
}

func (s *remediatePDFAStage) kind() stageKind { return kindRemediatePDFA }
func (s *remediatePDFAStage) name() string    { return "remediate-pdfa" }
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

// remediatePDFUAStage runs PDF/UA remediation at execution time.
type remediatePDFUAStage struct {
	profile models.PDFUAProfile
	opts    []RemediateOption
}

func (s *remediatePDFUAStage) kind() stageKind { return kindRemediatePDFUA }
func (s *remediatePDFUAStage) name() string    { return "remediate-pdfua" }
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
