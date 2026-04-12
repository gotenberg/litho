package pdf

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
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

// --- Validation ---

func TestPipeline_Validate_EmptyPipeline(t *testing.T) {
	err := NewPipeline().Validate()
	if !errors.Is(err, ErrPipelineEmpty) {
		t.Fatalf("want ErrPipelineEmpty, got %v", err)
	}
}

func TestPipeline_Validate_ApplyBeforeOpen(t *testing.T) {
	p := NewPipeline().Apply(func(_ context.Context, w *Writer) error {
		w.SetMetadata(Metadata{Title: "x"})
		return nil
	})
	err := p.Validate()
	if !errors.Is(err, ErrPipelineNoSource) {
		t.Fatalf("want ErrPipelineNoSource, got %v", err)
	}
}

func TestPipeline_Validate_MergeEmpty(t *testing.T) {
	r := openThreePage(t)
	defer r.Close()
	err := NewPipeline().Open(r).Merge(nil).Validate()
	if !errors.Is(err, ErrPipelineNothingToMerge) {
		t.Fatalf("want ErrPipelineNothingToMerge, got %v", err)
	}
}

func TestPipeline_Validate_SplitEmpty(t *testing.T) {
	r := openThreePage(t)
	defer r.Close()
	err := NewPipeline().Open(r).Split(nil).Validate()
	if !errors.Is(err, ErrPipelineEmptyPageList) {
		t.Fatalf("want ErrPipelineEmptyPageList, got %v", err)
	}
}

// --- Execution: pure Apply ---

func TestPipeline_OpenAndWrite(t *testing.T) {
	r := openThreePage(t)
	defer r.Close()

	var out bytes.Buffer
	result, err := NewPipeline().Open(r).WriteTo(context.Background(), &out)
	if err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if result == nil {
		t.Fatal("nil result")
	}
	r2, err := Open(context.Background(), bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Reopen: %v", err)
	}
	defer r2.Close()
	if n, _ := r2.NumPages(context.Background()); n != 3 {
		t.Errorf("pages: got %d, want 3", n)
	}
}

func TestPipeline_Apply_SetMetadata(t *testing.T) {
	r := openThreePage(t)
	defer r.Close()
	ctx := context.Background()

	var out bytes.Buffer
	_, err := NewPipeline().
		Open(r).
		Apply(func(_ context.Context, w *Writer) error {
			w.SetMetadata(Metadata{Title: "Piped"})
			return nil
		}).
		WriteTo(ctx, &out)
	if err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	r2, _ := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	defer r2.Close()
	meta, _ := r2.Metadata(ctx)
	if meta["Title"] != "Piped" {
		t.Errorf("title: got %q, want %q", meta["Title"], "Piped")
	}
}

func TestPipeline_Apply_Error_Surfaces(t *testing.T) {
	r := openThreePage(t)
	defer r.Close()

	var out bytes.Buffer
	_, err := NewPipeline().
		Open(r).
		Apply(func(_ context.Context, _ *Writer) error {
			return errors.New("boom")
		}).
		WriteTo(context.Background(), &out)
	if err == nil || !errors.As(err, new(*PipelineStageError)) {
		t.Fatalf("want PipelineStageError wrapping 'boom', got %v", err)
	}
}

// --- Execution: Split ---

func TestPipeline_Split_AsFinalStage(t *testing.T) {
	r := openThreePage(t)
	defer r.Close()
	ctx := context.Background()

	var out bytes.Buffer
	_, err := NewPipeline().Open(r).Split([]int{2}).WriteTo(ctx, &out)
	if err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	r2, _ := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	defer r2.Close()
	if n, _ := r2.NumPages(ctx); n != 1 {
		t.Errorf("pages: got %d, want 1", n)
	}
	text, _ := r2.ExtractText(ctx, 1)
	if text != "Page2" {
		t.Errorf("text: got %q, want Page2", text)
	}
}

func TestPipeline_Split_ThenApply(t *testing.T) {
	r := openThreePage(t)
	defer r.Close()
	ctx := context.Background()

	var out bytes.Buffer
	_, err := NewPipeline().
		Open(r).
		Split([]int{1, 2}).
		Apply(func(_ context.Context, w *Writer) error {
			w.SetMetadata(Metadata{Title: "After Split"})
			return nil
		}).
		WriteTo(ctx, &out)
	if err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	r2, _ := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	defer r2.Close()
	if n, _ := r2.NumPages(ctx); n != 2 {
		t.Errorf("pages: got %d, want 2", n)
	}
	meta, _ := r2.Metadata(ctx)
	if meta["Title"] != "After Split" {
		t.Errorf("title: got %q", meta["Title"])
	}
}

func TestPipeline_Apply_ThenSplit(t *testing.T) {
	r := openThreePage(t)
	defer r.Close()
	ctx := context.Background()

	var out bytes.Buffer
	_, err := NewPipeline().
		Open(r).
		Apply(func(_ context.Context, w *Writer) error {
			w.SetMetadata(Metadata{Title: "Before Split"})
			return nil
		}).
		Split([]int{1}).
		WriteTo(ctx, &out)
	if err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	r2, _ := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	defer r2.Close()
	meta, _ := r2.Metadata(ctx)
	if meta["Title"] != "Before Split" {
		t.Errorf("title: got %q", meta["Title"])
	}
}

// --- Execution: Split + Merge ---

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
		t.Errorf("stages: got %d, want 3", len(result.Stages))
	}
	r3, _ := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	defer r3.Close()
	if n, _ := r3.NumPages(ctx); n != 5 {
		t.Errorf("pages: got %d, want 5", n)
	}
}

// --- Temp-file management ---

func TestPipeline_TempDir_Honored(t *testing.T) {
	r := openThreePage(t)
	defer r.Close()
	tempDir := t.TempDir()

	var out bytes.Buffer
	_, err := NewPipeline().
		Open(r).
		Split([]int{1, 2}).
		Apply(func(_ context.Context, w *Writer) error {
			w.SetMetadata(Metadata{Title: "x"})
			return nil
		}).
		WriteTo(context.Background(), &out, WithPipelineTempDir(tempDir))
	if err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, filepath.Join(tempDir, e.Name()))
		}
		t.Errorf("temp dir not empty after success: %v", names)
	}
}

func TestPipeline_TempCleanup_OnError(t *testing.T) {
	r := openThreePage(t)
	defer r.Close()
	tempDir := t.TempDir()

	var out bytes.Buffer
	_, err := NewPipeline().
		Open(r).
		Split([]int{3}).
		Split([]int{99}). // out-of-bounds
		WriteTo(context.Background(), &out, WithPipelineTempDir(tempDir))
	if err == nil {
		t.Fatal("expected error")
	}
	entries, _ := os.ReadDir(tempDir)
	if len(entries) != 0 {
		t.Errorf("temp dir not empty after failure: %d entries", len(entries))
	}
}

func TestPipeline_PartialFailure_DstUntouched(t *testing.T) {
	r := openThreePage(t)
	defer r.Close()

	var out bytes.Buffer
	_, err := NewPipeline().
		Open(r).
		Split([]int{99}).
		Apply(func(_ context.Context, w *Writer) error {
			w.SetMetadata(Metadata{Title: "x"})
			return nil
		}).
		WriteTo(context.Background(), &out)
	if err == nil {
		t.Fatal("expected error")
	}
	if out.Len() != 0 {
		t.Errorf("dst should be untouched, got %d bytes", out.Len())
	}
}

// --- Context ---

func TestPipeline_ContextCanceled(t *testing.T) {
	r := openThreePage(t)
	defer r.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var out bytes.Buffer
	_, err := NewPipeline().
		Open(r).
		Apply(func(_ context.Context, w *Writer) error {
			w.SetMetadata(Metadata{Title: "x"})
			return nil
		}).
		WriteTo(ctx, &out)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("want context.Canceled, got %v", err)
	}
}

// --- Merge with bookmarks ---

func TestPipeline_MergeWithBookmarks(t *testing.T) {
	r1 := openThreePage(t)
	defer r1.Close()
	r2 := openThreePage(t)
	defer r2.Close()
	ctx := context.Background()

	var out bytes.Buffer
	_, err := NewPipeline().
		Open(r1).
		Merge([]*Reader{r2}, WithBookmarks(BookmarkOptions{
			Handling: BookmarkNone,
		})).
		WriteTo(ctx, &out)
	if err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	r3, _ := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	defer r3.Close()
	if n, _ := r3.NumPages(ctx); n != 6 {
		t.Errorf("pages: got %d, want 6", n)
	}
}
