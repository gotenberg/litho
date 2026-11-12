package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"runtime/metrics"
	"runtime/pprof"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gotenberg/litho/pkg/models"
	"github.com/gotenberg/litho/pkg/pdf"
)

// What `corpus convert-one --measure` reads around one operation, in the
// child process that runs it alone. The parent adds what only it sees: the
// process's peak resident memory and wall time from start to exit.

// costSample is what one operation cost, read in the process that ran it.
// Every count is the change across the operation, not since the process
// started.
type costSample struct {
	// WallSeconds is the operation's wall time, call to return.
	WallSeconds float64 `json:"wall_s"`
	// UserSeconds and SysSeconds are the CPU time the process spent during
	// the operation, every goroutine and the collector included.
	UserSeconds float64 `json:"user_s"`
	SysSeconds  float64 `json:"sys_s"`
	// RSSBaseBytes is the process's peak resident memory before the
	// operation began: the runtime, and for a conversion the font index.
	RSSBaseBytes int64 `json:"rss_base_bytes"`
	// HeapPeakBytes is the most heap the live and unswept objects held,
	// sampled every [costSampleEvery].
	HeapPeakBytes int64 `json:"heap_peak_bytes"`
	// AllocBytes and AllocObjects count what the operation allocated.
	AllocBytes   uint64 `json:"alloc_bytes"`
	AllocObjects uint64 `json:"alloc_objects"`
	// GCCount, GCPauseSeconds and GCCPUSeconds are the collections the
	// operation ran, their stop-the-world pauses and the CPU time the
	// runtime estimates the collector spent.
	GCCount        uint32  `json:"gc_count"`
	GCPauseSeconds float64 `json:"gc_pause_s"`
	GCCPUSeconds   float64 `json:"gc_cpu_s"`
	// BudgetPeakBytes is the most memory a conversion's budget accounted at
	// once, summed over the copies; 0 for a PDF operation, which has none.
	BudgetPeakBytes int64 `json:"budget_peak_bytes"`
	// LogRecords counts the Debug records a conversion logged.
	LogRecords int64 `json:"log_records,omitempty"`
	// InputBytes is the size of the source: the document and the files
	// beside it a stress HTML page shows.
	InputBytes int64 `json:"input_bytes"`
	// OutputPages and OutputBytes describe what the operation produced: the
	// PDF's pages and bytes, the text's bytes, or the rendered pixels' bytes.
	OutputPages int   `json:"output_pages"`
	OutputBytes int64 `json:"output_bytes"`
	// Copies is how many copies of the operation ran at once.
	Copies int `json:"copies"`
}

// costSampleEvery is how often the heap sampler reads the heap.
const costSampleEvery = time.Millisecond

// costHeapMetric is the runtime metric the sampler reads: the bytes of
// heap objects, live and not yet swept, which the process holds resident.
const costHeapMetric = "/memory/classes/heap/objects:bytes"

// costGCCPUMetric is the runtime's estimate of the CPU the collector spent.
const costGCCPUMetric = "/cpu/classes/gc/total:cpu-seconds"

// costHeapStep is how much the heap must grow past the last heap profile
// taken before the sampler takes another: a profile run writes the profile
// nearest the peak without writing one every sample.
const costHeapStep = 1.15

// costProfileNames are the files a profile run writes into its directory.
const (
	costCPUProfile   = "cpu.pprof"
	costHeapProfile  = "heap-peak.pprof"
	costAllocProfile = "allocs.pprof"
	costMutexProfile = "mutex.pprof"
	costBlockProfile = "block.pprof"
)

// costMutexFraction and costBlockRate are the sampling rates of the mutex
// and block profiles of a profile run: one contention event in 10, one
// blocking event per 100 µs blocked.
const (
	costMutexFraction = 10
	costBlockRate     = 100_000
)

// costProbe measures one operation: it starts before the operation and
// stops after it.
type costProbe struct {
	profileDir string

	start     time.Time
	rusage    costRusage
	mem       runtime.MemStats
	gcCPU     float64
	heapPeak  atomic.Int64
	stop      chan struct{}
	done      chan struct{}
	heapProf  []byte
	profileMu sync.Mutex
	cpu       *os.File
}

// startCostProbe starts measuring; profileDir, when set, receives the
// profiles of [costProfileNames].
func startCostProbe(profileDir string) (*costProbe, error) {
	p := &costProbe{profileDir: profileDir, stop: make(chan struct{}), done: make(chan struct{})}
	if profileDir != "" {
		if err := os.MkdirAll(profileDir, 0o755); err != nil {
			return nil, err
		}
		runtime.SetMutexProfileFraction(costMutexFraction)
		runtime.SetBlockProfileRate(costBlockRate)
		f, err := os.Create(filepath.Join(profileDir, costCPUProfile)) //nolint:gosec // a directory the caller names
		if err != nil {
			return nil, err
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			f.Close()
			return nil, err
		}
		p.cpu = f
	}
	runtime.GC()
	runtime.ReadMemStats(&p.mem)
	p.gcCPU = readCostMetric(costGCCPUMetric)
	p.rusage = readCostRusage()
	p.start = time.Now()
	go p.sample()
	return p, nil
}

// sample reads the heap every [costSampleEvery] until stop closes, keeping
// the peak and, in a profile run, the heap profile nearest it.
func (p *costProbe) sample() {
	t := time.NewTicker(costSampleEvery)
	defer t.Stop()
	s := []metrics.Sample{{Name: costHeapMetric}}
	profiled := int64(0)
	for {
		metrics.Read(s)
		if s[0].Value.Kind() == metrics.KindUint64 {
			heap := int64(min(s[0].Value.Uint64(), math.MaxInt64)) //nolint:gosec // bounded above
			if heap > p.heapPeak.Load() {
				p.heapPeak.Store(heap)
			}
			if p.profileDir != "" && float64(heap) > float64(profiled)*costHeapStep {
				var buf bytes.Buffer
				if err := pprof.Lookup("heap").WriteTo(&buf, 0); err == nil {
					p.profileMu.Lock()
					p.heapProf = buf.Bytes()
					p.profileMu.Unlock()
					profiled = heap
				}
			}
		}
		select {
		case <-p.stop:
			close(p.done)
			return
		case <-t.C:
		}
	}
}

// finish stops measuring and returns what the operation cost.
func (p *costProbe) finish() (costSample, error) {
	wall := time.Since(p.start)
	ru := readCostRusage()
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	gcCPU := readCostMetric(costGCCPUMetric)
	close(p.stop)
	<-p.done
	s := costSample{
		WallSeconds:    wall.Seconds(),
		UserSeconds:    ru.user - p.rusage.user,
		SysSeconds:     ru.sys - p.rusage.sys,
		RSSBaseBytes:   p.rusage.maxRSS,
		HeapPeakBytes:  p.heapPeak.Load(),
		AllocBytes:     mem.TotalAlloc - p.mem.TotalAlloc,
		AllocObjects:   mem.Mallocs - p.mem.Mallocs,
		GCCount:        mem.NumGC - p.mem.NumGC,
		GCPauseSeconds: float64(mem.PauseTotalNs-p.mem.PauseTotalNs) / 1e9,
		GCCPUSeconds:   gcCPU - p.gcCPU,
	}
	if p.profileDir == "" {
		return s, nil
	}
	pprof.StopCPUProfile()
	if err := p.cpu.Close(); err != nil {
		return s, err
	}
	p.profileMu.Lock()
	heap := p.heapProf
	p.profileMu.Unlock()
	if err := os.WriteFile(filepath.Join(p.profileDir, costHeapProfile), heap, 0o644); err != nil { //nolint:gosec // a profile, not a secret
		return s, err
	}
	for name, file := range map[string]string{"allocs": costAllocProfile, "mutex": costMutexProfile, "block": costBlockProfile} {
		if err := writeCostProfile(name, filepath.Join(p.profileDir, file)); err != nil {
			return s, err
		}
	}
	return s, nil
}

// writeCostProfile writes the runtime profile name to path.
func writeCostProfile(name, path string) error {
	f, err := os.Create(path) //nolint:gosec // a directory the caller names
	if err != nil {
		return err
	}
	if err := pprof.Lookup(name).WriteTo(f, 0); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// readCostMetric returns the float64 runtime metric name, 0 when the
// runtime does not report it.
func readCostMetric(name string) float64 {
	s := []metrics.Sample{{Name: name}}
	metrics.Read(s)
	if s[0].Value.Kind() != metrics.KindFloat64 {
		return 0
	}
	return s[0].Value.Float64()
}

// budgetPeakHandler reads a conversion's memory account from the Debug
// record [pdf.Conversion.WriteTo] logs as it ends, and counts every Debug
// record. The account is not part of the public API: the record is how the
// conversion states it.
type budgetPeakHandler struct {
	peak    *atomic.Int64
	records *atomic.Int64
}

// costBudgetRecord is the message of the record that holds the account.
const costBudgetRecord = "convert: memory account"

func (h budgetPeakHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h budgetPeakHandler) Handle(_ context.Context, r slog.Record) error {
	h.records.Add(1)
	if r.Message != costBudgetRecord {
		return nil
	}
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "peak" && a.Value.Kind() == slog.KindInt64 {
			h.peak.Store(a.Value.Int64())
			return false
		}
		return true
	})
	return nil
}

func (h budgetPeakHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h budgetPeakHandler) WithGroup(string) slog.Handler      { return h }

// costOps are the PDF operations `convert-one --op` measures, by name.
// Each reads the PDF at src and writes what it produces to out, returning
// the bytes it produced when they are not a PDF (text, pixels), else -1.
// An operation that draws text it may not find in the document's fonts
// reads the fonts of the font directories.
var costOps = map[string]costOp{
	// text opens the PDF and extracts the text of every page.
	"text": func(ctx context.Context, src string, _ []string, _ io.Writer) (int64, error) {
		r, err := pdf.OpenFile(ctx, src)
		if err != nil {
			return 0, err
		}
		defer r.Close()
		n, err := r.PageCount(ctx)
		if err != nil {
			return 0, err
		}
		var total int64
		for p := 1; p <= n; p++ {
			t, err := r.ExtractText(ctx, p)
			if err != nil && !errors.Is(err, pdf.ErrRequiresOCR) {
				return 0, fmt.Errorf("page %d: %w", p, err)
			}
			total += int64(len(t))
		}
		return total, nil
	},
	// rewrite rewrites the whole document with object streams.
	"rewrite": costWrite(func(context.Context, *pdf.Writer, []string) error { return nil }, pdf.WithFullRewrite(), pdf.WithObjectStreams()),
	// merge merges the document with itself.
	"merge": func(ctx context.Context, src string, _ []string, out io.Writer) (int64, error) {
		r, err := pdf.OpenFile(ctx, src)
		if err != nil {
			return 0, err
		}
		defer r.Close()
		return -1, pdf.Merge(ctx, []*pdf.Reader{r, r}, out)
	},
	// split extracts the first half of the pages, the first at least.
	"split": func(ctx context.Context, src string, _ []string, out io.Writer) (int64, error) {
		r, err := pdf.OpenFile(ctx, src)
		if err != nil {
			return 0, err
		}
		defer r.Close()
		n, err := r.PageCount(ctx)
		if err != nil {
			return 0, err
		}
		pages := make([]int, 0, (n+1)/2)
		for p := 1; p <= (n+1)/2; p++ {
			pages = append(pages, p)
		}
		return -1, pdf.Split(ctx, r, pages, out)
	},
	// render renders the first page at [costRenderDPI].
	"render": func(ctx context.Context, src string, _ []string, _ io.Writer) (int64, error) {
		r, err := pdf.OpenFile(ctx, src)
		if err != nil {
			return 0, err
		}
		defer r.Close()
		img, err := r.RenderPage(ctx, 1, pdf.WithRenderDPI(costRenderDPI))
		if err != nil {
			return 0, err
		}
		b := img.Bounds()
		return int64(b.Dx()) * int64(b.Dy()) * 4, nil
	},
	// pdfa validates the document against PDF/A-2b.
	"pdfa": costValidate(func(ctx context.Context, r *pdf.Reader) ([]pdf.Violation, error) {
		return r.ValidatePDFAReport(ctx, models.PDFA2b)
	}),
	// pdfua validates the document against PDF/UA-1.
	"pdfua": costValidate(func(ctx context.Context, r *pdf.Reader) ([]pdf.Violation, error) {
		return r.ValidatePDFUAReport(ctx, models.PDFUA1)
	}),
	// remediate remediates the document to PDF/A-2b and writes it.
	"remediate": costWrite(func(ctx context.Context, w *pdf.Writer, _ []string) error {
		_, err := w.RemediatePDFA(ctx, models.PDFA2b)
		return err
	}),
	// flatten flattens the form, drawing a value outside WinAnsiEncoding
	// with the fonts, and draws every printed annotation into its page.
	"flatten": costWrite(func(ctx context.Context, w *pdf.Writer, fonts []string) error {
		sources := make([]pdf.FontSource, len(fonts))
		for i, dir := range fonts {
			sources[i] = pdf.NewFontFileSource(os.DirFS(dir))
		}
		w.FlattenForm(pdf.WithFlattenFonts(pdf.JoinFontSources(sources...)))
		_, err := w.FlattenTransparency(ctx, pdf.WithFlattenPrintedAnnotations())
		return err
	}),
	// stamp stamps a diagonal watermark on every page.
	"stamp": costWrite(func(_ context.Context, w *pdf.Writer, _ []string) error {
		return w.StampText(pdf.TextStamp{Text: "CONFIDENTIAL", Position: pdf.PositionCenter, FontSize: 48, Opacity: 0.3, Rotation: 45, Color: [3]float64{0.8, 0, 0}})
	}),
	// colors converts every colour to the built-in CMYK profile.
	"colors": costWrite(func(ctx context.Context, w *pdf.Writer, _ []string) error {
		_, err := w.ConvertColors(ctx, pdf.CMYKColorTarget())
		return err
	}),
}

// costOp is one PDF operation of [costOps].
type costOp func(ctx context.Context, src string, fonts []string, out io.Writer) (int64, error)

// costRenderDPI is the resolution the render operation draws at.
const costRenderDPI = 150

// costWrite returns an operation that edits the document with edit and
// writes it with opts.
func costWrite(edit func(context.Context, *pdf.Writer, []string) error, opts ...pdf.WriteOption) costOp {
	return func(ctx context.Context, src string, fonts []string, out io.Writer) (int64, error) {
		r, err := pdf.OpenFile(ctx, src)
		if err != nil {
			return 0, err
		}
		defer r.Close()
		w := pdf.NewWriter(r)
		defer w.Close()
		if err := edit(ctx, w, fonts); err != nil {
			return 0, err
		}
		return -1, w.WriteTo(ctx, out, opts...)
	}
}

// costValidate returns an operation that validates the document with
// check, counting the bytes of a line a violation.
func costValidate(check func(context.Context, *pdf.Reader) ([]pdf.Violation, error)) costOp {
	return func(ctx context.Context, src string, _ []string, _ io.Writer) (int64, error) {
		r, err := pdf.OpenFile(ctx, src)
		if err != nil {
			return 0, err
		}
		defer r.Close()
		vs, err := check(ctx, r)
		if err != nil {
			return 0, err
		}
		var total int64
		for _, v := range vs {
			total += int64(len(v.Error()) + 1)
		}
		return total, nil
	}
}

// costJob is one copy of the operation a measured child runs.
type costJob struct {
	out    *os.File
	budget atomic.Int64
	logs   atomic.Int64
	report *pdf.ConvertReport
	text   int64
	err    error
}

// measureOne runs op (a name of [costOps], "convert", or "cold" for a
// conversion in a process whose font index is not built yet) on src copies
// times at once, each copy on its own goroutine, and returns how it ended
// and what it cost.
func measureOne(ctx context.Context, op, src string, fonts []string, copies int, profileDir string) convertOneResult {
	fail := func(err error) convertOneResult { return convertOneResult{Result: "error", Detail: err.Error()} }
	run, inputPath, err := costOperation(op, src, fonts)
	if err != nil {
		return fail(err)
	}
	inputBytes, err := costInputBytes(inputPath)
	if err != nil {
		return fail(err)
	}
	if op == "convert" {
		// A conversion indexes the fonts once a process: a service pays it
		// once, so the operation is measured with the index built, and the
		// "cold" operation measures it.
		if _, err := pdf.FromText(strings.NewReader("warm"), pdf.WithConvertFontDirs(fonts...)).WriteTo(ctx, io.Discard); err != nil {
			return fail(fmt.Errorf("building the font index: %w", err))
		}
	}
	jobs := make([]*costJob, copies)
	for i := range jobs {
		jobs[i] = &costJob{}
		if jobs[i].out, err = os.CreateTemp("", "litho-cost-*.out"); err != nil {
			return fail(err)
		}
		defer os.Remove(jobs[i].out.Name())
		defer jobs[i].out.Close()
	}
	probe, err := startCostProbe(profileDir)
	if err != nil {
		return fail(err)
	}
	var wg sync.WaitGroup
	for _, job := range jobs {
		wg.Go(func() { job.err = run(ctx, job) })
	}
	wg.Wait()
	sample, err := probe.finish()
	if err != nil {
		return fail(fmt.Errorf("writing the profiles: %w", err))
	}
	sample.Copies, sample.InputBytes = copies, inputBytes
	for _, job := range jobs {
		if job.err != nil {
			res := convertOneResult{Result: convertResultName(job.err), Detail: job.err.Error(), Cost: &sample}
			return res
		}
		sample.BudgetPeakBytes += job.budget.Load()
		sample.LogRecords += job.logs.Load()
	}
	// Every copy produced the same thing: the first one is described.
	first := jobs[0]
	sample.OutputPages, sample.OutputBytes, err = costOutput(ctx, first)
	if err != nil {
		return fail(fmt.Errorf("reading the output: %w", err))
	}
	return convertOneResult{Result: "pdf", Pages: sample.OutputPages, Cost: &sample}
}

// costOperation returns the function that runs op on src for one job, and
// the path of the document it reads.
func costOperation(op, src string, fonts []string) (func(context.Context, *costJob) error, string, error) {
	if op == "convert" || op == "cold" {
		source, err := loadRobustnessSource(src)
		if err != nil {
			return nil, "", err
		}
		client := &http.Client{Transport: refusingTransport{new(atomic.Int64)}}
		return func(ctx context.Context, job *costJob) error {
			in, err := os.Open(source.document) //nolint:gosec // a measurement input
			if err != nil {
				return err
			}
			defer in.Close()
			logger := slog.New(budgetPeakHandler{peak: &job.budget, records: &job.logs})
			opts := []pdf.ConvertOption{pdf.WithConvertLogger(logger)}
			if len(fonts) > 0 {
				opts = append(opts, pdf.WithConvertFontDirs(fonts...))
			}
			c, err := source.conversion(in, client, opts...)
			if err != nil {
				return err
			}
			job.report, err = c.WriteTo(ctx, job.out)
			return err
		}, src, nil
	}
	fn, ok := costOps[op]
	if !ok {
		return nil, "", fmt.Errorf("unknown operation %q", op)
	}
	return func(ctx context.Context, job *costJob) error {
		n, err := fn(ctx, src, fonts, job.out)
		job.text = n
		return err
	}, src, nil
}

// costInputBytes returns the size of the file at path, or of every file
// under the directory at path.
func costInputBytes(path string) (int64, error) {
	var total int64
	err := filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() && d.Name() != "options.json" {
			info, err := d.Info()
			if err != nil {
				return err
			}
			total += info.Size()
		}
		return nil
	})
	return total, err
}

// costOutput returns the pages and bytes of what job produced: a PDF's page
// count, read back, or none for text and pixels.
func costOutput(ctx context.Context, job *costJob) (int, int64, error) {
	if job.report != nil {
		info, err := job.out.Stat()
		if err != nil {
			return 0, 0, err
		}
		return job.report.Pages, info.Size(), nil
	}
	if job.text >= 0 {
		return 0, job.text, nil
	}
	info, err := job.out.Stat()
	if err != nil {
		return 0, 0, err
	}
	r, err := pdf.Open(ctx, job.out, info.Size())
	if err != nil {
		return 0, 0, err
	}
	defer r.Close()
	n, err := r.PageCount(ctx)
	return n, info.Size(), err
}
