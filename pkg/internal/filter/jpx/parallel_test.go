package jpx

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/panicerr"
)

func TestForEach_RunsEveryItemOnce(t *testing.T) {
	t.Parallel()

	for _, nw := range []int{1, 2, 7} {
		const n = 1000
		var counts [n]atomic.Int32
		err := forEach(context.Background(), n, nw, func(w, i int) error {
			if w < 0 || w >= nw {
				t.Errorf("worker index %d outside [0, %d)", w, nw)
			}
			counts[i].Add(1)
			return nil
		})
		if err != nil {
			t.Fatalf("%d workers: %v", nw, err)
		}
		for i := range counts {
			if c := counts[i].Load(); c != 1 {
				t.Fatalf("%d workers: item %d ran %d times", nw, i, c)
			}
		}
	}
}

// TestForEach_ReturnsTheFirstFailure checks the error is the serial loop's,
// that of the lowest failing item, however the workers are scheduled.
func TestForEach_ReturnsTheFirstFailure(t *testing.T) {
	t.Parallel()

	for range 200 {
		err := forEach(context.Background(), 500, 8, func(_, i int) error {
			if i == 37 || i == 41 || i == 400 {
				return fmt.Errorf("item %d", i)
			}
			return nil
		})
		if err == nil || err.Error() != "item 37" {
			t.Fatalf("got %v, want the error of item 37", err)
		}
	}
}

func TestForEach_RecoversAWorkerPanic(t *testing.T) {
	t.Parallel()

	for _, nw := range []int{2, 4} {
		err := forEach(context.Background(), 64, nw, func(_, i int) error {
			if i == 9 {
				panic("boom")
			}
			return nil
		})
		if !errors.Is(err, panicerr.ErrInternalPanic) {
			t.Fatalf("%d workers: got %v, want ErrInternalPanic", nw, err)
		}
		var pe *panicerr.Error
		if !errors.As(err, &pe) || len(pe.Stack) == 0 || !strings.Contains(err.Error(), "boom") {
			t.Fatalf("%d workers: the panic lost its value or stack: %v", nw, err)
		}
	}
}

func TestForEach_StopsOnCancellation(t *testing.T) {
	t.Parallel()

	for _, nw := range []int{1, 4} {
		ctx, cancel := context.WithCancel(context.Background())
		// A worker checks ctx before each item, so once cancel has returned
		// each starts at most the one item it had already checked.
		var canceled atomic.Bool
		var late atomic.Int32
		err := forEach(ctx, 100000, nw, func(_, i int) error {
			if canceled.Load() {
				late.Add(1)
			}
			if i == 10 {
				cancel()
				canceled.Store(true)
			}
			return nil
		})
		cancel()
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("%d workers: got %v, want context.Canceled", nw, err)
		}
		if n := late.Load(); n > int32(nw) { //nolint:gosec // nw is a single-digit worker count.
			t.Errorf("%d workers: %d items started after the cancellation", nw, n)
		}
	}
}

// TestDecode_ParallelMatchesSerial decodes every test codestream on one
// worker, then on several, and requires the same samples and the same error:
// the fixtures are too small for the default share to fan out, so the test
// forces it. It does not run in parallel, since it sets the package's worker
// count.
func TestDecode_ParallelMatchesSerial(t *testing.T) {
	paths, err := filepath.Glob("testdata/*.j2k")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no test codestreams: %v", err)
	}
	decode := func(workers int, data []byte) (*Image, error) {
		forcedWorkers.Store(int32(workers)) //nolint:gosec // the test's worker counts are single digits.
		defer forcedWorkers.Store(0)
		return Decode(context.Background(), data, DefaultMaxSamples)
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		// A truncated codestream exercises the error and partial-data paths.
		for _, input := range [][]byte{data, data[:len(data)*2/3]} {
			want, wantErr := decode(1, input)
			for _, workers := range []int{2, 3, 8} {
				got, gotErr := decode(workers, input)
				if fmt.Sprint(gotErr) != fmt.Sprint(wantErr) {
					t.Fatalf("%s (%d bytes) on %d workers: error %v, want %v", path, len(input), workers, gotErr, wantErr)
				}
				if wantErr != nil {
					continue
				}
				for c := range want.Components {
					if !slices.Equal(got.Components[c].Samples, want.Components[c].Samples) {
						t.Fatalf("%s (%d bytes) on %d workers: component %d differs from the serial decode", path, len(input), workers, c)
					}
				}
			}
		}
	}
}
