package pdf

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

func openThreePage(t *testing.T) *Reader {
	t.Helper()
	data := threePagePDF()
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return r
}

// --- Validation tests ---

func TestPipeline_Validate_EmptyPipeline(t *testing.T) {
	p := NewPipeline()
	err := p.Validate()
	if !errors.Is(err, ErrPipelineEmpty) {
		t.Fatalf("want ErrPipelineEmpty, got %v", err)
	}
}

func TestPipeline_Validate_SetMetadataBeforeOpen(t *testing.T) {
	p := NewPipeline().SetMetadata(Metadata{Title: "x"})
	err := p.Validate()
	if !errors.Is(err, ErrPipelineNoSource) {
		t.Fatalf("want ErrPipelineNoSource, got %v", err)
	}
	var stageErr *PipelineStageError
	if !errors.As(err, &stageErr) {
		t.Fatalf("want *PipelineStageError, got %T", err)
	}
	if stageErr.Index != 0 || stageErr.Name != "set-metadata" {
		t.Errorf("stage info: got %+v", stageErr)
	}
}

func TestPipeline_Validate_SignNotLast(t *testing.T) {
	r := openThreePage(t)
	defer r.Close()
	p := NewPipeline().
		Open(r).
		Sign(&mockSigner{}).
		SetMetadata(Metadata{Title: "x"})
	err := p.Validate()
	if !errors.Is(err, ErrPipelineSignNotLast) {
		t.Fatalf("want ErrPipelineSignNotLast, got %v", err)
	}
}

func TestPipeline_Validate_EncryptWithPDFA(t *testing.T) {
	r := openThreePage(t)
	defer r.Close()
	tests := []struct {
		name string
		p    *Pipeline
	}{
		{
			"encrypt before remediate",
			NewPipeline().Open(r).Encrypt(EncryptConfig{}).RemediatePDFA(models.PDFA2b),
		},
		{
			"remediate before encrypt",
			NewPipeline().Open(r).RemediatePDFA(models.PDFA2b).Encrypt(EncryptConfig{}),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.p.Validate()
			if !errors.Is(err, ErrPipelineEncryptWithPDFA) {
				t.Fatalf("want ErrPipelineEncryptWithPDFA, got %v", err)
			}
		})
	}
}

func TestPipeline_Validate_FormAfterFlatten(t *testing.T) {
	r := openThreePage(t)
	defer r.Close()
	p := NewPipeline().
		Open(r).
		FlattenForm().
		SetFormField("name", "value")
	err := p.Validate()
	if !errors.Is(err, ErrPipelineFormAfterFlatten) {
		t.Fatalf("want ErrPipelineFormAfterFlatten, got %v", err)
	}
}

func TestPipeline_Validate_FlattenThenRestructureThenForm(t *testing.T) {
	// A restructure resets the flatten flag because the new doc has its
	// own form state.
	r := openThreePage(t)
	defer r.Close()
	p := NewPipeline().
		Open(r).
		FlattenForm().
		Split([]int{1}).
		SetFormField("name", "value")
	if err := p.Validate(); err != nil {
		t.Fatalf("validation should pass after restructure, got %v", err)
	}
}

func TestPipeline_Validate_MergeEmpty(t *testing.T) {
	r := openThreePage(t)
	defer r.Close()
	p := NewPipeline().Open(r).Merge(nil)
	err := p.Validate()
	if !errors.Is(err, ErrPipelineNothingToMerge) {
		t.Fatalf("want ErrPipelineNothingToMerge, got %v", err)
	}
}

func TestPipeline_Validate_SplitEmpty(t *testing.T) {
	r := openThreePage(t)
	defer r.Close()
	p := NewPipeline().Open(r).Split(nil)
	err := p.Validate()
	if !errors.Is(err, ErrPipelineEmptyPageList) {
		t.Fatalf("want ErrPipelineEmptyPageList, got %v", err)
	}
}

func TestPipeline_Validate_InvalidRotation(t *testing.T) {
	r := openThreePage(t)
	defer r.Close()
	p := NewPipeline().Open(r).RotatePages(45, 1)
	if err := p.Err(); !errors.Is(err, ErrPipelineInvalidRotation) {
		t.Fatalf("Err: want ErrPipelineInvalidRotation, got %v", err)
	}
	if err := p.Validate(); !errors.Is(err, ErrPipelineInvalidRotation) {
		t.Fatalf("Validate: want ErrPipelineInvalidRotation, got %v", err)
	}
}

func TestPipeline_Validate_SignWithEncrypt(t *testing.T) {
	r := openThreePage(t)
	defer r.Close()
	p := NewPipeline().
		Open(r).
		Encrypt(EncryptConfig{}).
		Sign(&mockSigner{})
	err := p.Validate()
	if !errors.Is(err, ErrPipelineSignWithEncrypt) {
		t.Fatalf("want ErrPipelineSignWithEncrypt, got %v", err)
	}
}

// --- Positive execution tests ---

func TestPipeline_OpenAndWrite_CopiesSource(t *testing.T) {
	r := openThreePage(t)
	defer r.Close()

	var out bytes.Buffer
	result, err := NewPipeline().Open(r).WriteTo(context.Background(), &out)
	if err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if result == nil {
		t.Fatal("expected result")
	}

	r2, err := Open(context.Background(), bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open output: %v", err)
	}
	defer r2.Close()
	n, _ := r2.NumPages(context.Background())
	if n != 3 {
		t.Errorf("page count: got %d, want 3", n)
	}
}

func TestPipeline_SetMetadata_AppliesMutation(t *testing.T) {
	r := openThreePage(t)
	defer r.Close()

	ctx := context.Background()
	var out bytes.Buffer
	_, err := NewPipeline().
		Open(r).
		SetMetadata(Metadata{Title: "Piped Title"}).
		WriteTo(ctx, &out)
	if err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open output: %v", err)
	}
	defer r2.Close()
	meta, _ := r2.GetMetadata(ctx)
	if meta["Title"] != "Piped Title" {
		t.Errorf("title: got %q, want %q", meta["Title"], "Piped Title")
	}
}

func TestPipeline_Split_AsFinalStage(t *testing.T) {
	r := openThreePage(t)
	defer r.Close()

	ctx := context.Background()
	tempDir := t.TempDir()

	var out bytes.Buffer
	_, err := NewPipeline().
		Open(r).
		Split([]int{2}).
		WriteTo(ctx, &out, WithPipelineTempDir(tempDir))
	if err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	// Split was the last stage, so it wrote directly to dst — no temp file
	// should remain in tempDir.
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected no temp files, got %d", len(entries))
	}

	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open output: %v", err)
	}
	defer r2.Close()
	n, _ := r2.NumPages(ctx)
	if n != 1 {
		t.Errorf("page count: got %d, want 1", n)
	}
	text, _ := r2.ExtractText(ctx, 1)
	if text != "Page2" {
		t.Errorf("text: got %q, want %q", text, "Page2")
	}
}

func TestPipeline_Split_ThenMutate(t *testing.T) {
	r := openThreePage(t)
	defer r.Close()

	ctx := context.Background()

	var out bytes.Buffer
	_, err := NewPipeline().
		Open(r).
		Split([]int{1, 2}).
		SetMetadata(Metadata{Title: "After Split"}).
		WriteTo(ctx, &out)
	if err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open output: %v", err)
	}
	defer r2.Close()
	n, _ := r2.NumPages(ctx)
	if n != 2 {
		t.Errorf("page count: got %d, want 2", n)
	}
	meta, _ := r2.GetMetadata(ctx)
	if meta["Title"] != "After Split" {
		t.Errorf("title: got %q, want %q", meta["Title"], "After Split")
	}
}

func TestPipeline_Mutate_ThenSplit(t *testing.T) {
	// Pending mutations must be flushed before the restructure so that the
	// reopened reader sees them.
	r := openThreePage(t)
	defer r.Close()

	ctx := context.Background()

	var out bytes.Buffer
	_, err := NewPipeline().
		Open(r).
		SetMetadata(Metadata{Title: "Before Split"}).
		Split([]int{1}).
		WriteTo(ctx, &out)
	if err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open output: %v", err)
	}
	defer r2.Close()
	meta, _ := r2.GetMetadata(ctx)
	if meta["Title"] != "Before Split" {
		t.Errorf("title: got %q, want %q", meta["Title"], "Before Split")
	}
}

func TestPipeline_SplitThenMerge(t *testing.T) {
	r1 := openThreePage(t)
	defer r1.Close()
	r2 := openThreePage(t)
	defer r2.Close()

	ctx := context.Background()

	var out bytes.Buffer
	result, err := NewPipeline().
		Open(r1).
		Split([]int{1, 2}).
		Merge([]*Reader{r2}).
		WriteTo(ctx, &out)
	if err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if len(result.Stages) != 3 {
		t.Errorf("stage count: got %d, want 3", len(result.Stages))
	}

	r3, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open output: %v", err)
	}
	defer r3.Close()
	n, _ := r3.NumPages(ctx)
	if n != 5 {
		t.Errorf("page count: got %d, want 5 (2 from split + 3 from merged)", n)
	}
}

func TestPipeline_TempDir_Honored(t *testing.T) {
	// When a restructure is NOT the last stage, it must spill to a temp file
	// in the configured directory.
	r := openThreePage(t)
	defer r.Close()

	ctx := context.Background()
	tempDir := t.TempDir()

	var out bytes.Buffer
	_, err := NewPipeline().
		Open(r).
		Split([]int{1, 2}).
		SetMetadata(Metadata{Title: "x"}).
		WriteTo(ctx, &out, WithPipelineTempDir(tempDir))
	if err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	// After a successful run, the executor must have cleaned up the temp
	// files it created.
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, filepath.Join(tempDir, e.Name()))
		}
		t.Errorf("expected temp dir empty after success, got %v", names)
	}
}

func TestPipeline_TempCleanup_OnError(t *testing.T) {
	// Trigger a failure after a restructure has already created a temp file,
	// and verify the file is still removed.
	r := openThreePage(t)
	defer r.Close()

	ctx := context.Background()
	tempDir := t.TempDir()

	var out bytes.Buffer
	// Split([3]) succeeds; the next Split([99]) fails at execution time
	// (out-of-bounds page).
	_, err := NewPipeline().
		Open(r).
		Split([]int{3}).
		Split([]int{99}).
		WriteTo(ctx, &out, WithPipelineTempDir(tempDir))
	if err == nil {
		t.Fatal("expected error from out-of-bounds split")
	}

	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected temp dir empty after failure, got %d entries", len(entries))
	}
}

func TestPipeline_PartialFailure_DstUntouched(t *testing.T) {
	// An early-stage failure should not write anything to the caller's dst.
	r := openThreePage(t)
	defer r.Close()

	ctx := context.Background()

	var out bytes.Buffer
	_, err := NewPipeline().
		Open(r).
		Split([]int{99}). // fails at exec time, well before final stage
		SetMetadata(Metadata{Title: "x"}).
		WriteTo(ctx, &out)
	if err == nil {
		t.Fatal("expected error")
	}
	if out.Len() != 0 {
		t.Errorf("dst should be untouched on early failure, got %d bytes", out.Len())
	}
}

func TestPipeline_Err_ShortCircuits(t *testing.T) {
	r := openThreePage(t)
	defer r.Close()

	p := NewPipeline().
		Open(r).
		RotatePages(45, 1). // invalid, records firstErr
		SetMetadata(Metadata{Title: "x"})

	if p.Err() == nil {
		t.Fatal("expected Err to surface the rotation error")
	}

	var out bytes.Buffer
	_, err := p.WriteTo(context.Background(), &out)
	if !errors.Is(err, ErrPipelineInvalidRotation) {
		t.Fatalf("WriteTo: want ErrPipelineInvalidRotation, got %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("dst should be untouched on validation error, got %d bytes", out.Len())
	}
}

func TestPipeline_MergeWithBookmarks_StageOptions(t *testing.T) {
	// WithBookmarks on a Merge stage should flow through to the merge call.
	r1 := openThreePage(t)
	defer r1.Close()
	r2 := openThreePage(t)
	defer r2.Close()

	ctx := context.Background()
	var out bytes.Buffer
	_, err := NewPipeline().
		Open(r1).
		Merge([]*Reader{r2}, WithBookmarks(BookmarkOptions{
			Handling:       BookmarkNone,
			WrapEachSource: false,
		})).
		WriteTo(ctx, &out)
	if err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	r3, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open output: %v", err)
	}
	defer r3.Close()
	n, _ := r3.NumPages(ctx)
	if n != 6 {
		t.Errorf("page count: got %d, want 6", n)
	}
}

func TestPipeline_ContextCanceled(t *testing.T) {
	r := openThreePage(t)
	defer r.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var out bytes.Buffer
	_, err := NewPipeline().
		Open(r).
		SetMetadata(Metadata{Title: "x"}).
		WriteTo(ctx, &out)
	if err == nil {
		t.Fatal("expected error from canceled ctx")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("want context.Canceled, got %v", err)
	}
}
