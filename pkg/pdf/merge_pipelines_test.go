package pdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/models"
)

// mergedText returns a pipeline that converts text, with the test fonts.
func mergedText(t *testing.T, text string) *Pipeline {
	t.Helper()
	return NewPipeline().Convert(FromText(strings.NewReader(text), WithConvertFontDirs(convertFontDir(t))))
}

// mergedPages returns a pipeline that keeps pages of r.
func mergedPages(r *Reader, pages ...int) *Pipeline {
	return NewPipeline().Open(r).Split(pages)
}

// blockingStage is an Apply closure that signals started, then blocks until
// its context ends.
func blockingStage(started chan<- struct{}) func(context.Context, *Writer) error {
	var once sync.Once
	return func(ctx context.Context, _ *Writer) error {
		once.Do(func() { close(started) })
		<-ctx.Done()
		return ctx.Err()
	}
}

// mergedPageTexts returns the text of every page of data.
func mergedPageTexts(t *testing.T, data []byte) []string {
	t.Helper()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open the output: %v", err)
	}
	defer r.Close()
	n, err := r.PageCount(ctx)
	if err != nil {
		t.Fatalf("PageCount: %v", err)
	}
	texts := make([]string, n)
	for i := range n {
		text, err := r.ExtractText(ctx, i+1)
		if err != nil {
			t.Fatalf("ExtractText of page %d: %v", i+1, err)
		}
		texts[i] = strings.TrimSpace(text)
	}
	return texts
}

// assertEmptyDir fails when dir holds a file.
func assertEmptyDir(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		t.Errorf("an intermediate file outlives WriteTo: %s", e.Name())
	}
}

// assertNoGoroutineLeft fails when more goroutines run than before, once
// those that are ending have ended.
func assertNoGoroutineLeft(t *testing.T, before int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for runtime.NumGoroutine() > before {
		if time.Now().After(deadline) {
			t.Fatalf("%d goroutines run, %d before the pipeline", runtime.NumGoroutine(), before)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestMergePipelinesMergesInTheOrderGiven(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := openThreePage(t)
	defer r.Close()

	var out bytes.Buffer
	result, err := NewPipeline().MergePipelines([]*Pipeline{
		mergedText(t, "first conversion"),
		mergedPages(r, 3, 2),
		mergedText(t, "last conversion"),
	}).WriteTo(ctx, &out, WithPipelineConcurrency(3))
	if err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	texts := mergedPageTexts(t, out.Bytes())
	if len(texts) != 4 {
		t.Fatalf("the output has %d pages, want 4", len(texts))
	}
	if !strings.Contains(texts[0], "first conversion") || !strings.Contains(texts[3], "last conversion") {
		t.Errorf("the conversions are not first and last: %q", texts)
	}
	if len(result.Stages) != 1 {
		t.Fatalf("%d stage reports, want 1", len(result.Stages))
	}
	report := result.Stages[0]
	if report.Name != "merge-pipelines" || !report.Materialized || len(report.Pipelines) != 3 {
		t.Fatalf("stage report = %+v", report)
	}
	if got := report.Pipelines[0].Stages[0].Name; got != "convert" {
		t.Errorf("the first pipeline's first stage is %q, want convert", got)
	}
	if report.Pipelines[0].Stages[0].Conversion == nil || report.Pipelines[0].Stages[0].Conversion.Pages != 1 {
		t.Errorf("the first pipeline's conversion report = %+v", report.Pipelines[0].Stages[0].Conversion)
	}
	if got := report.Pipelines[1].Stages[1].Name; got != "split" {
		t.Errorf("the second pipeline's second stage is %q, want split", got)
	}
}

func TestMergePipelinesAppendsToTheCurrentDocument(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := openThreePage(t)
	defer r.Close()
	other := openThreePage(t)
	defer other.Close()

	var out bytes.Buffer
	result, err := NewPipeline().Open(r).
		Apply(func(_ context.Context, w *Writer) error {
			w.SetMetadata(models.Metadata{Title: "merged"})
			return nil
		}).
		MergePipelines([]*Pipeline{mergedPages(other, 1), mergedText(t, "appended")}).
		Apply(func(_ context.Context, w *Writer) error {
			w.SetMetadata(models.Metadata{Author: "litho"})
			return nil
		}).
		WriteTo(ctx, &out)
	if err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	texts := mergedPageTexts(t, out.Bytes())
	if len(texts) != 5 || !strings.Contains(texts[4], "appended") {
		t.Fatalf("pages = %q, want the source's three, then one page, then the conversion", texts)
	}
	got, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer got.Close()
	meta, err := got.Metadata(ctx)
	if err != nil {
		t.Fatalf("Metadata: %v", err)
	}
	if meta["Title"] != "merged" || meta["Author"] != "litho" {
		t.Errorf("metadata = %v, want the edits before and after the stage", meta)
	}
	if n := len(result.Stages); n != 4 || result.Stages[2].BytesOut == 0 {
		t.Errorf("stage reports = %+v", result.Stages)
	}
}

func TestMergePipelinesWritesTheSameBytesAtEveryConcurrency(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := openThreePage(t)
	defer r.Close()

	write := func(n int) []byte {
		var pipelines []*Pipeline
		for i := range 6 {
			if i%2 == 0 {
				pipelines = append(pipelines, mergedText(t, fmt.Sprintf("conversion %d", i)))
			} else {
				pipelines = append(pipelines, mergedPages(r, i%3+1))
			}
		}
		var out bytes.Buffer
		if _, err := NewPipeline().MergePipelines(pipelines).WriteTo(ctx, &out, WithPipelineConcurrency(n)); err != nil {
			t.Fatalf("WriteTo at concurrency %d: %v", n, err)
		}
		return out.Bytes()
	}
	if !bytes.Equal(write(1), write(8)) {
		t.Error("the merged bytes differ between concurrency 1 and 8")
	}
}

// TestMergePipelinesFailsWithTheFailingPipeline pins the error of a stage one
// pipeline of which fails, at each position: it names that pipeline and wraps
// its error, while the others, blocked until their context ends, are
// cancelled and reported for nothing.
func TestMergePipelinesFailsWithTheFailingPipeline(t *testing.T) {
	t.Parallel()
	errBoom := errors.New("boom")
	const n = 4
	for failing := range n {
		t.Run(fmt.Sprintf("position %d", failing), func(t *testing.T) {
			t.Parallel()
			r := openThreePage(t)
			defer r.Close()
			dir := t.TempDir()
			pipelines := make([]*Pipeline, n)
			for i := range pipelines {
				if i == failing {
					pipelines[i] = NewPipeline().Open(r).Apply(func(context.Context, *Writer) error { return errBoom })
					continue
				}
				pipelines[i] = NewPipeline().Open(r).Apply(blockingStage(make(chan struct{})))
			}
			_, err := NewPipeline().MergePipelines(pipelines).
				WriteTo(context.Background(), &bytes.Buffer{}, WithPipelineConcurrency(n), WithPipelineTempDir(dir))
			if !errors.Is(err, errBoom) {
				t.Fatalf("err = %v, want it to wrap the failing pipeline's", err)
			}
			if want := fmt.Sprintf("pipeline %d: ", failing); !strings.Contains(err.Error(), want) {
				t.Errorf("err = %v, want it to name %q", err, want)
			}
			var stageErr *PipelineStageError
			if !errors.As(err, &stageErr) || stageErr.Name != "merge-pipelines" {
				t.Errorf("err = %v, want a PipelineStageError of the merge-pipelines stage", err)
			}
			assertEmptyDir(t, dir)
		})
	}
}

// TestMergePipelinesReportsTheFailureThatStoppedTheOthers pins which failure
// the stage reports when a pipeline fails after another one stopped it: the
// one that stopped it. The lower pipeline waits for its context to end, then
// fails with an error of its own, as a reading the cancellation cut off
// reports a faulty document, and the stage records it as the cancellation it
// follows. Under a context the caller cancels, the same error is the
// cancellation's too.
func TestMergePipelinesReportsTheFailureThatStoppedTheOthers(t *testing.T) {
	t.Parallel()
	r := openThreePage(t)
	defer r.Close()
	errFirst, errAfter := errors.New("first"), errors.New("after")
	failsAfter := func(started chan<- struct{}) *Pipeline {
		return NewPipeline().Open(r).Apply(func(ctx context.Context, _ *Writer) error {
			if started != nil {
				close(started)
			}
			<-ctx.Done()
			return errAfter
		})
	}

	first := NewPipeline().Open(r).Apply(func(context.Context, *Writer) error { return errFirst })
	_, err := NewPipeline().MergePipelines([]*Pipeline{mergedPages(r, 1), failsAfter(nil), mergedPages(r, 2), first}).
		WriteTo(context.Background(), &bytes.Buffer{}, WithPipelineConcurrency(4))
	if !errors.Is(err, errFirst) || errors.Is(err, errAfter) || !strings.Contains(err.Error(), "pipeline 3: ") {
		t.Errorf("a sibling failed first: err = %v, want pipeline 3's alone", err)
	}

	started := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-started
		cancel()
	}()
	_, err = NewPipeline().MergePipelines([]*Pipeline{mergedPages(r, 1), failsAfter(started)}).
		WriteTo(ctx, &bytes.Buffer{}, WithPipelineConcurrency(2))
	if !errors.Is(err, context.Canceled) || errors.Is(err, errAfter) {
		t.Errorf("the caller cancelled: err = %v, want the cancellation alone", err)
	}
}

// TestMergedFailureReportsTheLowestPosition pins the failure reported when
// several pipelines failed before any stopped the rest, and when all were
// cancelled: the one at the lowest position.
func TestMergedFailureReportsTheLowestPosition(t *testing.T) {
	t.Parallel()
	errLow, errHigh := errors.New("low"), errors.New("high")
	outs := make([]mergedOutput, 4)
	err := mergedFailure(context.Background(), []error{nil, context.Canceled, errLow, errHigh}, outs)
	if !errors.Is(err, errLow) || !strings.Contains(err.Error(), "pipeline 2: ") {
		t.Errorf("two failures of their own: err = %v, want pipeline 2's", err)
	}
	err = mergedFailure(context.Background(), []error{nil, context.Canceled, context.Canceled, nil}, outs)
	if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "pipeline 1: ") {
		t.Errorf("cancellations alone: err = %v, want pipeline 1's", err)
	}
}

// TestMergePipelinesStopsOnCancellation cancels the caller's context while a
// pipeline blocks in an Apply stage: the stage fails with the cancellation,
// every worker has exited and every intermediate file is gone when WriteTo
// returns.
func TestMergePipelinesStopsOnCancellation(t *testing.T) {
	before := runtime.NumGoroutine()
	r := openThreePage(t)
	defer r.Close()
	dir := t.TempDir()
	started := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-started
		cancel()
	}()
	pipelines := []*Pipeline{mergedPages(r, 1), NewPipeline().Open(r).Apply(blockingStage(started))}
	for range 6 {
		pipelines = append(pipelines, mergedText(t, "never merged"))
	}
	_, err := NewPipeline().MergePipelines(pipelines).
		WriteTo(ctx, &bytes.Buffer{}, WithPipelineConcurrency(2), WithPipelineTempDir(dir))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want the cancellation", err)
	}
	assertEmptyDir(t, dir)
	assertNoGoroutineLeft(t, before)
}

// TestMergePipelinesRecoversAPanic pins a panic inside an Apply closure of a
// merged pipeline: the stage fails with an error wrapping ErrInternalPanic,
// the process lives, and nothing is left behind.
func TestMergePipelinesRecoversAPanic(t *testing.T) {
	before := runtime.NumGoroutine()
	r := openThreePage(t)
	defer r.Close()
	dir := t.TempDir()
	panicking := NewPipeline().Open(r).Apply(func(context.Context, *Writer) error { panic("merged pipeline panics") })
	_, err := NewPipeline().MergePipelines([]*Pipeline{mergedPages(r, 1), panicking, mergedText(t, "after")}).
		WriteTo(context.Background(), &bytes.Buffer{}, WithPipelineTempDir(dir))
	if !errors.Is(err, ErrInternalPanic) || !strings.Contains(err.Error(), "pipeline 1: ") {
		t.Fatalf("err = %v, want pipeline 1's, wrapping ErrInternalPanic", err)
	}
	assertEmptyDir(t, dir)
	assertNoGoroutineLeft(t, before)
}

func TestMergePipelinesRemovesItsIntermediateFiles(t *testing.T) {
	t.Parallel()
	r := openThreePage(t)
	defer r.Close()
	for _, tail := range []string{"last stage", "edited after"} {
		dir := t.TempDir()
		p := NewPipeline().MergePipelines([]*Pipeline{mergedText(t, "one"), mergedPages(r, 2), mergedText(t, "three")})
		if tail == "edited after" {
			p = p.Apply(func(_ context.Context, w *Writer) error {
				w.SetMetadata(models.Metadata{Title: "x"})
				return nil
			}).Split([]int{1, 2})
		}
		if _, err := p.WriteTo(context.Background(), &bytes.Buffer{}, WithPipelineTempDir(dir)); err != nil {
			t.Fatalf("%s: WriteTo: %v", tail, err)
		}
		assertEmptyDir(t, dir)
	}
}

func TestMergePipelinesRefusesWhatItCannotRun(t *testing.T) {
	t.Parallel()
	r := openThreePage(t)
	defer r.Close()
	shared := mergedPages(r, 1)
	nested := NewPipeline().MergePipelines([]*Pipeline{mergedPages(r, 1)})
	outer := NewPipeline()
	outer.MergePipelines([]*Pipeline{outer})
	for name, tc := range map[string]struct {
		p    *Pipeline
		want error
	}{
		"no pipeline":                {NewPipeline().MergePipelines(nil), ErrPipelineNoPipelines},
		"an empty list":              {NewPipeline().MergePipelines([]*Pipeline{}), ErrPipelineNoPipelines},
		"a nil pipeline":             {NewPipeline().MergePipelines([]*Pipeline{mergedPages(r, 1), nil}), ErrPipelineNoPipelines},
		"a nested merge":             {NewPipeline().MergePipelines([]*Pipeline{nested}), ErrPipelineNested},
		"one pipeline given twice":   {NewPipeline().MergePipelines([]*Pipeline{shared, shared}), ErrPipelineShared},
		"one pipeline in two stages": {NewPipeline().MergePipelines([]*Pipeline{shared}).MergePipelines([]*Pipeline{shared}), ErrPipelineShared},
		"the merging pipeline":       {outer, ErrPipelineShared},
		"an invalid merged pipeline": {NewPipeline().MergePipelines([]*Pipeline{NewPipeline()}), ErrPipelineEmpty},
		"a merged pipeline unopened": {NewPipeline().Open(r).Split([]int{1}).MergePipelines([]*Pipeline{NewPipeline().Split([]int{1})}), ErrPipelineNoSource},
	} {
		if err := tc.p.Validate(); !errors.Is(err, tc.want) {
			t.Errorf("%s: Validate = %v, want %v", name, err, tc.want)
		}
		if _, err := tc.p.WriteTo(context.Background(), &bytes.Buffer{}); !errors.Is(err, tc.want) {
			t.Errorf("%s: WriteTo = %v, want %v", name, err, tc.want)
		}
	}
}

// TestMergePipelinesRefusesASharedConversion checks that one Conversion given
// to two of the pipelines a MergePipelines stage runs, or to the merging
// pipeline and one it merges, is refused when the pipeline is checked, at
// every concurrency, before its source is read.
func TestMergePipelinesRefusesASharedConversion(t *testing.T) {
	t.Parallel()
	fonts := WithConvertFontDirs(convertFontDir(t))
	for _, concurrency := range []int{1, 2, 4} {
		src := strings.NewReader("A")
		c := FromText(src, fonts)
		for name, p := range map[string]*Pipeline{
			"two merged pipelines": NewPipeline().MergePipelines([]*Pipeline{
				NewPipeline().Convert(c), mergedText(t, "B"), NewPipeline().Convert(c),
			}),
			"the merging pipeline and a merged one": NewPipeline().Convert(c).MergePipelines([]*Pipeline{NewPipeline().Convert(c)}),
		} {
			if err := p.Validate(); !errors.Is(err, ErrPipelineConversionShared) {
				t.Errorf("%s: Validate = %v, want ErrPipelineConversionShared", name, err)
			}
			var out bytes.Buffer
			_, err := p.WriteTo(context.Background(), &out, WithPipelineConcurrency(concurrency))
			if !errors.Is(err, ErrPipelineConversionShared) || IsInvalidInput(err) {
				t.Errorf("%s at concurrency %d: WriteTo = %v, want ErrPipelineConversionShared, not invalid input", name, concurrency, err)
			}
			if out.Len() != 0 {
				t.Errorf("%s at concurrency %d: wrote %d bytes, want none", name, concurrency, out.Len())
			}
		}
		if src.Len() != 1 {
			t.Errorf("concurrency %d: the shared source was read", concurrency)
		}
	}
}

func TestMergePipelinesRefusesAConcurrencyOutOfRange(t *testing.T) {
	t.Parallel()
	r := openThreePage(t)
	defer r.Close()
	for _, n := range []int{-1, 0, 65} {
		_, err := NewPipeline().MergePipelines([]*Pipeline{mergedPages(r, 1)}).
			WriteTo(context.Background(), &bytes.Buffer{}, WithPipelineConcurrency(n))
		if !errors.Is(err, ErrInvalidPipelineConcurrency) {
			t.Errorf("concurrency %d: err = %v, want ErrInvalidPipelineConcurrency", n, err)
		}
	}
	for _, n := range []int{1, 64} {
		if _, err := NewPipeline().MergePipelines([]*Pipeline{mergedPages(r, 1)}).
			WriteTo(context.Background(), &bytes.Buffer{}, WithPipelineConcurrency(n)); err != nil {
			t.Errorf("concurrency %d: %v", n, err)
		}
	}
}

// TestMergePipelinesCarriesEncryptionToTheOutput pins what an encryption a
// merged pipeline asks for means: the merging pipeline's output carries it,
// while the merged pipeline's own output, an intermediate file, is written in
// the clear and merges.
func TestMergePipelinesCarriesEncryptionToTheOutput(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := openThreePage(t)
	defer r.Close()
	cfg := models.EncryptConfig{UserPassword: "user", OwnerPassword: "owner", Algorithm: models.EncryptAES256, Permissions: -1}

	for name, p := range map[string]*Pipeline{
		"Writer.Encrypt": NewPipeline().MergePipelines([]*Pipeline{
			mergedPages(r, 1),
			NewPipeline().Open(r).Apply(func(_ context.Context, w *Writer) error { w.Encrypt(cfg); return nil }),
		}),
		"WithEncryption on a last split": NewPipeline().MergePipelines([]*Pipeline{
			NewPipeline().Open(r).Split([]int{1, 2, 3}, WithEncryption(cfg)),
			mergedPages(r, 1),
		}),
		"a merge edited after": NewPipeline().MergePipelines([]*Pipeline{
			NewPipeline().Open(r).Apply(func(_ context.Context, w *Writer) error { w.Encrypt(cfg); return nil }),
			mergedPages(r, 1),
		}).Apply(func(_ context.Context, w *Writer) error {
			w.SetMetadata(models.Metadata{Title: "x"})
			return nil
		}),
	} {
		var out bytes.Buffer
		if _, err := p.WriteTo(ctx, &out); err != nil {
			t.Fatalf("%s: WriteTo: %v", name, err)
		}
		if _, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len())); !errors.Is(err, ErrInvalidPassword) {
			t.Errorf("%s: the output opens without its user password (%v)", name, err)
			continue
		}
		got, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()), WithPassword("user"))
		if err != nil {
			t.Fatalf("%s: the output does not open under its user password: %v", name, err)
		}
		if n, err := got.PageCount(ctx); err != nil || n != 4 {
			t.Errorf("%s: the output has %d pages (%v), want 4", name, n, err)
		}
		got.Close()
	}
}

// TestMergePipelinesReadsAnEncryptedSource pins a merged pipeline that only
// opens an encrypted source: its output is decrypted, not copied as the
// ciphertext no password reopens, and the merge counts as encrypted, so a
// later signature is refused.
func TestMergePipelinesReadsAnEncryptedSource(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	encrypted := encryptedWithUserPassword(t, "user", "owner")
	src, err := Open(ctx, bytes.NewReader(encrypted), int64(len(encrypted)), WithPassword("user"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer src.Close()
	r := openThreePage(t)
	defer r.Close()

	var out bytes.Buffer
	if _, err := NewPipeline().MergePipelines([]*Pipeline{NewPipeline().Open(src), mergedPages(r, 1)}).WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	got, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("the output does not open without a password: %v", err)
	}
	defer got.Close()
	if got.IsEncrypted() {
		t.Error("the output is encrypted")
	}

	_, err = NewPipeline().MergePipelines([]*Pipeline{NewPipeline().Open(src), mergedPages(r, 1)}).
		Apply(func(_ context.Context, w *Writer) error { w.Sign(newCMSTestSigner(t)); return nil }).
		WriteTo(ctx, &bytes.Buffer{})
	if !errors.Is(err, ErrSignFullRewriteIncompatible) {
		t.Errorf("signing the merge of an encrypted source: err = %v, want ErrSignFullRewriteIncompatible", err)
	}
}

// TestMergePipelinesReportsDroppedSignatures pins that a signature a merged
// pipeline writes, which the merge drops, is refused unless
// WithSignatureRemoval allows it, and then reaches the merging pipeline's
// DroppedSignatures.
func TestMergePipelinesReportsDroppedSignatures(t *testing.T) {
	t.Parallel()
	r := openThreePage(t)
	defer r.Close()
	merge := func() *Pipeline {
		return NewPipeline().MergePipelines([]*Pipeline{
			mergedPages(r, 1),
			NewPipeline().Open(r).Apply(func(_ context.Context, w *Writer) error { w.Sign(newCMSTestSigner(t)); return nil }),
		})
	}
	if _, err := merge().WriteTo(context.Background(), &bytes.Buffer{}); !errors.Is(err, ErrSignaturesDropped) {
		t.Errorf("WriteTo without WithSignatureRemoval: err = %v, want ErrSignaturesDropped", err)
	}
	var dropped []models.DroppedSignature
	result, err := merge().WriteTo(context.Background(), &bytes.Buffer{}, WithDroppedSignatures(&dropped), WithSignatureRemoval())
	if err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if len(result.DroppedSignatures) != 1 || len(dropped) != 1 {
		t.Errorf("dropped signatures: result %v, option %v, want the merged pipeline's one", result.DroppedSignatures, dropped)
	}
}

// TestMergePipelinesAllowsSignatureRemovalInEachPipeline pins that the
// WithSignatureRemoval given to the merging pipeline's WriteTo reaches the
// stages of the pipelines it merges: a merged pipeline whose Split drops the
// signature its Apply wrote is refused without the option and written with
// it.
func TestMergePipelinesAllowsSignatureRemovalInEachPipeline(t *testing.T) {
	t.Parallel()
	r := openThreePage(t)
	defer r.Close()
	merge := func() *Pipeline {
		return NewPipeline().MergePipelines([]*Pipeline{
			NewPipeline().Open(r).
				Apply(func(_ context.Context, w *Writer) error { w.Sign(newCMSTestSigner(t)); return nil }).
				Split([]int{1}),
		})
	}
	if _, err := merge().WriteTo(context.Background(), &bytes.Buffer{}); !errors.Is(err, ErrSignaturesDropped) {
		t.Errorf("WriteTo without WithSignatureRemoval: err = %v, want ErrSignaturesDropped", err)
	}
	result, err := merge().WriteTo(context.Background(), &bytes.Buffer{}, WithSignatureRemoval())
	if err != nil {
		t.Fatalf("WriteTo with WithSignatureRemoval: %v", err)
	}
	if len(result.DroppedSignatures) != 1 {
		t.Errorf("dropped signatures: %v, want the merged pipeline's one", result.DroppedSignatures)
	}
}

// TestConversionStageConformsThroughRemediation pins the rule of a
// conversion: it claims no conformance, and a remediation stage after it
// makes the output conform, as the last edit.
func TestConversionStageConformsThroughRemediation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	remediate := func(ctx context.Context, w *Writer) error {
		_, err := w.RemediatePDFA(ctx, models.PDFA2b)
		return err
	}
	conforms := func(t *testing.T, data []byte) {
		t.Helper()
		r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		defer r.Close()
		violations, err := r.ValidatePDFA(ctx, models.PDFA2b)
		if err != nil || len(violations) > 0 {
			t.Errorf("ValidatePDFA = %v, %v, want no violation", violations, err)
		}
	}

	t.Run("a conversion, then a remediation", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		if _, err := mergedText(t, "conforming").Apply(remediate).WriteTo(ctx, &out); err != nil {
			t.Fatalf("WriteTo: %v", err)
		}
		conforms(t, out.Bytes())
	})
	t.Run("a restructuring after the remediation", func(t *testing.T) {
		t.Parallel()
		r := openThreePage(t)
		defer r.Close()
		for name, p := range map[string]*Pipeline{
			"split":           mergedText(t, "x").Apply(remediate).Split([]int{1}),
			"merge":           mergedText(t, "x").Apply(remediate).Merge([]*Reader{r}),
			"merge-pipelines": mergedText(t, "x").Apply(remediate).MergePipelines([]*Pipeline{mergedPages(r, 1)}),
		} {
			if _, err := p.WriteTo(ctx, &bytes.Buffer{}); !errors.Is(err, ErrEditAfterRemediation) {
				t.Errorf("%s: err = %v, want ErrEditAfterRemediation", name, err)
			}
		}
	})
	t.Run("two conversions merged, then a remediation", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		if _, err := NewPipeline().MergePipelines([]*Pipeline{mergedText(t, "one"), mergedText(t, "two")}).
			Apply(remediate).WriteTo(ctx, &out); err != nil {
			t.Fatalf("WriteTo: %v", err)
		}
		conforms(t, out.Bytes())
		if texts := mergedPageTexts(t, out.Bytes()); len(texts) != 2 {
			t.Errorf("the output has %d pages, want 2", len(texts))
		}
	})
	t.Run("a merged pipeline that ends in a remediation", func(t *testing.T) {
		t.Parallel()
		r := openThreePage(t)
		defer r.Close()
		var out bytes.Buffer
		if _, err := NewPipeline().MergePipelines([]*Pipeline{mergedText(t, "remediated").Apply(remediate), mergedPages(r, 1)}).
			WriteTo(ctx, &out); err != nil {
			t.Fatalf("WriteTo: %v", err)
		}
		if texts := mergedPageTexts(t, out.Bytes()); len(texts) != 2 || !strings.Contains(texts[0], "remediated") {
			t.Errorf("pages = %q", texts)
		}
	})
}
