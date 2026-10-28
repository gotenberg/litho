package jpx

import (
	"context"
	"math"
	"runtime"
	"runtime/debug"
	"sync/atomic"

	"github.com/gotenberg/litho/pkg/internal/intconv"
	"github.com/gotenberg/litho/pkg/internal/panicerr"
)

// minBlocksPerWorker is the fewest code-blocks [tileDecoder.decodeBlocks]
// hands each worker: a tile of fewer blocks than two workers' worth decodes on
// the calling goroutine. It keeps the fan-out from costing more than the
// small tiles it would split, and is a tuning constant, not spec-mandated.
const minBlocksPerWorker = 32

// minLinesPerWorker is the fewest rows, or columns, [reconstructLevel] hands
// each worker of one filtering pass: the low resolution levels, a few lines
// each, run on the calling goroutine. A tuning constant, not spec-mandated.
const minLinesPerWorker = 128

// workersFor returns how many workers share n items of work, at least per
// items each: at most GOMAXPROCS, so the fan-out is bounded by the processors
// the runtime schedules on whatever the image declares, and at least one.
func workersFor(n, per int) int {
	if w := int(forcedWorkers.Load()); w > 0 {
		return max(1, min(w, n))
	}
	return max(1, min(runtime.GOMAXPROCS(0), n/per))
}

// forcedWorkers, when positive, replaces the share of [workersFor] by that
// many workers, one item each at least. Tests set it to drive the parallel
// paths over images too small to reach them; it is zero otherwise.
var forcedWorkers atomic.Int32

// workerHook, when set, runs in each worker of the parallel path before every
// item. Tests outside the package set it, through [SetWorkerHookForTest], to
// plant a worker panic; it is nil otherwise.
var workerHook atomic.Pointer[func(item int)]

// SetWorkerHookForTest makes every decode share its work among workers, two
// at least, and run hook in each worker before every item, until the restore
// function it returns is called. It exists for tests of the layers above this
// package, which plant a panic in a worker to check how they report it; no
// other code calls it. It affects every decode in the process, so a test that
// calls it must not run in parallel.
func SetWorkerHookForTest(workers int, hook func(item int)) (restore func()) {
	forcedWorkers.Store(int32(max(2, min(workers, maxTestWorkers)))) //nolint:gosec // bounded by maxTestWorkers.
	workerHook.Store(&hook)
	return func() {
		workerHook.Store(nil)
		forcedWorkers.Store(0)
	}
}

// maxTestWorkers bounds the workers [SetWorkerHookForTest] forces.
const maxTestWorkers = 64

// forEach calls work(w, i) for every i in [0, n), on nw workers, w being the
// worker's index in [0, nw), so a worker keeps scratch state of its own. With
// one worker it runs on the calling goroutine.
//
// The result is that of the serial loop whatever the schedule: workers take
// the items in increasing order, an item past one that failed is not started,
// and the error returned is that of the lowest item that failed. Every item
// below it has run, so it is the error a serial loop stops at; work must leave
// items independent of one another for the rest to follow.
//
// Per CONTRIBUTING "Goroutine lifecycle", no worker outlives the call: each
// checks ctx before every item, the collection selects on ctx.Done() and then
// waits for the workers, which stop at their next item, and a worker's panic
// is recovered into a [panicerr.Error] for that item, which carries the stack
// for the caller holding the document logger.
func forEach(ctx context.Context, n, nw int, work func(w, i int) error) error {
	if nw <= 1 {
		for i := range n {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := work(0, i); err != nil {
				return err
			}
		}
		return nil
	}
	type result struct {
		item int
		err  error
	}
	var next atomic.Int64
	var failed atomic.Int64
	failed.Store(math.MaxInt64)
	lower := func(i int) {
		for {
			f := failed.Load()
			if int64(i) >= f || failed.CompareAndSwap(f, int64(i)) {
				return
			}
		}
	}
	ch := make(chan result, nw)
	for w := range nw {
		go func() {
			res := result{item: -1}
			item := -1
			defer func() {
				if rec := recover(); rec != nil {
					lower(item)
					res = result{item: item, err: &panicerr.Error{Where: "JPEG 2000 decoding worker", Value: rec, Stack: debug.Stack()}}
				}
				ch <- res
			}()
			for {
				item = intconv.Clamp(next.Add(1) - 1)
				if item >= n || int64(item) > failed.Load() {
					return
				}
				err := ctx.Err()
				if err == nil {
					if hook := workerHook.Load(); hook != nil {
						(*hook)(item)
					}
					err = work(w, item)
				}
				if err != nil {
					lower(item)
					res = result{item: item, err: err}
					return
				}
			}
		}()
	}
	first := result{item: -1}
	for pending := nw; pending > 0; pending-- {
		select {
		case r := <-ch:
			if r.err != nil && (first.err == nil || r.item < first.item) {
				first = r
			}
		case <-ctx.Done():
			// The channel is buffered, so no worker blocks on its send, and
			// each stops at its next item: waiting keeps them from outliving
			// the call.
			for ; pending > 0; pending-- {
				<-ch
			}
			return ctx.Err()
		}
	}
	return first.err
}
