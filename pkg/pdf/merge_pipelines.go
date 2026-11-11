package pdf

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"runtime/debug"
	"slices"
	"sync"

	"github.com/gotenberg/litho/pkg/internal/panicerr"
)

type mergePipelinesStage struct {
	pipelines []*Pipeline
	opts      []WriteOption
}

func (s *mergePipelinesStage) kind() stageKind { return kindMerge }
func (s *mergePipelinesStage) name() string    { return stageNameMergePipelines }

// mergedOutput is what one pipeline a [Pipeline.MergePipelines] stage merges
// left: its output, the Reader over it and its result.
type mergedOutput struct {
	// file holds the output, an intermediate file of the merging pipeline.
	file *os.File
	// reader reads file, nil until the pipeline succeeded.
	reader *Reader
	// result is the pipeline's own result.
	result *PipelineResult
	// encrypt is the encryption the pipeline asked for its output, which the
	// merging pipeline's output owes the caller instead, nil when none did.
	encrypt *EncryptConfig
}

// apply runs the pipelines into intermediate files, then merges them, after
// the current document when the stage is not the first, as a Merge stage
// merges Readers: into the pipeline's output when the stage is the last, into
// an intermediate file the next stage reads otherwise. The encryption a
// pipeline asked for is carried to the merging pipeline's output, in the
// order given, as [executor.carryEncryption] carries a stage's.
func (s *mergePipelinesStage) apply(ctx context.Context, ex *executor) error {
	// A remediated current document is refused here, before any pipeline
	// runs (see [executor.flushToTempFile]).
	if err := ex.flushToTempFile(ctx, s.name()); err != nil {
		return err
	}
	outs := make([]mergedOutput, len(s.pipelines))
	defer ex.releaseMerged(outs)
	for i := range outs {
		f, err := ex.newTempFile()
		if err != nil {
			return err
		}
		outs[i].file = f
	}
	if err := s.runAll(ctx, ex, outs); err != nil {
		return err
	}

	sources := make([]*Reader, 0, len(outs)+1)
	if ex.cur != nil {
		sources = append(sources, ex.cur)
	}
	results := make([]*PipelineResult, len(outs))
	for i, out := range outs {
		sources = append(sources, out.reader)
		results[i] = out.result
		ex.recordDropped(out.result.DroppedSignatures...)
		if out.encrypt != nil {
			ex.carryEncryption(out.encrypt)
		}
	}
	opts := ex.restructureOptions(s.opts)
	report := StageReport{Index: ex.stageIdx, Name: s.name(), Materialized: true, Pipelines: results}

	if ex.isLastStage() {
		if err := Merge(ctx, sources, ex.finalDst, opts...); err != nil {
			return err
		}
		ex.outputFrom = sources
		ex.recordRestructured(ctx, sources...)
		ex.dstWritten = true
		ex.result.Stages = append(ex.result.Stages, report)
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
	if ex.cur == nil {
		// The merge is read with the configuration of the first pipeline's
		// output, as a Merge's output is read with its first source's. The
		// stage, not the executor, owns that Reader.
		ex.cur = sources[0]
		ex.curOwned = false
	}
	if err := ex.reopenAsCurrent(ctx, f, sources); err != nil {
		return err
	}
	if stat, statErr := f.Stat(); statErr == nil {
		report.BytesOut = stat.Size()
	}
	ex.result.Stages = append(ex.result.Stages, report)
	return nil
}

// runAll runs the pipelines, at most ex.concurrency at once, each into the
// file of its output. A bounded pool of workers takes them in order. The
// first failure cancels the others through their context, and no pipeline
// starts after it. Every worker has exited when runAll returns.
//
// # Corpus evidence
//
//   - litho/stress html-1mib: eight conversions of it merged, 2,056 pages,
//     peak RSS 424 MiB at concurrency 1 in 6.5 s, 1.29 GiB at 4 in 3.1 s,
//     2.10 GiB at 8 in 3.3 s, on 10 cores: each running conversion holds
//     about 240 MiB, and past 4 the time no longer falls.
func (s *mergePipelinesStage) runAll(ctx context.Context, ex *executor, outs []mergedOutput) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	errs := make([]error, len(outs))
	next := make(chan int)
	var wg sync.WaitGroup
	for range min(ex.concurrency, len(outs)) {
		wg.Go(func() {
			for i := range next {
				if err := ex.runMerged(runCtx, i, s.pipelines[i], &outs[i]); err != nil {
					errs[i] = err
					cancel()
				}
			}
		})
	}
feed:
	for i := range outs {
		if runCtx.Err() != nil {
			break
		}
		select {
		case next <- i:
		case <-runCtx.Done():
			break feed
		}
	}
	close(next)
	wg.Wait()
	return mergedFailure(ctx, errs, outs)
}

// mergedFailure returns the error of the pipeline that failed at the lowest
// position for a reason of its own, rather than the cancellation the first
// failure sent the others, naming it by its position. When every failure is a
// cancellation, the caller's context was cancelled: it returns the lowest
// one, or the context's error when no pipeline reported one. It returns nil
// when every pipeline wrote its output.
func mergedFailure(ctx context.Context, errs []error, outs []mergedOutput) error {
	first := -1
	for i, err := range errs {
		if err == nil {
			continue
		}
		if !errors.Is(err, context.Canceled) {
			return fmt.Errorf("pipeline %d: %w", i, err)
		}
		if first < 0 {
			first = i
		}
	}
	if first >= 0 {
		return fmt.Errorf("pipeline %d: %w", first, errs[first])
	}
	for i := range outs {
		if outs[i].reader == nil {
			// A pipeline that never started: only the caller's cancellation
			// stops the feed without a failure.
			return fmt.Errorf("pipeline %d: %w", i, context.Cause(ctx))
		}
	}
	return nil
}

// runMerged runs p, the pipeline at position i of a MergePipelines stage, on
// a worker goroutine, into out.file, and opens that file as out.reader. The
// pipeline runs as [Pipeline.WriteTo] runs it, with the logger, the temp
// directory and the default page size of the merging pipeline, and writes its
// output in the clear (see [executor.terminalOptions]). A panic of the
// pipeline is recovered here, on the worker that raised it, into an error
// wrapping [ErrInternalPanic].
func (ex *executor) runMerged(ctx context.Context, i int, p *Pipeline, out *mergedOutput) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = panicerr.Log(ex.logger, &panicerr.Error{
				Where: fmt.Sprintf("merged pipeline %d", i), Value: rec, Stack: debug.Stack(),
			})
		}
	}()
	opts := []WriteOption{WithWriteLogger(ex.logger), WithPipelineTempDir(ex.tempDir)}
	pageSize := slices.Clone(ex.pageSize)
	if pageSize != nil {
		opts = append(opts, func(o *writeOpts) { o.pageSize = pageSize })
	}
	sub := &executor{
		merged:      true,
		concurrency: ex.concurrency,
		tempDir:     ex.tempDir,
		pageSize:    pageSize,
		finalOpts:   opts,
		finalDst:    out.file,
		totalStages: len(p.stages),
		logger:      ex.logger,
		result:      &PipelineResult{},
	}
	defer sub.cleanup()
	if err := sub.run(ctx, p.stages); err != nil {
		return err
	}
	from := sub.outputFrom
	if last, ok := p.stages[len(p.stages)-1].(*composeStage); ok && from == nil {
		from = last.sources()
	}
	if len(from) == 0 {
		return errors.New("the pipeline wrote no document")
	}
	stat, err := out.file.Stat()
	if err != nil {
		return fmt.Errorf("sizing the pipeline's output: %w", err)
	}
	// The output is read with the configuration of the document it was
	// written from, as an intermediate file of the pipeline itself is (see
	// [executor.reopenAsCurrent]).
	r, err := Open(ctx, out.file, stat.Size(), from[0].reopenOptions()...)
	if err != nil {
		return fmt.Errorf("reading the pipeline's output back: %w", err)
	}
	r.adoptConfiguration(from[0])
	r.encryptedOrigin = slices.ContainsFunc(from, (*Reader).derivesFromEncrypted)
	out.reader, out.result, out.encrypt = r, sub.result, sub.encrypt
	return nil
}

// releaseMerged closes the Readers over the outputs of the merged pipelines
// and removes their files: the merge read them, or the stage failed.
func (ex *executor) releaseMerged(outs []mergedOutput) {
	for _, out := range outs {
		if out.reader != nil {
			if err := out.reader.Close(); err != nil {
				ex.logger.Debug("closing a merged pipeline's output failed", slog.Any("err", err))
			}
		}
		if out.file != nil {
			ex.removeTempFile(out.file)
		}
	}
}
