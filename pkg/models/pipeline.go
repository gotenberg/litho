package models

// PipelineResult holds per-stage execution info returned from
// [github.com/gotenberg/litho/pkg/pdf.Pipeline.WriteTo].
// Concurrent reads of a value are safe; a write needs synchronization.
type PipelineResult struct {
	// Stages reports one entry per executed stage in declaration order.
	Stages []StageReport `json:"stages"`
	// DroppedSignatures lists the signatures of the pipeline's documents that
	// its output does not carry, each once, in the order the stages dropped
	// them: a Split or a Merge drops every signature of the documents it
	// reads, and an Apply stage those its Writer's full rewrite drops (see
	// [github.com/gotenberg/litho/pkg/pdf.Writer.DroppedSignatures]).
	DroppedSignatures []DroppedSignature `json:"droppedSignatures"`
}

// StageReport summarizes a single pipeline stage.
// Concurrent reads of a value are safe; a write needs synchronization.
type StageReport struct {
	// Index is the 0-based position of the stage in declaration order.
	Index int `json:"index"`
	// Name is the short stage identifier: "open", "compose", "convert",
	// "merge-pipelines", "split", "merge", "apply", or "write".
	Name string `json:"name"`
	// Materialized is true if the stage flushed the pipeline state to
	// an intermediate temp file or to the final destination.
	Materialized bool `json:"materialized"`
	// BytesOut is the size of the intermediate file for restructuring
	// stages, or 0 otherwise.
	BytesOut int64 `json:"bytesOut"`
	// Conversion is the report of a
	// [github.com/gotenberg/litho/pkg/pdf.Pipeline.Convert] stage, nil for any
	// other stage.
	Conversion *ConvertReport `json:"conversion"`
	// Pipelines holds the result of each pipeline a
	// [github.com/gotenberg/litho/pkg/pdf.Pipeline.MergePipelines] stage ran,
	// in the order they were given, nil for any other stage.
	Pipelines []*PipelineResult `json:"pipelines"`
}
