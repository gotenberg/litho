package pdf

import (
	"context"
	"fmt"
	"io"
)

// Convert opens the pipeline on the PDF c writes, as [Conversion.WriteTo]
// writes it. It must be the first stage, in place of [Pipeline.Open]: a
// Convert after another stage fails with [ErrPipelineNoSource]. The stage
// reads c's source once, so a pipeline that converts runs once.
//
// A later stage edits the converted document as it edits an opened one. The
// conversion claims no conformance: an [Pipeline.Apply] stage that calls
// [Writer.RemediatePDFA] or [Writer.RemediatePDFUA] makes the output conform,
// and is the last edit, as on any pipeline.
//
// The conversion logs to the pipeline's logger and keeps its intermediate
// files in the pipeline's temp directory unless its own options name
// others. The document is read back within the limits of
// [WithConvertLimits]. Its report is the stage's
// [StageReport.Conversion].
func (p *Pipeline) Convert(c *Conversion) *Pipeline {
	return p.add(&convertStage{c: c})
}

type convertStage struct {
	c *Conversion
}

func (s *convertStage) kind() stageKind { return kindOpen }
func (s *convertStage) name() string    { return stageNameConvert }

// apply converts into an intermediate file and opens it as the current
// document, which the pipeline owns.
func (s *convertStage) apply(ctx context.Context, ex *executor) error {
	f, err := ex.newTempFile()
	if err != nil {
		return err
	}
	report, err := s.c.run(ctx, f, convertEnv{logger: ex.logger, tempDir: ex.tempDir})
	if err != nil {
		return err
	}
	size, err := f.Seek(0, io.SeekCurrent)
	if err != nil {
		return fmt.Errorf("sizing the converted file: %w", err)
	}
	logger := s.c.cfg.logger
	if logger == nil {
		logger = ex.logger
	}
	r, err := Open(ctx, f, size, WithLogger(logger), WithLimits(s.c.cfg.limits))
	if err != nil {
		return fmt.Errorf("reading the converted file back: %w", err)
	}
	ex.cur, ex.curOwned, ex.curFile = r, true, f
	ex.releaseWriter()
	ex.result.Stages = append(ex.result.Stages, StageReport{
		Index: ex.stageIdx, Name: s.name(), Materialized: true, BytesOut: size, Conversion: report,
	})
	return nil
}

// MergePipelines runs pipelines, several at once, and merges their outputs
// in the order given, as [Merge] merges documents. As the first stage it
// opens the pipeline on that merge, in place of [Pipeline.Open]; after
// another stage it appends them to the current document, as
// [Pipeline.Merge] appends Readers. The stage honours the options [Merge]
// honours.
//
// At most [WithPipelineConcurrency] pipelines run at a time, each to an
// intermediate file in the pipeline's temp directory, written in the clear
// and removed before [Pipeline.WriteTo] returns. Each runs within its own
// limits, so size a host for as many budgets as pipelines run at once. When
// one fails, those still running are cancelled and the stage fails with its
// error, naming the pipeline by its position; when several fail, the stage
// reports one of them. The result of each is the stage's
// [StageReport.Pipelines].
//
// A pipeline given here is declared like any other and never written by
// the caller: the stage writes it. It must not call MergePipelines itself
// ([ErrPipelineNested]) nor be given twice ([ErrPipelineShared]), and the
// list holds at least one ([ErrPipelineNoPipelines]). The caller keeps
// every Reader those pipelines open or merge open until
// [Pipeline.WriteTo] returns.
//
// The merged document conforms to no profile whatever its parts claimed:
// remediate it in a later [Pipeline.Apply] stage.
func (p *Pipeline) MergePipelines(pipelines []*Pipeline, opts ...WriteOption) *Pipeline {
	cp := append([]*Pipeline(nil), pipelines...)
	return p.add(&mergePipelinesStage{pipelines: cp, opts: opts})
}

type mergePipelinesStage struct {
	pipelines []*Pipeline
	opts      []WriteOption
}

func (s *mergePipelinesStage) kind() stageKind { return kindMerge }
func (s *mergePipelinesStage) name() string    { return stageNameMergePipelines }

// apply is written by wave 11, cell pipeline.
func (s *mergePipelinesStage) apply(_ context.Context, ex *executor) error {
	return fmt.Errorf("merging %d pipelines, %d at once, with %d options: not implemented", len(s.pipelines), ex.concurrency, len(s.opts))
}
