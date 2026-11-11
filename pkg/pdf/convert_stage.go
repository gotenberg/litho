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
// At most [WithPipelineConcurrency] pipelines run at a time, taken in the
// order given, each to an intermediate file in the pipeline's temp
// directory, written in the clear and removed before [Pipeline.WriteTo]
// returns. Each runs as [Pipeline.WriteTo] runs it, with the logger, the temp
// directory and the [WithDefaultPageSize] of the pipeline that merges it. The
// merged bytes are the same whatever the concurrency.
//
// Each pipeline spends memory within its own limits, a conversion up to
// twice [Limits.MaxConvertMemoryBytes], so size a host for that many times
// what one pipeline holds, plus the merge, which reads every output at
// once. Concurrency 1 holds one pipeline at a time.
//
// When one fails, those still running are cancelled through their context,
// none starts after it, and the stage fails naming the pipeline by its
// position and wrapping its error. When several fail, the stage reports the
// lowest position that failed for a reason other than that cancellation. A
// panic in a pipeline, an Apply closure's included, fails the stage with an
// error wrapping [ErrInternalPanic]. The result of each is the stage's
// [StageReport.Pipelines], and the signatures each drops, and those of its
// output the merge drops, reach [PipelineResult.DroppedSignatures].
//
// A pipeline given here is declared like any other and never written by
// the caller: the stage writes it. It must not call MergePipelines itself
// ([ErrPipelineNested]) nor be given twice, in this stage or another one of
// the pipeline, nor be the pipeline itself ([ErrPipelineShared]), and the
// list holds at least one, none nil ([ErrPipelineNoPipelines]). The caller
// keeps every Reader those pipelines open or merge open until
// [Pipeline.WriteTo] returns. One Reader may be opened by several of them.
//
// An encryption a pipeline asks for, by [Writer.Encrypt] in an Apply stage or
// by [WithEncryption] on one of its stages, is asked of the output of the
// pipeline that merges it, as one of its own stages would ask (see
// [Pipeline]). When several ask, the last given wins, and a later request of
// the merging pipeline replaces it. A pipeline that reads an encrypted
// document makes the merge count as encrypted, so a later [Writer.Sign] is
// refused.
//
// The merged document conforms to no profile whatever its parts claimed:
// remediate it in a later [Pipeline.Apply] stage. A MergePipelines after the
// stage that remediates is refused with [ErrEditAfterRemediation].
func (p *Pipeline) MergePipelines(pipelines []*Pipeline, opts ...WriteOption) *Pipeline {
	cp := append([]*Pipeline(nil), pipelines...)
	return p.add(&mergePipelinesStage{pipelines: cp, opts: opts})
}
