package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"text/tabwriter"
	"time"
)

// `corpus cost` measures what an operation costs: each row, a conversion or
// a PDF operation on one input, runs in a child process of its own (`corpus
// convert-one --measure`), so the child's peak memory is the operation's.
// The reading prints the median of the runs and their spread. It decides
// nothing: a reading depends on the machine, and no bound or baseline reads
// it.

// costColumn is one measured quantity of a run.
type costColumn struct {
	key   string
	title string
	// scale divides the value for the text table.
	scale float64
	// format prints the scaled value.
	format string
	get    func(costRun) float64
}

// costRun is one run of a row: what the child read, and what the parent
// saw of the child process.
type costRun struct {
	costSample
	// RSSPeakBytes is the child process's peak resident memory.
	RSSPeakBytes int64 `json:"rss_peak_bytes"`
	// ProcessSeconds is the child's wall time, start to exit.
	ProcessSeconds float64 `json:"process_s"`
}

// costColumns are the quantities a reading prints, in order.
var costColumns = []costColumn{
	{"wall_s", "wall s", 1, "%.2f", func(r costRun) float64 { return r.WallSeconds }},
	{"cpu_s", "cpu s", 1, "%.2f", func(r costRun) float64 { return r.UserSeconds + r.SysSeconds }},
	{"rss_peak_bytes", "rss MiB", 1 << 20, "%.0f", func(r costRun) float64 { return float64(r.RSSPeakBytes) }},
	{"heap_peak_bytes", "heap MiB", 1 << 20, "%.0f", func(r costRun) float64 { return float64(r.HeapPeakBytes) }},
	{"budget_peak_bytes", "budget MiB", 1 << 20, "%.0f", func(r costRun) float64 { return float64(r.BudgetPeakBytes) }},
	{"alloc_bytes", "alloc MiB", 1 << 20, "%.0f", func(r costRun) float64 { return float64(r.AllocBytes) }},
	{"alloc_objects", "allocs k", 1000, "%.0f", func(r costRun) float64 { return float64(r.AllocObjects) }},
	{"gc_count", "gc", 1, "%.0f", func(r costRun) float64 { return float64(r.GCCount) }},
	{"gc_pause_s", "pause ms", 1e-3, "%.1f", func(r costRun) float64 { return r.GCPauseSeconds }},
	{"gc_cpu_share", "gc cpu %", 0.01, "%.0f", func(r costRun) float64 {
		if cpu := r.UserSeconds + r.SysSeconds; cpu > 0 {
			return r.GCCPUSeconds / cpu
		}
		return 0
	}},
	{"rss_base_bytes", "base MiB", 1 << 20, "%.0f", func(r costRun) float64 { return float64(r.RSSBaseBytes) }},
	{"process_s", "proc s", 1, "%.2f", func(r costRun) float64 { return r.ProcessSeconds }},
}

// costRatios are the ratios a reading derives from a conversion row's
// medians.
var costRatios = []struct {
	key, title string
	num, den   string
}{
	{"rss_per_input", "rss/in", "rss_peak_bytes", "input_bytes"},
	{"rss_per_budget", "rss/budget", "rss_peak_bytes", "budget_peak_bytes"},
	{"alloc_per_input", "alloc/in", "alloc_bytes", "input_bytes"},
}

// costRowReading is one row of a reading.
type costRowReading struct {
	Row         string `json:"row"`
	Op          string `json:"op"`
	Input       string `json:"input"`
	Kind        string `json:"kind"`
	Result      string `json:"result"`
	Detail      string `json:"detail,omitempty"`
	InputBytes  int64  `json:"input_bytes"`
	OutputPages int    `json:"output_pages"`
	OutputBytes int64  `json:"output_bytes"`
	// Median, Min and Max hold each column of [costColumns] over the runs,
	// and Median the ratios of [costRatios] for a conversion.
	Median map[string]float64 `json:"median"`
	Min    map[string]float64 `json:"min"`
	Max    map[string]float64 `json:"max"`
	Runs   []costRun          `json:"runs"`
	// Profile is the directory of the row's profiles, when a run wrote them.
	Profile string `json:"profile,omitempty"`
}

// costReading is what `corpus cost` prints as JSON.
type costReading struct {
	// Taken is when the reading began, for the reader: nothing compares it.
	Taken      string           `json:"taken"`
	GOOS       string           `json:"goos"`
	GOARCH     string           `json:"goarch"`
	CPUs       int              `json:"cpus"`
	GoVersion  string           `json:"go_version"`
	GOGC       string           `json:"gogc,omitempty"`
	GOMEMLIMIT string           `json:"gomemlimit,omitempty"`
	Runs       int              `json:"runs"`
	Copies     int              `json:"copies"`
	Rows       []costRowReading `json:"rows"`
}

// costDefaultInputs is where the generated inputs are built, under the
// testdata.
const costDefaultInputs = "cost"

// runCost runs `corpus cost`.
func runCost(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("cost", flag.ExitOnError)
	rows := fs.String("rows", "", "Rows to measure: names or path.Match globs, comma-separated (empty: all)")
	runs := fs.Int("runs", 3, "Runs of each row; the reading prints their median and spread")
	copies := fs.Int("copies", 1, "Copies of each row's operation run at once in its process")
	format := fs.String("format", "text", "Output format: text or json")
	out := fs.String("out", "", "Also write the JSON reading to this file")
	profile := fs.String("profile", "", "Directory: one more run of each row writes its CPU, heap, mutex and block profiles under DIR/<row>")
	inputs := fs.String("inputs", "", "Directory of the generated inputs, built there when missing (default: the testdata's "+costDefaultInputs+")")
	list := fs.Bool("list", false, "List the rows and their inputs, and measure nothing")
	compare := fs.Bool("compare", false, "Compare two JSON readings, BEFORE and AFTER, row by row")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *compare {
		if fs.NArg() != 2 {
			fmt.Fprintln(os.Stderr, "usage: corpus cost --compare BEFORE.json AFTER.json")
			return 2
		}
		if err := compareCost(os.Stdout, fs.Arg(0), fs.Arg(1)); err != nil {
			fmt.Fprintf(os.Stderr, "cost: %v\n", err)
			return 1
		}
		return 0
	}
	if fs.NArg() != 0 || *runs < 1 || *copies < 1 || (*format != "text" && *format != "json") {
		fmt.Fprintln(os.Stderr, "usage: corpus cost [--rows=GLOBS] [--runs=N] [--copies=N] [--format=text|json] [--out=FILE] [--profile=DIR] [--inputs=DIR] [--list]")
		return 2
	}
	selected, err := selectCostRows(costRows(), *rows)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cost: %v\n", err)
		return 2
	}
	testdata := testdataDir()
	if *inputs == "" {
		*inputs = filepath.Join(testdata, costDefaultInputs)
	}
	byName := costInputs()
	if *list {
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ROW\tOP\tINPUT\tKIND")
		for _, r := range selected {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", r.name(), r.op, costSource(byName[r.input], *inputs, testdata), byName[r.input].kind)
		}
		w.Flush()
		return 0
	}
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cost: %v\n", err)
		return 1
	}
	fonts, err := fixtureFontDirs(fixtureFontsRoot(), diffFontsTarget)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cost: %v\n", err)
		return 1
	}
	var want []costInput
	for _, r := range selected {
		want = append(want, byName[r.input])
	}
	if err := buildCostInputs(*inputs, want, func(s string) { fmt.Fprintln(os.Stderr, s) }); err != nil {
		fmt.Fprintf(os.Stderr, "cost: building the inputs: %v\n", err)
		return 1
	}
	m := costMeasurer{exe: exe, fonts: fonts, copies: *copies}
	reading := costReading{
		Taken: time.Now().UTC().Format(time.RFC3339), GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, CPUs: runtime.NumCPU(),
		GoVersion: runtime.Version(), GOGC: os.Getenv("GOGC"), GOMEMLIMIT: os.Getenv("GOMEMLIMIT"), Runs: *runs, Copies: *copies,
	}
	readings := make([]costRowReading, len(selected))
	for i, r := range selected {
		in := byName[r.input]
		readings[i] = costRowReading{Row: r.name(), Op: r.op, Input: r.input, Kind: in.kind}
	}
	// The runs go round the rows, so a drift of the machine over the
	// reading touches every row alike.
	for run := range *runs {
		for i, r := range selected {
			if err := ctx.Err(); err != nil {
				fmt.Fprintf(os.Stderr, "cost: %v\n", err)
				return 1
			}
			fmt.Fprintf(os.Stderr, "run %d/%d: %s\n", run+1, *runs, r.name())
			res, proc, err := m.run(ctx, r, costSource(byName[r.input], *inputs, testdata), "")
			readings[i].add(res, proc, err)
		}
	}
	if *profile != "" {
		for i, r := range selected {
			dir, err := filepath.Abs(filepath.Join(*profile, strings.ReplaceAll(r.name(), "/", "_")))
			if err != nil {
				fmt.Fprintf(os.Stderr, "cost: %v\n", err)
				return 1
			}
			fmt.Fprintf(os.Stderr, "profile: %s\n", r.name())
			if _, _, err := m.run(ctx, r, costSource(byName[r.input], *inputs, testdata), dir); err != nil {
				fmt.Fprintf(os.Stderr, "cost: profiling %s: %v\n", r.name(), err)
				return 1
			}
			readings[i].Profile = dir
		}
	}
	for i := range readings {
		readings[i].summarize()
	}
	reading.Rows = readings
	data, err := json.MarshalIndent(reading, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "cost: %v\n", err)
		return 1
	}
	if *out != "" {
		if err := os.WriteFile(*out, append(data, '\n'), 0o644); err != nil { //nolint:gosec // a reading, not a secret
			fmt.Fprintf(os.Stderr, "cost: %v\n", err)
			return 1
		}
	}
	if *format == "json" {
		fmt.Println(string(data))
		return 0
	}
	printCost(os.Stdout, reading)
	return 0
}

// selectCostRows returns the rows whose name matches one of the patterns,
// every row for none, failing on a pattern no row matches.
func selectCostRows(rows []costRow, patterns string) ([]costRow, error) {
	if patterns == "" {
		return rows, nil
	}
	var out []costRow
	for _, p := range strings.Split(patterns, ",") {
		p = strings.TrimSpace(p)
		matched := false
		for _, r := range rows {
			ok, err := path.Match(p, r.name())
			if err != nil {
				return nil, fmt.Errorf("pattern %q: %w", p, err)
			}
			if ok {
				matched = true
				if !slices.Contains(out, r) {
					out = append(out, r)
				}
			}
		}
		if !matched {
			return nil, fmt.Errorf("no row matches %q (corpus cost --list names them)", p)
		}
	}
	return out, nil
}

// costMeasurer runs one row in a child process.
type costMeasurer struct {
	exe    string
	fonts  []string
	copies int
}

// errCostCrash reports a child that ended without a result: a fatal error
// the operation's recover cannot catch, or a kill.
var errCostCrash = errors.New("the measured process crashed")

// run runs row r on src in a child process, profileDir receiving its
// profiles when set, and returns what the child read and what the parent
// saw. The child keeps its temp files in a directory of its own, removed
// when it ends.
func (m costMeasurer) run(ctx context.Context, r costRow, src, profileDir string) (convertOneResult, costProcess, error) {
	tmp, err := os.MkdirTemp("", "litho-cost-*")
	if err != nil {
		return convertOneResult{}, costProcess{}, err
	}
	defer os.RemoveAll(tmp)
	args := []string{
		"convert-one", "--measure", "--op=" + r.op, fmt.Sprintf("--copies=%d", m.copies),
		"--fonts=" + strings.Join(m.fonts, string(os.PathListSeparator)),
	}
	if profileDir != "" {
		args = append(args, "--profile="+profileDir)
	}
	cmd := exec.CommandContext(ctx, m.exe, append(args, src)...) //nolint:gosec // this binary, a measurement input
	// os.TempDir reads TMPDIR on Unix and TMP on Windows.
	cmd.Env = append(os.Environ(), "TMPDIR="+tmp, "TMP="+tmp)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	start := time.Now()
	runErr := cmd.Run()
	elapsed := time.Since(start)
	proc := costProcess{seconds: elapsed.Seconds()}
	if cmd.ProcessState != nil {
		proc.rss = maxRSS(cmd.ProcessState)
	}
	var res convertOneResult
	if err := json.Unmarshal(stdout.Bytes(), &res); runErr != nil || err != nil {
		tail := stderr.String()
		if len(tail) > stderrTail {
			tail = "..." + tail[len(tail)-stderrTail:]
		}
		return res, proc, fmt.Errorf("%w: %v: %s", errCostCrash, runErr, strings.TrimSpace(tail))
	}
	if res.Cost == nil {
		return res, proc, fmt.Errorf("%s ended %s without a measure: %s", r.name(), res.Result, res.Detail)
	}
	return res, proc, nil
}

// costProcess is what the parent saw of a child process: its peak resident
// memory and its wall time, start to exit.
type costProcess struct {
	rss     int64
	seconds float64
}

// add records one run's result.
func (rr *costRowReading) add(res convertOneResult, proc costProcess, err error) {
	switch {
	case err != nil:
		rr.Result, rr.Detail = "crash", err.Error()
		return
	case res.Result != "pdf":
		rr.Result, rr.Detail = res.Result, res.Detail
	case rr.Result == "":
		rr.Result = "pdf"
	}
	c := res.Cost
	if c == nil {
		return
	}
	rr.InputBytes, rr.OutputPages, rr.OutputBytes = c.InputBytes, c.OutputPages, c.OutputBytes
	rr.Runs = append(rr.Runs, costRun{costSample: *c, RSSPeakBytes: proc.rss, ProcessSeconds: proc.seconds})
}

// summarize fills the median, minimum and maximum of every column, and the
// ratios of a conversion.
func (rr *costRowReading) summarize() {
	if len(rr.Runs) == 0 {
		return
	}
	rr.Median, rr.Min, rr.Max = map[string]float64{}, map[string]float64{}, map[string]float64{}
	for _, c := range costColumns {
		v := make([]float64, len(rr.Runs))
		for i, r := range rr.Runs {
			v[i] = c.get(r)
		}
		slices.Sort(v)
		rr.Min[c.key], rr.Max[c.key] = v[0], v[len(v)-1]
		if n := len(v); n%2 == 1 {
			rr.Median[c.key] = v[n/2]
		} else {
			rr.Median[c.key] = (v[n/2-1] + v[n/2]) / 2
		}
	}
	if rr.Op != "convert" && rr.Op != "cold" {
		return
	}
	values := map[string]float64{"input_bytes": float64(rr.InputBytes)}
	for k, v := range rr.Median {
		values[k] = v
	}
	for _, r := range costRatios {
		if d := values[r.den]; d > 0 {
			rr.Median[r.key] = values[r.num] / d
		}
	}
}

// costSpread returns how far the runs of a column lie apart, as a share of
// their median.
func costSpread(rr costRowReading, key string) float64 {
	m := rr.Median[key]
	if m == 0 {
		return 0
	}
	return (rr.Max[key] - rr.Min[key]) / m
}

// printCost prints a reading as a table: each column's median, and for the
// wall time, the CPU time and the peak memory their spread.
func printCost(w io.Writer, reading costReading) {
	fmt.Fprintf(w, "%s/%s, %d CPUs, %s, %d runs, %d copies at once", reading.GOOS, reading.GOARCH, reading.CPUs, reading.GoVersion, reading.Runs, reading.Copies)
	if reading.GOGC != "" {
		fmt.Fprintf(w, ", GOGC=%s", reading.GOGC)
	}
	if reading.GOMEMLIMIT != "" {
		fmt.Fprintf(w, ", GOMEMLIMIT=%s", reading.GOMEMLIMIT)
	}
	fmt.Fprintln(w)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', tabwriter.AlignRight)
	head := []string{"row", "result"}
	for _, c := range costColumns {
		head = append(head, c.title)
		if costSpreadColumns[c.key] {
			head = append(head, "±%")
		}
	}
	head = append(head, "in KiB", "pages", "out KiB")
	for _, r := range costRatios {
		head = append(head, r.title)
	}
	fmt.Fprintln(tw, strings.Join(head, "\t")+"\t")
	for _, rr := range reading.Rows {
		cells := []string{rr.Row, rr.Result}
		for _, c := range costColumns {
			if rr.Median == nil {
				cells = append(cells, "-")
			} else {
				cells = append(cells, fmt.Sprintf(c.format, rr.Median[c.key]/c.scale))
			}
			if costSpreadColumns[c.key] {
				cells = append(cells, fmt.Sprintf("%.0f", 100*costSpread(rr, c.key)))
			}
		}
		cells = append(cells, fmt.Sprintf("%.0f", float64(rr.InputBytes)/1024), fmt.Sprintf("%d", rr.OutputPages), fmt.Sprintf("%.0f", float64(rr.OutputBytes)/1024))
		for _, r := range costRatios {
			if v, ok := rr.Median[r.key]; ok {
				cells = append(cells, fmt.Sprintf("%.1f", v))
			} else {
				cells = append(cells, "")
			}
		}
		fmt.Fprintln(tw, strings.Join(cells, "\t")+"\t")
	}
	tw.Flush()
	for _, rr := range reading.Rows {
		if rr.Detail != "" {
			fmt.Fprintf(w, "%s: %s: %s\n", rr.Row, rr.Result, rr.Detail)
		}
	}
}

// costSpreadColumns are the columns whose spread the table prints: those
// the machine's load moves.
var costSpreadColumns = map[string]bool{"wall_s": true, "cpu_s": true, "rss_peak_bytes": true}

// compareCost prints, for each row two readings share, every column's
// median before and after and its change.
func compareCost(w io.Writer, beforePath, afterPath string) error {
	before, err := readCostReading(beforePath)
	if err != nil {
		return err
	}
	after, err := readCostReading(afterPath)
	if err != nil {
		return err
	}
	prev := map[string]costRowReading{}
	for _, rr := range before.Rows {
		prev[rr.Row] = rr
	}
	keys := make([]string, 0, len(costColumns)+len(costRatios))
	titles := map[string]string{}
	scales := map[string]float64{}
	formats := map[string]string{}
	for _, c := range costColumns {
		keys = append(keys, c.key)
		titles[c.key], scales[c.key], formats[c.key] = c.title, c.scale, c.format
	}
	for _, r := range costRatios {
		keys = append(keys, r.key)
		titles[r.key], scales[r.key], formats[r.key] = r.title, 1, "%.1f"
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintln(tw, "row\tcolumn\tbefore\tafter\tchange %\t")
	var missing []string
	for _, a := range after.Rows {
		b, ok := prev[a.Row]
		if !ok {
			missing = append(missing, a.Row+" (after only)")
			continue
		}
		delete(prev, a.Row)
		if b.Result != a.Result {
			fmt.Fprintf(tw, "%s\tresult\t%s\t%s\t\t\n", a.Row, b.Result, a.Result)
		}
		for _, k := range keys {
			bv, bok := b.Median[k]
			av, aok := a.Median[k]
			if !bok || !aok {
				continue
			}
			change := ""
			if bv != 0 {
				change = fmt.Sprintf("%+.1f", 100*(av-bv)/math.Abs(bv))
			}
			fmt.Fprintf(tw, "%s\t%s\t"+formats[k]+"\t"+formats[k]+"\t%s\t\n", a.Row, titles[k], bv/scales[k], av/scales[k], change)
		}
	}
	tw.Flush()
	for _, b := range before.Rows {
		if _, ok := prev[b.Row]; ok {
			missing = append(missing, b.Row+" (before only)")
		}
	}
	if len(missing) > 0 {
		fmt.Fprintf(w, "not compared: %s\n", strings.Join(missing, ", "))
	}
	if before.Copies != after.Copies || before.GOGC != after.GOGC || before.GOMEMLIMIT != after.GOMEMLIMIT || before.CPUs != after.CPUs {
		fmt.Fprintf(w, "the readings differ in setting: copies %d and %d, GOGC %q and %q, GOMEMLIMIT %q and %q, %d and %d CPUs\n",
			before.Copies, after.Copies, before.GOGC, after.GOGC, before.GOMEMLIMIT, after.GOMEMLIMIT, before.CPUs, after.CPUs)
	}
	return nil
}

// readCostReading reads a JSON reading.
func readCostReading(p string) (costReading, error) {
	var r costReading
	data, err := os.ReadFile(p) //nolint:gosec // a reading the caller names
	if err != nil {
		return r, err
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return r, fmt.Errorf("%s: %w", p, err)
	}
	return r, nil
}
